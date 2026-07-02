package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/royalrick/anbanwriter/server/storage"
)

type fakeMediaSourceStore struct {
	customDomain bool
	ownedPrefix  string
	signErr      error
	readErr      error
	readKey      string
	signedKey    string
	readData     []byte
}

func (s *fakeMediaSourceStore) Name() string { return "oss" }
func (s *fakeMediaSourceStore) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (s *fakeMediaSourceStore) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (s *fakeMediaSourceStore) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}
func (s *fakeMediaSourceStore) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (s *fakeMediaSourceStore) Read(_ context.Context, key string) ([]byte, error) {
	s.readKey = key
	if s.readErr != nil {
		return nil, s.readErr
	}
	return append([]byte(nil), s.readData...), nil
}
func (s *fakeMediaSourceStore) Delete(context.Context, string) error { return nil }
func (s *fakeMediaSourceStore) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	s.signedKey = key
	if s.signErr != nil {
		return "", s.signErr
	}
	return "https://signed.example.com/" + key + "?Signature=abc", nil
}
func (s *fakeMediaSourceStore) HasCustomDomain() bool { return s.customDomain }
func (s *fakeMediaSourceStore) IsOwnedURL(rawURL string) bool {
	return s.ownedPrefix != "" && strings.HasPrefix(rawURL, s.ownedPrefix)
}

func TestResolveMediaSourceSignsOwnedOSSURL(t *testing.T) {
	store := &fakeMediaSourceStore{ownedPrefix: "https://bucket.oss-cn.example.com/"}

	resolved, err := ResolveMediaSource(context.Background(), store, nil, MediaSourceRequest{
		RawURL: "https://bucket.oss-cn.example.com/uploads/video-audio/take.wav",
		TTL:    600,
	})
	if err != nil {
		t.Fatalf("ResolveMediaSource: %v", err)
	}
	if resolved.Key != "uploads/video-audio/take.wav" {
		t.Fatalf("Key = %q", resolved.Key)
	}
	if store.signedKey != resolved.Key || !strings.Contains(resolved.URL, "Signature=abc") {
		t.Fatalf("owned URL should be signed, url=%q signedKey=%q", resolved.URL, store.signedKey)
	}
	if resolved.External {
		t.Fatal("owned URL should not be marked external")
	}
}

func TestResolveMediaSourceResignsAlreadySignedOwnedURL(t *testing.T) {
	store := &fakeMediaSourceStore{ownedPrefix: "https://bucket.oss-cn.example.com/"}

	resolved, err := ResolveMediaSource(context.Background(), store, nil, MediaSourceRequest{
		RawURL: "https://bucket.oss-cn.example.com/uploads/video-audio/take.wav?Expires=1&Signature=old",
		TTL:    600,
	})
	if err != nil {
		t.Fatalf("ResolveMediaSource: %v", err)
	}
	if resolved.Key != "uploads/video-audio/take.wav" {
		t.Fatalf("Key = %q", resolved.Key)
	}
	if store.signedKey != resolved.Key || !strings.Contains(resolved.URL, "Signature=abc") {
		t.Fatalf("owned signed URL should be re-signed, url=%q signedKey=%q", resolved.URL, store.signedKey)
	}
}

func TestResolveMediaSourceCustomDomainReturnsPublicURL(t *testing.T) {
	store := &fakeMediaSourceStore{customDomain: true, ownedPrefix: "https://cdn.example.com/"}

	resolved, err := ResolveMediaSource(context.Background(), store, nil, MediaSourceRequest{
		RawURL: "https://cdn.example.com/uploads/video-audio/take.wav",
	})
	if err != nil {
		t.Fatalf("ResolveMediaSource: %v", err)
	}
	if resolved.URL != "https://cdn.example.com/uploads/video-audio/take.wav" {
		t.Fatalf("custom domain URL = %q", resolved.URL)
	}
	if store.signedKey != "" {
		t.Fatalf("custom domain should not call DownloadURL, signedKey=%q", store.signedKey)
	}
}

func TestResolveMediaSourceExternalHTTPSPassesThrough(t *testing.T) {
	store := &fakeMediaSourceStore{ownedPrefix: "https://bucket.oss-cn.example.com/"}

	resolved, err := ResolveMediaSource(context.Background(), store, nil, MediaSourceRequest{
		RawURL: "https://example.com/audio.wav",
	})
	if err != nil {
		t.Fatalf("ResolveMediaSource: %v", err)
	}
	if !resolved.External || resolved.URL != "https://example.com/audio.wav" {
		t.Fatalf("external URL resolution = %#v", resolved)
	}
	if store.signedKey != "" {
		t.Fatalf("external URL must not be signed, signedKey=%q", store.signedKey)
	}
}

func TestResolveMediaSourceRejectsExternalNonHTTPS(t *testing.T) {
	_, err := ResolveMediaSource(context.Background(), nil, nil, MediaSourceRequest{
		RawURL: "http://example.com/audio.wav",
	})
	if err == nil || !strings.Contains(err.Error(), "must be an HTTPS URL") {
		t.Fatalf("ResolveMediaSource error = %v", err)
	}
}

func TestResolveMediaSourceBytesReadsOwnedObject(t *testing.T) {
	store := &fakeMediaSourceStore{
		ownedPrefix: "https://bucket.oss-cn.example.com/",
		readData:    []byte("fake-wav"),
	}

	resolved, err := ResolveMediaSourceBytes(context.Background(), store, nil, MediaSourceRequest{
		RawURL:      "https://bucket.oss-cn.example.com/uploads/video-audio/take.wav",
		MaxBytes:    100,
		ContentType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("ResolveMediaSourceBytes: %v", err)
	}
	if string(resolved.Bytes) != "fake-wav" || store.readKey != "uploads/video-audio/take.wav" {
		t.Fatalf("owned bytes not read from storage: resolved=%#v readKey=%q", resolved, store.readKey)
	}
}
