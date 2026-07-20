package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAutoMigrateCreatesVideoGenerationSegments(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	segment := &VideoGenerationSegment{
		ID:                uuid.NewString(),
		VideoGenerationID: uuid.NewString(),
		TaskID:            uuid.NewString(),
		Index:             1,
		Status:            "submitted",
		Duration:          15,
	}
	if err := db.Create(segment).Error; err != nil {
		t.Fatalf("create segment: %v", err)
	}
	var count int64
	if err := db.Model(&VideoGenerationSegment{}).Where("video_generation_id = ?", segment.VideoGenerationID).Count(&count).Error; err != nil {
		t.Fatalf("count segments: %v", err)
	}
	if count != 1 {
		t.Fatalf("segments = %d, want 1", count)
	}
}
