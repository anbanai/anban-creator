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
				Bindings struct {
					TaskTypes []string `json:"task_types"`
				} `json:"bindings"`
			} `json:"packs"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || len(envelope.Data.Packs) != 7 {
		t.Fatalf("catalog response = %#v", envelope)
	}
	if envelope.Data.Packs[0].ID != "article" {
		t.Fatalf("first Pack = %q, want deterministic article", envelope.Data.Packs[0].ID)
	}
	for _, pack := range envelope.Data.Packs {
		if pack.ID == "hypit" {
			if pack.Kind != "managed" || !slices.Equal(pack.Surfaces, []string{"plugin", "project", "task", "plan"}) || !slices.Equal(pack.Bindings.TaskTypes, []string{"hypit"}) {
				t.Fatalf("video replication must expose the managed Studio task route: %#v", pack)
			}
			return
		}
	}
	t.Fatal("catalog must expose the video replication Pack")
}
