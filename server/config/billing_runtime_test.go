package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
)

func TestNewConfigLoadsBillingBundleRelativeToRootConfig(t *testing.T) {
	t.Setenv("ANBAN_TEST_BILLING_DIR", "catalogs")
	t.Setenv("ANBAN_TEST_BILLING_ADMIN_KEY", "billing-admin-secret")
	dir := t.TempDir()
	writeBillingRuntimeFixture(t, filepath.Join(dir, "catalogs"))
	path := filepath.Join(dir, "config.yaml")
	root := `database:
  dsn: root:dev@tcp(localhost:3306)/db
jwt:
  secret_key: secret
  access_expiry: 24h
  refresh_expiry: 168h
claude:
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    article: creator-agent-article:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
billing_runtime:
  config_dir: "${ANBAN_TEST_BILLING_DIR}"
  admin_api_key: "${ANBAN_TEST_BILLING_ADMIN_KEY}"
`
	if err := os.WriteFile(path, []byte(root), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := NewConfig(path)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.BillingRuntime.ConfigDir != filepath.Join(dir, "catalogs") {
		t.Fatalf("billing_runtime.config_dir = %q, want root-relative absolute path", cfg.BillingRuntime.ConfigDir)
	}
	if cfg.BillingRuntime.AdminAPIKey != "billing-admin-secret" {
		t.Fatalf("billing_runtime.admin_api_key was not expanded")
	}
	if cfg.BillingBundle == nil || !strings.HasPrefix(cfg.BillingBundle.Products.CatalogID, "retail-sha256-") || len(cfg.BillingBundle.Products.CatalogID) != len("retail-sha256-")+64 {
		t.Fatalf("BillingBundle = %#v, want content-addressed retail bundle", cfg.BillingBundle)
	}

	encoded, err := json.Marshal(cfg.BillingRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "billing-admin-secret") {
		t.Fatalf("serialized billing runtime leaked admin key: %s", encoded)
	}
}

func TestNewConfigKeepsBillingBundleOptionalUntilCutover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	root := `database:
  dsn: root:dev@tcp(localhost:3306)/db
jwt:
  secret_key: secret
  access_expiry: 24h
  refresh_expiry: 168h
claude:
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    article: creator-agent-article:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
`
	if err := os.WriteFile(path, []byte(root), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfig(path)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.BillingBundle != nil {
		t.Fatalf("BillingBundle = %#v, want nil when billing_runtime is not configured", cfg.BillingBundle)
	}
}

func TestNewConfigLoadsConfiguredBillingBundleStrictly(t *testing.T) {
	dir := t.TempDir()
	catalogDir := filepath.Join(dir, "catalogs")
	writeBillingRuntimeFixture(t, catalogDir)
	policyPath := filepath.Join(catalogDir, "policy.yaml")
	policy, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, append(policy, []byte("unknown: true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	root := `database:
  dsn: root:dev@tcp(localhost:3306)/db
jwt:
  secret_key: secret
  access_expiry: 24h
  refresh_expiry: 168h
claude:
  executor: docker
billing_runtime:
  config_dir: catalogs
  admin_api_key: secret
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(root), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = NewConfig(path)
	if !errors.Is(err, serverbilling.ErrInvalidConfig) {
		t.Fatalf("NewConfig error = %v, want billing.ErrInvalidConfig", err)
	}
}

func TestNewConfigRejectsUnknownBillingRuntimeField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	root := `database:
  dsn: root:dev@tcp(localhost:3306)/db
jwt:
  secret_key: secret
  access_expiry: 24h
  refresh_expiry: 168h
claude:
  executor: docker
billing_runtime:
  config_di: catalogs
  admin_api_key: secret
`
	if err := os.WriteFile(path, []byte(root), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewConfig(path)
	if err == nil || !strings.Contains(err.Error(), "billing_runtime.config_di") {
		t.Fatalf("NewConfig error = %v, want unknown billing_runtime.config_di rejection", err)
	}
}

func TestNewConfigRejectsDuplicateBillingRuntimeField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	root := `database:
  dsn: root:dev@tcp(localhost:3306)/db
jwt:
  secret_key: secret
  access_expiry: 24h
  refresh_expiry: 168h
claude:
  executor: docker
billing_runtime:
  config_dir: catalogs
  config_dir: other-catalogs
  admin_api_key: secret
`
	if err := os.WriteFile(path, []byte(root), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewConfig(path)
	if err == nil || !strings.Contains(err.Error(), "config_dir") {
		t.Fatalf("NewConfig error = %v, want duplicate billing_runtime.config_dir rejection", err)
	}
}

func writeBillingRuntimeFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"policy.yaml": `version: v1
credits_per_cny: 1000
task_admission: {require_zero_debt: true, require_full_price: true}
accepted_task: {continue_when_balance_negative: true, operation_charge_may_create_debt: true}
top_up: {repay_debt_first: true}
promotions: {may_repay_debt: false}
task_failure_reversal: {enabled: true, reasons: [platform_error, provider_error, execution_timeout, infrastructure_cancelled]}
`,
		"products.yaml": `currency: credits
tier_rates_percent: {free: 100, pro: 90, enterprise: 80}
skus:
  - {id: task.article.effective, operation: task.article, execution_profile: effective, charge_policy: task_admission, price_credits: 1000, delivery: verified}
`,
		"costs.yaml": `catalog_id: costs-v1
currency_rates: {CNY: "1.00"}
models:
  provider/model: {pricing_type: token, currency: CNY, unit: 1000000, input: "1.00", cache_read_input: "0.20", cache_creation_input: "1.00", output: "2.00", operator_evidence: test-fixture, effective_at: "2026-07-17T00:00:00Z"}
`,
		"promotions.yaml": `catalog_id: promotions-v1
programs:
  - {id: referral-v1, trigger: invitee_first_paid_topup, minimum_topup_cny: "10.00", inviter_credits: 100, invitee_credits: 100, expires_after: 24h, max_inviter_rewards: 1, can_repay_debt: false}
`,
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
