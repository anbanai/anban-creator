package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var errPreStartReplacementContended = fmt.Errorf("pre-start replacement lost concurrent transition")

func (s *TaskService) FindReconcilableExecutions(ctx context.Context, before time.Time, limit int) ([]*model.TaskExecution, error) {
	return s.repo.TaskExecutions().FindReconcilable(ctx, before, limit)
}

func (s *TaskService) RecordExecutionRuntime(ctx context.Context, executionID, podUID string, started bool) error {
	execution, _, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if isTerminalExecution(execution.Status) {
		return nil
	}
	if podUID != "" && execution.PodUID != "" && execution.PodUID != podUID {
		return fmt.Errorf("execution pod identity changed from %s to %s", execution.PodUID, podUID)
	}
	if started {
		won, err := s.repo.TaskExecutions().RecordRuntimeStarted(ctx, executionID, podUID)
		if err != nil {
			return err
		}
		if !won {
			return fmt.Errorf("record execution start lost guarded transition")
		}
		return nil
	}
	if podUID == "" || execution.PodUID == podUID {
		return nil
	}
	return s.repo.TaskExecutions().SetRuntimeIdentity(ctx, executionID, "", "", podUID)
}

func (s *TaskService) ResumeExecutionFinalization(ctx context.Context, executionID string) error {
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return err
	}
	if !isTerminalExecution(execution.Status) {
		return nil
	}
	task, err := s.repo.Tasks().FindByID(ctx, execution.TaskID)
	if err != nil {
		return err
	}
	if task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
		return nil
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

func (s *TaskService) ClaimExecutionCleanup(ctx context.Context, executionID, token string, lease time.Duration) (bool, error) {
	return s.repo.TaskExecutions().ClaimCleanup(ctx, executionID, token, lease)
}

func (s *TaskService) CompleteExecutionCleanup(ctx context.Context, executionID, token string) (bool, error) {
	return s.repo.TaskExecutions().CompleteCleanup(ctx, executionID, token)
}

func (s *TaskService) FailExecutionCleanup(ctx context.Context, executionID, token string, next time.Time) (bool, error) {
	return s.repo.TaskExecutions().FailCleanup(ctx, executionID, token, next)
}

func (s *TaskService) ReleaseExecutionCleanup(ctx context.Context, executionID, token string) error {
	return s.repo.TaskExecutions().ReleaseCleanup(ctx, executionID, token)
}

// ReconcileExecutionFailure either atomically replaces a pre-start attempt or
// sends the current attempt through the normal terminal finalizer. retryLimit
// counts replacements: limit=1 permits attempt 1 to create attempt 2.
func (s *TaskService) ReconcileExecutionFailure(ctx context.Context, executionID, status, reason string, diagnostics []byte, retryLimit int) error {
	execution, task, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if !execution.Started && execution.Attempt <= retryLimit {
		replacement, replaced, err := s.replacePreStartExecution(ctx, task, execution, reason, diagnostics)
		if err != nil {
			return err
		}
		if replaced {
			freshTask, err := s.repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				return err
			}
			return s.dispatchCurrentExecution(ctx, freshTask, replacement)
		}
		latest, reloadErr := s.repo.Tasks().FindByID(ctx, task.ID)
		if reloadErr == nil && latest.CurrentExecutionID != nil && *latest.CurrentExecutionID != execution.ID {
			return nil
		}
	}
	return s.TerminalizeCurrentExecution(ctx, executionID, status, reason, diagnostics)
}

func (s *TaskService) replacePreStartExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution, reason string, diagnostics []byte) (*model.TaskExecution, bool, error) {
	var replacement *model.TaskExecution
	replaced := false
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		currentTask, err := txRepo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			return err
		}
		current, err := txRepo.TaskExecutions().FindByID(ctx, execution.ID)
		if err != nil {
			return err
		}
		if currentTask.Status != model.TaskStatusRunning || currentTask.CurrentExecutionID == nil || *currentTask.CurrentExecutionID != current.ID || current.Started || isTerminalExecution(current.Status) {
			return nil
		}
		if err := txRepo.TaskFiles().DiscardCurrentExecution(ctx, task.ID, current.ID); err != nil {
			return fmt.Errorf("discard failed pre-start artifacts: %w", err)
		}
		encoded, _ := json.Marshal(&agent.ExecutionResult{Success: false, Error: reason, RemoteArtifacts: true})
		won, err := txRepo.TaskExecutions().Transition(ctx, current.ID,
			[]string{model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting}, model.TaskExecutionFailed,
			model.ExecutionTransition{TerminalReason: reason, Diagnostics: diagnostics, Result: encoded, FinalizationStatus: model.TaskExecutionFinalizationDone})
		if err != nil {
			return err
		}
		if !won {
			return errPreStartReplacementContended
		}
		replacement = &model.TaskExecution{
			ID:        uuid.NewString(),
			TaskID:    task.ID,
			Attempt:   current.Attempt + 1,
			Target:    current.Target,
			Status:    model.TaskExecutionCreated,
			Namespace: current.Namespace,
		}
		if err := txRepo.TaskExecutions().Create(ctx, replacement); err != nil {
			return err
		}
		won, err = txRepo.Tasks().SetCurrentExecution(ctx, task.ID, replacement.ID)
		if err != nil {
			return err
		}
		if !won {
			return errPreStartReplacementContended
		}
		replaced = true
		return nil
	})
	if err == errPreStartReplacementContended {
		return nil, false, nil
	}
	return replacement, replaced, err
}
