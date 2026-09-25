package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
func (s *streamArtifactStorage) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectNotFound, key)
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	return &storage.ObjectInfo{Key: key, Size: int64(len(data)), ContentType: "text/markdown", ETag: `"` + hash + `"`, SHA256: hash}, nil
}
func (s *streamArtifactStorage) PromoteObject(ctx context.Context, sourceKey, finalKey, expectedETag string) (*storage.ObjectInfo, error) {
	info, err := s.StatObject(ctx, sourceKey)
	if err != nil {
		return nil, err
	}
	if info.ETag != expectedETag {
		return nil, storage.ErrPromotionPreconditionFailed
	}
	if _, exists := s.objects[finalKey]; exists {
		return nil, storage.ErrObjectAlreadyExists
	}
	s.objects[finalKey] = append([]byte(nil), s.objects[sourceKey]...)
	return s.StatObject(ctx, finalKey)
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
	svc, repo, _, task := newTaskArtifactTestService(t)
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
	if !strings.Contains(got.ObjectKey, "/artifacts/staging/sha256/"+req.SHA256+"/") || !strings.HasSuffix(got.ObjectKey, "/"+req.RelativePath) {
		t.Fatalf("object key = %q, want hash-addressed staging namespace", got.ObjectKey)
	}
	stored, err := local.Read(t.Context(), got.ObjectKey)
	if err != nil || string(stored) != "artifact-body" {
		t.Fatalf("stored body = %q, %v", stored, err)
	}
	if err := svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files: []TaskArtifactManifestFile{{
			RelativePath: req.RelativePath, ObjectKey: got.ObjectKey, ContentType: req.ContentType,
			Size: got.Size, SHA256: got.SHA256,
		}},
	}); err != nil {
		t.Fatalf("FinalizeTaskArtifactManifest: %v", err)
	}
	finalKey := buildTaskArtifactFinalStorageKey(task, executionID, req.SHA256, req.RelativePath)
	pending, err := repo.TaskFiles().FindByExecutionID(t.Context(), executionID)
	if err != nil || len(pending) != 1 || pending[0].OSSKey != finalKey {
		t.Fatalf("pending manifest = %#v, %v", pending, err)
	}
	if body, err := local.Read(t.Context(), finalKey); err != nil || string(body) != "artifact-body" {
		t.Fatalf("immutable artifact = %q, %v", body, err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsMalformedAttemptKey(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)
	svc.store = &streamArtifactStorage{}
	executionID := attachStreamArtifactExecution(t, svc, task)
	req := streamArtifactRequest("artifact-body")
	malformed := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + req.SHA256 + "/not-a-uuid/" + req.RelativePath
	err := svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files:  []TaskArtifactManifestFile{{RelativePath: req.RelativePath, ObjectKey: malformed, Size: req.Size, SHA256: req.SHA256}},
	})
	if !errors.Is(err, ErrTaskArtifactInvalid) {
		t.Fatalf("malformed attempt key error = %v", err)
	}
}

func TestFinalizeTaskArtifactManifestRejectsUnregisteredAttemptKey(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)
	svc.store = &streamArtifactStorage{}
	executionID := attachStreamArtifactExecution(t, svc, task)
	req := streamArtifactRequest("artifact-body")
	unregistered := buildTaskArtifactStagingStorageKey(task, executionID, req.SHA256, uuid.NewString(), req.RelativePath)
	err := svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files:  []TaskArtifactManifestFile{{RelativePath: req.RelativePath, ObjectKey: unregistered, Size: req.Size, SHA256: req.SHA256}},
	})
	if !errors.Is(err, ErrTaskArtifactInvalid) {
		t.Fatalf("unregistered attempt key error = %v", err)
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

func TestTaskArtifactMutationsRejectDeletingTask(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	svc.store = &streamArtifactStorage{}
	executionID := attachStreamArtifactExecution(t, svc, task)
	if acquired, err := repo.Tasks().BeginDelete(t.Context(), task.ID); err != nil || !acquired {
		t.Fatalf("BeginDelete = %v, %v", acquired, err)
	}

	if _, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, streamArtifactRequest("artifact-body")); !errors.Is(err, ErrTaskDeleting) {
		t.Fatalf("StreamTaskArtifact error = %v, want ErrTaskDeleting", err)
	}
	if err := svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{TaskID: task.ID}); !errors.Is(err, ErrTaskDeleting) {
		t.Fatalf("FinalizeTaskArtifactManifest error = %v, want ErrTaskDeleting", err)
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
			profile, profiles := bootstrapTestProfile(t)
			tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
			if err != nil {
				t.Fatal(err)
			}
			svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{
				Store: &streamArtifactStorage{name: tc.provider}, TokenTTL: time.Hour, Registry: profiles,
			}, zerolog.Nop())
			snapshot, fingerprint, err := profile.Freeze()
			if err != nil {
				t.Fatal(err)
			}
			task := &model.Task{
				ID: "task-1", UserID: "user-1", ProjectID: "project-1",
				Type: model.PlatformArticle, Prompt: "write", Status: model.TaskStatusRunning,
				ExecutionProfile: profile.ID, AgentProfileSnapshot: snapshot, AgentProfileFingerprint: fingerprint,
			}
			execution := model.NewTaskExecutionAgentProfile(task.AgentProfileSnapshot, task.AgentProfileFingerprint)
			execution.ID = "execution-1"
			response, err := svc.buildResponse(t.Context(), &execution, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if response.ArtifactTransport.Mode != tc.want {
				t.Fatalf("artifact transport mode = %q, want %q", response.ArtifactTransport.Mode, tc.want)
			}
		})
	}
}

type adversarialStreamStorage struct {
	mu           sync.Mutex
	consume      int64
	uploadErr    error
	deleteErr    error
	cancel       context.CancelFunc
	objects      map[string][]byte
	deleted      []string
	deleteCtxErr error
}

func (s *adversarialStreamStorage) Name() string { return "local" }
func (s *adversarialStreamStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	var body bytes.Buffer
	var err error
	if s.consume >= 0 {
		_, err = io.CopyN(&body, reader, s.consume)
	} else {
		_, err = io.Copy(&body, reader)
	}
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), body.Bytes()...)
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.uploadErr != nil {
		return nil, s.uploadErr
	}
	return &storage.UploadResult{Key: key, Size: int64(body.Len()), MimeType: contentType}, nil
}
func (s *adversarialStreamStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errorsUnsupported("UploadFile")
}
func (s *adversarialStreamStorage) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errorsUnsupported("UploadURL")
}
func (s *adversarialStreamStorage) GetURL(key string) string { return key }
func (s *adversarialStreamStorage) Read(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}
func (s *adversarialStreamStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		s.deleteCtxErr = err
		return err
	}
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}
func (s *adversarialStreamStorage) DownloadURL(context.Context, string, int) (string, error) {
	return "", errorsUnsupported("DownloadURL")
}
func (s *adversarialStreamStorage) HasCustomDomain() bool  { return false }
func (s *adversarialStreamStorage) IsOwnedURL(string) bool { return false }

func TestStreamTaskArtifactIndependentlyVerifiesProviderConsumedBody(t *testing.T) {
	for _, tc := range []struct {
		name        string
		consumeDiff int64
		bodySuffix  string
	}{
		{name: "provider stops at declared size before extra byte", bodySuffix: "!"},
		{name: "provider consumes fewer than declared", consumeDiff: -1},
		{name: "provider consumes more than declared", consumeDiff: 1, bodySuffix: "!"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, task := newTaskArtifactTestService(t)
			req := streamArtifactRequest("artifact-body")
			store := &adversarialStreamStorage{consume: req.Size + tc.consumeDiff}
			svc.store = store
			executionID := attachStreamArtifactExecution(t, svc, task)
			req.Body = strings.NewReader("artifact-body" + tc.bodySuffix)

			if _, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, req); err == nil {
				t.Fatal("partial-success provider request succeeded")
			}
			if len(store.objects) != 0 || len(store.deleted) != 1 {
				t.Fatalf("objects=%#v deleted=%#v", store.objects, store.deleted)
			}
		})
	}
}

func TestStreamTaskArtifactCleanupDetachesFromCanceledRequest(t *testing.T) {
	svc, _, _, task := newTaskArtifactTestService(t)
	ctx, cancel := context.WithCancel(t.Context())
	store := &adversarialStreamStorage{consume: -1, cancel: cancel, uploadErr: context.Canceled}
	svc.store = store
	executionID := attachStreamArtifactExecution(t, svc, task)
	req := streamArtifactRequest("artifact-body")

	_, err := svc.StreamTaskArtifact(ctx, task.ID, task.UserID, executionID, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if store.deleteCtxErr != nil || len(store.deleted) != 1 || len(store.objects) != 0 {
		t.Fatalf("cleanup ctx error=%v deleted=%#v objects=%#v", store.deleteCtxErr, store.deleted, store.objects)
	}
}

func TestStreamTaskArtifactSurfacesUploadAndCleanupFailures(t *testing.T) {
	uploadErr := errors.New("upload failed after staging")
	cleanupErr := errors.New("cleanup failed")
	svc, _, _, task := newTaskArtifactTestService(t)
	store := &adversarialStreamStorage{consume: -1, uploadErr: uploadErr, deleteErr: cleanupErr}
	svc.store = store
	executionID := attachStreamArtifactExecution(t, svc, task)

	_, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, streamArtifactRequest("artifact-body"))
	if !errors.Is(err, uploadErr) || !errors.Is(err, cleanupErr) || !errors.Is(err, ErrTaskArtifactUnavailable) {
		t.Fatalf("joined error = %v", err)
	}
	if len(store.deleted) != 0 || len(store.objects) != 1 {
		t.Fatalf("failed upload cleanup state deleted=%#v objects=%#v", store.deleted, store.objects)
	}
	for key := range store.objects {
		session, findErr := svc.repo.UploadSessions().FindByID(t.Context(), streamArtifactSessionID(t, key))
		if findErr != nil || session.Status != model.UploadSessionPending || session.ExpiresAt.After(time.Now().Add(time.Minute)) {
			t.Fatalf("failed upload durable cleanup session = %#v, %v", session, findErr)
		}
	}
}

type coordinatedRetryStorage struct {
	*adversarialStreamStorage
	calls         atomic.Int32
	firstStored   chan struct{}
	secondEntered chan struct{}
	allowSecond   chan struct{}
}

func newCoordinatedRetryStorage() *coordinatedRetryStorage {
	return &coordinatedRetryStorage{
		adversarialStreamStorage: &adversarialStreamStorage{consume: -1},
		firstStored:              make(chan struct{}),
		secondEntered:            make(chan struct{}),
		allowSecond:              make(chan struct{}),
	}
}

func (s *coordinatedRetryStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	call := s.calls.Add(1)
	if call == 2 {
		close(s.secondEntered)
		<-s.allowSecond
	}
	result, err := s.adversarialStreamStorage.Upload(ctx, key, reader, contentType)
	if call == 1 {
		close(s.firstStored)
		<-s.secondEntered
	}
	return result, err
}

func TestStreamTaskArtifactAttemptKeysProtectPriorSuccess(t *testing.T) {
	t.Run("sequential", func(t *testing.T) {
		svc, _, _, task := newTaskArtifactTestService(t)
		store := &adversarialStreamStorage{consume: -1}
		svc.store = store
		executionID := attachStreamArtifactExecution(t, svc, task)
		first, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, streamArtifactRequest("artifact-body"))
		if err != nil {
			t.Fatal(err)
		}
		invalid := streamArtifactRequest("artifact-body")
		invalid.SHA256 = strings.Repeat("0", 64)
		if _, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, invalid); err == nil {
			t.Fatal("invalid retry succeeded")
		}
		if got := string(store.objects[first.ObjectKey]); got != "artifact-body" || len(store.objects) != 1 {
			t.Fatalf("prior object = %q objects=%#v deleted=%#v", got, store.objects, store.deleted)
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		svc, _, _, task := newTaskArtifactTestService(t)
		store := newCoordinatedRetryStorage()
		svc.store = store
		executionID := attachStreamArtifactExecution(t, svc, task)
		validDone := make(chan *TaskArtifactStreamResult, 1)
		errDone := make(chan error, 1)
		go func() {
			result, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, streamArtifactRequest("artifact-body"))
			if err != nil {
				errDone <- err
				return
			}
			validDone <- result
		}()
		<-store.firstStored
		go func() {
			invalid := streamArtifactRequest("artifact-body")
			invalid.SHA256 = strings.Repeat("0", 64)
			_, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, invalid)
			errDone <- err
		}()
		result := <-validDone
		close(store.allowSecond)
		if err := <-errDone; err == nil {
			t.Fatal("invalid concurrent retry succeeded")
		}
		if got := string(store.objects[result.ObjectKey]); got != "artifact-body" || len(store.objects) != 1 {
			t.Fatalf("prior object = %q objects=%#v deleted=%#v", got, store.objects, store.deleted)
		}
	})
}

func TestStreamTaskArtifactResponseLossIsReclaimedAfterExpiry(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = local
	executionID := attachStreamArtifactExecution(t, svc, task)

	result, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, streamArtifactRequest("artifact-body"))
	if err != nil {
		t.Fatal(err)
	}
	sessionID := streamArtifactSessionID(t, result.ObjectKey)
	session, err := repo.UploadSessions().FindByID(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("find durable stream attempt: %v", err)
	}
	if session.ID != sessionID || session.UserID != task.UserID || session.Purpose != DirectUploadPurposeTaskArtifact ||
		session.StagingKey != result.ObjectKey || session.Size != result.Size || session.Status != model.UploadSessionPending {
		t.Fatalf("stream attempt session = %#v", session)
	}

	cleaned, err := CleanupExpiredUploadSessions(t.Context(), local, repo, session.ExpiresAt.Add(time.Second), 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("cleanup response-loss attempt = %d, %v", cleaned, err)
	}
	if _, err := local.Read(t.Context(), result.ObjectKey); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired response-loss object read error = %v", err)
	}
}

func TestFinalizeTaskArtifactManifestReusesImmutableObjectAcrossStreamAttempts(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = local
	executionID := attachStreamArtifactExecution(t, svc, task)

	firstReq := streamArtifactRequest("artifact-body")
	first, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, firstReq)
	if err != nil {
		t.Fatal(err)
	}
	manifest := func(result *TaskArtifactStreamResult) error {
		return svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
			TaskID: task.ID,
			Files: []TaskArtifactManifestFile{{
				RelativePath: firstReq.RelativePath, ObjectKey: result.ObjectKey, ContentType: result.ContentType,
				Size: result.Size, SHA256: result.SHA256,
			}},
		})
	}
	if err := manifest(first); err != nil {
		t.Fatal(err)
	}
	second, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, streamArtifactRequest("artifact-body"))
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest(second); err != nil {
		t.Fatal(err)
	}

	firstSession, err := repo.UploadSessions().FindByID(t.Context(), streamArtifactSessionID(t, first.ObjectKey))
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := repo.UploadSessions().FindByID(t.Context(), streamArtifactSessionID(t, second.ObjectKey))
	if err != nil {
		t.Fatal(err)
	}
	if firstSession.Status != model.UploadSessionPending || secondSession.Status != model.UploadSessionPending {
		t.Fatalf("replacement sessions = first %#v second %#v", firstSession, secondSession)
	}
	for _, key := range []string{first.ObjectKey, second.ObjectKey} {
		if _, err := local.Read(t.Context(), key); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("finalized staging object %q read error = %v", key, err)
		}
	}
	finalKey := buildTaskArtifactFinalStorageKey(task, executionID, firstReq.SHA256, firstReq.RelativePath)
	if body, err := local.Read(t.Context(), finalKey); err != nil || string(body) != "artifact-body" {
		t.Fatalf("immutable object = %q, %v", body, err)
	}
}

func TestTaskArtifactManifestPromotionKeepsImmutableObjectAfterStagingCleanup(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = local
	executionID := attachStreamArtifactExecution(t, svc, task)
	req := streamArtifactRequest("artifact-body")
	result, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files: []TaskArtifactManifestFile{{
			RelativePath: req.RelativePath, ObjectKey: result.ObjectKey, ContentType: result.ContentType,
			Size: result.Size, SHA256: result.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	session, err := repo.UploadSessions().FindByID(t.Context(), streamArtifactSessionID(t, result.ObjectKey))
	if err != nil || session.Status != model.UploadSessionPending {
		t.Fatalf("released staging session = %#v, %v", session, err)
	}
	cleaned, err := CleanupExpiredUploadSessions(t.Context(), local, repo, session.ExpiresAt.Add(taskArtifactCleanupGrace+time.Second), 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("staging session cleanup = %d, %v", cleaned, err)
	}
	finalKey := buildTaskArtifactFinalStorageKey(task, executionID, req.SHA256, req.RelativePath)
	files, err := repo.TaskFiles().FindByExecutionID(t.Context(), executionID)
	if err != nil || len(files) != 1 || files[0].OSSKey != finalKey {
		t.Fatalf("immutable task files = %#v, %v", files, err)
	}
	if body, err := local.Read(t.Context(), finalKey); err != nil || string(body) != "artifact-body" {
		t.Fatalf("immutable object = %q, %v", body, err)
	}
}

func TestDeleteTaskSchedulesAdoptedStreamAttemptsForCleanup(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = local
	executionID := attachStreamArtifactExecution(t, svc, task)
	req := streamArtifactRequest("artifact-body")
	result, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files: []TaskArtifactManifestFile{{
			RelativePath: req.RelativePath, ObjectKey: result.ObjectKey, ContentType: result.ContentType,
			Size: result.Size, SHA256: result.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.TaskExecutions().Transition(t.Context(), executionID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionSucceeded,
		model.ExecutionTransition{
			FinalizationStatus: model.TaskExecutionFinalizationDone,
			CleanupStatus:      model.TaskExecutionCleanupDone,
		}); err != nil || !won {
		t.Fatalf("complete execution cleanup: won=%v err=%v", won, err)
	}
	if err := repo.Tasks().UpdateStatus(t.Context(), task.ID, model.TaskStatusCompleted); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if err := svc.Delete(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	session, err := repo.UploadSessions().FindByID(t.Context(), streamArtifactSessionID(t, result.ObjectKey))
	if err != nil || session.Status != model.UploadSessionPending || session.ExpiresAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("deleted task stream session = %#v, %v", session, err)
	}
	cleaned, err := CleanupExpiredUploadSessions(t.Context(), local, repo, time.Now().Add(time.Minute), 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("cleanup deleted task attempt = %d, %v", cleaned, err)
	}
	if _, err := local.Read(t.Context(), result.ObjectKey); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted task stream object read error = %v", err)
	}
}

type blockingArtifactCleanupStorage struct {
	*storage.LocalProvider
	deleteEntered chan struct{}
	allowDelete   chan struct{}
}

func (s *blockingArtifactCleanupStorage) Delete(ctx context.Context, key string) error {
	close(s.deleteEntered)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.allowDelete:
		return s.LocalProvider.Delete(ctx, key)
	}
}

func TestTaskArtifactManifestCannotAdoptCleanupClaimedAttempt(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &blockingArtifactCleanupStorage{
		LocalProvider: local,
		deleteEntered: make(chan struct{}),
		allowDelete:   make(chan struct{}),
	}
	svc.store = store
	executionID := attachStreamArtifactExecution(t, svc, task)
	req := streamArtifactRequest("artifact-body")
	result, err := svc.StreamTaskArtifact(t.Context(), task.ID, task.UserID, executionID, req)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.UploadSessions().FindByID(t.Context(), streamArtifactSessionID(t, result.ObjectKey))
	if err != nil {
		t.Fatalf("find durable stream attempt: %v", err)
	}

	cleanupDone := make(chan error, 1)
	go func() {
		_, err := CleanupExpiredUploadSessions(context.Background(), store, repo, session.ExpiresAt.Add(time.Second), 10)
		cleanupDone <- err
	}()
	select {
	case <-store.deleteEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not claim the stream attempt")
	}

	err = svc.FinalizeTaskArtifactManifest(t.Context(), task.ID, task.UserID, executionID, TaskArtifactManifestRequest{
		TaskID: task.ID,
		Files: []TaskArtifactManifestFile{{
			RelativePath: req.RelativePath, ObjectKey: result.ObjectKey, ContentType: result.ContentType,
			Size: result.Size, SHA256: result.SHA256,
		}},
	})
	if !errors.Is(err, ErrTaskArtifactInvalid) {
		close(store.allowDelete)
		t.Fatalf("cleanup-claimed manifest error = %v", err)
	}
	files, findErr := repo.TaskFiles().FindByExecutionID(t.Context(), executionID)
	if findErr != nil || len(files) != 0 {
		close(store.allowDelete)
		t.Fatalf("cleanup-claimed manifest files = %#v, %v", files, findErr)
	}
	close(store.allowDelete)
	if err := <-cleanupDone; err != nil {
		t.Fatal(err)
	}
}

func streamArtifactSessionID(t *testing.T, key string) string {
	t.Helper()
	_, remainder, ok := strings.Cut(key, "/staging/sha256/")
	if !ok {
		t.Fatalf("stream artifact key %q has no staging hash prefix", key)
	}
	_, remainder, ok = strings.Cut(remainder, "/")
	if !ok {
		t.Fatalf("stream artifact key %q has no upload id", key)
	}
	uploadID, _, ok := strings.Cut(remainder, "/")
	if !ok {
		t.Fatalf("stream artifact key %q has no relative path", key)
	}
	return uploadID
}
