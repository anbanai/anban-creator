package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type taskWorkspaceLifecycleFake struct {
	tasks []*model.Task
	err   error
}

func (f *taskWorkspaceLifecycleFake) DeleteTaskWorkspace(_ context.Context, task *model.Task) error {
	f.tasks = append(f.tasks, task)
	return f.err
}

func TestTaskDeleteRemovesDeterministicWorkspacePVC(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	workspace := &taskWorkspaceLifecycleFake{}
	svc.SetTaskWorkspaceLifecycle(workspace)
	ctx := context.Background()
	task := &model.Task{ID: uuid.NewString(), UserID: uuid.NewString(), ProjectID: uuid.NewString(), Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if len(workspace.tasks) != 1 || workspace.tasks[0].ID != task.ID || workspace.tasks[0].ProjectID != task.ProjectID || workspace.tasks[0].UserID != task.UserID {
		t.Fatalf("workspace deletions = %#v", workspace.tasks)
	}
}

func TestTaskDeleteSurfacesWorkspaceIdentityMismatch(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	svc.SetTaskWorkspaceLifecycle(&taskWorkspaceLifecycleFake{err: errors.New("PVC identity mismatch")})
	ctx := context.Background()
	task := &model.Task{ID: uuid.NewString(), UserID: uuid.NewString(), ProjectID: uuid.NewString(), Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	err := svc.Delete(ctx, task.ID)
	if err == nil || !strings.Contains(err.Error(), "PVC identity mismatch") {
		t.Fatalf("Delete error = %v, want workspace identity mismatch", err)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); err != nil {
		t.Fatalf("task was deleted after workspace ownership failure: %v", err)
	}
}

func TestTaskDeleteAbortsWhileRuntimePreparationIsInFlight(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
	ctx := context.Background()
	if won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionDispatching, model.ExecutionTransition{}); err != nil || !won {
		t.Fatalf("move execution to dispatching: won=%v err=%v", won, err)
	}
	if won, err := repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, "delete-in-flight-prepare", time.Minute); err != nil || !won {
		t.Fatalf("claim dispatch: won=%v err=%v", won, err)
	}
	svc.SetRuntimeDispatchLease(time.Minute)
	dispatcher := &cancelOrderingDispatcher{repo: repo}
	svc.SetRuntimeDispatcher(dispatcher)
	workspace := &taskWorkspaceLifecycleFake{}
	svc.SetTaskWorkspaceLifecycle(workspace)

	err := svc.Delete(ctx, task.ID)
	if !errors.Is(err, ErrRuntimePreparationInFlight) {
		t.Fatalf("Delete error = %v, want ErrRuntimePreparationInFlight", err)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); err != nil {
		t.Fatalf("task authority removed after prepare barrier: %v", err)
	}
	if _, err := repo.TaskExecutions().FindByID(ctx, execution.ID); err != nil {
		t.Fatalf("execution authority removed after prepare barrier: %v", err)
	}
	if len(workspace.tasks) != 0 {
		t.Fatalf("workspace deleted before runtime preparation settled: %#v", workspace.tasks)
	}
}

func TestTaskDeleteRetriesCancelledRuntimeCleanupBeforeRemovingAuthority(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	ctx := context.Background()
	dispatcher := &cancelOrderingDispatcher{repo: repo, deleteErr: errors.New("runtime provider unavailable")}
	svc.SetRuntimeDispatcher(dispatcher)
	svc.cleanupRetryBackoff = time.Millisecond
	workspace := &taskWorkspaceLifecycleFake{}
	svc.SetTaskWorkspaceLifecycle(workspace)

	err := svc.Delete(ctx, task.ID)
	if err == nil || !strings.Contains(err.Error(), "runtime provider unavailable") {
		t.Fatalf("first Delete error = %v, want runtime cleanup failure", err)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); err != nil {
		t.Fatalf("task authority removed after runtime cleanup failure: %v", err)
	}
	foundExecution, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		t.Fatalf("execution authority removed after runtime cleanup failure: %v", err)
	}
	if foundExecution.CleanupStatus != model.TaskExecutionCleanupPending {
		t.Fatalf("cleanup status = %q, want pending", foundExecution.CleanupStatus)
	}
	if len(workspace.tasks) != 0 {
		t.Fatalf("workspace deleted after runtime cleanup failure: %#v", workspace.tasks)
	}

	dispatcher.deleteErr = nil
	time.Sleep(2 * time.Millisecond)
	if err := svc.Delete(ctx, task.ID); err != nil {
		t.Fatalf("retry Delete: %v", err)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); err == nil {
		t.Fatal("task still exists after successful runtime cleanup retry")
	}
	if len(workspace.tasks) != 1 || workspace.tasks[0].ID != task.ID {
		t.Fatalf("workspace deletions after retry = %#v", workspace.tasks)
	}
}

func TestTaskDeleteWaitsForFailedRuntimeCleanupBeforeRemovingAuthority(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	ctx := context.Background()
	if won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
		model.ExecutionTransition{
			FinalizationStatus: model.TaskExecutionFinalizationDone,
			CleanupStatus:      model.TaskExecutionCleanupPending,
		}); err != nil || !won {
		t.Fatalf("fail execution: won=%v err=%v", won, err)
	}
	if err := repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusFailed); err != nil {
		t.Fatalf("fail task: %v", err)
	}
	svc.SetRuntimeDispatcher(&cancelOrderingDispatcher{repo: repo})
	workspace := &taskWorkspaceLifecycleFake{}
	svc.SetTaskWorkspaceLifecycle(workspace)

	err := svc.Delete(ctx, task.ID)
	if err == nil || !strings.Contains(err.Error(), "runtime cleanup is not complete") {
		t.Fatalf("Delete error = %v, want incomplete runtime cleanup", err)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); err != nil {
		t.Fatalf("task authority removed while failed runtime cleanup is pending: %v", err)
	}
	if _, err := repo.TaskExecutions().FindByID(ctx, execution.ID); err != nil {
		t.Fatalf("execution authority removed while failed runtime cleanup is pending: %v", err)
	}
	if len(workspace.tasks) != 0 {
		t.Fatalf("workspace deleted while failed runtime cleanup is pending: %#v", workspace.tasks)
	}

	const token = "delete-after-reconcile"
	if won, err := repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, token, time.Minute); err != nil || !won {
		t.Fatalf("claim cleanup: won=%v err=%v", won, err)
	}
	if won, err := repo.TaskExecutions().CompleteCleanup(ctx, execution.ID, token); err != nil || !won {
		t.Fatalf("complete cleanup: won=%v err=%v", won, err)
	}
	if err := svc.Delete(ctx, task.ID); err != nil {
		t.Fatalf("Delete after reconciled cleanup: %v", err)
	}
	if len(workspace.tasks) != 1 || workspace.tasks[0].ID != task.ID {
		t.Fatalf("workspace deletions after cleanup = %#v", workspace.tasks)
	}
}

func TestTaskDeleteDoesNotBypassManagedCleanupWithoutDispatcher(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	ctx := context.Background()
	if won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionCancelled,
		model.ExecutionTransition{
			FinalizationStatus: model.TaskExecutionFinalizationDone,
			CleanupStatus:      model.TaskExecutionCleanupPending,
		}); err != nil || !won {
		t.Fatalf("terminalize execution: won=%v err=%v", won, err)
	}
	workspace := &taskWorkspaceLifecycleFake{}
	svc.SetTaskWorkspaceLifecycle(workspace)

	err := svc.Delete(ctx, task.ID)
	if err == nil || !strings.Contains(err.Error(), "runtime cleanup is not complete") {
		t.Fatalf("Delete error = %v, want incomplete runtime cleanup", err)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); err != nil {
		t.Fatalf("task authority removed without runtime dispatcher: %v", err)
	}
	if _, err := repo.TaskExecutions().FindByID(ctx, execution.ID); err != nil {
		t.Fatalf("execution authority removed without runtime dispatcher: %v", err)
	}
	if len(workspace.tasks) != 0 {
		t.Fatalf("workspace deleted without runtime dispatcher: %#v", workspace.tasks)
	}

	svc.SetRuntimeDispatcher(&cancelOrderingDispatcher{repo: repo})
	if err := svc.Delete(ctx, task.ID); err != nil {
		t.Fatalf("Delete after dispatcher recovery: %v", err)
	}
}

func TestTaskDeletePreservesManagedFinalizationAuthority(t *testing.T) {
	tests := []struct {
		name               string
		finalizationStatus string
		claimFinalization  bool
	}{
		{
			name:               "active finalization lease",
			finalizationStatus: model.TaskExecutionFinalizationSettlement,
			claimFinalization:  true,
		},
		{
			name:               "interrupted finalization without lease",
			finalizationStatus: model.TaskExecutionFinalizationNotification,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, false)
			ctx := context.Background()
			if won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
				[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
				model.ExecutionTransition{
					FinalizationStatus: tt.finalizationStatus,
					CleanupStatus:      model.TaskExecutionCleanupDone,
				}); err != nil || !won {
				t.Fatalf("terminalize execution: won=%v err=%v", won, err)
			}
			if err := repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusFailed); err != nil {
				t.Fatalf("fail task: %v", err)
			}
			finalizationToken := ""
			if tt.claimFinalization {
				finalizationToken = uuid.NewString()
				if won, err := repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, finalizationToken, time.Minute); err != nil || !won {
					t.Fatalf("claim finalization: won=%v err=%v", won, err)
				}
			}

			err := svc.Delete(ctx, task.ID)
			if err == nil || !strings.Contains(err.Error(), "runtime finalization is not complete") {
				t.Fatalf("Delete error = %v, want incomplete runtime finalization", err)
			}
			retainedTask, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatalf("task authority removed before finalization completed: %v", err)
			}
			if retainedTask.DeletingAt == nil {
				t.Fatal("deletion barrier was not retained for finalization retry")
			}
			if _, err := repo.TaskExecutions().FindByID(ctx, execution.ID); err != nil {
				t.Fatalf("execution authority removed before finalization completed: %v", err)
			}

			if finalizationToken == "" {
				finalizationToken = uuid.NewString()
				if won, err := repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, finalizationToken, time.Minute); err != nil || !won {
					t.Fatalf("claim interrupted finalization: won=%v err=%v", won, err)
				}
			}
			if won, err := repo.TaskExecutions().AdvanceFinalization(ctx, execution.ID, finalizationToken, tt.finalizationStatus, model.TaskExecutionFinalizationDone); err != nil || !won {
				t.Fatalf("complete finalization: won=%v err=%v", won, err)
			}
			if err := svc.Delete(ctx, task.ID); err != nil {
				t.Fatalf("Delete after finalization completed: %v", err)
			}
			if _, err := repo.Tasks().FindByID(ctx, task.ID); err == nil {
				t.Fatal("task authority remains after completed finalization retry")
			}
		})
	}
}
