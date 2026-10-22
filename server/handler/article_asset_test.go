package handler

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestArticleAssetServesOnlyPublishedArticleImagesWithoutAuthentication(t *testing.T) {
	ctx := t.Context()
	repo := repository.New(setupTaskHandlerTestDB(t))
	asset := &model.Asset{ID: "2e27c66f-dc57-4d56-adf5-6e4f3d642bda", UserID: "user-1", Purpose: model.AssetPurposeArticleImage, StorageKey: "article-assets/image.png", FileName: "image.png", ContentType: "image/png", Size: 3, ETag: "hash"}
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	store := &fakeStorageProvider{data: map[string][]byte{asset.StorageKey: []byte("png")}}
	logger := zerolog.New(io.Discard)
	h := NewArticleAssetHandler(repo, store, &logger)
	app := fiber.New()
	app.Get("/api/v1/article-assets/:id", h.Get)
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/article-assets/"+asset.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK || string(body) != "png" || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("Cache-Control") == "" {
		t.Fatalf("response = status %d, body %q, headers %v", resp.StatusCode, body, resp.Header)
	}
}

func TestArticleAssetDoesNotExposeOtherAssetPurpose(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	asset := &model.Asset{ID: "2e27c66f-dc57-4d56-adf5-6e4f3d642bda", UserID: "user-1", Purpose: "project_reference", StorageKey: "references/image.png", FileName: "image.png", ContentType: "image/png", Size: 3, ETag: "hash"}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	app := fiber.New()
	app.Get("/api/v1/article-assets/:id", NewArticleAssetHandler(repo, &fakeStorageProvider{data: map[string][]byte{asset.StorageKey: []byte("png")}}, &logger).Get)
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/article-assets/"+asset.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestArticleAssetDeleteRequiresOwnerAndRemovesObject(t *testing.T) {
	repo := repository.New(setupTaskHandlerTestDB(t))
	asset := &model.Asset{ID: "2e27c66f-dc57-4d56-adf5-6e4f3d642bda", UserID: "owner", Purpose: model.AssetPurposeArticleImage, StorageKey: "article-assets/image.png", FileName: "image.png", ContentType: "image/png", Size: 3, ETag: "hash"}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	store := &fakeStorageProvider{data: map[string][]byte{asset.StorageKey: []byte("png")}}
	logger := zerolog.New(io.Discard)
	h := NewArticleAssetHandler(repo, store, &logger)
	app := fiber.New()
	app.Delete("/api/v1/article-assets/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.Delete(c)
	})

	foreignReq := httptest.NewRequest("DELETE", "/api/v1/article-assets/"+asset.ID, nil)
	foreignReq.Header.Set("X-User-ID", "other")
	foreign, err := app.Test(foreignReq)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Body.Close()
	if foreign.StatusCode != fiber.StatusNotFound {
		t.Fatalf("foreign delete status = %d, want 404", foreign.StatusCode)
	}
	if _, err := repo.Assets().FindByID(t.Context(), asset.ID); err != nil {
		t.Fatalf("foreign delete removed asset: %v", err)
	}

	ownedReq := httptest.NewRequest("DELETE", "/api/v1/article-assets/"+asset.ID, nil)
	ownedReq.Header.Set("X-User-ID", "owner")
	owned, err := app.Test(ownedReq)
	if err != nil {
		t.Fatal(err)
	}
	owned.Body.Close()
	if owned.StatusCode != fiber.StatusNoContent {
		t.Fatalf("owner delete status = %d, want 204", owned.StatusCode)
	}
	if _, err := repo.Assets().FindByID(t.Context(), asset.ID); err == nil {
		t.Fatal("owner delete kept the asset record")
	}
	if _, ok := store.data[asset.StorageKey]; ok {
		t.Fatal("owner delete kept the stored image")
	}
}
