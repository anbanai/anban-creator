package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUserMediaAssetAutoMigrateStoresIndependentLibraryMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:user-media-model?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&UserMediaAsset{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	row := &UserMediaAsset{ID: "media-1", UserID: "user-1", AssetID: "asset-1", MediaType: "video", Status: UserMediaAssetStatusQueued, RecommendedUses: []string{"产品演示"}, UsageNotes: "优先保留开场"}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got UserMediaAsset
	if err := db.First(&got, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Status != UserMediaAssetStatusQueued || len(got.RecommendedUses) != 1 || got.UsageNotes != row.UsageNotes {
		t.Fatalf("round trip = %#v", got)
	}
}
