package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/service"
)

func setupDesignerHandlerTest() *fiber.App {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enabled := true
	designerSvc := service.NewDesignerService(nil, nil, nil, &srvconfig.ImageAPIConfig{
		Designer: map[string]*appconfig.ImageAPI{
			"test-openai": {
				Alias:    "Test OpenAI",
				Enable:   &enabled,
				Provider: "openai",
				Model:    "gpt-image-2",
				Credits:  10,
			},
		},
	}, nil, &logger)
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
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}
