package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type noopTaskEnqueuer struct{}

func (noopTaskEnqueuer) Enqueue(string, []byte) error {
	return nil
}

func (noopTaskEnqueuer) EnqueueIn(string, []byte, time.Duration) error {
	return nil
}

func setupTaskHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func TestDownloadZipBlocksPaymentRequiredTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	taskID := uuid.New().String()
	fileID := uuid.New().String()
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	upload, err := store.Upload(ctx, "tasks/"+taskID+"/article.html", strings.NewReader("<main>ok</main>"), "text/html")
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "billlock",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: uuid.New().String(),
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.Tasks().UpdateBillingStatus(ctx, taskID, model.TaskBillingStatusPaymentRequired, 3200); err != nil {
		t.Fatalf("set billing status: %v", err)
	}
	locked, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if locked.BillingStatus != model.TaskBillingStatusPaymentRequired || locked.BillingShortfallCredits != 3200 {
		t.Fatalf("reloaded billing = %q/%d", locked.BillingStatus, locked.BillingShortfallCredits)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:              fileID,
		TaskID:          taskID,
		Role:            model.FileRoleHTML,
		FilePath:        "article.html",
		FileName:        "article.html",
		MimeType:        "text/html",
		FileSize:        upload.Size,
		OSSKey:          upload.Key,
		OSSURL:          upload.URL,
		StorageProvider: store.Name(),
	}); err != nil {
		t.Fatalf("create task file: %v", err)
	}

	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, nil, store, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files/zip", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.DownloadZip(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files/zip", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusPaymentRequired {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 402 body=%s", resp.StatusCode, data)
	}
}

func TestGetFilesRedactsDeliveryURLsForPaymentRequiredTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	taskID := uuid.New().String()
	fileID := uuid.New().String()
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	upload, err := store.Upload(ctx, "tasks/"+taskID+"/output/image_01.png", strings.NewReader("png"), "image/png")
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "fileslock",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: uuid.New().String(),
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:        fileID,
		TaskID:    taskID,
		Role:      model.FileRoleImage,
		FilePath:  "output/image_01.png",
		FileName:  "image_01.png",
		MimeType:  "image/png",
		FileSize:  upload.Size,
		OSSKey:    upload.Key,
		OSSURL:    upload.URL,
		MediaID:   "wechat-media-1",
		WechatURL: "https://mmbiz.qpic.cn/wechat-media-1",
	}); err != nil {
		t.Fatalf("create task file: %v", err)
	}
	if err := repo.Tasks().UpdateBillingStatus(ctx, taskID, model.TaskBillingStatusPaymentRequired, 3200); err != nil {
		t.Fatalf("set billing status: %v", err)
	}

	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, nil, store, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetFiles(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, data)
	}
	var body struct {
		Code int              `json:"code"`
		Msg  string           `json:"msg"`
		Data []model.TaskFile `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("files = %d, want 1", len(body.Data))
	}
	got := body.Data[0]
	if got.ID != fileID || got.FileName != "image_01.png" || got.MimeType != "image/png" || got.FileSize != upload.Size {
		t.Fatalf("metadata = %+v, want file metadata preserved", got)
	}
	if got.URL != "" || got.MediaID != "" || got.WechatURL != "" {
		t.Fatalf("delivery fields = url %q media_id %q wechat_url %q, want redacted", got.URL, got.MediaID, got.WechatURL)
	}
}

func TestVideoProductionBlocksPaymentRequiredTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	taskID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "videolock",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: uuid.New().String(),
		Type:      model.PlatformVideo,
		Status:    model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.Tasks().UpdateBillingStatus(ctx, taskID, model.TaskBillingStatusPaymentRequired, 800); err != nil {
		t.Fatalf("set billing status: %v", err)
	}

	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/video-production", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetVideoProduction(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/video-production", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusPaymentRequired {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 402 body=%s", resp.StatusCode, data)
	}
}

func TestTaskCreatePromptLengthLimit(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "prompt-limit@example.com",
		Password:   "hashed",
		InviteCode: "promptlimit",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	handler := NewTaskHandler(taskSvc, &logger)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handler.Create(c)
	})

	tests := []struct {
		name       string
		prompt     string
		wantStatus int
	}{
		{"allows 5120 characters", strings.Repeat("a", 5120), fiber.StatusOK},
		{"rejects more than 5120 characters", strings.Repeat("a", 5121), fiber.StatusBadRequest},
		{"counts unicode characters", strings.Repeat("汉", 5120), fiber.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"project_id":"` + projectID + `","prompt":"` + tt.prompt + `"}`
			req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// TestCreateTask_ImageModelKeyTierForbidden verifies that the handler returns
// 403 when a Free-tier user tries to create a task with a Pro-tier image preset,
// or with "custom" (Enterprise-only). Also covers the fail-closed path:
// Free users CAN still create tasks with Free-tier or empty keys.
func TestCreateTask_ImageModelKeyTierForbidden(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "tier-forbidden@example.com",
		Password:   "hashed",
		InviteCode: "tierforbidden",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	presets := []config.ImageModelPreset{
		{Key: "volcengine-standard", Provider: "volcengine", Model: "doubao-seedream", MinTier: "free"},
		{Key: "gemini-pro", Provider: "gemini", Model: "gemini-3-pro", MinTier: "pro"},
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetImagePresets(presets)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "free tier + free preset accepted",
			body:       `{"project_id":"` + projectID + `","image_model_key":"volcengine-standard"}`,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "free tier + empty key accepted",
			body:       `{"project_id":"` + projectID + `"}`,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "free tier + pro preset rejected",
			body:       `{"project_id":"` + projectID + `","image_model_key":"gemini-pro"}`,
			wantStatus: fiber.StatusForbidden,
		},
		{
			name:       "free tier + custom rejected",
			body:       `{"project_id":"` + projectID + `","image_model_key":"custom"}`,
			wantStatus: fiber.StatusForbidden,
		},
		{
			name:       "free tier + unknown key rejected",
			body:       `{"project_id":"` + projectID + `","image_model_key":"made-up"}`,
			wantStatus: fiber.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/tasks", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// TestCreateTask_ArticleImageTogglesPersist verifies the full
// handler→service→model→DB round-trip persists an explicit `false` for both
// article image toggles. This is a regression guard for the *bool /
// gorm:"default:true" mitigation: a plain bool with default:true silently
// coerces false→true at Create time (in-memory AND DB). It also guards against
// a handler wiring omission (forgetting to pass req.ArticleWithCover into
// CreateManualParams would silently default the user's choice to true). The
// value is re-read from the repo to assert the persisted state, not just the
// in-memory response.
func TestCreateTask_ArticleImageTogglesPersist(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "article-toggles@example.com",
		Password:   "hashed",
		InviteCode: "articletoggles",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"project_id":"` + projectID + `","prompt":"文章开关持久化测试","article_with_cover":false,"article_with_content_images":false}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil {
		t.Fatalf("find tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	for label, ptr := range map[string]*bool{
		"ArticleWithCover":         got.ArticleWithCover,
		"ArticleWithContentImages": got.ArticleWithContentImages,
	} {
		switch {
		case ptr == nil:
			t.Errorf("%s = nil, want non-nil false", label)
		case *ptr:
			t.Errorf("%s = true, want false", label)
		}
	}
}

func TestCloneTask_AllowsCompletedTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "clone-completed@example.com",
		Password:   "hashed",
		InviteCode: "clonecompleted",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.New().String()
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
		Prompt:    "clone this completed task",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Clone(c)
	})

	req := httptest.NewRequest("POST", "/tasks/"+taskID+"/clone", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ID == "" || env.Data.ID == taskID {
		t.Fatalf("new task id = %q, want fresh id", env.Data.ID)
	}
	if env.Data.Status != model.TaskStatusPending {
		t.Fatalf("new task status = %q, want pending", env.Data.Status)
	}
}

func TestResumeTask_ReusesCurrentTaskAndAcceptsPromptFilesAndLabels(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "resume@example.com",
		Password:   "hashed",
		InviteCode: "resume",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.New().String()
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
		Prompt:    "failed task",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, taskID), 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, workspaceRoot, nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("prompt", "继续写结论"); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := writer.WriteField("file_labels", `["修改意见"]`); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	part, err := writer.CreateFormFile("files", "notes.md")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := part.Write([]byte("# notes")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/resume", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ID != taskID || env.Data.Status != model.TaskStatusPending {
		t.Fatalf("resume response = id %q status %q, want same pending task", env.Data.ID, env.Data.Status)
	}
	latest, err := os.ReadFile(filepath.Join(workspaceRoot, taskID, ".anban-creator", "resume", "latest.md"))
	if err != nil {
		t.Fatalf("read latest.md: %v", err)
	}
	if text := string(latest); !strings.Contains(text, "继续写结论") || !strings.Contains(text, "修改意见") || !strings.Contains(text, "notes.md") {
		t.Fatalf("latest.md missing resume input:\n%s", text)
	}
}

func TestResumeTask_RejectsEmptyInput(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "resume-empty@example.com",
		Password:   "hashed",
		InviteCode: "resumeempty",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.New().String()
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, taskID), 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, workspaceRoot, nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/resume", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateTask_VideoMinimumBalanceReturnsHelpfulMessage(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:             userID,
		Email:          "video-task-balance@example.com",
		Password:       "hashed",
		InviteCode:     "videotaskbalance",
		CreditsBalance: 99_999,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "Video",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    service.VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Watermark:  &watermark,
		Preflight:  true,
	})
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	creditSvc := service.NewCreditService(repo, &config.CreditsConfig{
		TaskCosts: map[string]int{"video": 2000},
	}, &logger)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, creditSvc, &logger, "", nil, "", nil, nil)
	taskSvc.SetVideoCatalogAndCreditMultiplier(service.DefaultVideoModelCatalog(), 1000)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(`{"project_id":"`+projectID+`","prompt":"生成视频"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	bal, err := creditSvc.GetBalance(ctx, userID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if bal != 99_999-2000 {
		t.Fatalf("balance = %d, want base fee %d", bal, 99_999-2000)
	}
}

func TestGetTaskByIDIncludesCreditsCharged(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	taskID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "task-credits@example.com",
		Password:   "hashed",
		InviteCode: "taskcredits",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
		Prompt:    "详情扣分展示",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.Credits().CreateTransaction(ctx, &model.CreditTransaction{
		UserID:       userID,
		Type:         model.CreditTypeTaskDeduct,
		Amount:       -120,
		BalanceAfter: 880,
		TaskID:       &taskID,
		Description:  "任务扣除 -120",
	}); err != nil {
		t.Fatalf("create credit transaction: %v", err)
	}
	if err := repo.Credits().CreateTransaction(ctx, &model.CreditTransaction{
		UserID:       userID,
		Type:         model.CreditTypeImageGen,
		Amount:       -80,
		BalanceAfter: 800,
		TaskID:       &taskID,
		Description:  "操作扣费 (image_gen) -80",
	}); err != nil {
		t.Fatalf("create operation transaction: %v", err)
	}
	if err := repo.Credits().CreateTransaction(ctx, &model.CreditTransaction{
		UserID:       userID,
		Type:         model.CreditTypeTaskRefund,
		Amount:       20,
		BalanceAfter: 820,
		TaskID:       &taskID,
		Description:  "任务取消退还 +20",
	}); err != nil {
		t.Fatalf("create refund transaction: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Get("/tasks/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetByID(c)
	})

	req := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			ID             string `json:"id"`
			CreditsCharged int    `json:"credits_charged"`
			CreditsSummary struct {
				TaskConsumed      int `json:"task_consumed"`
				OperationConsumed int `json:"operation_consumed"`
				Refunded          int `json:"refunded"`
				NetConsumed       int `json:"net_consumed"`
			} `json:"credits_summary"`
			CreditTransactions []model.CreditTransaction `json:"credit_transactions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ID != taskID {
		t.Fatalf("id = %q, want %q", body.Data.ID, taskID)
	}
	if body.Data.CreditsCharged != 120 {
		t.Fatalf("credits_charged = %d, want 120", body.Data.CreditsCharged)
	}
	if body.Data.CreditsSummary.TaskConsumed != 120 ||
		body.Data.CreditsSummary.OperationConsumed != 80 ||
		body.Data.CreditsSummary.Refunded != 20 ||
		body.Data.CreditsSummary.NetConsumed != 180 {
		t.Fatalf("credits_summary = %+v, want task=120 operation=80 refunded=20 net=180", body.Data.CreditsSummary)
	}
	if len(body.Data.CreditTransactions) != 3 {
		t.Fatalf("credit_transactions len = %d, want 3", len(body.Data.CreditTransactions))
	}
}
