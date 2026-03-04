package storage

import (
	"testing"
	"time"
)

func TestContent_CreateAndFind(t *testing.T) {
	s := newTestStore(t)

	c := &Content{
		Type:      "article",
		Dir:       "/tmp/test-article",
		Status:    "created",
		Topic:     "茶文化",
		Style:     "dan-koe",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.UpsertContent(c); err != nil {
		t.Fatalf("UpsertContent: %v", err)
	}

	found, err := s.FindContentByDir("/tmp/test-article")
	if err != nil {
		t.Fatalf("FindContentByDir: %v", err)
	}

	if found.Type != "article" {
		t.Errorf("Type: got %q, want %q", found.Type, "article")
	}
	if found.Status != "created" {
		t.Errorf("Status: got %q, want %q", found.Status, "created")
	}
	if found.Topic != "茶文化" {
		t.Errorf("Topic: got %q, want %q", found.Topic, "茶文化")
	}
}

func TestContent_UpsertUpdatesNonZeroFields(t *testing.T) {
	s := newTestStore(t)

	// 初始记录
	c := &Content{
		Type:      "article",
		Dir:       "/tmp/test-upsert",
		Status:    "created",
		Topic:     "原始话题",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.UpsertContent(c); err != nil {
		t.Fatalf("initial UpsertContent: %v", err)
	}

	// 更新部分字段
	c2 := &Content{
		Dir:    "/tmp/test-upsert",
		Status: "outlined",
		Title:  "新标题",
	}
	if err := s.UpsertContent(c2); err != nil {
		t.Fatalf("update UpsertContent: %v", err)
	}

	found, err := s.FindContentByDir("/tmp/test-upsert")
	if err != nil {
		t.Fatalf("FindContentByDir: %v", err)
	}

	if found.Status != "outlined" {
		t.Errorf("Status: got %q, want %q", found.Status, "outlined")
	}
	if found.Title != "新标题" {
		t.Errorf("Title: got %q, want %q", found.Title, "新标题")
	}
	// 原有字段不应被零值覆盖
	if found.Topic != "原始话题" {
		t.Errorf("Topic should not be overwritten: got %q, want %q", found.Topic, "原始话题")
	}
}

func TestContent_ListUnpublishedContents(t *testing.T) {
	s := newTestStore(t)

	now := time.Now()
	contents := []*Content{
		{Type: "article", Dir: "/tmp/art1", Status: "created", CreatedAt: now, UpdatedAt: now},
		{Type: "article", Dir: "/tmp/art2", Status: "published", CreatedAt: now, UpdatedAt: now},
		{Type: "post", Dir: "/tmp/post1", Status: "planned", CreatedAt: now, UpdatedAt: now},
	}
	for _, c := range contents {
		if err := s.UpsertContent(c); err != nil {
			t.Fatalf("UpsertContent: %v", err)
		}
	}

	unpublished, err := s.ListUnpublishedContents(10)
	if err != nil {
		t.Fatalf("ListUnpublishedContents: %v", err)
	}

	if len(unpublished) != 2 {
		t.Errorf("got %d unpublished, want 2", len(unpublished))
	}
	for _, c := range unpublished {
		if c.Status == "published" {
			t.Errorf("published item should not be in unpublished list: %v", c.Dir)
		}
	}
}

func TestContent_UpdateContentFields(t *testing.T) {
	s := newTestStore(t)

	now := time.Now()
	c := &Content{
		Type:      "article",
		Dir:       "/tmp/test-fields",
		Status:    "drafted",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.UpsertContent(c); err != nil {
		t.Fatalf("UpsertContent: %v", err)
	}

	fields := map[string]any{
		"status":   "published",
		"media_id": "MEDIA_ID_123",
		"title":    "发布的文章",
	}
	if err := s.UpdateContentFields("/tmp/test-fields", fields); err != nil {
		t.Fatalf("UpdateContentFields: %v", err)
	}

	found, err := s.FindContentByDir("/tmp/test-fields")
	if err != nil {
		t.Fatalf("FindContentByDir: %v", err)
	}

	if found.Status != "published" {
		t.Errorf("Status: got %q, want %q", found.Status, "published")
	}
	if found.MediaID != "MEDIA_ID_123" {
		t.Errorf("MediaID: got %q, want %q", found.MediaID, "MEDIA_ID_123")
	}
	if found.Title != "发布的文章" {
		t.Errorf("Title: got %q, want %q", found.Title, "发布的文章")
	}
}

func TestContent_HasContents(t *testing.T) {
	s := newTestStore(t)

	has, err := s.HasContents()
	if err != nil {
		t.Fatalf("HasContents: %v", err)
	}
	if has {
		t.Error("expected no contents initially")
	}

	now := time.Now()
	if err := s.UpsertContent(&Content{
		Type: "article", Dir: "/tmp/hastest", Status: "created",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertContent: %v", err)
	}

	has, err = s.HasContents()
	if err != nil {
		t.Fatalf("HasContents after insert: %v", err)
	}
	if !has {
		t.Error("expected HasContents to return true after insert")
	}
}
