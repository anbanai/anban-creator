package billing

import "testing"

func TestLoadBundleProviderCostOpenAIImageUsage(t *testing.T) {
	costs := `catalog_id: provider-cost-v1
currency_rates:
  CNY: "1.00"
  USD: "7.20"
models:
  wangcai_openai/gpt-image-2-t:
    pricing_type: openai_image_usage
    currency: USD
    unit: 1000000
    text_input: "5.00"
    text_cached_input: "1.25"
    image_input: "8.00"
    image_cached_input: "2.00"
    image_output: "30.00"
    operator_evidence: wangcai-price-sheet
    effective_at: "2026-07-17T00:00:00Z"
`
	bundle, err := LoadBundle(writeBundleFixture(t, map[string]string{"costs.yaml": costs}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	got := bundle.Costs.Models["wangcai_openai/gpt-image-2-t"]
	if got.PricingType != "openai_image_usage" || got.Unit != 1_000_000 ||
		got.TextInput != 5_000_000 || got.TextCachedInput != 1_250_000 ||
		got.ImageInput != 8_000_000 || got.ImageCachedInput != 2_000_000 || got.ImageOutput != 30_000_000 {
		t.Fatalf("OpenAI image cost profile = %#v", got)
	}
}
