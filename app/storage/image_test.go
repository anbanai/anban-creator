package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCreateImage(t *testing.T) {
	s := newTestStore(t)

	img := &Image{
		Role:      "cover",
		Prompt:    "春天的茶园",
		Provider:  "gemini",
		LocalPath: "/tmp/test.png",
		CreatedAt: time.Now(),
	}
	if err := s.CreateImage(img); err != nil {
		t.Fatalf("CreateImage: %v", err)
	}
	if img.ID == 0 {
		t.Error("expected non-zero ID after create")
	}
}

func TestFindImageByPath(t *testing.T) {
	s := newTestStore(t)

	img := &Image{LocalPath: "/tmp/find_me.png", CreatedAt: time.Now()}
	_ = s.CreateImage(img)

	found, err := s.FindImageByPath("/tmp/find_me.png")
	if err != nil {
		t.Fatalf("FindImageByPath: %v", err)
	}
	if found.ID != img.ID {
		t.Errorf("got ID %d, want %d", found.ID, img.ID)
	}
}

func TestUpdateImageUpload(t *testing.T) {
	s := newTestStore(t)

	img := &Image{LocalPath: "/tmp/upload_me.png", CreatedAt: time.Now()}
	_ = s.CreateImage(img)

	if err := s.UpdateImageUpload("/tmp/upload_me.png", "media123", "https://wx.cdn/img.png"); err != nil {
		t.Fatalf("UpdateImageUpload: %v", err)
	}

	found, _ := s.FindImageByPath("/tmp/upload_me.png")
	if found.MediaID != "media123" {
		t.Errorf("got MediaID %q, want %q", found.MediaID, "media123")
	}
	if found.WechatURL != "https://wx.cdn/img.png" {
		t.Errorf("got WechatURL %q, want %q", found.WechatURL, "https://wx.cdn/img.png")
	}
}

func TestUpsertImageUpload_NewRecord(t *testing.T) {
	s := newTestStore(t)

	localPath := filepath.Join(t.TempDir(), "new.png")
	if err := s.UpsertImageUpload(localPath, "media999", "https://wx.cdn/new.png"); err != nil {
		t.Fatalf("UpsertImageUpload: %v", err)
	}

	found, err := s.FindImageByPath(localPath)
	if err != nil {
		t.Fatalf("FindImageByPath after upsert: %v", err)
	}
	if found.MediaID != "media999" {
		t.Errorf("got MediaID %q, want media999", found.MediaID)
	}
}

func TestListImages(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 3; i++ {
		_ = s.CreateImage(&Image{LocalPath: "/tmp/img_list_" + string(rune('0'+i)) + ".png", CreatedAt: time.Now()})
	}

	imgs, err := s.ListImages(10)
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(imgs) != 3 {
		t.Errorf("got %d images, want 3", len(imgs))
	}
}
