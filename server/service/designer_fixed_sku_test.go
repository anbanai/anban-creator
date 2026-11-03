package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appconfig "github.com/anbanai/anban-creator/server/app/config"
	appimage "github.com/anbanai/anban-creator/server/app/image"
	serverbilling "github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type designerFixedSKUFixture struct {
	service   *DesignerService
	repo      repository.Repository
	catalog   *BillingCatalogService
	wallet    *BillingWalletService
	userID    string
	projectID string
	db        *gorm.DB
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
	projectID := uuid.NewString()
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: "wechat", Name: "Designer test"}); err != nil {
		t.Fatal(err)
	}
	bundle := serverbilling.Bundle{
		Economics: serverbilling.EconomicsConfig{CreditsPerCNY: 1_000},
		Policy: serverbilling.PolicySnapshot{
			AcceptedTask:        serverbilling.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TaskFailureReversal: serverbilling.TaskFailureReversalPolicy{Enabled: true, Reasons: []string{"platform_error", "provider_error", "execution_timeout", "infrastructure_cancelled"}},
		},
		Products: serverbilling.ProductCatalog{CatalogID: "retail-designer-v1", Currency: "credits", TierRatesPercent: map[string]int64{"free": 100, "pro": 90, "enterprise": 80}, SKUs: []serverbilling.SKUConfig{{
			ID: "image.standard", Operation: "image.generate", ChargePolicy: "image_operation",
			PriceCredits: 500, Route: "image_generation.capabilities.standard", Delivery: "persisted_image",
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
	cfg := &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{DefaultCapability: "standard", Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
			"standard": {Enabled: true, Provider: "volcengine", Model: "image-v1", BaseURL: "https://images.example.com", APIKey: "secret", Alias: "Standard", MinTier: "free", BillingSKU: "image.standard", GenerationFeatures: ImageGenerationFeatures{MaxBatch: 1, DefaultSize: "1:1", SizePresets: []string{"1:1"}, QualityLevels: []string{"auto"}, OutputFormats: []string{"png"}}},
		}}},
	}
	logger := zerolog.New(io.Discard)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewDesignerService(db, cfg, store, &logger)
	service.SetBillingCatalogService(catalog)
	service.SetBillingWalletService(wallet)
	return &designerFixedSKUFixture{service: service, repo: repo, catalog: catalog, wallet: wallet, userID: userID, projectID: projectID, db: db}
}

func (f *designerFixedSKUFixture) request(t *testing.T) DesignerGenerateRequest {
	t.Helper()
	req := DesignerGenerateRequest{
		OperationID: uuid.NewString(), ProjectID: f.projectID, Prompt: "product poster",
		CapabilityKey: "standard", Size: "1:1", Quality: "auto", N: 1, OutputFormat: "png",
	}
	req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, req)
	quote, err := f.catalog.CreateQuote(context.Background(), QuoteRequest{
		UserID: f.userID, Operation: "image.generate", Route: "image_generation.capabilities.standard",
		RequestFingerprint: req.RequestFingerprint, IdempotencyScope: "designer-quote", IdempotencyKey: req.OperationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	req.QuoteID = quote.ID
	return req
}

func TestDesignerQuoteRejectsForeignProject(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	req.OperationID = uuid.NewString()
	req.ProjectID = uuid.NewString()
	_, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req)
	if !errors.Is(err, ErrProjectNotFound) && !errors.Is(err, ErrProjectOwnedByUser) {
		t.Fatalf("quote error = %v, want project ownership rejection", err)
	}
}

func TestDesignerRepeatedExecutionCallsProviderOnceAndSettlesOnce(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeImageProvider{err: errors.New("provider unavailable")}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) { return provider, nil }
	f.service.ExecuteGeneration(context.Background(), created.GenerationID)
	f.service.ExecuteGeneration(context.Background(), created.GenerationID)
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	var charges, settlements int64
	f.db.Model(&model.BillingCharge{}).Where("resource_type = ? AND resource_id = ?", "image_generation", created.GenerationID).Count(&charges)
	f.db.Model(&model.BillingSettlementOutbox{}).Where("resource_type = ? AND resource_id = ?", "image_generation", created.GenerationID).Count(&settlements)
	if charges != 1 || settlements != 1 {
		t.Fatalf("charges = %d, settlements = %d, want 1 each", charges, settlements)
	}
}

func TestDesignerAdmissionReplayAndConflictingOperation(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	first, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil || replay.GenerationID != first.GenerationID {
		t.Fatalf("replay = %#v, err=%v", replay, err)
	}
	conflict := req
	conflict.Prompt = "different work"
	conflict.RequestFingerprint = DesignerGenerationFingerprint(f.userID, conflict)
	if _, err := f.service.CreateGenerationRecord(context.Background(), f.userID, conflict); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	var generations, charges int64
	f.db.Model(&model.ImageGeneration{}).Where("id = ?", req.OperationID).Count(&generations)
	f.db.Model(&model.BillingCharge{}).Where("resource_type = ? AND resource_id = ? AND charge_kind = ?", "image_generation", req.OperationID, model.BillingChargeKindOperation).Count(&charges)
	if generations != 1 || charges != 1 {
		t.Fatalf("generations=%d charges=%d, want 1 each", generations, charges)
	}
}

func TestDesignerAdmissionRejectsExpiredAndMismatchedQuotesWithoutCharge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*designerFixedSKUFixture, *DesignerGenerateRequest)
	}{
		{"expired", func(f *designerFixedSKUFixture, req *DesignerGenerateRequest) {
			if err := f.db.Model(&model.BillingQuote{}).Where("id = ?", req.QuoteID).Update("expires_at", time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"fingerprint mismatch", func(_ *designerFixedSKUFixture, req *DesignerGenerateRequest) { req.RequestFingerprint = "different" }},
		{"persisted quote for another request", func(f *designerFixedSKUFixture, req *DesignerGenerateRequest) {
			req.Prompt = "different work"
			req.OperationID = uuid.NewString()
			req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, *req)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDesignerFixedSKUFixture(t, 500)
			req := f.request(t)
			tc.mutate(f, &req)
			_, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
			if tc.name == "expired" && !errors.Is(err, ErrBillingQuoteExpired) {
				t.Fatalf("expired quote error = %v", err)
			}
			if tc.name != "expired" && !errors.Is(err, ErrBillingQuoteMismatch) {
				t.Fatalf("mismatched quote error = %v", err)
			}
			var generations, charges int64
			f.db.Model(&model.ImageGeneration{}).Count(&generations)
			f.db.Model(&model.BillingCharge{}).Where("charge_kind = ?", model.BillingChargeKindOperation).Count(&charges)
			if generations != 0 || charges != 0 {
				t.Fatalf("generations=%d charges=%d, want zero", generations, charges)
			}
		})
	}
}

func TestDesignerConcurrentAdmissionChargesAndCreatesOnce(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	req := f.request(t)
	const workers = 6
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
			if err == nil && created.GenerationID != req.OperationID {
				err = errors.New("wrong generation ID")
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var generations, charges int64
	f.db.Model(&model.ImageGeneration{}).Where("id = ?", req.OperationID).Count(&generations)
	f.db.Model(&model.BillingCharge{}).Where("resource_type = ? AND resource_id = ? AND charge_kind = ?", "image_generation", req.OperationID, model.BillingChargeKindOperation).Count(&charges)
	if generations != 1 || charges != 1 {
		t.Fatalf("generations=%d charges=%d, want 1 each", generations, charges)
	}
}

func TestDesignerConcurrentExecutionCallsProviderOnce(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return designerReferenceProvider{generate: func(*appimage.GenerateOptions) (*appimage.GenerateResult, error) {
			calls.Add(1)
			close(started)
			<-release
			return nil, errors.New("provider failed")
		}}, nil
	}
	go func() { f.service.ExecuteGeneration(context.Background(), created.GenerationID); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first provider call did not start")
	}
	f.service.ExecuteGeneration(context.Background(), created.GenerationID)
	close(release)
	<-done
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
	var charges, settlements int64
	f.db.Model(&model.BillingCharge{}).Where("resource_type = ? AND resource_id = ? AND charge_kind = ?", "image_generation", req.OperationID, model.BillingChargeKindOperation).Count(&charges)
	f.db.Model(&model.BillingSettlementOutbox{}).Where("resource_type = ? AND resource_id = ?", "image_generation", req.OperationID).Count(&settlements)
	if charges != 1 || settlements != 1 {
		t.Fatalf("charges=%d settlements=%d, want 1 each", charges, settlements)
	}
}

type designerContextProvider struct{ generate func(context.Context) error }

func (designerContextProvider) Name() string { return "context-test" }
func (p designerContextProvider) Generate(ctx context.Context, _ string, _ *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
	return nil, p.generate(ctx)
}
func (designerContextProvider) Capabilities() *appimage.ProviderCapabilities {
	return &appimage.ProviderCapabilities{}
}

func TestDesignerProviderObservesConfiguredExecutionTimeout(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	route := f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"]
	route.Timeout = 40 * time.Millisecond
	f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	var sawDeadline atomic.Bool
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return designerContextProvider{generate: func(ctx context.Context) error {
			if _, ok := ctx.Deadline(); ok {
				sawDeadline.Store(true)
			}
			<-ctx.Done()
			return ctx.Err()
		}}, nil
	}
	started := time.Now()
	f.service.ExecuteGeneration(context.Background(), created.GenerationID)
	if !sawDeadline.Load() || time.Since(started) > time.Second {
		t.Fatalf("provider timeout not observed, deadline=%v elapsed=%s", sawDeadline.Load(), time.Since(started))
	}
	var gen model.ImageGeneration
	if err := f.db.First(&gen, "id = ?", req.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if gen.Status != model.ImageGenerationStatusFailed || gen.ExecutionDeadline == nil {
		t.Fatalf("generation = %#v", gen)
	}
}

func TestDesignerRecoveryExpiresStartedGenerationWithoutProviderReplay(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-10 * time.Minute)
	deadline := time.Now().Add(-time.Minute)
	if err := f.db.Model(&model.ImageGeneration{}).Where("id = ?", created.GenerationID).Updates(map[string]any{"started_at": started, "execution_deadline": deadline}).Error; err != nil {
		t.Fatal(err)
	}
	provider := &fakeImageProvider{err: errors.New("must not run")}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) { return provider, nil }
	if err := f.service.RecoverGenerations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RecoverGenerations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
	var gen model.ImageGeneration
	if err := f.db.First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if gen.Status != model.ImageGenerationStatusFailed {
		t.Fatalf("status = %s, want failed", gen.Status)
	}
	var settlements int64
	f.db.Model(&model.BillingSettlementOutbox{}).Where("resource_type = ? AND resource_id = ?", "image_generation", gen.ID).Count(&settlements)
	if settlements != 1 {
		t.Fatalf("settlements = %d, want 1", settlements)
	}
}

func TestDesignerRecoveryDispatchesOnlyUnstartedGeneration(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	generatedPath := t.TempDir() + "/generated.png"
	if err := os.WriteFile(generatedPath, taskImagePNG(4, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &fakeImageProvider{result: &appimage.GenerateResult{URL: generatedPath, ProviderRequestID: "recovered", ResponseType: "file"}}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) { return provider, nil }
	if err := f.service.RecoverGenerations(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var gen model.ImageGeneration
		if err := f.db.First(&gen, "id = ?", created.GenerationID).Error; err != nil {
			t.Fatal(err)
		}
		if gen.Status == model.ImageGenerationStatusCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var gen model.ImageGeneration
	if err := f.db.First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if gen.Status != model.ImageGenerationStatusCompleted || provider.calls != 1 {
		t.Fatalf("status=%s provider calls=%d", gen.Status, provider.calls)
	}
	var results int64
	f.db.Model(&model.ImageGenerationResult{}).Where("generation_id = ?", gen.ID).Count(&results)
	if results != 1 {
		t.Fatalf("results = %d, want 1", results)
	}
}

func TestDesignerStorageFailureLeavesNoDeliveredResultAndRequestsReversal(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	generatedPath := t.TempDir() + "/generated.png"
	if err := os.WriteFile(generatedPath, taskImagePNG(4, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	f.service.storage = &designerReferenceStore{}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return &fakeImageProvider{result: &appimage.GenerateResult{URL: generatedPath, ProviderRequestID: "storage-failure", ResponseType: "file"}}, nil
	}
	f.service.ExecuteGeneration(context.Background(), created.GenerationID)
	var gen model.ImageGeneration
	if err := f.db.First(&gen, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if gen.Status != model.ImageGenerationStatusFailed {
		t.Fatalf("status = %s, want failed", gen.Status)
	}
	var results, settlements int64
	f.db.Model(&model.ImageGenerationResult{}).Where("generation_id = ?", gen.ID).Count(&results)
	f.db.Model(&model.BillingSettlementOutbox{}).Where("resource_type = ? AND resource_id = ?", "image_generation", gen.ID).Count(&settlements)
	if results != 0 || settlements != 1 {
		t.Fatalf("results=%d settlements=%d, want 0 and 1", results, settlements)
	}
}

func TestDesignerLateResultCannotCompleteFailedGeneration(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-10 * time.Minute)
	deadline := time.Now().Add(-time.Minute)
	if err := f.db.Model(&model.ImageGeneration{}).Where("id = ?", created.GenerationID).Updates(map[string]any{"started_at": started, "execution_deadline": deadline}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.service.RecoverGenerations(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := f.repo.ImageGenerations().Complete(context.Background(), created.GenerationID, "late", []model.ImageGenerationResult{{GenerationID: created.GenerationID, FileID: "late.png", ImageURL: "/late.png"}}, time.Now())
	if err != nil || completed {
		t.Fatalf("late completion = %v, err=%v, want false", completed, err)
	}
	var results int64
	f.db.Model(&model.ImageGenerationResult{}).Where("generation_id = ?", created.GenerationID).Count(&results)
	if results != 0 {
		t.Fatalf("late result rows = %d, want 0", results)
	}
}

func TestDesignerOutputFailureHidesSourceLocations(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	for _, tc := range []struct{ name, source, secret string }{
		{"missing local file", "/private/anban-secret/generated.png", "anban-secret"},
		{"signed remote URL", closedURL + "/generated.png?signature=credential-secret", "credential-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDesignerFixedSKUFixture(t, 500)
			req := f.request(t)
			created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
			if err != nil {
				t.Fatal(err)
			}
			f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
				return &fakeImageProvider{result: &appimage.GenerateResult{URL: tc.source, ProviderRequestID: "redacted-output"}}, nil
			}
			f.service.ExecuteGeneration(context.Background(), created.GenerationID)
			public, err := f.service.GetGeneration(context.Background(), f.userID, created.GenerationID)
			if err != nil {
				t.Fatal(err)
			}
			if public.Status != model.ImageGenerationStatusFailed || public.ErrorCode != "image_output_unavailable" || strings.Contains(public.Error, tc.secret) || strings.Contains(public.Error, tc.source) {
				t.Fatalf("public failure = %#v", public)
			}
			var persisted model.ImageGeneration
			if err := f.db.First(&persisted, "id = ?", created.GenerationID).Error; err != nil {
				t.Fatal(err)
			}
			if strings.Contains(persisted.Error, tc.secret) {
				t.Fatalf("persisted error leaked source: %q", persisted.Error)
			}
		})
	}
}

func TestDesignerProviderPanicHidesPanicPayload(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return designerReferenceProvider{generate: func(*appimage.GenerateOptions) (*appimage.GenerateResult, error) {
			panic("https://provider.test/result?signature=panic-secret")
		}}, nil
	}
	f.service.ExecuteGeneration(context.Background(), created.GenerationID)
	public, err := f.service.GetGeneration(context.Background(), f.userID, created.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if public.Status != model.ImageGenerationStatusFailed || strings.Contains(public.Error, "panic-secret") || strings.Contains(public.Error, "provider.test") {
		t.Fatalf("public panic failure = %#v", public)
	}
}

func TestDesignerHistoryRetainsAdvancedOptionsAndEditInputs(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	route := f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"]
	route.GenerationFeatures.SupportsReference = true
	route.GenerationFeatures.SupportsMask = true
	route.GenerationFeatures.MaxReferenceImages = 2
	route.GenerationFeatures.HasCompression = true
	route.GenerationFeatures.HasBackground = true
	route.GenerationFeatures.Watermark = true
	f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
	refID, err := f.service.UploadReference(context.Background(), f.userID, "reference.png", "image/png", taskImagePNG(4, 4))
	if err != nil {
		t.Fatal(err)
	}
	maskID, err := f.service.UploadReference(context.Background(), f.userID, "mask.png", "image/png", taskImagePNG(4, 4))
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	req := DesignerGenerateRequest{OperationID: uuid.NewString(), ProjectID: f.projectID, Prompt: "retouch", CapabilityKey: "standard", Quality: "auto", Size: "1:1", N: 1, OutputFormat: "png", OutputCompression: 67, Background: "transparent", Watermark: &enabled, ReferenceFileIDs: []string{refID}, MaskFileID: maskID}
	quote, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	req.QuoteID, req.RequestFingerprint = quote.QuoteID, quote.RequestFingerprint
	if _, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req); err != nil {
		t.Fatal(err)
	}
	for _, public := range []DesignerGenerationPublic{func() DesignerGenerationPublic {
		v, err := f.service.GetGeneration(context.Background(), f.userID, req.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		return *v
	}(), func() DesignerGenerationPublic {
		rows, _, err := f.service.GetHistory(context.Background(), f.userID, f.projectID, 1, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("history rows = %d", len(rows))
		}
		return rows[0]
	}()} {
		if public.OutputCompression != 67 || public.Background != "transparent" || !public.Watermark || public.MaskFileID != maskID || len(public.ReferenceFileIDs) != 1 || public.ReferenceFileIDs[0] != refID {
			t.Fatalf("restored options = %#v", public)
		}
	}
}

func TestDesignerReferenceAndMaskGenerationDeliversStoredResult(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	route := f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"]
	route.GenerationFeatures.SupportsReference = true
	route.GenerationFeatures.SupportsMask = true
	route.GenerationFeatures.MaxReferenceImages = 2
	f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
	input := taskImagePNG(4, 4)
	refID, err := f.service.UploadReference(context.Background(), f.userID, "reference.png", "image/png", input)
	if err != nil {
		t.Fatal(err)
	}
	maskID, err := f.service.UploadReference(context.Background(), f.userID, "mask.png", "image/png", input)
	if err != nil {
		t.Fatal(err)
	}
	req := DesignerGenerateRequest{OperationID: uuid.NewString(), ProjectID: f.projectID, Prompt: "edit", CapabilityKey: "standard", Quality: "auto", Size: "1:1", N: 1, OutputFormat: "png", ReferenceFileIDs: []string{refID}, MaskFileID: maskID}
	quote, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	req.QuoteID, req.RequestFingerprint = quote.QuoteID, quote.RequestFingerprint
	if _, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req); err != nil {
		t.Fatal(err)
	}
	outputPath := t.TempDir() + "/edited.png"
	if err := os.WriteFile(outputPath, input, 0o644); err != nil {
		t.Fatal(err)
	}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return designerReferenceProvider{generate: func(opts *appimage.GenerateOptions) (*appimage.GenerateResult, error) {
			if len(opts.RefImagePaths) != 1 || opts.MaskPath == "" {
				t.Fatalf("missing edit inputs: %#v", opts)
			}
			refBytes, err := os.ReadFile(opts.RefImagePaths[0])
			if err != nil {
				t.Fatal(err)
			}
			maskBytes, err := os.ReadFile(opts.MaskPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(refBytes, input) || !bytes.Equal(maskBytes, input) {
				t.Fatal("provider received wrong edit bytes")
			}
			return &appimage.GenerateResult{URL: outputPath, ProviderRequestID: "mask-edit", ResponseType: "file"}, nil
		}}, nil
	}
	f.service.ExecuteGeneration(context.Background(), req.OperationID)
	public, err := f.service.GetGeneration(context.Background(), f.userID, req.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if public.Status != model.ImageGenerationStatusCompleted || len(public.Results) != 1 || public.Results[0].FileID == "" || public.Results[0].ImageURL == "" || public.Results[0].Width != 4 || public.Results[0].Height != 4 {
		t.Fatalf("delivered edit = %#v", public)
	}
}

func TestDesignerQuoteAndAdmissionRejectForeignMask(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	route := f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"]
	route.GenerationFeatures.SupportsReference = true
	route.GenerationFeatures.SupportsMask = true
	route.GenerationFeatures.MaxReferenceImages = 1
	f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
	input := taskImagePNG(4, 4)
	refID, err := f.service.UploadReference(context.Background(), f.userID, "reference.png", "image/png", input)
	if err != nil {
		t.Fatal(err)
	}
	otherID := uuid.NewString()
	maskID, err := f.service.UploadReference(context.Background(), otherID, "mask.png", "image/png", input)
	if err != nil {
		t.Fatal(err)
	}
	req := f.request(t)
	req.ReferenceFileIDs = []string{refID}
	req.MaskFileID = maskID
	req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, req)
	if _, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req); !errors.Is(err, ErrDesignerReferenceInvalid) {
		t.Fatalf("foreign mask quote error = %v", err)
	}
	if _, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req); !errors.Is(err, ErrDesignerReferenceInvalid) {
		t.Fatalf("foreign mask admission error = %v", err)
	}
	var count int64
	if err := f.db.Model(&model.ImageGeneration{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("generations after rejected mask = %d, %v", count, err)
	}
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

func TestDesignerCreateGenerationQuoteRejectsMissingCount(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := DesignerGenerateRequest{
		OperationID: uuid.NewString(), ProjectID: f.projectID, Prompt: "product poster",
		CapabilityKey: "standard", Size: "1:1", Quality: "auto", OutputFormat: "png",
	}
	if _, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req); err == nil || err.Error() != "n is required" {
		t.Fatalf("error = %v, want n is required", err)
	}
}

func TestDesignerCapabilityRejectsBillingSKURouteMismatch(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	if err := f.db.Model(&model.BillingSKU{}).Where("sk_uid = ?", "image.standard").Update("route", "image_generation.capabilities.professional").Error; err != nil {
		t.Fatal(err)
	}
	req := DesignerGenerateRequest{
		OperationID: uuid.NewString(), ProjectID: f.projectID, Prompt: "product poster",
		CapabilityKey: "standard", Size: "1:1", Quality: "auto", N: 1, OutputFormat: "png",
	}
	req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, req)
	if _, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req); err == nil || !strings.Contains(err.Error(), "want image.generate/image_generation.capabilities.standard") {
		t.Fatalf("route mismatch error = %v", err)
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

func TestDesignerCapabilityTierAuthorizationAppliesToQuoteAndGeneration(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 1000)
	f.service.fullCfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = srvconfig.ImageGenerationRouteConfig{
		Enabled: true, Provider: "volcengine", Model: "image-v1", BaseURL: "https://images.example.com", APIKey: "secret",
		MinTier: "enterprise", BillingSKU: "image.standard",
		GenerationFeatures: ImageGenerationFeatures{MaxBatch: 1, QualityLevels: []string{"auto"}},
	}
	req := DesignerGenerateRequest{OperationID: uuid.NewString(), ProjectID: f.projectID, Prompt: "restricted", CapabilityKey: "standard", Size: "1:1", Quality: "auto", N: 1, OutputFormat: "png"}
	req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, req)
	if _, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req); !errors.Is(err, ErrDesignerCapabilityAccessDenied) {
		t.Fatalf("free quote error = %v, want access denied", err)
	}
	req.QuoteID = "not-a-real-quote"
	req.RequestFingerprint = DesignerGenerationFingerprint(f.userID, req)
	if _, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req); !errors.Is(err, ErrDesignerCapabilityAccessDenied) {
		t.Fatalf("free generation error = %v, want access denied", err)
	}
	if err := f.db.Model(&model.User{}).Where("id = ?", f.userID).Update("tier", model.TierPro).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req); !errors.Is(err, ErrDesignerCapabilityAccessDenied) {
		t.Fatalf("pro quote error = %v, want access denied", err)
	}
	if err := f.db.Model(&model.User{}).Where("id = ?", f.userID).Update("tier", model.TierEnterprise).Error; err != nil {
		t.Fatal(err)
	}
	quote, err := f.service.CreateGenerationQuote(context.Background(), f.userID, req)
	if err != nil || quote == nil {
		t.Fatalf("enterprise quote = %#v err=%v", quote, err)
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
	pending, err := f.service.GetGeneration(context.Background(), f.userID, created.GenerationID)
	if err != nil || pending.BillingStatus != "reversal_pending" || pending.FinalCost != 500 {
		t.Fatalf("pending billing = %#v err=%v", pending, err)
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
	reversed, err := f.service.GetGeneration(context.Background(), f.userID, created.GenerationID)
	if err != nil || reversed.BillingStatus != "reversed" || reversed.FinalCost != 0 {
		t.Fatalf("reversed billing = %#v err=%v", reversed, err)
	}
}

func TestDesignerExecuteGenerationRejectsActualRatioBeforePersistenceAndReversesCharge(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}
	generatedPath := t.TempDir() + "/generated.png"
	if err := os.WriteFile(generatedPath, taskImagePNG(4, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return &fakeImageProvider{result: &appimage.GenerateResult{
			ProviderRequestID: "provider-ratio-mismatch",
			URL:               generatedPath, Model: "image-v1", ResponseType: "file",
		}}, nil
	}

	f.service.ExecuteGeneration(context.Background(), created.GenerationID)

	var generation model.ImageGeneration
	if err := f.db.First(&generation, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if generation.Status != model.ImageGenerationStatusFailed || !strings.Contains(generation.Error, "image_ratio_mismatch") {
		t.Fatalf("generation = %#v", generation)
	}
	if public := f.service.publicGeneration(&generation); public.ErrorCode != "image_ratio_mismatch" {
		t.Fatalf("public error_code = %q, want image_ratio_mismatch", public.ErrorCode)
	}
	var results int64
	if err := f.db.Model(&model.ImageGenerationResult{}).Where("generation_id = ?", created.GenerationID).Count(&results).Error; err != nil || results != 0 {
		t.Fatalf("persisted results = %d, err=%v; want none", results, err)
	}
	if processed, err := f.wallet.ProcessSettlementOutbox(context.Background(), 10); err != nil || processed != 1 {
		t.Fatalf("ProcessSettlementOutbox = %d, %v", processed, err)
	}
	account, err := f.repo.Billing().FindAccount(context.Background(), f.userID)
	if err != nil || account.PaidCredits != 500 || account.DebtCredits != 0 {
		t.Fatalf("account after reversal = %#v err=%v", account, err)
	}
}

func TestDesignerExecuteGenerationDownloadsRemoteResultOnceForValidationAndPersistence(t *testing.T) {
	f := newDesignerFixedSKUFixture(t, 500)
	req := f.request(t)
	created, err := f.service.CreateGenerationRecord(context.Background(), f.userID, req)
	if err != nil {
		t.Fatal(err)
	}

	imageBytes := taskImagePNG(4, 4)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests > 1 {
			http.Error(w, "expired", http.StatusGone)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	defer server.Close()

	f.service.providerFactory = func(*appconfig.ImageAPI, *zerolog.Logger) (appimage.Provider, error) {
		return &fakeImageProvider{result: &appimage.GenerateResult{
			ProviderRequestID: "provider-single-download",
			URL:               server.URL + "/generated.png",
			Model:             "image-v1",
			ResponseType:      "url",
		}}, nil
	}

	f.service.ExecuteGeneration(context.Background(), created.GenerationID)

	if requests != 1 {
		t.Fatalf("remote image requests = %d, want 1", requests)
	}
	var generation model.ImageGeneration
	if err := f.db.First(&generation, "id = ?", created.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if generation.Status != model.ImageGenerationStatusCompleted {
		t.Fatalf("generation status = %q, error = %q", generation.Status, generation.Error)
	}
	var result model.ImageGenerationResult
	if err := f.db.First(&result, "generation_id = ?", created.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if result.ImagePath != "" || result.FileID == "" || result.Width != 4 || result.Height != 4 {
		t.Fatalf("persisted result = %#v", result)
	}
	public, err := f.service.GetGeneration(context.Background(), f.userID, created.GenerationID)
	if err != nil || public.FinalCost != 500 || len(public.Results) != 1 || public.Results[0].ImageURL == "" {
		t.Fatalf("public generation = %#v err=%v", public, err)
	}
}
