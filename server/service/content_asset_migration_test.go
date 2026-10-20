package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateTopicPoolToContentAssetsIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TopicPool{UserID: "user-1", ProjectID: "project-1", Topic: "旧选题", Status: model.TopicStatusUnused}).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateTopicPoolToContentAssets(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	if err := MigrateTopicPoolToContentAssets(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	var assets []model.ContentAsset
	if err := db.Find(&assets).Error; err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 {
		t.Fatalf("asset count = %d, want 1", len(assets))
	}
	var topic model.TopicPool
	if err := db.First(&topic).Error; err != nil {
		t.Fatal(err)
	}
	if topic.ContentAssetID == nil || *topic.ContentAssetID != assets[0].ID {
		t.Fatalf("topic asset link = %#v, want %s", topic.ContentAssetID, assets[0].ID)
	}
}
