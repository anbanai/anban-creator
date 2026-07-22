package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

type fakeTaskArtifactStorage struct {
	name              string
	uploadKey         string
	uploadContentType string
	stats             map[string]*storage.ObjectInfo
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
	if f.stats == nil || f.stats[key] == nil {
		return nil, os.ErrNotExist
	}
	cp := *f.stats[key]
	return &cp, nil
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
	svc := NewTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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

func TestPrepareTaskArtifactUploadScopesKeyToUserProjectTask(t *testing.T) {
	svc, _, store, task := newTaskArtifactTestService(t)

	result, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, "", taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		RelativePath: "output/article.md",
		Filename:     "article.md",
		ContentType:  "text/markdown",
		Size:         12345,
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
		store.stats[key] = &storage.ObjectInfo{Key: key, Size: 3, ContentType: DetectTaskFileMIME(relPath)}
		manifest.Files = append(manifest.Files, TaskArtifactManifestFile{
			RelativePath: relPath, ObjectKey: key, ContentType: DetectTaskFileMIME(relPath), Size: 3, SHA256: strings.Repeat("a", 64),
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

func TestUploadExecutionTaskFileWithSettlementPersistsArtifactAndOutboxAtomically(t *testing.T) {
	fixture := newBillingWalletFixture(t, 500, 0, 0)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	store := &fakeTaskArtifactStorage{name: "oss"}
	svc := NewTaskService(fixture.repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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

	broken := NewTaskService(fixture.repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
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
	store.stats = map[string]*storage.ObjectInfo{objectKey: {Key: objectKey, Size: 7, ContentType: "text/markdown"}}
	req := TaskArtifactManifestRequest{TaskID: task.ID, ExecutionID: executionID, Files: []TaskArtifactManifestFile{{RelativePath: "output/article.md", ObjectKey: objectKey, Size: 7, SHA256: strings.Repeat("a", 64)}}}
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
	request := TaskArtifactPrepareRequest{TaskID: task.ID, ExecutionID: executionID, RelativePath: "output/article.md", Size: 7}
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
