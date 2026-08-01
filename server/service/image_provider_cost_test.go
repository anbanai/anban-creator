package service

import (
	"context"
	"testing"

	appimage "github.com/anbanai/anban-creator/app/image"
	"github.com/anbanai/anban-creator/server/model"
)

func TestImageProviderCostUsesPreTransformDimensions(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithMedia())
	svc := &ImageService{providerCostSvc: fixture.service}
	result := &ImageResult{
		ProviderRequestID: "seedream-original-output",
		Provider:          "volcengine_ark", Model: "doubao-seedream-5-0-pro-260628",
		ProviderOutputWidth: 2361, ProviderOutputHeight: 1000,
		Width: 900, Height: 383,
	}
	svc.recordImageProviderCost(context.Background(), "task-image", result)
	event, err := fixture.costRepo.FindEventByIdempotency(context.Background(), "provider_cost_base/volcengine_ark", "seedream-original-output")
	if err != nil {
		t.Fatal(err)
	}
	if event.Status != model.BillingProviderCostStatusReconciled || event.CostMicroCNY != 600_000 {
		t.Fatalf("provider cost = status %q cost %d, want original-output tier 600000", event.Status, event.CostMicroCNY)
	}
}

func TestImageProviderCostCanonicalCapabilityIdentitiesReconcile(t *testing.T) {
	fixture := newProviderCostFixtureWithBundle(t, providerCostBundleWithMedia())
	svc := &ImageService{providerCostSvc: fixture.service}
	for _, tt := range []struct {
		name   string
		result *ImageResult
	}{
		{
			name: "standard",
			result: &ImageResult{
				ProviderRequestID:    "standard-image-cost",
				Provider:             "volcengine_ark",
				Model:                "doubao-seedream-5-0-pro-260628",
				ProviderOutputWidth:  2048,
				ProviderOutputHeight: 2048,
			},
		},
		{
			name: "professional",
			result: &ImageResult{
				ProviderRequestID: "professional-image-cost",
				Provider:          "wangcai_openai",
				Model:             "gpt-image-2-t",
				Usage: &appimage.ImageGenerationUsage{
					TextInputTokens:   10,
					ImageOutputTokens: 20,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc.recordImageProviderCost(context.Background(), "task-"+tt.name, tt.result)
			event, err := fixture.costRepo.FindEventByIdempotency(
				context.Background(),
				"provider_cost_base/"+tt.result.Provider,
				tt.result.ProviderRequestID,
			)
			if err != nil {
				t.Fatalf("find provider cost event: %v", err)
			}
			if event.Status != model.BillingProviderCostStatusReconciled || event.CostMicroCNY <= 0 {
				t.Fatalf("provider cost = status %q cost %d, want reconciled positive cost", event.Status, event.CostMicroCNY)
			}
		})
	}
}
