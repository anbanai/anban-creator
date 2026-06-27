package service

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/storage"
)

// signFakeStore is a minimal storage.Provider for exercising SignURL routing.
type signFakeStore struct {
	customDomain bool
	ownedPrefix  string // IsOwnedURL true when rawURL starts with this
	signErr      error
}

func (s *signFakeStore) Name() string { return "fake" }
func (s *signFakeStore) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (s *signFakeStore) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (s *signFakeStore) GetURL(key string) string                     { return "https://cdn.example.com/" + key }
func (s *signFakeStore) Read(context.Context, string) ([]byte, error) { return nil, nil }
func (s *signFakeStore) Delete(context.Context, string) error         { return nil }
func (s *signFakeStore) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	if s.signErr != nil {
		return "", s.signErr
	}
	// Shape mimics an OSS v1 signed URL: signature carried in the query string.
	return "https://bucket.oss-cn-x.aliyuncs.com/" + key + "?OSSAccessKeyId=x&Expires=1&Signature=abc", nil
}
func (s *signFakeStore) HasCustomDomain() bool { return s.customDomain }
func (s *signFakeStore) IsOwnedURL(rawURL string) bool {
	return s.ownedPrefix != "" && strings.HasPrefix(rawURL, s.ownedPrefix)
}

func TestSignURL_NilStoreNoOp(t *testing.T) {
	in := "https://bucket.oss-cn-x.aliyuncs.com/uploads/references/u/f.jpg"
	if got := SignURL(context.Background(), nil, nil, in, 0); got != in {
		t.Fatalf("nil store should return input unchanged, got %q", got)
	}
}

func TestSignURL_EmptyInput(t *testing.T) {
	store := &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}
	for _, in := range []string{"", "   "} {
		if got := SignURL(context.Background(), store, nil, in, 0); got != strings.TrimSpace(in) {
			t.Fatalf("empty input %q should pass through, got %q", in, got)
		}
	}
}

func TestSignURL_ExternalURLNotSigned(t *testing.T) {
	store := &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}
	in := "https://images.unsplash.com/photo-123.jpeg"
	if got := SignURL(context.Background(), store, nil, in, 0); got != in {
		t.Fatalf("external URL must be returned unchanged (never signed), got %q", got)
	}
}

func TestSignURL_OwnedOSSURLSigned(t *testing.T) {
	store := &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}
	in := "https://bucket.oss-cn-x.aliyuncs.com/uploads/references/u/abc.jpg"
	got := SignURL(context.Background(), store, nil, in, 0)
	if !strings.Contains(got, "Signature=") {
		t.Fatalf("owned OSS URL should be signed (contain Signature=), got %q", got)
	}
	if !strings.HasPrefix(got, "https://bucket.oss-cn-x.aliyuncs.com/uploads/references/u/abc.jpg") {
		t.Fatalf("signed URL should be rooted at the original key path, got %q", got)
	}
}

func TestSignURL_LocalPathSigned(t *testing.T) {
	// Local-style /api/v1/files/ keys are server-owned and should resolve via the provider.
	store := &signFakeStore{ownedPrefix: "/api/v1/files/"}
	in := "/api/v1/files/uploads/references/u/abc.jpg"
	got := SignURL(context.Background(), store, nil, in, 0)
	if !strings.Contains(got, "Signature=") {
		t.Fatalf("owned local path should resolve to a signed URL, got %q", got)
	}
}

func TestSignURL_CustomDomainReturnsGetURL(t *testing.T) {
	store := &signFakeStore{customDomain: true, ownedPrefix: "https://cdn.example.com/"}
	in := "https://cdn.example.com/uploads/references/u/abc.jpg"
	got := SignURL(context.Background(), store, nil, in, 0)
	// Custom domain → permanent public URL, no Signature query.
	if strings.Contains(got, "Signature=") {
		t.Fatalf("custom-domain URL must not be signed, got %q", got)
	}
	if !strings.HasPrefix(got, "https://cdn.example.com/uploads/references/u/abc.jpg") {
		t.Fatalf("custom-domain URL should resolve via GetURL, got %q", got)
	}
}

func TestSignURL_DownloadURLErrorFallsBack(t *testing.T) {
	store := &signFakeStore{
		ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/",
		signErr:     io.ErrUnexpectedEOF,
	}
	in := "https://bucket.oss-cn-x.aliyuncs.com/uploads/references/u/abc.jpg"
	log := zerolog.Nop()
	if got := SignURL(context.Background(), store, &log, in, 0); got != in {
		t.Fatalf("on DownloadURL error should fall back to original URL, got %q", got)
	}
}

// TestSignTemplateURLs: both image fields resolve to signed URLs in place, and a
// nil template must not panic (guards the recommended-templates loop where a nil
// element is theoretically possible).
func TestSignTemplateURLs(t *testing.T) {
	store := &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}
	tmpl := &model.Template{
		ThumbnailURL:  "https://bucket.oss-cn-x.aliyuncs.com/uploads/references/u/a.jpg",
		PersonaAvatar: "https://bucket.oss-cn-x.aliyuncs.com/uploads/references/u/b.jpg",
	}
	SignTemplateURLs(context.Background(), store, nil, tmpl)
	if !strings.Contains(tmpl.ThumbnailURL, "Signature=") {
		t.Errorf("thumbnail should be signed, got %q", tmpl.ThumbnailURL)
	}
	if !strings.Contains(tmpl.PersonaAvatar, "Signature=") {
		t.Errorf("author avatar should be signed, got %q", tmpl.PersonaAvatar)
	}
	// nil template must not panic.
	SignTemplateURLs(context.Background(), store, nil, nil)
}
