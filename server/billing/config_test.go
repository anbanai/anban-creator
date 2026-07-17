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
		{name: "promotion cannot repay debt", overrides: map[string]string{"promotions.yaml": strings.Replace(validPromotionsYAML, "can_repay_debt: false", "can_repay_debt: true", 1)}, want: "contradicts policy.promotions.may_repay_debt"},
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
  reasons: [platform_error, provider_error]
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
