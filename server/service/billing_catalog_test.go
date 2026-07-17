package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBillingCatalogPublishesImmutableSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})

	first, err := svc.Publish(ctx)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	second, err := svc.Publish(ctx)
	if err != nil || second.CatalogID != first.CatalogID || string(second.Snapshot) != string(first.Snapshot) {
		t.Fatalf("idempotent Publish = %+v, %v; first = %+v", second, err, first)
	}

	conflicting := bundle
	conflicting.Products.SKUs = append([]billing.SKUConfig(nil), bundle.Products.SKUs...)
	conflicting.Products.SKUs[0].PriceCredits++
	_, err = NewBillingCatalogService(repo, &conflicting, BillingCatalogOptions{Now: func() time.Time { return now }}).Publish(ctx)
	if !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("conflicting Publish error = %v, want ErrBillingConflict", err)
	}

	persisted, err := repo.Billing().FindSKU(ctx, bundle.Products.CatalogID, bundle.Products.SKUs[0].ID)
	if err != nil || persisted.PriceCredits != bundle.Products.SKUs[0].PriceCredits {
		t.Fatalf("immutable persisted SKU = %+v, %v", persisted, err)
	}
}

func TestBillingCatalogParsesProductionAndResolvesExactRoute(t *testing.T) {
	bundle, err := billing.LoadBundle(filepath.Join("..", "billing"))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	repo := newBillingServiceRepository(t)
	svc := NewBillingCatalogService(repo, bundle, BillingCatalogOptions{})
	if _, err := svc.Publish(context.Background()); err != nil {
		t.Fatalf("Publish production catalog: %v", err)
	}

	taskSKU, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "task.article", "")
	if err != nil || taskSKU.SKUID != "task.article.standard.v1" {
		t.Fatalf("ResolveSKU task empty route = %+v, %v", taskSKU, err)
	}
	cover, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "mcp.generate_image", "image_generation.cover")
	if err != nil || cover.SKUID != "image.seedream.cover.v1" {
		t.Fatalf("ResolveSKU cover = %+v, %v", cover, err)
	}
	if _, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "mcp.generate_image", ""); !errors.Is(err, ErrBillingSKUNotFound) {
		t.Fatalf("empty route fallback error = %v, want ErrBillingSKUNotFound", err)
	}
	if _, err := svc.ResolveSKU(context.Background(), bundle.Products.CatalogID, "missing", ""); !errors.Is(err, ErrBillingSKUNotFound) {
		t.Fatalf("unknown operation error = %v, want ErrBillingSKUNotFound", err)
	}
}

func TestBillingCatalogQuoteReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	bundle := testBillingBundle()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{
		Now:      func() time.Time { return now },
		QuoteTTL: 2 * time.Minute,
	})
	if _, err := svc.Publish(ctx); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	req := QuoteRequest{
		UserID: "u1", Operation: "task.article", Route: "", RequestFingerprint: billingFingerprint("quote-request"),
		IdempotencyScope: "quote", IdempotencyKey: "quote-key-1",
	}
	first, err := svc.CreateQuote(ctx, req)
	if err != nil {
		t.Fatalf("CreateQuote: %v", err)
	}
	second, err := svc.CreateQuote(ctx, req)
	if err != nil || second.ID != first.ID {
		t.Fatalf("exact replay = %+v, %v; want quote %s", second, err, first.ID)
	}
	if first.PriceCredits != bundle.Products.SKUs[0].PriceCredits || first.CatalogID != bundle.Products.CatalogID || len(first.SKUSnapshot) == 0 {
		t.Fatalf("quote did not pin catalog SKU: %+v", first)
	}

	conflict := req
	conflict.RequestFingerprint = billingFingerprint("different-request")
	if _, err := svc.CreateQuote(ctx, conflict); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("conflicting replay error = %v, want ErrBillingConflict", err)
	}
}

func newBillingServiceRepository(t *testing.T) repository.Repository {
	t.Helper()
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared&_busy_timeout=10000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func testBillingBundle() billing.Bundle {
	return billing.Bundle{
		Policy: billing.PolicyCatalog{
			Version:       "2026-07-17",
			TaskAdmission: billing.TaskAdmissionPolicy{RequireZeroDebt: true, RequireFullPrice: true},
			AcceptedTask:  billing.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TopUp:         billing.TopUpPolicy{RepayDebtFirst: true},
			Promotions:    billing.PromotionsPolicy{MayRepayDebt: false},
		},
		Products: billing.ProductCatalog{
			CatalogID: "retail-test-v1", Currency: "credits",
			SKUs: []billing.SKUConfig{
				{ID: "task.article.v1", Operation: "task.article", ChargePolicy: "task_admission", PriceCredits: 500, Delivery: "article"},
				{ID: "image.cover.v1", Operation: "mcp.generate_image", ChargePolicy: "accepted_task_operation", PriceCredits: 100, Route: "image.cover", Delivery: "image"},
				{ID: "image.standalone.v1", Operation: "designer.generate_image", ChargePolicy: "standalone_operation", PriceCredits: 100, Route: "image.designer", Delivery: "image"},
			},
		},
	}
}
