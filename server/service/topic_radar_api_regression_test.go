package service

import (
	"context"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestTopicSubscriptionPatchPreservesConcurrentSchedulerProgress(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	sub := seedTopicRadarSubscription(t, repo)
	if err := db.Model(sub).Update("source_config", `{}`).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	advanced := now.Add(24 * time.Hour)
	fired := false
	if err := db.Callback().Query().After("gorm:query").Register("test:advance-schedule", func(tx *gorm.DB) {
		if !fired && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "TopicSubscription" {
			fired = true
			if err := db.Model(&model.TopicSubscription{}).Where("id = ?", sub.ID).Updates(map[string]any{"next_run_at": advanced, "last_run_at": now}).Error; err != nil {
				t.Error(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicRadarService(repo, nil, func() time.Time { return now })
	name := "renamed"
	if _, err := svc.UpdateSubscription(ctx, sub.UserID, sub.ID, UpdateTopicSubscriptionParams{Name: &name}); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.TopicSubscriptions().FindByID(ctx, sub.ID)
	if err != nil || saved.Name != name || saved.NextRunAt == nil || !saved.NextRunAt.Equal(advanced) || saved.LastRunAt == nil || !saved.LastRunAt.Equal(now) {
		t.Fatalf("settings patch clobbered scheduler progress: %+v %v", saved, err)
	}
}
