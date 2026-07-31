package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func TestMigrateBillingTierPricesBackfillsLegacyFlatCatalogAndIsIdempotent(t *testing.T) {
	db := newBillingTierMigrationDB(t)
	sku := seedBillingTierMigrationSKU(t, db, "retail-legacy", "task.article.legacy", 101)
	var logs bytes.Buffer
	logger := zerolog.New(&logs)

	for run := 0; run < 2; run++ {
		if err := MigrateBillingTierPrices(context.Background(), db, &logger); err != nil {
			t.Fatalf("run %d: %v", run+1, err)
		}
	}

	var prices []model.BillingSKUTierPrice
	if err := db.Where("catalog_id = ? AND sku_id = ?", sku.CatalogID, sku.SKUID).Order("tier ASC").Find(&prices).Error; err != nil {
		t.Fatal(err)
	}
	if len(prices) != 3 {
		t.Fatalf("tier price count = %d, want 3", len(prices))
	}
	for _, price := range prices {
		if price.PriceCredits != 101 || price.RuleID != sku.CatalogID+":"+string(price.Tier) {
			t.Fatalf("backfilled price = %#v", price)
		}
		var snapshot struct {
			RatePercent int64  `json:"rate_percent"`
			Rounding    string `json:"rounding"`
			Source      string `json:"source"`
		}
		if err := json.Unmarshal(price.Snapshot, &snapshot); err != nil || snapshot.RatePercent != 100 || snapshot.Rounding != "floor" || snapshot.Source != "legacy_flat" {
			t.Fatalf("backfill snapshot = %s, %#v, %v", price.Snapshot, snapshot, err)
		}
	}
	if output := logs.String(); !strings.Contains(output, `"scanned":1`) || !strings.Contains(output, `"backfilled":1`) || !strings.Contains(output, `"complete":0`) || !strings.Contains(output, `"corrupt":0`) {
		t.Fatalf("migration log = %s", output)
	}
}

func TestMigrateBillingTierPricesPreservesCompleteCatalog(t *testing.T) {
	db := newBillingTierMigrationDB(t)
	sku := seedBillingTierMigrationSKU(t, db, "retail-tiered", "task.article.balanced", 101)
	want := map[model.Tier]int64{model.TierFree: 101, model.TierPro: 90, model.TierEnterprise: 80}
	for tier, price := range want {
		row := model.BillingSKUTierPrice{
			ID: uuid.NewString(), CatalogID: sku.CatalogID, SKUID: sku.SKUID, Tier: tier,
			PriceCredits: price, RuleID: sku.CatalogID + ":" + string(tier), Snapshot: []byte(`{"existing":true}`), CreatedAt: time.Now().UTC(),
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	logger := zerolog.Nop()
	if err := MigrateBillingTierPrices(context.Background(), db, &logger); err != nil {
		t.Fatal(err)
	}
	var prices []model.BillingSKUTierPrice
	if err := db.Where("catalog_id = ? AND sku_id = ?", sku.CatalogID, sku.SKUID).Find(&prices).Error; err != nil {
		t.Fatal(err)
	}
	if len(prices) != 3 {
		t.Fatalf("tier price count = %d, want 3", len(prices))
	}
	for _, price := range prices {
		if price.PriceCredits != want[price.Tier] || string(price.Snapshot) != `{"existing":true}` {
			t.Fatalf("complete price was changed: %#v", price)
		}
	}
}

func TestMigrateBillingTierPricesRejectsPartialCatalog(t *testing.T) {
	db := newBillingTierMigrationDB(t)
	sku := seedBillingTierMigrationSKU(t, db, "retail-corrupt", "task.article.partial", 100)
	price := model.BillingSKUTierPrice{
		ID: uuid.NewString(), CatalogID: sku.CatalogID, SKUID: sku.SKUID, Tier: model.TierFree,
		PriceCredits: 100, RuleID: "existing", Snapshot: []byte(`{}`), CreatedAt: time.Now().UTC(),
	}
	if err := db.Create(&price).Error; err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	err := MigrateBillingTierPrices(context.Background(), db, &logger)
	if err == nil || !strings.Contains(err.Error(), sku.CatalogID) || !strings.Contains(err.Error(), sku.SKUID) || !strings.Contains(err.Error(), "partial tier prices") {
		t.Fatalf("migration error = %v", err)
	}
	var count int64
	if err := db.Model(&model.BillingSKUTierPrice{}).Where("catalog_id = ? AND sku_id = ?", sku.CatalogID, sku.SKUID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("partial rows changed: count=%d err=%v", count, err)
	}
}

func TestMigrateBillingTierPricesEmptyDatabaseIsNoOp(t *testing.T) {
	db := newBillingTierMigrationDB(t)
	logger := zerolog.Nop()
	if err := MigrateBillingTierPrices(context.Background(), db, &logger); err != nil {
		t.Fatal(err)
	}
}

func newBillingTierMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newMigrateTestDB(t)
	if err := db.AutoMigrate(&model.BillingCatalogVersion{}, &model.BillingSKU{}, &model.BillingSKUTierPrice{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedBillingTierMigrationSKU(t *testing.T, db *gorm.DB, catalogID, skuID string, price int64) model.BillingSKU {
	t.Helper()
	now := time.Now().UTC()
	catalog := model.BillingCatalogVersion{CatalogID: catalogID, Currency: "credits", Status: "published", PublishedAt: now, Snapshot: []byte(`{}`), CreatedAt: now}
	if err := db.Create(&catalog).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.BillingSKU{
		ID: uuid.NewString(), CatalogID: catalogID, SKUID: skuID, Operation: "task.article", ExecutionProfile: "balanced",
		PriceCredits: price, Policy: "task_admission", Delivery: "verified", Snapshot: []byte(`{}`), CreatedAt: now,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	return sku
}
