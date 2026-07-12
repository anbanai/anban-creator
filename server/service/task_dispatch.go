package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func (s *TaskService) SetKubernetesDispatcher(dispatcher agent.KubernetesDispatcher) {
	s.kubernetesDispatcher = dispatcher
}

func (s *TaskService) dispatchKubernetes(ctx context.Context, task *model.Task) error {
	execution, created, err := s.createCurrentExecution(ctx, task)
	if err != nil || !created {
		return err
	}
	if err := s.kubernetesDispatcher.Dispatch(ctx, execution, task); err != nil {
		return s.failDispatch(ctx, task, execution, err)
	}
	won, err := s.repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionDispatching}, model.TaskExecutionStarting,
		model.ExecutionTransition{})
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
			Status:  model.TaskExecutionDispatching,
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

func (s *TaskService) failDispatch(ctx context.Context, task *model.Task, execution *model.TaskExecution, dispatchErr error) error {
	diagnostics, _ := json.Marshal(map[string]string{"error": dispatchErr.Error()})
	terminalized := false
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		won, err := txRepo.TaskExecutions().Transition(ctx, execution.ID,
			[]string{model.TaskExecutionDispatching}, model.TaskExecutionFailed,
			model.ExecutionTransition{
				TerminalReason: "dispatch_failed",
				Diagnostics:    datatypes.JSON(diagnostics),
			})
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
