package repository

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTopicPoolAddWithSourceIsIdempotentByProjectDedupeKey(t *testing.T) {
	db := setupTestDB(t)
	repo := newTopicPoolRepository(db)
	projectID := "project-radar"
	first := &model.TopicPool{UserID: "user-1", ProjectID: projectID, Topic: "低成本 直播 选题", DedupeKey: model.NormalizeTopicDedupeKey("低成本 直播 选题"), SourceLabel: "选题雷达"}
	second := &model.TopicPool{UserID: "user-1", ProjectID: projectID, Topic: "  低成本 直播 选题 ", DedupeKey: model.NormalizeTopicDedupeKey("  低成本 直播 选题 "), SourceLabel: "选题雷达"}
	if created, err := repo.AddWithSource(context.Background(), first); err != nil || !created {
		t.Fatalf("first insertion: created = %t, error = %v", created, err)
	}
	if created, err := repo.AddWithSource(context.Background(), second); err != nil || created {
		t.Fatalf("duplicate insertion: created = %t, error = %v", created, err)
	}
	var count int64
	if err := db.Model(&model.TopicPool{}).Where("project_id = ?", projectID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("topic count = %d, want 1", count)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate should link existing id, got %d and %d", second.ID, first.ID)
	}
}
