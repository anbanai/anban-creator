package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"gorm.io/datatypes"
)

type readTrackingLocalStorage struct {
	*storage.LocalProvider
	readCalls int
}

type attachmentURLTaskStorage struct {
	*fakeTaskStorage
}

type faultingBulkZipStorage struct {
	*storage.LocalProvider
	faultKey string
}

type faultingBulkZipStream struct {
	data []byte
	err  error
}

func (s *faultingBulkZipStorage) OpenObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == s.faultKey {
		return &faultingBulkZipStream{data: []byte("partial"), err: errors.New("object stream interrupted")}, nil
	}
	return s.LocalProvider.OpenObject(ctx, key)
}

func (s *faultingBulkZipStream) Read(p []byte) (int, error) {
	if len(s.data) > 0 {
		n := copy(p, s.data)
		s.data = s.data[n:]
		return n, nil
	}
	return 0, s.err
}

func (*faultingBulkZipStream) Close() error { return nil }

func (s *attachmentURLTaskStorage) DownloadAttachmentURL(_ context.Context, key, filename string, _ int) (string, error) {
	return "https://download.example.com/" + key + "?filename=" + filename, nil
}

func (s *readTrackingLocalStorage) Read(ctx context.Context, key string) ([]byte, error) {
	s.readCalls++
	return s.LocalProvider.Read(ctx, key)
}

func TestTaskService_DownloadZipWithoutFilesReturnsDeliveryError(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	taskID := "delivery-empty-task"
	executionID := "delivery-empty-execution"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("SetCurrentExecution = %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}

	_, _, err := svc.DownloadZip(ctx, taskID)
	if !errors.Is(err, ErrNoDownloadableDeliveryFiles) {
		t.Fatalf("DownloadZip error = %v, want ErrNoDownloadableDeliveryFiles", err)
	}
}

func TestTaskService_DownloadZipWithUnreadableDeliveryReturnsDeliveryError(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = store
	ctx := context.Background()
	taskID := "unreadable-delivery-task"
	executionID := "unreadable-delivery-execution"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("SetCurrentExecution = %v, %v", ok, err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: "missing-content", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered,
		FilePath: "output/content.md", FileName: "content.md", OSSKey: "missing/content.md", StorageProvider: store.Name(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.DownloadZip(ctx, taskID); err == nil ||
		!errors.Is(err, storage.ErrObjectNotFound) || !strings.Contains(err.Error(), "content.md") {
		t.Fatalf("DownloadZip = %v, want content.md storage object not found error", err)
	}
}

func TestTaskService_DeliveryAndRetainedZipContainOnlyTheirOwnState(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{
		"tasks/task-state-zip/delivered.md": []byte("delivered"),
		"tasks/task-state-zip/retained.txt": []byte("retained"),
	}}
	svc.store = store
	ctx := context.Background()
	taskID, executionID := "task-state-zip", "execution-state-zip"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "article", AgentPackVersion: "1.0.4", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"article","path":"output/delivered.md","mime_type":"text/markdown"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("set current execution: %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: "delivered", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered, Role: model.FileRoleMarkdown, FilePath: "output/delivered.md", FileName: "delivered.md", MimeType: "text/markdown", OSSKey: "tasks/task-state-zip/delivered.md", StorageProvider: store.Name()},
		{ID: "retained", TaskID: taskID, ExecutionID: "failed-execution", State: model.TaskFileStateRetained, Role: model.FileRoleOther, FilePath: "output/retained.txt", FileName: "retained.txt", MimeType: "text/plain", OSSKey: "tasks/task-state-zip/retained.txt", StorageProvider: store.Name()},
	}); err != nil {
		t.Fatal(err)
	}

	assertZipEntries := func(stream io.ReadCloser, want string, forbidden string) {
		t.Helper()
		defer stream.Close()
		body, err := io.ReadAll(stream)
		if err != nil {
			t.Fatal(err)
		}
		archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(archive.File))
		for _, file := range archive.File {
			names = append(names, file.Name)
		}
		if !slices.Contains(names, want) || slices.Contains(names, forbidden) {
			t.Fatalf("zip entries = %v, want %q without %q", names, want, forbidden)
		}
	}

	delivered, _, err := svc.DownloadZip(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	assertZipEntries(delivered, "delivered.md", "retained.txt")
	retained, _, err := svc.DownloadRetainedZip(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	assertZipEntries(retained, "retained.txt", "delivered.md")
}

func TestTaskService_RetainedZipFailsWhenAnyArtifactIsUnavailable(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{
		"tasks/task-retained-incomplete/available.txt": []byte("available"),
	}}
	svc.store = store
	ctx := context.Background()
	taskID := "task-retained-incomplete"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: "available", TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateRetained, Role: model.FileRoleOther, FilePath: "output/available.txt", FileName: "available.txt", MimeType: "text/plain", OSSKey: "tasks/task-retained-incomplete/available.txt", StorageProvider: store.Name()},
		{ID: "missing", TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateRetained, Role: model.FileRoleOther, FilePath: "output/missing.txt", FileName: "missing.txt", MimeType: "text/plain", OSSKey: "tasks/task-retained-incomplete/missing.txt", StorageProvider: store.Name()},
	}); err != nil {
		t.Fatal(err)
	}

	if stream, _, err := svc.DownloadRetainedZip(ctx, taskID); err == nil {
		stream.Close()
		t.Fatal("DownloadRetainedZip error = nil, want unavailable artifact failure")
	}
}

func TestTaskService_DownloadTasksZipSkipsInterruptedFileAndKeepsReadableDelivery(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	localStore, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const badKey = "tasks/bulk-partial/output/content.md"
	store := &faultingBulkZipStorage{LocalProvider: localStore, faultKey: badKey}
	svc.store = store
	ctx := context.Background()
	userID := "bulk-partial-user"
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	taskID := "bulk-partial-task"
	executionID := "bulk-partial-execution"
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusRunning, Title: "Partial export",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[
			{"role":"content","path":"output/content.md","mime_type":"text/markdown"},
			{"role":"image","path":"output/cover.png","mime_type":"image/png"}
		]`),
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("SetCurrentExecution = %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	goodUpload, err := store.Upload(ctx, "tasks/bulk-partial/output/cover.png", strings.NewReader("good"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{
			ID: "bad-content", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered,
			FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown", FileSize: 32, OSSKey: badKey,
		},
		{
			ID: "good-cover", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered,
			FilePath: "output/cover.png", FileName: "cover.png", MimeType: "image/png", FileSize: goodUpload.Size, OSSKey: goodUpload.Key,
		},
	}); err != nil {
		t.Fatal(err)
	}

	buf, _, err := svc.DownloadTasksZip(ctx, userID, []string{taskID})
	if err != nil {
		t.Fatalf("DownloadTasksZip: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var manifest BulkDownloadZipManifest
	var hasGood, hasBad bool
	for _, file := range reader.File {
		switch {
		case file.Name == "manifest.json":
			rc, openErr := file.Open()
			if openErr != nil {
				t.Fatal(openErr)
			}
			decodeErr := json.NewDecoder(rc).Decode(&manifest)
			_ = rc.Close()
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
		case strings.HasSuffix(file.Name, "/output/cover.png"):
			hasGood = true
		case strings.HasSuffix(file.Name, "/output/content.md"):
			hasBad = true
		}
	}
	if !hasGood || hasBad {
		t.Fatalf("bulk ZIP readable=%v interrupted=%v; want only readable delivery", hasGood, hasBad)
	}
	if len(manifest.Tasks) != 1 || !manifest.Tasks[0].Included || manifest.Tasks[0].Reason != "file_read_failed" {
		t.Fatalf("bulk ZIP manifest = %+v", manifest.Tasks)
	}
}

func TestTaskFileDeliveryMetadataUsesFrozenContractAndState(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.store = &attachmentURLTaskStorage{fakeTaskStorage: &fakeTaskStorage{ownedPrefix: "https://download.example.com/"}}
	ctx := context.Background()
	taskID := "delivery-task"
	executionID := "delivery-execution"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.0", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[ {"role":"content","path":"output/content.md","mime_type":"text/markdown"} ]`),
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("SetCurrentExecution = %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	files := []*model.TaskFile{
		{ID: "content", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered, FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown", OSSKey: "tasks/delivery-task/output/content.md"},
		{ID: "review", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered, FilePath: "output/review.md", FileName: "review.md", URL: "https://public.example/review.md", MediaID: "process-media", WechatURL: "https://mmbiz.example/process-media"},
		{ID: "failure", TaskID: taskID, ExecutionID: "failed-execution", State: model.TaskFileStateRetained, FilePath: "output/content.md", FileName: "content.md", URL: "https://public.example/content.md"},
	}
	if err := repo.TaskFiles().BatchCreate(ctx, files); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetVisibleFiles(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].IsDeliverable || got[0].DeliveryRole != "content" || got[0].DownloadURL == "" || got[0].PreviewURL == "" {
		t.Fatalf("content metadata = %+v", got[0])
	}
	if got[0].DownloadURL != "https://download.example.com/tasks/delivery-task/output/content.md?filename=content.md" {
		t.Fatalf("content download URL = %q, want signed attachment URL", got[0].DownloadURL)
	}
	if got[1].IsDeliverable || got[1].DownloadURL != "" || got[1].PreviewURL == "" || got[1].URL != "" || got[1].MediaID != "" || got[1].WechatURL != "" {
		t.Fatalf("review metadata = %+v", got[1])
	}
	if got[2].IsDeliverable || got[2].DownloadURL == "" || got[2].URL != "" {
		t.Fatalf("collected metadata = %+v", got[2])
	}
}

func TestDeliveryMetadataUsesFrozenPathWhenMIMETypeDiffers(t *testing.T) {
	contract := []agentpack.DeliverySpec{{
		Role: "content", Path: "output/content.md", MIMEType: "text/markdown",
	}}
	file := &model.TaskFile{
		State: model.TaskFileStateDelivered, FilePath: "output/content.md", MimeType: "text/html",
	}

	if role, deliverable := deliveryMetadataFromContract(contract, file); !deliverable || role != "content" {
		t.Fatalf("delivery metadata = %q, %v; want frozen path match", role, deliverable)
	}
}

func TestValidateExecutionDeliveryRequiresEveryFrozenArtifact(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
	svc.store = store
	ctx := context.Background()
	taskID := "required-artifacts-task"
	executionID := "required-artifacts-execution"
	task := &model.Task{
		ID: taskID, UserID: "required-artifacts-user", ProjectID: "required-artifacts-project",
		Type: model.PlatformSeednote, Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	content := []byte("content")
	contentKey := buildTaskMCPArtifactStoragePrefix(task, executionID) + "output/content.md"
	store.files[contentKey] = content
	execution := &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: "frozen-digest",
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[
			{"role":"content","path":"output/content.md","mime_type":"text/markdown","required":true},
			{"role":"image_plan","path":"output/image-plan.md","mime_type":"text/markdown","required":true}
		]`),
	}
	files := []*model.TaskFile{{
		ID: "content", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStatePending,
		FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown",
		FileSize: int64(len(content)), OSSKey: contentKey, StorageProvider: store.Name(),
	}}

	err := svc.validateExecutionDelivery(ctx, taskID, execution, files)
	if !errors.Is(err, ErrTaskDeliveryObjectInvalid) || !strings.Contains(err.Error(), "output/image-plan.md") {
		t.Fatalf("validateExecutionDelivery = %v, want missing frozen image-plan", err)
	}
}

func TestValidateExecutionDeliveryRejectsObjectFromAnotherTaskNamespace(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
	svc.store = store
	ctx := context.Background()
	task := &model.Task{
		ID: "delivery-owner-task", UserID: "delivery-owner-user", ProjectID: "delivery-owner-project",
		Type: model.PlatformSeednote, Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{
		ID: "delivery-owner-execution", TaskID: task.ID, Attempt: 1,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: "frozen-digest",
		AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown","required":true}]`),
	}
	body := []byte("valid markdown")
	foreignKey := "uploads/users/other-user/projects/other-project/tasks/other-task/executions/other-execution/artifacts/mcp/output/content.md"
	store.files[foreignKey] = body
	files := []*model.TaskFile{{
		ID: "foreign-content", TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
		FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown",
		FileSize: int64(len(body)), OSSKey: foreignKey, StorageProvider: store.Name(),
	}}

	err := svc.validateExecutionDelivery(ctx, task.ID, execution, files)
	if !errors.Is(err, ErrTaskDeliveryObjectInvalid) || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("validateExecutionDelivery = %v, want foreign namespace rejection", err)
	}
}

func TestValidateExecutionDeliveryRejectsUnsafeHTMLTags(t *testing.T) {
	for _, tag := range []string{"script", "iframe", "form", "input", "style", "link"} {
		t.Run(tag, func(t *testing.T) {
			svc, repo := setupTaskServiceWithEnqueuer(t)
			store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
			svc.store = store
			ctx := context.Background()
			task := &model.Task{
				ID: "unsafe-html-task-" + tag, UserID: "unsafe-html-user", ProjectID: "unsafe-html-project",
				Type: model.PlatformArticle, Status: model.TaskStatusRunning,
			}
			if err := repo.Tasks().Create(ctx, task); err != nil {
				t.Fatal(err)
			}
			execution := &model.TaskExecution{
				ID: "unsafe-html-execution-" + tag, TaskID: task.ID, Attempt: 1,
				AgentPackID: "article", AgentPackVersion: "1.0.4", AgentPackDigest: "frozen-digest",
				AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"html","path":"output/05-article.html","mime_type":"text/html"}]`),
				AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"html","path":"output/05-article.html","mime_type":"text/html","required":true}]`),
			}
			body := []byte("<section><" + tag + ">unsafe</" + tag + "></section>")
			key := buildTaskMCPArtifactStoragePrefix(task, execution.ID) + "output/05-article.html"
			store.files[key] = body
			files := []*model.TaskFile{{
				ID: "unsafe-html-" + tag, TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
				FilePath: "output/05-article.html", FileName: "05-article.html", MimeType: "text/html",
				FileSize: int64(len(body)), OSSKey: key, StorageProvider: store.Name(),
			}}

			err := svc.validateExecutionDelivery(ctx, task.ID, execution, files)
			if !errors.Is(err, ErrTaskDeliveryObjectInvalid) || !strings.Contains(err.Error(), "unsafe HTML tag") {
				t.Fatalf("validateExecutionDelivery = %v, want unsafe HTML tag rejection", err)
			}
		})
	}
}

func TestValidateExecutionDeliveryRejectsExecutableHTMLAttributes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "event handler", body: `<img src="https://example.com/image.png" onerror="alert(1)">`},
		{name: "javascript URL", body: `<a href=" javascript:alert(1)">open</a>`},
		{name: "CSS expression", body: `<p style="width: expression(alert(1))">unsafe</p>`},
		{name: "CSS escaped URL", body: `<p style="background-image:\75rl(https://example.com/tracker.png)">unsafe</p>`},
		{name: "CSS comment URL", body: `<p style="background-image:u/**/rl(https://example.com/tracker.png)">unsafe</p>`},
		{name: "CSS image-set URL", body: `<p style="background-image:image-set(&quot;https://example.com/tracker.png&quot; 1x)">unsafe</p>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := setupTaskServiceWithEnqueuer(t)
			store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
			svc.store = store
			ctx := context.Background()
			task := &model.Task{
				ID:     "unsafe-attribute-task-" + strings.ReplaceAll(tt.name, " ", "-"),
				UserID: "unsafe-attribute-user", ProjectID: "unsafe-attribute-project",
				Type: model.PlatformArticle, Status: model.TaskStatusRunning,
			}
			if err := repo.Tasks().Create(ctx, task); err != nil {
				t.Fatal(err)
			}
			execution := &model.TaskExecution{
				ID: "unsafe-attribute-execution-" + strings.ReplaceAll(tt.name, " ", "-"), TaskID: task.ID, Attempt: 1,
				AgentPackID: "article", AgentPackVersion: "1.0.4", AgentPackDigest: "frozen-digest",
				AgentPackDeliveryContract:         datatypes.JSON(`[{"role":"html","path":"output/05-article.html","mime_type":"text/html"}]`),
				AgentPackRequiredArtifactContract: datatypes.JSON(`[{"role":"html","path":"output/05-article.html","mime_type":"text/html","required":true}]`),
			}
			body := []byte(tt.body)
			key := buildTaskMCPArtifactStoragePrefix(task, execution.ID) + "output/05-article.html"
			store.files[key] = body
			files := []*model.TaskFile{{
				ID:     "unsafe-attribute-file-" + strings.ReplaceAll(tt.name, " ", "-"),
				TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
				FilePath: "output/05-article.html", FileName: "05-article.html", MimeType: "text/html",
				FileSize: int64(len(body)), OSSKey: key, StorageProvider: store.Name(),
			}}

			err := svc.validateExecutionDelivery(ctx, task.ID, execution, files)
			if !errors.Is(err, ErrTaskDeliveryObjectInvalid) || !strings.Contains(err.Error(), "unsafe HTML attribute") {
				t.Fatalf("validateExecutionDelivery = %v, want executable HTML attribute rejection", err)
			}
		})
	}
}

func TestValidateExecutionDeliveryRejectsInvalidArticleCoreContent(t *testing.T) {
	tests := []struct {
		name     string
		markdown []byte
		html     []byte
	}{
		{name: "blank markdown", markdown: []byte(" \n\t"), html: []byte("<section><p>valid</p></section>")},
		{name: "invalid UTF-8 markdown", markdown: []byte{0xff, 0xfe}, html: []byte("<section><p>valid</p></section>")},
		{name: "plain text HTML", markdown: []byte("valid article"), html: []byte("valid article without markup")},
		{name: "invalid UTF-8 HTML", markdown: []byte("valid article"), html: []byte{'<', 'p', '>', 0xff, '<', '/', 'p', '>'}},
		{name: "unclosed HTML", markdown: []byte("valid article"), html: []byte("<section><p>valid article</section>")},
		{name: "misnested HTML", markdown: []byte("valid article"), html: []byte("<section><strong>valid article</section></strong>")},
		{name: "SVG namespace", markdown: []byte("valid article"), html: []byte(`<section><svg><a href="https://example.com">valid article</a></svg></section>`)},
		{name: "MathML namespace", markdown: []byte("valid article"), html: []byte(`<section><math><mtext>valid article</mtext></math></section>`)},
		{name: "object data URL", markdown: []byte("valid article"), html: []byte(`<object data="data:text/html,<script>alert(1)</script>"></object>`)},
		{name: "meta refresh", markdown: []byte("valid article"), html: []byte(`<meta http-equiv="refresh" content="0;url=javascript:alert(1)">`)},
		{name: "executable srcset", markdown: []byte("valid article"), html: []byte(`<img src="https://example.com/a.png" srcset="javascript:alert(1) 1x">`)},
		{name: "CSS URL", markdown: []byte("valid article"), html: []byte(`<p style="background-image:url(javascript:alert(1))">unsafe</p>`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateArticleCoreFixture(t, tt.markdown, tt.html)
			if !errors.Is(err, ErrTaskDeliveryObjectInvalid) {
				t.Fatalf("validateExecutionDelivery = %v, want ErrTaskDeliveryObjectInvalid", err)
			}
		})
	}
}

func validateArticleCoreFixture(t *testing.T, markdown, htmlBody []byte) error {
	t.Helper()
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store := &fakeTaskStorage{name: "oss", files: map[string][]byte{}}
	svc.store = store
	ctx := context.Background()
	task := &model.Task{
		ID:     "article-core-task-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")),
		UserID: "article-core-user", ProjectID: "article-core-project",
		Type: model.PlatformArticle, Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{
		ID:     "article-core-execution-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")),
		TaskID: task.ID, Attempt: 1,
		AgentPackID: "article", AgentPackVersion: "1.0.4", AgentPackDigest: "frozen-digest",
		AgentPackDeliveryContract: datatypes.JSON(`[
			{"role":"content","path":"output/04-article-final.md","mime_type":"text/markdown"},
			{"role":"html","path":"output/05-article.html","mime_type":"text/html"}
		]`),
		AgentPackRequiredArtifactContract: datatypes.JSON(`[
			{"role":"content","path":"output/04-article-final.md","mime_type":"text/markdown","required":true},
			{"role":"html","path":"output/05-article.html","mime_type":"text/html","required":true}
		]`),
	}
	type artifact struct {
		path string
		mime string
		body []byte
	}
	artifacts := []artifact{
		{path: "output/04-article-final.md", mime: "text/markdown", body: markdown},
		{path: "output/05-article.html", mime: "text/html", body: htmlBody},
	}
	files := make([]*model.TaskFile, 0, len(artifacts))
	for index, artifact := range artifacts {
		key := buildTaskMCPArtifactStoragePrefix(task, execution.ID) + artifact.path
		store.files[key] = artifact.body
		files = append(files, &model.TaskFile{
			ID: fmt.Sprintf("article-core-file-%d", index), TaskID: task.ID, ExecutionID: execution.ID,
			State: model.TaskFileStatePending, Role: model.FileRoleOther,
			FilePath: artifact.path, FileName: filepath.Base(artifact.path), MimeType: artifact.mime,
			FileSize: int64(len(artifact.body)), OSSKey: key, StorageProvider: store.Name(),
		})
	}
	return svc.validateExecutionDelivery(ctx, task.ID, execution, files)
}

func validTaskDeliveryFixtureBody(path, mimeType string) []byte {
	switch mimeType {
	case "text/markdown":
		return []byte("# Valid article\n\nThis is a durable delivery fixture.\n")
	case "text/html":
		return []byte("<section><p>Valid article delivery.</p></section>\n")
	case "application/json":
		if path == "output/draft.json" {
			html := validTaskDeliveryFixtureBody("output/05-article.html", "text/html")
			digest := sha256.Sum256(html)
			body, _ := json.Marshal(map[string]any{
				"schema_version": "1.0",
				"article": map[string]any{
					"title":          "Valid article",
					"digest":         "Durable delivery fixture",
					"content_path":   "output/05-article.html",
					"content_sha256": fmt.Sprintf("%x", digest),
				},
				"readiness": map[string]any{
					"status":         "ready",
					"code":           "",
					"evidence_paths": []string{"output/marketing-scan.json", "output/final-review.md", "output/viral-audit.md"},
				},
			})
			return body
		}
		if path == "output/marketing-scan.json" {
			markdown := validTaskDeliveryFixtureBody("output/04-article-final.md", "text/markdown")
			digest := sha256.Sum256(markdown)
			return []byte(fmt.Sprintf(`{"version":"1.0","status":"passed","content_hash":"%x","findings":[]}`, digest))
		}
		return []byte(`{"status":"passed"}`)
	default:
		return []byte(path)
	}
}

func TestTaskFileDeliveryMetadataFailsClosedWithoutFrozenContract(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	taskID := "legacy-delivery-task"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	file := &model.TaskFile{
		ID: "legacy-content", TaskID: taskID, State: model.TaskFileStateDelivered,
		FilePath: "output/content.md", FileName: "content.md", URL: "https://public.example/content.md",
	}
	if err := repo.TaskFiles().Create(ctx, file); err != nil {
		t.Fatal(err)
	}

	files, err := svc.GetVisibleFiles(ctx, taskID)
	if err != nil {
		t.Fatalf("GetVisibleFiles: %v", err)
	}
	if len(files) != 1 || files[0].IsDeliverable || files[0].URL != "" || files[0].DownloadURL != "" || files[0].PreviewURL == "" {
		t.Fatalf("legacy metadata = %+v, want preview-only process file", files)
	}
	if err := svc.RequireDownloadableTaskFile(ctx, taskID, files[0]); !errors.Is(err, ErrTaskFileDownloadNotAllowed) {
		t.Fatalf("RequireDownloadableTaskFile = %v, want ErrTaskFileDownloadNotAllowed", err)
	}
	if _, _, err := svc.DownloadZip(ctx, taskID); !errors.Is(err, ErrNoDownloadableDeliveryFiles) {
		t.Fatalf("DownloadZip = %v, want ErrNoDownloadableDeliveryFiles", err)
	}
}

func TestTaskFileDeliveryMetadataUsesEachPublishedFilesOwningExecutionContract(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	taskID := "resumed-delivery-task"
	firstExecutionID := "successful-execution"
	currentExecutionID := "failed-resume-execution"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	for _, execution := range []*model.TaskExecution{
		{
			ID: firstExecutionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
			AgentPackID: "seednote", AgentPackVersion: "1.0.0", AgentPackDigest: "first-digest",
			AgentPackDeliveryContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
		},
		{
			ID: currentExecutionID, TaskID: taskID, Attempt: 2, ParentExecutionID: firstExecutionID, Status: model.TaskExecutionFailed,
			AgentPackID: "seednote", AgentPackVersion: "2.0.0", AgentPackDigest: "current-digest",
			AgentPackDeliveryContract: datatypes.JSON(`[{"role":"review","path":"output/review.md","mime_type":"text/markdown"}]`),
		},
	} {
		if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, currentExecutionID); err != nil || !ok {
		t.Fatalf("SetCurrentExecution = %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, model.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: "content", TaskID: taskID, ExecutionID: firstExecutionID, State: model.TaskFileStateDelivered, FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown"},
		{ID: "review", TaskID: taskID, ExecutionID: firstExecutionID, State: model.TaskFileStateDelivered, FilePath: "output/review.md", FileName: "review.md", MimeType: "text/markdown"},
	}); err != nil {
		t.Fatal(err)
	}

	files, err := svc.GetVisibleFiles(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !files[0].IsDeliverable || files[0].DeliveryRole != "content" || files[1].IsDeliverable {
		t.Fatalf("delivery metadata = %#v, want files classified by execution %s", files, firstExecutionID)
	}
	deliverable, err := svc.FilterDeliverableFiles(ctx, taskID, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliverable) != 1 || deliverable[0].ID != "content" {
		t.Fatalf("deliverable files = %#v, want only content from the owning execution contract", deliverable)
	}
}

func TestTaskFileDirectPreviewURLOnlyUsesRemoteMedia(t *testing.T) {
	store := &fakeTaskStorage{ownedPrefix: "https://cdn.example/"}
	tests := []struct {
		name string
		file *model.TaskFile
		want string
	}{
		{name: "video signed URL", file: &model.TaskFile{MimeType: "video/mp4", URL: "https://cdn.example/final.mp4?token=abc"}, want: "https://cdn.example/final.mp4?token=abc"},
		{name: "audio signed URL", file: &model.TaskFile{MimeType: "audio/mpeg", URL: "https://cdn.example/final.mp3"}, want: "https://cdn.example/final.mp3"},
		{name: "insecure remote video", file: &model.TaskFile{MimeType: "video/mp4", URL: "http://cdn.example/final.mp4"}},
		{name: "local video", file: &model.TaskFile{MimeType: "video/mp4", URL: "/api/v1/files/user/task/final.mp4"}},
		{name: "remote markdown", file: &model.TaskFile{MimeType: "text/markdown", URL: "https://cdn.example/content.md"}},
		{name: "non http", file: &model.TaskFile{MimeType: "video/mp4", URL: "javascript:alert(1)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskFileDirectPreviewURL(store, tt.file); got != tt.want {
				t.Fatalf("taskFileDirectPreviewURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnrichFilesWithDeliveryMetadataRejectsUnownedDirectURLs(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store := &fakeTaskStorage{name: "oss", ownedPrefix: "https://owned.example/"}
	store.downloadURL = "http://owned.example/insecure-download"
	svc.store = &attachmentURLTaskStorage{fakeTaskStorage: store}
	ctx := context.Background()
	taskID := "owned-url-task"
	executionID := "owned-url-execution"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: "digest",
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"video","path":"output/final.mp4","mime_type":"video/mp4"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	file := &model.TaskFile{
		ID: "owned-url-file", TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered,
		FilePath: "output/final.mp4", FileName: "final.mp4", MimeType: "video/mp4",
		OSSKey: "unused", StorageProvider: "oss", URL: "https://attacker.example/final.mp4",
	}

	if err := svc.EnrichFilesWithDeliveryMetadata(ctx, taskID, []*model.TaskFile{file}); err != nil {
		t.Fatal(err)
	}
	if file.PreviewURL != taskFilePreviewURL(taskID, file.ID) || file.DownloadURL != taskFileDownloadURL(taskID, file.ID) {
		t.Fatalf("unsafe direct URLs were exposed: preview=%q download=%q", file.PreviewURL, file.DownloadURL)
	}
}
