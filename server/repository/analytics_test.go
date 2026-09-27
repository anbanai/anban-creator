package repository

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAnalyticsResolveContentTaskAliasThroughWechatPublication(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsContent{}, &model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.WechatPublication{ID: "pub-1", TaskID: "task-1", UserID: "user-1", ProjectID: "project-1", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsContent{
		ID:            "wechat_publication:pub-1",
		ProjectID:     "project-1",
		Platform:      model.PlatformArticle,
		PublicationID: "pub-1",
	}).Error; err != nil {
		t.Fatal(err)
	}

	content, err := New(db).Analytics().ResolveContent(context.Background(), "project-1", "task:task-1")
	if err != nil {
		t.Fatal(err)
	}
	if content.ID != "wechat_publication:pub-1" {
		t.Fatalf("content id = %q", content.ID)
	}
}
