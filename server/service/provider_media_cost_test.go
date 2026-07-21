package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/model"
)

func providerCostBundleWithMedia() *billing.Bundle {
	bundle := providerCostBundleWithPixels()
	bundle.Costs.Models["wangcai_openai/gpt-image-2-t"] = billing.ModelCostConfig{
		PricingType: "openai_image_usage", Currency: "USD", Unit: 1_000_000,
		TextInput: 5_000_000, TextCachedInput: 1_250_000, ImageInput: 8_000_000,
		ImageCachedInput: 2_000_000, ImageOutput: 30_000_000,
		OperatorEvidence: "test-openai-image-price", EffectiveAt: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
	}
	return bundle
}

func TestProviderCostOpenAIImageUsageUsesAllFiveCategoriesExactly(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithMedia())
	event, err := fixture.service.RecordOpenAIImageUsage(context.Background(), RecordOpenAIImageUsageCostRequest{
		TaskID: "task-image", Provider: "wangcai_openai", Model: "gpt-image-2-t",
		ProviderRequestID: "image-request-1", CatalogID: "cost-v1", IdempotencyKey: "image-request-1",
		Usage:  OpenAIImageUsage{TextInput: 100, TextCachedInput: 20, ImageInput: 30, ImageCachedInput: 10, ImageOutput: 40},
		Source: string(model.BillingProviderCostSourceProviderResponse),
	})
	if err != nil {
		t.Fatalf("RecordOpenAIImageUsage: %v", err)
	}
	// USD numerator = 100*5 + 20*1.25 + 30*8 + 10*2 + 40*30 = 1985 micro-USD
	// at 7.20 CNY/USD => 14292 micro-CNY.
	if event.CostMicroCNY != 14_292 {
		t.Fatalf("cost_micro_cny = %d, want 14292", event.CostMicroCNY)
	}
	var evidence map[string]any
	if err := json.Unmarshal(event.UsageEvidence, &evidence); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"prompt", "url", "authorization", "token"} {
		if _, exists := evidence[forbidden]; exists {
			t.Fatalf("unsafe evidence key %q persisted: %#v", forbidden, evidence)
		}
	}
}
