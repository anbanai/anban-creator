package billing

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadBundleLoadsTypedCatalogs(t *testing.T) {
	bundle, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle.Economics.CreditsPerCNY != 1000 {
		t.Fatalf("credits_per_cny = %d, want 1000", bundle.Economics.CreditsPerCNY)
	}
	if !bundle.Policy.AcceptedTask.ContinueWhenBalanceNegative || !bundle.Policy.AcceptedTask.OperationChargeMayCreateDebt {
		t.Fatalf("accepted_task policy = %#v", bundle.Policy.AcceptedTask)
	}
	if got := bundle.Products.SKUs[0].PriceCredits; got != 5000 {
		t.Fatalf("price_credits = %d, want 5000", got)
	}
	if got := bundle.Costs.CurrencyRates["USD"]; got != 7_200_000 {
		t.Fatalf("USD rate = %d, want 7200000", got)
	}
	if got := bundle.Costs.Models["provider/model"].Input; got != 6_000_000 {
		t.Fatalf("model input = %d, want 6000000", got)
	}
	program := bundle.Promotions.Programs[0]
	if program.MinimumTopUpCNY != 10_000_000 || program.ExpiresAfter != 30*24*time.Hour {
		t.Fatalf("program exact values = %#v", program)
	}
}

func TestLoadBundleRejectsStrictYAMLErrors(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{name: "unknown field", overrides: map[string]string{"economics.yaml": validEconomicsYAML + "unknown: true\n"}, want: "field unknown not found"},
		{name: "trailing document", overrides: map[string]string{"economics.yaml": validEconomicsYAML + "---\ncredits_per_cny: 2000\n"}, want: "trailing YAML document"},
		{name: "duplicate SKU", overrides: map[string]string{"products.yaml": strings.Replace(validProductsYAML, "skus:\n", "skus:\n  - id: task.seednote.balanced\n    operation: task.seednote\n    execution_profile: balanced\n    charge_policy: task_admission\n    price_credits: 5000\n    delivery: verified\n", 1)}, want: "duplicate SKU id"},
		{name: "duplicate program", overrides: map[string]string{"promotions.yaml": validPromotionsYAML + "  - id: referral-first-topup-v1\n    trigger: invitee_first_paid_topup\n    minimum_topup_cny: \"10.00\"\n    inviter_credits: 1\n    invitee_credits: 1\n    expires_after: 24h\n    max_inviter_rewards: 1\n"}, want: "duplicate referral program id"},
		{name: "malformed currency rate", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `"7.20"`, `"7.2.0"`, 1)}, want: "currency_rates.USD"},
		{name: "currency rate must be string", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `"7.20"`, `7.20`, 1)}, want: "must be a quoted decimal string"},
		{name: "malformed model price", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `input: "6.00"`, `input: "1e3"`, 1)}, want: "models.provider/model.input"},
		{name: "malformed promotion price", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, `"10.00"`, `"-10.00"`, 1)}, want: "minimum_topup_cny"},
		{name: "zero promotion minimum", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, `"10.00"`, `"0.00"`, 1)}, want: "minimum_topup_cny: must be positive"},
		{name: "zero inviter credits", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, "inviter_credits: 1000", "inviter_credits: 0", 1)}, want: "inviter_credits: must be positive"},
		{name: "zero invitee credits", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, "invitee_credits: 1000", "invitee_credits: 0", 1)}, want: "invitee_credits: must be positive"},
		{name: "missing currency reference", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `currency: "CNY"`, `currency: "EUR"`, 1)}, want: "references missing currency rate"},
		{name: "CNY rate must be unit", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `CNY: "1.00"`, `CNY: "1.01"`, 1)}, want: "currency_rates.CNY: must equal 1.00"},
		{name: "invalid charge policy", overrides: map[string]string{"products.yaml": strings.Replace(validProductsYAML, "task_admission", "runtime_usage", 1)}, want: "charge_policy"},
		{name: "legacy cost catalog ID", overrides: map[string]string{"costs.yaml": "catalog_id: old\n" + validCostsYAML}, want: "field catalog_id not found"},
		{name: "legacy promotion catalog ID", overrides: map[string]string{"promotions.yaml": "catalog_id: old\n" + validPromotionsYAML}, want: "field catalog_id not found"},
		{name: "legacy promotion debt field", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, "    max_inviter_rewards: 10\n", "    max_inviter_rewards: 10\n    can_repay_debt: false\n", 1)}, want: "field can_repay_debt not found"},
		{name: "legacy policy file", overrides: map[string]string{"policy.yaml": "credits_per_cny: 1000\n"}, want: "is no longer supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadBundle(writeBundleFixture(t, tt.overrides))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
			var configErr *ConfigError
			if !errors.As(err, &configErr) || configErr.File == "" {
				t.Fatalf("LoadBundle error = %#v, want structured ConfigError with file", err)
			}
		})
	}
}

func TestLoadBundleRequiresCNYCurrencyRate(t *testing.T) {
	costs := strings.Replace(validCostsYAML, "  CNY: \"1.00\"\n", "", 1)
	costs = strings.Replace(costs, `currency: "CNY"`, `currency: "USD"`, 1)
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "currency_rates.CNY: is required") {
		t.Fatalf("LoadBundle error = %v, want mandatory CNY rate rejection", err)
	}
}

func TestLoadBundleRejectsAmbiguousOrUnroutableSKUs(t *testing.T) {
	tests := []struct {
		name     string
		products string
		want     string
	}{
		{
			name: "duplicate billable identity normalizes blank route",
			products: validProductsYAML + `  - id: task.seednote.alternate
    operation: task.seednote
    execution_profile: balanced
    charge_policy: task_admission
    price_credits: 6000
    route: "   "
    delivery: alternate
`,
			want: "duplicates billable identity",
		},
		{
			name: "accepted task operation requires route",
			products: strings.Replace(validProductsYAML, `operation: task.seednote
    execution_profile: balanced
    charge_policy: task_admission`, `operation: mcp.generate_image
    charge_policy: accepted_task_operation`, 1),
			want: "route is required for accepted_task_operation",
		},
		{
			name: "standalone operation requires route",
			products: strings.Replace(validProductsYAML, `operation: task.seednote
    execution_profile: balanced
    charge_policy: task_admission`, `operation: image.generate
    charge_policy: standalone_operation`, 1),
			want: "route is required for standalone_operation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": tt.products}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadBundleNormalizesBlankTaskAdmissionRoute(t *testing.T) {
	products := strings.Replace(validProductsYAML, "    delivery: verified\n", "    route: \"   \"\n    delivery: verified\n", 1)
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if got := bundle.Products.SKUs[0].Route; got != "" {
		t.Fatalf("task admission route = %q, want canonical empty route", got)
	}
}

func TestLoadBundleCanonicalizesSKUFields(t *testing.T) {
	products := `currency: " credits "
tier_rates_percent: { free: 100, pro: 90, enterprise: 80 }
task_time_pricing:
  timezone: Asia/Shanghai
  peak_windows:
    - { start: "09:00", end: "12:00" }
    - { start: "14:00", end: "18:00" }
  off_peak_rate_percent: 80
skus:
  - id: " image.standard "
    operation: " mcp.generate_image "
    charge_policy: " accepted_task_operation "
    price_credits: 500
    route: " image_generation.content "
    delivery: " persisted_image "
`
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if !strings.HasPrefix(bundle.Products.CatalogID, "retail-sha256-") || bundle.Products.Currency != "credits" {
		t.Fatalf("product identity = catalog %q currency %q", bundle.Products.CatalogID, bundle.Products.Currency)
	}
	want := SKUConfig{
		ID: "image.standard", Operation: "mcp.generate_image", ChargePolicy: "accepted_task_operation",
		PriceCredits: 500, Route: "image_generation.content", Delivery: "persisted_image",
	}
	if got := bundle.Products.SKUs[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("SKU = %#v, want %#v", got, want)
	}
}

func TestLoadBundleCanonicalizesSinglePaddedOperation(t *testing.T) {
	products := strings.Replace(validProductsYAML, "operation: task.seednote", `operation: " task.seednote "`, 1)
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if got := bundle.Products.SKUs[0].Operation; got != "task.seednote" {
		t.Fatalf("operation = %q, want canonical task.seednote", got)
	}
}

func TestLoadBundleDerivesContentAddressedRetailCatalogID(t *testing.T) {
	first, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatalf("LoadBundle(first): %v", err)
	}
	second, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatalf("LoadBundle(second): %v", err)
	}
	if first.Products.CatalogID != second.Products.CatalogID {
		t.Fatalf("catalog IDs differ for identical content: %q != %q", first.Products.CatalogID, second.Products.CatalogID)
	}
	if !strings.HasPrefix(first.Products.CatalogID, "retail-sha256-") || len(first.Products.CatalogID) != len("retail-sha256-")+64 {
		t.Fatalf("catalog ID = %q, want full SHA-256 identity", first.Products.CatalogID)
	}

	changedPrice := strings.Replace(validProductsYAML, "price_credits: 5000", "price_credits: 5001", 1)
	priceBundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": changedPrice}))
	if err != nil {
		t.Fatalf("LoadBundle(changed price): %v", err)
	}
	if priceBundle.Products.CatalogID == first.Products.CatalogID {
		t.Fatal("price change did not change content-addressed catalog ID")
	}

	changedTimeRate := strings.Replace(validProductsYAML, "off_peak_rate_percent: 80", "off_peak_rate_percent: 79", 1)
	timeBundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": changedTimeRate}))
	if err != nil {
		t.Fatalf("LoadBundle(changed time rate): %v", err)
	}
	if timeBundle.Products.CatalogID == first.Products.CatalogID {
		t.Fatal("task time pricing change did not change content-addressed catalog ID")
	}

	changedCreditsPerCNY := strings.Replace(validEconomicsYAML, "credits_per_cny: 1000", "credits_per_cny: 2000", 1)
	creditsBundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"economics.yaml": changedCreditsPerCNY}))
	if err != nil {
		t.Fatalf("LoadBundle(changed credits per CNY): %v", err)
	}
	if creditsBundle.Products.CatalogID == first.Products.CatalogID {
		t.Fatal("credits_per_cny change did not change content-addressed catalog ID")
	}
}

func TestLoadBundleNormalizesTaskTimePricing(t *testing.T) {
	bundle, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	rule := bundle.Products.TaskTimePricing
	if rule.Timezone != "Asia/Shanghai" || rule.OffPeakRatePercent != 80 {
		t.Fatalf("task time pricing = %#v", rule)
	}
	wantPeak := []TimeWindow{{Start: "09:00", End: "12:00"}, {Start: "14:00", End: "18:00"}}
	wantOffPeak := []TimeWindow{{Start: "00:00", End: "09:00"}, {Start: "12:00", End: "14:00"}, {Start: "18:00", End: "24:00"}}
	if !reflect.DeepEqual(rule.PeakWindows, wantPeak) || !reflect.DeepEqual(rule.OffPeakWindows, wantOffPeak) {
		t.Fatalf("normalized windows peak=%#v off_peak=%#v", rule.PeakWindows, rule.OffPeakWindows)
	}
}

func TestLoadBundleMergesAdjacentPeakWindows(t *testing.T) {
	products := strings.Replace(validProductsYAML, `peak_windows:
  - { start: "09:00", end: "12:00" }
  - { start: "14:00", end: "18:00" }`, `peak_windows:
  - { start: "09:00", end: "12:00" }
  - { start: "12:00", end: "18:00" }`, 1)
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
	if err != nil {
		t.Fatal(err)
	}
	want := []TimeWindow{{Start: "09:00", End: "18:00"}}
	if !reflect.DeepEqual(bundle.Products.TaskTimePricing.PeakWindows, want) {
		t.Fatalf("peak windows = %#v, want %#v", bundle.Products.TaskTimePricing.PeakWindows, want)
	}
}

func TestLoadBundleRejectsInvalidTaskTimePricing(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "timezone missing", old: "Asia/Shanghai", new: "Mars/Olympus", want: "timezone"},
		{name: "timezone local is not IANA", old: "Asia/Shanghai", new: "Local", want: "timezone"},
		{name: "time must be exact", old: `start: "09:00"`, new: `start: "9:00"`, want: "HH:mm"},
		{name: "start cannot be day end", old: `start: "09:00"`, new: `start: "24:00"`, want: "valid HH:mm"},
		{name: "start before end", old: `start: "09:00", end: "12:00"`, new: `start: "12:00", end: "12:00"`, want: "start must be before end"},
		{name: "overlap", old: `start: "14:00", end: "18:00"`, new: `start: "11:00", end: "18:00"`, want: "must not overlap"},
		{name: "unordered", old: `peak_windows:
  - { start: "09:00", end: "12:00" }
  - { start: "14:00", end: "18:00" }`, new: `peak_windows:
  - { start: "14:00", end: "18:00" }
  - { start: "09:00", end: "12:00" }`, want: "ordered"},
		{name: "rate zero", old: "off_peak_rate_percent: 80", new: "off_peak_rate_percent: 0", want: "between 1 and 100"},
		{name: "rate over 100", old: "off_peak_rate_percent: 80", new: "off_peak_rate_percent: 101", want: "between 1 and 100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := strings.Replace(validProductsYAML, tt.old, tt.new, 1)
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}

	t.Run("requires full fifteen minute off peak slot", func(t *testing.T) {
		products := strings.Replace(validProductsYAML, `peak_windows:
  - { start: "09:00", end: "12:00" }
  - { start: "14:00", end: "18:00" }`, `peak_windows:
  - { start: "00:00", end: "12:00" }
  - { start: "12:14", end: "24:00" }`, 1)
		_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
		if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "15-minute off-peak slot") {
			t.Fatalf("LoadBundle error = %v", err)
		}
	})

	t.Run("accepts fifteen minute off peak slot spanning midnight", func(t *testing.T) {
		products := strings.Replace(validProductsYAML, `peak_windows:
  - { start: "09:00", end: "12:00" }
  - { start: "14:00", end: "18:00" }`, `peak_windows:
  - { start: "00:10", end: "23:55" }`, 1)
		if _, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products})); err != nil {
			t.Fatalf("LoadBundle: %v", err)
		}
	})
}

func TestLoadBundleAcceptsTaskTimeRateBoundaries(t *testing.T) {
	for _, rate := range []string{"1", "100"} {
		t.Run(rate, func(t *testing.T) {
			products := strings.Replace(validProductsYAML, "off_peak_rate_percent: 80", "off_peak_rate_percent: "+rate, 1)
			bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
			if err != nil {
				t.Fatalf("LoadBundle: %v", err)
			}
			if got := strconv.FormatInt(bundle.Products.TaskTimePricing.OffPeakRatePercent, 10); got != rate {
				t.Fatalf("off peak rate = %s, want %s", got, rate)
			}
		})
	}
}

func TestProductCatalogPriceForTierFloorsWithoutOverflow(t *testing.T) {
	bundle, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		price int64
		tier  string
		want  int64
	}{
		{name: "free list price", price: 101, tier: "free", want: 101},
		{name: "pro floors fractional credit", price: 101, tier: "pro", want: 90},
		{name: "enterprise floors fractional credit", price: 101, tier: "enterprise", want: 80},
		{name: "zero price", price: 0, tier: "pro", want: 0},
		{name: "maximum int64", price: math.MaxInt64, tier: "free", want: math.MaxInt64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := bundle.Products.PriceForTier(tt.price, tt.tier)
			if !ok || got != tt.want {
				t.Fatalf("PriceForTier(%d, %q) = %d, %v; want %d, true", tt.price, tt.tier, got, ok, tt.want)
			}
		})
	}
	if _, ok := bundle.Products.PriceForTier(100, "unknown"); ok {
		t.Fatal("unknown tier resolved a price")
	}
}

func TestLoadBundleRejectsLegacyRetailPricingFields(t *testing.T) {
	tests := []struct {
		name     string
		products string
		want     string
	}{
		{name: "manual catalog ID", products: "catalog_id: retail-v1\n" + validProductsYAML, want: "field catalog_id not found"},
		{name: "pricing model", products: "pricing_model: tier_matrix_v1\n" + validProductsYAML, want: "field pricing_model not found"},
		{name: "SKU tier prices", products: strings.Replace(validProductsYAML, "    delivery: verified\n", "    tier_prices: { free: 5000, pro: 4500, enterprise: 4000 }\n    delivery: verified\n", 1), want: "field tier_prices not found"},
		{name: "versioned SKU ID", products: strings.Replace(validProductsYAML, "task.seednote.balanced", "task.seednote.balanced.v1", 1), want: "must not end in a version suffix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": tt.products}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadBundleValidatesGlobalTierRates(t *testing.T) {
	tests := []struct {
		name, rates, want string
	}{
		{name: "missing tier", rates: "{ free: 100, pro: 90 }", want: "exactly free, pro, and enterprise"},
		{name: "rate below zero", rates: "{ free: 100, pro: -1, enterprise: 0 }", want: "must be between 0 and 100"},
		{name: "rate above one hundred", rates: "{ free: 100, pro: 101, enterprise: 80 }", want: "must be between 0 and 100"},
		{name: "free is list price", rates: "{ free: 99, pro: 90, enterprise: 80 }", want: "free: must equal 100"},
		{name: "rates are ordered", rates: "{ free: 100, pro: 80, enterprise: 90 }", want: "free >= pro >= enterprise"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := strings.Replace(validProductsYAML, "{ free: 100, pro: 90, enterprise: 80 }", tt.rates, 1)
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadBundleRequiresTaskAdmissionExecutionProfile(t *testing.T) {
	products := strings.Replace(validProductsYAML, "    execution_profile: balanced\n", "", 1)
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "must be effective, balanced, or quality") {
		t.Fatalf("LoadBundle error = %v, want required canonical execution profile", err)
	}
}

func TestLoadBundleRejectsCanonicalDuplicateSKUIDs(t *testing.T) {
	products := `currency: credits
tier_rates_percent: { free: 100, pro: 90, enterprise: 80 }
skus:
  - id: " duplicate "
    operation: task.one
    execution_profile: balanced
    charge_policy: task_admission
    price_credits: 100
    delivery: one
  - id: duplicate
    operation: task.two
    execution_profile: balanced
    charge_policy: task_admission
    price_credits: 100
    delivery: two
`
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "duplicate SKU id") {
		t.Fatalf("LoadBundle error = %v, want canonical duplicate SKU rejection", err)
	}
}

func TestLoadBundleCanonicalizesCurrencyIdentities(t *testing.T) {
	costs := strings.Replace(validCostsYAML, "  CNY: \"1.00\"", `  " CNY ": "1.00"`, 1)
	costs = strings.Replace(costs, `currency: "CNY"`, `currency: " CNY "`, 1)
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if _, ok := bundle.Costs.CurrencyRates["CNY"]; !ok || bundle.Costs.Models["provider/model"].Currency != "CNY" {
		t.Fatalf("currency identities not canonical: rates=%#v model=%#v", bundle.Costs.CurrencyRates, bundle.Costs.Models["provider/model"])
	}
}

func TestLoadBundleRejectsCanonicalDuplicateCurrencies(t *testing.T) {
	costs := strings.Replace(validCostsYAML, "  CNY: \"1.00\"", "  CNY: \"1.00\"\n  \" CNY \": \"1.00\"", 1)
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "duplicate canonical currency") {
		t.Fatalf("LoadBundle error = %v, want canonical duplicate currency rejection", err)
	}
}

func TestLoadBundleCanonicalizesCostModelIDs(t *testing.T) {
	costs := strings.Replace(validCostsYAML, "  provider/model:", `  " provider/model ":`, 1)
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if _, ok := bundle.Costs.Models["provider/model"]; !ok {
		t.Fatalf("model keys = %#v, want canonical provider/model", bundle.Costs.Models)
	}
	if _, exists := bundle.Costs.Models[" provider/model "]; exists {
		t.Fatalf("model keys retained padded identity: %#v", bundle.Costs.Models)
	}
}

func TestLoadBundleRejectsCanonicalDuplicateCostModelIDs(t *testing.T) {
	costs := validCostsYAML + `  " provider/model ":
    pricing_type: token
    currency: "CNY"
    unit: 1000000
    input: "6.00"
    cache_read_input: "1.20"
    cache_creation_input: "6.00"
    output: "30.00"
    operator_evidence: ark-price-sheet
    effective_at: "2026-07-17T00:00:00Z"
`
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "duplicate model id") {
		t.Fatalf("LoadBundle error = %v, want canonical duplicate model rejection", err)
	}
}

func TestLoadBundleCanonicalizesReferralProgramIDs(t *testing.T) {
	promotions := strings.Replace(validPromotionsYAML, "id: referral-first-topup-v1", `id: " referral-first-topup-v1 "`, 1)
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"promotions.yaml": promotions}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if got := bundle.Promotions.Programs[0].ID; got != ReferralFirstTopUpProgramID {
		t.Fatalf("program id = %q, want canonical %s", got, ReferralFirstTopUpProgramID)
	}
}

func TestLoadBundleRejectsCanonicalDuplicateReferralProgramIDs(t *testing.T) {
	promotions := validPromotionsYAML + `  - id: " referral-first-topup-v1 "
    trigger: invitee_first_paid_topup
    minimum_topup_cny: "10.00"
    inviter_credits: 1000
    invitee_credits: 1000
    expires_after: 30d
    max_inviter_rewards: 10
`
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"promotions.yaml": promotions}))
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "duplicate referral program id") {
		t.Fatalf("LoadBundle error = %v, want canonical duplicate referral rejection", err)
	}
}

func TestLoadBundleCanonicalizesRemainingEnumIdentifiers(t *testing.T) {
	t.Run("pricing type", func(t *testing.T) {
		costs := strings.Replace(validCostsYAML, "pricing_type: token", `pricing_type: " token "`, 1)
		bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
		if err != nil {
			t.Fatalf("LoadBundle: %v", err)
		}
		if got := bundle.Costs.Models["provider/model"].PricingType; got != "token" {
			t.Fatalf("pricing type = %q, want canonical token", got)
		}
	})

	t.Run("referral trigger", func(t *testing.T) {
		promotions := strings.Replace(validPromotionsYAML, "trigger: invitee_first_paid_topup", `trigger: " invitee_first_paid_topup "`, 1)
		bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"promotions.yaml": promotions}))
		if err != nil {
			t.Fatalf("LoadBundle: %v", err)
		}
		if got := bundle.Promotions.Programs[0].Trigger; got != "invitee_first_paid_topup" {
			t.Fatalf("trigger = %q, want canonical invitee_first_paid_topup", got)
		}
	})
}

func TestLoadBundleValidatesCostMetadataAndTokenProfile(t *testing.T) {
	tests := []struct {
		name  string
		costs string
		want  string
	}{
		{name: "operator evidence required", costs: strings.Replace(validCostsYAML, "    operator_evidence: ark-price-sheet\n", "", 1), want: "operator_evidence is required"},
		{name: "effective time required", costs: strings.Replace(validCostsYAML, "    effective_at: \"2026-07-17T00:00:00Z\"\n", "", 1), want: "effective_at must be RFC3339"},
		{name: "effective time RFC3339", costs: strings.Replace(validCostsYAML, "2026-07-17T00:00:00Z", "2026-07-17", 1), want: "effective_at must be RFC3339"},
		{name: "cache read price required", costs: strings.Replace(validCostsYAML, "    cache_read_input: \"1.20\"\n", "", 1), want: "token pricing requires"},
		{name: "cache creation price required", costs: strings.Replace(validCostsYAML, "    cache_creation_input: \"6.00\"\n", "", 1), want: "token pricing requires"},
		{name: "token prices positive", costs: strings.Replace(validCostsYAML, `input: "6.00"`, `input: "0.00"`, 1), want: "token prices must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": tt.costs}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadBundleValidatesOutputPixelTiers(t *testing.T) {
	validPixelCosts := `currency_rates:
  CNY: "1.00"
models:
  provider/image:
    pricing_type: output_pixel_tier
    currency: CNY
    tiers:
      - max_pixels: 2360000
        price: "0.30"
      - price: "0.60"
    operator_evidence: ark-price-sheet
    effective_at: "2026-07-17T00:00:00Z"
`
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": validPixelCosts}))
	if err != nil {
		t.Fatalf("LoadBundle valid output pixel tiers: %v", err)
	}
	if got := bundle.Costs.Models["provider/image"].Tiers; len(got) != 2 || got[0].MaxPixels != 2360000 || got[1].MaxPixels != 0 {
		t.Fatalf("pixel tiers = %#v", got)
	}

	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "tiers required", old: "    tiers:\n      - max_pixels: 2360000\n        price: \"0.30\"\n      - price: \"0.60\"\n", new: "    tiers: []\n", want: "requires tiers"},
		{name: "bounded tier positive", old: "max_pixels: 2360000", new: "max_pixels: -1", want: "bounded max_pixels must be positive"},
		{name: "bounds ascending", old: "      - price: \"0.60\"", new: "      - max_pixels: 100\n        price: \"0.40\"\n      - price: \"0.60\"", want: "max_pixels must be strictly ascending"},
		{name: "only final tier unbounded", old: "      - max_pixels: 2360000\n        price: \"0.30\"", new: "      - price: \"0.30\"", want: "only the final tier may be unbounded"},
		{name: "final tier unbounded", old: "      - price: \"0.60\"", new: "      - max_pixels: 5000000\n        price: \"0.60\"", want: "final tier must be unbounded"},
		{name: "tier prices positive", old: `price: "0.30"`, new: `price: "0.00"`, want: "price must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			costs := strings.Replace(validPixelCosts, tt.old, tt.new, 1)
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadBundleRequiresAllFiles(t *testing.T) {
	dir := writeBundleFixture(t, nil)
	if err := os.Remove(filepath.Join(dir, "costs.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := LoadBundle(dir)
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "costs.yaml") {
		t.Fatalf("LoadBundle error = %v, want missing costs.yaml", err)
	}
}

func TestLoadBundleRequiresExactMicroCNYCreditUnit(t *testing.T) {
	dir := writeBundleFixture(t, map[string]string{
		"economics.yaml": strings.Replace(validEconomicsYAML, "credits_per_cny: 1000", "credits_per_cny: 3000", 1),
	})
	_, err := LoadBundle(dir)
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "must divide 1000000 exactly") {
		t.Fatalf("LoadBundle error = %v, want exact micro-CNY conversion failure", err)
	}
}

func writeBundleFixture(t *testing.T, overrides map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"economics.yaml":  validEconomicsYAML,
		"products.yaml":   validProductsYAML,
		"costs.yaml":      validCostsYAML,
		"promotions.yaml": validPromotionsYAML,
	}
	for name, value := range overrides {
		files[name] = value
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

const validEconomicsYAML = `credits_per_cny: 1000
`

const validProductsYAML = `currency: credits
tier_rates_percent: { free: 100, pro: 90, enterprise: 80 }
task_time_pricing:
  timezone: Asia/Shanghai
  peak_windows:
  - { start: "09:00", end: "12:00" }
  - { start: "14:00", end: "18:00" }
  off_peak_rate_percent: 80
skus:
  - id: task.seednote.balanced
    operation: task.seednote
    execution_profile: balanced
    charge_policy: task_admission
    price_credits: 5000
    delivery: verified
`

const validCostsYAML = `currency_rates:
  USD: "7.20"
  CNY: "1.00"
models:
  provider/model:
    pricing_type: token
    currency: "CNY"
    unit: 1000000
    input: "6.00"
    cache_read_input: "1.20"
    cache_creation_input: "6.00"
    output: "30.00"
    operator_evidence: ark-price-sheet
    effective_at: "2026-07-17T00:00:00Z"
`

const validPromotionsYAML = `programs:
  - id: referral-first-topup-v1
    trigger: invitee_first_paid_topup
    minimum_topup_cny: "10.00"
    inviter_credits: 1000
    invitee_credits: 1000
    expires_after: 30d
    max_inviter_rewards: 10
`
