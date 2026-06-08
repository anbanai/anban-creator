package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/storage"
)

func setupTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Plan{}, &model.Task{}, &model.User{},
		&model.LoginSession{}, &model.TaskFile{}, &model.Channel{},
		&model.CreditTransaction{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func setupTaskServiceWithEnqueuer(t *testing.T) (*TaskService, repository.Repository) {
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
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	return svc, repo
}

// mockEnqueuer captures enqueued tasks without executing them.
type mockEnqueuer struct {
	enqueued []string
}

func (m *mockEnqueuer) Enqueue(taskType string, payload []byte) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

func (m *mockEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

type fakePublishedTrackingService struct {
	calls []struct {
		userID string
		taskID string
	}
	err error
}

func (f *fakePublishedTrackingService) EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string) error {
	f.calls = append(f.calls, struct {
		userID string
		taskID string
	}{userID: userID, taskID: taskID})
	return f.err
}

func TestTaskService_FinalizeTitleUpdatesCanonicalTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	title, err := svc.FinalizeTitle(ctx, userID, task.ID, "  月薪5000和月薪5万的人，喝茶差距在哪  ")
	if err != nil {
		t.Fatalf("FinalizeTitle: %v", err)
	}
	if title != "月薪5000和月薪5万的人，喝茶差距在哪" {
		t.Fatalf("title = %q", title)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Title != title {
		t.Fatalf("stored title = %q, want %q", found.Title, title)
	}
}

func TestTaskService_FinalizeTitleRejectsInvalidTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"empty", "   ", "title is required"},
		{"too long", strings.Repeat("长", 201), "title must be <= 200 characters"},
		{"artifact", "图片内容规划", "artifact title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.FinalizeTitle(ctx, userID, task.ID, tt.title)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("FinalizeTitle error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestTaskService_FinalizeTitleRejectsForeignTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherID := uuid.New().String()
	channelID := createTestChannel(t, repo, ownerID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    ownerID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, otherID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsUserKeyForUnownedTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    "",
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, userID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateWithinChannel(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	otherChannelID := createTestChannel(t, repo, userID, model.PlatformSeednote)

	existing := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Title:     "新手咖啡豆怎么选",
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	current := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	foreignChannel := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: otherChannelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	for _, task := range []*model.Task{existing, current, foreignChannel} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, current.ID, " 新手 咖啡豆怎么选 ")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}

	if _, err := svc.FinalizeTitle(ctx, userID, foreignChannel.ID, "新手咖啡豆怎么选"); err != nil {
		t.Fatalf("other channel duplicate should be allowed: %v", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateEvenWhenCurrentTaskAlreadyHasTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)

	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ChannelID: channelID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-current-with-same-title",
			UserID:    userID,
			ChannelID: channelID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusRunning,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, "task-current-with-same-title", "新手 咖啡豆怎么选")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}
}

func TestTaskService_ClearArtifactTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ChannelID: channelID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "图片内容规划",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ChannelID: channelID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "真实茶饮标题",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	count, err := svc.ClearArtifactTitles(ctx)
	if err != nil {
		t.Fatalf("ClearArtifactTitles: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	titles, err := svc.ListTitles(ctx, channelID)
	if err != nil {
		t.Fatalf("ListTitles: %v", err)
	}
	if len(titles) != 1 || titles[0] != "真实茶饮标题" {
		t.Fatalf("titles = %v, want [真实茶饮标题]", titles)
	}
}

type fakeTaskExecutor struct {
	result *agent.ExecutionResult
	err    error
}

func (f *fakeTaskExecutor) Execute(ctx context.Context, opts *agent.ExecutionOptions) (*agent.ExecutionResult, error) {
	return f.result, f.err
}

func TestTaskService_ExecuteDoesNotExtractTitleFromWorkspace(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "image-plan.md"), []byte("# 图片内容规划\n\ninternal"), 0644); err != nil {
		t.Fatalf("write image plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "content.md"), []byte("# 真实最终标题\n\ncontent"), 0644); err != nil {
		t.Fatalf("write content: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
		Title:     "AI 已上报标题",
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svc := NewTaskService(repo, &fakeTaskExecutor{result: &agent.ExecutionResult{
		Success: true,
		WorkDir: workDir,
	}}, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.HandleExecution(ctx, task, nil); err != nil {
		t.Fatalf("HandleExecution: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Title != "AI 已上报标题" {
		t.Fatalf("title = %q, want existing AI-reported title", found.Title)
	}
}

func TestTaskService_HandleExecutionFailure_PermanentAuthErrorDoesNotRetry(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	svc := NewTaskService(repo, nil, enqueuer, nil, nil, &logger, "", nil, "", nil, nil)

	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:                  uuid.New().String(),
		UserID:              userID,
		ChannelID:           channelID,
		Type:                model.PlatformArticle,
		Status:              model.TaskStatusRunning,
		Prompt:              "auth failure",
		MaxRetries:          model.DefaultRetries,
		RetryCount:          0,
		RateLimitRetryCount: 0,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	execErr := fmt.Errorf("agent execution failed: Failed to authenticate. API Error: 403 {\"error\":{\"type\":\"forbidden\",\"message\":\"Request not allowed\"}}")
	if err := svc.HandleExecutionFailure(ctx, task, execErr); err == nil {
		t.Fatal("expected permanent auth error to be returned")
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want %q", found.Status, model.TaskStatusFailed)
	}
	if found.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0", found.RetryCount)
	}
	if found.RateLimitRetryCount != 0 {
		t.Fatalf("rate_limit_retry_count = %d, want 0", found.RateLimitRetryCount)
	}
	if found.CompletedAt == nil {
		t.Fatal("completed_at was not set")
	}
	if !strings.Contains(found.ErrorMessage, "API Error: 403") {
		t.Fatalf("error_message = %q, want API Error: 403", found.ErrorMessage)
	}
	if len(enqueuer.enqueued) != 0 {
		t.Fatalf("auth error should not enqueue retries, got %d", len(enqueuer.enqueued))
	}
}

func TestBuildNoOutputFilesErrorIncludesLastToolError(t *testing.T) {
	result := &agent.ExecutionResult{
		Model:             "claude-test",
		NumTurns:          10,
		ToolUseCount:      9,
		ToolErrorCount:    1,
		LastToolErrorTool: "generate_image",
		LastToolError:     "provider rejected model",
	}

	msg := buildNoOutputFilesError(result)
	if !strings.Contains(msg, "tool_errors=1") {
		t.Fatalf("error = %q, want tool error count", msg)
	}
	if !strings.Contains(msg, "generate_image failed: provider rejected model") {
		t.Fatalf("error = %q, want last MCP tool error", msg)
	}
	if strings.Contains(msg, "check user model config") {
		t.Fatalf("error = %q, should not use old generic model-config hint", msg)
	}
}

func TestBuildNoOutputFilesErrorWithoutToolErrorKeepsFallback(t *testing.T) {
	result := &agent.ExecutionResult{
		Model:        "",
		NumTurns:     10,
		ToolUseCount: 9,
	}

	msg := buildNoOutputFilesError(result)
	if !strings.Contains(msg, "tool_uses=9") {
		t.Fatalf("error = %q, want tool use count", msg)
	}
	if !strings.Contains(msg, "files may have been written to an unexpected location") {
		t.Fatalf("error = %q, want fallback location diagnostic", msg)
	}
}

func TestTaskService_CreateManual(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, "wechat")

	tasks, err := svc.CreateManual(context.Background(), userID, channelID, "Test topic", 1, "", nil, "", nil)
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.ID == "" {
		t.Error("task.ID should not be empty")
	}
	if task.UserID != userID {
		t.Errorf("UserID = %q, want %q", task.UserID, userID)
	}
	if task.ChannelID != channelID {
		t.Errorf("ChannelID = %q, want %q", task.ChannelID, channelID)
	}
	if task.Status != model.TaskStatusPending {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskStatusPending)
	}
	if task.Prompt != "Test topic" {
		t.Errorf("Prompt = %q, want %q", task.Prompt, "Test topic")
	}
}

func TestTaskService_CreateManual_NoChannel(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.CreateManual(context.Background(), "user1", "", "topic", 1, "", nil, "", nil)
	if err == nil {
		t.Error("expected error for empty channel_id")
	}
}

func TestTaskService_CreateManual_WrongUser(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, "wechat")

	_, err := svc.CreateManual(context.Background(), "wrong-user", channelID, "topic", 1, "", nil, "", nil)
	if err == nil {
		t.Error("expected error for wrong user")
	}
}

func TestTaskService_GetByID(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), userID, channelID, "Find me", 1, "", nil, "", nil)
	task := taskSlice[0]

	found, err := svc.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.ID != task.ID {
		t.Errorf("ID = %q, want %q", found.ID, task.ID)
	}
}

func TestTaskService_GetByID_NotFound(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.GetByID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestTaskService_Cancel(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), userID, channelID, "Cancel me", 1, "", nil, "", nil)
	task := taskSlice[0]

	err := svc.Cancel(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	found, _ := svc.GetByID(context.Background(), task.ID)
	if found.Status != model.TaskStatusCancelled {
		t.Errorf("Status = %q, want %q", found.Status, model.TaskStatusCancelled)
	}
}

func TestTaskService_List(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, "wechat")

	svc.CreateManual(context.Background(), userID, channelID, "Task 1", 1, "", nil, "", nil)
	svc.CreateManual(context.Background(), userID, channelID, "Task 2", 1, "", nil, "", nil)

	tasks, total, err := svc.List(context.Background(), userID, 0, 10, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total < 2 {
		t.Errorf("total = %d, want >= 2", total)
	}
	if len(tasks) < 2 {
		t.Errorf("len(tasks) = %d, want >= 2", len(tasks))
	}
}

func TestTaskService_List_ByStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, "wechat")

	taskSlice, _ := svc.CreateManual(context.Background(), userID, channelID, "Pending task", 1, "", nil, "", nil)
	task := taskSlice[0]
	svc.Cancel(context.Background(), task.ID)

	// Filter by pending — should find none
	tasks, total, err := svc.List(context.Background(), userID, 0, 10, model.TaskStatusPending, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 {
		t.Errorf("pending total = %d, want 0", total)
	}
	if len(tasks) != 0 {
		t.Errorf("pending tasks = %d, want 0", len(tasks))
	}

	// Filter by cancelled
	tasks, total, err = svc.List(context.Background(), userID, 0, 10, model.TaskStatusCancelled, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("cancelled total = %d, want 1", total)
	}
}

func TestTaskService_GetFiles(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	files, err := svc.GetFiles(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestTaskService_SetPublishedCreatesSeednoteTracking(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	trackingSvc := &fakePublishedTrackingService{}
	svc.SetSeednoteTrackingService(trackingSvc)

	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.SetPublished(ctx, userID, task.ID, true); err != nil {
		t.Fatalf("SetPublished: %v", err)
	}

	if len(trackingSvc.calls) != 1 {
		t.Fatalf("tracking calls = %d, want 1", len(trackingSvc.calls))
	}
	if trackingSvc.calls[0].userID != userID || trackingSvc.calls[0].taskID != task.ID {
		t.Fatalf("tracking call = %+v", trackingSvc.calls[0])
	}
}

func TestTaskService_SetPublishedSkipsTrackingForNonSeednoteOrUnpublish(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	trackingSvc := &fakePublishedTrackingService{}
	svc.SetSeednoteTrackingService(trackingSvc)

	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.SetPublished(ctx, userID, task.ID, true); err != nil {
		t.Fatalf("SetPublished article: %v", err)
	}
	if err := svc.SetPublished(ctx, userID, task.ID, false); err != nil {
		t.Fatalf("SetPublished false: %v", err)
	}
	if len(trackingSvc.calls) != 0 {
		t.Fatalf("tracking calls = %d, want 0", len(trackingSvc.calls))
	}
}

func TestTaskService_DownloadTasksZip(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformSeednote)
	otherChannelID := createTestChannel(t, repo, otherUserID, model.PlatformSeednote)

	completed := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "A finished task",
		Title:     "Finished",
	}
	pending := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusPending,
		Prompt:    "A pending task",
	}
	foreign := &model.Task{
		ID:        uuid.New().String(),
		UserID:    otherUserID,
		ChannelID: otherChannelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "Foreign task",
	}
	for _, task := range []*model.Task{completed, pending, foreign} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	if _, err := svc.UploadTaskFileFromReader(ctx, completed.ID, userID, "output/article.md", strings.NewReader("# hello"), "text/markdown", 7); err != nil {
		t.Fatalf("upload completed task file: %v", err)
	}
	if _, err := svc.UploadTaskFileFromReader(ctx, foreign.ID, otherUserID, "output/secret.md", strings.NewReader("secret"), "text/markdown", 6); err != nil {
		t.Fatalf("upload foreign task file: %v", err)
	}

	buf, zipName, err := svc.DownloadTasksZip(ctx, userID, []string{completed.ID, pending.ID, foreign.ID})
	if err != nil {
		t.Fatalf("DownloadTasksZip: %v", err)
	}
	if !strings.HasPrefix(zipName, "tasks_export_") || !strings.HasSuffix(zipName, ".zip") {
		t.Fatalf("unexpected zip name: %s", zipName)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	var hasManifest bool
	var hasCompletedFile bool
	for _, file := range reader.File {
		if file.Name == "manifest.json" {
			hasManifest = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open manifest: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			manifest := string(data)
			var parsedManifest BulkDownloadZipManifest
			if err := json.Unmarshal(data, &parsedManifest); err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			for _, want := range []string{completed.ID, pending.ID, foreign.ID, "task_not_completed", "unavailable"} {
				if !strings.Contains(manifest, want) {
					t.Fatalf("manifest missing %q: %s", want, manifest)
				}
			}
			if strings.Contains(manifest, "forbidden") || strings.Contains(manifest, "task_not_found") {
				t.Fatalf("manifest uses distinguishable unavailable reasons: %s", manifest)
			}
			for _, task := range parsedManifest.Tasks {
				if task.TaskID == foreign.ID && (task.Title != "" || task.Status != "") {
					t.Fatalf("manifest leaked foreign task metadata: %+v", task)
				}
			}
		}
		if strings.HasSuffix(file.Name, "output/article.md") {
			hasCompletedFile = true
		}
		if strings.Contains(file.Name, "secret.md") {
			t.Fatalf("foreign file leaked into zip: %s", file.Name)
		}
	}
	if !hasManifest {
		t.Fatal("manifest.json missing")
	}
	if !hasCompletedFile {
		t.Fatal("completed task file missing")
	}
}

func TestTaskService_RebuildWorkflowStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	channelID := createTestChannel(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
		Prompt:    "workflow task",
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	files := []struct {
		path    string
		body    string
		mime    string
		size    int64
		wantErr bool
	}{
		{"output/03-draft.md", "# Draft", "text/markdown", 7, false},
		{"output/04-final.md", "# Final", "text/markdown", 7, false},
		{"output/review.json", `{"overall_score":91,"readiness":"ready","strengths":["清晰"],"risks":[],"next_actions":["发布"]}`, "application/json", 92, false},
	}
	for _, file := range files {
		if _, err := svc.UploadTaskFileFromReader(ctx, task.ID, userID, file.path, strings.NewReader(file.body), file.mime, file.size); err != nil {
			t.Fatalf("upload %s: %v", file.path, err)
		}
	}

	if err := svc.RebuildWorkflowStatus(ctx, task.ID); err != nil {
		t.Fatalf("RebuildWorkflowStatus: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.WorkflowStatus == nil || *found.WorkflowStatus == "" {
		t.Fatal("workflow_status was not persisted")
	}

	var status WorkflowStatus
	if err := json.Unmarshal([]byte(*found.WorkflowStatus), &status); err != nil {
		t.Fatalf("unmarshal workflow status: %v", err)
	}
	if status.Review == nil {
		t.Fatal("expected review summary")
	}
	if status.Review.OverallScore != 91 {
		t.Fatalf("review score = %d, want 91", status.Review.OverallScore)
	}
}
