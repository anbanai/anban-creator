package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This is the persisted TopicPool schema before topic-radar introduced dedupe.
// Keep it separate from TopicPool so the fixture exercises an actual upgrade.
type legacyTopicPoolRow struct {
	ID             uint    `gorm:"primaryKey;autoIncrement"`
	UserID         string  `gorm:"type:char(36);index;not null"`
	ProjectID      string  `gorm:"type:char(36);index;not null"`
	Topic          string  `gorm:"type:varchar(500);not null"`
	Status         string  `gorm:"type:varchar(20);default:'unused'"`
	TaskID         *string `gorm:"type:char(36)"`
	ContentAssetID *string `gorm:"type:char(36);index"`
	UsedAt         *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (legacyTopicPoolRow) TableName() string { return "topic_pool" }

func TestAutoMigrateTopicPoolPreservesLegacyRowsAndBackfillsCanonicalDedupe(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyTopicPoolRow{}); err != nil {
		t.Fatal(err)
	}
	taskID, assetID := "old-task", "old-asset"
	usedAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	original := []legacyTopicPoolRow{
		{ID: 1, UserID: "user-1", ProjectID: "project-1", Topic: "Launch Idea", Status: TopicStatusUsed, TaskID: &taskID, ContentAssetID: &assetID, UsedAt: &usedAt},
		{ID: 2, UserID: "user-1", ProjectID: "project-1", Topic: " launch   IDEA ", Status: TopicStatusUnused},
		{ID: 3, UserID: "user-1", ProjectID: "project-1", Topic: "独立选题", Status: TopicStatusUnused},
		{ID: 4, UserID: "user-2", ProjectID: "project-2", Topic: "Launch Idea", Status: TopicStatusUnused},
	}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}

	for pass := 1; pass <= 2; pass++ {
		if err := AutoMigrate(db); err != nil {
			t.Fatalf("upgrade pass %d: %v", pass, err)
		}
		var rows []TopicPool
		if err := db.Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != len(original) {
			t.Fatalf("history rows = %d, want %d", len(rows), len(original))
		}
		for i, row := range rows {
			want := original[i]
			if row.ID != want.ID || row.UserID != want.UserID || row.ProjectID != want.ProjectID || row.Topic != want.Topic || row.Status != want.Status || !row.CreatedAt.Equal(want.CreatedAt) || !row.UpdatedAt.Equal(want.UpdatedAt) {
				t.Fatalf("legacy history changed: got %+v, want %+v", row, want)
			}
		}
		first := rows[0]
		if first.TaskID == nil || *first.TaskID != taskID || first.ContentAssetID == nil || *first.ContentAssetID != assetID || first.UsedAt == nil || !first.UsedAt.Equal(usedAt) {
			t.Fatalf("legacy references changed: %+v", first)
		}
		var canonical TopicPool
		if err := db.Where("project_id = ? AND dedupe_key = ?", "project-1", "launch idea").First(&canonical).Error; err != nil {
			t.Fatal(err)
		}
		if canonical.ID != 1 {
			t.Fatalf("canonical topic ID = %d, want 1", canonical.ID)
		}
		if rows[2].DedupeKey != "独立选题" || rows[3].DedupeKey != "launch idea" {
			t.Fatalf("unique/project-scoped history was not backfilled: %+v", rows)
		}
	}

	duplicate := TopicPool{UserID: "user-1", ProjectID: "project-1", Topic: "LAUNCH  IDEA"}
	result := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "project_id"}, {Name: "dedupe_key"}}, DoNothing: true}).Create(&duplicate)
	if result.Error != nil || result.RowsAffected != 0 {
		t.Fatalf("new duplicate bypassed legacy canonical row: affected=%d err=%v", result.RowsAffected, result.Error)
	}
	var legacyDuplicate TopicPool
	if err := db.First(&legacyDuplicate, 2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&legacyDuplicate).Updates(map[string]any{"status": TopicStatusUsed, "task_id": "new-task"}).Error; err != nil {
		t.Fatalf("legacy duplicate cannot retain its lifecycle: %v", err)
	}
}

func TestAutoMigrateTopicPoolResumesInterruptedBackfill(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyTopicPoolRow{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]legacyTopicPoolRow{
		{ID: 1, UserID: "user", ProjectID: "project", Topic: "First"},
		{ID: 2, UserID: "user", ProjectID: "project", Topic: " FIRST "},
		{ID: 3, UserID: "user", ProjectID: "project", Topic: "Second"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE topic_pool ADD COLUMN dedupe_key varchar(500) NOT NULL DEFAULT ''").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("topic_pool").Where("id = 1").UpdateColumn("dedupe_key", "first").Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("resume interrupted migration: %v", err)
	}
	var rows []TopicPool
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].DedupeKey != "first" || rows[2].DedupeKey != "second" {
		t.Fatalf("partial backfill did not resume: %+v", rows)
	}
	var duplicates int64
	if err := db.Table("topic_pool").Where("id = 2 AND dedupe_key IS NULL").Count(&duplicates).Error; err != nil || duplicates != 1 {
		t.Fatalf("legacy duplicate was not preserved: count=%d err=%v", duplicates, err)
	}
}
