package storage

import (
	"testing"
	"time"
)

func TestCreateDraft(t *testing.T) {
	s := newTestStore(t)

	d := &Draft{
		MediaID:   "media_abc123",
		Title:     "测试文章",
		Digest:    "这是摘要",
		Type:      "article",
		CreatedAt: time.Now(),
	}
	if err := s.CreateDraft(d); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if d.ID == 0 {
		t.Error("expected non-zero ID after create")
	}
}

func TestListDrafts(t *testing.T) {
	s := newTestStore(t)

	for range 3 {
		_ = s.CreateDraft(&Draft{MediaID: "media_x", Type: "article", CreatedAt: time.Now()})
	}

	drafts, err := s.ListDrafts(10)
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(drafts) != 3 {
		t.Errorf("got %d drafts, want 3", len(drafts))
	}
}
