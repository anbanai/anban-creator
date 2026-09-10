package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

func ShouldReverseTaskCharge(reason string, durableDelivery bool) bool {
	if durableDelivery {
		return false
	}
	switch strings.TrimSpace(reason) {
	case model.TaskBillingTerminalPlatformError,
		model.TaskBillingTerminalProviderError,
		model.TaskBillingTerminalExecutionTimeout,
		model.TaskBillingTerminalInfrastructureCancelled,
		model.TaskBillingTerminalPlanPaused:
		return true
	default:
		return false
	}
}

func terminalBillingReason(execution *model.TaskExecution, result *agent.ExecutionResult) string {
	if execution == nil {
		return model.TaskBillingTerminalPlatformError
	}
	if execution.Status == model.TaskExecutionSucceeded {
		return model.TaskBillingTerminalCompleted
	}
	if execution.Status == model.TaskExecutionCancelled && execution.TerminalReason == model.TaskBillingTerminalUserCancelled {
		return model.TaskBillingTerminalUserCancelled
	}
	if result != nil && approvedTaskBillingTerminalReason(result.TerminalReason) {
		return result.TerminalReason
	}
	if approvedTaskBillingTerminalReason(execution.TerminalReason) {
		return execution.TerminalReason
	}
	switch execution.Status {
	case model.TaskExecutionTimedOut:
		return model.TaskBillingTerminalExecutionTimeout
	case model.TaskExecutionCancelled:
		return model.TaskBillingTerminalInfrastructureCancelled
	default:
		return model.TaskBillingTerminalPlatformError
	}
}

func approvedTaskBillingTerminalReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case model.TaskBillingTerminalPlatformError,
		model.TaskBillingTerminalProviderError,
		model.TaskBillingTerminalExecutionTimeout,
		model.TaskBillingTerminalInfrastructureCancelled,
		model.TaskBillingTerminalPlanPaused:
		return true
	default:
		return false
	}
}

func (s *TaskService) taskHasDurableDelivery(ctx context.Context, taskID string) (bool, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return false, err
	}
	files, err := s.repo.TaskFiles().FindAllByTaskID(ctx, taskID)
	if err != nil {
		return false, err
	}
	executions := make(map[string]*model.TaskExecution)
	for _, file := range files {
		if file == nil || file.TaskID != taskID || file.State != model.TaskFileStatePublished {
			continue
		}
		executionID := strings.TrimSpace(file.ExecutionID)
		if executionID == "" {
			continue
		}
		execution, resolved := executions[executionID]
		if !resolved {
			execution, err = s.repo.TaskExecutions().FindByID(ctx, executionID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				executions[executionID] = nil
				continue
			}
			if err != nil {
				return false, err
			}
			if execution.TaskID != taskID {
				execution = nil
			}
			executions[executionID] = execution
		}
		if execution == nil {
			continue
		}
		contract, contractErr := resolveFrozenExecutionDeliveryContract(execution)
		if contractErr != nil {
			continue
		}
		spec, matched := agentpack.MatchDeliverySpec(contract, file.FilePath)
		if !matched || normalizedMediaType(file.MimeType) != normalizedMediaType(spec.MIMEType) {
			continue
		}
		if validateErr := s.validateStoredDeliveryObject(ctx, task, execution.ID, file, spec); validateErr == nil {
			return true, nil
		} else if !errors.Is(validateErr, ErrTaskDeliveryObjectInvalid) {
			return false, validateErr
		}
	}
	return false, nil
}

func (s *TaskService) persistTerminalBillingInTx(ctx context.Context, tx repository.Repository, task *model.Task, execution *model.TaskExecution, reason string, durableDelivery bool) error {
	if task == nil || execution == nil {
		return fmt.Errorf("terminal task billing identity is required")
	}
	if err := tx.Tasks().UpdateBillingTerminalReason(ctx, task.ID, reason); err != nil {
		return fmt.Errorf("persist task billing terminal reason: %w", err)
	}
	task.BillingTerminalReason = reason
	if !ShouldReverseTaskCharge(reason, durableDelivery) || task.BillingChargeID == nil || strings.TrimSpace(*task.BillingChargeID) == "" {
		return nil
	}
	if s.billingWalletSvc == nil {
		return fmt.Errorf("fixed task billing wallet is not configured")
	}
	chargeID := strings.TrimSpace(*task.BillingChargeID)
	if _, err := tx.Billing().FindReversal(ctx, chargeID); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if _, err := tx.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", chargeID); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	_, err := s.billingWalletSvc.EnqueueSettlementInTx(ctx, tx, SettlementIntent{
		Action: model.BillingSettlementActionReverseTask,
		UserID: task.UserID, ResourceType: "task", ResourceID: task.ID,
		TaskID: task.ID, AttemptID: execution.ID, ChargeID: chargeID,
		CatalogID: task.BillingCatalogID, SKUID: task.BillingSKUID, Reason: reason,
		RequestFingerprint: billingFingerprint("task-terminal-reversal", task.ID, chargeID, reason),
		IdempotencyScope:   "task-terminal-reversal", IdempotencyKey: chargeID,
	})
	if err != nil {
		return fmt.Errorf("enqueue task charge reversal: %w", err)
	}
	return nil
}

func (s *TaskService) failPendingAdmittedTask(ctx context.Context, task *model.Task, reason, message string) error {
	_, err := s.failPendingAdmittedTaskTransitionWithRetry(ctx, task, reason, message)
	return err
}

func (s *TaskService) failPendingAdmittedTaskTransition(ctx context.Context, task *model.Task, reason, message string) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("task is required")
	}
	var won bool
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var err error
		won, err = tx.Tasks().FailPendingTask(ctx, task.ID, message)
		if err != nil {
			return err
		}
		if !won {
			return nil
		}
		return s.persistTerminalBillingInTx(ctx, tx, task, &model.TaskExecution{}, reason, false)
	})
	return won, err
}

func (s *TaskService) FailRunningTaskForInfrastructure(ctx context.Context, taskID, message string) (bool, error) {
	return s.failRunningTaskWithBilling(ctx, taskID, model.TaskBillingTerminalInfrastructureCancelled, message)
}

func (s *TaskService) failRunningTaskWithBilling(ctx context.Context, taskID, reason, message string) (bool, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return false, err
	}
	durableDelivery, err := s.taskHasDurableDelivery(ctx, taskID)
	if err != nil {
		return false, err
	}
	won, err := s.repo.Tasks().FailRunningTask(ctx, taskID, message)
	if err != nil || !won {
		return won, err
	}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		return s.persistTerminalBillingInTx(ctx, tx, task, &model.TaskExecution{}, reason, durableDelivery)
	})
	return true, err
}
