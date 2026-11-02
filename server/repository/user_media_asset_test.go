package repository

import (
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUserMediaAssetRepositoryScopesSearchAndRanksProjectMatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:user-media-repository?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := newUserMediaAssetRepository(db)
	for _, asset := range []*model.Asset{
		{ID: "asset-1", UserID: "user-1", Purpose: "media_library", StorageKey: "assets/asset-1", FileName: "front-packaging.png", ContentType: "image/png", Size: 1, ETag: "etag-1"},
		{ID: "asset-2", UserID: "user-1", Purpose: "media_library", StorageKey: "assets/asset-2", FileName: "back-packaging.png", ContentType: "image/png", Size: 1, ETag: "etag-2"},
		{ID: "asset-3", UserID: "user-2", Purpose: "media_library", StorageKey: "assets/asset-3", FileName: "foreign.png", ContentType: "image/png", Size: 1, ETag: "etag-3"},
		{ID: "asset-4", UserID: "user-1", Purpose: "media_library", StorageKey: "assets/asset-4", FileName: "demo.mp4", ContentType: "video/mp4", Size: 1, ETag: "etag-4"},
	} {
		if err := db.Create(asset).Error; err != nil {
			t.Fatalf("create asset %s: %v", asset.ID, err)
		}
	}
	rows := []*model.UserMediaAsset{
		{ID: "same-project", UserID: "user-1", AssetID: "asset-1", ProjectID: "project-1", MediaType: "image", Status: model.UserMediaAssetStatusAvailable, Description: "包装正面"},
		{ID: "other-project", UserID: "user-1", AssetID: "asset-2", ProjectID: "project-2", MediaType: "image", Status: model.UserMediaAssetStatusAvailable, Description: "包装背面"},
		{ID: "foreign", UserID: "user-2", AssetID: "asset-3", ProjectID: "project-1", MediaType: "image", Status: model.UserMediaAssetStatusAvailable, Description: "包装正面"},
		{ID: "queued", UserID: "user-1", AssetID: "asset-4", ProjectID: "project-1", MediaType: "video", Status: model.UserMediaAssetStatusQueued, Description: "包装演示"},
	}
	for _, row := range rows {
		if err := repo.Create(t.Context(), row); err != nil {
			t.Fatalf("create %s: %v", row.ID, err)
		}
	}
	got, err := repo.ListReadyCandidates(t.Context(), "user-1", "project-1", "包装", "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 2 || got[0].ID != "same-project" || got[1].ID != "other-project" {
		t.Fatalf("candidate ids = %q, %q", got[0].ID, got[1].ID)
	}
	if _, err := repo.FindByIDForUser(t.Context(), "user-2", "same-project", false); err == nil {
		t.Fatal("foreign user accessed media")
	}
	byName, _, err := repo.List(t.Context(), UserMediaAssetListFilter{UserID: "user-1", Query: "front-packaging", Limit: 10})
	if err != nil {
		t.Fatalf("filename search: %v", err)
	}
	if len(byName) != 1 || byName[0].ID != "same-project" {
		t.Fatalf("filename search ids = %#v", byName)
	}
}

func TestUserMediaAssetRepositoryFindsOnlyActiveOwnedAvailableMediaByStorageKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:user-media-storage-keys?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	repo := newUserMediaAssetRepository(db)
	assets := []*model.Asset{
		{ID: "owned", UserID: "user-1", Purpose: "media_library", StorageKey: "assets/users/user-1/owned.mp4", FileName: "owned.mp4", ContentType: "video/mp4", Size: 1, ETag: "1"},
		{ID: "foreign", UserID: "user-2", Purpose: "media_library", StorageKey: "assets/users/user-1/foreign.mp4", FileName: "foreign.mp4", ContentType: "video/mp4", Size: 1, ETag: "2"},
		{ID: "other-purpose", UserID: "user-1", Purpose: "article_image", StorageKey: "assets/users/user-1/image.mp4", FileName: "image.mp4", ContentType: "video/mp4", Size: 1, ETag: "3"},
	}
	for _, asset := range assets {
		if err := db.Create(asset).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows := []*model.UserMediaAsset{
		{ID: "owned-row", UserID: "user-1", AssetID: "owned", MediaType: "video", Status: model.UserMediaAssetStatusAvailable},
		{ID: "queued-row", UserID: "user-1", AssetID: "foreign", MediaType: "video", Status: model.UserMediaAssetStatusQueued},
		{ID: "removed-row", UserID: "user-1", AssetID: "other-purpose", MediaType: "video", Status: model.UserMediaAssetStatusAvailable, RemovedAt: func() *time.Time { value := time.Now(); return &value }()},
	}
	for _, row := range rows {
		if err := repo.Create(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.FindActiveByStorageKeysForUser(t.Context(), "user-1", []string{"assets/users/user-1/owned.mp4", "assets/users/user-1/foreign.mp4", "assets/users/user-1/image.mp4"}, "video")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "owned-row" {
		t.Fatalf("rows = %#v", got)
	}
}
