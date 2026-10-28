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
	if envelope.Code != 0 || len(envelope.Data.Packs) != 18 {
		t.Fatalf("catalog response = %#v", envelope)
	}
	if envelope.Data.Packs[0].ID != "ecommerce" {
		t.Fatalf("first Pack = %q, want deterministic ecommerce", envelope.Data.Packs[0].ID)
	}
	for _, pack := range envelope.Data.Packs {
		if pack.ID == "video-edit" {
			if pack.Kind != "managed" || !slices.Equal(pack.Surfaces, []string{"plugin", "task"}) || pack.Channel != "video-edit" || !slices.Equal(pack.Bindings.TaskKinds, []string{"video_edit"}) {
				t.Fatalf("managed video-edit Pack has an incomplete task contract: %#v", pack)
			}
		}
		if pack.ID == "hypit" {
			if pack.Kind != "managed" || !slices.Equal(pack.Surfaces, []string{"plugin", "task"}) || pack.Channel != "hypit" || !slices.Equal(pack.Bindings.TaskKinds, []string{"hypit"}) {
				t.Fatalf("managed video Pack has an incomplete task contract: %#v", pack)
			}
		}
		if pack.ID == "viral-ip-video-editor" {
			if pack.Kind != "plugin" || !slices.Equal(pack.Surfaces, []string{"plugin"}) || pack.Channel != "" || len(pack.Bindings.TaskKinds) != 0 {
				t.Fatalf("viral-ip-video-editor Pack has an incomplete plugin contract: %#v", pack)
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
	var interactive struct {
		ID       string
		Kind     string
		Surfaces []string
		Channel  string
		Bindings struct {
			TaskKinds []string
		}
	}
	for _, pack := range envelope.Data.Packs {
		if pack.ID == "project-creator" {
			interactive.ID = pack.ID
			interactive.Kind = pack.Kind
			interactive.Surfaces = pack.Surfaces
			interactive.Channel = pack.Channel
			interactive.Bindings.TaskKinds = pack.Bindings.TaskKinds
			break
		}
	}
	if interactive.ID == "" || interactive.Kind != "managed" || !slices.Equal(interactive.Surfaces, []string{"plugin"}) || interactive.Channel != "" || len(interactive.Bindings.TaskKinds) != 0 {
		t.Fatalf("interactive Project Creator Pack has an invalid routing contract: %#v", interactive)
	}
}
