package handler

import (
	"encoding/json"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHypitCapabilitiesDisabledAndNoSecrets(t *testing.T) {
	svc := service.NewHypitCapabilityService(config.HypitConfig{Env: map[string]string{"PROVIDER_API_KEY": "must-never-leak"}})
	app := fiber.New()
	app.Get("/api/v1/hypit-capabilities", NewHypitCapabilityHandler(svc).List)
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/hypit-capabilities", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Data service.HypitCapabilities `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Enabled || envelope.Data.Configured || envelope.Data.Limits.MaxProjectBytes != 2<<30 {
		t.Fatalf("bad capabilities %+v", envelope.Data)
	}
	b, _ := json.Marshal(envelope)
	if strings.Contains(string(b), "must-never-leak") {
		t.Fatal("secret leaked")
	}
}

func TestHypitSourceCapabilitiesHTTPAuthorizesOwner(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	cfg := config.HypitConfig{Enabled: true, RuntimeProfile: map[string]any{"format": "hypit.runtime-local@1", "endpoints": map[string]any{"local": map[string]any{"use": "@hypit/provider-media-local"}}}}
	cfg.ApplyDefaults()
	cfg.Limits.MaxDurationSeconds = 60
	logger := zerolog.Nop()
	tasks := newHandlerTaskService(t, repo, nil, nil, &logger, "", nil, nil)
	tasks.SetHypitConfig(cfg)
	source := &model.Task{ID: "source", UserID: "owner", Type: model.PlatformHypit, HypitRuntimeSnapshot: datatypes.JSON(`{"profile":{"format":"hypit.runtime-local@1","endpoints":{"local":{"use":"@hypit/provider-media-local"}}},"limits":{"max_duration_seconds":180,"timeout_minutes":7}}`)}
	if err := repo.Tasks().Create(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	h := NewHypitCapabilityHandler(service.NewHypitCapabilityService(cfg))
	h.SetTaskService(tasks)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-Test-User")); return c.Next() })
	app.Get("/api/v1/hypit-capabilities", h.List)
	for _, tc := range []struct {
		user   string
		status int
	}{{"owner", 200}, {"foreign", 403}} {
		req := httptest.NewRequest("GET", "/api/v1/hypit-capabilities?source_task_id=source", nil)
		req.Header.Set("X-Test-User", tc.user)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("%s status=%d", tc.user, resp.StatusCode)
		}
		if tc.status == 200 {
			var envelope struct {
				Data service.HypitCapabilities `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Data.Limits.MaxDurationSeconds != 180 || envelope.Data.Limits.TimeoutMinutes != 7 {
				t.Fatalf("current limits returned: %+v", envelope.Data)
			}
		}
	}
}
