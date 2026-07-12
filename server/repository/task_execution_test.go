package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type taskExecutionTestRepository struct {
	Repository
	db *gorm.DB
}

func setupTaskExecutionRepository(t *testing.T) taskExecutionTestRepository {
	t.Helper()

	db := setupTestDB(t)
	return taskExecutionTestRepository{Repository: New(db), db: db}
}

func seedTaskExecution(t *testing.T, repo taskExecutionTestRepository, status string) *model.TaskExecution {
	t.Helper()

	execution := &model.TaskExecution{
		ID:      uuid.NewString(),
		TaskID:  uuid.NewString(),
		Attempt: 1,
		Target:  "kubernetes",
		Status:  status,
	}
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatalf("create task execution: %v", err)
	}
	return execution
}

func TestTaskExecutionRepositoryTransitionIsCAS(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	execution := seedTaskExecution(t, repo, model.TaskExecutionStarting)

	won, err := repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionRunning,
		model.ExecutionTransition{Started: true, PodUID: "pod-1"})
	if err != nil || !won {
		t.Fatalf("first transition = %v, %v", won, err)
	}

	won, err = repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionFailed,
		model.ExecutionTransition{TerminalReason: "late"})
	if err != nil || won {
		t.Fatalf("stale transition = %v, %v", won, err)
	}

	found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatalf("find transitioned execution: %v", err)
	}
	if found.Status != model.TaskExecutionRunning || !found.Started || found.StartedAt == nil || found.PodUID != "pod-1" {
		t.Fatalf("transitioned execution = %+v", found)
	}
	if found.CompletedAt != nil || found.TerminalReason != "" {
		t.Fatalf("stale transition changed terminal fields: %+v", found)
	}
}

func TestTaskExecutionRepositoryTerminalTransitionIsAtomic(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	execution := seedTaskExecution(t, repo, model.TaskExecutionRunning)
	diagnostics := datatypes.JSON(`{"exit_code":137}`)

	won, err := repo.TaskExecutions().Transition(context.Background(), execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
		model.ExecutionTransition{
			ManifestStatus: "rejected",
			TerminalReason: "oom_killed",
			Diagnostics:    diagnostics,
		})
	if err != nil || !won {
		t.Fatalf("terminal transition = %v, %v", won, err)
	}

	found, err := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if err != nil {
		t.Fatalf("find terminal execution: %v", err)
	}
	if found.Status != model.TaskExecutionFailed || found.CompletedAt == nil {
		t.Fatalf("terminal status = %q, completed_at = %v", found.Status, found.CompletedAt)
	}
	if found.ManifestStatus != "rejected" || found.TerminalReason != "oom_killed" || string(found.Diagnostics) != string(diagnostics) {
		t.Fatalf("terminal transition fields = %+v", found)
	}
}

func TestTaskExecutionRepositoryFindsCurrentAttempt(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	taskID := uuid.NewString()
	currentID := uuid.NewString()
	task := &model.Task{
		ID:                 taskID,
		UserID:             uuid.NewString(),
		Type:               "article",
		Prompt:             "topic",
		CurrentExecutionID: &currentID,
	}
	if err := repo.db.WithContext(ctx).Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	for attempt, id := range []string{uuid.NewString(), currentID} {
		execution := &model.TaskExecution{
			ID:      id,
			TaskID:  taskID,
			Attempt: attempt + 1,
			Target:  "kubernetes",
			Status:  model.TaskExecutionStarting,
		}
		if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
			t.Fatalf("create attempt %d: %v", attempt+1, err)
		}
	}

	found, err := repo.TaskExecutions().FindCurrentByTaskID(ctx, taskID)
	if err != nil {
		t.Fatalf("find current execution: %v", err)
	}
	if found.ID != currentID || found.Attempt != 2 {
		t.Fatalf("current execution = %+v", found)
	}
}

func TestTaskExecutionRepositoryRuntimeAndReconciliation(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	refreshed := seedTaskExecution(t, repo, model.TaskExecutionStarting)
	stale := seedTaskExecution(t, repo, model.TaskExecutionDispatching)
	terminal := seedTaskExecution(t, repo, model.TaskExecutionSucceeded)
	cutoff := time.Now().UTC().Add(-time.Minute)
	old := cutoff.Add(-time.Minute)
	if err := repo.db.Model(&model.TaskExecution{}).Where("id IN ?", []string{stale.ID, terminal.ID}).Update("updated_at", old).Error; err != nil {
		t.Fatalf("age executions: %v", err)
	}

	if err := repo.TaskExecutions().SetRuntimeIdentity(ctx, refreshed.ID, "agent-system", "agent-job-1", "pod-1"); err != nil {
		t.Fatalf("set runtime identity: %v", err)
	}
	heartbeat := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.TaskExecutions().UpdateHeartbeat(ctx, refreshed.ID, heartbeat); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}
	found, err := repo.TaskExecutions().FindByID(ctx, refreshed.ID)
	if err != nil {
		t.Fatalf("find execution: %v", err)
	}
	if found.Namespace != "agent-system" || found.JobName != "agent-job-1" || found.PodUID != "pod-1" {
		t.Fatalf("runtime identity = %+v", found)
	}
	if found.LastHeartbeatAt == nil || !found.LastHeartbeatAt.Equal(heartbeat) {
		t.Fatalf("last heartbeat = %v, want %v", found.LastHeartbeatAt, heartbeat)
	}

	// The stale active attempt is selected; refreshed and terminal attempts are not.
	reconcilable, err := repo.TaskExecutions().FindReconcilable(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("find reconcilable: %v", err)
	}
	if len(reconcilable) != 1 || reconcilable[0].ID != stale.ID {
		t.Fatalf("reconcilable executions = %+v, want only %s", reconcilable, stale.ID)
	}
}

func TestTaskExecutionRepositoryIsAvailableInTransactions(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	ctx := context.Background()
	executionID := uuid.NewString()

	err := repo.WithTx(ctx, func(txRepo Repository) error {
		if txRepo.TaskExecutions() == nil {
			t.Fatal("TaskExecutions() should not be nil")
		}
		return txRepo.TaskExecutions().Create(ctx, &model.TaskExecution{
			ID: executionID, TaskID: uuid.NewString(), Attempt: 1,
			Target: "kubernetes", Status: model.TaskExecutionCreated,
		})
	})
	if err != nil {
		t.Fatalf("create in transaction: %v", err)
	}
	if _, err := repo.TaskExecutions().FindByID(ctx, executionID); err != nil {
		t.Fatalf("find committed execution: %v", err)
	}

	_, err = repo.TaskExecutions().FindByID(ctx, "missing")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing execution error = %v, want %v", err, gorm.ErrRecordNotFound)
	}
}
