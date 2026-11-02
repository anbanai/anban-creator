package service

import (
	"context"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

func supersedeAttempt(t *testing.T, repo repository.Repository, task *model.Task, attempt int) *model.TaskExecution {
	t.Helper()
	ctx := context.Background()
	next := &model.TaskExecution{
		ID: uuid.NewString(), TaskID: task.ID, Attempt: attempt, Target: "docker",
		Status: model.TaskExecutionRunning, Started: true,
	}
	if err := repo.TaskExecutions().Create(ctx, next); err != nil {
		t.Fatalf("create superseding execution: %v", err)
	}
	won, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, next.ID)
	if err != nil || !won {
		t.Fatalf("switch current execution: won=%v err=%v", won, err)
	}
	task.CurrentExecutionID = &next.ID
	return next
}

func TestResumeExecutionFinalizationReportsSupersededAttempt(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	ctx := context.Background()
	won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
		model.ExecutionTransition{TerminalReason: "old_attempt_failed"})
	if err != nil || !won {
		t.Fatalf("terminalize superseded attempt: won=%v err=%v", won, err)
	}
	supersedeAttempt(t, repo, task, 2)

	if err := svc.ResumeExecutionFinalization(ctx, execution.ID); !errors.Is(err, agent.ErrExecutionNotCurrent) {
		t.Fatalf("resume finalization error = %v, want %v", err, agent.ErrExecutionNotCurrent)
	}
}

func TestReconcileServiceReportsSupersededActiveAttempt(t *testing.T) {
	svc, repo, task, execution := setupCloudCompletionTest(t, false)
	ctx := context.Background()
	supersedeAttempt(t, repo, task, 2)

	for name, call := range map[string]func() error{
		"record instance":   func() error { return svc.RecordExecutionInstance(ctx, execution.ID, "pod-1") },
		"resume dispatch":   func() error { return svc.ResumeExecutionDispatch(ctx, execution.ID) },
		"reconcile failure": func() error { return svc.ReconcileExecutionFailure(ctx, execution.ID, model.TaskExecutionFailed, "runtime_missing", nil) },
	} {
		if err := call(); !errors.Is(err, agent.ErrExecutionNotCurrent) {
			t.Fatalf("%s error = %v, want %v", name, err, agent.ErrExecutionNotCurrent)
		}
	}
}

func TestRetireSupersededExecution(t *testing.T) {
	ctx := context.Background()

	t.Run("terminal attempt keeps its recorded outcome", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, false)
		oldResult := []byte(`{"success":false,"error":"old attempt failed"}`)
		won, err := repo.TaskExecutions().Transition(ctx, execution.ID,
			[]string{model.TaskExecutionRunning}, model.TaskExecutionFailed,
			model.ExecutionTransition{TerminalReason: "old_attempt_failed", Result: oldResult})
		if err != nil || !won {
			t.Fatalf("terminalize superseded attempt: won=%v err=%v", won, err)
		}
		next := supersedeAttempt(t, repo, task, 2)

		retired, err := svc.RetireSupersededExecution(ctx, execution.ID)
		if err != nil || !retired {
			t.Fatalf("retire superseded execution = %v, %v; want true", retired, err)
		}
		old, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		if old.Status != model.TaskExecutionFailed || old.TerminalReason != "old_attempt_failed" ||
			old.FinalizationStatus != model.TaskExecutionFinalizationSuperseded || string(old.Result) != string(oldResult) {
			t.Fatalf("retired attempt changed its outcome: %#v", old)
		}
		gotTask, err := repo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if gotTask.Status != model.TaskStatusRunning || gotTask.CurrentExecutionID == nil ||
			*gotTask.CurrentExecutionID != next.ID || gotTask.Result != nil || gotTask.ErrorMessage != "" {
			t.Fatalf("retirement mutated the task: %#v", gotTask)
		}
		if retired, err := svc.RetireSupersededExecution(ctx, execution.ID); err != nil || retired {
			t.Fatalf("second retire = %v, %v; want false", retired, err)
		}
	})

	t.Run("active attempt is cancelled for cleanup", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, false)
		next := supersedeAttempt(t, repo, task, 2)

		retired, err := svc.RetireSupersededExecution(ctx, execution.ID)
		if err != nil || !retired {
			t.Fatalf("retire superseded execution = %v, %v; want true", retired, err)
		}
		old, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		if old.Status != model.TaskExecutionCancelled || old.TerminalReason != "superseded" ||
			old.FinalizationStatus != model.TaskExecutionFinalizationSuperseded ||
			old.CleanupStatus != model.TaskExecutionCleanupPending || old.CompletedAt == nil || old.Result != nil {
			t.Fatalf("retired active attempt = %#v", old)
		}
		gotTask, err := repo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if gotTask.CurrentExecutionID == nil || *gotTask.CurrentExecutionID != next.ID {
			t.Fatalf("current execution = %v, want %s", gotTask.CurrentExecutionID, next.ID)
		}
	})

	t.Run("current attempt is never retired", func(t *testing.T) {
		svc, repo, task, execution := setupCloudCompletionTest(t, false)
		retired, err := svc.RetireSupersededExecution(ctx, execution.ID)
		if err != nil || retired {
			t.Fatalf("retire current execution = %v, %v; want false", retired, err)
		}
		got, err := repo.TaskExecutions().FindByID(ctx, execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != model.TaskExecutionRunning || got.FinalizationStatus != "" {
			t.Fatalf("current attempt changed: %#v", got)
		}
		gotTask, err := repo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if gotTask.CurrentExecutionID == nil || *gotTask.CurrentExecutionID != execution.ID {
			t.Fatalf("current execution changed to %v", gotTask.CurrentExecutionID)
		}
	})

	t.Run("unknown attempt is a no-op", func(t *testing.T) {
		svc, _, _, _ := setupCloudCompletionTest(t, false)
		retired, err := svc.RetireSupersededExecution(ctx, uuid.NewString())
		if err != nil || retired {
			t.Fatalf("retire unknown execution = %v, %v; want false, nil", retired, err)
		}
	})
}
