package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreditPricingKeepsGPTImage2OutOfFixedImageCosts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	logger := zerolog.New(io.Discard)
	enabled := true
	cfg := &srvconfig.Config{
		Credits: srvconfig.CreditsConfig{TaskCosts: map[string]int{"article": 1000}},
		ModelRoutes: srvconfig.ModelRoutesConfig{
			ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
				Designer: map[string]srvconfig.ImageGenerationRouteConfig{
					"gpt_image_2": {Provider: "wangcai_openai", Model: "gpt-image-2", Enabled: true},
				},
			},
		},
		ModelPrices: srvconfig.ModelPricesConfig{
			CurrencyRates: map[string]srvconfig.CurrencyRate{"USD": {ToCNY: 7.2}},
			ImageGeneration: map[string]srvconfig.ImageGenerationPrice{
				"wangcai_openai/gpt-image-2": {
					PricingType:  srvconfig.ImagePricingTypeOpenAIUsage,
					Currency:     "USD",
					Unit:         1_000_000,
					RequireUsage: true,
					EstimateTable: map[string]map[string]srvconfig.FlexibleFloat{
						"1024x1024": {"medium": srvconfig.FlexibleFloat(0.053)},
					},
				},
			},
		},
		ImageAPI: srvconfig.ImageAPIConfig{
			Designer: map[string]*config.ImageAPI{
				"gpt_image_2": {Enable: &enabled, Provider: "openai", Model: "gpt-image-2", Credits: 999},
			},
		},
	}
	creditSvc := service.NewCreditService(repository.New(db), &cfg.Credits, &logger)
	handler := NewCreditHandler(creditSvc, cfg, "", &logger)
	app := fiber.New()
	app.Get("/credits/pricing", handler.Pricing)

	resp, err := app.Test(httptest.NewRequest("GET", "/credits/pricing", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, _ := json.Marshal(body.Data)
	var data struct {
		ModelCosts  map[string]map[string]int `json:"model_costs"`
		ModelPrices struct {
			ImageGeneration map[string]any `json:"image_generation"`
		} `json:"model_prices"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if got := data.ModelCosts["image_gen"]["openai/gpt-image-2"]; got != 0 {
		t.Fatalf("fixed GPT Image 2 image_gen cost = %d, want absent/zero", got)
	}
	if _, ok := data.ModelPrices.ImageGeneration["wangcai_openai/gpt-image-2"]; !ok {
		t.Fatalf("dynamic GPT Image 2 pricing missing from model_prices.image_generation: %#v", data.ModelPrices.ImageGeneration)
	}
}
