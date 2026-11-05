package router

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestArticleAssetRoutesAllowSeparateStudioOrigin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := repository.New(db)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	asset := &model.Asset{ID: uuid.NewString(), UserID: "owner", Purpose: model.AssetPurposeArticleImage, StorageKey: "article-assets/image.png", ContentType: "image/png"}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upload(t.Context(), asset.StorageKey, strings.NewReader("png"), asset.ContentType); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	const studioOrigin = "https://creator.anbanai.com"
	app := NewRouter(&Services{Config: &config.Config{CORS: config.CORSConfig{AllowedOrigins: []string{studioOrigin}}}, Logger: &logger, Repo: repo, StorageProvider: store})
	for _, tc := range []struct {
		method string
		status int
	}{{"GET", 200}, {"DELETE", 401}, {"OPTIONS", 204}} {
		t.Run(tc.method, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/api/v1/article-assets/"+asset.ID, nil)
			request.Header.Set("Origin", studioOrigin)
			if tc.method == "OPTIONS" {
				request.Header.Set("Access-Control-Request-Method", "DELETE")
				request.Header.Set("Access-Control-Request-Headers", "authorization")
			}
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != tc.status || response.Header.Get("Access-Control-Allow-Origin") != studioOrigin {
				t.Fatalf("%s: status %d, headers %v", tc.method, response.StatusCode, response.Header)
			}
			if tc.method == "GET" && response.Header.Get("Cross-Origin-Resource-Policy") == "same-origin" {
				t.Fatal("published article images must remain embeddable outside the API origin")
			}
		})
	}
}
