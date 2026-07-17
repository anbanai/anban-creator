package billing

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProductionBillingBundleMatchesPolicy(t *testing.T) {
	bundle := loadProductionBundle(t)
	policy := bundle.Policy
	if policy.Version != "2026-07-17" || policy.CreditsPerCNY != 1000 {
		t.Fatalf("policy identity = version %q credits/CNY %d", policy.Version, policy.CreditsPerCNY)
	}
	if !policy.TaskAdmission.RequireZeroDebt || !policy.TaskAdmission.RequireFullPrice {
		t.Fatalf("task admission policy = %#v", policy.TaskAdmission)
	}
	if !policy.AcceptedTask.ContinueWhenBalanceNegative || !policy.AcceptedTask.OperationChargeMayCreateDebt {
		t.Fatalf("accepted task policy = %#v", policy.AcceptedTask)
	}
	if !policy.TopUp.RepayDebtFirst || policy.Promotions.MayRepayDebt || !policy.TaskFailureReversal.Enabled {
		t.Fatalf("finance policy = topup %#v promotions %#v reversal %#v", policy.TopUp, policy.Promotions, policy.TaskFailureReversal)
	}
	wantReasons := map[string]bool{
		"platform_error": true, "provider_error": true, "execution_timeout": true, "infrastructure_cancelled": true,
	}
	if len(policy.TaskFailureReversal.Reasons) != len(wantReasons) {
		t.Fatalf("reversal reasons = %#v", policy.TaskFailureReversal.Reasons)
	}
	for _, reason := range policy.TaskFailureReversal.Reasons {
		if !wantReasons[reason] {
			t.Fatalf("unexpected reversal reason %q", reason)
		}
	}
}

func TestProductionTaskCatalogHasExactInitialCoverage(t *testing.T) {
	type expectedTask struct {
		price    int64
		delivery string
	}
	want := map[string]expectedTask{
		"task.article":        {price: 6000, delivery: "article_artifacts_verified"},
		"task.seednote":       {price: 5000, delivery: "seednote_artifacts_verified"},
		"task.moments":        {price: 3000, delivery: "moments_artifacts_verified"},
		"task.ecommerce":      {price: 3000, delivery: "ecommerce_artifacts_verified"},
		"task.videocreator":   {price: 2000, delivery: "final_video_verified"},
		"task.videoeditor":    {price: 2000, delivery: "edited_video_verified"},
		"task.montage":        {price: 2000, delivery: "montage_artifacts_verified"},
		"task.viral_analysis": {price: 1200, delivery: "viral_analysis_report_verified"},
	}
	seen := make(map[string]int)
	for _, sku := range loadProductionBundle(t).Products.SKUs {
		if sku.ChargePolicy != "task_admission" {
			continue
		}
		expected, ok := want[sku.Operation]
		if !ok {
			t.Fatalf("unexpected task operation %q", sku.Operation)
		}
		seen[sku.Operation]++
		if sku.Route != "" || sku.PriceCredits != expected.price || sku.Delivery != expected.delivery {
			t.Fatalf("task SKU %q = %#v, want price=%d delivery=%q and no route", sku.Operation, sku, expected.price, expected.delivery)
		}
	}
	for operation := range want {
		if seen[operation] != 1 {
			t.Fatalf("task operation %q occurs %d times, want exactly once", operation, seen[operation])
		}
	}
}

func TestProductionImageCatalogHasExactInitialCoverage(t *testing.T) {
	type imageIdentity struct {
		operation string
		policy    string
		route     string
	}
	want := map[imageIdentity]bool{
		{operation: "mcp.generate_image", policy: "accepted_task_operation", route: "image_generation.cover"}:               true,
		{operation: "mcp.generate_image", policy: "accepted_task_operation", route: "image_generation.content"}:             true,
		{operation: "designer.generate_image", policy: "standalone_operation", route: "image_generation.designer.seedream"}: true,
	}
	seen := make(map[imageIdentity]int)
	for _, sku := range loadProductionBundle(t).Products.SKUs {
		if !strings.HasPrefix(sku.Route, "image_generation.") {
			continue
		}
		identity := imageIdentity{operation: sku.Operation, policy: sku.ChargePolicy, route: sku.Route}
		if !want[identity] {
			t.Fatalf("unexpected image SKU mapping %#v", sku)
		}
		seen[identity]++
		if sku.PriceCredits != 500 || sku.Delivery != "persisted_image" {
			t.Fatalf("image SKU mapping %#v must cost 500 credits and persist an image", sku)
		}
	}
	for identity := range want {
		if seen[identity] != 1 {
			t.Fatalf("image mapping %#v occurs %d times, want exactly once", identity, seen[identity])
		}
	}
}

func TestProductionCostCatalogHasExactEvidencedProfiles(t *testing.T) {
	type expectedCost struct {
		pricingType string
		currency    string
		input       MicroCNY
		cacheRead   MicroCNY
		cacheCreate MicroCNY
		output      MicroCNY
	}
	want := map[string]expectedCost{
		"volcengine_ark/doubao-seed-evolving":           {"token", "CNY", 6_000_000, 1_200_000, 6_000_000, 30_000_000},
		"volcengine_ark/doubao-seed-2-1-pro-260628":     {"token", "CNY", 6_000_000, 1_200_000, 6_000_000, 30_000_000},
		"volcengine_ark/doubao-seed-2-1-turbo-260628":   {"token", "CNY", 3_000_000, 600_000, 3_000_000, 15_000_000},
		"moonshot/kimi-k2.7-code":                       {"token", "USD", 950_000, 190_000, 950_000, 4_000_000},
		"moonshot/kimi-k2.7-code-highspeed":             {"token", "USD", 1_900_000, 380_000, 1_900_000, 8_000_000},
		"volcengine_ark/doubao-seedream-5-0-pro-260628": {pricingType: "output_pixel_tier", currency: "CNY"},
	}
	bundle := loadProductionBundle(t)
	if bundle.Costs.CurrencyRates["CNY"] != 1_000_000 || bundle.Costs.CurrencyRates["USD"] != 7_200_000 || len(bundle.Costs.CurrencyRates) != 2 {
		t.Fatalf("currency rates = %#v, want only CNY=1.00 and USD=7.20", bundle.Costs.CurrencyRates)
	}
	if len(bundle.Costs.Models) != len(want) {
		t.Fatalf("cost model count = %d, want %d", len(bundle.Costs.Models), len(want))
	}
	for modelID, expected := range want {
		model, ok := bundle.Costs.Models[modelID]
		if !ok {
			t.Fatalf("missing cost profile %q", modelID)
		}
		if model.PricingType != expected.pricingType || model.Currency != expected.currency || strings.TrimSpace(model.OperatorEvidence) == "" || model.EffectiveAt.IsZero() {
			t.Fatalf("cost profile %q identity/evidence = %#v", modelID, model)
		}
		if model.PricingType == "token" {
			if model.Unit != 1_000_000 || model.Input != expected.input || model.CacheReadInput != expected.cacheRead || model.CacheCreationInput != expected.cacheCreate || model.Output != expected.output || len(model.Tiers) != 0 {
				t.Fatalf("token cost profile %q = %#v", modelID, model)
			}
			continue
		}
		if len(model.Tiers) != 2 || model.Tiers[0] != (CostTier{MaxPixels: 2_360_000, Price: 300_000}) || model.Tiers[1] != (CostTier{MaxPixels: 0, Price: 600_000}) {
			t.Fatalf("Seedream tiers = %#v", model.Tiers)
		}
	}
}

func TestProductionPromotionsContainOnlyFirstPaidTopUpReferral(t *testing.T) {
	programs := loadProductionBundle(t).Promotions.Programs
	if len(programs) != 1 {
		t.Fatalf("promotion programs = %#v, want exactly one referral", programs)
	}
	program := programs[0]
	if program.ID != "referral-first-topup-v1" || program.Trigger != "invitee_first_paid_topup" || program.MinimumTopUpCNY != 10_000_000 || program.InviterCredits != 1000 || program.InviteeCredits != 1000 || program.ExpiresAfter != 30*24*time.Hour || program.MaxInviterRewards != 10 || program.CanRepayDebt {
		t.Fatalf("referral program = %#v", program)
	}
}

func TestProductionRetailCatalogDocumentsPreCutoverBoundary(t *testing.T) {
	for _, sku := range loadProductionBundle(t).Products.SKUs {
		identity := strings.ToLower(sku.ID + " " + sku.Operation + " " + sku.Route)
		// Exact video selector SKUs and GPT Image 2 quality/size SKUs are published
		// by Plan 3 Task 5. Their absence keeps this foundation non-cutover-ready.
		if strings.Contains(identity, "gpt-image") || strings.Contains(identity, "gpt_image") || strings.Contains(sku.Route, "video_generation.") {
			t.Fatalf("pre-cutover retail catalog unexpectedly exposes %q", identity)
		}
	}
}

func TestProductionLoaderRejectsUnknownDuplicateAndAmbiguousMappings(t *testing.T) {
	products := readProductionCatalog(t, "products.yaml")
	tests := []struct {
		name     string
		products string
		want     string
	}{
		{
			name: "unknown legacy provider budget field",
			products: strings.Replace(products, `    delivery: "article_artifacts_verified"`, `    delivery: "article_artifacts_verified"
    provider_cost_budget: 1`, 1),
			want: "field provider_cost_budget not found",
		},
		{
			name: "duplicate SKU ID",
			products: products + `  - id: "task.article.standard.v1"
    operation: "task.duplicate"
    charge_policy: "task_admission"
    price_credits: 1
    delivery: "duplicate"
`,
			want: "duplicate SKU id",
		},
		{
			name: "ambiguous billable identity",
			products: products + `  - id: "task.article.alternate.v1"
    operation: "task.article"
    charge_policy: "task_admission"
    price_credits: 1
    delivery: "alternate"
`,
			want: "duplicates billable identity",
		},
		{
			name:     "unknown charge policy",
			products: strings.Replace(products, `charge_policy: "task_admission"`, `charge_policy: "unknown_policy"`, 1),
			want:     "unsupported value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadBundle(writeProductionBundle(t, map[string]string{"products.yaml": tt.products}))
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadBundle error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}
}

func TestExampleConfigDocumentsOptionalBillingRuntimePointer(t *testing.T) {
	example, err := os.ReadFile(filepath.Join(productionBillingDir(t), "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "billing_runtime:\n  config_dir: \"./billing\"\n  admin_api_key: \"${ANBAN_BILLING_ADMIN_API_KEY}\"\n"
	if !strings.Contains(string(example), want) {
		t.Fatalf("config.example.yaml does not document the production billing runtime pointer")
	}
}

func loadProductionBundle(t *testing.T) *Bundle {
	t.Helper()
	bundle, err := LoadBundle(productionBillingDir(t))
	if err != nil {
		t.Fatalf("LoadBundle(production): %v", err)
	}
	return bundle
}

func productionBillingDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve production billing directory")
	}
	return filepath.Dir(file)
}

func readProductionCatalog(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(productionBillingDir(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}

func writeProductionBundle(t *testing.T, overrides map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"policy.yaml", "products.yaml", "costs.yaml", "promotions.yaml"} {
		contents := readProductionCatalog(t, name)
		if override, ok := overrides[name]; ok {
			contents = override
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}
