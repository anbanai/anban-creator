package repository

import (
	"context"
	"errors"
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

func TestAnalyticsPublishRebuildCleansPreviousGenerationForProject(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsState{}, &model.AnalyticsBucket{}); err != nil {
		t.Fatal(err)
	}
	r := New(db).Analytics()
	ctx := context.Background()
	if err := db.Create(&model.AnalyticsState{ProjectID: "project-1", ActiveGeneration: 1, Status: "rebuilding"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsBucket{ProjectID: "project-1", Generation: 1, ContentID: "content-1", MetricBasis: "cumulative", Granularity: "day", BucketStart: "2026-09-01"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsBucket{ProjectID: "project-1", Generation: 2, ContentID: "content-1", MetricBasis: "cumulative", Granularity: "day", BucketStart: "2026-09-01"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsBucket{ProjectID: "project-2", Generation: 1, ContentID: "content-2", MetricBasis: "cumulative", Granularity: "day", BucketStart: "2026-09-01"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := r.PublishRebuild(ctx, "project-1", 2); err != nil {
		t.Fatal(err)
	}
	var old int64
	if err := db.Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ?", "project-1", 1).Count(&old).Error; err != nil {
		t.Fatal(err)
	}
	if old != 0 {
		t.Fatalf("old generation bucket count = %d, want 0", old)
	}
	var active int64
	if err := db.Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ?", "project-1", 2).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active generation bucket count = %d, want 1", active)
	}
	var other int64
	if err := db.Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ?", "project-2", 1).Count(&other).Error; err != nil {
		t.Fatal(err)
	}
	if other != 1 {
		t.Fatalf("other project bucket count = %d, want 1", other)
	}
}

func TestAnalyticsPublishRebuildRejectsMissingOrAlreadyPublishedGeneration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsState{}, &model.AnalyticsBucket{}); err != nil {
		t.Fatal(err)
	}
	r := New(db).Analytics()
	ctx := context.Background()
	if err := db.Create(&model.AnalyticsState{ProjectID: "project-1", ActiveGeneration: 2, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.PublishRebuild(ctx, "project-1", 3); !errors.Is(err, ErrAnalyticsRebuildSuperseded) {
		t.Fatalf("missing rebuilding state error = %v", err)
	}
}

func TestAnalyticsRebuildCannotModifyPublishedGeneration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsState{}, &model.AnalyticsBucket{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsState{ProjectID: "project-1", ActiveGeneration: 2, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsBucket{ProjectID: "project-1", Generation: 2, ContentID: "content-1", MetricBasis: "cumulative", Granularity: "day", BucketStart: "2026-09-01"}).Error; err != nil {
		t.Fatal(err)
	}

	err = New(db).Analytics().Rebuild(context.Background(), "project-1", 2)
	if !errors.Is(err, ErrAnalyticsRebuildSuperseded) {
		t.Fatalf("rebuild published generation error = %v, want ErrAnalyticsRebuildSuperseded", err)
	}
	var count int64
	if err := db.Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ?", "project-1", 2).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("published generation bucket count = %d, want 1", count)
	}
}
