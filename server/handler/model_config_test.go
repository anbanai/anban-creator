package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func setupModelConfigHandlerTest(t *testing.T) *fiber.App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.UserModelConfig{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	cfg := &srvconfig.Config{
		ImageAPI: srvconfig.ImageAPIConfig{
			Cover:   &appconfig.ImageAPI{},
			Content: &appconfig.ImageAPI{},
		},
	}
	svc := service.NewModelConfigService(repo, cfg, &logger)
	h := NewModelConfigHandler(svc, &logger)
	app := fiber.New()
	app.Put("/model-config", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Update(c)
	})
	return app
}

func TestModelConfigUpdateRejectsInvalidImageProvider(t *testing.T) {
	app := setupModelConfigHandlerTest(t)

	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader(`{
		"image": {
			"provider": "chat-compatible",
			"endpoint": "https://custom.example/v1",
			"api_key": "key",
			"model": "model"
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestModelConfigUpdateRejectsIncompleteImageProvider(t *testing.T) {
	app := setupModelConfigHandlerTest(t)

	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader(`{
		"image": {
			"provider": "openai",
			"endpoint": "https://custom.example/v1",
			"model": "gpt-image-2"
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestModelConfigUpdateRejectsImageKeepExistingKeyWithoutExistingKey(t *testing.T) {
	app := setupModelConfigHandlerTest(t)

	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader(`{
		"image": {
			"provider": "openai",
			"endpoint": "https://custom.example/v1",
			"api_key": "****",
			"model": "gpt-image-2"
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestModelConfigUpdateAcceptsSupportedImageProviders(t *testing.T) {
	for _, provider := range []string{"openai", "gemini", "volcengine"} {
		t.Run(provider, func(t *testing.T) {
			app := setupModelConfigHandlerTest(t)
			body := `{
				"image": {
					"provider": "` + provider + `",
					"endpoint": "https://custom.example/v1",
					"api_key": "key",
					"model": "model"
				}
			}`
			req := httptest.NewRequest("PUT", "/model-config", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
			}
		})
	}
}
