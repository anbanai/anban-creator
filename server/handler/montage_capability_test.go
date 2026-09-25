package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

func TestMontageCapabilityHandlerListUsesVideoGenerationNameWhenUnavailable(t *testing.T) {
	app := fiber.New()
	app.Get("/api/v1/montage-capabilities", NewMontageCapabilityHandler(nil).List)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/montage-capabilities", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusServiceUnavailable)
	}
	var envelope struct {
		Msg string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Msg != "视频生成能力不可用" {
		t.Fatalf("message = %q, want 视频生成能力不可用", envelope.Msg)
	}
}

func TestMontageCapabilityHandlerListReturnsPublicCatalog(t *testing.T) {
	svc := service.NewMontageCapabilityService(config.MontageConfig{
		Enabled: true, DefaultPipeline: "cinematic",
		AllowedPipelines: []string{"cinematic", "clip-factory"}, MaxDurationSeconds: 600, MaxAssets: 20,
		Env: map[string]string{"OPENAI_API_KEY": "secret"},
	})
	app := fiber.New()
	app.Get("/api/v1/montage-capabilities", NewMontageCapabilityHandler(svc).List)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/montage-capabilities", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Data service.MontageCapabilityCatalog `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.Enabled || envelope.Data.DefaultPipeline != "cinematic" || len(envelope.Data.Items) != 2 {
		t.Fatalf("catalog = %#v", envelope.Data)
	}
	payload, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(payload))
	for _, forbidden := range []string{"provider", "api_key", "tool_policy", "pipeline_defaults", "budget"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public montage catalog contains %q: %s", forbidden, text)
		}
	}
}
