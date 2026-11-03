package handler

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

func TestServeFilePersistedDesignerResultOwnership(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	if err := db.AutoMigrate(&model.ImageGeneration{}, &model.ImageGenerationResult{}); err != nil {
		t.Fatal(err)
	}
	key := "user-1/designer/generation-1/0.png"
	if err := db.Create(&model.ImageGeneration{ID: "generation-1", UserID: "user-1", Status: model.ImageGenerationStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ImageGenerationResult{GenerationID: "generation-1", FileID: key}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ImageGeneration{ID: "unfinished", UserID: "user-1", Status: model.ImageGenerationStatusGenerating}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ImageGenerationResult{GenerationID: "unfinished", FileID: "user-1/designer/unfinished/0.png"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, user, key string
		status          int
	}{
		{"owned persisted result", "user-1", key, 200},
		{"foreign persisted result", "user-2", key, 403},
		{"unrecorded own namespace", "user-1", "user-1/designer/unrecorded/0.png", 403},
		{"unauthenticated", "", key, 401},
		{"unfinished result", "user-1", "user-1/designer/unfinished/0.png", 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			store := &fakeStorageProvider{data: map[string][]byte{key: []byte("designer-png")}}
			h := NewFileHandler(store, &logger)
			h.SetDesignerService(service.NewDesignerService(db, &config.Config{}, store, &logger))
			app := fiber.New()
			app.Use(func(c fiber.Ctx) error { c.Locals("user_id", test.user); return c.Next() })
			app.Get("/files/*", h.ServeFile)
			response, err := app.Test(httptest.NewRequest("GET", "/files/"+test.key, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.status {
				t.Fatalf("status=%d want=%d body=%s", response.StatusCode, test.status, body)
			}
			if test.status == 200 && string(body) != "designer-png" {
				t.Fatalf("body=%q", body)
			}
			if test.status != 200 && len(store.read) != 0 {
				t.Fatalf("unauthorized storage read: %v", store.read)
			}
		})
	}
}
