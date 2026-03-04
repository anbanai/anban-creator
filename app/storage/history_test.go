package storage

import (
	"testing"
	"time"
)

func TestUpsertHistories(t *testing.T) {
	s := newTestStore(t)

	now := time.Now()
	items := []History{
		{Source: "draft", ItemID: "draft_001", Title: "草稿文章", UpdateTime: now.Unix(), SyncedAt: now},
		{Source: "published", ItemID: "pub_001", Title: "已发布文章", UpdateTime: now.Unix(), SyncedAt: now},
	}
	if err := s.UpsertHistories(items); err != nil {
		t.Fatalf("UpsertHistories: %v", err)
	}

	// upsert 更新
	items[0].Title = "草稿文章（更新）"
	if err := s.UpsertHistories(items); err != nil {
		t.Fatalf("UpsertHistories update: %v", err)
	}

	list, err := s.ListHistories(10)
	if err != nil {
		t.Fatalf("ListHistories: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("got %d histories, want 2", len(list))
	}
}

func TestHasHistories(t *testing.T) {
	s := newTestStore(t)

	has, err := s.HasHistories()
	if err != nil {
		t.Fatalf("HasHistories: %v", err)
	}
	if has {
		t.Error("expected no histories initially")
	}

	now := time.Now()
	_ = s.UpsertHistories([]History{{Source: "draft", ItemID: "x", UpdateTime: now.Unix(), SyncedAt: now}})

	has, err = s.HasHistories()
	if err != nil {
		t.Fatalf("HasHistories after insert: %v", err)
	}
	if !has {
		t.Error("expected has histories after insert")
	}
}

func TestGetLastSyncTime(t *testing.T) {
	s := newTestStore(t)

	// empty store
	_, err := s.GetLastSyncTime()
	if err == nil {
		t.Error("expected error for empty store")
	}

	now := time.Now()
	earlier := now.Add(-time.Hour)
	_ = s.UpsertHistories([]History{
		{Source: "draft", ItemID: "a", UpdateTime: now.Unix(), SyncedAt: now},
		{Source: "draft", ItemID: "b", UpdateTime: earlier.Unix(), SyncedAt: earlier},
	})

	lastSync, err := s.GetLastSyncTime()
	if err != nil {
		t.Fatalf("GetLastSyncTime: %v", err)
	}
	// Should be the most recent sync time
	if lastSync.Before(earlier) {
		t.Errorf("got sync time %v, expected >= %v", lastSync, earlier)
	}
}
