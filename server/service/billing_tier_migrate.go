package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type billingTierMigrationStats struct {
	Scanned    int
	Complete   int
	Backfilled int
	Corrupt    int
}

func MigrateBillingTierPrices(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	var skus []model.BillingSKU
	if err := db.WithContext(ctx).Order("catalog_id ASC, sk_uid ASC").Find(&skus).Error; err != nil {
		return fmt.Errorf("list billing SKUs for tier migration: %w", err)
	}
	stats := billingTierMigrationStats{Scanned: len(skus)}
	for _, sku := range skus {
		var prices []model.BillingSKUTierPrice
		if err := db.WithContext(ctx).
			Where("catalog_id = ? AND sku_id = ?", sku.CatalogID, sku.SKUID).
			Order("tier ASC").
			Find(&prices).Error; err != nil {
			return fmt.Errorf("list tier prices for catalog %q SKU %q: %w", sku.CatalogID, sku.SKUID, err)
		}
		switch {
		case len(prices) == 0:
			if err := backfillLegacyFlatTierPrices(ctx, db, sku); err != nil {
				return err
			}
			stats.Backfilled++
		case completeTierPriceSet(prices):
			stats.Complete++
		default:
			stats.Corrupt++
			logBillingTierMigration(log, stats)
			return fmt.Errorf("billing catalog %q SKU %q has partial tier prices", sku.CatalogID, sku.SKUID)
		}
	}
	logBillingTierMigration(log, stats)
	return nil
}

func backfillLegacyFlatTierPrices(ctx context.Context, db *gorm.DB, sku model.BillingSKU) error {
	now := time.Now().UTC()
	prices := make([]model.BillingSKUTierPrice, 0, len(billing.RequiredPricingTiers))
	for _, tierName := range billing.RequiredPricingTiers {
		ruleID := sku.CatalogID + ":" + tierName
		snapshot, err := json.Marshal(struct {
			CatalogID     string `json:"catalog_id"`
			SKUID         string `json:"sku_id"`
			Tier          string `json:"tier"`
			ListPrice     int64  `json:"list_price_credits"`
			RatePercent   int64  `json:"rate_percent"`
			Rounding      string `json:"rounding"`
			PriceCredits  int64  `json:"price_credits"`
			PricingRuleID string `json:"pricing_rule_id"`
			Source        string `json:"source"`
		}{
			CatalogID: sku.CatalogID, SKUID: sku.SKUID, Tier: tierName, ListPrice: sku.PriceCredits,
			RatePercent: 100, Rounding: "floor", PriceCredits: sku.PriceCredits, PricingRuleID: ruleID, Source: "legacy_flat",
		})
		if err != nil {
			return fmt.Errorf("encode legacy flat tier price for catalog %q SKU %q: %w", sku.CatalogID, sku.SKUID, err)
		}
		prices = append(prices, model.BillingSKUTierPrice{
			ID: uuid.NewString(), CatalogID: sku.CatalogID, SKUID: sku.SKUID, Tier: model.Tier(tierName),
			PriceCredits: sku.PriceCredits, RuleID: ruleID, Snapshot: snapshot, CreatedAt: now,
		})
	}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&prices).Error; err != nil {
		return fmt.Errorf("backfill tier prices for catalog %q SKU %q: %w", sku.CatalogID, sku.SKUID, err)
	}
	var persisted []model.BillingSKUTierPrice
	if err := db.WithContext(ctx).Where("catalog_id = ? AND sku_id = ?", sku.CatalogID, sku.SKUID).Find(&persisted).Error; err != nil {
		return fmt.Errorf("verify tier prices for catalog %q SKU %q: %w", sku.CatalogID, sku.SKUID, err)
	}
	if !completeTierPriceSet(persisted) {
		return fmt.Errorf("billing catalog %q SKU %q has partial tier prices after backfill", sku.CatalogID, sku.SKUID)
	}
	return nil
}

func completeTierPriceSet(prices []model.BillingSKUTierPrice) bool {
	if len(prices) != len(billing.RequiredPricingTiers) {
		return false
	}
	seen := make(map[model.Tier]struct{}, len(prices))
	for _, price := range prices {
		if !model.ValidTiers[price.Tier] {
			return false
		}
		seen[price.Tier] = struct{}{}
	}
	return len(seen) == len(billing.RequiredPricingTiers)
}

func logBillingTierMigration(log *zerolog.Logger, stats billingTierMigrationStats) {
	if log == nil {
		return
	}
	log.Info().
		Int("scanned", stats.Scanned).
		Int("complete", stats.Complete).
		Int("backfilled", stats.Backfilled).
		Int("corrupt", stats.Corrupt).
		Msg("billing tier price migration completed")
}
