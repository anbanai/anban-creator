package billing

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProductionBillingBundleMatchesPolicy(t *testing.T) {
	bundle := loadProductionBundle(t)
	if bundle.Economics.CreditsPerCNY != 1000 {
		t.Fatalf("economics credits/CNY = %d", bundle.Economics.CreditsPerCNY)
	}
	policy := bundle.Policy
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

func TestContentAddressedRetailCatalog(t *testing.T) {
	if err := initialRetailCatalogContractError(loadProductionBundle(t).Products); err != nil {
		t.Fatal(err)
	}
}

func TestProviderCostCatalogV4(t *testing.T) {
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
		"deepseek/deepseek-v4-flash":                    {"token", "USD", 140_000, 2_800, 140_000, 280_000},
		"deepseek/deepseek-v4-pro":                      {"token", "USD", 435_000, 3_625, 435_000, 870_000},
		"moonshot/kimi-k3":                              {"token", "CNY", 20_000_000, 2_000_000, 20_000_000, 100_000_000},
		"moonshot/kimi-k2.7-code":                       {"token", "CNY", 6_500_000, 1_300_000, 6_500_000, 27_000_000},
		"moonshot/kimi-k2.7-code-highspeed":             {"token", "CNY", 13_000_000, 2_600_000, 13_000_000, 54_000_000},
		"zhipu/glm-5.2":                                 {"token", "CNY", 8_000_000, 2_000_000, 8_000_000, 28_000_000},
		"volcengine_ark/doubao-seedream-5-0-pro-260628": {pricingType: "output_pixel_tier", currency: "CNY"},
		"wangcai_openai/gpt-image-2-t":                  {pricingType: "openai_image_usage", currency: "USD"},
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
		if model.PricingType != expected.pricingType || model.Currency != expected.currency {
			t.Fatalf("cost profile %q identity = %#v", modelID, model)
		}
		if model.PricingType == "token" {
			if model.Unit != 1_000_000 || model.Input != expected.input || model.CacheReadInput != expected.cacheRead || model.CacheCreationInput != expected.cacheCreate || model.Output != expected.output || len(model.Tiers) != 0 {
				t.Fatalf("token cost profile %q = %#v", modelID, model)
			}
			continue
		}
		switch model.PricingType {
		case "output_pixel_tier":
			if len(model.Tiers) != 2 || model.Tiers[0] != (CostTier{MaxPixels: 2_360_000, Price: 300_000}) || model.Tiers[1] != (CostTier{MaxPixels: 0, Price: 600_000}) {
				t.Fatalf("Seedream tiers = %#v", model.Tiers)
			}
		case "openai_image_usage":
			if model.Unit != 1_000_000 || model.TextInput != 5_000_000 || model.TextCachedInput != 1_250_000 || model.ImageInput != 8_000_000 || model.ImageCachedInput != 2_000_000 || model.ImageOutput != 30_000_000 {
				t.Fatalf("OpenAI image cost profile = %#v", model)
			}
		}
	}
}

func TestProductionCatalogMetadataMatchesApprovedSnapshots(t *testing.T) {
	if err := initialCatalogMetadataContractError(loadProductionBundle(t)); err != nil {
		t.Fatal(err)
	}
}

func TestProductionPromotionsContainOnlyFirstPaidTopUpReferral(t *testing.T) {
	programs := loadProductionBundle(t).Promotions.Programs
	if len(programs) != 1 {
		t.Fatalf("promotion programs = %#v, want exactly one referral", programs)
	}
	program := programs[0]
	if program.ID != ReferralFirstTopUpProgramID || program.Trigger != "invitee_first_paid_topup" || program.MinimumTopUpCNY != 10_000_000 || program.InviterCredits != 1000 || program.InviteeCredits != 1000 || program.ExpiresAfter != 30*24*time.Hour || program.MaxInviterRewards != 10 {
		t.Fatalf("referral program = %#v", program)
	}
}

func TestInitialCatalogMetadataContractRejectsMutations(t *testing.T) {
	production := loadProductionBundle(t)
	tests := []struct {
		name   string
		mutate func(*Bundle)
		want   string
	}{
		{
			name: "cost catalog ID",
			mutate: func(bundle *Bundle) {
				bundle.Costs.CatalogID = "provider-cost-other"
			},
			want: "cost catalog ID",
		},
		{
			name: "promotion catalog ID",
			mutate: func(bundle *Bundle) {
				bundle.Promotions.CatalogID = "promotion-other"
			},
			want: "promotion catalog ID",
		},
		{
			name: "operator evidence locator",
			mutate: func(bundle *Bundle) {
				modelID := "volcengine_ark/doubao-seed-evolving"
				model := bundle.Costs.Models[modelID]
				model.OperatorEvidence = "pricing-snapshot:changed"
				bundle.Costs.Models[modelID] = model
			},
			want: "operator_evidence",
		},
		{
			name: "effective timestamp",
			mutate: func(bundle *Bundle) {
				modelID := "moonshot/kimi-k2.7-code"
				model := bundle.Costs.Models[modelID]
				model.EffectiveAt = model.EffectiveAt.Add(time.Second)
				bundle.Costs.Models[modelID] = model
			},
			want: "effective_at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := cloneCatalogMetadataBundle(production)
			tt.mutate(&bundle)
			err := initialCatalogMetadataContractError(&bundle)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("contract error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestInitialRetailCatalogContractRejectsUnsupportedAdditions(t *testing.T) {
	production := loadProductionBundle(t).Products
	tests := []struct {
		name string
		sku  SKUConfig
		want string
	}{
		{
			name: "arbitrary extra SKU",
			sku: SKUConfig{
				ID: "extra.arbitrary", Operation: "extra.arbitrary", ChargePolicy: "standalone_operation",
				PriceCredits: 1, Route: "extra.arbitrary", Delivery: "extra",
			},
			want: "unexpected SKU",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := production
			catalog.SKUs = append(append([]SKUConfig(nil), production.SKUs...), tt.sku)
			err := initialRetailCatalogContractError(catalog)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("contract error = %v, want error containing %q", err, tt.want)
			}
		})
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
			products: products + `  - id: "task.article.balanced"
    operation: "task.duplicate"
    execution_profile: "balanced"
    charge_policy: "task_admission"
    price_credits: 1
    delivery: "duplicate"
`,
			want: "duplicate SKU id",
		},
		{
			name: "ambiguous billable identity",
			products: products + `  - id: "task.article.alternate"
    operation: "task.article"
    execution_profile: "balanced"
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
	for _, name := range []string{"economics.yaml", "products.yaml", "costs.yaml", "promotions.yaml"} {
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

func cloneCatalogMetadataBundle(source *Bundle) Bundle {
	clone := *source
	clone.Costs.Models = make(map[string]ModelCostConfig, len(source.Costs.Models))
	for modelID, model := range source.Costs.Models {
		clone.Costs.Models[modelID] = model
	}
	return clone
}

func initialCatalogMetadataContractError(bundle *Bundle) error {
	if !strings.HasPrefix(bundle.Costs.CatalogID, "provider-cost-sha256-") || len(bundle.Costs.CatalogID) != len("provider-cost-sha256-")+64 {
		return fmt.Errorf("cost catalog ID = %q", bundle.Costs.CatalogID)
	}
	if !strings.HasPrefix(bundle.Promotions.CatalogID, "promotion-sha256-") || len(bundle.Promotions.CatalogID) != len("promotion-sha256-")+64 {
		return fmt.Errorf("promotion catalog ID = %q", bundle.Promotions.CatalogID)
	}
	wantEvidence := map[string]string{
		"volcengine_ark/doubao-seed-evolving":           "pricing-snapshot:volcengine-ark:model-token-prices:2026-07-13",
		"volcengine_ark/doubao-seed-2-1-pro-260628":     "pricing-snapshot:volcengine-ark:model-token-prices:2026-07-13",
		"volcengine_ark/doubao-seed-2-1-turbo-260628":   "pricing-snapshot:volcengine-ark:model-token-prices:2026-07-13",
		"deepseek/deepseek-v4-flash":                    "https://api-docs.deepseek.com/quick_start/pricing/",
		"deepseek/deepseek-v4-pro":                      "https://api-docs.deepseek.com/quick_start/pricing/",
		"moonshot/kimi-k3":                              "https://platform.moonshot.cn/docs/pricing/chat",
		"moonshot/kimi-k2.7-code":                       "https://platform.moonshot.cn/docs/pricing/chat",
		"moonshot/kimi-k2.7-code-highspeed":             "https://platform.moonshot.cn/docs/pricing/chat",
		"zhipu/glm-5.2":                                 "https://open.bigmodel.cn/pricing",
		"volcengine_ark/doubao-seedream-5-0-pro-260628": "pricing-snapshot:volcengine-ark:seedream-output-pixel-prices:2026-07-13",
		"wangcai_openai/gpt-image-2-t":                  "pricing-snapshot:wangcai-openai:gpt-image-2-t-usage-prices:2026-07-13",
	}
	newEffectiveAt := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	legacyEffectiveAt := time.Date(2026, time.July, 13, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	if len(bundle.Costs.Models) != len(wantEvidence) {
		return fmt.Errorf("cost model count = %d, want %d evidenced models", len(bundle.Costs.Models), len(wantEvidence))
	}
	for modelID, evidence := range wantEvidence {
		model, ok := bundle.Costs.Models[modelID]
		if !ok {
			return fmt.Errorf("missing evidenced cost model %q", modelID)
		}
		if model.OperatorEvidence != evidence {
			return fmt.Errorf("models.%s.operator_evidence = %q, want %q", modelID, model.OperatorEvidence, evidence)
		}
		wantEffectiveAt := legacyEffectiveAt
		if strings.HasPrefix(modelID, "deepseek/") || strings.HasPrefix(modelID, "moonshot/") || strings.HasPrefix(modelID, "zhipu/") {
			wantEffectiveAt = newEffectiveAt
		}
		if !model.EffectiveAt.Equal(wantEffectiveAt) {
			return fmt.Errorf("models.%s.effective_at = %q, want %q", modelID, model.EffectiveAt.Format(time.RFC3339), wantEffectiveAt.Format(time.RFC3339))
		}
	}
	return nil
}

func initialRetailCatalogContractError(catalog ProductCatalog) error {
	type skuSnapshot struct {
		operation        string
		executionProfile string
		chargePolicy     string
		route            string
		delivery         string
		priceCredits     int64
	}
	want := map[string]skuSnapshot{
		"image.standard":     {operation: "image.generate", chargePolicy: "image_operation", priceCredits: 500, route: "image_generation.capabilities.standard", delivery: "persisted_image"},
		"image.professional": {operation: "image.generate", chargePolicy: "image_operation", priceCredits: 500, route: "image_generation.capabilities.professional", delivery: "persisted_image"},
	}
	taskTypes := []struct {
		id, operation, delivery string
		balanced                int64
	}{
		{"article", "task.article", "article_artifacts_verified", 6000},
		{"seednote", "task.seednote", "seednote_artifacts_verified", 5000},
		{"moments", "task.moments", "moments_artifacts_verified", 3000},
		{"ecommerce", "task.ecommerce", "ecommerce_artifacts_verified", 3000},
		{"montage", "task.montage", "montage_artifacts_verified", 2000},
	}
	profiles := []struct {
		id        string
		numerator int64
	}{
		{"effective", 8},
		{"balanced", 10},
		{"quality", 30},
	}
	for _, taskType := range taskTypes {
		for _, profile := range profiles {
			id := "task." + taskType.id + "." + profile.id
			want[id] = skuSnapshot{
				operation: taskType.operation, executionProfile: profile.id, chargePolicy: "task_admission",
				priceCredits: taskType.balanced * profile.numerator / 10, delivery: taskType.delivery,
			}
		}
	}
	for _, profile := range []string{"effective", "balanced", "quality"} {
		want["task.viral-analysis."+profile] = skuSnapshot{
			operation: "task.viral_analysis", executionProfile: profile, chargePolicy: "task_admission", priceCredits: 1200,
			delivery: "viral_analysis_report_verified",
		}
	}
	if !strings.HasPrefix(catalog.CatalogID, "retail-sha256-") || len(catalog.CatalogID) != len("retail-sha256-")+64 || catalog.Currency != "credits" {
		return fmt.Errorf("retail catalog identity = %q/%q", catalog.CatalogID, catalog.Currency)
	}
	wantRates := map[string]int64{"free": 100, "pro": 90, "enterprise": 80}
	if len(catalog.TierRatesPercent) != len(wantRates) {
		return fmt.Errorf("tier rates = %#v, want %#v", catalog.TierRatesPercent, wantRates)
	}
	for tier, rate := range wantRates {
		if catalog.TierRatesPercent[tier] != rate {
			return fmt.Errorf("tier rate %q = %d, want %d", tier, catalog.TierRatesPercent[tier], rate)
		}
	}
	seen := make(map[string]int, len(catalog.SKUs))
	for _, sku := range catalog.SKUs {
		expected, ok := want[sku.ID]
		if !ok {
			return fmt.Errorf("unexpected SKU ID %q", sku.ID)
		}
		actual := skuSnapshot{
			operation: sku.Operation, executionProfile: sku.ExecutionProfile, chargePolicy: sku.ChargePolicy, route: sku.Route,
			delivery: sku.Delivery, priceCredits: sku.PriceCredits,
		}
		if actual != expected {
			return fmt.Errorf("SKU %q snapshot = %#v, want %#v", sku.ID, actual, expected)
		}
		seen[sku.ID]++
		if seen[sku.ID] != 1 {
			return fmt.Errorf("SKU ID %q occurs %d times", sku.ID, seen[sku.ID])
		}
	}
	if len(catalog.SKUs) != len(want) {
		return fmt.Errorf("retail SKU count = %d, want %d", len(catalog.SKUs), len(want))
	}
	for skuID := range want {
		if seen[skuID] != 1 {
			return fmt.Errorf("missing SKU ID %q", skuID)
		}
	}
	return nil
}

func TestAgentProfileCatalogV7UsesOnlyNewIDsAndKeepsDesignerIsolated(t *testing.T) {
	catalog := loadProductionBundle(t).Products
	allowed := map[string]bool{"effective": true, "balanced": true, "quality": true}
	for _, sku := range catalog.SKUs {
		if strings.HasPrefix(sku.Operation, "task.") && sku.ExecutionProfile != "" && !allowed[sku.ExecutionProfile] {
			t.Fatalf("task SKU %s uses profile %q", sku.ID, sku.ExecutionProfile)
		}
		if strings.HasPrefix(sku.Operation, "designer.") && sku.ExecutionProfile != "" {
			t.Fatalf("Designer SKU %s has Agent profile %q", sku.ID, sku.ExecutionProfile)
		}
	}
}
