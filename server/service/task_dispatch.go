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

func (s *TaskService) SetKubernetesDispatchLease(now func() time.Time, duration time.Duration) {
	s.dispatchNow = now
	if duration > 0 {
		s.dispatchLeaseDuration = duration
	}
}

func (s *TaskService) kubernetesDispatchNow() time.Time {
	if s.dispatchNow != nil {
		return s.dispatchNow()
	}
	return time.Now()
}

func (s *TaskService) kubernetesDispatchLease() time.Duration {
	if s.dispatchLeaseDuration > 0 {
		return s.dispatchLeaseDuration
	}
	return defaultKubernetesDispatchLease
}

func (s *TaskService) dispatchKubernetes(ctx context.Context, task *model.Task) error {
	execution, created, err := s.createCurrentExecution(ctx, task)
	if err != nil {
		return err
	}
	if !created {
		execution, err = s.repo.TaskExecutions().FindCurrentByTaskID(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("task %s is not pending and has no readable current execution: %w", task.ID, err)
		}
	} else {
		task.Status = model.TaskStatusRunning
	}
	return s.dispatchCurrentExecution(ctx, task, execution)
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

	now := s.kubernetesDispatchNow()
	token := uuid.NewString()
	won, err := s.repo.TaskExecutions().ClaimDispatch(ctx, execution.ID, token, now, now.Add(-s.kubernetesDispatchLease()))
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
	execution.DispatchClaimedAt = &now

	if err := s.kubernetesDispatcher.Dispatch(ctx, execution, task); err != nil {
		return s.failDispatch(ctx, task, execution, token, err)
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
	terminalized := false
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		won, err := txRepo.TaskExecutions().FailDispatch(ctx, execution.ID, token, "dispatch_failed", diagnostics)
		if err != nil {
			return fmt.Errorf("fail task execution: %w", err)
		}
		if !won {
			return nil
		}
		won, err = txRepo.Tasks().FailRunningTask(ctx, task.ID, dispatchErr.Error())
		if err != nil {
			return fmt.Errorf("fail dispatched task: %w", err)
		}
		if !won {
			return fmt.Errorf("fail dispatched task: task %s is no longer running", task.ID)
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

	if task.ProjectID != "" && s.pubsub != nil {
		s.pubsub.ReleaseSlot(ctx, task.ProjectID)
	}
	s.refundTaskByMode(ctx, task, "dispatch_failed")
	s.notifyTerminal(ctx, task, model.TaskStatusFailed, dispatchErr.Error())
	if task.ProjectID != "" {
		if err := s.DispatchPendingTasks(ctx, task.ProjectID); err != nil && s.logger != nil {
			s.logger.Warn().Err(err).Str("project_id", task.ProjectID).
				Msg("failed to dispatch pending tasks after Kubernetes dispatch failure")
		}
	}
	return fmt.Errorf("dispatch Kubernetes execution: %w", dispatchErr)
}
