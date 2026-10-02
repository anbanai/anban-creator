package handler

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAgentPackHandlerListsEmbeddedCatalog(t *testing.T) {
	app := fiber.New()
	h := NewAgentPackHandler()
	app.Get("/agent-packs", h.List)

	resp, err := app.Test(httptest.NewRequest("GET", "/agent-packs", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Packs []struct {
				ID       string   `json:"id"`
				Kind     string   `json:"kind"`
				Surfaces []string `json:"surfaces"`
				Channel  string   `json:"channel"`
				Bindings struct {
					TaskKinds []string `json:"task_kinds"`
				} `json:"bindings"`
			} `json:"packs"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || len(envelope.Data.Packs) != 10 {
		t.Fatalf("catalog response = %#v", envelope)
	}
	if envelope.Data.Packs[0].ID != "ecommerce" {
		t.Fatalf("first Pack = %q, want deterministic ecommerce", envelope.Data.Packs[0].ID)
	}
	for _, pack := range envelope.Data.Packs {
		if pack.ID == "hypit" {
			if pack.Kind != "plugin" || !slices.Equal(pack.Surfaces, []string{"plugin"}) || pack.Channel != "" {
				t.Fatalf("plugin-only Pack exposed as a channel Agent: %#v", pack)
			}
		}
	}
	for _, id := range []string{"wechat-article", "seednote", "wechat-picture"} {
		found := false
		for _, pack := range envelope.Data.Packs {
			if pack.ID != id {
				continue
			}
			found = pack.Kind == "managed" && pack.Channel == id && len(pack.Bindings.TaskKinds) > 0
			break
		}
		if !found {
			t.Fatalf("channel Agent %q not found with a single matching channel", id)
		}
	}
}
