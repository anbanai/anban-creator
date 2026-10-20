package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

func TestLocalLifeCapabilityHandlerReturnsTypes(t *testing.T) {
	app := fiber.New()
	app.Get("/api/v1/local-life-capabilities", NewLocalLifeCapabilityHandler(service.NewLocalLifeCapabilityService()).List)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/local-life-capabilities", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var envelope struct {
		Data service.LocalLifeCapabilityCatalog `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Types) != 6 {
		t.Fatalf("types = %#v", envelope.Data.Types)
	}
	if envelope.Data.Enabled {
		t.Fatal("default capability catalog must be disabled")
	}
}
