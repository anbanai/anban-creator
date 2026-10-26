package repository

import (
	"testing"

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
