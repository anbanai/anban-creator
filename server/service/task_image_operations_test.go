package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeTaskImageOperationsImage struct {
	uploadPath   string
	compressPath string
}

func (f *fakeTaskImageOperationsImage) UploadImage(_ context.Context, _, _, filePath string) (*UploadImageResult, error) {
	f.uploadPath = filePath
	return &UploadImageResult{URL: "https://cdn.example/image.png"}, nil
}

func (f *fakeTaskImageOperationsImage) CompressImage(filePath string, _ int) (string, bool, error) {
	f.compressPath = filePath
	return filePath + ".compressed", true, nil
}

type fakeTaskImageUnderstandingClient struct {
	calls  int
	source string
	usage  srvconfig.TokenUsage
	err    error
}

func (f *fakeTaskImageUnderstandingClient) CompleteWithImageResult(_ context.Context, _, _, imageSource string) (*LLMResult, error) {
	f.calls++
	f.source = imageSource
	if f.err != nil {
		return nil, f.err
	}
	return &LLMResult{Text: "  analysis  ", Usage: f.usage}, nil
}

type fakeTaskImageOperationsCost struct {
	recorded     *RecordProviderTokenCostRequest
	unreconciled *RecordMediaUnreconciledRequest
	recordErr    error
}

type recordingCropStorage struct {
	storage.Provider

	mu             sync.Mutex
	uploadKeys     []string
	deletedKeys    []string
	blockFirst     bool
	firstStarted   chan struct{}
	releaseFirst   chan struct{}
	firstBlockOnce sync.Once
}

func (s *recordingCropStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	if s.isCropOutput(key) {
		s.mu.Lock()
		s.uploadKeys = append(s.uploadKeys, key)
		s.mu.Unlock()
		if s.blockFirst {
			blocked := false
			s.firstBlockOnce.Do(func() {
				blocked = true
				close(s.firstStarted)
			})
			if blocked {
				select {
				case <-ctx.Done():
					return nil, context.Cause(ctx)
				case <-s.releaseFirst:
				}
			}
		}
	}
	return s.Provider.Upload(ctx, key, reader, contentType)
}

func (s *recordingCropStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	s.deletedKeys = append(s.deletedKeys, key)
	s.mu.Unlock()
	return s.Provider.Delete(ctx, key)
}

func (s *recordingCropStorage) outputUploadKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uploadKeys...)
}

func (s *recordingCropStorage) outputDeletedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deletedKeys...)
}

func (s *recordingCropStorage) isCropOutput(key string) bool {
	return strings.Contains(filepath.ToSlash(key), "/mcp/output/cropped")
}

type cropPersistenceFailureRepository struct {
	repository.Repository
	taskFiles repository.TaskFileRepository
	err       error
}

func (r *cropPersistenceFailureRepository) TaskFiles() repository.TaskFileRepository {
	return r.taskFiles
}

func (r *cropPersistenceFailureRepository) WithTx(context.Context, func(repository.Repository) error) error {
	return r.err
}

type cropPersistenceFailureTaskFiles struct {
	repository.TaskFileRepository
	err error
}

func (r *cropPersistenceFailureTaskFiles) InsertIfAbsent(context.Context, *model.TaskFile) (*model.TaskFile, bool, error) {
	return nil, false, r.err
}

func (r *cropPersistenceFailureTaskFiles) ReplaceIfCurrent(context.Context, *model.TaskFile, *model.TaskFile) (*model.TaskFile, bool, error) {
	return nil, false, r.err
}

type taskImageOperationsCropFixture struct {
	service     *TaskImageOperationsService
	tasks       *TaskService
	repo        repository.Repository
	store       *recordingCropStorage
	task        *model.Task
	userID      string
	executionID string
}

func newTaskImageOperationsCropFixture(t *testing.T) *taskImageOperationsCropFixture {
	t.Helper()
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: model.PlatformArticle, Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	executionID := startTaskArtifactExecution(t, repo, task)
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingCropStorage{Provider: local}
	for index, source := range [][]byte{
		cropTestPNG(t, color.NRGBA{R: 0xff, A: 0xff}),
		cropTestPNG(t, color.NRGBA{B: 0xff, A: 0xff}),
	} {
		relPath := []string{"output/source-a.png", "output/source-b.png"}[index]
		key := "fixtures/" + filepath.Base(relPath)
		if _, err := local.Upload(ctx, key, bytes.NewReader(source), "image/png"); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.TaskFiles().UpsertPendingCurrentExecution(ctx, task.ID, executionID, &model.TaskFile{
			TaskID: task.ID, ExecutionID: executionID, Role: model.FileRoleImage,
			FilePath: relPath, FileName: filepath.Base(relPath), MimeType: "image/png",
			FileSize: int64(len(source)), ContentHash: hashTaskFileContent(source),
			OSSKey: key, OSSURL: local.GetURL(key), StorageProvider: local.Name(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	tasks := newTestTaskService(repo, nil, store, &logger, "", nil, nil)
	return &taskImageOperationsCropFixture{
		service: NewTaskImageOperationsService(tasks, nil, nil, nil, TaskImageOperationsConfig{}, &logger),
		tasks:   tasks, repo: repo, store: store, task: task, userID: userID, executionID: executionID,
	}
}

func cropTestPNG(t *testing.T, fill color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, fill)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func findCropOutputFile(t *testing.T, repo repository.Repository, executionID string) *model.TaskFile {
	t.Helper()
	files, err := repo.TaskFiles().FindByExecutionID(context.Background(), executionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.FilePath == "output/cropped.png" {
			return file
		}
	}
	t.Fatal("cropped task file was not persisted")
	return nil
}

func cropRequest(f *taskImageOperationsCropFixture, inputPath string) CropTaskImageRequest {
	return CropTaskImageRequest{
		UserID: f.userID, TaskID: f.task.ID, ExecutionID: f.executionID,
		InputPath: inputPath, OutputPath: "output/cropped.png",
		TargetWidth: 1, TargetHeight: 1, Anchor: "center",
	}
}

func TestTaskImageOperationsCropUsesContentAddressedObject(t *testing.T) {
	f := newTaskImageOperationsCropFixture(t)
	result, err := f.service.Crop(context.Background(), cropRequest(f, "output/source-a.png"))
	if err != nil {
		t.Fatal(err)
	}
	persisted := findCropOutputFile(t, f.repo, f.executionID)
	if !strings.Contains(persisted.OSSKey, "-"+result.ContentHash+"-") {
		t.Fatalf("cropped object key = %q, want content hash %q in immutable key", persisted.OSSKey, result.ContentHash)
	}
	plainKey := buildTaskMCPArtifactStoragePrefix(f.task, f.executionID) + "output/cropped.png"
	if persisted.OSSKey == plainKey {
		t.Fatalf("cropped object reused mutable path key %q", plainKey)
	}
}

func TestTaskImageOperationsCropCleansObjectWhenPersistenceFails(t *testing.T) {
	f := newTaskImageOperationsCropFixture(t)
	wantErr := errors.New("forced crop persistence failure")
	failingTaskFiles := &cropPersistenceFailureTaskFiles{TaskFileRepository: f.repo.TaskFiles(), err: wantErr}
	failingRepo := &cropPersistenceFailureRepository{Repository: f.repo, taskFiles: failingTaskFiles, err: wantErr}
	logger := zerolog.Nop()
	tasks := newTestTaskService(failingRepo, nil, f.store, &logger, "", nil, nil)
	service := NewTaskImageOperationsService(tasks, nil, nil, nil, TaskImageOperationsConfig{}, &logger)

	if _, err := service.Crop(context.Background(), cropRequest(f, "output/source-a.png")); !errors.Is(err, wantErr) {
		t.Fatalf("Crop error = %v, want %v", err, wantErr)
	}
	uploaded := f.store.outputUploadKeys()
	if len(uploaded) != 1 {
		t.Fatalf("cropped uploads = %#v, want one attempted object", uploaded)
	}
	if deleted := f.store.outputDeletedKeys(); len(deleted) != 1 || deleted[0] != uploaded[0] {
		t.Fatalf("deleted cropped objects = %#v, want %#v", deleted, uploaded)
	}
	if _, err := f.store.Read(context.Background(), uploaded[0]); err == nil {
		t.Fatalf("unpersisted cropped object %q remains readable", uploaded[0])
	}
}

func TestTaskImageOperationsConcurrentCropNeverReusesObjectKey(t *testing.T) {
	f := newTaskImageOperationsCropFixture(t)
	f.store.blockFirst = true
	f.store.firstStarted = make(chan struct{})
	f.store.releaseFirst = make(chan struct{})
	type cropResult struct {
		result *CropTaskImageResult
		err    error
	}
	firstDone := make(chan cropResult, 1)
	go func() {
		result, err := f.service.Crop(context.Background(), cropRequest(f, "output/source-a.png"))
		firstDone <- cropResult{result: result, err: err}
	}()
	select {
	case <-f.store.firstStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first crop did not reach storage upload")
	}
	secondDone := make(chan cropResult, 1)
	go func() {
		result, err := f.service.Crop(context.Background(), cropRequest(f, "output/source-b.png"))
		secondDone <- cropResult{result: result, err: err}
	}()
	var second cropResult
	select {
	case second = <-secondDone:
	case <-time.After(5 * time.Second):
		close(f.store.releaseFirst)
		t.Fatal("second crop did not complete while the first upload was blocked")
	}
	close(f.store.releaseFirst)
	first := <-firstDone
	if first.err != nil || first.result == nil || second.err != nil || second.result == nil {
		t.Fatalf("concurrent Crop results = first %#v/%v second %#v/%v", first.result, first.err, second.result, second.err)
	}

	uploaded := f.store.outputUploadKeys()
	unique := make(map[string]struct{}, len(uploaded))
	for _, key := range uploaded {
		if _, exists := unique[key]; exists {
			t.Fatalf("concurrent crops reused mutable object key %q: %#v", key, uploaded)
		}
		unique[key] = struct{}{}
	}
	persisted := findCropOutputFile(t, f.repo, f.executionID)
	body, err := f.store.Read(context.Background(), persisted.OSSKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := hashTaskFileContent(body); got != persisted.ContentHash {
		t.Fatalf("persisted crop metadata hash = %q, object hash = %q", persisted.ContentHash, got)
	}
}

func (f *fakeTaskImageOperationsCost) CatalogID() string { return "catalog" }
func (f *fakeTaskImageOperationsCost) RecordProviderTokenUsage(_ context.Context, req RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error) {
	f.recorded = &req
	return &model.BillingProviderCostEvent{}, f.recordErr
}
func (f *fakeTaskImageOperationsCost) RecordMediaUnreconciled(_ context.Context, req RecordMediaUnreconciledRequest) (*model.BillingProviderCostEvent, error) {
	f.unreconciled = &req
	return &model.BillingProviderCostEvent{}, f.recordErr
}

func TestTaskImageOperationsKeepRuntimePathsAndRecordAnalysisCost(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := "task-image-operations-user"
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	taskID := "task-image-operations-task"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	imagePath := filepath.Join(workspace, taskID, "output", "image.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	images := &fakeTaskImageOperationsImage{}
	understanding := &fakeTaskImageUnderstandingClient{usage: srvconfig.TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}}
	cost := &fakeTaskImageOperationsCost{}
	svc := NewTaskImageOperationsService(tasks, images, understanding, cost, TaskImageOperationsConfig{
		UnderstandingProvider: "provider", UnderstandingModel: "model",
	}, &logger)

	if _, err := svc.Upload(ctx, UploadTaskImageRequest{UserID: userID, ProjectID: projectID, TaskID: taskID, FilePath: "output/image.png"}); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if images.uploadPath != "output/image.png" {
		t.Fatalf("upload path = %q, want runtime-relative path", images.uploadPath)
	}
	if _, err := svc.Compress(ctx, CompressTaskImageRequest{UserID: userID, TaskID: taskID, FilePath: "output/image.png"}); err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if images.compressPath != "output/image.png" {
		t.Fatalf("compress path = %q, want runtime-relative path", images.compressPath)
	}
	result, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: userID, ProjectID: projectID, TaskID: taskID, FilePath: imagePath, Prompt: "inspect"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Analysis != "analysis" || understanding.calls != 1 || cost.recorded == nil {
		t.Fatalf("result=%#v understanding_calls=%d cost=%#v", result, understanding.calls, cost.recorded)
	}
	if cost.recorded.TaskID != taskID || cost.recorded.Provider != "provider" || cost.recorded.Model != "model" {
		t.Fatalf("provider cost request = %#v", cost.recorded)
	}
}

func TestTaskImageOperationsRejectForeignTaskBeforeDelegation(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	ownerID := "task-image-owner"
	projectID := createTestProject(t, repo, ownerID, model.PlatformArticle)
	taskID := "task-image-foreign"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: ownerID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	understanding := &fakeTaskImageUnderstandingClient{}
	svc := NewTaskImageOperationsService(newTestTaskService(repo, nil, nil, &logger, "", nil, nil), nil, understanding, nil, TaskImageOperationsConfig{}, &logger)
	_, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: "foreign", ProjectID: projectID, TaskID: taskID, ImageURL: "https://example.com/image.png", Prompt: "inspect"})
	if !errors.Is(err, ErrTaskImageOperationOwnership) {
		t.Fatalf("Analyze error = %v, want ownership error", err)
	}
	if understanding.calls != 0 {
		t.Fatalf("understanding calls = %d, want 0", understanding.calls)
	}
}

func TestTaskImageOperationsRejectsForeignProjectWithoutTaskBeforeDelegation(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	ownerID := "task-image-project-owner-no-task"
	projectID := createTestProject(t, repo, ownerID, model.PlatformArticle)
	imagePath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), 0o644); err != nil {
		t.Fatal(err)
	}
	understanding := &fakeTaskImageUnderstandingClient{}
	svc := NewTaskImageOperationsService(newTestTaskService(repo, nil, nil, &logger, "", nil, nil), nil, understanding, nil, TaskImageOperationsConfig{}, &logger)
	_, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: "foreign", ProjectID: projectID, FilePath: imagePath, Prompt: "inspect"})
	if err == nil || !strings.Contains(err.Error(), "project does not belong to user") {
		t.Fatalf("Analyze error = %v, want project ownership error", err)
	}
	if understanding.calls != 0 {
		t.Fatalf("understanding calls = %d, want 0", understanding.calls)
	}
}

func TestTaskImageOperationsRejectsProjectMismatchBeforeDelegation(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := "task-image-project-owner"
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	taskID := "task-image-project-mismatch"
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	understanding := &fakeTaskImageUnderstandingClient{}
	svc := NewTaskImageOperationsService(newTestTaskService(repo, nil, nil, &logger, "", nil, nil), nil, understanding, nil, TaskImageOperationsConfig{}, &logger)
	_, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: userID, ProjectID: "wrong-project", TaskID: taskID, ImageURL: "https://example.com/image.png", Prompt: "inspect"})
	if !errors.Is(err, ErrTaskImageOperationProjectMismatch) || understanding.calls != 0 {
		t.Fatalf("Analyze = %v, understanding calls=%d; want project mismatch before delegation", err, understanding.calls)
	}
}

func TestTaskImageOperationsRejectsInvalidLocalAndRemoteImageSources(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := "task-image-invalid-source-user"
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	tasks := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	localText := filepath.Join(t.TempDir(), "not-image.txt")
	if err := os.WriteFile(localText, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	localLarge := filepath.Join(t.TempDir(), "large.png")
	if err := os.WriteFile(localLarge, make([]byte, maxAnalyzedTaskImageBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		filePath   string
		imageURL   string
		downloaded []byte
		want       string
	}{
		{name: "local non-image", filePath: localText, want: "file is not an image"},
		{name: "local oversized", filePath: localLarge, want: "too large"},
		{name: "remote non-image", imageURL: "https://example.com/text", downloaded: []byte("plain text"), want: "downloaded file is not an image"},
		{name: "remote oversized", imageURL: "https://example.com/large.png", downloaded: make([]byte, maxAnalyzedTaskImageBytes+1), want: "exceeds max size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			understanding := &fakeTaskImageUnderstandingClient{}
			svc := NewTaskImageOperationsService(tasks, nil, understanding, nil, TaskImageOperationsConfig{}, &logger)
			svc.downloadAnalysisImage = func(context.Context, string, int64) ([]byte, error) {
				if int64(len(tt.downloaded)) > maxAnalyzedTaskImageBytes {
					return nil, errors.New("external image exceeds max size")
				}
				return tt.downloaded, nil
			}
			_, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: userID, ImageURL: tt.imageURL, FilePath: tt.filePath, ProjectID: projectID, Prompt: "inspect"})
			if err == nil || !strings.Contains(err.Error(), tt.want) || understanding.calls != 0 {
				t.Fatalf("Analyze = %v, calls=%d; want %q before delegation", err, understanding.calls, tt.want)
			}
		})
	}
}

func TestTaskImageAnalysisSSRFAndRedirectPolicy(t *testing.T) {
	for _, address := range []string{
		"0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.0.1", "172.16.0.1",
		"192.0.0.1", "192.0.2.1", "192.168.0.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1",
		"::1", "64:ff9b::a00:1", "64:ff9b:1::1", "100::1", "100:0:0:1::1",
		"2001:db8::1", "5f00::1", "fc00::1", "fe80::1",
	} {
		if isPublicTaskAnalysisIP(net.ParseIP(address)) {
			t.Errorf("special-use address %s accepted as public", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicTaskAnalysisIP(net.ParseIP(address)) {
			t.Errorf("public address %s rejected", address)
		}
	}
	for _, tt := range []struct {
		name string
		req  *http.Request
		via  []*http.Request
		want string
	}{
		{name: "downgrade", req: &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com"}}, want: "must use https"},
		{name: "too many", req: &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}, via: make([]*http.Request, 3), want: "too many redirects"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateTaskAnalysisRedirect(tt.req, tt.via); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("redirect validation = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTaskImageAnalysisCostEvidenceLifecycle(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.Nop()
	userID := "task-image-cost-user"
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	tasks := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name             string
		understanding    *fakeTaskImageUnderstandingClient
		costErr          error
		wantAnalyzeError bool
		wantUnreconciled bool
		wantCacheRead    int64
	}{
		{name: "analysis failure is unreconciled", understanding: &fakeTaskImageUnderstandingClient{err: errors.New("vision failed")}, wantAnalyzeError: true, wantUnreconciled: true},
		{name: "zero usage is unreconciled", understanding: &fakeTaskImageUnderstandingClient{}, wantUnreconciled: true},
		{name: "cached token fallback", understanding: &fakeTaskImageUnderstandingClient{usage: srvconfig.TokenUsage{InputTokens: 9, CachedInputTokens: 4, OutputTokens: 1, TotalTokens: 10}}, wantCacheRead: 4},
		{name: "explicit cache read wins", understanding: &fakeTaskImageUnderstandingClient{usage: srvconfig.TokenUsage{InputTokens: 9, CachedInputTokens: 4, CacheReadInputTokens: 6, CacheCreationInputTokens: 2, OutputTokens: 1, TotalTokens: 10}}, wantCacheRead: 6},
		{name: "cost write failure preserves result", understanding: &fakeTaskImageUnderstandingClient{usage: srvconfig.TokenUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}, costErr: errors.New("cost write failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := &fakeTaskImageOperationsCost{recordErr: tt.costErr}
			svc := NewTaskImageOperationsService(tasks, nil, tt.understanding, cost, TaskImageOperationsConfig{UnderstandingProvider: "provider", UnderstandingModel: "model"}, &logger)
			result, err := svc.Analyze(ctx, AnalyzeTaskImageRequest{UserID: userID, ProjectID: projectID, FilePath: path, Prompt: "inspect"})
			if tt.wantAnalyzeError {
				if err == nil || result != nil {
					t.Fatalf("Analyze = %#v, %v; want analysis error", result, err)
				}
			} else if err != nil || result == nil || result.Analysis != "analysis" {
				t.Fatalf("Analyze = %#v, %v; want successful result", result, err)
			}
			if tt.wantUnreconciled {
				if cost.unreconciled == nil || cost.unreconciled.ReasonCode != model.BillingExecutionCostReasonMissingProviderUsage {
					t.Fatalf("unreconciled evidence = %#v", cost.unreconciled)
				}
				return
			}
			if cost.recorded == nil || cost.recorded.Usage.CacheRead != tt.wantCacheRead {
				t.Fatalf("token evidence = %#v, want cache read %d", cost.recorded, tt.wantCacheRead)
			}
		})
	}
}
