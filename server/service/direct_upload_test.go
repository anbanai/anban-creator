package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type fakeDirectUploadStore struct {
	name        string
	uploadKey   string
	contentType string
	expires     int
	deleted     []string
}

func (s *fakeDirectUploadStore) Name() string { return s.name }
func (s *fakeDirectUploadStore) UploadURL(_ context.Context, key string, contentType string, expires int) (string, error) {
	s.uploadKey = key
	s.contentType = contentType
	s.expires = expires
	return "https://oss-upload.example.com/" + key, nil
}
func (s *fakeDirectUploadStore) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (s *fakeDirectUploadStore) DownloadURL(_ context.Context, key string, expires int) (string, error) {
	return "https://oss-download.example.com/" + key, nil
}
func (s *fakeDirectUploadStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

type fakePendingUploadRepo struct {
	created   *model.PendingUpload
	uploads   map[string]*model.PendingUpload
	finalized []string
}

func (r *fakePendingUploadRepo) CreatePendingUpload(_ context.Context, upload *model.PendingUpload) error {
	cp := *upload
	r.created = &cp
	if r.uploads == nil {
		r.uploads = map[string]*model.PendingUpload{}
	}
	r.uploads[upload.ID] = &cp
	return nil
}

func (r *fakePendingUploadRepo) FindPendingUploadByID(_ context.Context, id string) (*model.PendingUpload, error) {
	if r.uploads == nil || r.uploads[id] == nil {
		return nil, ErrPendingUploadNotFound
	}
	cp := *r.uploads[id]
	return &cp, nil
}

func (r *fakePendingUploadRepo) FinalizePendingUploads(_ context.Context, ids []string, _ time.Time) error {
	r.finalized = append(r.finalized, ids...)
	return nil
}

func (r *fakePendingUploadRepo) FindExpiredPendingUploads(_ context.Context, _ time.Time, _ int) ([]*model.PendingUpload, error) {
	return nil, nil
}

func (r *fakePendingUploadRepo) MarkPendingUploadExpired(_ context.Context, id string, _ time.Time) error {
	return nil
}

func TestPrepareDirectUploadCreatesPendingScopedSTSSession(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	repo := &fakePendingUploadRepo{}
	issuer := StaticUploadCredentialIssuer(func(_ context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
		if !strings.Contains(req.Policy, "uploads/pending/user-1/") {
			t.Fatalf("policy = %s, want user pending prefix", req.Policy)
		}
		var policy struct {
			Statement []struct {
				Action []string `json:"Action"`
			} `json:"Statement"`
		}
		if err := json.Unmarshal([]byte(req.Policy), &policy); err != nil {
			t.Fatalf("policy is not JSON: %v", err)
		}
		actions := strings.Join(policy.Statement[0].Action, ",")
		for _, want := range []string{"oss:PutObject", "oss:InitiateMultipartUpload", "oss:UploadPart", "oss:CompleteMultipartUpload", "oss:AbortMultipartUpload", "oss:ListParts"} {
			if !strings.Contains(actions, want) {
				t.Fatalf("policy actions = %s, want %s", actions, want)
			}
		}
		return &UploadCredential{
			AccessKeyID:     "sts-ak",
			AccessKeySecret: "sts-secret",
			SecurityToken:   "sts-token",
			ExpiresAt:       time.Now().Add(15 * time.Minute),
		}, nil
	})

	result, err := PrepareDirectUpload(context.Background(), store, repo, DirectUploadConfig{
		Storage: config.StorageConfig{
			Provider:       "oss",
			Endpoint:       "oss-cn-hangzhou.aliyuncs.com",
			BucketName:     "anban-test",
			Region:         "oss-cn-hangzhou",
			STSRoleArn:     "acs:ram::1:role/upload",
			STSSessionName: "studio-upload",
		},
		CredentialIssuer: issuer,
		Now:              func() time.Time { return time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC) },
	}, DirectUploadPrepareRequest{
		UserID:      "user-1",
		Purpose:     DirectUploadPurposeVideoReference,
		Filename:    "test.MP4",
		ContentType: "video/mp4",
		Size:        42 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("PrepareDirectUpload: %v", err)
	}
	if result.UploadID == "" || result.Key == "" || result.PublicURL == "" {
		t.Fatalf("missing direct upload fields: %#v", result)
	}
	if !strings.HasPrefix(result.Key, "uploads/pending/user-1/") || !strings.HasSuffix(result.Key, ".mp4") {
		t.Fatalf("key = %q, want user pending mp4 key", result.Key)
	}
	if result.STSAccessKeyID != "sts-ak" || result.STSSecurityToken != "sts-token" {
		t.Fatalf("sts fields = %#v", result)
	}
	if result.Bucket != "anban-test" || result.Region != "oss-cn-hangzhou" {
		t.Fatalf("oss target = bucket %q region %q", result.Bucket, result.Region)
	}
	if result.MaxSize != 50*1024*1024 {
		t.Fatalf("max_size = %d, want 50MB", result.MaxSize)
	}
	if repo.created == nil || repo.created.Status != model.PendingUploadStatusPending {
		t.Fatalf("pending upload not recorded: %#v", repo.created)
	}
	if store.uploadKey != result.Key || store.contentType != "video/mp4" {
		t.Fatalf("signed upload key/content-type = %q/%q", store.uploadKey, store.contentType)
	}
}

func TestPrepareDirectUploadAllowsAIEntryMediaAndDocuments(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	repo := &fakePendingUploadRepo{}
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Minute)}, nil
		}),
	}

	media, err := PrepareDirectUpload(context.Background(), store, repo, cfg, DirectUploadPrepareRequest{
		UserID:      "u",
		Purpose:     DirectUploadPurposeAIEntryAttachment,
		Filename:    "demo.mp4",
		ContentType: "video/mp4",
		Size:        50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("PrepareDirectUpload media: %v", err)
	}
	if media.MaxSize != 50*1024*1024 {
		t.Fatalf("media max size = %d, want 50MB", media.MaxSize)
	}

	doc, err := PrepareDirectUpload(context.Background(), store, repo, cfg, DirectUploadPrepareRequest{
		UserID:      "u",
		Purpose:     DirectUploadPurposeAIEntryAttachment,
		Filename:    "brief.pdf",
		ContentType: "application/pdf",
		Size:        25 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("PrepareDirectUpload document: %v", err)
	}
	if doc.MaxSize != 25*1024*1024 {
		t.Fatalf("document max size = %d, want 25MB", doc.MaxSize)
	}
}

func TestPrepareDirectUploadInfersGenericContentTypeFromExtension(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	repo := &fakePendingUploadRepo{}
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Minute)}, nil
		}),
	}

	result, err := PrepareDirectUpload(context.Background(), store, repo, cfg, DirectUploadPrepareRequest{
		UserID:      "u",
		Purpose:     DirectUploadPurposeVideoReference,
		Filename:    "voice.m4a",
		ContentType: "application/octet-stream",
		Size:        1024,
	})
	if err != nil {
		t.Fatalf("PrepareDirectUpload generic m4a: %v", err)
	}
	if got := result.Headers["Content-Type"]; got != "audio/mp4" {
		t.Fatalf("Content-Type header = %q, want audio/mp4", got)
	}
	if store.contentType != "audio/mp4" {
		t.Fatalf("signed upload content type = %q, want audio/mp4", store.contentType)
	}

	result, err = PrepareDirectUpload(context.Background(), store, repo, cfg, DirectUploadPrepareRequest{
		UserID:      "u",
		Purpose:     DirectUploadPurposeAIEntryAttachment,
		Filename:    "brief.md",
		ContentType: "",
		Size:        1024,
	})
	if err != nil {
		t.Fatalf("PrepareDirectUpload empty md: %v", err)
	}
	if got := result.Headers["Content-Type"]; got != "text/markdown" {
		t.Fatalf("Content-Type header = %q, want text/markdown", got)
	}
}

func TestPrepareDirectUploadRejectsInvalidPurposeSizeAndMIME(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	repo := &fakePendingUploadRepo{}
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{}, nil
		}),
	}
	cases := []DirectUploadPrepareRequest{
		{UserID: "u", Purpose: "bad", Filename: "a.png", ContentType: "image/png", Size: 1},
		{UserID: "u", Purpose: DirectUploadPurposeProjectReference, Filename: "a.mp4", ContentType: "video/mp4", Size: 1},
		{UserID: "u", Purpose: DirectUploadPurposeVideoReference, Filename: "a.mp4", ContentType: "video/mp4", Size: 51 * 1024 * 1024},
		{UserID: "u", Purpose: DirectUploadPurposeAIEntryAttachment, Filename: "brief.exe", ContentType: "application/x-msdownload", Size: 1},
		{UserID: "u", Purpose: DirectUploadPurposeAIEntryAttachment, Filename: "brief.pdf", ContentType: "application/pdf", Size: 26 * 1024 * 1024},
	}
	for _, tc := range cases {
		if _, err := PrepareDirectUpload(context.Background(), store, repo, cfg, tc); err == nil {
			t.Fatalf("PrepareDirectUpload(%+v) succeeded, want error", tc)
		}
	}
}

func TestFinalizePendingUploadURLsRejectsCrossUserAndExpired(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{
		"ok": {
			ID:        "ok",
			UserID:    "user-1",
			Purpose:   DirectUploadPurposeVideoReference,
			Key:       "uploads/pending/user-1/ok/ref.mp4",
			PublicURL: "https://cdn.example.com/uploads/pending/user-1/ok/ref.mp4",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(time.Minute),
		},
		"other": {
			ID:        "other",
			UserID:    "user-2",
			Purpose:   DirectUploadPurposeVideoReference,
			PublicURL: "https://cdn.example.com/uploads/pending/user-2/other/ref.mp4",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(time.Minute),
		},
		"expired": {
			ID:        "expired",
			UserID:    "user-1",
			Purpose:   DirectUploadPurposeVideoReference,
			PublicURL: "https://cdn.example.com/uploads/pending/user-1/expired/ref.mp4",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(-time.Minute),
		},
	}}
	if err := FinalizePendingUploadURLs(context.Background(), repo, "user-1", DirectUploadPurposeVideoReference, []string{
		"https://cdn.example.com/uploads/pending/user-1/ok/ref.mp4?x=1",
	}, now); err != nil {
		t.Fatalf("FinalizePendingUploadURLs ok: %v", err)
	}
	if len(repo.finalized) != 1 || repo.finalized[0] != "ok" {
		t.Fatalf("finalized = %v, want [ok]", repo.finalized)
	}
	if err := FinalizePendingUploadURLs(context.Background(), repo, "user-1", DirectUploadPurposeVideoReference, []string{
		"https://cdn.example.com/uploads/pending/user-2/other/ref.mp4",
	}, now); err == nil {
		t.Fatal("cross-user pending upload finalized, want error")
	}
	if err := FinalizePendingUploadURLs(context.Background(), repo, "user-1", DirectUploadPurposeVideoReference, []string{
		"https://cdn.example.com/uploads/pending/user-1/expired/ref.mp4",
	}, now); err == nil {
		t.Fatal("expired pending upload finalized, want error")
	}
}

func TestFinalizePendingUploadURLsRejectsMismatchedPendingURL(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{
		"ok": {
			ID:        "ok",
			UserID:    "user-1",
			Purpose:   DirectUploadPurposeVideoReference,
			Key:       "uploads/pending/user-1/ok/ref.mp4",
			PublicURL: "https://cdn.example.com/uploads/pending/user-1/ok/ref.mp4",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(time.Minute),
		},
	}}

	cases := []string{
		"https://evil.example.com/uploads/pending/user-1/ok/ref.mp4",
		"https://cdn.example.com/uploads/pending/user-1/ok/other.mp4",
	}
	for _, raw := range cases {
		if err := FinalizePendingUploadURLs(context.Background(), repo, "user-1", DirectUploadPurposeVideoReference, []string{raw}, now); err == nil {
			t.Fatalf("FinalizePendingUploadURLs(%q) succeeded, want mismatch error", raw)
		}
	}
	if len(repo.finalized) != 0 {
		t.Fatalf("finalized = %v, want no finalized uploads", repo.finalized)
	}
}

func TestValidatePendingUploadURLReturnsKeyWithoutFinalizing(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{
		"ok": {
			ID:        "ok",
			UserID:    "user-1",
			Purpose:   DirectUploadPurposeProjectReference,
			Key:       "uploads/pending/user-1/ok/ref.png",
			PublicURL: "https://cdn.example.com/uploads/pending/user-1/ok/ref.png",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(time.Minute),
		},
	}}

	key, err := ValidatePendingUploadURL(context.Background(), repo, "user-1", []string{DirectUploadPurposeProjectReference}, "https://cdn.example.com/uploads/pending/user-1/ok/ref.png?x=1", now)
	if err != nil {
		t.Fatalf("ValidatePendingUploadURL: %v", err)
	}
	if key != "uploads/pending/user-1/ok/ref.png" {
		t.Fatalf("key = %q, want pending object key", key)
	}
	if len(repo.finalized) != 0 {
		t.Fatalf("ValidatePendingUploadURL finalized uploads = %v, want none", repo.finalized)
	}
}

func TestValidatePendingUploadURLRejectsUnauthorizedAndInvalid(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{
		"other": {
			ID:        "other",
			UserID:    "user-2",
			Purpose:   DirectUploadPurposeProjectReference,
			Key:       "uploads/pending/user-2/other/ref.png",
			PublicURL: "https://cdn.example.com/uploads/pending/user-2/other/ref.png",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(time.Minute),
		},
		"wrong-purpose": {
			ID:        "wrong-purpose",
			UserID:    "user-1",
			Purpose:   DirectUploadPurposeAIEntryAttachment,
			Key:       "uploads/pending/user-1/wrong-purpose/ref.png",
			PublicURL: "https://cdn.example.com/uploads/pending/user-1/wrong-purpose/ref.png",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(time.Minute),
		},
		"expired": {
			ID:        "expired",
			UserID:    "user-1",
			Purpose:   DirectUploadPurposeProjectReference,
			Key:       "uploads/pending/user-1/expired/ref.png",
			PublicURL: "https://cdn.example.com/uploads/pending/user-1/expired/ref.png",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(-time.Minute),
		},
		"mismatch": {
			ID:        "mismatch",
			UserID:    "user-1",
			Purpose:   DirectUploadPurposeProjectReference,
			Key:       "uploads/pending/user-1/mismatch/ref.png",
			PublicURL: "https://cdn.example.com/uploads/pending/user-1/mismatch/ref.png",
			Status:    model.PendingUploadStatusPending,
			ExpiresAt: now.Add(time.Minute),
		},
	}}

	cases := []struct {
		name string
		url  string
	}{
		{"cross user", "https://cdn.example.com/uploads/pending/user-2/other/ref.png"},
		{"wrong purpose", "https://cdn.example.com/uploads/pending/user-1/wrong-purpose/ref.png"},
		{"expired", "https://cdn.example.com/uploads/pending/user-1/expired/ref.png"},
		{"host mismatch", "https://evil.example.com/uploads/pending/user-1/mismatch/ref.png"},
		{"path mismatch", "https://cdn.example.com/uploads/pending/user-1/mismatch/other.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidatePendingUploadURL(context.Background(), repo, "user-1", []string{DirectUploadPurposeProjectReference}, tc.url, now); err == nil {
				t.Fatalf("ValidatePendingUploadURL(%q) succeeded, want error", tc.url)
			}
		})
	}
	if len(repo.finalized) != 0 {
		t.Fatalf("ValidatePendingUploadURL finalized uploads = %v, want none", repo.finalized)
	}
}
