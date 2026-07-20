package scheduler

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

type recordingEnqueuer struct {
	items []queuedTask
}

type queuedTask struct {
	taskType string
	payload  []byte
}

func (e *recordingEnqueuer) Enqueue(taskType string, payload []byte) error {
	e.items = append(e.items, queuedTask{taskType: taskType, payload: append([]byte(nil), payload...)})
	return nil
}

func (e *recordingEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	e.items = append(e.items, queuedTask{taskType: taskType, payload: append([]byte(nil), payload...)})
	return nil
}

func (e *recordingEnqueuer) EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error) {
	e.items = append(e.items, queuedTask{taskType: taskType, payload: append([]byte(nil), payload...)})
	return true, nil
}

func setupPlanCheckerTest(t *testing.T) (repository.Repository, *service.TaskService, *recordingEnqueuer, *zerolog.Logger) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})

	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Plan{}, &model.Task{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &recordingEnqueuer{}
	taskSvc := service.NewTaskService(repo, nil, enqueuer, nil, &logger, "", nil, "", nil, nil)
	return repo, taskSvc, enqueuer, &logger
}

func TestTriggerPlanNowCreatesTaskAndAdvancesNextRun(t *testing.T) {
	repo, taskSvc, enqueuer, logger := setupPlanCheckerTest(t)
	ctx := context.Background()

	userID := uuid.New().String()
	projectID := uuid.New().String()
	originalNextRun := time.Now().Add(-time.Hour).Truncate(time.Second)

	if err := repo.Users().Create(ctx, &model.User{
		ID:       userID,
		Email:    "planner@example.com",
		Nickname: "Planner",
		Password: "hashed-password",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:                 projectID,
		UserID:             userID,
		Platform:           model.PlatformSeednote,
		Name:               "Seednote",
		MaxConcurrentTasks: 1,
		Status:             model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	plan := &model.Plan{
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformArticle,
		Title:              "Fallback title",
		CronExpr:           "0 * * * *",
		Prompt:             "Write from this plan",
		Status:             model.PlanStatusActive,
		ReferenceImageURL:  "https://example.com/ref.png",
		SkipReferenceImage: true,
		Watermark:          true,
		NextRunAt:          &originalNextRun,
	}
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if err := TriggerPlanNow(ctx, repo, taskSvc, plan.ID, logger); err != nil {
		t.Fatalf("TriggerPlanNow: %v", err)
	}

	tasks, total, err := taskSvc.List(ctx, userID, 0, 10, "", projectID, "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("tasks total=%d len=%d, want one task", total, len(tasks))
	}
	task := tasks[0]
	if task.Type != model.PlatformSeednote {
		t.Fatalf("task type = %q, want project platform %q", task.Type, model.PlatformSeednote)
	}
	if task.Prompt != plan.Prompt {
		t.Fatalf("task prompt = %q, want %q", task.Prompt, plan.Prompt)
	}
	if !task.SkipReferenceImage || !task.Watermark || task.ReferenceImageURL != plan.ReferenceImageURL {
		t.Fatalf("task image settings not copied from plan: %#v", task)
	}

	if len(enqueuer.items) != 1 {
		t.Fatalf("enqueued %d items, want 1", len(enqueuer.items))
	}
	if enqueuer.items[0].taskType != service.TypeContentGenerate {
		t.Fatalf("enqueued task type = %q, want %q", enqueuer.items[0].taskType, service.TypeContentGenerate)
	}
	var payload map[string]string
	if err := json.Unmarshal(enqueuer.items[0].payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["task_id"] != task.ID || payload["user_id"] != userID {
		t.Fatalf("payload = %#v, want task_id=%q user_id=%q", payload, task.ID, userID)
	}

	updated, err := repo.Plans().FindByID(ctx, plan.ID)
	if err != nil {
		t.Fatalf("find updated plan: %v", err)
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.After(originalNextRun) {
		t.Fatalf("next_run_at = %v, want after %v", updated.NextRunAt, originalNextRun)
	}
}

func TestTriggerPlanNowSkipsInactivePlanWithoutRetryableError(t *testing.T) {
	repo, taskSvc, enqueuer, logger := setupPlanCheckerTest(t)
	ctx := context.Background()

	plan := &model.Plan{
		ID:       uuid.New().String(),
		UserID:   uuid.New().String(),
		Type:     model.PlatformSeednote,
		CronExpr: "0 * * * *",
		Status:   model.PlanStatusPaused,
	}
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	err := TriggerPlanNow(ctx, repo, taskSvc, plan.ID, logger)
	if err != nil {
		t.Fatalf("TriggerPlanNow returned retryable error for inactive plan: %v", err)
	}
	tasks, total, err := taskSvc.List(ctx, plan.UserID, 0, 10, "", "", "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if total != 0 || len(tasks) != 0 {
		t.Fatalf("tasks total=%d len=%d, want no task for inactive plan", total, len(tasks))
	}
	if len(enqueuer.items) != 0 {
		t.Fatalf("enqueued %d items, want none", len(enqueuer.items))
	}
}

func TestStuckTaskReaperSkipsDurableExecution(t *testing.T) {
	repo, taskSvc, _, logger := setupPlanCheckerTest(t)
	ctx := context.Background()
	stale := time.Now().Add(-stuckTaskThreshold - time.Minute)
	executionID := uuid.NewString()
	task := &model.Task{
		ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformSeednote,
		Status: model.TaskStatusRunning, StartedAt: &stale, CurrentExecutionID: &executionID,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	reapStuckTasks(ctx, repo, taskSvc, logger)
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskStatusRunning {
		t.Fatalf("durable execution was reaped: status=%q error=%q", found.Status, found.ErrorMessage)
	}
}
