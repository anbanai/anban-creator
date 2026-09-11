package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type referenceAssetStore struct {
	objects       map[string]*storage.ObjectInfo
	signedKeys    []string
	signedTTLs    []int
	promoteCalls  int
	downloadErr   error
	emptyDownload bool
}

func (s *referenceAssetStore) Name() string { return "oss" }
func (s *referenceAssetStore) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, errors.New("unexpected upload")
}
func (s *referenceAssetStore) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("unexpected upload")
}
func (s *referenceAssetStore) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("unexpected upload URL")
}
func (s *referenceAssetStore) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (s *referenceAssetStore) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("unexpected read")
}
func (s *referenceAssetStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}
func (s *referenceAssetStore) DownloadURL(_ context.Context, key string, ttl int) (string, error) {
	s.signedKeys = append(s.signedKeys, key)
	s.signedTTLs = append(s.signedTTLs, ttl)
	if s.downloadErr != nil {
		return "", s.downloadErr
	}
	if s.emptyDownload {
		return "", nil
	}
	return "https://download.example.com/" + key, nil
}
func (s *referenceAssetStore) HasCustomDomain() bool  { return false }
func (s *referenceAssetStore) IsOwnedURL(string) bool { return false }
func (s *referenceAssetStore) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
	info := s.objects[key]
	if info == nil {
		return nil, storage.ErrObjectNotFound
	}
	copy := *info
	return &copy, nil
}
func (s *referenceAssetStore) PromoteObject(_ context.Context, sourceKey, finalKey, expectedETag string) (*storage.ObjectInfo, error) {
	s.promoteCalls++
	if s.objects[finalKey] != nil {
		return nil, storage.ErrObjectAlreadyExists
	}
	source := s.objects[sourceKey]
	if source == nil {
		return nil, storage.ErrObjectNotFound
	}
	if source.ETag != expectedETag {
		return nil, storage.ErrPromotionPreconditionFailed
	}
	copy := *source
	copy.Key = finalKey
	s.objects[finalKey] = &copy
	return &copy, nil
}

type providerOnlyReferenceAssetStore struct {
	base *referenceAssetStore
}

type referenceAssetRepositoryOverride struct {
	repository.Repository
	uploadSessions repository.UploadSessionRepository
	assets         repository.AssetRepository
}

func (r *referenceAssetRepositoryOverride) UploadSessions() repository.UploadSessionRepository {
	if r.uploadSessions != nil {
		return r.uploadSessions
	}
	return r.Repository.UploadSessions()
}

func (r *referenceAssetRepositoryOverride) Assets() repository.AssetRepository {
	if r.assets != nil {
		return r.assets
	}
	return r.Repository.Assets()
}

type failingReferenceUploadSessionRepository struct {
	repository.UploadSessionRepository
	err error
}

func (r *failingReferenceUploadSessionRepository) FindByID(context.Context, string) (*model.UploadSession, error) {
	return nil, r.err
}

type failingReferenceAssetRepository struct {
	repository.AssetRepository
	err error
}

func (r *failingReferenceAssetRepository) FindOwnedByID(context.Context, string, string) (*model.Asset, error) {
	return nil, r.err
}

func (s *providerOnlyReferenceAssetStore) Name() string { return s.base.Name() }
func (s *providerOnlyReferenceAssetStore) Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	return s.base.Upload(ctx, key, reader, contentType)
}
func (s *providerOnlyReferenceAssetStore) UploadFile(ctx context.Context, key, filePath, contentType string) (*storage.UploadResult, error) {
	return s.base.UploadFile(ctx, key, filePath, contentType)
}
func (s *providerOnlyReferenceAssetStore) UploadURL(ctx context.Context, key, contentType string, ttl int) (string, error) {
	return s.base.UploadURL(ctx, key, contentType, ttl)
}
func (s *providerOnlyReferenceAssetStore) GetURL(key string) string { return s.base.GetURL(key) }
func (s *providerOnlyReferenceAssetStore) Read(ctx context.Context, key string) ([]byte, error) {
	return s.base.Read(ctx, key)
}
func (s *providerOnlyReferenceAssetStore) Delete(ctx context.Context, key string) error {
	return s.base.Delete(ctx, key)
}
func (s *providerOnlyReferenceAssetStore) DownloadURL(ctx context.Context, key string, ttl int) (string, error) {
	return s.base.DownloadURL(ctx, key, ttl)
}
func (s *providerOnlyReferenceAssetStore) HasCustomDomain() bool { return s.base.HasCustomDomain() }
func (s *providerOnlyReferenceAssetStore) IsOwnedURL(rawURL string) bool {
	return s.base.IsOwnedURL(rawURL)
}

func TestReferenceImageSelectionValidate(t *testing.T) {
	tests := []struct {
		name string
		in   ReferenceImageSelection
		ok   bool
	}{
		{name: "asset", in: ReferenceImageSelection{AssetID: "asset-1"}, ok: true},
		{name: "session", in: ReferenceImageSelection{UploadSessionID: "session-1"}, ok: true},
		{name: "both", in: ReferenceImageSelection{AssetID: "asset-1", UploadSessionID: "session-1"}, ok: false},
		{name: "empty", in: ReferenceImageSelection{}, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Validate() == nil; got != tt.ok {
				t.Fatalf("Validate() success = %v, want %v", got, tt.ok)
			}
		})
	}
}

func TestIsReferenceAssetError(t *testing.T) {
	for _, err := range []error{
		ErrReferenceImageSelectionInvalid,
		ErrReferenceAssetPurposeMismatch,
		ErrReferenceAssetInvalidMetadata,
		ErrReferenceAssetForbidden,
		ErrReferenceAssetConcurrentFinalization,
		ErrReferenceAssetExpired,
		ErrReferenceAssetUnavailable,
	} {
		if !IsReferenceAssetError(fmt.Errorf("wrapped: %w", err)) {
			t.Fatalf("IsReferenceAssetError(%v) = false", err)
		}
	}
	if IsReferenceAssetError(errors.New("other")) {
		t.Fatal("unknown error classified as reference asset error")
	}
}

func TestReferenceAssetServiceResolveSelection(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	newService := func(t *testing.T) (*ReferenceAssetService, repository.Repository, *referenceAssetStore) {
		t.Helper()
		repo := newDirectUploadTestRepository(t)
		store := &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo)}
		return NewReferenceAssetService(repo, store, func() time.Time { return now }), repo, store
	}

	t.Run("session finalization", func(t *testing.T) {
		svc, repo, store := newService(t)
		session := &model.UploadSession{
			ID: "session-1", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
			StagingKey: "uploads/pending/user-1/session-1/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
			ExpiresAt: now.Add(time.Minute),
		}
		seedUploadSession(t, repo, session)
		store.objects[session.StagingKey] = &storage.ObjectInfo{
			Key: session.StagingKey, Size: session.Size, ContentType: session.ContentType, ETag: "etag-1",
		}

		assetID, err := svc.ResolveSelection(t.Context(), session.UserID, ReferenceImageSelection{UploadSessionID: session.ID}, []string{DirectUploadPurposeProjectReference})
		if err != nil {
			t.Fatalf("ResolveSelection: %v", err)
		}
		if assetID != session.ID || store.promoteCalls != 1 {
			t.Fatalf("asset ID = %q, promotions = %d; want %q, 1", assetID, store.promoteCalls, session.ID)
		}
	})

	t.Run("existing owned asset reuse", func(t *testing.T) {
		svc, repo, store := newService(t)
		asset := referenceAssetFixture("asset-1", "user-1", DirectUploadPurposeProjectReference)
		seedReferenceAsset(t, repo, asset)

		assetID, err := svc.ResolveSelection(t.Context(), asset.UserID, ReferenceImageSelection{AssetID: asset.ID}, []string{asset.Purpose})
		if err != nil {
			t.Fatalf("ResolveSelection: %v", err)
		}
		if assetID != asset.ID || store.promoteCalls != 0 {
			t.Fatalf("asset ID = %q, promotions = %d; want %q, 0", assetID, store.promoteCalls, asset.ID)
		}
	})

	t.Run("wrong purpose", func(t *testing.T) {
		svc, repo, _ := newService(t)
		asset := referenceAssetFixture("asset-wrong-purpose", "user-1", DirectUploadPurposeTaskReference)
		seedReferenceAsset(t, repo, asset)
		_, err := svc.ResolveSelection(t.Context(), asset.UserID, ReferenceImageSelection{AssetID: asset.ID}, []string{DirectUploadPurposeProjectReference})
		if !errors.Is(err, ErrReferenceAssetPurposeMismatch) {
			t.Fatalf("error = %v, want ErrReferenceAssetPurposeMismatch", err)
		}
	})

	t.Run("wrong session purpose", func(t *testing.T) {
		svc, repo, _ := newService(t)
		session := &model.UploadSession{
			ID: "session-wrong-purpose", UserID: "user-1", Purpose: DirectUploadPurposeTaskReference,
			StagingKey: "uploads/pending/user-1/session-wrong-purpose/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
			ExpiresAt: now.Add(time.Minute),
		}
		seedUploadSession(t, repo, session)
		_, err := svc.ResolveSelection(t.Context(), session.UserID, ReferenceImageSelection{UploadSessionID: session.ID}, []string{DirectUploadPurposeProjectReference})
		if !errors.Is(err, ErrReferenceAssetPurposeMismatch) {
			t.Fatalf("error = %v, want ErrReferenceAssetPurposeMismatch", err)
		}
	})

	t.Run("foreign asset", func(t *testing.T) {
		svc, repo, _ := newService(t)
		asset := referenceAssetFixture("asset-foreign", "user-2", DirectUploadPurposeProjectReference)
		seedReferenceAsset(t, repo, asset)
		_, err := svc.ResolveSelection(t.Context(), "user-1", ReferenceImageSelection{AssetID: asset.ID}, []string{asset.Purpose})
		if !errors.Is(err, ErrReferenceAssetForbidden) {
			t.Fatalf("error = %v, want ErrReferenceAssetForbidden", err)
		}
	})

	t.Run("missing asset", func(t *testing.T) {
		svc, _, _ := newService(t)
		_, err := svc.ResolveSelection(t.Context(), "user-1", ReferenceImageSelection{AssetID: "missing"}, []string{DirectUploadPurposeProjectReference})
		if !errors.Is(err, ErrReferenceAssetForbidden) {
			t.Fatalf("error = %v, want ErrReferenceAssetForbidden", err)
		}
	})

	t.Run("foreign and missing session are opaque", func(t *testing.T) {
		svc, repo, _ := newService(t)
		foreign := &model.UploadSession{
			ID: "session-foreign", UserID: "user-2", Purpose: DirectUploadPurposeProjectReference,
			StagingKey: "uploads/pending/user-2/session-foreign/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
			ExpiresAt: now.Add(time.Minute),
		}
		seedUploadSession(t, repo, foreign)
		for _, id := range []string{foreign.ID, "session-missing"} {
			_, err := svc.ResolveSelection(t.Context(), "user-1", ReferenceImageSelection{UploadSessionID: id}, []string{foreign.Purpose})
			if !errors.Is(err, ErrReferenceAssetForbidden) {
				t.Fatalf("session %q error = %v, want ErrReferenceAssetForbidden", id, err)
			}
		}
	})
}

func TestReferenceAssetServicePresentSignsRepositoryKey(t *testing.T) {
	now := time.Date(2026, 7, 17, 19, 0, 0, 0, time.UTC)
	repo := newDirectUploadTestRepository(t)
	asset := referenceAssetFixture("asset-1", "user-1", DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	store := &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo)}
	svc := NewReferenceAssetService(repo, store, func() time.Time { return now })

	view, err := svc.Present(t.Context(), asset.UserID, asset.ID, []string{asset.Purpose})
	if err != nil {
		t.Fatalf("Present: %v", err)
	}
	if view.AssetID != asset.ID || view.DownloadURL != "https://download.example.com/"+asset.StorageKey {
		t.Fatalf("view = %#v", view)
	}
	if !view.DownloadExpiresAt.Equal(now.Add(DefaultSignedURLTTL * time.Second)) {
		t.Fatalf("DownloadExpiresAt = %s", view.DownloadExpiresAt)
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
		t.Fatalf("signed keys = %#v, want repository key %q", store.signedKeys, asset.StorageKey)
	}
	if len(store.signedTTLs) != 1 || store.signedTTLs[0] != DefaultSignedURLTTL {
		t.Fatalf("signed TTLs = %#v, want %d", store.signedTTLs, DefaultSignedURLTTL)
	}
}

func TestReferenceAssetServiceRejectsInvalidImageMetadata(t *testing.T) {
	repo := newDirectUploadTestRepository(t)
	asset := referenceAssetFixture("asset-video", "user-1", DirectUploadPurposeAIEntryAttachment)
	asset.FileName = "reference.mp4"
	asset.ContentType = "video/mp4"
	seedReferenceAsset(t, repo, asset)
	svc := NewReferenceAssetService(repo, &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo)}, time.Now)

	_, err := svc.RequireOwned(t.Context(), asset.UserID, asset.ID, []string{asset.Purpose})
	if !errors.Is(err, ErrReferenceAssetInvalidMetadata) {
		t.Fatalf("error = %v, want ErrReferenceAssetInvalidMetadata", err)
	}
}

func TestReferenceAssetServiceExistingAssetWorksWithoutPromotionCapability(t *testing.T) {
	repo := newDirectUploadTestRepository(t)
	asset := referenceAssetFixture("asset-provider-only", "user-1", DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	base := &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo)}
	store := &providerOnlyReferenceAssetStore{base: base}
	svc := NewReferenceAssetService(repo, store, time.Now)

	assetID, err := svc.ResolveSelection(t.Context(), asset.UserID, ReferenceImageSelection{AssetID: asset.ID}, []string{asset.Purpose})
	if err != nil || assetID != asset.ID {
		t.Fatalf("ResolveSelection = %q, %v; want %q, nil", assetID, err, asset.ID)
	}
	if _, err := svc.Present(t.Context(), asset.UserID, asset.ID, []string{asset.Purpose}); err != nil {
		t.Fatalf("Present with provider-only store: %v", err)
	}
}

func TestReferenceAssetServiceSessionRequiresPromotionCapability(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	repo := newDirectUploadTestRepository(t)
	session := &model.UploadSession{
		ID: "session-no-promoter", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/user-1/session-no-promoter/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
		ExpiresAt: now.Add(time.Minute),
	}
	seedUploadSession(t, repo, session)
	base := &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo)}
	svc := NewReferenceAssetService(repo, &providerOnlyReferenceAssetStore{base: base}, func() time.Time { return now })

	_, err := svc.ResolveSelection(t.Context(), session.UserID, ReferenceImageSelection{UploadSessionID: session.ID}, []string{session.Purpose})
	if !errors.Is(err, ErrReferenceAssetUnavailable) {
		t.Fatalf("error = %v, want ErrReferenceAssetUnavailable", err)
	}
}

func TestReferenceAssetServicePreservesRepositoryErrorChains(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		resolve   func(context.Context, *ReferenceAssetService) error
		wrapRepo  func(repository.Repository, error) repository.Repository
	}{
		{
			name: "find session", operation: "find upload session",
			resolve: func(ctx context.Context, svc *ReferenceAssetService) error {
				_, err := svc.ResolveSelection(ctx, "user-1", ReferenceImageSelection{UploadSessionID: "session-1"}, []string{DirectUploadPurposeProjectReference})
				return err
			},
			wrapRepo: func(base repository.Repository, err error) repository.Repository {
				return &referenceAssetRepositoryOverride{Repository: base, uploadSessions: &failingReferenceUploadSessionRepository{UploadSessionRepository: base.UploadSessions(), err: err}}
			},
		},
		{
			name: "find asset", operation: "find asset",
			resolve: func(ctx context.Context, svc *ReferenceAssetService) error {
				_, err := svc.RequireOwned(ctx, "user-1", "asset-1", []string{DirectUploadPurposeProjectReference})
				return err
			},
			wrapRepo: func(base repository.Repository, err error) repository.Repository {
				return &referenceAssetRepositoryOverride{Repository: base, assets: &failingReferenceAssetRepository{AssetRepository: base.Assets(), err: err}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := newDirectUploadTestRepository(t)
			rootCause := context.Canceled
			svc := NewReferenceAssetService(tt.wrapRepo(base, rootCause), &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo)}, time.Now)
			err := tt.resolve(t.Context(), svc)
			assertReferenceErrorChain(t, err, tt.operation, ErrReferenceAssetUnavailable, rootCause)
		})
	}
}

func TestReferenceAssetServicePresentPreservesSigningErrorChain(t *testing.T) {
	repo := newDirectUploadTestRepository(t)
	asset := referenceAssetFixture("asset-sign-error", "user-1", DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	rootCause := context.DeadlineExceeded
	store := &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo), downloadErr: rootCause}
	svc := NewReferenceAssetService(repo, store, time.Now)

	_, err := svc.Present(t.Context(), asset.UserID, asset.ID, []string{asset.Purpose})
	assertReferenceErrorChain(t, err, "sign asset download", ErrReferenceAssetUnavailable, rootCause)
}

func TestReferenceAssetServicePresentClassifiesEmptySignedURL(t *testing.T) {
	repo := newDirectUploadTestRepository(t)
	asset := referenceAssetFixture("asset-empty-sign", "user-1", DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	store := &referenceAssetStore{objects: make(map[string]*storage.ObjectInfo), emptyDownload: true}
	svc := NewReferenceAssetService(repo, store, time.Now)

	_, err := svc.Present(t.Context(), asset.UserID, asset.ID, []string{asset.Purpose})
	assertReferenceErrorChain(t, err, "sign asset download", ErrReferenceAssetUnavailable, errReferenceAssetEmptyDownloadURL)
}

func TestMapReferenceFinalizationErrorPreservesClassifiedAndRootCauses(t *testing.T) {
	rootCause := errors.New("root finalization failure")
	tests := []struct {
		name           string
		finalizeClass  error
		referenceClass error
	}{
		{name: "access denied", finalizeClass: ErrUploadSessionAccessDenied, referenceClass: ErrReferenceAssetForbidden},
		{name: "expired", finalizeClass: ErrUploadSessionExpired, referenceClass: ErrReferenceAssetExpired},
		{name: "conflict", finalizeClass: ErrUploadSessionStateConflict, referenceClass: ErrReferenceAssetConcurrentFinalization},
		{name: "metadata", finalizeClass: ErrUploadSessionObjectInvalid, referenceClass: ErrReferenceAssetInvalidMetadata},
		{name: "unavailable", finalizeClass: ErrUploadSessionUnavailable, referenceClass: ErrReferenceAssetUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalizeErr := fmt.Errorf("provider operation: %w", errors.Join(tt.finalizeClass, rootCause))
			err := mapReferenceFinalizationError(finalizeErr)
			assertReferenceErrorChain(t, err, "finalize upload session", tt.referenceClass, tt.finalizeClass, rootCause)
		})
	}
	unknown := errors.New("unknown finalization failure")
	assertReferenceErrorChain(t, mapReferenceFinalizationError(unknown), "finalize upload session", ErrReferenceAssetUnavailable, unknown)
}

func assertReferenceErrorChain(t *testing.T, err error, operation string, causes ...error) {
	t.Helper()
	if err == nil {
		t.Fatal("error is nil")
	}
	if !strings.Contains(err.Error(), operation) {
		t.Fatalf("error = %q, want operation %q", err, operation)
	}
	for _, cause := range causes {
		if !errors.Is(err, cause) {
			t.Fatalf("error = %v, want errors.Is(..., %v)", err, cause)
		}
	}
}

func referenceAssetFixture(id, userID, purpose string) *model.Asset {
	return &model.Asset{
		ID: id, UserID: userID, Purpose: purpose,
		StorageKey: "assets/users/" + userID + "/" + id + "/ref.png",
		FileName:   "ref.png", ContentType: "image/png", Size: 1024, ETag: "etag-" + id,
	}
}

func TestReferenceAssetIDsRemainSourceSpecific(t *testing.T) {
	task := &model.Task{ReferenceImageAssetID: "task-asset", SkipReferenceImage: true}
	task.SetProjectSnapshot(model.ProjectSnapshot{ReferenceImageAssetID: "project-asset"})

	if got := taskReferenceAssetID(task); got != "task-asset" {
		t.Fatalf("direct reference with skip = %q, want task-asset", got)
	}
	if got := projectStyleReferenceAssetID(task); got != "" {
		t.Fatalf("inherited reference with skip = %q, want empty", got)
	}
	task.SkipReferenceImage = false
	if got := projectStyleReferenceAssetID(task); got != "project-asset" {
		t.Fatalf("inherited reference = %q, want project-asset", got)
	}
	if got := taskReferenceAssetID(nil); got != "" {
		t.Fatalf("nil direct task reference = %q, want empty", got)
	}
	if got := projectStyleReferenceAssetID(nil); got != "" {
		t.Fatalf("nil project style reference = %q, want empty", got)
	}
}

func TestResolveReferenceAssetValidatesPurposeBySource(t *testing.T) {
	tests := []struct {
		name       string
		direct     bool
		purpose    string
		wantErr    error
		foreign    bool
		missing    bool
		badContent bool
	}{
		{name: "direct task reference", direct: true, purpose: DirectUploadPurposeTaskReference},
		{name: "direct AI entry attachment", direct: true, purpose: DirectUploadPurposeAIEntryAttachment},
		{name: "direct rejects project purpose", direct: true, purpose: DirectUploadPurposeProjectReference, wantErr: ErrReferenceAssetPurposeMismatch},
		{name: "snapshot project reference", purpose: DirectUploadPurposeProjectReference},
		{name: "snapshot rejects task purpose", purpose: DirectUploadPurposeTaskReference, wantErr: ErrReferenceAssetPurposeMismatch},
		{name: "foreign is opaque", direct: true, purpose: DirectUploadPurposeTaskReference, foreign: true, wantErr: ErrReferenceAssetForbidden},
		{name: "missing is opaque", direct: true, missing: true, wantErr: ErrReferenceAssetForbidden},
		{name: "invalid metadata fails closed", direct: true, purpose: DirectUploadPurposeTaskReference, badContent: true, wantErr: ErrReferenceAssetInvalidMetadata},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newDirectUploadTestRepository(t)
			owner := "user-1"
			assetID := "asset-1"
			if !tt.missing {
				assetOwner := owner
				if tt.foreign {
					assetOwner = "user-2"
				}
				asset := referenceAssetFixture(assetID, assetOwner, tt.purpose)
				if tt.badContent {
					asset.FileName, asset.ContentType = "reference.txt", "text/plain"
				}
				seedReferenceAsset(t, repo, asset)
			}
			task := &model.Task{UserID: owner}
			if tt.direct {
				task.ReferenceImageAssetID = assetID
			} else {
				task.SetProjectSnapshot(model.ProjectSnapshot{ReferenceImageAssetID: assetID})
			}

			var asset *model.Asset
			var err error
			if tt.direct {
				asset, err = resolveTaskReferenceAsset(t.Context(), repo, task)
			} else {
				asset, err = resolveProjectStyleReferenceAsset(t.Context(), repo, task)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || asset != nil {
					t.Fatalf("resolve asset=%#v err=%v, want %v", asset, err, tt.wantErr)
				}
				return
			}
			if err != nil || asset == nil || asset.ID != assetID {
				t.Fatalf("resolve asset=%#v err=%v, want %s", asset, err, assetID)
			}
		})
	}
}

func seedReferenceAsset(t *testing.T, repo repository.Repository, asset *model.Asset) {
	t.Helper()
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
}

var _ storage.Provider = (*referenceAssetStore)(nil)
var _ storage.Provider = (*providerOnlyReferenceAssetStore)(nil)
