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
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeDirectUploadStore struct {
	name        string
	uploadKey   string
	contentType string
	expires     int
	deleted     []string
	deleteErr   error
	deleteErrs  map[string]error
	objects     map[string]*storage.ObjectInfo
	statErr     error
	statHook    func(context.Context, string)
	promoteHook func(sourceKey, finalKey string)
	promoteErr  error
	promoteETag string
	promoted    []string
	readCalls   int
}

func (s *fakeDirectUploadStore) PromoteObject(_ context.Context, sourceKey, finalKey, expectedETag string) (*storage.ObjectInfo, error) {
	if s.promoteHook != nil {
		s.promoteHook(sourceKey, finalKey)
	}
	if s.promoteErr != nil {
		return nil, s.promoteErr
	}
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
	if s.promoteETag != "" {
		copy.ETag = s.promoteETag
	}
	s.objects[finalKey] = &copy
	s.promoted = append(s.promoted, sourceKey+"->"+finalKey)
	return &copy, nil
}

func (s *fakeDirectUploadStore) Read(_ context.Context, _ string) ([]byte, error) {
	s.readCalls++
	return nil, errors.New("unexpected object body read")
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
	if err := s.deleteErrs[key]; err != nil {
		return err
	}
	return s.deleteErr
}
func (s *fakeDirectUploadStore) StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	if s.statHook != nil {
		s.statHook(ctx, key)
	}
	if s.statErr != nil {
		return nil, s.statErr
	}
	info := s.objects[key]
	if info == nil {
		return nil, storage.ErrObjectNotFound
	}
	copy := *info
	return &copy, nil
}

type fakeUploadSessionRepo struct {
	createdSession *model.UploadSession
}

func (r *fakeUploadSessionRepo) Create(_ context.Context, session *model.UploadSession) error {
	cp := *session
	r.createdSession = &cp
	return nil
}

func (r *fakeUploadSessionRepo) FindByID(context.Context, string) (*model.UploadSession, error) {
	return nil, model.ErrUploadSessionNotFound
}

func (r *fakeUploadSessionRepo) ClaimFinalization(context.Context, string, string, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) RecordPromotionSourceETag(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) RecordFinalizationETag(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) ClaimFinalizationRecovery(context.Context, string, string, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) CompleteFinalization(context.Context, string, string, string, time.Time) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) ReleaseFinalization(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) FindForCleanup(context.Context, time.Time, time.Time, int) ([]*model.UploadSession, error) {
	return nil, nil
}

func (r *fakeUploadSessionRepo) ClaimExpiration(context.Context, string, string, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) CompleteExpiration(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) ReopenExpiration(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r *fakeUploadSessionRepo) RescheduleExpiration(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}

func newDirectUploadTestRepository(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:direct-upload-"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return repository.New(db)
}

func seedUploadSession(t *testing.T, repo repository.Repository, session *model.UploadSession) {
	t.Helper()
	if err := repo.UploadSessions().Create(t.Context(), session); err != nil {
		t.Fatalf("seed upload session: %v", err)
	}
}

func matchingUploadSessionStore(session *model.UploadSession) *fakeDirectUploadStore {
	return &fakeDirectUploadStore{objects: map[string]*storage.ObjectInfo{
		session.StagingKey: {
			Key: session.StagingKey, Size: session.Size,
			ContentType: session.ContentType, ETag: "etag-" + session.ID,
		},
	}}
}

func TestFinalizeUploadSessionUsesPromotionTargetETagWithoutReadingBody(t *testing.T) {
	now := time.Date(2026, 7, 17, 15, 0, 0, 0, time.UTC)
	repo := newDirectUploadTestRepository(t)
	session := &model.UploadSession{
		ID: "session-1", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/user-1/session-1/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
		ExpiresAt: now.Add(time.Hour),
	}
	seedUploadSession(t, repo, session)
	store := matchingUploadSessionStore(session)
	store.promoteETag = "etag-final-session-1"

	asset, err := FinalizeUploadSession(t.Context(), store, repo, FinalizeUploadRequest{
		SessionID: session.ID, UserID: session.UserID,
		AllowedPurposes: []string{DirectUploadPurposeProjectReference}, Now: now,
	})
	if err != nil {
		t.Fatalf("FinalizeUploadSession: %v", err)
	}
	if asset.ID != session.ID || asset.StorageKey != "assets/users/user-1/session-1/ref.png" {
		t.Fatalf("asset identity = %#v", asset)
	}
	if asset.UserID != session.UserID || asset.Purpose != session.Purpose || asset.FileName != session.FileName || asset.ContentType != session.ContentType || asset.Size != session.Size || asset.ETag != "etag-final-session-1" {
		t.Fatalf("asset metadata = %#v", asset)
	}
	finalized, err := repo.UploadSessions().FindByID(t.Context(), session.ID)
	if err != nil || finalized.PromotionSourceETag != "etag-session-1" || finalized.FinalizationETag != asset.ETag || finalized.PromotionSourceETag == finalized.FinalizationETag {
		t.Fatalf("source/target fingerprints = %#v, %v", finalized, err)
	}
	if store.readCalls != 0 {
		t.Fatalf("storage body reads = %d, want 0", store.readCalls)
	}
	wantPromotion := session.StagingKey + "->" + asset.StorageKey
	if len(store.promoted) != 1 || store.promoted[0] != wantPromotion {
		t.Fatalf("promotions = %#v, want [%q]", store.promoted, wantPromotion)
	}
}

type failFirstUploadFinalizationTx struct {
	repository.Repository
	fail bool
}

type uploadSessionRepositoryOverride struct {
	repository.Repository
	uploadSessions repository.UploadSessionRepository
}

func (r *uploadSessionRepositoryOverride) UploadSessions() repository.UploadSessionRepository {
	return r.uploadSessions
}

type failFirstTargetFingerprintRepository struct {
	repository.UploadSessionRepository
	fail bool
}

func (r *failFirstTargetFingerprintRepository) RecordFinalizationETag(ctx context.Context, id, token, etag string) (bool, error) {
	if r.fail {
		r.fail = false
		return false, errors.New("injected target fingerprint database failure")
	}
	return r.UploadSessionRepository.RecordFinalizationETag(ctx, id, token, etag)
}

func (r *failFirstUploadFinalizationTx) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	if !r.fail {
		return r.Repository.WithTx(ctx, fn)
	}
	r.fail = false
	return r.Repository.WithTx(ctx, func(txRepo repository.Repository) error {
		if err := fn(txRepo); err != nil {
			return err
		}
		return errors.New("injected post-promotion database failure")
	})
}

func TestFinalizeUploadSessionCompletesAfterCopyBeforeDatabaseFailure(t *testing.T) {
	now := time.Date(2026, 7, 17, 15, 10, 0, 0, time.UTC)
	baseRepo := newDirectUploadTestRepository(t)
	repo := &failFirstUploadFinalizationTx{Repository: baseRepo, fail: true}
	session := &model.UploadSession{
		ID: "session-retry", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/user-1/session-retry/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
		ExpiresAt: now.Add(time.Hour),
	}
	seedUploadSession(t, repo, session)
	store := matchingUploadSessionStore(session)
	store.promoteETag = "etag-final-session-retry"
	req := FinalizeUploadRequest{SessionID: session.ID, UserID: session.UserID, AllowedPurposes: []string{session.Purpose}, Now: now}

	if _, err := FinalizeUploadSession(t.Context(), store, repo, req); err == nil {
		t.Fatal("first finalization unexpectedly succeeded")
	}
	failed, err := baseRepo.UploadSessions().FindByID(t.Context(), session.ID)
	if err != nil || failed.FinalizationETag != "etag-final-session-retry" {
		t.Fatalf("failed finalization fingerprint = %#v, %v", failed, err)
	}
	delete(store.objects, session.StagingKey)
	req.Now = now.Add(2 * time.Hour)
	asset, err := FinalizeUploadSession(t.Context(), store, repo, req)
	if err != nil {
		t.Fatalf("retry FinalizeUploadSession: %v", err)
	}
	if asset.ID != session.ID {
		t.Fatalf("asset ID = %q, want %q", asset.ID, session.ID)
	}
	if len(store.promoted) != 1 {
		t.Fatalf("successful promotions = %#v, want one immutable copy", store.promoted)
	}
	finalized, err := baseRepo.UploadSessions().FindByID(t.Context(), session.ID)
	if err != nil || finalized.Status != model.UploadSessionFinalized || finalized.AssetID != session.ID || finalized.FinalizationETag != "etag-final-session-retry" || asset.ETag != finalized.FinalizationETag {
		t.Fatalf("recovered upload session = %#v, %v", finalized, err)
	}
}

func TestFinalizeUploadSessionRecoversWhenTargetFingerprintWriteFailsAfterCopy(t *testing.T) {
	now := time.Date(2026, 7, 17, 15, 20, 0, 0, time.UTC)
	baseRepo := newDirectUploadTestRepository(t)
	sessions := &failFirstTargetFingerprintRepository{UploadSessionRepository: baseRepo.UploadSessions(), fail: true}
	repo := &uploadSessionRepositoryOverride{Repository: baseRepo, uploadSessions: sessions}
	session := &model.UploadSession{
		ID: "session-fingerprint-retry", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/user-1/session-fingerprint-retry/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
		ExpiresAt: now.Add(time.Hour),
	}
	seedUploadSession(t, repo, session)
	store := matchingUploadSessionStore(session)
	store.promoteETag = "etag-target-fingerprint-retry"
	req := FinalizeUploadRequest{SessionID: session.ID, UserID: session.UserID, AllowedPurposes: []string{session.Purpose}, Now: now}

	if _, err := FinalizeUploadSession(t.Context(), store, repo, req); !errors.Is(err, ErrUploadSessionUnavailable) {
		t.Fatalf("first finalization error = %v, want ErrUploadSessionUnavailable", err)
	}
	interrupted, err := baseRepo.UploadSessions().FindByID(t.Context(), session.ID)
	if err != nil || interrupted.Status != model.UploadSessionPending || interrupted.PromotionSourceETag != "etag-session-fingerprint-retry" || interrupted.FinalizationETag != "" {
		t.Fatalf("interrupted fingerprints = %#v, %v", interrupted, err)
	}
	if store.objects["assets/users/user-1/session-fingerprint-retry/ref.png"] == nil {
		t.Fatal("promotion target missing after target fingerprint write failure")
	}

	req.Now = now.Add(2 * time.Hour)
	asset, err := FinalizeUploadSession(t.Context(), store, repo, req)
	if err != nil {
		t.Fatalf("retry FinalizeUploadSession: %v", err)
	}
	if asset.ETag != "etag-target-fingerprint-retry" {
		t.Fatalf("recovered asset ETag = %q", asset.ETag)
	}
	if len(store.promoted) != 1 {
		t.Fatalf("promotions = %#v, want one immutable copy", store.promoted)
	}
	finalized, err := baseRepo.UploadSessions().FindByID(t.Context(), session.ID)
	if err != nil || finalized.Status != model.UploadSessionFinalized || finalized.FinalizationETag != asset.ETag {
		t.Fatalf("recovered session = %#v, %v", finalized, err)
	}
}

func TestFinalizeUploadSessionRejectsUnverifiedExistingFinalObject(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name        string
		fingerprint string
	}{
		{name: "missing persisted fingerprint"},
		{name: "mismatched persisted fingerprint", fingerprint: "etag-other"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newDirectUploadTestRepository(t)
			session := &model.UploadSession{
				ID: "unverified-final", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
				StagingKey: "uploads/pending/user-1/unverified-final/ref.png", FileName: "ref.png",
				ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
				ExpiresAt: now.Add(time.Hour), FinalizationETag: tt.fingerprint,
			}
			seedUploadSession(t, repo, session)
			store := matchingUploadSessionStore(session)
			finalKey := "assets/users/user-1/unverified-final/ref.png"
			store.objects[finalKey] = &storage.ObjectInfo{Key: finalKey, Size: session.Size, ContentType: session.ContentType, ETag: "etag-unverified-final"}

			_, err := FinalizeUploadSession(t.Context(), store, repo, FinalizeUploadRequest{
				SessionID: session.ID, UserID: session.UserID, AllowedPurposes: []string{session.Purpose}, Now: now,
			})
			if !errors.Is(err, ErrUploadSessionObjectInvalid) {
				t.Fatalf("FinalizeUploadSession error = %v, want ErrUploadSessionObjectInvalid", err)
			}
			if _, err := repo.Assets().FindByID(t.Context(), session.ID); !errors.Is(err, model.ErrAssetNotFound) {
				t.Fatalf("unverified final object was adopted: %v", err)
			}
		})
	}
}

func TestCleanupExpiredUploadSessionsRecoversPromotedFinalObject(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 30, 0, 0, time.UTC)
	baseRepo := newDirectUploadTestRepository(t)
	sessions := &failFirstTargetFingerprintRepository{UploadSessionRepository: baseRepo.UploadSessions(), fail: true}
	repo := &uploadSessionRepositoryOverride{Repository: baseRepo, uploadSessions: sessions}
	session := &model.UploadSession{
		ID: "cleanup-recovery", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/user-1/cleanup-recovery/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
		ExpiresAt: now.Add(time.Hour),
	}
	seedUploadSession(t, repo, session)
	store := matchingUploadSessionStore(session)
	store.promoteETag = "etag-final-cleanup-recovery"
	if _, err := FinalizeUploadSession(t.Context(), store, repo, FinalizeUploadRequest{
		SessionID: session.ID, UserID: session.UserID, AllowedPurposes: []string{session.Purpose}, Now: now,
	}); err == nil {
		t.Fatal("first finalization unexpectedly succeeded")
	}
	cleanupAt := now.Add(2 * time.Hour)
	cleaned, err := CleanupExpiredUploadSessions(t.Context(), store, baseRepo, cleanupAt, 10)
	if err != nil {
		t.Fatalf("CleanupExpiredUploadSessions: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("expired sessions cleaned = %d, want recovery instead", cleaned)
	}
	found, err := baseRepo.UploadSessions().FindByID(t.Context(), session.ID)
	if err != nil || found.Status != model.UploadSessionFinalized || found.AssetID != session.ID || found.PromotionSourceETag != "etag-cleanup-recovery" || found.FinalizationETag != "etag-final-cleanup-recovery" {
		t.Fatalf("cleanup recovery session = %#v, %v", found, err)
	}
	asset, err := baseRepo.Assets().FindByID(t.Context(), session.ID)
	if err != nil || asset.StorageKey != "assets/users/user-1/cleanup-recovery/ref.png" || asset.ETag != found.FinalizationETag {
		t.Fatalf("cleanup recovery asset = %#v, %v", asset, err)
	}
	for _, key := range store.deleted {
		if key == asset.StorageKey {
			t.Fatalf("cleanup deleted immutable final object %q", key)
		}
	}
	if len(store.promoted) != 1 {
		t.Fatalf("promotions = %#v, want one", store.promoted)
	}
}

func TestFinalizeUploadSessionBoundsRemoteOperationBelowLease(t *testing.T) {
	now := time.Date(2026, 7, 18, 11, 0, 0, 0, time.UTC)
	repo := newDirectUploadTestRepository(t)
	session := &model.UploadSession{
		ID: "bounded-finalization", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/user-1/bounded-finalization/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Hour),
	}
	seedUploadSession(t, repo, session)
	store := matchingUploadSessionStore(session)
	sawBoundedContext := false
	store.statHook = func(ctx context.Context, _ string) {
		deadline, ok := ctx.Deadline()
		if ok && time.Until(deadline) > 0 && time.Until(deadline) <= 46*time.Second {
			sawBoundedContext = true
		}
	}
	if _, err := FinalizeUploadSession(t.Context(), store, repo, FinalizeUploadRequest{
		SessionID: session.ID, UserID: session.UserID, AllowedPurposes: []string{session.Purpose}, Now: now,
	}); err != nil {
		t.Fatalf("FinalizeUploadSession: %v", err)
	}
	if !sawBoundedContext {
		t.Fatal("remote finalization calls did not receive a deadline below the lease")
	}
}

func TestFinalizeUploadSessionRejectsInvalidState(t *testing.T) {
	now := time.Date(2026, 7, 17, 15, 20, 0, 0, time.UTC)
	tests := []struct {
		name    string
		mutate  func(*model.UploadSession, *fakeDirectUploadStore)
		request func(*model.UploadSession) FinalizeUploadRequest
		want    error
	}{
		{name: "expired session", mutate: func(s *model.UploadSession, _ *fakeDirectUploadStore) { s.ExpiresAt = now }, want: ErrUploadSessionExpired},
		{name: "foreign user", request: func(s *model.UploadSession) FinalizeUploadRequest {
			return FinalizeUploadRequest{SessionID: s.ID, UserID: "user-2", AllowedPurposes: []string{s.Purpose}, Now: now}
		}, want: ErrUploadSessionAccessDenied},
		{name: "disallowed purpose", request: func(s *model.UploadSession) FinalizeUploadRequest {
			return FinalizeUploadRequest{SessionID: s.ID, UserID: s.UserID, AllowedPurposes: []string{DirectUploadPurposeTaskReference}, Now: now}
		}, want: ErrUploadSessionAccessDenied},
		{name: "staging size mismatch", mutate: func(_ *model.UploadSession, st *fakeDirectUploadStore) {
			st.objects["uploads/pending/user-1/session-invalid/ref.png"].Size++
		}, want: ErrUploadSessionObjectInvalid},
		{name: "normalized content type mismatch", mutate: func(_ *model.UploadSession, st *fakeDirectUploadStore) {
			st.objects["uploads/pending/user-1/session-invalid/ref.png"].ContentType = "image/jpeg; charset=binary"
		}, want: ErrUploadSessionObjectInvalid},
		{name: "missing etag", mutate: func(_ *model.UploadSession, st *fakeDirectUploadStore) {
			st.objects["uploads/pending/user-1/session-invalid/ref.png"].ETag = ""
		}, want: ErrUploadSessionObjectInvalid},
		{name: "source etag precondition failure", mutate: func(_ *model.UploadSession, st *fakeDirectUploadStore) {
			st.promoteErr = storage.ErrPromotionPreconditionFailed
		}, want: ErrUploadSessionObjectInvalid},
		{name: "conflicting final object", mutate: func(_ *model.UploadSession, st *fakeDirectUploadStore) {
			st.objects["assets/users/user-1/session-invalid/ref.png"] = &storage.ObjectInfo{Key: "assets/users/user-1/session-invalid/ref.png", Size: 99, ContentType: "image/png", ETag: "other"}
		}, want: ErrUploadSessionObjectInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newDirectUploadTestRepository(t)
			session := &model.UploadSession{
				ID: "session-invalid", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
				StagingKey: "uploads/pending/user-1/session-invalid/ref.png", FileName: "ref.png",
				ContentType: "image/png", Size: 1024, Status: model.UploadSessionPending,
				ExpiresAt: now.Add(time.Hour),
			}
			store := matchingUploadSessionStore(session)
			if tt.mutate != nil {
				tt.mutate(session, store)
			}
			seedUploadSession(t, repo, session)
			req := FinalizeUploadRequest{SessionID: session.ID, UserID: session.UserID, AllowedPurposes: []string{session.Purpose}, Now: now}
			if tt.request != nil {
				req = tt.request(session)
			}
			_, err := FinalizeUploadSession(t.Context(), store, repo, req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCleanupExpiredUploadSessionsNeverDeletesFinalAsset(t *testing.T) {
	now := time.Date(2026, 7, 17, 15, 30, 0, 0, time.UTC)
	stale := now.Add(-10 * time.Minute)
	repo := newDirectUploadTestRepository(t)
	sessions := []*model.UploadSession{
		{ID: "pending", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user-1/pending/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 1, Status: model.UploadSessionPending, ExpiresAt: now.Add(-time.Hour)},
		{ID: "finalizing", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user-1/finalizing/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 1, Status: model.UploadSessionFinalizing, ExpiresAt: now.Add(-time.Hour), FinalizationToken: "stale", FinalizationClaimedAt: &stale},
		{ID: "expiring", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user-1/expiring/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 1, Status: model.UploadSessionExpiring, ExpiresAt: now.Add(-time.Hour), CleanupClaimID: "stale", CleanupClaimedAt: &stale},
		{ID: "finalized", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user-1/finalized/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 1, Status: model.UploadSessionFinalized, ExpiresAt: now.Add(-time.Hour), FinalizationETag: "etag-final", AssetID: "finalized"},
	}
	for _, session := range sessions {
		seedUploadSession(t, repo, session)
	}
	finalKey := "assets/users/user-1/finalized/ref.png"
	if err := repo.Assets().Create(t.Context(), &model.Asset{ID: "finalized", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference, StorageKey: finalKey, FileName: "ref.png", ContentType: "image/png", Size: 1, ETag: "etag-final"}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	store := &fakeDirectUploadStore{}

	cleaned, err := CleanupExpiredUploadSessions(t.Context(), store, repo, now, 100)
	if err != nil {
		t.Fatalf("CleanupExpiredUploadSessions: %v", err)
	}
	if cleaned != 3 {
		t.Fatalf("cleaned = %d, want 3", cleaned)
	}
	wantDeleted := map[string]bool{
		sessions[0].StagingKey: true,
		sessions[1].StagingKey: true,
		sessions[2].StagingKey: true,
	}
	if len(store.deleted) != len(wantDeleted) {
		t.Fatalf("deleted = %#v", store.deleted)
	}
	for _, key := range store.deleted {
		if !wantDeleted[key] || key == finalKey || strings.HasPrefix(key, "assets/users/") {
			t.Fatalf("cleanup deleted unexpected key %q", key)
		}
	}
}

func TestPrepareDirectUploadCreatesPendingScopedSTSSession(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	repo := &fakeUploadSessionRepo{}
	var policyResource []string
	issuer := StaticUploadCredentialIssuer(func(_ context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
		if strings.Contains(req.Policy, "uploads/finalized/") {
			t.Fatalf("policy granted finalized namespace: %s", req.Policy)
		}
		var policy struct {
			Statement []struct {
				Action   []string `json:"Action"`
				Resource []string `json:"Resource"`
			} `json:"Statement"`
		}
		if err := json.Unmarshal([]byte(req.Policy), &policy); err != nil {
			t.Fatalf("policy is not JSON: %v", err)
		}
		if len(policy.Statement) != 1 {
			t.Fatalf("policy statements = %#v, want one exact-key grant", policy.Statement)
		}
		policyResource = append([]string(nil), policy.Statement[0].Resource...)
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
		Purpose:     DirectUploadPurposeMontageAsset,
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
	wantResource := "acs:oss:*:*:anban-test/" + result.Key
	if len(policyResource) != 1 || policyResource[0] != wantResource {
		t.Fatalf("STS resource = %#v, want exact source ARN %q", policyResource, wantResource)
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
	if repo.createdSession == nil || repo.createdSession.Status != model.UploadSessionPending || repo.createdSession.NextCleanupAt != nil {
		t.Fatalf("upload session not recorded: %#v", repo.createdSession)
	}
	if store.uploadKey != result.Key || store.contentType != "video/mp4" {
		t.Fatalf("signed upload key/content-type = %q/%q", store.uploadKey, store.contentType)
	}
}

func TestPrepareDirectUploadAllowsAIEntryMediaAndDocuments(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	repo := &fakeUploadSessionRepo{}
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
	repo := &fakeUploadSessionRepo{}
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
	repo := &fakeUploadSessionRepo{}
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Minute)}, nil
		}),
	}

	result, err := PrepareDirectUpload(context.Background(), store, repo, cfg, DirectUploadPrepareRequest{
		UserID:      "u",
		Purpose:     DirectUploadPurposeMontageAsset,
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
	repo := &fakeUploadSessionRepo{}
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{}, nil
		}),
	}
	cases := []DirectUploadPrepareRequest{
		{UserID: "u", Purpose: "bad", Filename: "a.png", ContentType: "image/png", Size: 1},
		{UserID: "u", Purpose: DirectUploadPurposeProjectReference, Filename: "a.mp4", ContentType: "video/mp4", Size: 1},
		{UserID: "u", Purpose: DirectUploadPurposeMontageAsset, Filename: "a.mp4", ContentType: "video/mp4", Size: 51 * 1024 * 1024},
		{UserID: "u", Purpose: DirectUploadPurposeAIEntryAttachment, Filename: "brief.exe", ContentType: "application/x-msdownload", Size: 1},
		{UserID: "u", Purpose: DirectUploadPurposeAIEntryAttachment, Filename: "brief.pdf", ContentType: "application/pdf", Size: 26 * 1024 * 1024},
	}
	for _, tc := range cases {
		if _, err := PrepareDirectUpload(context.Background(), store, repo, cfg, tc); err == nil {
			t.Fatalf("PrepareDirectUpload(%+v) succeeded, want error", tc)
		}
	}
}

func TestPrepareDirectUploadRequiresExactMIMEExtensionPairs(t *testing.T) {
	store := &fakeDirectUploadStore{name: "oss"}
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Minute)}, nil
		}),
	}
	tests := []struct {
		name        string
		filename    string
		contentType string
		wantOK      bool
	}{
		{name: "png", filename: "photo.png", contentType: "image/png", wantOK: true},
		{name: "jpeg alias", filename: "photo.jpg", contentType: "image/jpeg", wantOK: true},
		{name: "m4a alias", filename: "voice.m4a", contentType: "audio/mp4", wantOK: true},
		{name: "quicktime alias", filename: "clip.mov", contentType: "video/quicktime", wantOK: true},
		{name: "docx", filename: "brief.docx", contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", wantOK: true},
		{name: "markdown", filename: "brief.md", contentType: "text/markdown", wantOK: true},
		{name: "generic fallback", filename: "voice.m4a", contentType: "application/octet-stream", wantOK: true},
		{name: "extensionless known mime", filename: "photo", contentType: "image/png", wantOK: true},
		{name: "svg disguised as png", filename: "payload.png", contentType: "image/svg+xml"},
		{name: "same category mismatch", filename: "photo.jpg", contentType: "image/png"},
		{name: "audio mime on video extension", filename: "clip.mp4", contentType: "audio/mp4"},
		{name: "executable mime on text extension", filename: "notes.txt", contentType: "application/x-msdownload"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PrepareDirectUpload(context.Background(), store, &fakeUploadSessionRepo{}, cfg, DirectUploadPrepareRequest{
				UserID: "u", Purpose: DirectUploadPurposeAIEntryAttachment,
				Filename: tt.filename, ContentType: tt.contentType, Size: 1024,
			})
			if tt.wantOK && err != nil {
				t.Fatalf("PrepareDirectUpload(%s, %s): %v", tt.filename, tt.contentType, err)
			}
			if !tt.wantOK && err == nil {
				t.Fatalf("PrepareDirectUpload(%s, %s) succeeded, want error", tt.filename, tt.contentType)
			}
		})
	}
}

func TestPrepareAndFinalizeExtensionlessDirectUploadsUseCanonicalExtensions(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	cfg := DirectUploadConfig{
		Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", Region: "oss-cn-hangzhou"},
		CredentialIssuer: StaticUploadCredentialIssuer(func(context.Context, UploadCredentialRequest) (*UploadCredential, error) {
			return &UploadCredential{AccessKeyID: "ak", AccessKeySecret: "sk", SecurityToken: "token", ExpiresAt: now.Add(time.Hour)}, nil
		}),
		Now: func() time.Time { return now },
	}
	tests := []struct {
		name          string
		filename      string
		contentType   string
		wantExtension string
	}{
		{name: "jpeg", filename: "photo", contentType: "image/jpeg", wantExtension: ".jpg"},
		{name: "png", filename: "diagram", contentType: "image/png", wantExtension: ".png"},
		{name: "mp4", filename: "clip", contentType: "video/mp4", wantExtension: ".mp4"},
		{name: "mp3", filename: "voice", contentType: "audio/mpeg", wantExtension: ".mp3"},
		{name: "pdf", filename: "brief", contentType: "application/pdf", wantExtension: ".pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeDirectUploadStore{name: "oss", objects: map[string]*storage.ObjectInfo{}}
			repo := newDirectUploadTestRepository(t)
			result, err := PrepareDirectUpload(context.Background(), store, repo.UploadSessions(), cfg, DirectUploadPrepareRequest{
				UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
				Filename: tt.filename, ContentType: tt.contentType, Size: 1024,
			})
			if err != nil {
				t.Fatalf("PrepareDirectUpload: %v", err)
			}
			wantFilename := tt.filename + tt.wantExtension
			session, err := repo.UploadSessions().FindByID(t.Context(), result.UploadSessionID)
			if err != nil || session.FileName != wantFilename || !strings.HasSuffix(result.StagingKey, "/"+wantFilename) {
				t.Fatalf("prepared upload = %#v key=%q error=%v, want filename %q", session, result.StagingKey, err, wantFilename)
			}
			store.objects[result.StagingKey] = &storage.ObjectInfo{
				Key: result.StagingKey, Size: 1024, ContentType: tt.contentType, ETag: "etag-" + result.UploadID,
			}
			asset, err := FinalizeUploadSession(context.Background(), store, repo, FinalizeUploadRequest{
				SessionID: result.UploadSessionID, UserID: "user-1", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}, Now: now,
			})
			if err != nil {
				t.Fatalf("FinalizeUploadSession: %v", err)
			}
			wantFinalKey := "assets/users/user-1/" + result.UploadID + "/" + wantFilename
			if asset.StorageKey != wantFinalKey || store.objects[wantFinalKey] == nil {
				t.Fatalf("finalized asset = %#v object=%#v, want key %q", asset, store.objects[wantFinalKey], wantFinalKey)
			}
		})
	}
}

func TestFinalizeUploadSessionURLsRequiresRepositoryIdentity(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 0, 0, 0, time.UTC)
	t.Run("owned staging URL finalizes and rewrites", func(t *testing.T) {
		repo := newDirectUploadTestRepository(t)
		session := &model.UploadSession{
			ID: "url-session", UserID: "user-1", Purpose: DirectUploadPurposeMontageAsset,
			StagingKey: "uploads/pending/user-1/url-session/clip.mp4", FileName: "clip.mp4",
			ContentType: "video/mp4", Size: 12, Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Hour),
		}
		seedUploadSession(t, repo, session)
		store := matchingUploadSessionStore(session)
		raw := store.GetURL(session.StagingKey)
		rewrites, err := FinalizeUploadSessionURLs(t.Context(), store, repo, session.UserID, session.Purpose, []string{raw}, now, func(string) bool { return true })
		if err != nil {
			t.Fatalf("FinalizeUploadSessionURLs: %v", err)
		}
		want := store.GetURL("assets/users/user-1/url-session/clip.mp4")
		if rewrites[raw] != want {
			t.Fatalf("rewrite = %q, want %q", rewrites[raw], want)
		}
	})

	for _, tt := range []struct {
		name    string
		userID  string
		rawKey  string
		owned   bool
		wantErr error
	}{
		{name: "foreign owner", userID: "user-2", rawKey: "uploads/pending/user-1/url-session/clip.mp4", owned: true, wantErr: ErrUploadSessionAccessDenied},
		{name: "asserted key mismatch", userID: "user-1", rawKey: "uploads/pending/user-1/url-session/forged.mp4", owned: true, wantErr: ErrUploadSessionAccessDenied},
		{name: "external pending-shaped URL is ignored", userID: "user-1", rawKey: "uploads/pending/user-1/url-session/clip.mp4", owned: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newDirectUploadTestRepository(t)
			session := &model.UploadSession{
				ID: "url-session", UserID: "user-1", Purpose: DirectUploadPurposeMontageAsset,
				StagingKey: "uploads/pending/user-1/url-session/clip.mp4", FileName: "clip.mp4",
				ContentType: "video/mp4", Size: 12, Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Hour),
			}
			seedUploadSession(t, repo, session)
			store := matchingUploadSessionStore(session)
			raw := "https://external.example.com/" + tt.rawKey
			if tt.owned {
				raw = store.GetURL(tt.rawKey)
			}
			rewrites, err := FinalizeUploadSessionURLs(t.Context(), store, repo, tt.userID, session.Purpose, []string{raw}, now, func(string) bool { return tt.owned })
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && len(rewrites) != 0 {
				t.Fatalf("external URL rewrites = %#v, want none", rewrites)
			}
		})
	}
}

func TestCleanupExpiredUploadSessionsReopensClaimWhenDeleteFails(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 10, 0, 0, time.UTC)
	repo := newDirectUploadTestRepository(t)
	session := &model.UploadSession{
		ID: "cleanup-retry", UserID: "user-1", Purpose: DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/user-1/cleanup-retry/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 1, Status: model.UploadSessionPending, ExpiresAt: now.Add(-time.Hour),
	}
	seedUploadSession(t, repo, session)
	store := &fakeDirectUploadStore{deleteErr: errors.New("temporary delete failure")}

	if _, err := CleanupExpiredUploadSessions(t.Context(), store, repo, now, 10); err == nil {
		t.Fatal("cleanup delete failure was ignored")
	}
	found, err := repo.UploadSessions().FindByID(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("find upload session: %v", err)
	}
	if found.Status != model.UploadSessionPending || found.CleanupClaimID != "" || found.CleanupClaimedAt != nil {
		t.Fatalf("reopened upload session = %#v", found)
	}
}
