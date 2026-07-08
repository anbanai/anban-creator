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
					},
				},
			},
		},
		ModelPrices: srvconfig.ModelPricesConfig{
			CurrencyRates: map[string]srvconfig.CurrencyRate{"USD": {ToCNY: 7.2}},
			ImageGeneration: map[string]srvconfig.ImageGenerationPrice{
				"wangcai_openai/gpt-image-2": {
					PricingType:      srvconfig.ImagePricingTypeOpenAIUsage,
					Currency:         "USD",
					Unit:             1_000_000,
					RequireUsage:     true,
					TextInput:        5,
					TextCachedInput:  1.25,
					ImageInput:       8,
					ImageCachedInput: 2,
					ImageOutput:      30,
					EstimateTable: map[string]map[string]srvconfig.FlexibleFloat{
						"1024x1024": {"medium": srvconfig.FlexibleFloat(0.053)},
					},
				},
			},
		},
		Billing: srvconfig.BillingConfig{CreditsPerCNY: 1000, MinimumChargeCredits: 1},
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
	designerSvc := service.NewDesignerService(nil, nil, nil, cfg, nil, &logger)
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
	if providers[0].ProviderKey != "wangcai_openai" || providers[0].Route != "image_generation.designer.test-openai" {
		t.Fatalf("provider route fields = %+v", providers[0])
	}
	if providers[0].Capabilities.MaxBatch != 10 || !providers[0].Capabilities.SupportsReference || len(providers[0].Capabilities.QualityLevels) == 0 {
		t.Fatalf("capabilities = %+v", providers[0].Capabilities)
	}
	if providers[0].Capabilities.DefaultSize != "auto" {
		t.Fatalf("default size = %q, want auto for GPT Image", providers[0].Capabilities.DefaultSize)
	}
	if len(providers[0].Capabilities.SizePresets) == 0 || providers[0].Capabilities.SizePresets[0] != "auto" {
		t.Fatalf("size presets = %+v, want auto first for GPT Image", providers[0].Capabilities.SizePresets)
	}
	if providers[0].Pricing.PricingType != srvconfig.ImagePricingTypeOpenAIUsage || !providers[0].Pricing.RequiresUsage {
		t.Fatalf("pricing = %+v", providers[0].Pricing)
	}
	if providers[0].Credits != 0 {
		t.Fatalf("legacy credits = %d, want 0 for dynamic GPT Image 2", providers[0].Credits)
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
