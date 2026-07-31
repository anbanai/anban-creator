package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	appconfig "github.com/anbanai/anban-creator/app/config"
	appimage "github.com/anbanai/anban-creator/app/image"
	serverbilling "github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type designerFixedSKUFixture struct {
	service *DesignerService
	repo    repository.Repository
	catalog *BillingCatalogService
	wallet  *BillingWalletService
	userID  string
	db      *gorm.DB
}

type failFirstTransactionRepository struct {
	repository.Repository
	fail bool
}

func (r *failFirstTransactionRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	if r.fail {
		r.fail = false
		return errors.New("transient transaction failure")
	}
	return r.Repository.WithTx(ctx, fn)
}

func newDesignerFixedSKUFixture(t *testing.T, paid int64) *designerFixedSKUFixture {
	t.Helper()
	repo, db := newBillingServiceRepositoryWithDB(t)
	userID := uuid.NewString()
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@test.invalid", Password: "fixture", InviteCode: uuid.NewString()[:8]}); err != nil {
		t.Fatal(err)
	}
	bundle := serverbilling.Bundle{
		Policy: serverbilling.PolicyCatalog{
			AcceptedTask:        serverbilling.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TaskFailureReversal: serverbilling.TaskFailureReversalPolicy{Enabled: true, Reasons: []string{"platform_error", "provider_error", "execution_timeout", "infrastructure_cancelled"}},
		},
		Products: serverbilling.ProductCatalog{CatalogID: "retail-designer-v1", Currency: "credits", TierRatesPercent: map[string]int64{"free": 100, "pro": 90, "enterprise": 80}, SKUs: []serverbilling.SKUConfig{{
			ID: "image.seedream.designer", Operation: "designer.generate_image", ChargePolicy: "standalone_operation",
			PriceCredits: 500, Route: "image_generation.designer.seedream", Delivery: "persisted_image",
		}}},
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	catalog := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: userID, PaidCredits: paid}); err != nil {
		t.Fatal(err)
	}
	if paid > 0 {
		createBillingLot(t, repo, model.BillingCreditLot{
			ID: uuid.NewString(), UserID: userID, Kind: model.BillingCreditLotKindPaid,
			SourceType: "fixture", SourceID: uuid.NewString(), CatalogID: bundle.Products.CatalogID,
			OriginalCredits: paid, AvailableCredits: paid, CreatedAt: now,
		})
	}
	wallet := NewBillingWalletService(repo, &bundle, BillingWalletOptions{Now: func() time.Time { return now }})
	enabled := true
	cfg := &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{Designer: map[string]srvconfig.ImageGenerationRouteConfig{
			"seedream": {Enabled: true, Provider: "volcengine_ark", Model: "doubao-seedream-5-0-pro-260628", Capabilities: DesignerProviderCapabilities{MaxBatch: 1}},
		}}},
		ImageAPI: srvconfig.ImageAPIConfig{Designer: map[string]*appconfig.ImageAPI{
			"seedream": {Enable: &enabled, Provider: "volcengine", Model: "doubao-seedream-5-0-pro-260628"},
		}},
	}
	logger := zerolog.New(io.Discard)
	service := NewDesignerService(db, cfg, nil, &logger)
	service.SetBillingCatalogService(catalog)
	service.SetBillingWalletService(wallet)
	return &designerFixedSKUFixture{service: service, repo: repo, catalog: catalog, wallet: wallet, userID: userID, db: db}
}

func (f *designerFixedSKUFixture) request(t *testing.T) DesignerGenerateRequest {
	t.Helper()
	req := DesignerGenerateRequest{
		OperationID: uuid.NewString(), ProjectID: uuid.NewString(), Prompt: "product poster",
		ProviderID: "seedream", Size: "1:1", N: 1,
	}
	req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, req)
	quote, err := f.catalog.CreateQuote(context.Background(), QuoteRequest{
		UserID: f.userID, Operation: "designer.generate_image", Route: "image_generation.designer.seedream",
		RequestFingerprint: req.RequestFingerprint, IdempotencyScope: "designer-quote", IdempotencyKey: req.OperationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	req.QuoteID = quote.ID
	return req
}

func TestDesignerCreateGenerationChargesFixedStandaloneSKU(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	if created.GenerationID != req.OperationID || created.PriceCredits != 500 {
		t.Fatalf("created = %#v", created)
	}
	account, err := f.repo.Billing().FindAccount(context.Background(), f.userID)
	if err != nil || account.PaidCredits != 0 || account.DebtCredits != 0 {
		t.Fatalf("account = %#v err=%v", account, err)
	}
	var generation model.ImageGeneration
	if err := f.db.First(&generation, "id = ?", req.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if generation.Cost != 500 || generation.EstimatedCost != 500 || generation.BillingStatus != "charged" || generation.BillingChargeID == nil || *generation.BillingChargeID == "" || generation.BillingQuoteID != req.QuoteID {
		t.Fatalf("generation billing = %#v", generation)
	}
}

func TestDesignerCreateGenerationRejectsInsufficientWalletBeforeProvider(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 499)
	req := f.request(t)
	providerCalls := 0
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		providerCalls++
		return &fakeImageProvider{}, nil
	}
	_, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if !errors.Is(err, ErrBillingInsufficientForStandaloneOperation) {
		t.Fatalf("error = %v", err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d", providerCalls)
	}
	var count int64
	if err := f.db.Model(&model.ImageGeneration{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("generation count = %d err=%v", count, err)
	}
}

func TestDesignerCreateGenerationRejectsBatchWithoutCountSKU(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 1000)
	req := f.request(t)
	req.N = 2
	req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, req)
	_, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err == nil || err.Error() != "n must equal 1; batch designer SKUs are not configured" {
		t.Fatalf("error = %v", err)
	}
}

func TestDesignerCreateGenerationRollsBackWalletAndQuoteWhenRecordInsertFails(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	callbackName := "test:fail-designer-generation"
	if err := f.db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == (model.ImageGeneration{}).TableName() {
			tx.AddError(errors.New("forced generation insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove(callbackName) })

	if _, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req); err == nil || !strings.Contains(err.Error(), "forced generation insert failure") {
		t.Fatalf("error = %v", err)
	}
	account, err := f.repo.Billing().FindAccount(context.Background(), f.userID)
	if err != nil || account.PaidCredits != 500 || account.DebtCredits != 0 {
		t.Fatalf("account after rollback = %#v err=%v", account, err)
	}
	var quote model.BillingQuote
	if quoteErr := f.db.First(&quote, "id = ?", req.QuoteID).Error; quoteErr != nil || quote.ConsumedAt != nil {
		t.Fatalf("quote after rollback = %#v err=%v", quote, quoteErr)
	}
	var charges int64
	if err := f.db.Model(&model.BillingCharge{}).Count(&charges).Error; err != nil || charges != 0 {
		t.Fatalf("charge count after rollback = %d err=%v", charges, err)
	}
}

func TestDesignerExecuteGenerationReversesFixedChargeOnProviderFailure(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeImageProvider{err: errors.New("provider unavailable")}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) { return provider, nil }
	f.service.ExecuteGeneration(context.Background(), created.GenerationID)
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d", provider.calls)
	}
	account, err := f.repo.Billing().FindAccount(context.Background(), f.userID)
	if err != nil || account.PaidCredits != 0 || account.DebtCredits != 0 {
		t.Fatalf("account before outbox reversal = %#v err=%v", account, err)
	}
	var generation model.ImageGeneration
	if err := f.db.First(&generation, "id = ?", created.GenerationID).Error; err != nil || generation.Status != model.ImageGenerationStatusFailed {
		t.Fatalf("generation = %#v err=%v", generation, err)
	}
	var settlement model.BillingSettlementOutbox
	if err := f.db.First(&settlement, "resource_type = ? AND resource_id = ?", "image_generation", created.GenerationID).Error; err != nil || settlement.Action != model.BillingSettlementActionReverseOperation || settlement.Status != "pending" {
		t.Fatalf("settlement = %#v err=%v", settlement, err)
	}
	f.service.billingWallet.repo = &failFirstTransactionRepository{Repository: f.repo, fail: true}
	if processed, err := f.service.billingWallet.ProcessSettlementOutbox(context.Background(), 10); err == nil || processed != 0 {
		t.Fatalf("transient process = %d, %v", processed, err)
	}
	if processed, err := f.service.billingWallet.ProcessSettlementOutbox(context.Background(), 10); err != nil || processed != 1 {
		t.Fatalf("retry process = %d, %v", processed, err)
	}
	if processed, err := f.service.billingWallet.ProcessSettlementOutbox(context.Background(), 10); err != nil || processed != 0 {
		t.Fatalf("replay process = %d, %v", processed, err)
	}
	account, err = f.repo.Billing().FindAccount(context.Background(), f.userID)
	if err != nil || account.PaidCredits != 500 || account.DebtCredits != 0 {
		t.Fatalf("account after outbox reversal = %#v err=%v", account, err)
	}
	var reversals int64
	if err := f.db.Model(&model.BillingCharge{}).Where("charge_kind = ?", model.BillingChargeKindReversal).Count(&reversals).Error; err != nil || reversals != 1 {
		t.Fatalf("reversal count = %d err=%v", reversals, err)
	}
}
