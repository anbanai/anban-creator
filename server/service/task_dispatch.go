package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

var ErrDispatchInProgress = errors.New("Kubernetes dispatch is already in progress")

const defaultKubernetesDispatchLease = time.Minute

func (s *TaskService) SetKubernetesDispatcher(dispatcher agent.KubernetesDispatcher) {
	s.kubernetesDispatcher = dispatcher
}

func (s *TaskService) SetKubernetesDispatchLease(duration time.Duration) {
	if duration > 0 {
		s.dispatchLeaseDuration = duration
	}
}

func (s *TaskService) kubernetesDispatchLease() time.Duration {
	if s.dispatchLeaseDuration > 0 {
		return s.dispatchLeaseDuration
	}
	return defaultKubernetesDispatchLease
}

func (s *TaskService) dispatchKubernetes(ctx context.Context, task *model.Task) error {
	if s.dispatchBeforeCreate != nil {
		s.dispatchBeforeCreate()
	}
	execution, created, err := s.createCurrentExecution(ctx, task)
	if err != nil {
		return err
	}
	if !created {
		task, execution, err = s.reloadCurrentDispatchState(ctx, task.ID)
		if err != nil {
			return err
		}
	} else {
		task.Status = model.TaskStatusRunning
	}
	return s.dispatchCurrentExecution(ctx, task, execution)
}

func (s *TaskService) reloadCurrentDispatchState(ctx context.Context, taskID string) (*model.Task, *model.TaskExecution, error) {
	var task *model.Task
	var execution *model.TaskExecution
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		var err error
		task, err = txRepo.Tasks().FindByID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("reload contended task %s: %w", taskID, err)
		}
		execution, err = txRepo.TaskExecutions().FindCurrentByTaskID(ctx, taskID)
		if err != nil {
			return fmt.Errorf("task %s has no readable current execution: %w", taskID, err)
		}
		return nil
	})
	return task, execution, err
}

func (s *TaskService) dispatchCurrentExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution) error {
	if task.Status != model.TaskStatusRunning {
		return fmt.Errorf("task %s has current execution %s but task status is %s", task.ID, execution.ID, task.Status)
	}
	switch execution.Status {
	case model.TaskExecutionStarting, model.TaskExecutionRunning:
		return nil
	case model.TaskExecutionCreated, model.TaskExecutionDispatching:
		// Continue below and claim an unowned or expired dispatch.
	default:
		return fmt.Errorf("task %s is running with current execution %s in status %s", task.ID, execution.ID, execution.Status)
	}

	token := uuid.NewString()
	won, err := s.repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, token, s.kubernetesDispatchLease())
	if err != nil {
		return fmt.Errorf("claim Kubernetes dispatch: %w", err)
	}
	if !won {
		latest, findErr := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
		if findErr != nil {
			return fmt.Errorf("reload contended Kubernetes dispatch: %w", findErr)
		}
		if latest.Status == model.TaskExecutionStarting || latest.Status == model.TaskExecutionRunning {
			return nil
		}
		if latest.Status == model.TaskExecutionDispatching {
			return fmt.Errorf("%w: execution %s", ErrDispatchInProgress, execution.ID)
		}
		return fmt.Errorf("Kubernetes dispatch claim lost to execution status %s", latest.Status)
	}
	execution.Status = model.TaskExecutionDispatching
	execution.DispatchClaimToken = token

	if err := s.kubernetesDispatcher.Dispatch(ctx, execution, task); err != nil {
		if agent.IsPermanentDispatchError(err) {
			return s.failDispatch(ctx, task, execution, token, err)
		}
		abandoned, abandonErr := s.repo.TaskExecutions().AbandonDispatch(ctx, execution.ID, token)
		if abandonErr != nil {
			return errors.Join(fmt.Errorf("ambiguous Kubernetes dispatch: %w", err), fmt.Errorf("abandon dispatch claim: %w", abandonErr))
		}
		if !abandoned {
			return errors.Join(fmt.Errorf("ambiguous Kubernetes dispatch: %w", err), fmt.Errorf("abandon dispatch claim: stale execution %s", execution.ID))
		}
		return fmt.Errorf("ambiguous Kubernetes dispatch: %w", err)
	}
	won, err = s.repo.TaskExecutions().CompleteDispatch(ctx, execution.ID, token)
	if err != nil {
		return fmt.Errorf("mark Kubernetes execution starting: %w", err)
	}
	if !won {
		return fmt.Errorf("mark Kubernetes execution starting: stale execution %s", execution.ID)
	}
	return nil
}

func (s *TaskService) createCurrentExecution(ctx context.Context, task *model.Task) (*model.TaskExecution, bool, error) {
	var execution *model.TaskExecution
	created := false
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		swapped, err := txRepo.Tasks().CompareAndSwapStatusAndStartedAt(
			ctx, task.ID, model.TaskStatusPending, model.TaskStatusRunning,
		)
		if err != nil {
			return fmt.Errorf("claim task for Kubernetes dispatch: %w", err)
		}
		if !swapped {
			return nil
		}

		attempt, err := txRepo.TaskExecutions().NextAttempt(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("allocate task execution attempt: %w", err)
		}
		execution = &model.TaskExecution{
			ID:      uuid.NewString(),
			TaskID:  task.ID,
			Attempt: attempt,
			Target:  "kubernetes",
			Status:  model.TaskExecutionCreated,
		}
		if err := txRepo.TaskExecutions().Create(ctx, execution); err != nil {
			return fmt.Errorf("create task execution: %w", err)
		}
		updated, err := txRepo.Tasks().SetCurrentExecution(ctx, task.ID, execution.ID)
		if err != nil {
			return fmt.Errorf("set current task execution: %w", err)
		}
		if !updated {
			return fmt.Errorf("set current task execution: task %s is no longer running", task.ID)
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return execution, created, nil
}

func (s *TaskService) failDispatch(ctx context.Context, task *model.Task, execution *model.TaskExecution, token string, dispatchErr error) error {
	diagnostics, _ := json.Marshal(map[string]string{"error": dispatchErr.Error()})
	executionResult, _ := json.Marshal(&agent.ExecutionResult{
		Success:         false,
		Error:           dispatchErr.Error(),
		RemoteArtifacts: true,
	})
	terminalized := false
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		won, err := txRepo.TaskExecutions().FailDispatch(ctx, execution.ID, token, "dispatch_failed", diagnostics, executionResult)
		if err != nil {
			return fmt.Errorf("fail task execution: %w", err)
		}
		if !won {
			return nil
		}
		terminalized = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("dispatch Kubernetes execution: %w (terminalize: %v)", dispatchErr, err)
	}
	if !terminalized {
		return fmt.Errorf("dispatch Kubernetes execution: %w", dispatchErr)
	}

	execution.Status = model.TaskExecutionFailed
	execution.TerminalReason = "dispatch_failed"
	execution.Diagnostics = diagnostics
	execution.Result = executionResult
	execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	execution.CleanupStatus = model.TaskExecutionCleanupPending
	if finalizeErr := s.finalizeTaskFromExecution(ctx, task, execution); finalizeErr != nil {
		return errors.Join(
			fmt.Errorf("dispatch Kubernetes execution: %w", dispatchErr),
			fmt.Errorf("finalize failed Kubernetes dispatch: %w", finalizeErr),
		)
	}
	return fmt.Errorf("dispatch Kubernetes execution: %w", dispatchErr)
}
