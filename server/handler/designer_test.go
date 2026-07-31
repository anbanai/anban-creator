package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/service"
)

func setupDesignerHandlerTest() *fiber.App {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enabled := true
	cfg := &srvconfig.Config{
		ModelRoutes: srvconfig.ModelRoutesConfig{
			ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
				Designer: map[string]srvconfig.ImageGenerationRouteConfig{
					"test-openai": {
						Alias:    "Test OpenAI",
						Enabled:  true,
						Provider: "wangcai_openai",
						Model:    "gpt-image-2",
						Capabilities: service.DesignerProviderCapabilities{
							QualityLevels:      []string{"auto", "low", "medium", "high"},
							SizePresets:        []string{"auto", "1024x1024", "1536x1024", "1024x1536"},
							DefaultSize:        "auto",
							MaxBatch:           10,
							MaxReferenceImages: 16,
							SupportsReference:  true,
							SupportsMask:       true,
							OutputFormats:      []string{"png", "jpeg", "webp"},
							HasBackground:      true,
							HasCompression:     true,
						},
					},
				},
			},
		},
		ImageAPI: srvconfig.ImageAPIConfig{
			Designer: map[string]*appconfig.ImageAPI{
				"test-openai": {
					Alias:    "Test OpenAI",
					Enable:   &enabled,
					Provider: "wangcai_openai",
					Model:    "gpt-image-2",
				},
			},
		},
	}
	designerSvc := service.NewDesignerService(nil, cfg, nil, &logger)
	handler := NewDesignerHandler(designerSvc, &logger)

	app := fiber.New()
	app.Get("/designer/providers", handler.GetProviders)
	return app
}

func TestDesignerProvidersUsesStandardResponseEnvelope(t *testing.T) {
	app := setupDesignerHandlerTest()

	resp, err := app.Test(httptest.NewRequest("GET", "/designer/providers", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	defer resp.Body.Close()

	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := rawBody["code"]; !ok {
		t.Fatalf("response missing standard code field: %s", string(mustMarshalJSON(t, rawBody)))
	}
	if _, ok := rawBody["msg"]; !ok {
		t.Fatalf("response missing standard msg field: %s", string(mustMarshalJSON(t, rawBody)))
	}
	var body Response
	if err := json.Unmarshal(mustMarshalJSON(t, rawBody), &body); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("code = %d, want 0", body.Code)
	}
	if body.Msg != "success" {
		t.Fatalf("msg = %q, want success", body.Msg)
	}
	raw, err := json.Marshal(body.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var providers []service.DesignerProviderInfo
	if err := json.Unmarshal(raw, &providers); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if len(providers) != 1 || providers[0].ID != "test-openai" {
		t.Fatalf("providers = %+v", providers)
	}
	if providers[0].Idx != 0 {
		t.Fatalf("providers[0].Idx = %d, want 0", providers[0].Idx)
	}
	if providers[0].ProviderKey != "" || providers[0].Route != "" || providers[0].Provider != "" || providers[0].Model != "" {
		t.Fatalf("provider identity leaked into public response = %+v", providers[0])
	}
	if providers[0].Capabilities.MaxBatch != 1 || !providers[0].Capabilities.SupportsReference || len(providers[0].Capabilities.QualityLevels) == 0 {
		t.Fatalf("capabilities = %+v", providers[0].Capabilities)
	}
	if providers[0].Capabilities.DefaultSize != "auto" {
		t.Fatalf("default size = %q, want auto for GPT Image", providers[0].Capabilities.DefaultSize)
	}
	if len(providers[0].Capabilities.SizePresets) == 0 || providers[0].Capabilities.SizePresets[0] != "auto" {
		t.Fatalf("size presets = %+v, want auto first for GPT Image", providers[0].Capabilities.SizePresets)
	}
	if providers[0].Pricing.PricingType != "fixed_sku" || providers[0].Pricing.Currency != "credits" || providers[0].Pricing.BillingNote != "fixed retail SKU" {
		t.Fatalf("pricing = %+v", providers[0].Pricing)
	}
	if providers[0].Credits != 0 {
		t.Fatalf("credits = %d, want 0 without an injected retail catalog", providers[0].Credits)
	}
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}
