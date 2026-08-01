package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/gofiber/fiber/v3"
)

func handlerCapability(alias, tier, sku string, order int) config.ImageGenerationRouteConfig {
	return config.ImageGenerationRouteConfig{
		Alias: alias, Description: alias + " description", MinTier: tier, BillingSKU: sku,
		Provider: "internal-provider", Model: "internal-model", BaseURL: "https://secret.example.com", APIKey: "secret",
		Enabled: true, SortOrder: order, QualityRank: order,
		Features: config.DesignerProviderCapabilities{DefaultSize: "1:1", SizePresets: []string{"1:1"}, MaxBatch: 1, OutputFormats: []string{"png"}},
	}
}

func TestImageCapabilityOptionPublicContractOmitsInternalRouteFields(t *testing.T) {
	payload, err := json.Marshal(ImageCapabilityOption{
		Key: "professional", DisplayName: "Professional", Description: "Detailed images", MinTier: "pro",
		PriceCredits: 500, PriceAvailable: true, Enabled: true, SortOrder: 20,
		Features: config.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(payload))
	for _, forbidden := range []string{"provider", "model", "base_url", "api_key", "billing_sku", "route"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public image capability payload contains %q: %s", forbidden, text)
		}
	}
}

func TestImageCapabilitiesListFiltersByTierAndReturnsDefault(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	user := &model.User{ID: "free-user", Email: "free@example.com", Password: "x", InviteCode: "free-code", Tier: model.TierFree}
	if err := repo.Users().Create(t.Context(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	routes := config.ImageGenerationRoutesConfig{DefaultCapability: "standard", Capabilities: map[string]config.ImageGenerationRouteConfig{
		"standard":     handlerCapability("Standard", "free", "image.standard", 10),
		"professional": handlerCapability("Professional", "pro", "image.professional", 20),
	}}
	standard := routes.Capabilities["standard"]
	standard.Features.MaxBatch = 10
	routes.Capabilities["standard"] = standard
	h := NewImageCapabilityHandler(routes, repo, nil, nil)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", user.ID); return c.Next() })
	app.Get("/api/v1/image-capabilities", h.List)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/image-capabilities", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Data struct {
			Items             []ImageCapabilityOption `json:"items"`
			Tier              string                  `json:"tier"`
			DefaultCapability string                  `json:"default_capability"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Tier != "free" || envelope.Data.DefaultCapability != "standard" {
		t.Fatalf("catalog metadata = %#v", envelope.Data)
	}
	if len(envelope.Data.Items) != 1 || envelope.Data.Items[0].Key != "standard" {
		t.Fatalf("free catalog items = %#v", envelope.Data.Items)
	}
	if envelope.Data.Items[0].Features.MaxBatch != 1 {
		t.Fatalf("public max_batch = %d, want 1", envelope.Data.Items[0].Features.MaxBatch)
	}
}

func TestValidateImageCapabilityKey(t *testing.T) {
	routes := map[string]config.ImageGenerationRouteConfig{
		"standard":     handlerCapability("Standard", "free", "image.standard", 10),
		"professional": handlerCapability("Professional", "pro", "image.professional", 20),
	}
	tests := []struct {
		name    string
		key     string
		tier    model.Tier
		wantErr bool
	}{
		{name: "default empty", key: "", tier: model.TierFree},
		{name: "free", key: "standard", tier: model.TierFree},
		{name: "pro denied", key: "professional", tier: model.TierFree, wantErr: true},
		{name: "pro allowed", key: "professional", tier: model.TierPro},
		{name: "unknown", key: "missing", tier: model.TierEnterprise, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateImageCapabilityKey(tt.key, tt.tier, routes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
