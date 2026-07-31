package billing

import (
	"strings"
	"testing"
	"time"
)

func TestLoadBundleSeparatesEconomicsFromFixedPolicy(t *testing.T) {
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{
		"economics.yaml": "credits_per_cny: 1000\n",
	}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle.Economics.CreditsPerCNY != 1000 {
		t.Fatalf("credits_per_cny = %d, want 1000", bundle.Economics.CreditsPerCNY)
	}
	if !bundle.Policy.TaskAdmission.RequireZeroDebt || !bundle.Policy.TaskAdmission.RequireFullPrice ||
		!bundle.Policy.AcceptedTask.ContinueWhenBalanceNegative || !bundle.Policy.AcceptedTask.OperationChargeMayCreateDebt ||
		!bundle.Policy.TopUp.RepayDebtFirst || bundle.Policy.Promotions.MayRepayDebt ||
		!bundle.Policy.TaskFailureReversal.Enabled {
		t.Fatalf("fixed policy = %#v", bundle.Policy)
	}
}

func TestContentAddressedCostCatalogCanonicalization(t *testing.T) {
	first, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Costs.CatalogID, "provider-cost-sha256-") || len(first.Costs.CatalogID) != len("provider-cost-sha256-")+64 {
		t.Fatalf("cost catalog ID = %q", first.Costs.CatalogID)
	}
	equivalent := strings.Replace(validCostsYAML, "  USD: \"7.20\"\n  CNY: \"1.00\"", "  CNY: \"1.00\"\n  USD: \"7.20\"", 1)
	equivalent = strings.Replace(equivalent, "2026-07-17T00:00:00Z", "2026-07-17T08:00:00+08:00", 1)
	equivalent = strings.Replace(equivalent, `input: "6.00"`, `input: " 6.00 "`, 1)
	second, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": equivalent}))
	if err != nil {
		t.Fatal(err)
	}
	if second.Costs.CatalogID != first.Costs.CatalogID {
		t.Fatalf("semantic equivalent cost IDs differ: %q != %q", second.Costs.CatalogID, first.Costs.CatalogID)
	}

	mutations := []struct {
		name, old, new string
	}{
		{"exchange rate", `USD: "7.20"`, `USD: "7.21"`},
		{"price", `input: "6.00"`, `input: "6.01"`},
		{"model", "provider/model:", "provider/model-2:"},
		{"evidence", "ark-price-sheet", "ark-price-sheet-v2"},
		{"effective time", "2026-07-17T00:00:00Z", "2026-07-17T00:00:01Z"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			changed, err := LoadBundle(writeBundleFixture(t, map[string]string{
				"costs.yaml": strings.Replace(validCostsYAML, mutation.old, mutation.new, 1),
			}))
			if err != nil {
				t.Fatal(err)
			}
			if changed.Costs.CatalogID == first.Costs.CatalogID {
				t.Fatalf("%s did not change cost catalog ID", mutation.name)
			}
		})
	}
}

func TestContentAddressedPromotionCatalog(t *testing.T) {
	first, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Promotions.CatalogID, "promotion-sha256-") || len(first.Promotions.CatalogID) != len("promotion-sha256-")+64 {
		t.Fatalf("promotion catalog ID = %q", first.Promotions.CatalogID)
	}

	changedProgram, err := LoadBundle(writeBundleFixture(t, map[string]string{
		"promotions.yaml": strings.Replace(validPromotionsYAML, "inviter_credits: 1000", "inviter_credits: 1001", 1),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if changedProgram.Promotions.CatalogID == first.Promotions.CatalogID {
		t.Fatal("program change did not change promotion catalog ID")
	}
	changedEconomics, err := LoadBundle(writeBundleFixture(t, map[string]string{"economics.yaml": "credits_per_cny: 2000\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if changedEconomics.Promotions.CatalogID == first.Promotions.CatalogID {
		t.Fatal("economics change did not change promotion catalog ID")
	}
	disabled, err := LoadBundle(writeBundleFixture(t, map[string]string{"promotions.yaml": "programs: []\n"}))
	if err != nil || len(disabled.Promotions.Programs) != 0 {
		t.Fatalf("disabled promotions = %#v, %v", disabled, err)
	}
}

func TestPromotionCatalogRejectsDuplicateTrigger(t *testing.T) {
	second := strings.Replace(validPromotionsYAML, "id: referral-first-topup-v1", "id: another-referral", 1)
	second = strings.TrimPrefix(second, "programs:\n")
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"promotions.yaml": validPromotionsYAML + second}))
	if err == nil || !strings.Contains(err.Error(), "duplicate trigger") {
		t.Fatalf("LoadBundle error = %v, want duplicate trigger", err)
	}
}

func TestContentAddressedCostCatalogPreservesTierOrderAndValues(t *testing.T) {
	base := CostCatalog{
		CurrencyRates: map[string]MicroCNY{"CNY": 1_000_000},
		Models: map[string]ModelCostConfig{"provider/image": {
			PricingType: "output_pixel_tier", Currency: "CNY", OperatorEvidence: "evidence",
			EffectiveAt: mustParseCatalogTime(t, "2026-07-17T00:00:00Z"),
			Tiers:       []CostTier{{MaxPixels: 2_360_000, Price: 300_000}, {Price: 600_000}},
		}},
	}
	baseID, err := costCatalogID(base)
	if err != nil {
		t.Fatal(err)
	}
	changedPrice := base
	changedPrice.Models = map[string]ModelCostConfig{"provider/image": base.Models["provider/image"]}
	model := changedPrice.Models["provider/image"]
	model.Tiers = append([]CostTier(nil), model.Tiers...)
	model.Tiers[0].Price++
	changedPrice.Models["provider/image"] = model
	priceID, err := costCatalogID(changedPrice)
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.Models = map[string]ModelCostConfig{"provider/image": base.Models["provider/image"]}
	model = reordered.Models["provider/image"]
	model.Tiers = []CostTier{model.Tiers[1], model.Tiers[0]}
	reordered.Models["provider/image"] = model
	orderID, err := costCatalogID(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if baseID == priceID || baseID == orderID {
		t.Fatalf("tier-sensitive IDs base=%q price=%q order=%q", baseID, priceID, orderID)
	}
}

func mustParseCatalogTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
