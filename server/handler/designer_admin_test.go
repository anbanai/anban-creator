package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDesignerHandlerRejectsNonAdminOnEveryEndpoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	user := &model.User{ID: "ordinary-user", Email: "ordinary@example.test", Password: "fixture", IsAdmin: false}
	if err := repo.Users().Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewDesignerHandler(service.NewDesignerService(db, nil, nil, &logger), &logger)
	h.SetDirectUploadDependencies(repo, nil)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", user.ID); return c.Next() })
	app.Post("/designer/quote", h.Quote)
	app.Post("/designer/generate", h.Generate)
	app.Post("/designer/register-reference", h.RegisterReference)
	app.Post("/designer/upload-reference", h.UploadReference)
	app.Post("/designer/upload-reference-from-url", h.UploadReferenceFromURL)
	app.Get("/designer/history", h.GetHistory)
	app.Get("/designer/generations/:id", h.GetGeneration)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, "/designer/quote"},
		{http.MethodPost, "/designer/generate"},
		{http.MethodPost, "/designer/register-reference"},
		{http.MethodPost, "/designer/upload-reference"},
		{http.MethodPost, "/designer/upload-reference-from-url"},
		{http.MethodGet, "/designer/history"},
		{http.MethodGet, "/designer/generations/missing"},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			req, err := http.NewRequest(endpoint.method, "http://example.test"+endpoint.path, strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
		})
	}
}

func TestDesignerURLReferenceReturnsPersistedMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	userID := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Email: userID + "@example.test", Password: "fixture", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := userID + "/designer/result.png"
	data := []byte("stored-image")
	if _, err := store.Upload(t.Context(), key, bytes.NewReader(data), "image/png"); err != nil {
		t.Fatal(err)
	}
	gen := model.ImageGeneration{ID: uuid.NewString(), UserID: userID, Prompt: "image", Provider: "openai", Model: "test", Status: model.ImageGenerationStatusCompleted, RequestFingerprint: "fixture"}
	if err := db.Create(&gen).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ImageGenerationResult{GenerationID: gen.ID, FileID: key, ImageURL: store.GetURL(key)}).Error; err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewDesignerHandler(service.NewDesignerService(db, nil, store, &logger), &logger)
	h.SetDirectUploadDependencies(repo, store)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", userID); return c.Next() })
	app.Post("/designer/upload-reference-from-url", h.UploadReferenceFromURL)
	req, err := http.NewRequest(http.MethodPost, "http://example.test/designer/upload-reference-from-url", strings.NewReader(`{"url":"`+store.GetURL(key)+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			FileID   string `json:"file_id"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.FileID == "" || body.Data.Filename != "source.png" || body.Data.Size != int64(len(data)) {
		t.Fatalf("reference response = %#v", body.Data)
	}
}
