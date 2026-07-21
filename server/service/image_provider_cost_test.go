package service

import (
	"context"
	"testing"

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
