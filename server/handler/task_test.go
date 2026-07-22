package handler

import (
	"context"
	"encoding/json"
	"io"
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

	"github.com/anbanai/anban-creator/server/billing"
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

func seedHandlerUploadSession(t *testing.T, repo repository.Repository, userID, uploadID, purpose, filename, contentType string) string {
	t.Helper()
	key := "uploads/pending/" + userID + "/" + uploadID + "/" + filename
	publicURL := "https://cdn.example.com/" + key
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID: uploadID, UserID: userID, Purpose: purpose, StagingKey: key,
		FileName: filename, ContentType: contentType, Size: 1,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session %s: %v", uploadID, err)
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

func TestDownloadAndPreviewRemainAvailableForCompletedTask(t *testing.T) {
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
		ID: taskID, UserID: userID, ProjectID: uuid.New().String(),
		Type: model.PlatformArticle, Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
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
	taskSvc := service.NewTaskService(repo, nil, nil, store, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files/zip", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.DownloadZip(c)
	})
	app.Get("/tasks/:id/preview", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.PreviewHTML(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files/zip", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("zip status = %d, want 200 body=%s", resp.StatusCode, data)
	}

	preview, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/preview", nil))
	if err != nil {
		t.Fatalf("preview request: %v", err)
	}
	defer preview.Body.Close()
	previewBody, _ := io.ReadAll(preview.Body)
	if preview.StatusCode != fiber.StatusOK || !strings.Contains(string(previewBody), "<main>ok</main>") {
		t.Fatalf("preview = status %d body=%s, want accessible HTML", preview.StatusCode, previewBody)
	}
}

func TestGetFilesPreservesDeliveryURLs(t *testing.T) {
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
		ID: taskID, UserID: userID, ProjectID: uuid.New().String(),
		Type: model.PlatformSeednote, Status: model.TaskStatusCompleted,
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
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, nil, store, &logger, "", nil, "", nil, nil)
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
	if got.URL == "" || got.MediaID != "wechat-media-1" || got.WechatURL != "https://mmbiz.qpic.cn/wechat-media-1" {
		t.Fatalf("delivery fields = url %q media_id %q wechat_url %q, want preserved", got.URL, got.MediaID, got.WechatURL)
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
	h := NewTaskHandler(service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil), &logger)
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

func TestMarkPublishedWorksForCompletedTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, taskID := uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "publishlegacy"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: uuid.NewString(), Type: model.PlatformArticle, Status: model.TaskStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewTaskHandler(service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil), &logger)
	app := fiber.New()
	app.Patch("/tasks/:id/published", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.MarkPublished(c)
	})

	req := httptest.NewRequest(http.MethodPatch, "/tasks/"+taskID+"/published", strings.NewReader(`{"published":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, data)
	}
	found, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || !found.Published {
		t.Fatalf("published task = %+v err=%v", found, err)
	}
}

func TestTaskHandlerProductionDoesNotReferenceLegacyPaymentRequiredColumns(t *testing.T) {
	raw, err := os.ReadFile("task.go")
	if err != nil {
		t.Fatalf("read task.go: %v", err)
	}
	for _, forbidden := range []string{
		"TaskBillingStatusPaymentRequired", "BillingShortfallCredits", "taskBillingLocked", "ensureTaskBillingUnlocked",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("task.go still contains legacy delivery gate %q", forbidden)
		}
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Clone(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/clone", `{
		"prompt":"edited exact clone prompt",
		"input_attachments":[{"type":"text","text":"edited exact attachment","file_name":"brief.txt"}]
	}`)
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
	if env.Data.ProjectID != projectID || env.Data.Type != model.PlatformArticle || env.Data.Prompt != "edited exact clone prompt" {
		t.Fatalf("exact clone destination/config = project %q type %q prompt %q", env.Data.ProjectID, env.Data.Type, env.Data.Prompt)
	}
	attachments := env.Data.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].Text != "edited exact attachment" {
		t.Fatalf("exact clone attachments = %#v", attachments)
	}
}

func TestCloneTask_FullEditableOverrides(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "editable-clone@example.com", Password: "hashed", InviteCode: "editableclone", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformSeednote, Name: "Source seednote", Status: model.ProjectStatusActive}
	destinationProject := &model.Project{
		ID:           uuid.NewString(),
		UserID:       userID,
		Platform:     model.PlatformArticle,
		Name:         "Destination article",
		Status:       model.ProjectStatusActive,
		Instructions: "current destination instructions",
		VisualStyle:  "current destination visual style",
	}
	for _, project := range []*model.Project{sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project: %v", err)
		}
	}
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: model.PlatformSeednote, Status: model.TaskStatusCompleted, Prompt: "source prompt"}
	source.SetProjectSnapshot(model.SnapshotProject(sourceProject))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create source task: %v", err)
	}
	referenceAsset := cutoverAsset(uuid.NewString(), userID, service.DirectUploadPurposeTaskReference, "clone-ref.png", "image/png")
	if err := repo.Assets().Create(ctx, referenceAsset); err != nil {
		t.Fatalf("create reference asset: %v", err)
	}

	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	h.SetImagePresets([]config.ImageModelPreset{{Key: "free-image", Provider: "test", Model: "image-v1", MinTier: "free"}})
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

	resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{
		"project_id":"`+destinationProject.ID+`",
		"quantity":2,
		"prompt":"edited full clone prompt",
		"image_ratio":"1:1",
		"image_model_key":"free-image",
		"skip_reference_image":true,
		"reference_image":{"asset_id":"`+referenceAsset.ID+`"},
		"input_attachments":[{"type":"text","text":"validated attachment","file_name":"brief.txt"}],
		"watermark":true,
		"goal":"edited goal",
		"goal_mode":true,
		"has_content_image":false,
		"has_tail_image":true,
		"article_with_cover":false,
		"article_with_content_images":false,
		"execution_target":"local"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var env struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ProjectID != destinationProject.ID || env.Data.Type != model.PlatformArticle || env.Data.Prompt != "edited full clone prompt" {
		t.Fatalf("response task = project %q type %q prompt %q", env.Data.ProjectID, env.Data.Type, env.Data.Prompt)
	}

	tasks, err := repo.Tasks().FindByUserID(ctx, userID, destinationProject.ID, "", 0, 10)
	if err != nil {
		t.Fatalf("find destination tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("destination tasks = %d, want 2", len(tasks))
	}
	for _, task := range tasks {
		snapshot := task.ProjectSnapshot.Data()
		if task.Type != destinationProject.Platform || snapshot.Platform != destinationProject.Platform || snapshot.ProjectName != destinationProject.Name || snapshot.Instructions != destinationProject.Instructions || snapshot.VisualStyle != destinationProject.VisualStyle {
			t.Fatalf("destination snapshot = %#v task type=%q", snapshot, task.Type)
		}
		if task.ImageRatio != "1:1" || task.ImageModelKey != "free-image" || !task.SkipReferenceImage || task.ReferenceImageAssetID != referenceAsset.ID || !task.Watermark {
			t.Fatalf("shared overrides = %#v", task)
		}
		if task.Goal != "edited goal" || !task.GoalMode || task.HasContentImage || !task.HasTailImage || task.ArticleWithCover == nil || *task.ArticleWithCover || task.ArticleWithContentImages == nil || *task.ArticleWithContentImages {
			t.Fatalf("goal/image overrides = %#v", task)
		}
		if task.ExecutionTarget != model.ExecutionTargetLocal || task.LocalClaimDeadline == nil {
			t.Fatalf("execution target = %q deadline=%v", task.ExecutionTarget, task.LocalClaimDeadline)
		}
		if got := task.InputAttachments.Data(); len(got) != 1 || got[0].Text != "validated attachment" {
			t.Fatalf("attachments = %#v", got)
		}
		if task.InputSourceTaskID != source.ID || task.InputSourceProjectID != source.ProjectID {
			t.Fatalf("source provenance = %q/%q", task.InputSourceTaskID, task.InputSourceProjectID)
		}
	}
}

func TestCloneTask_FullEditableTypeSpecificFields(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		typeFields string
		assert     func(t *testing.T, task *model.Task)
	}{
		{
			name:       "ecommerce",
			platform:   model.PlatformEcommerce,
			typeFields: `"product_photos":["https://example.com/product.png"],"selected_modules":{"main_images":2},"target_platform":"amazon","selling_points":"durable","language":"en","provider_strategy_override":"balanced"`,
			assert: func(t *testing.T, task *model.Task) {
				got := task.Ecommerce.Data()
				if got.SelectedModules["main_images"] != 2 || got.TargetPlatform != "amazon" || got.SellingPoints != "durable" || got.Language != "en" || got.ProviderStrategyOverride != "balanced" || len(got.ProductPhotos) != 1 {
					t.Fatalf("ecommerce config = %#v", got)
				}
			},
		},
		{
			name:       "montage",
			platform:   model.PlatformMontage,
			typeFields: `"montage_input":{"brief":"edited montage brief","pipeline_key":"default","preferences":{"aspect_ratio":"16:9","duration_seconds":30}}`,
			assert: func(t *testing.T, task *model.Task) {
				got := task.MontageInput.Data()
				if got.Brief != "edited montage brief" || got.PipelineKey != "default" || got.Preferences.AspectRatio != "16:9" || got.Preferences.DurationSeconds != 30 {
					t.Fatalf("montage input = %#v", got)
				}
			},
		},
		{
			name:       "ecommerce language only",
			platform:   model.PlatformEcommerce,
			typeFields: `"language":"zh-CN"`,
			assert: func(t *testing.T, task *model.Task) {
				got := task.Ecommerce.Data()
				if got.SelectedModules["main_images"] != 1 || got.Language != "zh-CN" {
					t.Fatalf("language-only ecommerce config = %#v", got)
				}
			},
		},
		{
			name:       "ecommerce provider strategy only",
			platform:   model.PlatformEcommerce,
			typeFields: `"provider_strategy_override":"quality"`,
			assert: func(t *testing.T, task *model.Task) {
				got := task.Ecommerce.Data()
				if got.SelectedModules["main_images"] != 1 || got.ProviderStrategyOverride != "quality" {
					t.Fatalf("provider-only ecommerce config = %#v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskHandlerTestDB(t)
			repo := repository.New(db)
			userID := uuid.NewString()
			if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Email: tt.name + "-clone@example.com", Password: "hashed", InviteCode: tt.name + "clone"}); err != nil {
				t.Fatal(err)
			}
			sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "source", Status: model.ProjectStatusActive}
			destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: tt.platform, Name: tt.name, Status: model.ProjectStatusActive}
			if tt.platform == model.PlatformEcommerce {
				destinationProject.SetEcommerceDefaults(model.EcommerceProjectDefaults{DefaultSelectedModules: map[string]int{"main_images": 1}})
			}
			for _, project := range []*model.Project{sourceProject, destinationProject} {
				if err := repo.Projects().Create(t.Context(), project); err != nil {
					t.Fatal(err)
				}
			}
			source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: sourceProject.Platform, Status: model.TaskStatusCompleted}
			if err := repo.Tasks().Create(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			logger := zerolog.New(io.Discard)
			taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
			h := NewTaskHandler(taskSvc, &logger)
			h.SetRepository(repo)
			app := fiber.New()
			app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

			resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{"project_id":"`+destinationProject.ID+`","quantity":2,`+tt.typeFields+`}`)
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d body=%s", resp.StatusCode, body)
			}
			tasks, err := repo.Tasks().FindByUserID(t.Context(), userID, destinationProject.ID, "", 0, 10)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("destination tasks = %d err=%v", len(tasks), err)
			}
			tt.assert(t, tasks[0])
		})
	}
}

type cloneSourceReuseFixture struct {
	repo               repository.Repository
	app                *fiber.App
	userID             string
	rootProjectID      string
	rootTaskID         string
	source             *model.Task
	destinationProject *model.Project
}

func setupCloneSourceReuseFixture(t *testing.T, destinationPlatform string) *cloneSourceReuseFixture {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: uuid.NewString() + "@source-reuse.test", Password: "hashed", InviteCode: strings.ReplaceAll(uuid.NewString(), "-", "")[:16]}); err != nil {
		t.Fatal(err)
	}
	rootProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "root source", Status: model.ProjectStatusActive}
	sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "intermediate source", Status: model.ProjectStatusActive}
	destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: destinationPlatform, Name: "destination", Status: model.ProjectStatusActive}
	if destinationPlatform == model.PlatformEcommerce {
		destinationProject.SetEcommerceDefaults(model.EcommerceProjectDefaults{DefaultSelectedModules: map[string]int{"main_images": 1}})
	}
	for _, project := range []*model.Project{rootProject, sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	rootTask := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: rootProject.ID, Type: rootProject.Platform, Status: model.TaskStatusCompleted}
	source := &model.Task{
		ID:                   uuid.NewString(),
		UserID:               userID,
		ProjectID:            sourceProject.ID,
		Type:                 sourceProject.Platform,
		Status:               model.TaskStatusCompleted,
		InputSourceTaskID:    rootTask.ID,
		InputSourceProjectID: rootProject.ID,
	}
	for _, task := range []*model.Task{rootTask, source} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	store := &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })
	return &cloneSourceReuseFixture{
		repo:               repo,
		app:                app,
		userID:             userID,
		rootProjectID:      rootProject.ID,
		rootTaskID:         rootTask.ID,
		source:             source,
		destinationProject: destinationProject,
	}
}

func cloneSourceTaskURL(userID, projectID, taskID, fileName string) string {
	key := strings.Join([]string{"uploads", "users", userID, "projects", projectID, "tasks", taskID, "inputs", fileName}, "/")
	return "/api/v1/files/" + key
}

func cloneSourceReuseRequest(platform, projectID, rawURL string) string {
	switch platform {
	case model.PlatformEcommerce:
		return `{"project_id":"` + projectID + `","quantity":1,"product_photos":["` + rawURL + `"]}`
	case model.PlatformMontage:
		return `{"project_id":"` + projectID + `","quantity":1,"montage_input":{"brief":"reuse source","source_assets":[{"type":"image","url":"` + rawURL + `","file_name":"source.png","mime_type":"image/png"}]}}`
	default:
		return `{"project_id":"` + projectID + `","quantity":1,"input_attachments":[{"type":"image","url":"` + rawURL + `","file_name":"source.png","content_type":"image/png"}]}`
	}
}

func TestCloneTask_FullEditableAllowsTrustedRootSourceReuse(t *testing.T) {
	for _, platform := range []string{model.PlatformArticle, model.PlatformEcommerce, model.PlatformMontage} {
		t.Run(platform, func(t *testing.T) {
			fixture := setupCloneSourceReuseFixture(t, platform)
			rawURL := cloneSourceTaskURL(fixture.userID, fixture.rootProjectID, fixture.rootTaskID, "source.png")
			resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", cloneSourceReuseRequest(platform, fixture.destinationProject.ID, rawURL))
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
			}
			tasks, err := fixture.repo.Tasks().FindByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "", 0, 10)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("destination tasks = %d err=%v", len(tasks), err)
			}
			got := tasks[0]
			if got.InputSourceTaskID != fixture.rootTaskID || got.InputSourceProjectID != fixture.rootProjectID {
				t.Fatalf("root source = %q/%q", got.InputSourceTaskID, got.InputSourceProjectID)
			}
			switch platform {
			case model.PlatformEcommerce:
				if photos := got.Ecommerce.Data().ProductPhotos; len(photos) != 1 || photos[0] != rawURL {
					t.Fatalf("product photos = %#v", photos)
				}
			case model.PlatformMontage:
				if assets := got.MontageInput.Data().SourceAssets; len(assets) != 1 || assets[0].URL != rawURL {
					t.Fatalf("montage assets = %#v", assets)
				}
			default:
				if attachments := got.InputAttachments.Data(); len(attachments) != 1 || attachments[0].URL != rawURL {
					t.Fatalf("attachments = %#v", attachments)
				}
			}
		})
	}
}

func TestCloneTask_FullEditableAllowsPublicExternalSourceURLs(t *testing.T) {
	for _, platform := range []string{model.PlatformEcommerce, model.PlatformMontage} {
		t.Run(platform, func(t *testing.T) {
			fixture := setupCloneSourceReuseFixture(t, platform)
			rawURL := "https://public.example.com/source.png"
			resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", cloneSourceReuseRequest(platform, fixture.destinationProject.ID, rawURL))
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
			}
			tasks, err := fixture.repo.Tasks().FindByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "", 0, 10)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("destination tasks = %d err=%v", len(tasks), err)
			}
		})
	}
}

func TestCloneTask_FullEditableRejectsUntrustedTaskScopedSourceURLs(t *testing.T) {
	mismatches := []struct {
		name string
		ids  func(*cloneSourceReuseFixture) (string, string, string)
	}{
		{name: "wrong user", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return uuid.NewString(), f.rootProjectID, f.rootTaskID
		}},
		{name: "wrong project", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return f.userID, uuid.NewString(), f.rootTaskID
		}},
		{name: "wrong task", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return f.userID, f.rootProjectID, uuid.NewString()
		}},
	}
	for _, platform := range []string{model.PlatformArticle, model.PlatformEcommerce, model.PlatformMontage} {
		for _, mismatch := range mismatches {
			t.Run(platform+"/"+mismatch.name, func(t *testing.T) {
				fixture := setupCloneSourceReuseFixture(t, platform)
				userID, projectID, taskID := mismatch.ids(fixture)
				rawURL := cloneSourceTaskURL(userID, projectID, taskID, "source.png")
				resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", cloneSourceReuseRequest(platform, fixture.destinationProject.ID, rawURL))
				defer resp.Body.Close()
				if resp.StatusCode != fiber.StatusBadRequest {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, body)
				}
				count, err := fixture.repo.Tasks().CountByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "")
				if err != nil || count != 0 {
					t.Fatalf("destination task count = %d err=%v, want 0", count, err)
				}
				total, err := fixture.repo.Tasks().CountByUserID(t.Context(), fixture.userID, "", "")
				if err != nil || total != 2 {
					t.Fatalf("total task count = %d err=%v, want unchanged root+source tasks", total, err)
				}
			})
		}
	}
}

func TestCloneTask_FullEditableRejectsInvalidInputBeforePersistence(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, repo repository.Repository, userID string, destination *model.Project) string
		wantStatus int
	}{
		{
			name: "foreign project",
			prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
				destination.UserID = uuid.NewString()
				if err := repo.Projects().Create(t.Context(), destination); err != nil {
					t.Fatal(err)
				}
				return `{"project_id":"` + destination.ID + `","quantity":1}`
			},
			wantStatus: fiber.StatusForbidden,
		},
		{
			name: "inactive project",
			prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
				destination.Status = model.ProjectStatusArchived
				if err := repo.Projects().Create(t.Context(), destination); err != nil {
					t.Fatal(err)
				}
				return `{"project_id":"` + destination.ID + `","quantity":1}`
			},
			wantStatus: fiber.StatusBadRequest,
		},
		{name: "quantity zero", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"project_id":"` + destination.ID + `","quantity":0}`
		}, wantStatus: fiber.StatusBadRequest},
		{name: "quantity above five", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"project_id":"` + destination.ID + `","quantity":6}`
		}, wantStatus: fiber.StatusBadRequest},
		{name: "unavailable model", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"project_id":"` + destination.ID + `","quantity":1,"image_model_key":"unknown"}`
		}, wantStatus: fiber.StatusForbidden},
		{name: "invalid goal", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"project_id":"` + destination.ID + `","quantity":1,"goal_mode":true,"goal":"  "}`
		}, wantStatus: fiber.StatusBadRequest},
		{name: "unsafe attachment", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"project_id":"` + destination.ID + `","quantity":1,"input_attachments":[{"type":"image","url":"file:///etc/passwd","file_name":"passwd.png","content_type":"image/png"}]}`
		}, wantStatus: fiber.StatusBadRequest},
		{name: "inaccessible reference", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			asset := cutoverAsset(uuid.NewString(), uuid.NewString(), service.DirectUploadPurposeTaskReference, "foreign.png", "image/png")
			if err := repo.Assets().Create(t.Context(), asset); err != nil {
				t.Fatal(err)
			}
			return `{"project_id":"` + destination.ID + `","quantity":1,"reference_image":{"asset_id":"` + asset.ID + `"}}`
		}, wantStatus: fiber.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskHandlerTestDB(t)
			repo := repository.New(db)
			userID := uuid.NewString()
			if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Email: tt.name + "@example.com", Password: "hashed", InviteCode: strings.ReplaceAll(tt.name, " ", "")}); err != nil {
				t.Fatal(err)
			}
			sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "source", Status: model.ProjectStatusActive}
			if err := repo.Projects().Create(t.Context(), sourceProject); err != nil {
				t.Fatal(err)
			}
			source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: sourceProject.Platform, Status: model.TaskStatusCompleted}
			if err := repo.Tasks().Create(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			destination := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "destination", Status: model.ProjectStatusActive}
			body := tt.prepare(t, repo, userID, destination)

			store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
			logger := zerolog.New(io.Discard)
			taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, &logger, "", nil, "", nil, nil)
			referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			h := NewTaskHandler(taskSvc, &logger)
			h.SetRepository(repo)
			h.SetStore(store)
			h.SetReferenceAssetService(referenceSvc)
			h.SetImagePresets([]config.ImageModelPreset{{Key: "free-image", Provider: "test", Model: "image-v1", MinTier: "free"}})
			app := fiber.New()
			app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

			resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", body)
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, tt.wantStatus, responseBody)
			}
			tasks, err := repo.Tasks().FindByUserID(t.Context(), userID, "", "", 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 || tasks[0].ID != source.ID {
				t.Fatalf("tasks after rejection = %#v, want only source", tasks)
			}
		})
	}
}

func TestCloneTask_FullEditableRejectsInsufficientBalanceWithoutCreatingTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "clone-insufficient@example.com", Password: "hashed", InviteCode: "cloneinsufficient"}); err != nil {
		t.Fatal(err)
	}
	sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "source", Status: model.ProjectStatusActive}
	destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "destination", Status: model.ProjectStatusActive}
	for _, project := range []*model.Project{sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	bundle := billing.Bundle{
		Policy: billing.PolicyCatalog{
			Version:       "2026-07-22",
			TaskAdmission: billing.TaskAdmissionPolicy{RequireZeroDebt: true, RequireFullPrice: true},
			AcceptedTask:  billing.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TopUp:         billing.TopUpPolicy{RepayDebtFirst: true},
		},
		Products: billing.ProductCatalog{
			CatalogID: "clone-insufficient-v1",
			Currency:  "credits",
			SKUs: []billing.SKUConfig{{
				ID: "task.article.v1", Operation: "task.article", ChargePolicy: "task_admission", PriceCredits: 500, Delivery: "article",
			}},
		},
	}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	catalog := service.NewBillingCatalogService(repo, &bundle, service.BillingCatalogOptions{Now: func() time.Time { return now }})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatalf("publish billing catalog: %v", err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: userID}); err != nil {
		t.Fatalf("create empty wallet: %v", err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetBillingCatalogService(catalog)
	taskSvc.SetBillingWalletService(service.NewBillingWalletService(repo, &bundle, service.BillingWalletOptions{Now: func() time.Time { return now }}))
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

	resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{"project_id":"`+destinationProject.ID+`","quantity":1,"prompt":"new task"}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusPaymentRequired {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 402 body=%s", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, "", "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != source.ID {
		t.Fatalf("tasks after insufficient balance = %#v, want only source", tasks)
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
	uploadID := "resume-upload"
	pendingKey := "uploads/pending/" + userID + "/" + uploadID + "/notes.md"
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: uploadID, UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment,
		StagingKey: pendingKey, FileName: "notes.md", ContentType: "text/markdown", Size: int64(len("# notes")),
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	store := uploadSessionStatStore(repo.UploadSessions())
	store.data = map[string][]byte{pendingKey: []byte("# notes")}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/resume", `{
		"prompt":"继续写结论",
		"input_attachments":[{
			"type":"text","upload_id":"`+uploadID+`","key":"`+pendingKey+`",
			"file_name":"forged.exe","content_type":"application/x-msdownload","size":999999,
			"instruction":"修改意见"
		}]
	}`)
	defer resp.Body.Close()
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
	upload, err := repo.UploadSessions().FindByID(ctx, uploadID)
	if err != nil || upload.Status != model.UploadSessionFinalized || upload.AssetID == "" {
		t.Fatalf("upload session = %#v err=%v, want finalized", upload, err)
	}
}

func TestResumeTask_AcceptsJSONPromptOnly(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "resume-prompt@example.com", Password: "hashed", InviteCode: "resumeprompt"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.NewString()
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusFailed}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/resume", `{"prompt":"继续","input_attachments":[]}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 200", resp.StatusCode, body)
	}
	resumed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("task status = %q, want pending", resumed.Status)
	}
	attachments := resumed.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].Role != model.EntryAttachmentRoleResumeLatest || !strings.Contains(attachments[0].Text, "继续") {
		t.Fatalf("resume attachments = %#v, want latest prompt", attachments)
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/resume", `{
		"input_attachments":[{
			"type":"text","upload_id":"resume-storage-upload",
			"key":"uploads/pending/`+userID+`/resume-storage-upload/notes.md"
		}]
	}`)
	defer resp.Body.Close()
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, workspaceRoot, nil, nil)
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

func TestGetTaskByIDIncludesFixedBillingIdentity(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	taskID := uuid.NewString()
	chargeID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID: userID, Email: "task-billing@example.com", Password: "hashed", InviteCode: "taskbilling",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, Prompt: "详情固定价格展示",
		BillingQuoteID: "quote-v1", BillingCatalogID: "retail-v1", BillingSKUID: "task.article.standard.v1",
		BillingChargeID: &chargeID, BillingPriceCredits: 6000,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewTaskHandler(service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil), &logger)
	app := fiber.New()
	app.Get("/tasks/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetByID(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			ID                  string  `json:"id"`
			BillingCatalogID    string  `json:"billing_catalog_id"`
			BillingSKUID        string  `json:"billing_sku_id"`
			BillingChargeID     *string `json:"billing_charge_id"`
			BillingPriceCredits int64   `json:"billing_price_credits"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ID != taskID || body.Data.BillingCatalogID != "retail-v1" ||
		body.Data.BillingSKUID != "task.article.standard.v1" || body.Data.BillingChargeID == nil ||
		*body.Data.BillingChargeID != chargeID || body.Data.BillingPriceCredits != 6000 {
		t.Fatalf("task billing identity = %#v", body.Data)
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
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
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

func TestCreateTaskRejectsNonImageSeednoteAttachment(t *testing.T) {
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
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil {
		t.Fatalf("find tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks = %#v, want none", tasks)
	}
}
