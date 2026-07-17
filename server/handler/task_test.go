package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func (noopTaskEnqueuer) EnqueueUnique(string, []byte, string) (bool, error) {
	return true, nil
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

func seedPendingHandlerUpload(t *testing.T, repo repository.Repository, userID, uploadID, purpose, filename, contentType string) string {
	t.Helper()
	key := "uploads/pending/" + userID + "/" + uploadID + "/" + filename
	publicURL := "https://cdn.example.com/" + key
	if err := repo.PendingUploads().CreatePendingUpload(t.Context(), &model.PendingUpload{
		ID: uploadID, UserID: userID, Purpose: purpose, Key: key, PublicURL: publicURL,
		FileName: filename, ContentType: contentType, Size: 1,
		Status: model.PendingUploadStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed pending upload %s: %v", uploadID, err)
	}
	return publicURL
}

func postJSON(t *testing.T, app *fiber.App, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func decodeEnvelopeRawData(t *testing.T, resp *http.Response) map[string]json.RawMessage {
	t.Helper()
	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data == nil {
		t.Fatalf("response data is nil")
	}
	return env.Data
}

func mustMarshalTaskJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return raw
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

func TestGetFilesReturnsPublishedAndCollectedFiles(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, taskID := uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "visiblefiles"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: uuid.NewString(), Type: model.PlatformSeednote, Status: model.TaskStatusFailed}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: "successful", State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md"},
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateCollected, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json"},
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewTaskHandler(service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil), &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetFiles(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Data []model.TaskFile `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Data[0].State != model.TaskFileStatePublished || body.Data[1].State != model.TaskFileStateCollected {
		t.Fatalf("files = %#v", body.Data)
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
		Type:      model.PlatformVideoCreator,
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

func TestCreateTaskMontageReturnsSingleTaskWhenQuantityIsClamped(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "omhandler",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformMontage,
		Name:     "Montage",
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

	resp := postJSON(t, app, "/tasks", `{
		"project_id": "`+projectID+`",
		"quantity": 3,
		"montage_input": {
			"brief": "做一条新品发布短片",
			"pipeline_key": "default"
		}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	data := decodeEnvelopeRawData(t, resp)
	if _, ok := data["id"]; !ok {
		t.Fatalf("response data should be a single task object, got keys %#v", data)
	}
	if _, ok := data["montage_input"]; !ok {
		t.Fatalf("response missing montage_input: keys %#v", data)
	}
}

func TestCreateTaskEcommerceKeepsArrayResponseWhenRequestQuantityExceedsOne(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "ecommercearray",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformEcommerce,
		Name:     "Ecommerce",
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

	resp := postJSON(t, app, "/tasks", `{
		"project_id": "`+projectID+`",
		"quantity": 3,
		"selected_modules": {"main_images": 1},
		"product_photos": ["https://cdn.example.com/cup.png"]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var env struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode array response: %v", err)
	}
	if len(env.Data) != 1 {
		t.Fatalf("response data len = %d, want one clamped ecommerce task", len(env.Data))
	}
}

func TestCreateTaskPersistsFinalReferenceAndEcommercePhotoURLs(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "finalecomref"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformEcommerce, Name: "Ecommerce", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	refID, photoID := uuid.NewString(), uuid.NewString()
	refURL := seedPendingHandlerUpload(t, repo, userID, refID, service.DirectUploadPurposeTaskReference, "reference.png", "image/png")
	photoURL := seedPendingHandlerUpload(t, repo, userID, photoID, service.DirectUploadPurposeEcommercePhoto, "product.png", "image/png")
	store := pendingUploadStatStore(repo.PendingUploads())
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{"project_id":"`+projectID+`","reference_image_url":"`+refURL+`","product_photos":["`+photoURL+`"],"selected_modules":{"main_images":1}}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if bytes.Contains(body, []byte("uploads/pending/")) || !bytes.Contains(body, []byte("uploads/finalized/")) {
		t.Fatalf("task response contains non-final upload URL: %s", body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("persisted tasks = %#v, %v", tasks, err)
	}
	wantRef := "/api/v1/files/uploads/finalized/" + userID + "/" + refID + "/reference.png"
	wantPhoto := "/api/v1/files/uploads/finalized/" + userID + "/" + photoID + "/product.png"
	photos := tasks[0].Ecommerce.Data().ProductPhotos
	if tasks[0].ReferenceImageURL != wantRef || len(photos) != 1 || photos[0] != wantPhoto {
		t.Fatalf("persisted reference/photos = %q/%#v", tasks[0].ReferenceImageURL, photos)
	}
	for id, wantKey := range map[string]string{
		refID:   "uploads/finalized/" + userID + "/" + refID + "/reference.png",
		photoID: "uploads/finalized/" + userID + "/" + photoID + "/product.png",
	} {
		upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, id)
		if err != nil || upload.Status != model.PendingUploadStatusFinalized || upload.FinalizedKey != wantKey {
			t.Fatalf("finalized upload %s = %#v, %v", id, upload, err)
		}
	}
}

func TestCreateVideoTaskPersistsFinalReferenceURL(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID, uploadID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "finalvideoref"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformVideoCreator, Name: "Video", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	refURL := seedPendingHandlerUpload(t, repo, userID, uploadID, service.DirectUploadPurposeVideoReference, "reference.mp4", "video/mp4")
	store := pendingUploadStatStore(repo.PendingUploads())
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{"project_id":"`+projectID+`","video_creator_input":{"brief":"生成产品视频","references":[{"type":"video_url","url":"`+refURL+`"}]}}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if bytes.Contains(body, []byte("uploads/pending/")) || !bytes.Contains(body, []byte("uploads/finalized/")) {
		t.Fatalf("video task response contains non-final reference: %s", body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("persisted tasks = %#v, %v", tasks, err)
	}
	references := tasks[0].VideoInput.Data().References
	wantURL := "/api/v1/files/uploads/finalized/" + userID + "/" + uploadID + "/reference.mp4"
	if len(references) != 1 || references[0].URL != wantURL {
		t.Fatalf("persisted video references = %#v", references)
	}
	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil || upload.Status != model.PendingUploadStatusFinalized || upload.FinalizedKey != "uploads/finalized/"+userID+"/"+uploadID+"/reference.mp4" {
		t.Fatalf("finalized video upload = %#v, %v", upload, err)
	}
}

func TestCreateTaskMontageFinalizesSourceAssetUploads(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := uuid.New().String()
	assetURL := "https://cdn.example.com/uploads/pending/" + userID + "/" + uploadID + "/clip.mp4"
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "omasset",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformMontage,
		Name:     "Montage",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeMontageAsset,
		Key:         "uploads/pending/" + userID + "/" + uploadID + "/clip.mp4",
		PublicURL:   assetURL,
		FileName:    "clip.mp4",
		ContentType: "video/mp4",
		Size:        1234,
		Status:      model.PendingUploadStatusPending,
		ExpiresAt:   time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("create pending upload: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, pendingUploadStatStore(repo.PendingUploads()), nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{
		"project_id": "`+projectID+`",
		"montage_input": {
			"brief": "剪成一条发布会短片",
			"source_assets": [{"type": "video", "url": "`+assetURL+`"}]
		}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if bytes.Contains(responseBody, []byte("uploads/pending/")) || !bytes.Contains(responseBody, []byte("uploads/finalized/")) {
		t.Fatalf("task persisted non-final montage URL: %s", responseBody)
	}
	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil {
		t.Fatalf("find pending upload: %v", err)
	}
	if upload.Status != model.PendingUploadStatusFinalized || upload.FinalizedKey != "uploads/finalized/"+userID+"/"+uploadID+"/clip.mp4" {
		t.Fatalf("upload identity = %#v", upload)
	}
}

func TestCreateTaskRejectsMontageAssetOnOtherPlatformWithoutFinalizing(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := uuid.New().String()
	assetURL := "https://cdn.example.com/uploads/pending/" + userID + "/" + uploadID + "/clip.mp4"
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "omwrong",
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
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeMontageAsset,
		Key:         "uploads/pending/" + userID + "/" + uploadID + "/clip.mp4",
		PublicURL:   assetURL,
		FileName:    "clip.mp4",
		ContentType: "video/mp4",
		Size:        1234,
		Status:      model.PendingUploadStatusPending,
		ExpiresAt:   time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("create pending upload: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{
		"project_id": "`+projectID+`",
		"montage_input": {
			"brief": "错误平台",
			"source_assets": [{"type": "video", "url": "`+assetURL+`"}]
		}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, body)
	}
	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil {
		t.Fatalf("find pending upload: %v", err)
	}
	if upload.Status != model.PendingUploadStatusPending {
		t.Fatalf("upload status = %q, want pending", upload.Status)
	}
}

func TestCreateTaskRejectsMalformedPendingReferenceWithoutCreatingTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "badpending",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote,
		Name: "Seednote", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})
	body := fmt.Sprintf(`{
		"project_id": %q,
		"prompt": "test",
		"reference_image_url": %q
	}`, projectID, "https://cdn.example.com/uploads%2Fpending%2F"+userID)
	resp := postJSON(t, app, "/tasks", body)
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusBadRequest || !bytes.Contains(responseBody, []byte("pending upload URL is invalid")) {
		t.Fatalf("status/body = %d/%s", resp.StatusCode, responseBody)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("tasks/error = %d/%v, want 0/nil", len(tasks), err)
	}
}

func TestCreateTaskAllowsExternalPendingLikeReferencePath(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "externalpending",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote,
		Name: "Seednote", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(&fakeStorageProvider{})
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})
	body := fmt.Sprintf(`{
		"project_id": %q,
		"prompt": "test",
		"reference_image_url": %q
	}`, projectID, "https://external.example.com/uploads/pending/user-1/external-id/ref.png")
	resp := postJSON(t, app, "/tasks", body)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 200", resp.StatusCode, responseBody)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks/error = %d/%v, want 1/nil", len(tasks), err)
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

func TestCloneTaskAcceptsFinalInputSnapshot(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "clone-snapshot@example.com", Password: "hashed", InviteCode: "clonesnapshot", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.NewString()
	source := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted, Prompt: "source prompt"}
	source.SetInputAttachments([]model.EntryAttachment{{Role: "brief", Text: "source attachment"}, {Role: model.EntryAttachmentRoleResumeLatest, Text: "old resume"}})
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create task: %v", err)
	}
	uploadID := "clone-upload"
	pendingKey := "uploads/pending/" + userID + "/" + uploadID + "/reference.png"
	finalKey := "uploads/finalized/" + userID + "/" + uploadID + "/reference.png"
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID: uploadID, UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment,
		Key: pendingKey, FileName: "reference.png", ContentType: "image/png", Size: 123,
		Status: model.PendingUploadStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create pending upload: %v", err)
	}
	store := pendingUploadStatStore(repo.PendingUploads())
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Clone(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/clone", `{
		"prompt":"  edited prompt  ",
		"input_attachments":[{
			"type":"image","upload_id":"`+uploadID+`","key":"`+pendingKey+`",
			"file_name":"forged.exe","content_type":"application/x-msdownload","size":999999,
			"url":"https://attacker.example/secret","instruction":"  keep logo  "
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}
	var envelope struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Prompt != "edited prompt" {
		t.Fatalf("prompt = %q", envelope.Data.Prompt)
	}
	got := envelope.Data.InputAttachments.Data()
	if len(got) != 1 || got[0].UploadID != uploadID || got[0].Key != finalKey || got[0].URL != "" || got[0].FileName != "reference.png" || got[0].ContentType != "image/png" || got[0].Size != 123 || got[0].Instruction != "keep logo" {
		t.Fatalf("clone attachments = %#v, want exact verified final snapshot", got)
	}
	found, err := repo.Tasks().FindByID(ctx, envelope.Data.ID)
	if err != nil {
		t.Fatalf("find clone: %v", err)
	}
	if stored := found.InputAttachments.Data(); len(stored) != 1 || stored[0].Key != finalKey || stored[0].URL != "" {
		t.Fatalf("stored clone attachments = %#v", stored)
	}

	emptyResp := postJSON(t, app, "/tasks/"+taskID+"/clone", `{
		"prompt":"",
		"input_attachments":[{
			"type":"image","upload_id":"`+uploadID+`","key":"`+pendingKey+`",
			"file_name":"forged.exe","content_type":"application/x-msdownload","size":999999
		}]
	}`)
	defer emptyResp.Body.Close()
	if emptyResp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(emptyResp.Body)
		t.Fatalf("empty prompt status = %d, body=%s", emptyResp.StatusCode, body)
	}
	var emptyEnvelope struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(emptyResp.Body).Decode(&emptyEnvelope); err != nil {
		t.Fatalf("decode empty prompt response: %v", err)
	}
	if emptyEnvelope.Data.Prompt != "" {
		t.Fatalf("empty prompt clone prompt = %q", emptyEnvelope.Data.Prompt)
	}
	if got := emptyEnvelope.Data.InputAttachments.Data(); len(got) != 1 || got[0].Key != finalKey || got[0].FileName != "reference.png" {
		t.Fatalf("empty prompt clone attachments = %#v", got)
	}
}

func TestCreateVideoEditorPromotesVerifiedPromptVideoToPersistedInputReference(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "prompt-video@example.com", Password: "hashed", InviteCode: "promptvideo", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformVideoEditor, Name: "Video Editor", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	uploadID := "video-upload"
	pendingKey := "uploads/pending/" + userID + "/" + uploadID + "/source.mp4"
	finalKey := "uploads/finalized/" + userID + "/" + uploadID + "/source.mp4"
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID: uploadID, UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment,
		Key: pendingKey, FileName: "source.mp4", ContentType: "video/mp4", Size: 321,
		Status: model.PendingUploadStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create pending upload: %v", err)
	}
	store := pendingUploadStatStore(repo.PendingUploads())
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{
		"project_id":"`+projectID+`",
		"prompt":"给源视频加字幕",
		"quantity":1,
		"input_attachments":[{
			"type":"video","upload_id":"`+uploadID+`","key":"`+pendingKey+`",
			"file_name":"forged.mov","content_type":"video/quicktime","size":999
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}
	var envelope struct {
		Data struct {
			ID               string           `json:"id"`
			VideoEditorInput model.VideoInput `json:"video_editor_input"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	refs := envelope.Data.VideoEditorInput.References
	if len(refs) != 1 || refs[0].Type != service.VideoReferenceVideo || refs[0].URL != finalKey || refs[0].FileName != "source.mp4" || refs[0].MimeType != "video/mp4" || refs[0].FileSize != 321 {
		t.Fatalf("response video input references = %#v", refs)
	}
	found, err := repo.Tasks().FindByID(ctx, envelope.Data.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	stored := found.VideoInput.Data().References
	if len(stored) != 1 || stored[0].URL != finalKey || strings.Contains(strings.ToLower(stored[0].URL), "signature=") {
		t.Fatalf("stored video input references = %#v", stored)
	}
}

func TestCloneTaskRejectsEmptyMontageBriefBeforeSideEffects(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "clone-montage@example.com", Password: "hashed", InviteCode: "clonemontage", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformMontage, Name: "Montage", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.NewString()
	source := &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformMontage,
		Status: model.TaskStatusCompleted, Prompt: "source prompt", ExecutionTarget: model.ExecutionTargetCloud,
	}
	source.SetMontageInput(model.MontageInput{Brief: "source montage brief", PipelineKey: "default"})
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create task: %v", err)
	}

	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Clone(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/clone", `{"prompt":"  \n\t "}`)
	defer resp.Body.Close()
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body.Msg, "montage task requires brief") {
		t.Fatalf("status=%d response=%#v; want montage validation error", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 1 || tasks[0].ID != taskID {
		t.Fatalf("invalid montage clone changed tasks: tasks=%#v err=%v", tasks, err)
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
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
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
	resumed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	var latest, file bool
	for _, attachment := range resumed.InputAttachments.Data() {
		switch attachment.Role {
		case model.EntryAttachmentRoleResumeLatest:
			latest = strings.Contains(attachment.Text, "继续写结论") && strings.Contains(attachment.Text, "修改意见") && strings.Contains(attachment.Text, "notes.md")
		case model.EntryAttachmentRoleResumeFile:
			file = attachment.Key != "" && attachment.FileName == "notes.md"
		}
	}
	if !latest || !file {
		t.Fatalf("resume attachments latest=%v file=%v: %#v", latest, file, resumed.InputAttachments.Data())
	}
}

func TestResumeTask_Returns503WhenFileStorageUnavailable(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "resume-storage@example.com", Password: "hashed", InviteCode: "resume-storage"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.NewString()
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "notes.md")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("notes")); err != nil {
		t.Fatalf("write form file: %v", err)
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
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var env Response
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Msg != "补充文件存储暂不可用，请稍后重试" {
		t.Fatalf("message = %q", env.Msg)
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
	taskSvc.SetNASResumeEnabled(true)
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
		Platform: model.PlatformVideoCreator,
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
		TaskCosts: map[string]int{model.PlatformVideoCreator: 2000},
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

func TestCreateTask_VideoSplitInputsAndResponseFields(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	creatorProjectID := uuid.New().String()
	editorProjectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "video-split-handler@example.com",
		Password:   "hashed",
		InviteCode: "videosplit",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, project := range []*model.Project{
		{ID: creatorProjectID, UserID: userID, Platform: model.PlatformVideoCreator, Name: "Creator", Status: model.ProjectStatusActive},
		{ID: editorProjectID, UserID: userID, Platform: model.PlatformVideoEditor, Name: "Editor", Status: model.ProjectStatusActive},
	} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project %s: %v", project.Platform, err)
		}
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	t.Run("creator accepts creator input and never exposes generic video fields", func(t *testing.T) {
		body := `{"project_id":"` + creatorProjectID + `","video_creator_input":{"brief":"生成产品视频","hard_constraints":{"ratio":"9:16"}}}`
		resp := postJSON(t, app, "/tasks", body)
		if resp.StatusCode != fiber.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, raw)
		}
		data := decodeEnvelopeRawData(t, resp)
		if _, ok := data["video_creator_input"]; !ok {
			t.Fatalf("response missing video_creator_input: %s", string(mustMarshalTaskJSON(t, data)))
		}
		if _, ok := data["video_input"]; ok {
			t.Fatalf("response exposed generic video_input: %s", string(mustMarshalTaskJSON(t, data)))
		}
		tasks, err := repo.Tasks().FindByUserID(ctx, userID, creatorProjectID, "", 0, 10)
		if err != nil {
			t.Fatalf("find creator tasks: %v", err)
		}
		if len(tasks) != 1 || tasks[0].VideoInput.Data().Brief != "生成产品视频" {
			t.Fatalf("stored creator video input = %#v", tasks)
		}
	})

	t.Run("editor accepts editor input and never exposes generic video fields", func(t *testing.T) {
		body := `{"project_id":"` + editorProjectID + `","prompt":"加字幕并剪成 30 秒","video_editor_input":{"brief":"加字幕并剪成 30 秒","references":[{"type":"video_url","url":"https://cdn.example.com/source.mp4"}]}}`
		resp := postJSON(t, app, "/tasks", body)
		if resp.StatusCode != fiber.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, raw)
		}
		data := decodeEnvelopeRawData(t, resp)
		if _, ok := data["video_editor_input"]; !ok {
			t.Fatalf("response missing video_editor_input: %s", string(mustMarshalTaskJSON(t, data)))
		}
		if _, ok := data["video_input"]; ok {
			t.Fatalf("response exposed generic video_input: %s", string(mustMarshalTaskJSON(t, data)))
		}
	})

	t.Run("legacy generic video fields are rejected", func(t *testing.T) {
		body := `{"project_id":"` + creatorProjectID + `","video_input":{"brief":"旧字段不允许"},"video_config":{"ratio":"9:16"}}`
		resp := postJSON(t, app, "/tasks", body)
		if resp.StatusCode != fiber.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, raw)
		}
	})

	t.Run("mismatched workflow input is rejected", func(t *testing.T) {
		body := `{"project_id":"` + editorProjectID + `","video_creator_input":{"brief":"生成而不是剪辑"}}`
		resp := postJSON(t, app, "/tasks", body)
		if resp.StatusCode != fiber.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, raw)
		}
	})
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

func setupSeednoteTaskCreateHandler(t *testing.T) (*fiber.App, repository.Repository, context.Context, string, string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "seedrefs",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote references",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	handler := NewTaskHandler(taskSvc, &logger)
	handler.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handler.Create(c)
	})
	return app, repo, ctx, userID, projectID
}

func TestCreateTaskAcceptsSeednoteInputAttachments(t *testing.T) {
	app, repo, ctx, _, projectID := setupSeednoteTaskCreateHandler(t)
	resp := postJSON(t, app, "/tasks", `{
		"project_id":"`+projectID+`",
		"prompt":"生成新品种草图文",
		"input_attachments":[{
			"type":"image",
			"url":"/api/v1/files/uploads/references/product.png",
			"file_name":" product.png ",
			"content_type":"image/png",
			"instruction":"  保持包装、Logo 和瓶盖颜色  "
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	data := decodeEnvelopeRawData(t, resp)
	var taskID string
	if err := json.Unmarshal(data["id"], &taskID); err != nil {
		t.Fatalf("decode task id: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 {
		t.Fatalf("input attachments = %#v, want one", attachments)
	}
	if attachments[0].URL != "/api/v1/files/uploads/references/product.png" || attachments[0].FileName != "product.png" || attachments[0].Instruction != "保持包装、Logo 和瓶盖颜色" {
		t.Fatalf("stored attachment = %#v", attachments[0])
	}
}

func TestCreateTaskAcceptsAllAgentAttachmentTypes(t *testing.T) {
	app, repo, ctx, _, projectID := setupSeednoteTaskCreateHandler(t)
	body := fmt.Sprintf(`{"project_id":%q,"prompt":"test","input_attachments":%s}`, projectID, fiveTypeHandlerAttachmentsJSON)
	resp := postJSON(t, app, "/tasks", body)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
	}
	data := decodeEnvelopeRawData(t, resp)
	var task model.Task
	if err := json.Unmarshal(data["id"], &task.ID); err != nil || task.ID == "" {
		t.Fatalf("decode task id: %v", err)
	}
	stored, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil || len(stored.InputAttachments.Data()) != 5 {
		t.Fatalf("stored attachments = %#v, err = %v", stored, err)
	}
	attachments := stored.InputAttachments.Data()
	if attachments[1].ContentType != "application/ogg" || attachments[4].ContentType != "application/csv" {
		t.Fatalf("stored canonical application MIME attachments = %#v", attachments)
	}
}

func TestCreateTaskRejectsInvalidAgentAttachmentsAtHandler(t *testing.T) {
	for _, tt := range handlerAttachmentRouteRejectionCases("foreign-upload", "uploads/pending/foreign/foreign-upload/product.png") {
		t.Run(tt.name, func(t *testing.T) {
			app, repo, ctx, userID, projectID := setupSeednoteTaskCreateHandler(t)
			if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
				ID: "foreign-upload", UserID: "foreign-user", Purpose: service.DirectUploadPurposeAIEntryAttachment,
				Key: "uploads/pending/foreign/foreign-upload/product.png", FileName: "product.png", ContentType: "image/png", Size: 10,
				Status: model.PendingUploadStatusPending, ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatalf("create foreign upload: %v", err)
			}
			body := fmt.Sprintf(`{"project_id":%q,"prompt":"test","input_attachments":%s}`, projectID, tt.attachments)
			resp := postJSON(t, app, "/tasks", body)
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusBadRequest {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 400: %s", resp.StatusCode, raw)
			}
			tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
			if err != nil || len(tasks) != 0 {
				t.Fatalf("rejected request persisted tasks = %#v, err = %v", tasks, err)
			}
		})
	}
}

func TestCreateTaskAcceptsNonImageSeednoteAttachment(t *testing.T) {
	app, repo, ctx, userID, projectID := setupSeednoteTaskCreateHandler(t)
	resp := postJSON(t, app, "/tasks", `{
		"project_id":"`+projectID+`",
		"prompt":"生成新品种草图文",
		"input_attachments":[{
			"type":"video",
			"url":"/api/v1/files/uploads/references/demo.mp4",
			"file_name":"demo.mp4",
			"content_type":"video/mp4"
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil {
		t.Fatalf("find tasks: %v", err)
	}
	if len(tasks) != 1 || len(tasks[0].InputAttachments.Data()) != 1 || tasks[0].InputAttachments.Data()[0].Type != "video" {
		t.Fatalf("tasks = %#v, want one video attachment", tasks)
	}
}
