package scheduler

import (
	"context"
	"encoding/json"
	"errors"
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

type hookedSchedulerPlanRepository struct {
	repository.PlanRepository
	nextRunCASCalls  int
	beforeNextRunCAS func()
}

func (r *hookedSchedulerPlanRepository) UpdateNextRunAtIf(ctx context.Context, id string, nextRunAt, expectedNextRunAt *time.Time) (bool, error) {
	r.nextRunCASCalls++
	if r.beforeNextRunCAS != nil {
		r.beforeNextRunCAS()
	}
	return r.PlanRepository.UpdateNextRunAtIf(ctx, id, nextRunAt, expectedNextRunAt)
}

type schedulerRepositoryOverride struct {
	repository.Repository
	plans repository.PlanRepository
}

func (r schedulerRepositoryOverride) Plans() repository.PlanRepository {
	return r.plans
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

	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Plan{}, &model.Task{}, &model.Asset{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &recordingEnqueuer{}
	taskSvc := service.NewTaskService(repo, enqueuer, nil, &logger, "", nil, nil)
	profiles, err := service.NewAgentProfileRegistry([]service.AgentExecutionProfile{{
		ID: "cost_effective", DisplayName: "Cost effective", Description: "fixture",
		Provider: "deepseek", Protocol: "anthropic",
		Models: model.AgentModelMatrix{
			Default: "deepseek-v4-pro", Opus: "deepseek-v4-pro", Fable: "deepseek-v4-pro",
			Sonnet: "deepseek-v4-pro", Haiku: "deepseek-v4-pro",
		},
		ModelUsageAliases: map[string]string{"deepseek-v4-pro": "deepseek-v4-pro"},
		BaseURL:           "https://anthropic.example.com", AuthToken: "scheduler-test-token",
		MinTier: model.TierFree, Available: true,
	}})
	if err != nil {
		t.Fatalf("create profile registry: %v", err)
	}
	taskSvc.SetAgentProfileRegistry(profiles)
	taskSvc.SetReferenceAssetService(service.NewReferenceAssetService(repo, nil, time.Now))
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
	asset := &model.Asset{ID: uuid.NewString(), UserID: userID, Purpose: service.DirectUploadPurposeTaskReference, StorageKey: "assets/users/" + userID + "/plan-reference/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, ETag: "etag"}
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatalf("create reference asset: %v", err)
	}
	plan := &model.Plan{ExecutionProfile: "cost_effective",
		ID:                    uuid.New().String(),
		UserID:                userID,
		ProjectID:             projectID,
		Type:                  model.PlatformArticle,
		Title:                 "Fallback title",
		CronExpr:              "0 * * * *",
		Prompt:                "Write from this plan",
		Status:                model.PlanStatusActive,
		ReferenceImageAssetID: asset.ID,
		SkipReferenceImage:    true,
		Watermark:             true,
		NextRunAt:             &originalNextRun,
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
	if !task.SkipReferenceImage || !task.Watermark || task.ReferenceImageAssetID != plan.ReferenceImageAssetID {
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

func TestTriggerPlanNowRejectsInvalidReferenceBeforeTaskCreation(t *testing.T) {
	repo, taskSvc, enqueuer, logger := setupPlanCheckerTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	nextRun := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "invalid-reference@example.com", Password: "hashed"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	plan := &model.Plan{ExecutionProfile: "cost_effective",
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		CronExpr: "0 * * * *", Status: model.PlanStatusActive, NextRunAt: &nextRun,
		ReferenceImageAssetID: "missing-asset",
	}
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatal(err)
	}

	err := TriggerPlanNow(ctx, repo, taskSvc, plan.ID, logger)
	if !errors.Is(err, service.ErrReferenceAssetForbidden) {
		t.Fatalf("TriggerPlanNow error = %v, want forbidden", err)
	}
	_, total, listErr := taskSvc.List(ctx, userID, 0, 10, "", projectID, "")
	if listErr != nil || total != 0 {
		t.Fatalf("tasks after invalid reference = %d, %v", total, listErr)
	}
	if len(enqueuer.items) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(enqueuer.items))
	}
	updated, findErr := repo.Plans().FindByID(ctx, plan.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.Equal(nextRun) {
		t.Fatalf("next_run_at advanced after invalid reference: %v", updated.NextRunAt)
	}
}

func TestAdvancePlanNextRunDoesNotOverwriteConcurrentReferenceUpdate(t *testing.T) {
	base, _, _, _ := setupPlanCheckerTest(t)
	oldNext := time.Now().Add(-time.Hour).Truncate(time.Second)
	plan := &model.Plan{ExecutionProfile: "cost_effective",
		ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformArticle,
		Prompt: "before", ReferenceImageAssetID: "asset-a", CronExpr: "0 * * * *",
		Status: model.PlanStatusActive, NextRunAt: &oldNext,
	}
	if err := base.Plans().Create(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	stale, err := base.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	hooked := &hookedSchedulerPlanRepository{PlanRepository: base.Plans()}
	hooked.beforeNextRunCAS = func() {
		current, err := base.Plans().FindByID(t.Context(), plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		current.ReferenceImageAssetID = "asset-b"
		if err := base.Plans().Update(t.Context(), current); err != nil {
			t.Fatal(err)
		}
	}
	repo := schedulerRepositoryOverride{Repository: base, plans: hooked}

	next, err := advancePlanNextRun(t.Context(), repo, stale)
	if err != nil {
		t.Fatalf("advancePlanNextRun: %v", err)
	}
	if hooked.nextRunCASCalls != 1 {
		t.Fatalf("next_run CAS calls = %d, want 1", hooked.nextRunCASCalls)
	}
	persisted, err := base.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ReferenceImageAssetID != "asset-b" || persisted.Prompt != "before" {
		t.Fatalf("stale scheduler write changed plan = prompt %q reference %q", persisted.Prompt, persisted.ReferenceImageAssetID)
	}
	if next == nil || persisted.NextRunAt == nil || !persisted.NextRunAt.Equal(*next) {
		t.Fatalf("next_run_at = %v, want %v", persisted.NextRunAt, next)
	}
}

func TestAdvancePlanNextRunSkipsConcurrentNextRunUpdate(t *testing.T) {
	base, _, _, _ := setupPlanCheckerTest(t)
	oldNext := time.Now().Add(-time.Hour).Truncate(time.Second)
	concurrentNext := oldNext.Add(30 * time.Minute)
	plan := &model.Plan{ExecutionProfile: "cost_effective",
		ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformArticle,
		Prompt: "before", ReferenceImageAssetID: "asset-a", CronExpr: "0 * * * *",
		Status: model.PlanStatusActive, NextRunAt: &oldNext,
	}
	if err := base.Plans().Create(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	stale, err := base.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	hooked := &hookedSchedulerPlanRepository{PlanRepository: base.Plans()}
	hooked.beforeNextRunCAS = func() {
		current, err := base.Plans().FindByID(t.Context(), plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		current.ReferenceImageAssetID = "asset-b"
		current.NextRunAt = &concurrentNext
		if err := base.Plans().Update(t.Context(), current); err != nil {
			t.Fatal(err)
		}
	}
	repo := schedulerRepositoryOverride{Repository: base, plans: hooked}

	next, err := advancePlanNextRun(t.Context(), repo, stale)
	if err != nil {
		t.Fatalf("advancePlanNextRun: %v", err)
	}
	if next != nil {
		t.Fatalf("next = %v, want skipped conflict", next)
	}
	persisted, err := base.Plans().FindByID(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ReferenceImageAssetID != "asset-b" || persisted.NextRunAt == nil || !persisted.NextRunAt.Equal(concurrentNext) {
		t.Fatalf("conflict changed plan = reference %q next %v", persisted.ReferenceImageAssetID, persisted.NextRunAt)
	}
}

func TestTriggerPlanNowSkipsInactivePlanWithoutRetryableError(t *testing.T) {
	repo, taskSvc, enqueuer, logger := setupPlanCheckerTest(t)
	ctx := context.Background()

	plan := &model.Plan{ExecutionProfile: "cost_effective",
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
