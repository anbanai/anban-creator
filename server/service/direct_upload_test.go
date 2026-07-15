package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeDirectUploadStore struct {
	name        string
	uploadKey   string
	contentType string
	expires     int
	deleted     []string
	deleteErr   error
	objects     map[string]*storage.ObjectInfo
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
	return s.deleteErr
}
func (s *fakeDirectUploadStore) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
	info := s.objects[key]
	if info == nil {
		return nil, errors.New("object not found")
	}
	copy := *info
	return &copy, nil
}

func finalizeVerifiedDirectUploadsWithStore(ctx context.Context, store *fakeDirectUploadStore, repo PendingUploadRepository, uploads []*VerifiedDirectUpload, now time.Time) error {
	return FinalizeVerifiedDirectUploads(ctx, store, repo, uploads, now)
}

func matchingDirectUploadStore(uploads ...*model.PendingUpload) *fakeDirectUploadStore {
	store := &fakeDirectUploadStore{objects: make(map[string]*storage.ObjectInfo, len(uploads))}
	for _, upload := range uploads {
		if upload == nil {
			continue
		}
		store.objects[upload.Key] = &storage.ObjectInfo{Key: upload.Key, Size: upload.Size, ContentType: upload.ContentType}
	}
	return store
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

func (r *fakePendingUploadRepo) FinalizePendingUploads(_ context.Context, ids []string, finalizedAt time.Time) (int64, error) {
	var claimed int64
	for _, id := range ids {
		upload := r.uploads[id]
		if upload == nil || upload.Status != model.PendingUploadStatusPending || !upload.ExpiresAt.After(finalizedAt) {
			continue
		}
		upload.Status = model.PendingUploadStatusFinalized
		r.finalized = append(r.finalized, id)
		claimed++
	}
	return claimed, nil
}

func (r *fakePendingUploadRepo) FinalizePendingUploadClaims(_ context.Context, claims []model.PendingUploadClaim, finalizedAt time.Time) error {
	for _, claim := range claims {
		upload := r.uploads[claim.UploadID]
		if upload == nil || upload.UserID != claim.UserID || upload.Key != claim.Key || !directUploadPurposeAllowed(upload.Purpose, claim.AllowedPurposes) {
			return model.ErrPendingUploadClaimRejected
		}
		if upload.Status != model.PendingUploadStatusFinalized && (upload.Status != model.PendingUploadStatusPending || !upload.ExpiresAt.After(finalizedAt)) {
			return model.ErrPendingUploadClaimRejected
		}
	}
	for _, claim := range claims {
		upload := r.uploads[claim.UploadID]
		if upload.Status == model.PendingUploadStatusPending {
			upload.Status = model.PendingUploadStatusFinalized
			r.finalized = append(r.finalized, upload.ID)
		}
	}
	return nil
}

func (r *fakePendingUploadRepo) FindPendingUploadsForCleanup(_ context.Context, before, staleBefore time.Time, limit int) ([]*model.PendingUpload, error) {
	uploads := make([]*model.PendingUpload, 0)
	for _, upload := range r.uploads {
		claimablePending := upload.Status == model.PendingUploadStatusPending && !upload.ExpiresAt.After(before)
		claimableAbandoned := upload.Status == model.PendingUploadStatusExpiring && upload.CleanupClaimedAt != nil && !upload.CleanupClaimedAt.After(staleBefore)
		if claimablePending || claimableAbandoned {
			uploads = append(uploads, upload)
			if len(uploads) == limit {
				break
			}
		}
	}
	return uploads, nil
}

func (r *fakePendingUploadRepo) ClaimPendingUploadExpiration(_ context.Context, id, claimID string, claimedAt, staleBefore time.Time) (bool, error) {
	upload := r.uploads[id]
	claimablePending := upload != nil && upload.Status == model.PendingUploadStatusPending && !upload.ExpiresAt.After(claimedAt)
	claimableAbandoned := upload != nil && upload.Status == model.PendingUploadStatusExpiring && upload.CleanupClaimedAt != nil && !upload.CleanupClaimedAt.After(staleBefore)
	if !claimablePending && !claimableAbandoned {
		return false, nil
	}
	upload.Status = model.PendingUploadStatusExpiring
	upload.CleanupClaimID = claimID
	upload.CleanupClaimedAt = &claimedAt
	return true, nil
}

func (r *fakePendingUploadRepo) CompletePendingUploadExpiration(_ context.Context, id, claimID string, expiredAt time.Time) (bool, error) {
	upload := r.uploads[id]
	if upload == nil || upload.Status != model.PendingUploadStatusExpiring || upload.CleanupClaimID != claimID {
		return false, nil
	}
	upload.Status = model.PendingUploadStatusExpired
	upload.CleanupClaimID = ""
	upload.CleanupClaimedAt = nil
	upload.ExpiredAt = &expiredAt
	return true, nil
}

func (r *fakePendingUploadRepo) ReopenPendingUploadExpiration(_ context.Context, id, claimID string) (bool, error) {
	upload := r.uploads[id]
	if upload == nil || upload.Status != model.PendingUploadStatusExpiring || upload.CleanupClaimID != claimID {
		return false, nil
	}
	upload.Status = model.PendingUploadStatusPending
	upload.CleanupClaimID = ""
	upload.CleanupClaimedAt = nil
	return true, nil
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

func TestPrepareDirectUploadAllowsMontageMediaAndDocuments(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	repo := &fakePendingUploadRepo{}
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Minute)}, nil
		}),
	}

	cases := []DirectUploadPrepareRequest{
		{UserID: "u", Purpose: DirectUploadPurposeMontageAsset, Filename: "clip.mp4", ContentType: "video/mp4", Size: 50 * 1024 * 1024},
		{UserID: "u", Purpose: DirectUploadPurposeMontageAsset, Filename: "storyboard.png", ContentType: "image/png", Size: 8 * 1024 * 1024},
		{UserID: "u", Purpose: DirectUploadPurposeMontageAsset, Filename: "voice.m4a", ContentType: "audio/mp4", Size: 4 * 1024 * 1024},
		{UserID: "u", Purpose: DirectUploadPurposeMontageAsset, Filename: "script.md", ContentType: "text/markdown", Size: 1024},
		{UserID: "u", Purpose: DirectUploadPurposeMontageAsset, Filename: "brief.pdf", ContentType: "application/pdf", Size: 25 * 1024 * 1024},
	}
	for _, tc := range cases {
		if _, err := PrepareDirectUpload(context.Background(), store, repo, cfg, tc); err != nil {
			t.Fatalf("PrepareDirectUpload(%s/%s): %v", tc.Filename, tc.ContentType, err)
		}
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

func TestResolveDirectUploadAttachment(t *testing.T) {
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	const (
		uploadID = "upload-1"
		key      = "uploads/pending/user-1/upload-1/product.png"
	)
	want := VerifiedDirectUpload{
		UploadID:    uploadID,
		Key:         key,
		FileName:    "repository-product.png",
		ContentType: "image/png",
		Size:        2048,
		Purpose:     DirectUploadPurposeAIEntryAttachment,
	}

	tests := []struct {
		name          string
		upload        *model.PendingUpload
		lookupID      string
		userID        string
		purposes      []string
		assertedKey   string
		want          *VerifiedDirectUpload
		wantErr       error
		wantFinalized bool
	}{
		{
			name: "finalizes matching pending upload and returns repository metadata",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
				Key: key, FileName: want.FileName, ContentType: want.ContentType, Size: want.Size,
				Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Minute),
			},
			lookupID: uploadID, userID: "user-1", purposes: []string{DirectUploadPurposeTaskReference, DirectUploadPurposeAIEntryAttachment},
			assertedKey: key, want: &want, wantFinalized: true,
		},
		{
			name: "reuses matching finalized upload after pending expiry",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
				Key: key, FileName: want.FileName, ContentType: want.ContentType, Size: want.Size,
				Status: model.PendingUploadStatusFinalized, ExpiresAt: now.Add(-time.Hour),
			},
			lookupID: uploadID, userID: "user-1", purposes: []string{DirectUploadPurposeAIEntryAttachment},
			assertedKey: key, want: &want,
		},
		{
			name: "rejects cross user",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-2", Purpose: DirectUploadPurposeAIEntryAttachment, Key: key,
				Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Minute),
			},
			lookupID: uploadID, userID: "user-1", purposes: []string{DirectUploadPurposeAIEntryAttachment}, assertedKey: key,
			wantErr: ErrPendingUploadAccessDenied,
		},
		{
			name: "rejects wrong purpose",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-1", Purpose: DirectUploadPurposeProjectReference, Key: key,
				Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Minute),
			},
			lookupID: uploadID, userID: "user-1", purposes: []string{DirectUploadPurposeAIEntryAttachment}, assertedKey: key,
			wantErr: ErrPendingUploadAccessDenied,
		},
		{
			name: "rejects mismatched asserted key",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment, Key: key,
				Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Minute),
			},
			lookupID: uploadID, userID: "user-1", purposes: []string{DirectUploadPurposeAIEntryAttachment}, assertedKey: key + ".forged",
			wantErr: ErrPendingUploadAccessDenied,
		},
		{
			name: "rejects expired pending upload",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment, Key: key,
				Status: model.PendingUploadStatusPending, ExpiresAt: now,
			},
			lookupID: uploadID, userID: "user-1", purposes: []string{DirectUploadPurposeAIEntryAttachment}, assertedKey: key,
			wantErr: ErrPendingUploadExpired,
		},
		{
			name: "rejects unknown upload id",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment, Key: key,
				Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Minute),
			},
			lookupID: "unknown", userID: "user-1", purposes: []string{DirectUploadPurposeAIEntryAttachment}, assertedKey: key,
			wantErr: ErrPendingUploadAccessDenied,
		},
		{
			name: "rejects invalid status",
			upload: &model.PendingUpload{
				ID: uploadID, UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment, Key: key,
				Status: model.PendingUploadStatusExpired, ExpiresAt: now.Add(time.Minute),
			},
			lookupID: uploadID, userID: "user-1", purposes: []string{DirectUploadPurposeAIEntryAttachment}, assertedKey: key,
			wantErr: ErrPendingUploadNotPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{tt.upload.ID: tt.upload}}
			got, err := ResolveDirectUploadAttachment(context.Background(), matchingDirectUploadStore(tt.upload), repo, tt.userID, tt.purposes, tt.lookupID, tt.assertedKey, now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if got != nil {
					t.Fatalf("result = %#v, want nil", got)
				}
				if len(repo.finalized) != 0 {
					t.Fatalf("finalized = %#v, want none", repo.finalized)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("result = %#v, want %#v", got, tt.want)
			}
			if tt.wantFinalized {
				if len(repo.finalized) != 1 || repo.finalized[0] != uploadID {
					t.Fatalf("finalized = %#v, want [%s]", repo.finalized, uploadID)
				}
			} else if len(repo.finalized) != 0 {
				t.Fatalf("finalized = %#v, want none", repo.finalized)
			}
		})
	}
}

func TestFinalizeVerifiedDirectUploadsRejectsLostPendingClaim(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	upload := &model.PendingUpload{
		ID: "upload-1", UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
		Key: "uploads/pending/user-1/upload-1/input.png", FileName: "input.png", ContentType: "image/png", Size: 1,
		Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
	}
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{upload.ID: upload}}
	verified, err := VerifyDirectUploadAttachment(context.Background(), repo, upload.UserID, []string{upload.Purpose}, upload.ID, upload.Key, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	upload.Status = model.PendingUploadStatusExpired

	err = FinalizeVerifiedDirectUploads(context.Background(), matchingDirectUploadStore(upload), repo, []*VerifiedDirectUpload{verified}, now)
	if !errors.Is(err, ErrPendingUploadNotPending) {
		t.Fatalf("finalize error = %v, want lost-claim validation error", err)
	}
	if len(repo.finalized) != 0 {
		t.Fatalf("lost claim finalized IDs = %#v", repo.finalized)
	}
}

func TestFinalizeVerifiedDirectUploadsRejectsOversizedActualObjectBeforeClaim(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC)
	upload := &model.PendingUpload{
		ID: "upload-oversized", UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
		Key: "uploads/pending/user-1/upload-oversized/input.png", FileName: "input.png",
		ContentType: "image/png", Size: 1, Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
	}
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{upload.ID: upload}}
	store := &fakeDirectUploadStore{objects: map[string]*storage.ObjectInfo{
		upload.Key: {Key: upload.Key, Size: 51 * 1024 * 1024, ContentType: "image/png"},
	}}
	verified, err := VerifyDirectUploadAttachment(context.Background(), repo, upload.UserID, []string{upload.Purpose}, upload.ID, upload.Key, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	err = finalizeVerifiedDirectUploadsWithStore(context.Background(), store, repo, []*VerifiedDirectUpload{verified}, now)
	if err == nil || (!strings.Contains(err.Error(), "size mismatch") && !strings.Contains(err.Error(), "exceeds")) {
		t.Fatalf("finalize error = %v, want actual object size rejection", err)
	}
	if upload.Status != model.PendingUploadStatusPending || len(repo.finalized) != 0 {
		t.Fatalf("metadata mismatch changed claim state: status=%s finalized=%#v", upload.Status, repo.finalized)
	}
}

func TestFinalizeVerifiedDirectUploadsValidatesNormalizedObjectContentType(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 45, 0, 0, time.UTC)
	for _, tt := range []struct {
		name       string
		actualType string
		wantErr    bool
	}{
		{name: "ignores parameters and case", actualType: "Image/PNG; charset=binary"},
		{name: "rejects missing metadata", actualType: "", wantErr: true},
		{name: "rejects substituted generic type", actualType: "application/octet-stream", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upload := &model.PendingUpload{
				ID: "upload-mime", UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
				Key: "uploads/pending/user-1/upload-mime/input.png", FileName: "input.png",
				ContentType: "image/png", Size: 1, Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
			}
			repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{upload.ID: upload}}
			verified, err := VerifyDirectUploadAttachment(context.Background(), repo, upload.UserID, []string{upload.Purpose}, upload.ID, upload.Key, now)
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			store := &fakeDirectUploadStore{objects: map[string]*storage.ObjectInfo{
				upload.Key: {Key: upload.Key, Size: upload.Size, ContentType: tt.actualType},
			}}
			err = FinalizeVerifiedDirectUploads(context.Background(), store, repo, []*VerifiedDirectUpload{verified}, now)
			if tt.wantErr && !errors.Is(err, ErrPendingUploadObjectInvalid) {
				t.Fatalf("error = %v, want metadata rejection", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("finalize: %v", err)
			}
			if tt.wantErr && upload.Status != model.PendingUploadStatusPending {
				t.Fatalf("metadata mismatch changed status to %s", upload.Status)
			}
		})
	}
}

func TestFinalizeVerifiedDirectUploadsValidatesEveryObjectBeforeClaimingAny(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 50, 0, 0, time.UTC)
	uploads := []*model.PendingUpload{
		{
			ID: "upload-a", UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
			Key: "uploads/pending/user-1/upload-a/a.png", FileName: "a.png", ContentType: "image/png", Size: 1,
			Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
		},
		{
			ID: "upload-b", UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
			Key: "uploads/pending/user-1/upload-b/b.png", FileName: "b.png", ContentType: "image/png", Size: 1,
			Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
		},
	}
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{uploads[0].ID: uploads[0], uploads[1].ID: uploads[1]}}
	verified := make([]*VerifiedDirectUpload, 0, len(uploads))
	for _, upload := range uploads {
		item, err := VerifyDirectUploadAttachment(context.Background(), repo, upload.UserID, []string{upload.Purpose}, upload.ID, upload.Key, now)
		if err != nil {
			t.Fatalf("verify %s: %v", upload.ID, err)
		}
		verified = append(verified, item)
	}
	store := matchingDirectUploadStore(uploads...)
	store.objects[uploads[1].Key].Size = 51 * 1024 * 1024

	err := FinalizeVerifiedDirectUploads(context.Background(), store, repo, verified, now)
	if !errors.Is(err, ErrPendingUploadObjectInvalid) {
		t.Fatalf("error = %v, want object metadata rejection", err)
	}
	if len(repo.finalized) != 0 || uploads[0].Status != model.PendingUploadStatusPending || uploads[1].Status != model.PendingUploadStatusPending {
		t.Fatalf("partial finalize: finalized=%#v statuses=%s,%s", repo.finalized, uploads[0].Status, uploads[1].Status)
	}
}

func TestFinalizeVerifiedDirectUploadsReusesConcurrentlyFinalizedIdentity(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)
	upload := &model.PendingUpload{
		ID: "upload-1", UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
		Key: "uploads/pending/user-1/upload-1/input.png", PublicURL: "https://cdn/upload-1", FileName: "input.png", ContentType: "image/png", Size: 1,
		Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.PendingUploads().CreatePendingUpload(ctx, upload); err != nil {
		t.Fatalf("create upload: %v", err)
	}
	first, err := VerifyDirectUploadAttachment(ctx, repo.PendingUploads(), upload.UserID, []string{upload.Purpose}, upload.ID, upload.Key, now)
	if err != nil {
		t.Fatalf("first verify: %v", err)
	}
	second, err := VerifyDirectUploadAttachment(ctx, repo.PendingUploads(), upload.UserID, []string{upload.Purpose}, upload.ID, upload.Key, now)
	if err != nil {
		t.Fatalf("second verify: %v", err)
	}
	store := matchingDirectUploadStore(upload)
	if err := FinalizeVerifiedDirectUploads(ctx, store, repo.PendingUploads(), []*VerifiedDirectUpload{first}, now); err != nil {
		t.Fatalf("first finalize: %v", err)
	}
	if err := FinalizeVerifiedDirectUploads(ctx, store, repo.PendingUploads(), []*VerifiedDirectUpload{second}, now.Add(time.Second)); err != nil {
		t.Fatalf("concurrent finalized reuse: %v", err)
	}
}

func TestFinalizePendingUploadURLsRejectsCrossUserAndExpired(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{
		"ok": {
			ID:          "ok",
			UserID:      "user-1",
			Purpose:     DirectUploadPurposeVideoReference,
			Key:         "uploads/pending/user-1/ok/ref.mp4",
			PublicURL:   "https://cdn.example.com/uploads/pending/user-1/ok/ref.mp4",
			FileName:    "ref.mp4",
			ContentType: "video/mp4",
			Size:        1,
			Status:      model.PendingUploadStatusPending,
			ExpiresAt:   now.Add(time.Minute),
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
	store := matchingDirectUploadStore(repo.uploads["ok"])
	if err := FinalizePendingUploadURLs(context.Background(), store, repo, "user-1", DirectUploadPurposeVideoReference, []string{
		"https://cdn.example.com/uploads/pending/user-1/ok/ref.mp4?x=1",
	}, now); err != nil {
		t.Fatalf("FinalizePendingUploadURLs ok: %v", err)
	}
	if len(repo.finalized) != 1 || repo.finalized[0] != "ok" {
		t.Fatalf("finalized = %v, want [ok]", repo.finalized)
	}
	if err := FinalizePendingUploadURLs(context.Background(), store, repo, "user-1", DirectUploadPurposeVideoReference, []string{
		"https://cdn.example.com/uploads/pending/user-2/other/ref.mp4",
	}, now); err == nil {
		t.Fatal("cross-user pending upload finalized, want error")
	}
	if err := FinalizePendingUploadURLs(context.Background(), store, repo, "user-1", DirectUploadPurposeVideoReference, []string{
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
		if err := FinalizePendingUploadURLs(context.Background(), matchingDirectUploadStore(repo.uploads["ok"]), repo, "user-1", DirectUploadPurposeVideoReference, []string{raw}, now); err == nil {
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

func TestCleanupExpiredPendingUploadsReopensClaimWhenDeleteFails(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	upload := &model.PendingUpload{
		ID: "expired", Key: "uploads/pending/user-1/expired/input.png",
		Status: model.PendingUploadStatusPending, ExpiresAt: now.Add(-time.Minute),
	}
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{upload.ID: upload}}
	store := &fakeDirectUploadStore{name: "oss", deleteErr: errors.New("delete failed")}

	cleaned, err := CleanupExpiredPendingUploads(context.Background(), store, repo, now, 100)
	if err == nil || err.Error() != "delete failed" {
		t.Fatalf("cleanup error = %v, want delete failed", err)
	}
	if cleaned != 0 {
		t.Fatalf("cleaned = %d, want 0", cleaned)
	}
	if upload.Status != model.PendingUploadStatusPending || upload.ExpiredAt != nil {
		t.Fatalf("failed delete left upload claimed: %#v", upload)
	}

	store.deleteErr = nil
	cleaned, err = CleanupExpiredPendingUploads(context.Background(), store, repo, now, 100)
	if err != nil {
		t.Fatalf("retry cleanup: %v", err)
	}
	if cleaned != 1 || upload.Status != model.PendingUploadStatusExpired {
		t.Fatalf("retry cleanup = %d, upload %#v", cleaned, upload)
	}
}

func TestCleanupExpiredPendingUploadsRecoversAbandonedClaim(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	staleClaim := now.Add(-10 * time.Minute)
	upload := &model.PendingUpload{
		ID: "abandoned", Key: "uploads/pending/user-1/abandoned/input.png",
		Status: model.PendingUploadStatusExpiring, ExpiresAt: now.Add(-time.Hour), CleanupClaimID: "stale-claim", CleanupClaimedAt: &staleClaim,
	}
	repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{upload.ID: upload}}
	store := &fakeDirectUploadStore{name: "oss"}

	cleaned, err := CleanupExpiredPendingUploads(context.Background(), store, repo, now, 100)
	if err != nil {
		t.Fatalf("cleanup abandoned claim: %v", err)
	}
	if cleaned != 1 || upload.Status != model.PendingUploadStatusExpired || upload.CleanupClaimedAt != nil {
		t.Fatalf("cleanup = %d, upload %#v", cleaned, upload)
	}
	if len(store.deleted) != 1 || store.deleted[0] != upload.Key {
		t.Fatalf("deleted keys = %#v", store.deleted)
	}
}
