package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

const taskArtifactTestSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeTaskArtifactStorage struct {
	name               string
	uploadKey          string
	uploadContentType  string
	uploadCalls        int
	uploadURLCalls     int
	stats              map[string]*storage.ObjectInfo
	statErr            error
	statReturnsNil     bool
	statKeys           []string
	promoteCalls       []taskArtifactPromotion
	promoteErr         error
	promoteErrBySource map[string]error
	beforePromote      func(sourceKey, finalKey string)
	afterPromote       func(sourceKey, finalKey string)
	objects            map[string][]byte
	deletedKeys        []string
}

type taskArtifactPromotion struct {
	sourceKey    string
	finalKey     string
	expectedETag string
}

type statOnlyTaskArtifactStorage struct {
	storage.Provider
	statProvider storage.ObjectStatProvider
}

type taskArtifactRepositoryOverride struct {
	repository.Repository
	uploadSessions repository.UploadSessionRepository
}

func (r *taskArtifactRepositoryOverride) UploadSessions() repository.UploadSessionRepository {
	return r.uploadSessions
}

type failingTaskArtifactUploadSessionRepository struct {
	repository.UploadSessionRepository
	err error
}

func (r *failingTaskArtifactUploadSessionRepository) Create(context.Context, *model.UploadSession) error {
	return r.err
}

type recordingTaskArtifactUploadSessionRepository struct {
	repository.UploadSessionRepository
	releasedIDs []string
}

func (r *recordingTaskArtifactUploadSessionRepository) ReleaseFinalization(ctx context.Context, id, token string) (bool, error) {
	r.releasedIDs = append(r.releasedIDs, id)
	return r.UploadSessionRepository.ReleaseFinalization(ctx, id, token)
}

type blockingFirstTaskArtifactClaimRepository struct {
	repository.UploadSessionRepository
	firstClaimed chan struct{}
	releaseFirst chan struct{}
	once         sync.Once
}

func (r *blockingFirstTaskArtifactClaimRepository) ClaimFinalization(ctx context.Context, id, token string, claimedAt, claimStaleBefore time.Time) (bool, error) {
	claimed, err := r.UploadSessionRepository.ClaimFinalization(ctx, id, token, claimedAt, claimStaleBefore)
	if err != nil || !claimed {
		return claimed, err
	}
	blocked := false
	r.once.Do(func() {
		blocked = true
		close(r.firstClaimed)
	})
	if blocked {
		select {
		case <-ctx.Done():
			return false, context.Cause(ctx)
		case <-r.releaseFirst:
		}
	}
	return true, nil
}

func (s *statOnlyTaskArtifactStorage) StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	return s.statProvider.StatObject(ctx, key)
}

func (f *fakeTaskArtifactStorage) Name() string {
	if f.name != "" {
		return f.name
	}
	return "oss"
}

func (f *fakeTaskArtifactStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	f.uploadCalls++
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return &storage.UploadResult{URL: f.GetURL(key), Key: key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (f *fakeTaskArtifactStorage) UploadFile(ctx context.Context, key string, filePath string, contentType string) (*storage.UploadResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return f.Upload(ctx, key, file, contentType)
}

func (f *fakeTaskArtifactStorage) UploadURL(_ context.Context, key string, contentType string, _ int) (string, error) {
	f.uploadURLCalls++
	f.uploadKey = key
	f.uploadContentType = contentType
	return "https://upload.example.com/" + key, nil
}

func (f *fakeTaskArtifactStorage) GetURL(key string) string {
	return "https://cdn.example.com/" + key
}

func (f *fakeTaskArtifactStorage) Read(context.Context, string) ([]byte, error) {
	return nil, os.ErrNotExist
}

func (f *fakeTaskArtifactStorage) Delete(_ context.Context, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	delete(f.stats, key)
	delete(f.objects, key)
	return nil
}

func (f *fakeTaskArtifactStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return "https://download.example.com/" + key, nil
}

func (f *fakeTaskArtifactStorage) HasCustomDomain() bool { return true }

func (f *fakeTaskArtifactStorage) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://cdn.example.com/")
}

func (f *fakeTaskArtifactStorage) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
	f.statKeys = append(f.statKeys, key)
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.statReturnsNil {
		return nil, nil
	}
	if f.stats == nil || f.stats[key] == nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectNotFound, key)
	}
	cp := *f.stats[key]
	return &cp, nil
}

func (f *fakeTaskArtifactStorage) PromoteObject(_ context.Context, sourceKey, finalKey, expectedETag string) (*storage.ObjectInfo, error) {
	f.promoteCalls = append(f.promoteCalls, taskArtifactPromotion{sourceKey: sourceKey, finalKey: finalKey, expectedETag: expectedETag})
	if f.beforePromote != nil {
		f.beforePromote(sourceKey, finalKey)
	}
	if err := f.promoteErrBySource[sourceKey]; err != nil {
		delete(f.promoteErrBySource, sourceKey)
		return nil, err
	}
	if f.promoteErr != nil {
		return nil, f.promoteErr
	}
	source := f.stats[sourceKey]
	if source == nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectNotFound, sourceKey)
	}
	if source.ETag != expectedETag {
		return nil, fmt.Errorf("%w: %s", storage.ErrPromotionPreconditionFailed, sourceKey)
	}
	if f.stats[finalKey] != nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectAlreadyExists, finalKey)
	}
	copy := *source
	copy.Key = finalKey
	if f.stats == nil {
		f.stats = make(map[string]*storage.ObjectInfo)
	}
	f.stats[finalKey] = &copy
	if f.objects != nil {
		f.objects[finalKey] = append([]byte(nil), f.objects[sourceKey]...)
	}
	if f.afterPromote != nil {
		f.afterPromote(sourceKey, finalKey)
	}
	return &copy, nil
}

func startTaskArtifactExecution(t *testing.T, repo repository.Repository, task *model.Task) string {
	t.Helper()
	executionID := uuid.NewString()
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.TaskExecutions().Create(context.Background(), &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true, StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	return executionID
}

func newTaskArtifactTestService(t *testing.T) (*TaskService, repository.Repository, *fakeTaskArtifactStorage, *model.Task) {
	t.Helper()
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	store := &fakeTaskArtifactStorage{name: "oss"}
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return svc, repo, store, task
}

func taskArtifactDirectUploadConfig(t *testing.T) DirectUploadConfig {
	t.Helper()
	return DirectUploadConfig{
		Storage: config.StorageConfig{
			Provider:       "oss",
			Endpoint:       "oss-cn-hangzhou.aliyuncs.com",
			BucketName:     "anban-test",
			Region:         "oss-cn-hangzhou",
			STSRoleArn:     "acs:ram::1:role/upload",
			STSSessionName: "agent-artifact-upload",
		},
		CredentialIssuer: StaticUploadCredentialIssuer(func(_ context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
			if !strings.Contains(req.Policy, "uploads/users/") || !strings.Contains(req.Policy, "/artifacts/staging/sha256/") || !strings.Contains(req.Policy, "/output/article.md") {
				t.Fatalf("policy = %s, want task artifact staging key", req.Policy)
			}
			return &UploadCredential{
				AccessKeyID:     "sts-ak",
				AccessKeySecret: "sts-secret",
				SecurityToken:   "sts-token",
				ExpiresAt:       time.Now().Add(15 * time.Minute),
			}, nil
		}),
		Now: time.Now,
	}
}

func expectedTaskArtifactFinalKey(task *model.Task, executionID, hash, relPath string) string {
	return buildTaskArtifactStoragePrefix(task, executionID) + "workspace/sha256/" + hash + "/" + relPath
}

func assertTaskArtifactStagingKey(t *testing.T, task *model.Task, executionID, hash, relPath, key string) {
	t.Helper()
	prefix := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + hash + "/"
	remainder, ok := strings.CutPrefix(key, prefix)
	if !ok {
		t.Fatalf("staging key = %q, want prefix %q", key, prefix)
	}
	uploadID, suffix, ok := strings.Cut(remainder, "/")
	if !ok || suffix != relPath {
		t.Fatalf("staging key = %q, want UUID and exact suffix %q", key, relPath)
	}
	parsed, err := uuid.Parse(uploadID)
	if err != nil || parsed.String() != uploadID {
		t.Fatalf("staging key UUID = %q, want canonical UUID", uploadID)
	}
}

func taskArtifactManifestEntry(relPath, objectKey string, size int64, hash string) TaskArtifactManifestFile {
	return TaskArtifactManifestFile{
		RelativePath: relPath,
		ObjectKey:    objectKey,
		ContentType:  normalizeTaskArtifactContentType("", relPath),
		Size:         size,
		SHA256:       hash,
	}
}

func seedTaskArtifactUploadSession(t *testing.T, repo repository.Repository, task *model.Task, stagingKey, relPath string, size int64, mutate func(*model.UploadSession)) *model.UploadSession {
	t.Helper()
	executionID := ""
	if task.CurrentExecutionID != nil {
		executionID = *task.CurrentExecutionID
	}
	prefix := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/"
	remainder, ok := strings.CutPrefix(stagingKey, prefix)
	if !ok {
		t.Fatalf("staging key %q does not use task artifact prefix", stagingKey)
	}
	parts := strings.SplitN(remainder, "/", 3)
	if len(parts) != 3 {
		t.Fatalf("staging key %q has no upload session ID", stagingKey)
	}
	session := &model.UploadSession{
		ID: parts[1], UserID: task.UserID, Purpose: DirectUploadPurposeTaskArtifact,
		StagingKey: stagingKey, FileName: filepath.Base(relPath),
		ContentType: normalizeTaskArtifactContentType("", relPath), Size: size,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}
	if mutate != nil {
		mutate(session)
	}
	if err := repo.UploadSessions().Create(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func taskArtifactDirectUploadConfigWithCredentialCounter(t *testing.T, calls *int) DirectUploadConfig {
	t.Helper()
	cfg := taskArtifactDirectUploadConfig(t)
	issuer := cfg.CredentialIssuer
	cfg.CredentialIssuer = StaticUploadCredentialIssuer(func(ctx context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
		*calls++
		return issuer.IssueUploadCredential(ctx, req)
	})
	return cfg
}

func TestPrepareTaskArtifactUploadTargetsUniqueStagingKey(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	request := TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md",
		ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	}
	var policy string
	cfg := taskArtifactDirectUploadConfig(t)
	issuer := cfg.CredentialIssuer
	cfg.CredentialIssuer = StaticUploadCredentialIssuer(func(ctx context.Context, req UploadCredentialRequest) (*UploadCredential, error) {
		policy = req.Policy
		return issuer.IssueUploadCredential(ctx, req)
	})

	result, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, cfg, request)
	if err != nil {
		t.Fatal(err)
	}
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, request.RelativePath)
	assertTaskArtifactStagingKey(t, task, executionID, taskArtifactTestSHA256, request.RelativePath, result.Key)
	if result.Key == finalKey || store.uploadKey != result.Key || !strings.Contains(result.UploadURL, result.Key) {
		t.Fatalf("prepare = %#v upload key=%q, want signed staging key distinct from %q", result, store.uploadKey, finalKey)
	}
	if !strings.Contains(policy, result.Key) || strings.Contains(policy, finalKey) {
		t.Fatalf("policy = %s, want only staging key %q and never final key %q", policy, result.Key, finalKey)
	}
	if len(store.statKeys) != 1 || store.statKeys[0] != finalKey {
		t.Fatalf("stat keys = %#v, want immutable final key %q", store.statKeys, finalKey)
	}
	session, err := repo.UploadSessions().FindByID(ctx, result.UploadID)
	if err != nil {
		t.Fatalf("find task artifact upload session: %v", err)
	}
	if result.UploadSessionID != result.UploadID || session.UserID != task.UserID || session.Purpose != DirectUploadPurposeTaskArtifact ||
		session.StagingKey != result.Key || session.FileName != "article.md" || session.ContentType != "text/markdown" ||
		session.Size != request.Size || session.Status != model.UploadSessionPending || session.NextCleanupAt != nil || !session.ExpiresAt.After(cfg.Now()) {
		t.Fatalf("task artifact upload session = %#v result=%#v", session, result)
	}
	second, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, cfg, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Key == result.Key {
		t.Fatalf("two missing-final prepares reused staging key %q", result.Key)
	}
	assertTaskArtifactStagingKey(t, task, executionID, taskArtifactTestSHA256, request.RelativePath, second.Key)
}

func TestPrepareTaskArtifactUploadFailsWhenSessionPersistenceFails(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	persistErr := errors.New("upload session unavailable")
	svc.repo = &taskArtifactRepositoryOverride{
		Repository: repo,
		uploadSessions: &failingTaskArtifactUploadSessionRepository{
			UploadSessionRepository: repo.UploadSessions(), err: persistErr,
		},
	}

	result, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if result != nil || !errors.Is(err, ErrTaskArtifactPersistence) || !strings.Contains(err.Error(), persistErr.Error()) {
		t.Fatalf("PrepareTaskArtifactUpload = %#v, %v; want persistence failure and no response", result, err)
	}
	if store.uploadURLCalls != 1 {
		t.Fatalf("upload URL calls = %d, want credentials prepared before durable session failure", store.uploadURLCalls)
	}
}

func TestFinalizeTaskArtifactManifestRejectsOversizeBeforeSideEffects(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	firstKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/first.md")
	oversizeKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		firstKey: {Key: firstKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "final"},
	}
	err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{
			taskArtifactManifestEntry("output/first.md", firstKey, 7, taskArtifactTestSHA256),
			taskArtifactManifestEntry("output/article.md", oversizeKey, maxTaskArtifactUploadBytes+1, taskArtifactTestSHA256),
		},
	})
	if !errors.Is(err, ErrTaskArtifactInvalid) || !strings.Contains(err.Error(), "512 MB") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want size limit rejection", err)
	}
	if len(store.statKeys) != 0 || len(store.promoteCalls) != 0 || len(store.deletedKeys) != 0 {
		t.Fatalf("oversize manifest touched storage: stat=%#v promote=%#v delete=%#v", store.statKeys, store.promoteCalls, store.deletedKeys)
	}
	rows, findErr := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if findErr != nil || len(rows) != 0 {
		t.Fatalf("oversize manifest persisted rows = %#v, err=%v", rows, findErr)
	}
}

func TestPrepareTaskArtifactUploadRejectsStorageWithoutConditionalPromotion(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	svc.store = &statOnlyTaskArtifactStorage{Provider: store, statProvider: store}
	credentialCalls := 0

	_, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfigWithCredentialCounter(t, &credentialCalls), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if !errors.Is(err, ErrTaskArtifactUnavailable) || !strings.Contains(err.Error(), "immutable object promotion") {
		t.Fatalf("PrepareTaskArtifactUpload error = %v, want promotion capability rejection", err)
	}
	if store.uploadURLCalls != 0 || credentialCalls != 0 {
		t.Fatalf("unsupported storage issued upload authority: urls=%d credentials=%d", store.uploadURLCalls, credentialCalls)
	}
}

func TestPrepareTaskArtifactUploadReusesOnlyExactImmutableFinal(t *testing.T) {
	for _, tt := range []struct {
		name      string
		info      storage.ObjectInfo
		wantError string
	}{
		{name: "exact match", info: storage.ObjectInfo{Size: 7, SHA256: taskArtifactTestSHA256, ETag: "final-etag"}},
		{name: "wrong hash", info: storage.ObjectInfo{Size: 7, SHA256: strings.Repeat("b", 64), ETag: "wrong-etag"}, wantError: "immutable final object metadata mismatch"},
		{name: "wrong size", info: storage.ObjectInfo{Size: 8, SHA256: taskArtifactTestSHA256, ETag: "wrong-etag"}, wantError: "immutable final object metadata mismatch"},
		{name: "missing hash", info: storage.ObjectInfo{Size: 7, ETag: "wrong-etag"}, wantError: "immutable final object metadata mismatch"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, store, task := newTaskArtifactTestService(t)
			ctx := context.Background()
			executionID := startTaskArtifactExecution(t, repo, task)
			finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
			tt.info.Key = finalKey
			store.stats = map[string]*storage.ObjectInfo{finalKey: &tt.info}
			credentialCalls := 0
			result, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfigWithCredentialCounter(t, &credentialCalls), TaskArtifactPrepareRequest{
				TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
			})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("PrepareTaskArtifactUpload error = %v, want %q", err, tt.wantError)
				}
				if store.uploadURLCalls != 0 || credentialCalls != 0 {
					t.Fatalf("mismatching final issued upload authority: urls=%d credentials=%d", store.uploadURLCalls, credentialCalls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.UploadRequired || result.Key != finalKey || result.ETag != "final-etag" || credentialCalls != 0 || store.uploadURLCalls != 0 {
				t.Fatalf("matching final prepare = %#v urls=%d credentials=%d", result, store.uploadURLCalls, credentialCalls)
			}
		})
	}
}

func TestFinalizeTaskArtifactManifestPromotesStagingAndPersistsImmutableFinal(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.stats = make(map[string]*storage.ObjectInfo)
	store.stats[prepared.Key] = &storage.ObjectInfo{Key: prepared.Key, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "staging-etag"}
	store.beforePromote = func(_, _ string) {
		claimed, findErr := repo.UploadSessions().FindByID(ctx, prepared.UploadID)
		if findErr != nil {
			t.Fatalf("find claimed task artifact session: %v", findErr)
		}
		if claimed.Status != model.UploadSessionFinalizing || claimed.FinalizationToken == "" || claimed.FinalizationClaimedAt == nil {
			t.Fatalf("task artifact session during promotion = %#v, want active finalization lease", claimed)
		}
	}
	manifest := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{
		taskArtifactManifestEntry("output/article.md", prepared.Key, 7, taskArtifactTestSHA256),
	}}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); err != nil {
		t.Fatal(err)
	}
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	if len(store.promoteCalls) != 1 || store.promoteCalls[0] != (taskArtifactPromotion{sourceKey: prepared.Key, finalKey: finalKey, expectedETag: "staging-etag"}) {
		t.Fatalf("promotions = %#v, want staging to immutable final", store.promoteCalls)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %#v, err=%v", rows, err)
	}
	if rows[0].OSSKey != finalKey || rows[0].OSSURL != store.GetURL(finalKey) || rows[0].ContentHash != taskArtifactTestSHA256 {
		t.Fatalf("persisted row = %#v, want immutable final key", rows[0])
	}
	found, err := repo.UploadSessions().FindByID(ctx, prepared.UploadID)
	if err != nil || found.Status != model.UploadSessionPending || found.FinalizationToken != "" || found.FinalizationClaimedAt != nil {
		t.Fatalf("released task artifact session = %#v, err=%v", found, err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsActiveClaimAndRetriesAfterRelease(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.stats = map[string]*storage.ObjectInfo{
		prepared.Key: {Key: prepared.Key, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "staging"},
	}
	now := time.Now()
	claimed, err := repo.UploadSessions().ClaimFinalization(ctx, prepared.UploadID, "concurrent-claim", now, now.Add(-uploadFinalizationLease))
	if err != nil || !claimed {
		t.Fatalf("ClaimFinalization = %v, %v", claimed, err)
	}
	manifest := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{
		taskArtifactManifestEntry("output/article.md", prepared.Key, 7, taskArtifactTestSHA256),
	}}
	err = svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest)
	if !errors.Is(err, ErrTaskArtifactUnavailable) || len(store.promoteCalls) != 0 || len(store.deletedKeys) != 0 {
		t.Fatalf("active-claim finalize = %v promote=%#v delete=%#v; want retryable unavailable", err, store.promoteCalls, store.deletedKeys)
	}
	released, err := repo.UploadSessions().ReleaseFinalization(ctx, prepared.UploadID, "concurrent-claim")
	if err != nil || !released {
		t.Fatalf("ReleaseFinalization = %v, %v", released, err)
	}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); err != nil {
		t.Fatalf("retry after release: %v", err)
	}
}

func TestFinalizeTaskArtifactManifestAllowsOnlyOneConcurrentClaim(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.stats = map[string]*storage.ObjectInfo{
		prepared.Key: {Key: prepared.Key, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "staging"},
	}
	sessions := &blockingFirstTaskArtifactClaimRepository{
		UploadSessionRepository: repo.UploadSessions(),
		firstClaimed:            make(chan struct{}),
		releaseFirst:            make(chan struct{}),
	}
	svc.repo = &taskArtifactRepositoryOverride{Repository: repo, uploadSessions: sessions}
	manifest := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{
		taskArtifactManifestEntry("output/article.md", prepared.Key, 7, taskArtifactTestSHA256),
	}}
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest)
	}()
	select {
	case <-sessions.firstClaimed:
	case <-time.After(5 * time.Second):
		t.Fatal("first finalization did not acquire its claim")
	}
	secondErr := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest)
	if !errors.Is(secondErr, ErrTaskArtifactUnavailable) {
		t.Fatalf("second concurrent finalize error = %v, want retryable unavailable", secondErr)
	}
	close(sessions.releaseFirst)
	select {
	case firstErr := <-firstResult:
		if firstErr != nil {
			t.Fatalf("first concurrent finalize: %v", firstErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first finalization did not finish after release")
	}
	if len(store.promoteCalls) != 1 {
		t.Fatalf("concurrent promotions = %#v, want one", store.promoteCalls)
	}
	found, err := repo.UploadSessions().FindByID(ctx, prepared.UploadID)
	if err != nil || found.Status != model.UploadSessionPending || found.FinalizationToken != "" {
		t.Fatalf("concurrent finalization session = %#v, err=%v", found, err)
	}
}

func TestTaskArtifactStagingCleanupAndFinalExistingRetry(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	now := time.Now().Truncate(time.Second)
	cfg := taskArtifactDirectUploadConfig(t)
	cfg.Now = func() time.Time { return now }
	cfg.Storage.DirectUploadExpiresSeconds = 60
	prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, cfg, TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.UploadSessions().FindByID(ctx, prepared.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	store.stats = map[string]*storage.ObjectInfo{
		prepared.Key: {Key: prepared.Key, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "staging-a"},
	}
	manifest := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{
		taskArtifactManifestEntry("output/article.md", prepared.Key, 7, taskArtifactTestSHA256),
	}}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); err != nil {
		t.Fatal(err)
	}
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	if store.stats[prepared.Key] != nil || store.stats[finalKey] == nil || len(store.deletedKeys) != 1 || store.deletedKeys[0] != prepared.Key {
		t.Fatalf("post-commit cleanup: staging=%#v final=%#v deleted=%#v", store.stats[prepared.Key], store.stats[finalKey], store.deletedKeys)
	}
	found, err := repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil || found.Status != model.UploadSessionPending {
		t.Fatalf("post-commit upload session = %#v, err=%v; want pending for delayed cleanup", found, err)
	}

	// Cleanup remains repeatable for a bounded grace window so a delayed PUT
	// cannot permanently recreate staging after the first expiration sweep.
	firstCleanupAt := session.ExpiresAt.Add(time.Second)
	cleaned, err := CleanupExpiredUploadSessions(ctx, store, repo, firstCleanupAt, 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("first CleanupExpiredUploadSessions = %d, %v", cleaned, err)
	}
	if store.stats[prepared.Key] != nil || store.stats[finalKey] == nil || len(store.deletedKeys) != 2 || store.deletedKeys[1] != prepared.Key {
		t.Fatalf("first expiration cleanup: staging=%#v final=%#v deleted=%#v", store.stats[prepared.Key], store.stats[finalKey], store.deletedKeys)
	}
	found, err = repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil || found.Status != model.UploadSessionPending || found.NextCleanupAt == nil || !found.NextCleanupAt.Equal(firstCleanupAt.Add(taskArtifactCleanupRetryDelay)) {
		t.Fatalf("task artifact session during cleanup grace = %#v, err=%v", found, err)
	}

	store.stats[prepared.Key] = &storage.ObjectInfo{Key: prepared.Key, Size: 9, SHA256: strings.Repeat("b", 64), ETag: "staging-b"}
	cleaned, err = CleanupExpiredUploadSessions(ctx, store, repo, firstCleanupAt.Add(taskArtifactCleanupRetryDelay), 10)
	if err != nil || cleaned != 1 || store.stats[prepared.Key] != nil || store.stats[finalKey] == nil || len(store.deletedKeys) != 3 || store.deletedKeys[2] != prepared.Key {
		t.Fatalf("delayed PUT cleanup = %d, %v staging=%#v final=%#v deleted=%#v", cleaned, err, store.stats[prepared.Key], store.stats[finalKey], store.deletedKeys)
	}
	found, err = repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil || found.Status != model.UploadSessionPending {
		t.Fatalf("task artifact session after delayed cleanup = %#v, err=%v", found, err)
	}

	cleaned, err = CleanupExpiredUploadSessions(ctx, store, repo, session.ExpiresAt.Add(taskArtifactCleanupGrace), 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("terminal CleanupExpiredUploadSessions = %d, %v", cleaned, err)
	}
	found, err = repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil || found.Status != model.UploadSessionExpired || found.NextCleanupAt != nil {
		t.Fatalf("terminal task artifact session = %#v, err=%v", found, err)
	}
	deleted := len(store.deletedKeys)
	cleaned, err = CleanupExpiredUploadSessions(ctx, store, repo, session.ExpiresAt.Add(time.Hour+time.Minute), 10)
	if err != nil || cleaned != 0 || len(store.deletedKeys) != deleted || store.stats[finalKey] == nil {
		t.Fatalf("post-grace cleanup = %d, %v deleted=%#v final=%#v", cleaned, err, store.deletedKeys, store.stats[finalKey])
	}

	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); err != nil {
		t.Fatalf("final-existing retry after staging cleanup: %v", err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(rows) != 1 || rows[0].OSSKey != finalKey {
		t.Fatalf("final-existing retry rows = %#v, err=%v", rows, err)
	}
}

func TestCleanupExpiredAbandonedTaskArtifactSessionNeverDeletesFinal(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	now := time.Now().UTC().Truncate(time.Second)
	cfg := taskArtifactDirectUploadConfig(t)
	cfg.Now = func() time.Time { return now }
	cfg.Storage.DirectUploadExpiresSeconds = 60
	prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, cfg, TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.UploadSessions().FindByID(ctx, prepared.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		prepared.Key: {Key: prepared.Key, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "staging"},
		finalKey:     {Key: finalKey, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "final"},
	}
	cleaned, err := CleanupExpiredUploadSessions(ctx, store, repo, session.ExpiresAt.Add(time.Second), 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("CleanupExpiredUploadSessions = %d, %v", cleaned, err)
	}
	if store.stats[prepared.Key] != nil || store.stats[finalKey] == nil || len(store.deletedKeys) != 1 || store.deletedKeys[0] != prepared.Key {
		t.Fatalf("abandoned cleanup touched wrong objects: staging=%#v final=%#v deleted=%#v", store.stats[prepared.Key], store.stats[finalKey], store.deletedKeys)
	}
}

func TestCleanupExpiredTaskArtifactWaitsForFreshFinalizationClaim(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	now := time.Now().UTC().Truncate(time.Second)
	cfg := taskArtifactDirectUploadConfig(t)
	cfg.Now = func() time.Time { return now }
	cfg.Storage.DirectUploadExpiresSeconds = 60
	prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, cfg, TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.UploadSessions().FindByID(ctx, prepared.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	claimAt := session.ExpiresAt.Add(-time.Second)
	claimed, err := repo.UploadSessions().ClaimFinalization(ctx, session.ID, "fresh-finalizer", claimAt, claimAt.Add(-uploadFinalizationLease))
	if err != nil || !claimed {
		t.Fatalf("ClaimFinalization = %v, %v", claimed, err)
	}
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		prepared.Key: {Key: prepared.Key, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "staging"},
		finalKey:     {Key: finalKey, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "final"},
	}
	cleanupAt := session.ExpiresAt.Add(time.Second)
	cleaned, err := CleanupExpiredUploadSessions(ctx, store, repo, cleanupAt, 10)
	if err != nil || cleaned != 0 || len(store.deletedKeys) != 0 || store.stats[prepared.Key] == nil || store.stats[finalKey] == nil {
		t.Fatalf("cleanup during fresh claim = %d, %v staging=%#v final=%#v deleted=%#v", cleaned, err, store.stats[prepared.Key], store.stats[finalKey], store.deletedKeys)
	}
	released, err := repo.UploadSessions().ReleaseFinalization(ctx, session.ID, "fresh-finalizer")
	if err != nil || !released {
		t.Fatalf("ReleaseFinalization = %v, %v", released, err)
	}
	cleaned, err = CleanupExpiredUploadSessions(ctx, store, repo, cleanupAt, 10)
	if err != nil || cleaned != 1 || len(store.deletedKeys) != 1 || store.deletedKeys[0] != prepared.Key || store.stats[finalKey] == nil {
		t.Fatalf("cleanup after release = %d, %v final=%#v deleted=%#v", cleaned, err, store.stats[finalKey], store.deletedKeys)
	}
}

func TestCleanupExpiredTaskArtifactSkipsAssetFinalizationRecovery(t *testing.T) {
	_, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	now := time.Now().UTC().Truncate(time.Second)
	stagingKey := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
	session := seedTaskArtifactUploadSession(t, repo, task, stagingKey, "output/article.md", 7, func(session *model.UploadSession) {
		session.ExpiresAt = now.Add(-time.Minute)
		session.PromotionSourceETag = "must-not-trigger-asset-recovery"
		session.FinalizationETag = "must-not-trigger-asset-recovery"
	})
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		stagingKey: {Key: stagingKey, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "staging"},
		finalKey:   {Key: finalKey, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "final"},
	}
	cleaned, err := CleanupExpiredUploadSessions(ctx, store, repo, now, 10)
	if err != nil || cleaned != 1 || len(store.statKeys) != 0 || len(store.deletedKeys) != 1 || store.deletedKeys[0] != stagingKey || store.stats[finalKey] == nil {
		t.Fatalf("task artifact cleanup = %d, %v stats=%#v final=%#v deleted=%#v", cleaned, err, store.statKeys, store.stats[finalKey], store.deletedKeys)
	}
	found, err := repo.UploadSessions().FindByID(ctx, session.ID)
	if err != nil || found.Status != model.UploadSessionPending {
		t.Fatalf("task artifact recovery isolation session = %#v, err=%v", found, err)
	}
}

func TestCleanupExpiredTaskArtifactsFairAcrossBatchLimit(t *testing.T) {
	_, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	expiresAt := time.Now().Truncate(time.Second).Add(-time.Minute)
	store.stats = make(map[string]*storage.ObjectInfo)
	const (
		totalSessions = 101
		batchLimit    = 100
	)
	for i := 0; i < totalSessions; i++ {
		relPath := fmt.Sprintf("output/fair-%03d.md", i)
		stagingKey := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/" + relPath
		seedTaskArtifactUploadSession(t, repo, task, stagingKey, relPath, 7, func(session *model.UploadSession) {
			session.ExpiresAt = expiresAt
			if i == totalSessions-1 {
				session.ExpiresAt = expiresAt.Add(time.Second)
			}
		})
		store.stats[stagingKey] = &storage.ObjectInfo{Key: stagingKey, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "staging"}
	}

	cleanupAt := expiresAt.Add(time.Minute)
	cleaned, err := CleanupExpiredUploadSessions(ctx, store, repo, cleanupAt, batchLimit)
	if err != nil || cleaned != batchLimit || len(store.deletedKeys) != batchLimit {
		t.Fatalf("first cleanup = %d, %v deleted=%d; want %d", cleaned, err, len(store.deletedKeys), batchLimit)
	}
	cleaned, err = CleanupExpiredUploadSessions(ctx, store, repo, cleanupAt, batchLimit)
	if err != nil || cleaned != totalSessions-batchLimit {
		t.Fatalf("second cleanup = %d, %v; want remaining %d", cleaned, err, totalSessions-batchLimit)
	}
	deleted := make(map[string]struct{}, len(store.deletedKeys))
	for _, key := range store.deletedKeys {
		deleted[key] = struct{}{}
	}
	if len(deleted) != totalSessions {
		t.Fatalf("unique staging keys cleaned after two batches = %d, want %d (raw deletes=%d)", len(deleted), totalSessions, len(store.deletedKeys))
	}
}

func TestFinalizeTaskArtifactManifestRequiresMatchingPendingStagingSession(t *testing.T) {
	for _, tt := range []struct {
		name   string
		seed   bool
		mutate func(*model.UploadSession)
	}{
		{name: "missing session"},
		{name: "foreign owner", seed: true, mutate: func(s *model.UploadSession) { s.UserID = uuid.NewString() }},
		{name: "wrong purpose", seed: true, mutate: func(s *model.UploadSession) { s.Purpose = DirectUploadPurposeProjectReference }},
		{name: "wrong key", seed: true, mutate: func(s *model.UploadSession) { s.StagingKey += ".other" }},
		{name: "wrong size", seed: true, mutate: func(s *model.UploadSession) { s.Size++ }},
		{name: "expired", seed: true, mutate: func(s *model.UploadSession) { s.ExpiresAt = time.Now().Add(-time.Minute) }},
		{name: "non-pending", seed: true, mutate: func(s *model.UploadSession) { s.Status = model.UploadSessionExpired }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, store, task := newTaskArtifactTestService(t)
			ctx := context.Background()
			executionID := startTaskArtifactExecution(t, repo, task)
			stagingKey := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
			if tt.seed {
				seedTaskArtifactUploadSession(t, repo, task, stagingKey, "output/article.md", 7, tt.mutate)
			}
			store.stats = map[string]*storage.ObjectInfo{
				stagingKey: {Key: stagingKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "staging"},
			}
			err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
				TaskID: task.ID, ExecutionID: executionID,
				Files: []TaskArtifactManifestFile{taskArtifactManifestEntry("output/article.md", stagingKey, 7, taskArtifactTestSHA256)},
			})
			if !errors.Is(err, ErrTaskArtifactInvalid) {
				t.Fatalf("FinalizeTaskArtifactManifest error = %v, want invalid staging session", err)
			}
			if len(store.promoteCalls) != 0 || len(store.deletedKeys) != 0 {
				t.Fatalf("invalid staging session mutated storage: promote=%#v delete=%#v", store.promoteCalls, store.deletedKeys)
			}
			rows, findErr := repo.TaskFiles().FindByExecutionID(ctx, executionID)
			if findErr != nil || len(rows) != 0 {
				t.Fatalf("invalid staging session persisted rows = %#v, err=%v", rows, findErr)
			}
		})
	}
}

func TestFinalizeTaskArtifactManifestRetainsStagingWhenPersistenceFails(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	sessions := &recordingTaskArtifactUploadSessionRepository{UploadSessionRepository: repo.UploadSessions()}
	svc.repo = &taskArtifactRepositoryOverride{Repository: repo, uploadSessions: sessions}
	prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.stats = map[string]*storage.ObjectInfo{
		prepared.Key: {Key: prepared.Key, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "staging"},
	}
	entry := taskArtifactManifestEntry("output/article.md", prepared.Key, 7, taskArtifactTestSHA256)
	err = svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{entry, entry},
	})
	if !errors.Is(err, ErrTaskArtifactPersistence) {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want persistence failure", err)
	}
	if store.stats[prepared.Key] == nil || len(store.deletedKeys) != 0 {
		t.Fatalf("failed persistence cleaned staging: object=%#v deleted=%#v", store.stats[prepared.Key], store.deletedKeys)
	}
	found, findErr := repo.UploadSessions().FindByID(ctx, prepared.UploadID)
	if findErr != nil || found.Status != model.UploadSessionPending || found.FinalizationToken != "" || len(sessions.releasedIDs) != 1 || sessions.releasedIDs[0] != prepared.UploadID {
		t.Fatalf("failed persistence claim release: session=%#v err=%v releases=%#v", found, findErr, sessions.releasedIDs)
	}
}

func TestFinalizeTaskArtifactManifestRejectsChangedPromotionSourceWithoutPersistence(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	stagingKey := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
	seedTaskArtifactUploadSession(t, repo, task, stagingKey, "output/article.md", 7, nil)
	store.stats = map[string]*storage.ObjectInfo{
		stagingKey: {Key: stagingKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "observed-etag"},
	}
	store.promoteErr = storage.ErrPromotionPreconditionFailed
	err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{taskArtifactManifestEntry("output/article.md", stagingKey, 7, taskArtifactTestSHA256)},
	})
	if !errors.Is(err, ErrTaskArtifactUnavailable) || !strings.Contains(err.Error(), "source changed") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want source-changed unavailable error", err)
	}
	rows, findErr := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if findErr != nil || len(rows) != 0 {
		t.Fatalf("failed promotion persisted rows = %#v, err=%v", rows, findErr)
	}
}

func TestFinalizeTaskArtifactManifestRequiresNonemptyStagingETag(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	stagingKey := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
	seedTaskArtifactUploadSession(t, repo, task, stagingKey, "output/article.md", 7, nil)
	store.stats = map[string]*storage.ObjectInfo{
		stagingKey: {Key: stagingKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256},
	}
	err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{taskArtifactManifestEntry("output/article.md", stagingKey, 7, taskArtifactTestSHA256)},
	})
	if !errors.Is(err, ErrTaskArtifactInvalid) || !strings.Contains(err.Error(), "staging ETag is required") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want missing ETag rejection", err)
	}
	if len(store.promoteCalls) != 0 {
		t.Fatalf("missing ETag triggered promotions: %#v", store.promoteCalls)
	}
}

func TestFinalizeTaskArtifactManifestDelayedStagingOverwriteCannotChangeFinal(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	stagingKey := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
	seedTaskArtifactUploadSession(t, repo, task, stagingKey, "output/article.md", 7, nil)
	store.stats = map[string]*storage.ObjectInfo{
		stagingKey: {Key: stagingKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "version-a"},
	}
	store.objects = map[string][]byte{stagingKey: []byte("version-a")}
	store.afterPromote = func(sourceKey, _ string) {
		store.stats[sourceKey] = &storage.ObjectInfo{Key: sourceKey, Size: 9, SHA256: strings.Repeat("b", 64), ETag: "version-b"}
		store.objects[sourceKey] = []byte("version-b")
	}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{taskArtifactManifestEntry("output/article.md", stagingKey, 7, taskArtifactTestSHA256)},
	}); err != nil {
		t.Fatal(err)
	}
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	final := store.stats[finalKey]
	if final == nil || final.Size != 7 || final.SHA256 != taskArtifactTestSHA256 || final.ETag != "version-a" {
		t.Fatalf("immutable final metadata = %#v, want promoted version A", final)
	}
	if got := string(store.objects[finalKey]); got != "version-a" {
		t.Fatalf("immutable final bytes = %q, want version-a after delayed staging overwrite", got)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(rows) != 1 || rows[0].OSSKey != finalKey || rows[0].OSSURL != store.GetURL(finalKey) {
		t.Fatalf("persisted rows = %#v, err=%v", rows, err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsConflictingRaceFinal(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	stagingKey := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
	finalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		stagingKey: {Key: stagingKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "staging-etag"},
		finalKey:   {Key: finalKey, Size: 8, ContentType: "text/markdown", SHA256: strings.Repeat("b", 64), ETag: "conflict-etag"},
	}
	err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{taskArtifactManifestEntry("output/article.md", stagingKey, 7, taskArtifactTestSHA256)},
	})
	if !errors.Is(err, ErrTaskArtifactInvalid) || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want conflicting immutable final rejection", err)
	}
	rows, findErr := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if findErr != nil || len(rows) != 0 {
		t.Fatalf("conflicting final persisted rows = %#v, err=%v", rows, findErr)
	}
}

func TestFinalizeTaskArtifactManifestRejectsMalformedOrCrossScopeStagingKeys(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	prefix := buildTaskArtifactStoragePrefix(task, executionID)
	otherExecutionID := uuid.NewString()
	validFinalKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	validStagingKey := prefix + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
	for _, tt := range []struct {
		name       string
		key        string
		storageKey string
	}{
		{name: "legacy deterministic", key: prefix + "output/article.md"},
		{name: "malformed UUID", key: prefix + "staging/sha256/" + taskArtifactTestSHA256 + "/not-a-uuid/output/article.md"},
		{name: "cross hash", key: prefix + "staging/sha256/" + strings.Repeat("b", 64) + "/" + uuid.NewString() + "/output/article.md"},
		{name: "cross path", key: prefix + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/other.md"},
		{name: "cross execution", key: buildTaskArtifactStoragePrefix(task, otherExecutionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"},
		{name: "final leading whitespace", key: " " + validFinalKey, storageKey: validFinalKey},
		{name: "final leading slash", key: "/" + validFinalKey, storageKey: validFinalKey},
		{name: "staging leading whitespace", key: " " + validStagingKey, storageKey: validStagingKey},
		{name: "staging leading slash", key: "/" + validStagingKey, storageKey: validStagingKey},
	} {
		t.Run(tt.name, func(t *testing.T) {
			storageKey := firstNonEmptyString(tt.storageKey, tt.key)
			store.stats = map[string]*storage.ObjectInfo{storageKey: {Key: storageKey, Size: 7, SHA256: taskArtifactTestSHA256, ETag: "etag"}}
			err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
				TaskID: task.ID, ExecutionID: executionID,
				Files: []TaskArtifactManifestFile{taskArtifactManifestEntry("output/article.md", tt.key, 7, taskArtifactTestSHA256)},
			})
			if !errors.Is(err, ErrTaskArtifactInvalid) {
				t.Fatalf("FinalizeTaskArtifactManifest error = %v, want invalid object key", err)
			}
		})
	}
}

func TestTaskArtifactImmutableVersionsPreserveRowIdentityAndReuseFinal(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	uploadVersion := func(hash, etag string) string {
		t.Helper()
		prepared, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
			TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: hash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if store.stats == nil {
			store.stats = make(map[string]*storage.ObjectInfo)
		}
		store.stats[prepared.Key] = &storage.ObjectInfo{Key: prepared.Key, Size: 7, ContentType: "text/markdown", SHA256: hash, ETag: etag}
		if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
			TaskID: task.ID, ExecutionID: executionID,
			Files: []TaskArtifactManifestFile{taskArtifactManifestEntry("output/article.md", prepared.Key, 7, hash)},
		}); err != nil {
			t.Fatal(err)
		}
		return expectedTaskArtifactFinalKey(task, executionID, hash, "output/article.md")
	}
	firstKey := uploadVersion(taskArtifactTestSHA256, "etag-a")
	firstRows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(firstRows) != 1 {
		t.Fatalf("first rows = %#v, err=%v", firstRows, err)
	}
	firstID := firstRows[0].ID
	credentialCalls := 0
	retry, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfigWithCredentialCounter(t, &credentialCalls), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry.UploadRequired || retry.Key != firstKey || credentialCalls != 0 {
		t.Fatalf("unchanged retry = %#v credentials=%d, want immutable reuse", retry, credentialCalls)
	}
	secondHash := strings.Repeat("b", 64)
	secondKey := uploadVersion(secondHash, "etag-b")
	secondRows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(secondRows) != 1 {
		t.Fatalf("second rows = %#v, err=%v", secondRows, err)
	}
	if secondKey == firstKey || secondRows[0].ID != firstID || secondRows[0].OSSKey != secondKey || secondRows[0].ContentHash != secondHash {
		t.Fatalf("versioned row = %#v, want stable ID %s and new key distinct from %s", secondRows[0], firstID, firstKey)
	}
}

func TestFinalizeTaskArtifactManifestRetriesPartialPromotionIdempotently(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	sessions := &recordingTaskArtifactUploadSessionRepository{UploadSessionRepository: repo.UploadSessions()}
	svc.repo = &taskArtifactRepositoryOverride{Repository: repo, uploadSessions: sessions}
	hashB := strings.Repeat("b", 64)
	stagingA := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + taskArtifactTestSHA256 + "/" + uuid.NewString() + "/output/article.md"
	stagingB := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + hashB + "/" + uuid.NewString() + "/output/cover.png"
	sessionA := seedTaskArtifactUploadSession(t, repo, task, stagingA, "output/article.md", 7, nil)
	sessionB := seedTaskArtifactUploadSession(t, repo, task, stagingB, "output/cover.png", 8, nil)
	store.stats = map[string]*storage.ObjectInfo{
		stagingA: {Key: stagingA, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256, ETag: "etag-a"},
		stagingB: {Key: stagingB, Size: 8, ContentType: "image/png", SHA256: hashB, ETag: "etag-b"},
	}
	store.promoteErrBySource = map[string]error{stagingB: storage.ErrPromotionPreconditionFailed}
	manifest := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{
		taskArtifactManifestEntry("output/article.md", stagingA, 7, taskArtifactTestSHA256),
		taskArtifactManifestEntry("output/cover.png", stagingB, 8, hashB),
	}}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); !errors.Is(err, ErrTaskArtifactUnavailable) {
		t.Fatalf("first finalize error = %v, want promotion failure", err)
	}
	if len(sessions.releasedIDs) != 2 {
		t.Fatalf("partial promotion releases = %#v, want both claims released", sessions.releasedIDs)
	}
	for _, sessionID := range []string{sessionA.ID, sessionB.ID} {
		found, findErr := repo.UploadSessions().FindByID(ctx, sessionID)
		if findErr != nil || found.Status != model.UploadSessionPending || found.FinalizationToken != "" {
			t.Fatalf("partial promotion session %s = %#v, err=%v", sessionID, found, findErr)
		}
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("partial promotion persisted rows = %#v, err=%v", rows, err)
	}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); err != nil {
		t.Fatalf("retry finalize: %v", err)
	}
	rows, err = repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("retry rows = %#v, err=%v", rows, err)
	}
	wantKeys := map[string]bool{
		expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md"): true,
		expectedTaskArtifactFinalKey(task, executionID, hashB, "output/cover.png"):                   true,
	}
	for _, row := range rows {
		if !wantKeys[row.OSSKey] {
			t.Fatalf("retry persisted non-final key %q", row.OSSKey)
		}
	}
}

func TestPrepareTaskArtifactUploadScopesKeyToUserProjectTask(t *testing.T) {
	svc, _, store, task := newTaskArtifactTestService(t)

	result, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, "", taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		RelativePath: "output/article.md",
		Filename:     "article.md",
		ContentType:  "text/markdown",
		Size:         12345,
		SHA256:       taskArtifactTestSHA256,
	})
	if err != nil {
		t.Fatalf("PrepareTaskArtifactUpload: %v", err)
	}

	assertTaskArtifactStagingKey(t, task, "", taskArtifactTestSHA256, "output/article.md", result.Key)
	if store.uploadKey != result.Key || store.uploadContentType != "text/markdown" {
		t.Fatalf("signed upload = key %q content-type %q, want %q text/markdown", store.uploadKey, store.uploadContentType, result.Key)
	}
	if result.STSAccessKeyID != "sts-ak" || result.STSSecurityToken != "sts-token" {
		t.Fatalf("sts fields = %#v", result)
	}
}

func TestPrepareTaskArtifactUploadSkipsOnlyMatchingStoredObject(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	key := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	req := TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md",
		ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	}

	for _, tt := range []struct {
		name            string
		info            storage.ObjectInfo
		uploadRequired  bool
		wantStoredETag  string
		wantCredentials bool
		wantError       bool
	}{
		{
			name:            "matching size and hash",
			info:            storage.ObjectInfo{Key: key, Size: 7, ContentType: "text/markdown", ETag: "etag-1", SHA256: taskArtifactTestSHA256},
			uploadRequired:  false,
			wantStoredETag:  "etag-1",
			wantCredentials: false,
		},
		{
			name:      "wrong hash",
			info:      storage.ObjectInfo{Key: key, Size: 7, ContentType: "text/markdown", SHA256: strings.Repeat("b", 64)},
			wantError: true,
		},
		{
			name:      "wrong size",
			info:      storage.ObjectInfo{Key: key, Size: 8, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256},
			wantError: true,
		},
		{
			name:      "missing hash metadata",
			info:      storage.ObjectInfo{Key: key, Size: 7, ContentType: "text/markdown"},
			wantError: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store.stats = map[string]*storage.ObjectInfo{key: &tt.info}
			store.uploadKey = ""
			store.uploadContentType = ""
			store.uploadURLCalls = 0
			credentialCalls := 0

			result, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfigWithCredentialCounter(t, &credentialCalls), req)
			if tt.wantError {
				if !errors.Is(err, ErrTaskArtifactInvalid) {
					t.Fatalf("PrepareTaskArtifactUpload error = %v, want ErrTaskArtifactInvalid", err)
				}
				if store.uploadURLCalls != 0 || credentialCalls != 0 {
					t.Fatalf("mismatching immutable final issued authority: urls=%d credentials=%d", store.uploadURLCalls, credentialCalls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.UploadRequired != tt.uploadRequired {
				t.Fatalf("UploadRequired = %v, want %v", result.UploadRequired, tt.uploadRequired)
			}
			if result.ETag != tt.wantStoredETag {
				t.Fatalf("ETag = %q, want %q", result.ETag, tt.wantStoredETag)
			}
			if gotCredentials := result.STSAccessKeyID != "" || result.STSAccessKeySecret != "" || result.STSSecurityToken != ""; gotCredentials != tt.wantCredentials {
				t.Fatalf("credentials present = %v, want %v", gotCredentials, tt.wantCredentials)
			}
			if !tt.uploadRequired {
				if store.uploadURLCalls != 0 {
					t.Fatalf("matching object requested %d upload URLs", store.uploadURLCalls)
				}
				if credentialCalls != 0 {
					t.Fatalf("matching object issued %d credentials", credentialCalls)
				}
				return
			}
			if result.Headers["X-Oss-Meta-Sha256"] != taskArtifactTestSHA256 {
				t.Fatalf("hash header = %q, want %q", result.Headers["X-Oss-Meta-Sha256"], taskArtifactTestSHA256)
			}
		})
	}
}

func TestPrepareTaskArtifactUploadFailsClosedWhenStatIsUnavailable(t *testing.T) {
	for _, tt := range []struct {
		name           string
		statErr        error
		statReturnsNil bool
	}{
		{name: "unexpected error", statErr: errors.New("storage unavailable")},
		{name: "nil object without error", statReturnsNil: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, store, task := newTaskArtifactTestService(t)
			ctx := context.Background()
			executionID := startTaskArtifactExecution(t, repo, task)
			store.statErr = tt.statErr
			store.statReturnsNil = tt.statReturnsNil
			credentialCalls := 0

			_, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfigWithCredentialCounter(t, &credentialCalls), TaskArtifactPrepareRequest{
				TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
			})
			if !errors.Is(err, ErrTaskArtifactUnavailable) {
				t.Fatalf("PrepareTaskArtifactUpload error = %v, want ErrTaskArtifactUnavailable", err)
			}
			if store.uploadURLCalls != 0 {
				t.Fatalf("unavailable stat requested %d upload URLs", store.uploadURLCalls)
			}
			if credentialCalls != 0 {
				t.Fatalf("unavailable stat issued %d credentials", credentialCalls)
			}
		})
	}
}

func TestFinalizeTaskArtifactManifestRejectsStoredHashMismatch(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	key := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		key: {Key: key, Size: 7, ContentType: "text/markdown", SHA256: strings.Repeat("b", 64)},
	}

	err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{{RelativePath: "output/article.md", ObjectKey: key, ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256}},
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want sha256 mismatch", err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsStoredSizeMismatch(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	key := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		key: {Key: key, Size: 8, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256},
	}

	err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{{RelativePath: "output/article.md", ObjectKey: key, ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256}},
	})
	if err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want size mismatch", err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsEntireMultiFileManifestWithoutReplacingPendingRows(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	prefix := buildTaskArtifactStoragePrefix(task, executionID)
	existing, err := repo.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, executionID, &model.TaskFile{
		ID: uuid.NewString(), Role: model.FileRoleMarkdown, FileName: "previous.md", FilePath: "mcp/previous.md",
		MimeType: "text/markdown", FileSize: 9, ContentHash: strings.Repeat("c", 64),
		OSSKey: prefix + "mcp/previous.md", OSSURL: store.GetURL(prefix + "mcp/previous.md"), StorageProvider: store.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}

	validKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	mismatchedKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/cover.png")
	store.stats = map[string]*storage.ObjectInfo{
		validKey:      {Key: validKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256},
		mismatchedKey: {Key: mismatchedKey, Size: 7, ContentType: "image/png", SHA256: strings.Repeat("b", 64)},
	}
	err = svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{
			{RelativePath: "output/article.md", ObjectKey: validKey, ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256},
			{RelativePath: "output/cover.png", ObjectKey: mismatchedKey, ContentType: "image/png", Size: 7, SHA256: taskArtifactTestSHA256},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want sha256 mismatch", err)
	}
	after, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || len(after) != 1 {
		t.Fatalf("pending rows = %#v, want unchanged %#v", after, before)
	}
	if got, want := after[0], existing; got.ID != want.ID || got.FilePath != want.FilePath || got.ContentHash != want.ContentHash || got.OSSKey != want.OSSKey || got.FileSize != want.FileSize {
		t.Fatalf("pending row changed = %#v, want %#v", got, want)
	}
}

func TestTaskArtifactEndpointsRejectTerminalExecution(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	if transitioned, err := repo.TaskExecutions().Transition(ctx, executionID, []string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded, model.ExecutionTransition{}); err != nil || !transitioned {
		t.Fatalf("transition execution: transitioned=%v err=%v", transitioned, err)
	}

	_, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", ContentType: "text/markdown", Size: 7, SHA256: taskArtifactTestSHA256,
	})
	if !errors.Is(err, ErrTaskArtifactExecutionConflict) {
		t.Fatalf("PrepareTaskArtifactUpload error = %v, want ErrTaskArtifactExecutionConflict", err)
	}

	err = svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID})
	if !errors.Is(err, ErrTaskArtifactExecutionConflict) {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want ErrTaskArtifactExecutionConflict", err)
	}
}

func TestFinalizeTaskArtifactManifestPreservesExecutionMCPArtifacts(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	task.Type = model.PlatformSeednote
	task.HasContentImage = true
	executionID := uuid.NewString()
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes",
		Status: model.TaskExecutionRunning, Started: true, StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}

	generated, err := svc.UploadExecutionTaskFileFromReader(ctx, task.ID, task.UserID, executionID,
		"output/seednote/title/image_01.png", strings.NewReader("png"), "image/png", 3)
	if err != nil {
		t.Fatal(err)
	}
	if generated.ExecutionID != executionID || generated.State != model.TaskFileStatePending || !strings.Contains(generated.OSSKey, "/mcp/") {
		t.Fatalf("generated execution artifact = %#v", generated)
	}

	var paths []string
	for _, name := range seednoteCompletionArtifactNamesForTest(false, false) {
		paths = append(paths, "output/seednote/title/"+name)
	}
	manifest := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID}
	store.stats = make(map[string]*storage.ObjectInfo, len(paths))
	for _, relPath := range paths {
		key := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, relPath)
		store.stats[key] = &storage.ObjectInfo{Key: key, Size: 3, ContentType: DetectTaskFileMIME(relPath), SHA256: taskArtifactTestSHA256}
		manifest.Files = append(manifest.Files, TaskArtifactManifestFile{
			RelativePath: relPath, ObjectKey: key, ContentType: DetectTaskFileMIME(relPath), Size: 3, SHA256: taskArtifactTestSHA256,
		})
	}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, manifest); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := len(paths) + 1
	if len(pending) != wantCount {
		t.Fatalf("pending artifact count = %d, want %d: %#v", len(pending), wantCount, pending)
	}
	if validation := serveragent.ValidateTaskArtifactsFromTaskFiles(task, pending); !validation.Valid {
		t.Fatalf("merged execution artifacts are invalid: %#v", validation)
	}
}

func TestFinalizeTaskArtifactManifestWorkspacePathWinsWithoutBreakingSettlement(t *testing.T) {
	repo, db := newBillingServiceRepositoryWithDB(t)
	fixture := newBillingWalletFixtureWithRepository(t, repo, 500, 0, 0)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	store := &fakeTaskArtifactStorage{name: "oss"}
	svc := newTestTaskService(fixture.repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetBillingWalletService(fixture.wallet)
	projectID := createTestProject(t, fixture.repo, billingWalletUserID, model.PlatformArticle)
	task := &model.Task{
		ID: uuid.NewString(), UserID: billingWalletUserID, ProjectID: projectID,
		Type: model.PlatformArticle, Status: model.TaskStatusRunning,
	}
	if err := fixture.repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	executionID := startTaskArtifactExecution(t, fixture.repo, task)
	operationID := "internal:image:collision"
	generated, err := svc.UploadExecutionTaskFileWithSettlementFromReader(
		ctx, task.ID, task.UserID, executionID, "output/cover.png",
		strings.NewReader("old"), "image/png", 3,
		TaskFileOperationSettlement{
			CatalogID: "retail-test-v1", SKUID: "image.cover.v1", ToolCallID: operationID,
			RequestFingerprint: billingFingerprint(operationID),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	operationObjectKey := generated.OSSKey
	operationContentHash := generated.ContentHash

	settlementKey := billingFingerprint(task.ID, executionID, operationID)
	if processed, err := fixture.wallet.ProcessSettlementOutbox(ctx, 10); err != nil || processed != 1 {
		t.Fatalf("ProcessSettlementOutbox = %d, %v, want one processed settlement", processed, err)
	}
	settlementBefore, err := fixture.repo.Billing().FindSettlementByKey(ctx, "mcp-image-settlement", settlementKey)
	if err != nil {
		t.Fatal(err)
	}
	if settlementBefore.Status != "processed" || settlementBefore.Attempts != 1 || settlementBefore.ResourceID != generated.ID {
		t.Fatalf("processed settlement = %#v, want one processed attempt linked to %s", settlementBefore, generated.ID)
	}
	chargeBefore, err := fixture.repo.Billing().FindChargeByOperation(ctx, task.ID, executionID, operationID, "retail-test-v1", "image.cover.v1")
	if err != nil {
		t.Fatal(err)
	}
	if chargeBefore.Kind != model.BillingChargeKindOperation || chargeBefore.Status != model.BillingChargeStatusPosted ||
		chargeBefore.PriceCredits != 500 || chargeBefore.PaidCredits != 500 ||
		chargeBefore.PromotionalCredits != 0 || chargeBefore.DebtCredits != 0 ||
		chargeBefore.ResourceID != generated.ID || chargeBefore.IdempotencyScope != "outbox-charge" || chargeBefore.IdempotencyKey != settlementBefore.ID {
		t.Fatalf("operation charge = %#v, want resource %s linked to settlement %s", chargeBefore, generated.ID, settlementBefore.ID)
	}
	var deductionBefore model.BillingWalletEntry
	if err := db.Where("charge_id = ?", chargeBefore.ID).First(&deductionBefore).Error; err != nil {
		t.Fatal(err)
	}
	if deductionBefore.EventKind != model.BillingWalletEventKindCharge || deductionBefore.PaidDelta != -500 ||
		deductionBefore.PromotionalDelta != 0 || deductionBefore.DebtDelta != 0 || deductionBefore.ResourceID != generated.ID {
		t.Fatalf("wallet deduction = %#v, want one 500-credit paid deduction for %s", deductionBefore, generated.ID)
	}
	accountBefore := fixture.account(t, billingWalletUserID)
	if accountBefore.PaidCredits != 0 || accountBefore.PromotionalCredits != 0 || accountBefore.DebtCredits != 0 {
		t.Fatalf("account after operation charge = %#v, want fully consumed 500-credit balance", accountBefore)
	}
	var outboxBefore, chargesBefore, deductionsBefore int64
	if err := db.Model(&model.BillingSettlementOutbox{}).
		Where("task_id = ? AND attempt_id = ?", task.ID, executionID).
		Count(&outboxBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingCharge{}).
		Where("operation_task_id = ? AND attempt_id = ?", task.ID, executionID).
		Count(&chargesBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingWalletEntry{}).
		Where("charge_id = ?", chargeBefore.ID).
		Count(&deductionsBefore).Error; err != nil {
		t.Fatal(err)
	}

	newHashBytes := sha256.Sum256([]byte("new"))
	newHash := hex.EncodeToString(newHashBytes[:])
	workspaceKey := expectedTaskArtifactFinalKey(task, executionID, newHash, "output/cover.png")
	store.stats = map[string]*storage.ObjectInfo{
		workspaceKey: {Key: workspaceKey, Size: 3, ContentType: "image/png", SHA256: newHash},
	}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID,
		Files: []TaskArtifactManifestFile{{
			RelativePath: "output/cover.png", ObjectKey: workspaceKey,
			ContentType: "image/png", Size: 3, SHA256: newHash,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if processed, err := fixture.wallet.ProcessSettlementOutbox(ctx, 10); err != nil || processed != 0 {
		t.Fatalf("duplicate ProcessSettlementOutbox = %d, %v, want no work", processed, err)
	}

	rows, err := fixture.repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("execution rows = %d, want 1: %#v", len(rows), rows)
	}
	if got := rows[0]; got.ID != generated.ID || got.OSSKey != workspaceKey || got.ContentHash != newHash {
		t.Fatalf("workspace replacement = %#v, want ID %s key %s hash %s", got, generated.ID, workspaceKey, newHash)
	}
	uploadsBeforeReplay := store.uploadCalls
	replayFile, replaySnapshot, err := svc.FindTaskFileOperationSettlement(ctx, task.ID, executionID, operationID, billingFingerprint(operationID))
	if err != nil {
		t.Fatal(err)
	}
	var replay ImageOperationResultSnapshot
	if err := json.Unmarshal(replaySnapshot, &replay); err != nil {
		t.Fatal(err)
	}
	svc.EnrichFilesWithURLs(ctx, []*model.TaskFile{replayFile})
	replay.DownloadURL = replayFile.URL
	if replayFile.ID != generated.ID || replayFile.OSSKey != operationObjectKey || replayFile.ContentHash != operationContentHash || replay.DownloadURL != store.GetURL(operationObjectKey) {
		t.Fatalf("operation replay = file %#v snapshot %#v, want stable ID and immutable operation delivery", replayFile, replay)
	}
	if store.uploadCalls != uploadsBeforeReplay {
		t.Fatalf("operation replay upload calls = %d, want unchanged %d", store.uploadCalls, uploadsBeforeReplay)
	}
	settlementAfter, err := fixture.repo.Billing().FindSettlementByKey(ctx, "mcp-image-settlement", settlementKey)
	if err != nil {
		t.Fatal(err)
	}
	if settlementAfter.ID != settlementBefore.ID || settlementAfter.ResourceID != generated.ID ||
		settlementAfter.Status != settlementBefore.Status || settlementAfter.Attempts != settlementBefore.Attempts {
		t.Fatalf("settlement after replacement = %#v, want stable settlement %s linked to %s", settlementAfter, settlementBefore.ID, generated.ID)
	}
	chargeAfter, err := fixture.repo.Billing().FindChargeByOperation(ctx, task.ID, executionID, operationID, "retail-test-v1", "image.cover.v1")
	if err != nil {
		t.Fatal(err)
	}
	if chargeAfter.ID != chargeBefore.ID || chargeAfter.ResourceID != chargeBefore.ResourceID ||
		chargeAfter.IdempotencyScope != chargeBefore.IdempotencyScope || chargeAfter.IdempotencyKey != chargeBefore.IdempotencyKey ||
		chargeAfter.PriceCredits != chargeBefore.PriceCredits || chargeAfter.PaidCredits != chargeBefore.PaidCredits ||
		chargeAfter.PromotionalCredits != chargeBefore.PromotionalCredits || chargeAfter.DebtCredits != chargeBefore.DebtCredits {
		t.Fatalf("charge after replacement = %#v, want unchanged charge %#v", chargeAfter, chargeBefore)
	}
	var deductionAfter model.BillingWalletEntry
	if err := db.Where("charge_id = ?", chargeBefore.ID).First(&deductionAfter).Error; err != nil {
		t.Fatal(err)
	}
	if deductionAfter.ID != deductionBefore.ID || deductionAfter.PaidDelta != deductionBefore.PaidDelta ||
		deductionAfter.PromotionalDelta != deductionBefore.PromotionalDelta || deductionAfter.DebtDelta != deductionBefore.DebtDelta {
		t.Fatalf("deduction after replacement = %#v, want unchanged deduction %#v", deductionAfter, deductionBefore)
	}
	accountAfter := fixture.account(t, billingWalletUserID)
	if accountAfter.PaidCredits != accountBefore.PaidCredits || accountAfter.PromotionalCredits != accountBefore.PromotionalCredits ||
		accountAfter.DebtCredits != accountBefore.DebtCredits || accountAfter.Version != accountBefore.Version {
		t.Fatalf("account after replacement = %#v, want unchanged projection %#v", accountAfter, accountBefore)
	}
	var outboxAfter, chargesAfter, deductionsAfter int64
	if err := db.Model(&model.BillingSettlementOutbox{}).
		Where("task_id = ? AND attempt_id = ?", task.ID, executionID).
		Count(&outboxAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingCharge{}).
		Where("operation_task_id = ? AND attempt_id = ?", task.ID, executionID).
		Count(&chargesAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingWalletEntry{}).
		Where("charge_id = ?", chargeBefore.ID).
		Count(&deductionsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if outboxBefore != 1 || outboxAfter != outboxBefore || chargesBefore != 1 || chargesAfter != chargesBefore ||
		deductionsBefore != 1 || deductionsAfter != deductionsBefore {
		t.Fatalf("billing rows changed: outbox %d -> %d, charges %d -> %d, deductions %d -> %d",
			outboxBefore, outboxAfter, chargesBefore, chargesAfter, deductionsBefore, deductionsAfter)
	}
}

func TestFinalizeTaskArtifactEmptyManifestPreservesOnlyMCPArtifacts(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	mcpID := uuid.NewString()
	mcpPath := "output/cover.png"
	mcpKey := buildTaskMCPArtifactStoragePrefix(task, executionID) + mcpPath
	workspacePath := "output/article.md"
	workspaceKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, workspacePath)
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{
			ID: mcpID, TaskID: task.ID, ExecutionID: executionID, State: model.TaskFileStatePending,
			Role: model.FileRoleImage, FileName: "cover.png", MimeType: "image/png", FileSize: 3,
			ContentHash: taskArtifactTestSHA256, OSSKey: mcpKey, OSSURL: "https://cdn.example.com/" + mcpKey,
			StorageProvider: "oss", FilePath: mcpPath,
		},
		{
			ID: uuid.NewString(), TaskID: task.ID, ExecutionID: executionID, State: model.TaskFileStatePending,
			Role: model.FileRoleMarkdown, FileName: "article.md", MimeType: "text/markdown", FileSize: 7,
			ContentHash: taskArtifactTestSHA256, OSSKey: workspaceKey, OSSURL: "https://cdn.example.com/" + workspaceKey,
			StorageProvider: "oss", FilePath: workspacePath,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID, ExecutionID: executionID, Files: nil,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("execution rows = %d, want 1: %#v", len(rows), rows)
	}
	if got := rows[0]; got.ID != mcpID || got.FilePath != mcpPath || got.OSSKey != mcpKey {
		t.Fatalf("preserved artifact = %#v, want MCP row %s at %s with key %s", got, mcpID, mcpPath, mcpKey)
	}
}

func TestUpdateTaskFileMetadataRejectsLateExecutionMutation(t *testing.T) {
	t.Run("workspace collision changed delivery identity", func(t *testing.T) {
		svc, repo, store, task := newTaskArtifactTestService(t)
		ctx := context.Background()
		executionID := startTaskArtifactExecution(t, repo, task)
		original, err := svc.UploadExecutionTaskFileFromReader(ctx, task.ID, task.UserID, executionID,
			"output/cover.png", strings.NewReader("old"), "image/png", 3)
		if err != nil {
			t.Fatal(err)
		}
		newHashBytes := sha256.Sum256([]byte("new"))
		newHash := hex.EncodeToString(newHashBytes[:])
		workspaceKey := expectedTaskArtifactFinalKey(task, executionID, newHash, original.FilePath)
		store.stats = map[string]*storage.ObjectInfo{
			workspaceKey: {Key: workspaceKey, Size: 3, ContentType: "image/png", SHA256: newHash},
		}
		if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
			TaskID: task.ID, ExecutionID: executionID,
			Files: []TaskArtifactManifestFile{{
				RelativePath: original.FilePath, ObjectKey: workspaceKey,
				ContentType: "image/png", Size: 3, SHA256: newHash,
			}},
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := svc.UpdateTaskFileMetadata(ctx, original, model.FileRoleCover, "late-media", "https://late.example/image"); !errors.Is(err, repository.ErrTaskFileDeliveryIdentityChanged) {
			t.Fatalf("late metadata error = %v, want ErrTaskFileDeliveryIdentityChanged", err)
		}
		rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
		if err != nil || len(rows) != 1 {
			t.Fatalf("execution rows = %#v, err=%v", rows, err)
		}
		if got := rows[0]; got.ID != original.ID || got.OSSKey != workspaceKey || got.ContentHash != newHash || got.MediaID != "" || got.WechatURL != "" {
			t.Fatalf("workspace delivery mutated by late metadata: %#v", got)
		}
	})

	t.Run("empty manifest removed delivery row", func(t *testing.T) {
		svc, repo, _, task := newTaskArtifactTestService(t)
		ctx := context.Background()
		executionID := startTaskArtifactExecution(t, repo, task)
		original, err := repo.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, executionID, &model.TaskFile{
			Role: model.FileRoleImage, FilePath: "output/stale.png", FileName: "stale.png",
			OSSKey: "workspace/stale.png", ContentHash: taskArtifactTestSHA256,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
			TaskID: task.ID, ExecutionID: executionID,
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := svc.UpdateTaskFileMetadata(ctx, original, model.FileRoleCover, "late-media", "https://late.example/image"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("late metadata error = %v, want gorm.ErrRecordNotFound", err)
		}
		rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
		if err != nil || len(rows) != 0 {
			t.Fatalf("removed execution rows = %#v, err=%v", rows, err)
		}
	})

	t.Run("terminal execution", func(t *testing.T) {
		svc, repo, _, task := newTaskArtifactTestService(t)
		ctx := context.Background()
		executionID := startTaskArtifactExecution(t, repo, task)
		original, err := svc.UploadExecutionTaskFileFromReader(ctx, task.ID, task.UserID, executionID,
			"output/cover.png", strings.NewReader("old"), "image/png", 3)
		if err != nil {
			t.Fatal(err)
		}
		if changed, err := repo.TaskExecutions().Transition(ctx, executionID, []string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded, model.ExecutionTransition{}); err != nil || !changed {
			t.Fatalf("terminal transition changed=%v err=%v", changed, err)
		}

		if _, err := svc.UpdateTaskFileMetadata(ctx, original, model.FileRoleCover, "late-media", "https://late.example/image"); !errors.Is(err, repository.ErrTaskFileTaskNotRunning) {
			t.Fatalf("late metadata error = %v, want ErrTaskFileTaskNotRunning", err)
		}
		rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
		if err != nil || len(rows) != 1 || rows[0].MediaID != "" || rows[0].WechatURL != "" {
			t.Fatalf("terminal execution rows mutated: %#v, err=%v", rows, err)
		}
	})

	t.Run("published manifest", func(t *testing.T) {
		svc, repo, _, task := newTaskArtifactTestService(t)
		ctx := context.Background()
		executionID := startTaskArtifactExecution(t, repo, task)
		original, err := svc.UploadExecutionTaskFileFromReader(ctx, task.ID, task.UserID, executionID,
			"output/cover.png", strings.NewReader("old"), "image/png", 3)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.TaskFiles().PublishCurrentExecution(ctx, task.ID, executionID); err != nil {
			t.Fatal(err)
		}

		if _, err := svc.UpdateTaskFileMetadata(ctx, original, model.FileRoleCover, "late-media", "https://late.example/image"); !errors.Is(err, repository.ErrTaskFileManifestState) {
			t.Fatalf("late metadata error = %v, want ErrTaskFileManifestState", err)
		}
		rows, err := repo.TaskFiles().FindByExecutionID(ctx, executionID)
		if err != nil || len(rows) != 1 || rows[0].State != model.TaskFileStatePublished || rows[0].MediaID != "" || rows[0].WechatURL != "" {
			t.Fatalf("published execution rows mutated: %#v, err=%v", rows, err)
		}
	})
}

func TestUploadExecutionTaskFileWithSettlementPersistsArtifactAndOutboxAtomically(t *testing.T) {
	fixture := newBillingWalletFixture(t, 500, 0, 0)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	store := &fakeTaskArtifactStorage{name: "oss"}
	svc := newTestTaskService(fixture.repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetBillingWalletService(fixture.wallet)
	projectID := createTestProject(t, fixture.repo, billingWalletUserID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: billingWalletUserID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := fixture.repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	executionID := uuid.NewString()
	if err := fixture.repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true}); err != nil {
		t.Fatal(err)
	}
	if won, err := fixture.repo.Tasks().SetCurrentExecution(ctx, task.ID, executionID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}

	file, err := svc.UploadExecutionTaskFileWithSettlementFromReader(ctx, task.ID, task.UserID, executionID, "output/cover.png", strings.NewReader("image"), "image/png", 5, TaskFileOperationSettlement{
		CatalogID: "retail-test-v1", SKUID: "image.cover.v1", ToolCallID: "internal:image:call-1", RequestFingerprint: billingFingerprint("call-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	settlement, err := fixture.repo.Billing().FindSettlementByKey(ctx, "mcp-image-settlement", billingFingerprint(task.ID, executionID, "internal:image:call-1"))
	if err != nil || settlement.ResourceID != file.ID || settlement.TaskID == nil || *settlement.TaskID != task.ID {
		t.Fatalf("settlement = %#v err=%v", settlement, err)
	}

	broken := newTestTaskService(fixture.repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	_, err = broken.UploadExecutionTaskFileWithSettlementFromReader(ctx, task.ID, task.UserID, executionID, "output/content.png", strings.NewReader("other"), "image/png", 5, TaskFileOperationSettlement{
		CatalogID: "retail-test-v1", SKUID: "image.cover.v1", ToolCallID: "internal:image:call-2", RequestFingerprint: billingFingerprint("call-2"),
	})
	if err == nil {
		t.Fatal("missing wallet did not fail settlement transaction")
	}
	pending, findErr := fixture.repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if findErr != nil || len(pending) != 1 || pending[0].FilePath != "output/cover.png" {
		t.Fatalf("artifact transaction leaked pending rows: files=%#v err=%v", pending, findErr)
	}
	if settlement, findErr := fixture.repo.Billing().FindSettlementByKey(ctx, "mcp-image-settlement", billingFingerprint(task.ID, executionID, "internal:image:call-2")); !errors.Is(findErr, gorm.ErrRecordNotFound) || settlement != nil {
		t.Fatalf("outbox committed without artifact: settlement=%#v err=%v", settlement, findErr)
	}
}

func TestPrepareTaskArtifactUploadRejectsUnsafeRelativePath(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)

	for _, tt := range []struct {
		name        string
		relPath     string
		wantMessage string
	}{
		{name: "parent traversal", relPath: "../secret.md", wantMessage: "invalid relative path"},
		{name: "dotfile", relPath: "output/.env", wantMessage: "refusing to upload dotfile"},
		{name: "runtime directory", relPath: ".git/config", wantMessage: "refusing to upload runtime directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, "", taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
				RelativePath: tt.relPath,
				Filename:     "secret.md",
				ContentType:  "text/markdown",
				Size:         12,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("PrepareTaskArtifactUpload error = %v, want %q", err, tt.wantMessage)
			}
		})
	}
}

func TestPrepareTaskArtifactUploadRejectsWrongUserAndNonOSSStorage(t *testing.T) {
	svc, _, store, task := newTaskArtifactTestService(t)

	_, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, uuid.NewString(), "", taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		RelativePath: "output/article.md",
		ContentType:  "text/markdown",
		Size:         12,
	})
	if err == nil || !strings.Contains(err.Error(), "authenticated user") {
		t.Fatalf("PrepareTaskArtifactUpload wrong user error = %v, want access rejection", err)
	}

	store.name = "local"
	_, err = svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, "", taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		RelativePath: "output/article.md",
		ContentType:  "text/markdown",
		Size:         12,
	})
	if err == nil || !strings.Contains(err.Error(), "require OSS storage") {
		t.Fatalf("PrepareTaskArtifactUpload non-OSS error = %v, want OSS rejection", err)
	}
}

func TestPrepareTaskArtifactUploadRejectsMissingSTSRole(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)
	cfg := taskArtifactDirectUploadConfig(t)
	cfg.Storage.STSRoleArn = ""
	cfg.CredentialIssuer = nil
	cfg.Storage.AccessKeyID = "ak"
	cfg.Storage.AccessKeySecret = "secret"

	_, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, "", cfg, TaskArtifactPrepareRequest{
		RelativePath: "output/article.md",
		ContentType:  "text/markdown",
		Size:         12,
		SHA256:       taskArtifactTestSHA256,
	})
	if err == nil || !strings.Contains(err.Error(), "storage.sts_role_arn") {
		t.Fatalf("PrepareTaskArtifactUpload missing STS role error = %v, want sts_role_arn rejection", err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsObjectOutsideTaskPrefix(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)

	err := svc.FinalizeTaskArtifactManifest(context.Background(), task.ID, task.UserID, "", TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files: []TaskArtifactManifestFile{{
			RelativePath: "output/article.md",
			ObjectKey:    "uploads/users/other/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/output/article.md",
			ContentType:  "text/markdown",
			Size:         12,
			SHA256:       strings.Repeat("a", 64),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "outside task artifact prefix") {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want prefix rejection", err)
	}
}

func TestFinalizeTaskArtifactManifestPersistsTaskFile(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	body := []byte("# title\n\nbody")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	objectKey := expectedTaskArtifactFinalKey(task, "", hash, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{
		objectKey: {
			Key:         objectKey,
			Size:        int64(len(body)),
			ContentType: "text/markdown",
			ETag:        "etag-1",
			SHA256:      hash,
		},
	}

	if err := svc.FinalizeTaskArtifactManifest(context.Background(), task.ID, task.UserID, "", TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files: []TaskArtifactManifestFile{{
			RelativePath: "output/article.md",
			ObjectKey:    objectKey,
			ContentType:  "text/markdown",
			Size:         int64(len(body)),
			SHA256:       hash,
		}},
	}); err != nil {
		t.Fatalf("FinalizeTaskArtifactManifest: %v", err)
	}

	files, err := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("find task files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("file count = %d, want 1", len(files))
	}
	file := files[0]
	if file.FilePath != "output/article.md" || file.OSSKey != objectKey {
		t.Fatalf("file path/key = %q/%q, want output/article.md/%q", file.FilePath, file.OSSKey, objectKey)
	}
	if file.Role != model.FileRoleMarkdown || file.MimeType != "text/markdown" {
		t.Fatalf("file role/mime = %q/%q, want markdown/text", file.Role, file.MimeType)
	}
	if file.FileSize != int64(len(body)) || file.ContentHash != hash {
		t.Fatalf("file size/hash = %d/%q, want %d/%q", file.FileSize, file.ContentHash, len(body), hash)
	}
}

func TestExecutionArtifactManifestStaysPendingUntilPublication(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := uuid.NewString()
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, StartedAt: &now}); err != nil {
		t.Fatal(err)
	}
	objectKey := expectedTaskArtifactFinalKey(task, executionID, taskArtifactTestSHA256, "output/article.md")
	store.stats = map[string]*storage.ObjectInfo{objectKey: {Key: objectKey, Size: 7, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256}}
	req := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{{RelativePath: "output/article.md", ObjectKey: objectKey, Size: 7, SHA256: taskArtifactTestSHA256}}}
	if err := svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, executionID, req); err != nil {
		t.Fatal(err)
	}
	visible, _ := repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if len(visible) != 0 {
		t.Fatalf("pending manifest leaked: %#v", visible)
	}
	pending, _ := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if len(pending) != 1 || pending[0].State != model.TaskFileStatePending {
		t.Fatalf("pending = %#v", pending)
	}
	if err := repo.TaskFiles().PublishCurrentExecution(ctx, task.ID, executionID); err != nil {
		t.Fatal(err)
	}
	visible, _ = repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if len(visible) != 1 || visible[0].ExecutionID != executionID {
		t.Fatalf("published = %#v", visible)
	}
}

func TestExecutionArtifactPrepareRejectsMissingOrStaleIdentity(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := uuid.NewString()
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, StartedAt: &now}); err != nil {
		t.Fatal(err)
	}
	request := TaskArtifactPrepareRequest{TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", Size: 7, SHA256: taskArtifactTestSHA256}
	result, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfig(t), request)
	if err != nil {
		t.Fatal(err)
	}
	want := "/executions/" + executionID + "/artifacts/staging/sha256/" + taskArtifactTestSHA256 + "/"
	if !strings.Contains(result.Key, want) {
		t.Fatalf("key = %q, want segment %q", result.Key, want)
	}
	if _, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, "", taskArtifactDirectUploadConfig(t), request); err == nil || !strings.Contains(err.Error(), "execution identity") {
		t.Fatalf("API-key execution impersonation error = %v", err)
	}
	other := uuid.NewString()
	if _, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, other, taskArtifactDirectUploadConfig(t), request); err == nil {
		t.Fatal("stale authenticated execution accepted")
	}
}

func TestCloudTaskRejectsLegacyArtifactRequestsWithoutExecutionIdentity(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := uuid.NewString()
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, StartedAt: &now}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, "", taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		TaskID: task.ID, RelativePath: "output/article.md", Size: 7,
	})
	if err == nil || !strings.Contains(err.Error(), "execution identity") {
		t.Fatalf("legacy prepare error = %v, want execution identity rejection", err)
	}
	if store.uploadKey != "" {
		t.Fatalf("rejected prepare signed object %q", store.uploadKey)
	}

	err = svc.FinalizeTaskArtifactManifest(ctx, task.ID, task.UserID, "", TaskArtifactManifestRequest{TaskID: task.ID, Files: nil})
	if err == nil || !strings.Contains(err.Error(), "execution identity") {
		t.Fatalf("legacy manifest error = %v, want execution identity rejection", err)
	}
	rows, findErr := repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if findErr != nil || len(rows) != 0 {
		t.Fatalf("rejected manifest rows = %#v, %v", rows, findErr)
	}
}
