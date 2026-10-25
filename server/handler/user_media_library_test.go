package handler

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func TestUserMediaLibraryHandlerListRequiresIdentityAndScopesResults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	for _, userID := range []string{"user-1", "user-2"} {
		assetID := uuid.NewString()
		if err := repo.Assets().Create(t.Context(), &model.Asset{ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeMediaLibrary, StorageKey: userID + ".png", FileName: userID + ".png", ContentType: "image/png", Size: 1, ETag: userID}); err != nil {
			t.Fatal(err)
		}
		if err := repo.UserMediaAssets().Create(t.Context(), &model.UserMediaAsset{ID: userID + "-media", UserID: userID, AssetID: assetID, MediaType: "image", Status: model.UserMediaAssetStatusAvailable}); err != nil {
			t.Fatal(err)
		}
	}
	h := NewUserMediaLibraryHandler(service.NewUserMediaLibraryService(repo, nil, nil), nil)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if userID := c.Get("X-User-ID"); userID != "" {
			c.Locals("user_id", userID)
		}
		return c.Next()
	})
	app.Get("/media-assets", h.List)

	unauthorized := httptestRequest(t, app, http.MethodGet, "/media-assets", "", nil)
	if unauthorized.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}
	response := httptestRequest(t, app, http.MethodGet, "/media-assets", "user-1", nil)
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("list status = %d", response.StatusCode)
	}
	body := decodeBody(t, response)
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != "user-1-media" {
		t.Fatalf("scoped items = %#v", items)
	}
}

func httptestRequest(t *testing.T, app *fiber.App, method, path, userID string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
