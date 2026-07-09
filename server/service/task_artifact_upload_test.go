package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

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
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
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

	result, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
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

func TestPrepareTaskArtifactUploadRejectsUnsafeRelativePath(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)

	_, err := svc.PrepareTaskArtifactUpload(context.Background(), task.ID, task.UserID, taskArtifactDirectUploadConfig(t), TaskArtifactPrepareRequest{
		RelativePath: "../secret.md",
		Filename:     "secret.md",
		ContentType:  "text/markdown",
		Size:         12,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid relative path") {
		t.Fatalf("PrepareTaskArtifactUpload error = %v, want invalid relative path", err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsObjectOutsideTaskPrefix(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)

	err := svc.FinalizeTaskArtifactManifest(context.Background(), task.ID, task.UserID, TaskArtifactManifestRequest{
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

	if err := svc.FinalizeTaskArtifactManifest(context.Background(), task.ID, task.UserID, TaskArtifactManifestRequest{
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
