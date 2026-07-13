package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupCloudCompletionTest(t *testing.T, withArtifact bool, startedOverride ...bool) (*TaskService, repository.Repository, *model.Task, *model.TaskExecution) {
	t.Helper()
	db := setupTaskTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, nil, nil, &logger, "", nil, "", nil, nil)
	task := &model.Task{ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformArticle, Status: model.TaskStatusRunning}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	started := true
	if len(startedOverride) > 0 {
		started = startedOverride[0]
	}
	executionStatus := model.TaskExecutionRunning
	if !started {
		executionStatus = model.TaskExecutionStarting
	}
	execution := &model.TaskExecution{ID: uuid.NewString(), TaskID: task.ID, Attempt: 1, Target: "kubernetes", Status: executionStatus, Started: started}
	if withArtifact {
		execution.ManifestStatus = model.TaskExecutionManifestPending
	}
	if err := repo.TaskExecutions().Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(context.Background(), task.ID, execution.ID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}
	task.CurrentExecutionID = &execution.ID
	if withArtifact {
		if err := repo.TaskFiles().BatchCreate(context.Background(), []*model.TaskFile{{
			ID: uuid.NewString(), TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
			Role: "content", FilePath: "output/content.md", FileName: "content.md", FileSize: 8,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	return svc, repo, task, execution
}

func TestCompleteCloudExecutionCurrentAttemptAndDuplicate(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	old := &model.TaskExecution{ID: uuid.NewString(), TaskID: task.ID, Attempt: 0, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true}
	if err := repo.TaskExecutions().Create(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	if err := svc.CompleteCloudExecution(context.Background(), old.ID, result); !errors.Is(err, ErrStaleTaskExecution) {
		t.Fatalf("stale completion error = %v", err)
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatalf("duplicate completion: %v", err)
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone || len(files) != 1 {
		t.Fatalf("task=%s execution=%s files=%d", foundTask.Status, foundExecution.FinalizationStatus, len(files))
	}
}

func TestCompleteCloudExecutionRejectsSuccessWithoutManifest(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, &agent.ExecutionResult{Success: true, RemoteArtifacts: true}); err != nil {
		t.Fatal(err)
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if foundTask.Status != model.TaskStatusFailed || foundExecution.Status != model.TaskExecutionFailed || foundExecution.ManifestStatus != model.TaskExecutionManifestDiscarded {
		t.Fatalf("task=%s execution=%s manifest=%s", foundTask.Status, foundExecution.Status, foundExecution.ManifestStatus)
	}
}

func TestCompleteCloudExecutionConcurrentDuplicate(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.CompleteCloudExecution(context.Background(), execution.ID, result)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
		t.Fatal(err)
	}
	found, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if found.FinalizationStatus != model.TaskExecutionFinalizationDone || len(files) != 1 {
		t.Fatalf("finalization=%s files=%d", found.FinalizationStatus, len(files))
	}
}

func TestCompleteCloudExecutionResumesEveryDurableStage(t *testing.T) {
	for _, stage := range []string{
		model.TaskExecutionFinalizationTerminal,
		model.TaskExecutionFinalizationArtifacts,
		model.TaskExecutionFinalizationTask,
	} {
		t.Run(stage, func(t *testing.T) {
			svc, repo, task, execution := setupCloudCompletionTest(t, true)
			injected := false
			svc.finalizationAfterStage = func(got string) error {
				if got == stage && !injected {
					injected = true
					return errors.New("injected finalization failure")
				}
				return nil
			}
			result := &agent.ExecutionResult{Success: true, RemoteArtifacts: true}
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err == nil {
				t.Fatal("expected injected failure")
			}
			svc.finalizationAfterStage = nil
			if err := svc.CompleteCloudExecution(context.Background(), execution.ID, result); err != nil {
				t.Fatalf("resume: %v", err)
			}
			foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
			foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
			if foundTask.Status != model.TaskStatusCompleted || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone {
				t.Fatalf("task=%s finalization=%s", foundTask.Status, foundExecution.FinalizationStatus)
			}
		})
	}
}

type cancelOrderingDispatcher struct {
	repo           repository.Repository
	statusAtDelete string
	deleteErr      error
}

func (*cancelOrderingDispatcher) Dispatch(context.Context, *model.TaskExecution, *model.Task) error {
	return nil
}
func (d *cancelOrderingDispatcher) Delete(_ context.Context, execution *model.TaskExecution) error {
	found, _ := d.repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	d.statusAtDelete = found.Status
	return d.deleteErr
}
func (*cancelOrderingDispatcher) DeleteProjectMemory(context.Context, string) error { return nil }
func (*cancelOrderingDispatcher) Inspect(context.Context, *model.TaskExecution) (*agent.KubernetesExecutionState, error) {
	return nil, nil
}

func TestCancelCloudMarksAttemptBeforeDeleteAndDoesNotRollBack(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true)
	dispatcher := &cancelOrderingDispatcher{repo: repo, deleteErr: errors.New("delete unavailable")}
	svc.SetKubernetesDispatcher(dispatcher)
	err := svc.CancelForUser(context.Background(), task.UserID, task.ID)
	if err == nil {
		t.Fatal("expected delete error")
	}
	foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
	foundExecution, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
	if dispatcher.statusAtDelete != model.TaskExecutionCancelled || foundTask.Status != model.TaskStatusCancelled || foundExecution.Status != model.TaskExecutionCancelled {
		t.Fatalf("delete=%s task=%s execution=%s", dispatcher.statusAtDelete, foundTask.Status, foundExecution.Status)
	}
	if foundExecution.CompletedAt == nil || time.Since(*foundExecution.CompletedAt) > time.Minute {
		t.Fatalf("completed_at = %v", foundExecution.CompletedAt)
	}
}

func TestReconcileExecutionFailureRetriesOnlyPreStart(t *testing.T) {
	t.Run("one configured replacement", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
		dispatcher := &dispatchTestDispatcher{}
		svc.SetKubernetesDispatcher(dispatcher)
		if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "image_pull_failed", nil, 1); err != nil {
			t.Fatal(err)
		}
		current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		old, _ := repo.TaskExecutions().FindByID(context.Background(), execution.ID)
		foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
		if current.Attempt != 2 || current.Status != model.TaskExecutionStarting || old.Status != model.TaskExecutionFailed || foundTask.Status != model.TaskStatusRunning {
			t.Fatalf("current=%+v old=%s task=%s", current, old.Status, foundTask.Status)
		}
	})

	t.Run("post-start enters terminal finalizer", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, true, true)
		dispatcher := &dispatchTestDispatcher{}
		svc.SetKubernetesDispatcher(dispatcher)
		if err := svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "job_failed", nil, 1); err != nil {
			t.Fatal(err)
		}
		current, _ := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
		foundTask, _ := repo.Tasks().FindByID(context.Background(), task.ID)
		if current.ID != execution.ID || current.Status != model.TaskExecutionFailed || foundTask.Status != model.TaskStatusFailed || dispatcher.callCount() != 0 {
			t.Fatalf("current=%+v task=%s dispatches=%d", current, foundTask.Status, dispatcher.callCount())
		}
	})
}

func TestConcurrentPreStartReconcileCreatesOneReplacement(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, true, false)
	dispatcher := &dispatchTestDispatcher{}
	svc.SetKubernetesDispatcher(dispatcher)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.ReconcileExecutionFailure(context.Background(), execution.ID, model.TaskExecutionFailed, "scheduling_failed", nil, 1)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrStaleTaskExecution) {
			t.Fatal(err)
		}
	}
	current, err := repo.TaskExecutions().FindCurrentByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := repo.TaskExecutions().NextAttempt(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Attempt != 2 || next != 3 || dispatcher.createCount() != 1 {
		t.Fatalf("current attempt=%d next=%d jobs=%d", current.Attempt, next, dispatcher.createCount())
	}
}
