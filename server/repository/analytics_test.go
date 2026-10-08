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
		Platform:      model.PlatformWechat,
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

func TestAnalyticsAggregatesByWechatContentType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsContent{}, &model.AnalyticsBucket{}); err != nil {
		t.Fatal(err)
	}
	one, five := int64(3), int64(5)
	contents := []model.AnalyticsContent{
		{ID: "article", ProjectID: "project-1", Channel: model.ChannelArticle, ContentType: "wechat-article"},
		{ID: "picture", ProjectID: "project-1", Channel: model.ChannelWechatPicture, ContentType: "wechat-picture"},
	}
	if err := db.Create(&contents).Error; err != nil {
		t.Fatal(err)
	}
	buckets := []model.AnalyticsBucket{
		{ProjectID: "project-1", Generation: 1, ContentID: "article", MetricBasis: "cumulative", Granularity: "month", BucketStart: "2026-02-01", LastStatDate: "2026-02-28", AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &one}},
		{ProjectID: "project-1", Generation: 1, ContentID: "picture", MetricBasis: "cumulative", Granularity: "month", BucketStart: "2026-02-01", LastStatDate: "2026-02-28", AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &five}},
		{ProjectID: "project-1", Generation: 1, ContentID: "article", MetricBasis: "cumulative", Granularity: "day", BucketStart: "2026-02-10", LastStatDate: "2026-02-10", AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &one}},
		{ProjectID: "project-1", Generation: 1, ContentID: "picture", MetricBasis: "cumulative", Granularity: "day", BucketStart: "2026-02-10", LastStatDate: "2026-02-10", AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &five}},
	}
	if err := db.Create(&buckets).Error; err != nil {
		t.Fatal(err)
	}
	r := New(db).Analytics().WithMetricFamily(model.AnalyticsMetricsWechat)
	for _, tc := range []struct {
		contentType string
		want        int64
	}{
		{"wechat-article", 3},
		{"wechat-picture", 5},
	} {
		t.Run(tc.contentType, func(t *testing.T) {
			metrics, coverage, err := r.RangeTotals(context.Background(), "project-1", 1, "cumulative", "2026-02-10", "2026-02-20", "", tc.contentType, false)
			if err != nil || metrics.CommentCount == nil || *metrics.CommentCount != tc.want || coverage != 1 {
				t.Fatalf("range totals = %+v, coverage %d, error %v", metrics, coverage, err)
			}
			series, err := r.Series(context.Background(), "project-1", 1, "cumulative", "2026-02-10", "2026-02-20", "month", "", tc.contentType)
			if err != nil || len(series) != 1 || series[0].CommentCount == nil || *series[0].CommentCount != tc.want {
				t.Fatalf("series = %+v, error %v", series, err)
			}
		})
	}
	mismatched, err := r.Series(context.Background(), "project-1", 1, "cumulative", "2026-02-10", "2026-02-20", "month", "article", "wechat-picture")
	if err != nil || len(mismatched) != 0 {
		t.Fatalf("mismatched detail series = %+v, error %v", mismatched, err)
	}
}

func TestAnalyticsContentMetadataPrefersPictureTaskOverLegacyPublicationType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsContent{}, &model.AnalyticsBucket{}, &model.Task{}, &model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	count := int64(5)
	if err := db.Create(&model.Task{ID: "picture-task", UserID: "user-1", ProjectID: "project-1", Channel: model.ChannelWechatPicture, Type: model.TaskTypeWechatPicture, Title: "贴图任务"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.WechatPublication{ID: "picture-publication", TaskID: "picture-task", UserID: "user-1", ProjectID: "project-1", DraftArticleType: "news", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsContent{ID: "wechat_publication:picture-publication", ProjectID: "project-1", Platform: model.PlatformWechat, ContentType: "", TaskID: "picture-task", PublicationID: "picture-publication"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsBucket{ProjectID: "project-1", Generation: 1, ContentID: "wechat_publication:picture-publication", MetricBasis: "cumulative", Granularity: "month", BucketStart: "2026-02-01", LastStatDate: "2026-02-28", AnalyticsMetrics: model.AnalyticsMetrics{CommentCount: &count}}).Error; err != nil {
		t.Fatal(err)
	}

	r := New(db).Analytics().WithMetricFamily(model.AnalyticsMetricsWechat)
	metrics, coverage, err := r.RangeTotals(context.Background(), "project-1", 1, "cumulative", "2026-02-01", "2026-02-28", "", model.TaskTypeWechatPicture, false)
	if err != nil || metrics.CommentCount == nil || *metrics.CommentCount != count || coverage != 1 {
		t.Fatalf("legacy picture range totals = %+v, coverage %d, error %v", metrics, coverage, err)
	}
	rows, total, err := r.Contents(context.Background(), "project-1", 1, "cumulative", "2026-02-01", "2026-02-28", AnalyticsContentFilter{ContentType: model.TaskTypeWechatPicture, Limit: 25})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != "wechat_publication:picture-publication" {
		t.Fatalf("legacy picture content rows = %+v, total %d, error %v", rows, total, err)
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
