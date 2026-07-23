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

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

type streamArtifactStorage struct {
	name    string
	objects map[string][]byte
	deleted []string
}

func (s *streamArtifactStorage) Name() string {
	if s.name != "" {
		return s.name
	}
	return "local"
}
func (s *streamArtifactStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = data
	return &storage.UploadResult{Key: key, URL: s.GetURL(key), Size: int64(len(data)), MimeType: contentType}, nil
}
func (s *streamArtifactStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errorsUnsupported("UploadFile")
}
func (s *streamArtifactStorage) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errorsUnsupported("UploadURL")
}
func (s *streamArtifactStorage) GetURL(key string) string { return "/api/v1/files/" + key }
func (s *streamArtifactStorage) Read(_ context.Context, key string) ([]byte, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}
func (s *streamArtifactStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}
func (s *streamArtifactStorage) DownloadURL(context.Context, string, int) (string, error) {
	return "", errorsUnsupported("DownloadURL")
}
func (s *streamArtifactStorage) HasCustomDomain() bool  { return false }
func (s *streamArtifactStorage) IsOwnedURL(string) bool { return false }

type unsupportedStorageOperation string

func (e unsupportedStorageOperation) Error() string { return string(e) + " unsupported" }
func errorsUnsupported(operation string) error      { return unsupportedStorageOperation(operation) }

func attachStreamArtifactExecution(t *testing.T, svc *TaskService, task *model.Task) string {
	t.Helper()
	executionID := uuid.NewString()
	task.CurrentExecutionID = &executionID
	if err := svc.repo.Tasks().Update(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := svc.repo.TaskExecutions().Create(t.Context(), &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Target: "docker",
		Status: model.TaskExecutionRunning, Started: true, StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	return executionID
}

func streamArtifactRequest(body string) TaskArtifactStreamRequest {
	sum := sha256.Sum256([]byte(body))
	return TaskArtifactStreamRequest{
		RelativePath: "output/article.md",
		ContentType:  "text/markdown",
		Size:         int64(len(body)),
		SHA256:       hex.EncodeToString(sum[:]),
		Body:         strings.NewReader(body),
	}
}

func TestStreamTaskArtifactUploadsWithoutOSS(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = local
	executionID := attachStreamArtifactExecution(t, svc, task)
	req := streamArtifactRequest("artifact-body")

	got, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, req)
	if err != nil {
		t.Fatalf("StreamTaskArtifact: %v", err)
	}
	if got.ObjectKey == "" || got.Size != int64(len("artifact-body")) || got.SHA256 != req.SHA256 {
		t.Fatalf("result = %#v", got)
	}
	stored, err := local.Read(t.Context(), got.ObjectKey)
	if err != nil || string(stored) != "artifact-body" {
		t.Fatalf("stored body = %q, %v", stored, err)
	}
}

func TestStreamTaskArtifactRejectsInvalidBodiesAndCleansStoredObject(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TaskArtifactStreamRequest)
		staged bool
	}{
		{name: "path traversal", mutate: func(req *TaskArtifactStreamRequest) { req.RelativePath = "../secret" }},
		{name: "excess declared size", mutate: func(req *TaskArtifactStreamRequest) { req.Size = maxTaskArtifactUploadBytes + 1 }},
		{name: "short body", staged: true, mutate: func(req *TaskArtifactStreamRequest) { req.Size++ }},
		{name: "long body", staged: true, mutate: func(req *TaskArtifactStreamRequest) { req.Size-- }},
		{name: "digest mismatch", staged: true, mutate: func(req *TaskArtifactStreamRequest) { req.SHA256 = strings.Repeat("0", 64) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, task := newTaskArtifactTestService(t)
			store := &streamArtifactStorage{}
			svc.store = store
			executionID := attachStreamArtifactExecution(t, svc, task)
			req := streamArtifactRequest("artifact-body")
			tc.mutate(&req)

			if _, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, req); err == nil {
				t.Fatal("StreamTaskArtifact succeeded")
			}
			if len(store.objects) != 0 {
				t.Fatalf("failed upload left objects: %#v", store.objects)
			}
			if tc.staged && len(store.deleted) != 1 {
				t.Fatalf("deleted = %#v, want one staged object cleanup", store.deleted)
			}
			if !tc.staged && len(store.deleted) != 0 {
				t.Fatalf("pre-upload rejection deleted = %#v", store.deleted)
			}
		})
	}
}

func TestStreamTaskArtifactRequiresCurrentExecutionAuthorization(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)
	store := &streamArtifactStorage{}
	svc.store = store
	executionID := attachStreamArtifactExecution(t, svc, task)

	for _, tc := range []struct {
		name, taskID, userID, executionID string
	}{
		{name: "wrong task", taskID: uuid.NewString(), userID: task.UserID, executionID: executionID},
		{name: "wrong user", taskID: task.ID, userID: uuid.NewString(), executionID: executionID},
		{name: "missing execution", taskID: task.ID, userID: task.UserID},
		{name: "stale execution", taskID: task.ID, userID: task.UserID, executionID: uuid.NewString()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := streamArtifactRequest("artifact-body")
			if _, err := svc.StreamTaskArtifact(t.Context(), tc.taskID, tc.userID, tc.executionID, req); err == nil {
				t.Fatal("unauthorized stream succeeded")
			}
		})
	}
	if len(store.objects) != 0 {
		t.Fatalf("unauthorized streams wrote objects: %#v", store.objects)
	}
}

func TestAgentBootstrapArtifactTransportFollowsStorageProvider(t *testing.T) {
	for _, tc := range []struct {
		provider, want string
	}{
		{provider: "oss", want: "direct"},
		{provider: "local", want: "stream"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			repo := openBootstrapTestRepository(t)
			tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
			if err != nil {
				t.Fatal(err)
			}
			svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{
				Model: "claude-test", Store: &streamArtifactStorage{name: tc.provider},
				TokenTTL: time.Hour, RuntimeEnv: bootstrapTestRuntimeEnv(),
				ModelUsageAliases: bootstrapTestModelUsageAliases(),
			}, zerolog.Nop())
			task := &model.Task{
				ID: "task-1", UserID: "user-1", ProjectID: "project-1",
				Type: model.PlatformArticle, Prompt: "write", Status: model.TaskStatusRunning,
			}
			response, err := svc.buildResponse(t.Context(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if response.ArtifactTransport.Mode != tc.want {
				t.Fatalf("artifact transport mode = %q, want %q", response.ArtifactTransport.Mode, tc.want)
			}
		})
	}
}
