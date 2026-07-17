package billing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadBundleLoadsTypedCatalogs(t *testing.T) {
	bundle, err := LoadBundle(writeBundleFixture(t, nil))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle.Policy.CreditsPerCNY != 1000 {
		t.Fatalf("credits_per_cny = %d, want 1000", bundle.Policy.CreditsPerCNY)
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
		{name: "unknown field", overrides: map[string]string{"policy.yaml": validPolicyYAML + "unknown: true\n"}, want: "field unknown not found"},
		{name: "trailing document", overrides: map[string]string{"policy.yaml": validPolicyYAML + "---\nversion: second\n"}, want: "trailing YAML document"},
		{name: "duplicate SKU", overrides: map[string]string{"products.yaml": strings.Replace(validProductsYAML, "skus:\n", "skus:\n  - id: task.seednote.standard.v1\n    operation: task.seednote\n    charge_policy: task_admission\n    price_credits: 5000\n    delivery: verified\n", 1)}, want: "duplicate SKU id"},
		{name: "duplicate program", overrides: map[string]string{"promotions.yaml": validPromotionsYAML + "  - id: referral-v1\n    trigger: invitee_first_paid_topup\n    minimum_topup_cny: \"10.00\"\n    inviter_credits: 1\n    invitee_credits: 1\n    expires_after: 24h\n    max_inviter_rewards: 1\n    can_repay_debt: false\n"}, want: "duplicate referral program id"},
		{name: "malformed currency rate", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `"7.20"`, `"7.2.0"`, 1)}, want: "currency_rates.USD"},
		{name: "currency rate must be string", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `"7.20"`, `7.20`, 1)}, want: "must be a quoted decimal string"},
		{name: "malformed model price", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `input: "6.00"`, `input: "1e3"`, 1)}, want: "models.provider/model.input"},
		{name: "malformed promotion price", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, `"10.00"`, `"-10.00"`, 1)}, want: "minimum_topup_cny"},
		{name: "missing currency reference", overrides: map[string]string{"costs.yaml": strings.Replace(validCostsYAML, `currency: "CNY"`, `currency: "EUR"`, 1)}, want: "references missing currency rate"},
		{name: "invalid charge policy", overrides: map[string]string{"products.yaml": strings.Replace(validProductsYAML, "task_admission", "runtime_usage", 1)}, want: "charge_policy"},
		{name: "promotion cannot repay debt", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, "can_repay_debt: false", "can_repay_debt: true", 1)}, want: "can_repay_debt must be false"},
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

func TestLoadBundleRejectsWeakenedPolicy(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "task admission requires zero debt", old: "require_zero_debt: true", new: "require_zero_debt: false", want: "task_admission.require_zero_debt: must be true"},
		{name: "task admission requires full price", old: "require_full_price: true", new: "require_full_price: false", want: "task_admission.require_full_price: must be true"},
		{name: "accepted task continues negative", old: "continue_when_balance_negative: true", new: "continue_when_balance_negative: false", want: "accepted_task.continue_when_balance_negative: must be true"},
		{name: "operation may create debt", old: "operation_charge_may_create_debt: true", new: "operation_charge_may_create_debt: false", want: "accepted_task.operation_charge_may_create_debt: must be true"},
		{name: "topup repays debt first", old: "repay_debt_first: true", new: "repay_debt_first: false", want: "top_up.repay_debt_first: must be true"},
		{name: "promotions never repay debt", old: "may_repay_debt: false", new: "may_repay_debt: true", want: "promotions.may_repay_debt: must be false"},
		{name: "failure reversal enabled", old: "enabled: true", new: "enabled: false", want: "task_failure_reversal.enabled: must be true"},
		{name: "failure reasons required", old: "reasons: [platform_error, provider_error, execution_timeout, infrastructure_cancelled]", new: "reasons: []", want: "task_failure_reversal.reasons: must not be empty"},
		{name: "failure reasons constrained", old: "provider_error", new: "user_cancelled", want: "unsupported reversal reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := strings.Replace(validPolicyYAML, tt.old, tt.new, 1)
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"policy.yaml": policy}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadBundleRequiresExactReversalReasonSet(t *testing.T) {
	tests := []struct {
		name    string
		reasons string
		want    string
	}{
		{name: "missing reason", reasons: "[platform_error, provider_error, execution_timeout]", want: "must equal the approved set"},
		{name: "extra reason", reasons: "[platform_error, provider_error, execution_timeout, infrastructure_cancelled, user_cancelled]", want: "must equal the approved set"},
		{name: "duplicate reason", reasons: "[platform_error, provider_error, execution_timeout, platform_error]", want: "duplicate reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := strings.Replace(validPolicyYAML, "[platform_error, provider_error, execution_timeout, infrastructure_cancelled]", tt.reasons, 1)
			_, err := LoadBundle(writeBundleFixture(t, map[string]string{"policy.yaml": policy}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
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
			products: validProductsYAML + `  - id: task.seednote.alternate.v1
    operation: task.seednote
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
    charge_policy: task_admission`, `operation: mcp.generate_image
    charge_policy: accepted_task_operation`, 1),
			want: "route is required for accepted_task_operation",
		},
		{
			name: "standalone operation requires route",
			products: strings.Replace(validProductsYAML, `operation: task.seednote
    charge_policy: task_admission`, `operation: designer.generate_image
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
	products := `catalog_id: " retail-v1 "
currency: " credits "
skus:
  - id: " image.standard.v1 "
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
	if bundle.Products.CatalogID != "retail-v1" || bundle.Products.Currency != "credits" {
		t.Fatalf("product identity = catalog %q currency %q", bundle.Products.CatalogID, bundle.Products.Currency)
	}
	want := SKUConfig{
		ID: "image.standard.v1", Operation: "mcp.generate_image", ChargePolicy: "accepted_task_operation",
		PriceCredits: 500, Route: "image_generation.content", Delivery: "persisted_image",
	}
	if got := bundle.Products.SKUs[0]; got != want {
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

func TestLoadBundleRejectsCanonicalDuplicateSKUIDs(t *testing.T) {
	products := `catalog_id: retail-v1
currency: credits
skus:
  - id: " duplicate.v1 "
    operation: task.one
    charge_policy: task_admission
    price_credits: 100
    delivery: one
  - id: duplicate.v1
    operation: task.two
    charge_policy: task_admission
    price_credits: 100
    delivery: two
`
	_, err := LoadBundle(writeBundleFixture(t, map[string]string{"products.yaml": products}))
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "duplicate SKU id") {
		t.Fatalf("LoadBundle error = %v, want canonical duplicate SKU rejection", err)
	}
}

func TestLoadBundleCanonicalizesCatalogAndCurrencyIdentities(t *testing.T) {
	policy := strings.Replace(validPolicyYAML, `version: "2026-07-17"`, `version: " 2026-07-17 "`, 1)
	costs := strings.Replace(validCostsYAML, "catalog_id: provider-cost-v1", `catalog_id: " provider-cost-v1 "`, 1)
	costs = strings.Replace(costs, "  CNY: \"1.00\"", `  " CNY ": "1.00"`, 1)
	costs = strings.Replace(costs, `currency: "CNY"`, `currency: " CNY "`, 1)
	promotions := strings.Replace(validPromotionsYAML, "catalog_id: promotion-v1", `catalog_id: " promotion-v1 "`, 1)
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{
		"policy.yaml": policy, "costs.yaml": costs, "promotions.yaml": promotions,
	}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle.Policy.Version != "2026-07-17" || bundle.Costs.CatalogID != "provider-cost-v1" || bundle.Promotions.CatalogID != "promotion-v1" {
		t.Fatalf("catalog identities not canonical: policy=%q costs=%q promotions=%q", bundle.Policy.Version, bundle.Costs.CatalogID, bundle.Promotions.CatalogID)
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
	validPixelCosts := `catalog_id: provider-cost-v1
currency_rates:
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

func writeBundleFixture(t *testing.T, overrides map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"policy.yaml":     validPolicyYAML,
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

const validPolicyYAML = `version: "2026-07-17"
credits_per_cny: 1000
task_admission:
  require_zero_debt: true
  require_full_price: true
accepted_task:
  continue_when_balance_negative: true
  operation_charge_may_create_debt: true
top_up:
  repay_debt_first: true
promotions:
  may_repay_debt: false
task_failure_reversal:
  enabled: true
  reasons: [platform_error, provider_error, execution_timeout, infrastructure_cancelled]
`

const validProductsYAML = `catalog_id: retail-v1
currency: credits
skus:
  - id: task.seednote.standard.v1
    operation: task.seednote
    charge_policy: task_admission
    price_credits: 5000
    delivery: verified
`

const validCostsYAML = `catalog_id: provider-cost-v1
currency_rates:
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

const validPromotionsYAML = `catalog_id: promotion-v1
programs:
  - id: referral-v1
    trigger: invitee_first_paid_topup
    minimum_topup_cny: "10.00"
    inviter_credits: 1000
    invitee_credits: 1000
    expires_after: 30d
    max_inviter_rewards: 10
    can_repay_debt: false
`
