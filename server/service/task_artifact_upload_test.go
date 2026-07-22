package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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
	name              string
	uploadKey         string
	uploadContentType string
	uploadURLCalls    int
	stats             map[string]*storage.ObjectInfo
	statErr           error
	statReturnsNil    bool
}

func (f *fakeTaskArtifactStorage) Name() string {
	if f.name != "" {
		return f.name
	}
	return "oss"
}

func (f *fakeTaskArtifactStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
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

func (f *fakeTaskArtifactStorage) Delete(context.Context, string) error { return nil }

func (f *fakeTaskArtifactStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return "https://download.example.com/" + key, nil
}

func (f *fakeTaskArtifactStorage) HasCustomDomain() bool { return true }

func (f *fakeTaskArtifactStorage) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://cdn.example.com/")
}

func (f *fakeTaskArtifactStorage) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
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
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
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
			if !strings.Contains(req.Policy, "uploads/users/") || !strings.Contains(req.Policy, "/artifacts/output/article.md") {
				t.Fatalf("policy = %s, want task artifact object key", req.Policy)
			}
			return &UploadCredential{
				AccessKeyID:     "sts-ak",
				AccessKeySecret: "sts-secret",
				SecurityToken:   "sts-token",
				ExpiresAt:       time.Now().Add(15 * time.Minute),
			}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC) },
	}
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

	wantKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/output/article.md"
	if result.Key != wantKey {
		t.Fatalf("key = %q, want %q", result.Key, wantKey)
	}
	if store.uploadKey != wantKey || store.uploadContentType != "text/markdown" {
		t.Fatalf("signed upload = key %q content-type %q, want %q text/markdown", store.uploadKey, store.uploadContentType, wantKey)
	}
	if result.STSAccessKeyID != "sts-ak" || result.STSSecurityToken != "sts-token" {
		t.Fatalf("sts fields = %#v", result)
	}
}

func TestPrepareTaskArtifactUploadSkipsOnlyMatchingStoredObject(t *testing.T) {
	svc, repo, store, task := newTaskArtifactTestService(t)
	ctx := context.Background()
	executionID := startTaskArtifactExecution(t, repo, task)
	key := buildTaskArtifactStorageKey(task, executionID, "output/article.md")
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
	}{
		{
			name:            "matching size and hash",
			info:            storage.ObjectInfo{Key: key, Size: 7, ContentType: "text/markdown", ETag: "etag-1", SHA256: taskArtifactTestSHA256},
			uploadRequired:  false,
			wantStoredETag:  "etag-1",
			wantCredentials: false,
		},
		{
			name:            "wrong hash",
			info:            storage.ObjectInfo{Key: key, Size: 7, ContentType: "text/markdown", SHA256: strings.Repeat("b", 64)},
			uploadRequired:  true,
			wantCredentials: true,
		},
		{
			name:            "wrong size",
			info:            storage.ObjectInfo{Key: key, Size: 8, ContentType: "text/markdown", SHA256: taskArtifactTestSHA256},
			uploadRequired:  true,
			wantCredentials: true,
		},
		{
			name:            "missing hash metadata",
			info:            storage.ObjectInfo{Key: key, Size: 7, ContentType: "text/markdown"},
			uploadRequired:  true,
			wantCredentials: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store.stats = map[string]*storage.ObjectInfo{key: &tt.info}
			store.uploadKey = ""
			store.uploadContentType = ""
			store.uploadURLCalls = 0
			credentialCalls := 0

			result, err := svc.PrepareTaskArtifactUpload(ctx, task.ID, task.UserID, executionID, taskArtifactDirectUploadConfigWithCredentialCounter(t, &credentialCalls), req)
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
	key := buildTaskArtifactStorageKey(task, executionID, "output/article.md")
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
	key := buildTaskArtifactStorageKey(task, executionID, "output/article.md")
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

	validKey := buildTaskArtifactStorageKey(task, executionID, "output/article.md")
	mismatchedKey := buildTaskArtifactStorageKey(task, executionID, "output/cover.png")
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
		key := buildTaskArtifactStorageKey(task, executionID, relPath)
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
	svc := NewTaskService(fixture.repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
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
	workspaceKey := buildTaskArtifactStorageKey(task, executionID, "output/cover.png")
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
	workspaceKey := buildTaskArtifactStorageKey(task, executionID, workspacePath)
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

func TestUploadExecutionTaskFileWithSettlementPersistsArtifactAndOutboxAtomically(t *testing.T) {
	fixture := newBillingWalletFixture(t, 500, 0, 0)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	store := &fakeTaskArtifactStorage{name: "oss"}
	svc := NewTaskService(fixture.repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
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

	broken := NewTaskService(fixture.repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
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
	objectKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/output/article.md"
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
			ETag:         "etag-1",
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
	objectKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/executions/" + executionID + "/artifacts/output/article.md"
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
	want := "/executions/" + executionID + "/artifacts/output/article.md"
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
