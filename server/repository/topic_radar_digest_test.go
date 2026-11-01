package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestTopicDigestCreateIfAbsentReportsSilentUniqueConflicts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TopicDigest{}); err != nil {
		t.Fatal(err)
	}
	repo := newTopicDigestRepository(db)
	ctx := context.Background()
	activeKey := "subscription-1"
	base := func(id, occurrence, request string, active *string) *model.TopicDigest {
		return &model.TopicDigest{
			ID: id, SubscriptionID: "subscription-1", UserID: "user-1", ProjectID: "project-1",
			OccurrenceKey: occurrence, RequestKey: request, PlannedAt: time.Now().UTC(),
			Status: model.TopicDigestStatusQueued, DispatchStatus: model.TopicDigestDispatchPending,
			ActiveKey: active, InputSnapshot: []byte(`{}`), InputHash: "hash",
		}
	}

	created, err := repo.CreateIfAbsent(ctx, base("digest-1", "occurrence-1", "request-1", &activeKey))
	if err != nil || !created {
		t.Fatalf("first insert = %v, %v", created, err)
	}
	created, err = repo.CreateIfAbsent(ctx, base("digest-2", "occurrence-2", "request-2", &activeKey))
	if err != nil || created {
		t.Fatalf("active-key conflict = %v, %v; want silent no-op", created, err)
	}
	if _, err := repo.FindByOccurrence(ctx, "occurrence-2"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("conflicting digest lookup = %v, want not found", err)
	}
	created, err = repo.CreateIfAbsent(ctx, base("digest-3", "occurrence-1", "request-3", nil))
	if err != nil || created {
		t.Fatalf("occurrence conflict = %v, %v; want silent no-op", created, err)
	}
	existing, err := repo.FindByOccurrence(ctx, "occurrence-1")
	if err != nil || existing.ID != "digest-1" {
		t.Fatalf("existing digest = %#v, %v", existing, err)
	}
}
