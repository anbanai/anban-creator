package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeVideoReferenceStorage struct {
	name              string
	url               string
	key               string
	files             map[string][]byte
	uploadContentType string
	downloadURL       string
}

func (f *fakeVideoReferenceStorage) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fake"
}

func (f *fakeVideoReferenceStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	f.key = key
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[key] = append([]byte(nil), data...)
	return &storage.UploadResult{URL: f.url, Key: key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (f *fakeVideoReferenceStorage) UploadFile(ctx context.Context, key string, filePath string, contentType string) (*storage.UploadResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return f.Upload(ctx, key, file, contentType)
}

func (f *fakeVideoReferenceStorage) GetURL(key string) string { return f.url }
func (f *fakeVideoReferenceStorage) Read(_ context.Context, key string) ([]byte, error) {
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), data...), nil
}
func (f *fakeVideoReferenceStorage) Delete(context.Context, string) error { return nil }
func (f *fakeVideoReferenceStorage) UploadURL(_ context.Context, _ string, contentType string, _ int) (string, error) {
	f.uploadContentType = contentType
	return "https://upload.example.com/put?signature=1", nil
}
func (f *fakeVideoReferenceStorage) DownloadURL(context.Context, string, int) (string, error) {
	if f.downloadURL != "" {
		return f.downloadURL, nil
	}
	return "https://download.example.com/get?signature=1", nil
}
func (f *fakeVideoReferenceStorage) HasCustomDomain() bool {
	return strings.HasPrefix(f.url, "https://")
}
func (f *fakeVideoReferenceStorage) IsOwnedURL(string) bool { return false }

func setupMCPVideoProject(t *testing.T) (context.Context, repository.Repository, string, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	userID := ""
	project := &model.Project{
		ID:       uuid.NewString(),
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "视频项目",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    service.VideoPurposePlanting,
		ModelKey:   "seedance-2.0",
		Resolution: "1080p",
		Ratio:      "9:16",
		Duration:   15,
		Watermark:  &watermark,
		Preflight:  true,
	})
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0", "seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0",
		MaxResolution: "1080p",
		MaxDuration:   15,
	})
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	logger := zerolog.New(io.Discard)
	svcs = &Services{ProjectSvc: service.NewProjectService(repo, &logger)}
	ctx := context.Background()
	return ctx, repo, userID, project.ID
}

func setupMCPVideoProjectWithServices(t *testing.T, store storage.Provider) (context.Context, repository.Repository, string, string) {
	t.Helper()
	ctx, repo, userID, projectID := setupMCPVideoProject(t)
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, nil, store, nil, &logger, "", nil, "", nil, nil)
	svcs.TaskSvc = taskSvc
	svcs.Store = store
	return ctx, repo, userID, projectID
}

func createMCPVideoTask(t *testing.T, repo repository.Repository, userID, projectID string) string {
	t.Helper()
	task := &model.Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformVideo,
		Status:    model.TaskStatusRunning,
		Title:     "视频生成任务",
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task.ID
}

func newArkTaskServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/contents/generations/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "cgt-video-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/contents/generations/tasks/cgt-video-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "cgt-video-1",
				"model":          "doubao-seedance-2-0-260128",
				"status":         "succeeded",
				"content":        map[string]any{"video_url": "https://cdn.example.com/provider/out.mp4", "last_frame_url": "https://cdn.example.com/provider/last.png", "file_url": "https://cdn.example.com/provider/file.mp4"},
				"resolution":     "720p",
				"ratio":          "16:9",
				"duration":       5,
				"revised_prompt": "优化后的提示词",
			})
		default:
			t.Fatalf("unexpected Ark request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func makeTinyMP4(t *testing.T, durationSeconds string) []byte {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for video duration probing test")
	}
	out := filepath.Join(t.TempDir(), "tiny.mp4")
	cmd := exec.Command(ffmpeg, "-y", "-f", "lavfi", "-i", "color=c=black:s=16x16:r=10:d="+durationSeconds, "-an", "-pix_fmt", "yuv420p", out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate tiny mp4: %v\n%s", err, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read tiny mp4: %v", err)
	}
	return data
}

func TestDownloadFileRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("123456"))
	}))
	defer srv.Close()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	out := filepath.Join(t.TempDir(), "video.mp4")
	err := downloadFile(context.Background(), srv.URL, out, 5)
	if err == nil || !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatalf("downloadFile error = %v, want max size error", err)
	}
	info, statErr := os.Stat(out)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat output: %v", statErr)
	}
	if info != nil && info.Size() <= 5 {
		t.Fatalf("expected oversized partial file, got size %d", info.Size())
	}
}

func TestRegisterVideoReferenceUploadsLocalFileAndRejectsPrivateStorageURL(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "/api/v1/files/uploads/video-references/u/ref.png"}
	svcs = &Services{Store: store}

	refPath := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(refPath, []byte("fake-png"), 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","type":"image_url","file_path":` + strconv.Quote(refPath) + `}`)},
	}
	result, err := registerVideoReferenceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected local storage URL to be rejected")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "OSS/CDN") || !strings.Contains(text, "HTTPS") {
		t.Fatalf("unexpected error text: %q", text)
	}
	if !strings.Contains(store.key, "uploads/video-references/") {
		t.Fatalf("storage key = %q, want video reference prefix", store.key)
	}
}

func TestRegisterVideoReferenceRequiresProjectID(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"type":"image_url","url":"https://example.com/ref.png"}`)},
	}
	result, err := registerVideoReferenceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "project_id is required") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestRegisterVideoReferenceReturnsPublicURL(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://cdn.example.com/uploads/video-references/u/ref.png"}
	svcs = &Services{Store: store}

	refPath := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(refPath, []byte("fake-png"), 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","type":"image_url","file_path":` + strconv.Quote(refPath) + `}`)},
	}
	result, err := registerVideoReferenceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "https://cdn.example.com/uploads/video-references/u/ref.png") || !strings.Contains(text, "ark_url") {
		t.Fatalf("unexpected result: %s", text)
	}
}

func TestRegisterVideoReferenceUploadsLocalFileAsTaskFileWhenTaskIDProvided(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/uploads/video-references/u/ref.png"}
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, store)
	taskID := createMCPVideoTask(t, repo, userID, projectID)

	refPath := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(refPath, []byte("fake-png"), 0644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"type":"image_url",
			"file_path":` + strconv.Quote(refPath) + `,
			"reference_role":"product_appearance"
		}`)},
	}
	result, err := registerVideoReferenceHandler(ctx, req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	files, err := repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task files: %v", err)
	}
	if len(files) != 1 || files[0].OSSKey == "" || files[0].StorageProvider == "" {
		t.Fatalf("reference file was not registered as task file: %#v", files)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"task_file"`) || !strings.Contains(text, `"task_file_id"`) {
		t.Fatalf("registered reference missing task file metadata: %s", text)
	}
}

func TestBuildVideoGenerationPlanHandlerValidatesArguments(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, _, _, projectID := setupMCPVideoProject(t)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":` + strconv.Quote(projectID) + `,"prompt":"生成视频","purpose":"unknown"}`)},
	}
	result, err := buildVideoGenerationPlanHandler(ctx, req)
	if err != nil {
		t.Fatalf("buildVideoGenerationPlanHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "purpose must be one of") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestBuildVideoGenerationPlanHandlerReturnsPayloadPreview(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, _, _, projectID := setupMCPVideoProject(t)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"prompt":"生成一条咖啡杯种草视频",
			"purpose":"planting",
			"references":[{"type":"image_url","url":"https://example.com/cup.png"}],
			"duration":15,
			"ratio":"9:16",
			"resolution":"1080p"
		}`)},
	}
	result, err := buildVideoGenerationPlanHandler(ctx, req)
	if err != nil {
		t.Fatalf("buildVideoGenerationPlanHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"generation_plan", "sdk_payload_preview", "estimated_credits", "reference-anchors.md", "shot-plan.md"} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q: %s", want, text)
		}
	}
}

func TestCreateVideoGenerationTaskHandlerRequiresService(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	svcs = &Services{}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","prompt":"生成视频"}`)},
	}
	result, err := createVideoGenerationTaskHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("createVideoGenerationTaskHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "video service not available") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestCreateVideoGenerationTaskHandlerRejectsInvalidReference(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, _, _, projectID := setupMCPVideoProject(t)
	svcs.VideoSvc = service.NewVideoService(&config.VideoAPIConfig{
		Key:     "test-key",
		BaseURL: "https://example.com",
	})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"prompt":"生成视频",
			"references":[{"type":"image_url","url":"http://localhost/a.png"}]
		}`)},
	}
	result, err := createVideoGenerationTaskHandler(ctx, req)
	if err != nil {
		t.Fatalf("createVideoGenerationTaskHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "publicly accessible HTTPS") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestDownloadVideoGenerationResultDoesNotRequireAgentOutputPath(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":"p1","task_id":"task-1","video_url":"https://example.com/out.mp4"}`)},
	}
	result, err := downloadVideoGenerationResultHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("downloadVideoGenerationResultHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error because services/storage are not configured, not because output_path is required")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; strings.Contains(text, "output_path is required") {
		t.Fatalf("must not require agent output_path, got %q", text)
	}
}

func TestCreateVideoGenerationTaskPersistsGenerationRecordAndTaskSnapshot(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/task/video.mp4"}
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, store)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	arkSrv := newArkTaskServer(t)
	defer arkSrv.Close()
	svcs.VideoSvc = service.NewVideoService(&config.VideoAPIConfig{Key: "test-key", BaseURL: arkSrv.URL, Timeout: time.Second})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"prompt":"生成一条 5 秒产品推广视频",
			"purpose":"promotion",
			"model":"seedance-2.0",
			"resolution":"720p",
			"ratio":"16:9",
			"duration":5,
			"references":[{"type":"image_url","url":"https://example.com/product.png","reference_role":"product_appearance"}]
		}`)},
	}
	result, err := createVideoGenerationTaskHandler(ctx, req)
	if err != nil {
		t.Fatalf("createVideoGenerationTaskHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	gen, err := repo.VideoGenerations().FindByArkTaskID(ctx, "cgt-video-1")
	if err != nil {
		t.Fatalf("find video generation: %v", err)
	}
	if gen.TaskID != taskID || gen.ProjectID != projectID || gen.Status != "submitted" {
		t.Fatalf("generation linkage/status = %#v", gen)
	}
	if gen.CreditsCharged <= 0 || gen.PricingBreakdown.Data().CNY <= 0 {
		t.Fatalf("pricing not persisted: %#v", gen)
	}
	if !strings.Contains(string(gen.References), "product_appearance") {
		t.Fatalf("reference role not persisted: %s", string(gen.References))
	}
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if task.VideoGenerationID != gen.ID || task.VideoEstimatedCredits != gen.CreditsCharged || task.VideoCreditsCharged != gen.CreditsCharged {
		t.Fatalf("task video snapshot not linked: %#v generation=%#v", task, gen)
	}
}

func TestQueryAndDownloadVideoGenerationUpdatePersistentRecordAndOSSFile(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/tasks/generated.mp4"}
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, store)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	gen := &model.VideoGeneration{
		UserID:    userID,
		ProjectID: projectID,
		TaskID:    taskID,
		ArkTaskID: "cgt-video-1",
		Status:    "submitted",
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		t.Fatalf("create generation: %v", err)
	}
	arkSrv := newArkTaskServer(t)
	defer arkSrv.Close()
	svcs.VideoSvc = service.NewVideoService(&config.VideoAPIConfig{Key: "test-key", BaseURL: arkSrv.URL, Timeout: time.Second})

	queryReq := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":` + strconv.Quote(projectID) + `,"video_task_id":"cgt-video-1"}`)}}
	queryResult, err := queryVideoGenerationTaskHandler(ctx, queryReq)
	if err != nil {
		t.Fatalf("queryVideoGenerationTaskHandler returned error: %v", err)
	}
	if queryResult.IsError {
		t.Fatalf("unexpected query error: %s", queryResult.Content[0].(*mcp.TextContent).Text)
	}
	updated, err := repo.VideoGenerations().FindByArkTaskID(ctx, "cgt-video-1")
	if err != nil {
		t.Fatalf("find updated generation: %v", err)
	}
	if updated.Status != "succeeded" || !strings.Contains(string(updated.ProviderURLs), "provider/out.mp4") {
		t.Fatalf("query did not persist provider metadata: %#v urls=%s", updated, string(updated.ProviderURLs))
	}

	videoSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("fake-video"))
	}))
	defer videoSrv.Close()
	oldClient := http.DefaultClient
	http.DefaultClient = videoSrv.Client()
	t.Cleanup(func() { http.DefaultClient = oldClient })
	downloadReq := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
		"project_id":` + strconv.Quote(projectID) + `,
		"task_id":` + strconv.Quote(taskID) + `,
		"video_url":` + strconv.Quote(videoSrv.URL+"/out.mp4") + `,
		"file_name":"final.mp4"
	}`)}}
	downloadResult, err := downloadVideoGenerationResultHandler(ctx, downloadReq)
	if err != nil {
		t.Fatalf("downloadVideoGenerationResultHandler returned error: %v", err)
	}
	if downloadResult.IsError {
		t.Fatalf("unexpected download error: %s", downloadResult.Content[0].(*mcp.TextContent).Text)
	}
	updated, err = repo.VideoGenerations().FindByArkTaskID(ctx, "cgt-video-1")
	if err != nil {
		t.Fatalf("find downloaded generation: %v", err)
	}
	if !strings.Contains(string(updated.TaskFileIDs), "generated_video") {
		t.Fatalf("download did not persist task file IDs: %s", string(updated.TaskFileIDs))
	}
	files, err := repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task files: %v", err)
	}
	if len(files) != 1 || files[0].StorageProvider == "" || files[0].OSSKey == "" {
		t.Fatalf("generated video was not registered as OSS task file: %#v", files)
	}
}

func TestRegisterVideoReferenceTaskFileMeasuresVideoDurationAndIgnoresAgentDuration(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/tasks/input.mp4"}
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, store)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	videoData := makeTinyMP4(t, "3")
	tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, taskID, userID, "refs/input.mp4", bytes.NewReader(videoData), "video/mp4", int64(len(videoData)))
	if err != nil {
		t.Fatalf("upload task file: %v", err)
	}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"task_file_id":` + strconv.Quote(tf.ID) + `,
			"type":"video_url",
			"reference_role":"motion_reference",
			"input_duration_seconds":99
		}`)},
	}
	result, err := registerVideoReferenceHandler(ctx, req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if strings.Contains(text, `"input_duration_seconds":99`) {
		t.Fatalf("agent-supplied duration must not be trusted: %s", text)
	}
	if !strings.Contains(text, `"reference_role":"motion_reference"`) || !strings.Contains(text, `"input_duration_seconds"`) {
		t.Fatalf("registered reference missing role or measured duration: %s", text)
	}
}

func TestBuildVideoPlanRejectsUnmeasuredRawVideoReferenceDuration(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, _, _, projectID := setupMCPVideoProject(t)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"prompt":"生成一条视频改编",
			"references":[{"type":"video_url","url":"https://example.com/input.mp4","input_duration_seconds":99}]
		}`)},
	}
	result, err := buildVideoGenerationPlanHandler(ctx, req)
	if err != nil {
		t.Fatalf("buildVideoGenerationPlanHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected raw video reference without server measurement to be rejected")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "register_video_reference") || !strings.Contains(text, "task_file_id") {
		t.Fatalf("unexpected error text: %q", text)
	}
}
