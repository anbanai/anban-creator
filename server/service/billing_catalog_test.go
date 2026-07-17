package service

import (
	"context"
	"encoding/json"
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

func TestBillingCatalogQuoteReplaySurvivesLatestCatalogRollover(t *testing.T) {
	ctx := context.Background()
	repo := newBillingServiceRepository(t)
	firstBundle := testBillingBundle()
	firstNow := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	firstService := NewBillingCatalogService(repo, &firstBundle, BillingCatalogOptions{Now: func() time.Time { return firstNow }})
	if _, err := firstService.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	req := QuoteRequest{
		UserID: "u1", Operation: "task.article", RequestFingerprint: billingFingerprint("rollover"),
		IdempotencyScope: "quote", IdempotencyKey: "rollover",
	}
	first, err := firstService.CreateQuote(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	secondBundle := testBillingBundle()
	secondBundle.Products.CatalogID = "retail-test-v2"
	secondBundle.Products.SKUs[0].PriceCredits++
	secondNow := firstNow.Add(time.Hour)
	secondService := NewBillingCatalogService(repo, &secondBundle, BillingCatalogOptions{Now: func() time.Time { return secondNow }})
	if _, err := secondService.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	replay, err := secondService.CreateQuote(ctx, req)
	if err != nil || replay.ID != first.ID || replay.CatalogID != firstBundle.Products.CatalogID {
		t.Fatalf("rollover replay = %+v, %v; want original %+v", replay, err, first)
	}

	conflict := req
	conflict.Route = "different-route"
	if _, err := secondService.CreateQuote(ctx, conflict); !errors.Is(err, ErrBillingConflict) {
		t.Fatalf("rollover parameter drift error = %v, want conflict", err)
	}
}

func TestBillingCatalogPublishUsesSemanticJSONAndCompleteSKUEvidence(t *testing.T) {
	t.Run("semantic JSON normalization", func(t *testing.T) {
		repo, db := newBillingServiceRepositoryWithDB(t)
		bundle := testBillingBundle()
		now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
		svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
		if _, err := svc.Publish(context.Background()); err != nil {
			t.Fatal(err)
		}
		catalog, err := repo.Billing().FindCatalogVersion(context.Background(), bundle.Products.CatalogID)
		if err != nil {
			t.Fatal(err)
		}
		var catalogJSON any
		if err := json.Unmarshal(catalog.Snapshot, &catalogJSON); err != nil {
			t.Fatal(err)
		}
		indentedCatalog, err := json.MarshalIndent(catalogJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.BillingCatalogVersion{}).Where("catalog_id = ?", catalog.CatalogID).Update("snapshot", indentedCatalog).Error; err != nil {
			t.Fatal(err)
		}
		skus, err := repo.Billing().ListSKUsByCatalog(context.Background(), catalog.CatalogID)
		if err != nil {
			t.Fatal(err)
		}
		for _, sku := range skus {
			var skuJSON any
			if err := json.Unmarshal(sku.Snapshot, &skuJSON); err != nil {
				t.Fatal(err)
			}
			indentedSKU, err := json.MarshalIndent(skuJSON, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.BillingSKU{}).Where("id = ?", sku.ID).Update("snapshot", indentedSKU).Error; err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.Publish(context.Background()); err != nil {
			t.Fatalf("semantic replay: %v", err)
		}
	})

	for _, tt := range []struct {
		name   string
		mutate func(*testing.T, repository.Repository, *gorm.DB, billing.Bundle)
	}{
		{
			name: "missing SKU",
			mutate: func(t *testing.T, repo repository.Repository, db *gorm.DB, bundle billing.Bundle) {
				skus, err := repo.Billing().ListSKUsByCatalog(context.Background(), bundle.Products.CatalogID)
				if err != nil || len(skus) == 0 {
					t.Fatalf("list SKUs = %+v, %v", skus, err)
				}
				if err := db.Delete(&model.BillingSKU{}, "id = ?", skus[0].ID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "extra SKU",
			mutate: func(t *testing.T, _ repository.Repository, db *gorm.DB, bundle billing.Bundle) {
				extra := model.BillingSKU{
					ID: uuid.NewString(), CatalogID: bundle.Products.CatalogID, SKUID: "extra.v1", Operation: "extra",
					PriceCredits: 1, Policy: "task_admission", Delivery: "task", Snapshot: []byte(`{"ID":"extra.v1"}`),
				}
				if err := db.Create(&extra).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "drifted SKU",
			mutate: func(t *testing.T, repo repository.Repository, db *gorm.DB, bundle billing.Bundle) {
				skus, err := repo.Billing().ListSKUsByCatalog(context.Background(), bundle.Products.CatalogID)
				if err != nil || len(skus) == 0 {
					t.Fatalf("list SKUs = %+v, %v", skus, err)
				}
				if err := db.Model(&model.BillingSKU{}).Where("id = ?", skus[0].ID).Update("price_credits", skus[0].PriceCredits+1).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, db := newBillingServiceRepositoryWithDB(t)
			bundle := testBillingBundle()
			now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
			svc := NewBillingCatalogService(repo, &bundle, BillingCatalogOptions{Now: func() time.Time { return now }})
			if _, err := svc.Publish(context.Background()); err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, repo, db, bundle)
			if _, err := svc.Publish(context.Background()); !errors.Is(err, ErrBillingConflict) {
				t.Fatalf("evidence drift error = %v, want conflict", err)
			}
		})
	}
}

func TestSameJSONSemanticPreservesIntegerPrecision(t *testing.T) {
	left := []byte(`{"price_credits":9007199254740992}`)
	right := []byte(`{"price_credits":9007199254740993}`)
	if sameJSONSemantic(left, right) {
		t.Fatal("distinct int64 JSON values compared equal")
	}
}

func newBillingServiceRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo, _ := newBillingServiceRepositoryWithDB(t)
	return repo
}

func newBillingServiceRepositoryWithDB(t *testing.T) (repository.Repository, *gorm.DB) {
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
	return repo, db
}

func testBillingBundle() billing.Bundle {
	return billing.Bundle{
		Policy: billing.PolicyCatalog{
			Version:       "2026-07-17",
			TaskAdmission: billing.TaskAdmissionPolicy{RequireZeroDebt: true, RequireFullPrice: true},
			AcceptedTask:  billing.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TopUp:         billing.TopUpPolicy{RepayDebtFirst: true},
			Promotions:    billing.PromotionsPolicy{MayRepayDebt: false},
			TaskFailureReversal: billing.TaskFailureReversalPolicy{
				Enabled: true,
				Reasons: []string{"platform_error", "provider_error", "execution_timeout", "infrastructure_cancelled"},
			},
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
