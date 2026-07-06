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
	"gorm.io/datatypes"
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

func TestVideoGenerationSchemaDoesNotExposeModelSelection(t *testing.T) {
	schema := videoGenerationInputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing or wrong type: %#v", schema["properties"])
	}
	if _, ok := props["model"]; ok {
		t.Fatalf("video generation schema must not expose model")
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing or wrong type: %#v", schema["required"])
	}
	if !containsAnyString(required, "task_id") {
		t.Fatalf("video generation schema must require task_id, got %#v", required)
	}
}

func TestVideoGenerationToolsRejectExplicitModel(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":"project-1",
			"task_id":"task-1",
			"prompt":"test video",
			"model":"seedance-2.0-mini"
		}`)},
	}

	handlers := map[string]func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error){
		"build_video_generation_plan":      buildVideoGenerationPlanHandler,
		"validate_video_generation_params": validateVideoGenerationParamsHandler,
		"create_video_generation_job":      createVideoGenerationJobHandler,
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			res, err := handler(withMCPUserID(context.Background(), "user-1"), req)
			if err != nil {
				t.Fatalf("%s returned error: %v", name, err)
			}
			if res == nil || !res.IsError {
				t.Fatalf("expected tool error, got %#v", res)
			}
			if !strings.Contains(callToolText(res), "model is not accepted") {
				t.Fatalf("error text = %q, want model rejection", callToolText(res))
			}
		})
	}
}

func TestBuildVideoGenerationPlanRejectsUnavailablePersistedProjectModel(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    service.VideoPurposePlanting,
		ModelKey:   "missing-video",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
	})
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"missing-video", "seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"prompt":"生成视频"
		}`)},
	}
	res, err := buildVideoGenerationPlanHandler(ctx, req)
	if err != nil {
		t.Fatalf("buildVideoGenerationPlanHandler returned error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error, got %#v", res)
	}
	if text := callToolText(res); !strings.Contains(text, "模型未配置或不可用: missing-video") {
		t.Fatalf("error text = %q, want unavailable persisted project model", text)
	}
}

func TestBuildVideoGenerationPlanRejectsUnavailablePersistedTaskModel(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTaskWithConfig(t, repo, userID, projectID, model.VideoTaskConfig{
		ModelKey: "missing-video",
	})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"prompt":"生成视频"
		}`)},
	}
	res, err := buildVideoGenerationPlanHandler(ctx, req)
	if err != nil {
		t.Fatalf("buildVideoGenerationPlanHandler returned error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error, got %#v", res)
	}
	if text := callToolText(res); !strings.Contains(text, "模型未配置或不可用: missing-video") {
		t.Fatalf("error text = %q, want unavailable persisted task model", text)
	}
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
	projectSvc := service.NewProjectService(repo, &logger)
	projectSvc.SetVideoCatalog(service.DefaultVideoModelCatalog())
	svcs = &Services{ProjectSvc: projectSvc}
	SetBillingServices(nil, nil, &config.Config{VideoAPI: config.VideoAPIConfig{
		ModelCatalog: defaultMCPVideoModelCatalogEntries(),
	}})
	ctx := context.Background()
	return ctx, repo, userID, project.ID
}

func defaultMCPVideoModelCatalogEntries() []config.VideoModelCatalogEntry {
	return []config.VideoModelCatalogEntry{
		{Key: "seedance-2.0"},
		{Key: "seedance-2.0-mini"},
	}
}

func setupMCPVideoProjectWithServices(t *testing.T, store storage.Provider) (context.Context, repository.Repository, string, string) {
	t.Helper()
	ctx, repo, userID, projectID := setupMCPVideoProject(t)
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, nil, store, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetVideoCatalogAndCreditMultiplier(service.DefaultVideoModelCatalog(), 1000)
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

func createMCPVideoTaskWithConfig(t *testing.T, repo repository.Repository, userID, projectID string, cfg model.VideoTaskConfig) string {
	t.Helper()
	task := &model.Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformVideo,
		Status:    model.TaskStatusRunning,
		Title:     "视频生成任务",
	}
	task.SetVideoConfig(cfg)
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task.ID
}

func modelIDForVideoKey(t *testing.T, key string) string {
	t.Helper()
	spec, ok := service.DefaultVideoModelCatalog()[key]
	if !ok {
		t.Fatalf("video model key %q is not in test catalog", key)
	}
	return spec.ModelID
}

type fakeVideoVisionLLM struct {
	imageSource string
	videoSource string
	userPrompt  string
	response    string
	err         error
	errors      []error
	model       string
	usage       config.TokenUsage
	calls       []fakeVideoVisionCall
}

func (f *fakeVideoVisionLLM) Complete(context.Context, string, string) (string, error) {
	return "", errors.New("text completion should not be used for video reference analysis")
}

type fakeVideoVisionCall struct {
	kind        string
	imageSource string
	userPrompt  string
}

type staticVideoTransport struct {
	data []byte
}

func (t staticVideoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"video/mp4"}},
		Body:       io.NopCloser(bytes.NewReader(t.data)),
		Request:    req,
	}, nil
}

func (f *fakeVideoVisionLLM) CompleteWithVideoURL(_ context.Context, _ string, userPrompt, videoURL string) (string, error) {
	f.videoSource = videoURL
	f.userPrompt = userPrompt
	f.calls = append(f.calls, fakeVideoVisionCall{kind: "video", imageSource: videoURL, userPrompt: userPrompt})
	if len(f.errors) >= len(f.calls) && f.errors[len(f.calls)-1] != nil {
		return "", f.errors[len(f.calls)-1]
	}
	if f.err != nil {
		return "", f.err
	}
	return f.videoResponse()
}

func (f *fakeVideoVisionLLM) CompleteWithVideoURLResult(_ context.Context, _ string, userPrompt, videoURL string) (*service.LLMResult, error) {
	f.videoSource = videoURL
	f.userPrompt = userPrompt
	f.calls = append(f.calls, fakeVideoVisionCall{kind: "video", imageSource: videoURL, userPrompt: userPrompt})
	if len(f.errors) >= len(f.calls) && f.errors[len(f.calls)-1] != nil {
		return nil, f.errors[len(f.calls)-1]
	}
	if f.err != nil {
		return nil, f.err
	}
	text, err := f.videoResponse()
	if err != nil {
		return nil, err
	}
	return &service.LLMResult{Text: text, Model: f.model, Usage: f.usage}, nil
}

func (f *fakeVideoVisionLLM) CompleteWithImage(_ context.Context, _ string, userPrompt, imageSource string) (string, error) {
	f.imageSource = imageSource
	f.userPrompt = userPrompt
	f.calls = append(f.calls, fakeVideoVisionCall{kind: "image", imageSource: imageSource, userPrompt: userPrompt})
	if len(f.errors) >= len(f.calls) && f.errors[len(f.calls)-1] != nil {
		return "", f.errors[len(f.calls)-1]
	}
	if f.err != nil {
		return "", f.err
	}
	return f.videoResponse()
}

func (f *fakeVideoVisionLLM) videoResponse() (string, error) {
	if f.response != "" {
		return f.response, nil
	}
	return `{
		"visual_summary":"办公室里一位效率博主面对镜头讲解会议记录方法。",
		"timeline":[{"time_range":"0-3s","visual":"人物看向镜头，右手指向白板","action":"开场钩子"}],
		"subjects":["效率博主"],
		"people":[{"role":"主体","appearance":"黑色衬衫，短发","expression":"自信、亲和"}],
		"expressions":["自信","亲和"],
		"actions":["指向白板","讲解"],
		"scenes":["办公室"],
		"camera_motion":["中景固定镜头"],
		"rhythm":["快节奏口播"],
		"must_keep":["黑色衬衫主体身份","办公室场景","快节奏口播"],
		"can_change":["手势细节","白板内容"],
		"must_not_change":["主体年龄气质","职业场景"],
		"planning_hints":["每个镜头重复主体身份锚点","开头三秒给明确利益点"]
	}`, nil
}

func TestAnalyzeVideoReferenceUsesVisionModelAndRegistersArtifact(t *testing.T) {
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/tasks/video-understanding.json"}
	ctx, repo, _, projectID := setupMCPVideoProjectWithServices(t, store)
	taskID := createMCPVideoTask(t, repo, "", projectID)
	vision := &fakeVideoVisionLLM{}
	logger := zerolog.New(io.Discard)
	writingSvc := service.NewWritingService(repo, vision, "", time.Minute, &logger)
	writingSvc.SetVideoUnderstandingClient(vision)
	svcs.WritingSvc = writingSvc

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"video_url":"https://cdn.example.com/ref.mp4",
			"reference_role":"subject identity",
			"purpose_hint":"personal_ip"
		}`)},
	}
	result, err := analyzeVideoReferenceHandler(ctx, req)
	if err != nil {
		t.Fatalf("analyzeVideoReferenceHandler returned error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{`"analysis_mode":"native_video"`, `"visual_summary"`, `"must_keep"`, `"video-understanding.json"`, `"task_file"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("analysis response missing %q: %s", want, text)
		}
	}
	if vision.videoSource != "https://cdn.example.com/ref.mp4" {
		t.Fatalf("vision videoSource = %q, want video URL", vision.videoSource)
	}
	if !strings.Contains(vision.userPrompt, "subject identity") || !strings.Contains(vision.userPrompt, "personal_ip") {
		t.Fatalf("vision prompt did not include role and purpose hint: %s", vision.userPrompt)
	}
	files, err := repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		t.Fatalf("list task files: %v", err)
	}
	found := false
	for _, f := range files {
		if f.FileName == "video-understanding.json" && f.MimeType == "application/json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered task files did not include video-understanding.json: %#v", files)
	}
}

func TestAnalyzeVideoReferenceChargesTokenUsageAndStoresMetadata(t *testing.T) {
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/tasks/video-understanding.json"}
	ctx, repo, _, projectID := setupMCPVideoProjectWithServices(t, store)
	userID := uuid.NewString()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:             userID,
		Email:          userID + "@example.com",
		Password:       "hashed",
		Tier:           model.TierFree,
		InviteCode:     strings.ToUpper(userID[:8]),
		CreditsBalance: 10_000,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ctx = withMCPUserID(ctx, userID)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	vision := &fakeVideoVisionLLM{
		model: "kimi-k2.7-code-highspeed",
		usage: config.TokenUsage{
			InputTokens:       10_000,
			CachedInputTokens: 2_000,
			OutputTokens:      1_000,
			TotalTokens:       11_000,
		},
	}
	logger := zerolog.New(io.Discard)
	writingSvc := service.NewWritingService(repo, nil, "", time.Minute, &logger)
	writingSvc.SetVideoUnderstandingClient(vision)
	svcs.WritingSvc = writingSvc
	creditSvc := service.NewCreditService(repo, &config.CreditsConfig{}, &logger)
	SetBillingServices(creditSvc, nil, &config.Config{
		VideoUnderstanding: config.VideoUnderstandingRuntimeConfig{
			UnderstandingRuntimeConfig: config.UnderstandingRuntimeConfig{
				ProviderKey: "moonshot",
				Model:       "kimi-k2.7-code-highspeed",
			},
		},
		ModelPrices: config.ModelPricesConfig{
			CurrencyRates: map[string]config.CurrencyRate{"USD": {ToCNY: 7.2}},
			TokenModels: map[string]config.TokenModelPrice{
				"moonshot/kimi-k2.7-code-highspeed": {
					Currency:    "USD",
					Unit:        1_000_000,
					CachedInput: 0.38,
					Input:       1.90,
					Output:      8.00,
				},
			},
		},
		Billing: config.BillingConfig{
			CreditsPerCNY:         1000,
			TierMultipliers:       map[string]float64{"free": 1.30},
			DefaultUserMultiplier: 1.0,
			MinimumChargeCredits:  1,
		},
	})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"video_url":"https://cdn.example.com/ref.mp4"
		}`)},
	}
	result, err := analyzeVideoReferenceHandler(ctx, req)
	if err != nil {
		t.Fatalf("analyzeVideoReferenceHandler returned error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"credits_charged":225`) || !strings.Contains(text, `"total_tokens":11000`) {
		t.Fatalf("response missing token billing details: %s", text)
	}
	txs, err := repo.Credits().FindByTaskIDAndUserID(ctx, taskID, userID)
	if err != nil {
		t.Fatalf("find task transactions: %v", err)
	}
	if len(txs) != 1 || txs[0].Type != model.CreditTypeVideoUnderstanding || txs[0].Amount != -225 {
		t.Fatalf("transactions = %#v, want one video understanding deduction", txs)
	}
	var metadata model.CreditTransactionMetadata
	if err := json.Unmarshal(txs[0].Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.TotalTokens != 11_000 || metadata.BaseCredits != 173 || metadata.FinalCredits != 225 {
		t.Fatalf("metadata = %#v, want token cost snapshot", metadata)
	}
}

func TestAnalyzeVideoReferenceFailsFastWithoutSampledFrames(t *testing.T) {
	ctx, _, _, projectID := setupMCPVideoProject(t)
	vision := &fakeVideoVisionLLM{errors: []error{errors.New("native video URL unsupported")}}
	logger := zerolog.New(io.Discard)
	writingSvc := service.NewWritingService(nil, nil, "", time.Minute, &logger)
	writingSvc.SetVideoUnderstandingClient(vision)
	svcs.WritingSvc = writingSvc

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"video_url":"https://cdn.example.com/ref.mp4"
		}`)},
	}
	result, err := analyzeVideoReferenceHandler(ctx, req)
	if err != nil {
		t.Fatalf("analyzeVideoReferenceHandler returned error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "native video URL unsupported") {
		t.Fatalf("expected native video error, got: %s", text)
	}
	if strings.Contains(text, "sampled_frames") {
		t.Fatalf("response still mentions sampled frame fallback: %s", text)
	}
	if len(vision.calls) != 1 || vision.calls[0].kind != "video" {
		t.Fatalf("vision calls = %#v, want exactly one native video call", vision.calls)
	}
}

func TestAnalyzeVideoReferenceRejectsPrivateURL(t *testing.T) {
	ctx, _, _, projectID := setupMCPVideoProject(t)
	logger := zerolog.New(io.Discard)
	vision := &fakeVideoVisionLLM{}
	writingSvc := service.NewWritingService(nil, nil, "", time.Minute, &logger)
	writingSvc.SetVideoUnderstandingClient(vision)
	svcs.WritingSvc = writingSvc
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"video_url":"http://127.0.0.1/ref.mp4"
		}`)},
	}
	result, err := analyzeVideoReferenceHandler(ctx, req)
	if err != nil {
		t.Fatalf("analyzeVideoReferenceHandler returned error: %v", err)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "publicly accessible HTTPS") {
		t.Fatalf("expected public HTTPS error, got: %s", text)
	}
}

func TestAnalyzeVideoReferenceRequiresVisionService(t *testing.T) {
	ctx, _, _, projectID := setupMCPVideoProject(t)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"video_url":"https://cdn.example.com/ref.mp4"
		}`)},
	}
	result, err := analyzeVideoReferenceHandler(ctx, req)
	if err != nil {
		t.Fatalf("analyzeVideoReferenceHandler returned error: %v", err)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "writing/vision service not available") {
		t.Fatalf("expected vision service error, got: %s", text)
	}
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

func TestRegisterVideoReferencePreservesSavedVideoDuration(t *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":"p1",
			"type":"video_url",
			"url":"https://cdn.example.com/uploads/video-references/u/input.mp4",
			"reference_role":"motion_reference",
			"input_duration_seconds":7.25
		}`)},
	}
	result, err := registerVideoReferenceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("registerVideoReferenceHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"input_duration_seconds":7.25`) || !strings.Contains(text, `"reference_role":"motion_reference"`) {
		t.Fatalf("registered reference did not preserve saved metadata: %s", text)
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
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"project_id":` + strconv.Quote(projectID) + `,"task_id":` + strconv.Quote(taskID) + `,"prompt":"生成视频","purpose":"unknown"}`)},
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

func TestGetProjectVideoProfileFiltersUnconfiguredPolicyModels(t *testing.T) {
	oldSvcs := svcs
	oldBill := billSvc
	t.Cleanup(func() {
		svcs = oldSvcs
		billSvc = oldBill
	})
	ctx, repo, userID, projectID := setupMCPVideoProject(t)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0", "missing-model"},
		DefaultModel:  "missing-model",
		MaxResolution: "1080p",
		MaxDuration:   15,
	})
	project.SetVideoDefaults(model.VideoDefaults{
		ModelKey:   "missing-model",
		Resolution: "1080p",
		Ratio:      "9:16",
		Duration:   15,
	})
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}
	SetBillingServices(nil, nil, &config.Config{VideoAPI: config.VideoAPIConfig{ModelCatalog: []config.VideoModelCatalogEntry{{
		Key:                  "seedance-2.0",
		DisplayName:          "Configured Seedance",
		ModelID:              "doubao-seedance-2-0-260128",
		SupportedResolutions: []string{"1080p"},
		SupportedRatios:      []string{"9:16"},
		MinDuration:          1,
		MaxDuration:          15,
	}}}})

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{"project_id": projectID})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	text := string(encoded)
	if strings.Contains(text, "missing-model") {
		t.Fatalf("profile exposed unconfigured model: %s", text)
	}
	if !strings.Contains(text, `"allowed_models":["seedance-2.0"]`) {
		t.Fatalf("profile did not preserve configured allowed model: %s", text)
	}
}

func TestBuildAccountInfoVideoProjectReturnsResolvedVideoBlock(t *testing.T) {
	old := svcs
	oldBill := billSvc
	t.Cleanup(func() {
		svcs = old
		billSvc = oldBill
	})
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	task.Prompt = "生成一条咖啡杯种草视频"
	task.SetProjectSnapshot(model.ProjectSnapshot{
		ProjectName:  "快照视频项目",
		Platform:     model.PlatformVideo,
		Instructions: "面向露营人群的咖啡杯项目",
		Keywords:     "咖啡杯,露营,便携",
		VisualStyle:  "自然光、真实手持，禁止卡通化",
	})
	task.SetVideoConfig(model.VideoTaskConfig{
		Purpose:    service.VideoPurposePlanting,
		ModelKey:   "seedance-2.0",
		Resolution: "1080p",
		Ratio:      "9:16",
		Duration:   15,
		References: []model.VideoReferenceAsset{{
			Type:          "image_url",
			URL:           "https://cdn.example.com/cup.png",
			ReferenceRole: "product appearance",
			FileName:      "cup.png",
		}},
	})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": projectID,
		"task_id":    taskID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	resolved, ok := info["resolved_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resolved_profile missing or wrong type: %T", info["resolved_profile"])
	}
	if got := resolved["name"]; got != "快照视频项目" {
		t.Fatalf("resolved_profile.name = %v, want snapshot project name", got)
	}
	if got := resolved["uses_project_snapshot"]; got != true {
		t.Fatalf("uses_project_snapshot = %v, want true", got)
	}
	if got := resolved["creative_constraints"]; got != "自然光、真实手持，禁止卡通化" {
		t.Fatalf("creative_constraints = %v, want snapshot visual style", got)
	}
	video, ok := info["video"].(map[string]any)
	if !ok {
		t.Fatalf("video block missing or wrong type: %T", info["video"])
	}
	defaults := video["defaults"].(map[string]any)
	if got := defaults["model_key"]; got != "seedance-2.0" {
		t.Fatalf("video.defaults.model_key = %v, want seedance-2.0", got)
	}
	policy := video["policy"].(map[string]any)
	if got := policy["default_model"]; got != "seedance-2.0" {
		t.Fatalf("video.policy.default_model = %v, want seedance-2.0", got)
	}
	refs := video["references"].([]model.VideoReferenceAsset)
	if len(refs) != 1 || refs[0].ReferenceRole != "product appearance" {
		t.Fatalf("video.references = %#v, want task video references", refs)
	}
	anchors, ok := video["visual_anchor_generation"].(map[string]any)
	if !ok {
		t.Fatalf("video.visual_anchor_generation missing or wrong type: %T", video["visual_anchor_generation"])
	}
	if got := anchors["available"]; got != true {
		t.Fatalf("video.visual_anchor_generation.available = %v, want true", got)
	}
	if got := anchors["default_image_type"]; got != "content" {
		t.Fatalf("video.visual_anchor_generation.default_image_type = %v, want content", got)
	}
	if got := anchors["max_auto_anchors"]; got != 3 {
		t.Fatalf("video.visual_anchor_generation.max_auto_anchors = %v, want 3", got)
	}
	for _, field := range []string{"verify_with_vision", "register_tool", "fallback"} {
		value, _ := anchors[field].(string)
		if strings.TrimSpace(value) == "" {
			t.Fatalf("video.visual_anchor_generation.%s is empty: %#v", field, anchors[field])
		}
	}
	pricing := video["pricing"].(map[string]any)
	if got := pricing["min_balance"]; got != service.MinVideoCreationBalance {
		t.Fatalf("video.pricing.min_balance = %v, want %d", got, service.MinVideoCreationBalance)
	}
	brief, ok := info["agent_brief"].(string)
	if !ok || !strings.Contains(brief, "快照视频项目") || !strings.Contains(brief, "自然光、真实手持") || !strings.Contains(brief, "seedance-2.0") {
		t.Fatalf("agent_brief missing video project context: %#v", info["agent_brief"])
	}
}

func TestBuildVideoGenerationPlanHandlerReturnsPayloadPreview(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
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

func TestBuildVideoPlanAllowsTaskSavedVideoReferenceDuration(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/tasks/input.mp4"}
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, store)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	task.SetVideoConfig(model.VideoTaskConfig{
		References: []model.VideoReferenceAsset{{
			Type:                 service.VideoReferenceVideo,
			URL:                  "https://cdn.example.com/uploads/video-references/u/input.mp4",
			ReferenceRole:        "motion_reference",
			InputDurationSeconds: 7.25,
		}},
	})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task: %v", err)
	}
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
			"prompt":"基于已有动作参考生成一条种草视频",
			"references":[{
				"type":"video_url",
				"url":"https://cdn.example.com/uploads/video-references/u/input.mp4",
				"reference_role":"motion_reference",
				"input_duration_seconds":7.25
			}]
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
	if !strings.Contains(text, `"input_video":true`) || !strings.Contains(text, `"input_seconds":7.25`) {
		t.Fatalf("pricing did not include saved video input duration: %s", text)
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
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	svcs.VideoSvc = service.NewVideoService(&config.VideoAPIConfig{
		Key:     "test-key",
		BaseURL: "https://example.com",
	})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
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
	taskID := createMCPVideoTaskWithConfig(t, repo, userID, projectID, model.VideoTaskConfig{
		ModelKey: "seedance-2.0-mini",
	})
	arkSrv := newArkTaskServer(t)
	defer arkSrv.Close()
	svcs.VideoSvc = service.NewVideoService(&config.VideoAPIConfig{Key: "test-key", BaseURL: arkSrv.URL, Timeout: time.Second})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
				"task_id":` + strconv.Quote(taskID) + `,
				"prompt":"生成一条 5 秒产品推广视频",
				"purpose":"promotion",
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
	resolved := gen.ResolvedParams.Data()
	if resolved.ModelKey != "seedance-2.0-mini" || resolved.Model != modelIDForVideoKey(t, "seedance-2.0-mini") {
		t.Fatalf("generation model = %s/%s, want task-bound seedance-2.0-mini/%s", resolved.ModelKey, resolved.Model, modelIDForVideoKey(t, "seedance-2.0-mini"))
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
	if !strings.Contains(string(updated.TaskFileIDs), "final_video") {
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

func TestValidateVideoDeliveryRequiresExistingFinalVideoTaskFile(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	gen := &model.VideoGeneration{
		UserID:      userID,
		ProjectID:   projectID,
		TaskID:      taskID,
		Status:      "archived",
		TaskFileIDs: datatypes.JSON([]byte(`{"final_video":"missing-file-id"}`)),
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		t.Fatalf("create generation: %v", err)
	}

	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
		"project_id":` + strconv.Quote(projectID) + `,
		"task_id":` + strconv.Quote(taskID) + `,
		"video_generation_id":` + strconv.Quote(gen.ID) + `
	}`)}}
	result, err := validateVideoDeliveryHandler(ctx, req)
	if err != nil {
		t.Fatalf("validateVideoDeliveryHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected validate error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"valid":false`) || !strings.Contains(text, "final_video task file is not registered") {
		t.Fatalf("validate result = %s, want missing registered file", text)
	}
}

func TestDownloadVideoGenerationResultsRejectsInvalidSegmentShape(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	gen := &model.VideoGeneration{
		UserID:    userID,
		ProjectID: projectID,
		TaskID:    taskID,
		Status:    "succeeded",
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if err := repo.VideoGenerations().CreateSegment(ctx, &model.VideoGenerationSegment{
		VideoGenerationID: gen.ID,
		UserID:            userID,
		ProjectID:         projectID,
		TaskID:            taskID,
		Index:             1,
		Status:            "succeeded",
		Duration:          15,
	}); err != nil {
		t.Fatalf("create segment: %v", err)
	}

	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
		"project_id":` + strconv.Quote(projectID) + `,
		"task_id":` + strconv.Quote(taskID) + `,
		"video_generation_id":` + strconv.Quote(gen.ID) + `,
		"segments":["not-an-object"]
	}`)}}
	result, err := downloadVideoGenerationResultsHandler(ctx, req)
	if err != nil {
		t.Fatalf("downloadVideoGenerationResultsHandler returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected invalid segment shape to fail")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "segments[0] must be an object") {
		t.Fatalf("unexpected error text: %q", text)
	}
}

func TestDownloadVideoGenerationResultsDoesNotMarkPartialMultiSegmentAsFinal(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	store := &fakeVideoReferenceStorage{url: "https://oss.example.com/tasks/segment.mp4"}
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, store)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	gen := &model.VideoGeneration{
		UserID:    userID,
		ProjectID: projectID,
		TaskID:    taskID,
		Status:    "succeeded",
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		t.Fatalf("create generation: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if err := repo.VideoGenerations().CreateSegment(ctx, &model.VideoGenerationSegment{
			VideoGenerationID: gen.ID,
			UserID:            userID,
			ProjectID:         projectID,
			TaskID:            taskID,
			Index:             i,
			Status:            "succeeded",
			Duration:          15,
		}); err != nil {
			t.Fatalf("create segment %d: %v", i, err)
		}
	}
	videoSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("fake-segment-video"))
	}))
	defer videoSrv.Close()
	oldClient := http.DefaultClient
	http.DefaultClient = videoSrv.Client()
	t.Cleanup(func() { http.DefaultClient = oldClient })

	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
		"project_id":` + strconv.Quote(projectID) + `,
		"task_id":` + strconv.Quote(taskID) + `,
		"video_generation_id":` + strconv.Quote(gen.ID) + `,
		"segments":[{"index":1,"video_url":` + strconv.Quote(videoSrv.URL+"/segment-01.mp4") + `}]
	}`)}}
	result, err := downloadVideoGenerationResultsHandler(ctx, req)
	if err != nil {
		t.Fatalf("downloadVideoGenerationResultsHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected download error: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	updated, err := repo.VideoGenerations().FindByID(ctx, gen.ID)
	if err != nil {
		t.Fatalf("find generation: %v", err)
	}
	if strings.Contains(string(updated.TaskFileIDs), "final_video") {
		t.Fatalf("partial multi-segment download must not set final_video: %s", string(updated.TaskFileIDs))
	}
	if updated.Status == "archived" {
		t.Fatalf("partial multi-segment download must not archive job before compose")
	}
}

func TestQueryVideoGenerationJobDoesNotCompleteWhenProviderQueryFails(t *testing.T) {
	old := svcs
	t.Cleanup(func() { svcs = old })
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	gen := &model.VideoGeneration{
		UserID:    userID,
		ProjectID: projectID,
		TaskID:    taskID,
		Status:    "submitted",
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if err := repo.VideoGenerations().CreateSegment(ctx, &model.VideoGenerationSegment{
		VideoGenerationID: gen.ID,
		UserID:            userID,
		ProjectID:         projectID,
		TaskID:            taskID,
		Index:             1,
		ArkTaskID:         "cgt-video-1",
		Status:            "succeeded",
		Duration:          15,
	}); err != nil {
		t.Fatalf("create segment: %v", err)
	}
	arkSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"temporary provider outage"}}`))
	}))
	defer arkSrv.Close()
	svcs.VideoSvc = service.NewVideoService(&config.VideoAPIConfig{Key: "test-key", BaseURL: arkSrv.URL, Timeout: time.Second})

	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
		"project_id":` + strconv.Quote(projectID) + `,
		"task_id":` + strconv.Quote(taskID) + `,
		"video_generation_id":` + strconv.Quote(gen.ID) + `
	}`)}}
	result, err := queryVideoGenerationJobHandler(ctx, req)
	if err != nil {
		t.Fatalf("queryVideoGenerationJobHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected query error result: %s", result.Content[0].(*mcp.TextContent).Text)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "query_error") {
		t.Fatalf("query response missing query_error: %s", text)
	}
	updated, err := repo.VideoGenerations().FindByID(ctx, gen.ID)
	if err != nil {
		t.Fatalf("find generation: %v", err)
	}
	if updated.Status == "succeeded" {
		t.Fatalf("provider query failure must not complete job: %#v", updated)
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
	ctx, repo, userID, projectID := setupMCPVideoProjectWithServices(t, nil)
	taskID := createMCPVideoTask(t, repo, userID, projectID)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":` + strconv.Quote(projectID) + `,
			"task_id":` + strconv.Quote(taskID) + `,
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
