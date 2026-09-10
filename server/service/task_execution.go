package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
)

var ErrExecutionTerminalPersistence = errors.New("execution terminal persistence failed")

const (
	pendingFailurePersistenceAttempts   = 3
	pendingFailurePersistenceRetryDelay = 50 * time.Millisecond
)

type pendingExecutionPreparation uint8

const (
	pendingExecutionSkipped pendingExecutionPreparation = iota
	pendingExecutionReady
	pendingExecutionTerminalized
)

func validateMontageCompletionArtifacts(files []*model.TaskFile) agent.ArtifactValidation {
	required := []string{
		"output/final.mp4",
		"output/montage-project.json",
		"output/cover.png",
		"output/delivery-manifest.json",
	}
	requiredSet := make(map[string]bool, len(required))
	for _, path := range required {
		requiredSet[path] = true
	}
	present := make(map[string]bool, len(required))
	meaningful := 0
	for _, file := range files {
		if file == nil || file.FileSize <= 0 ||
			(file.State != model.TaskFileStatePending && file.State != model.TaskFileStatePublished) {
			continue
		}
		path := filepath.ToSlash(strings.TrimSpace(file.FilePath))
		if !present[path] && requiredSet[path] {
			present[path] = true
			meaningful++
		}
	}
	var missing []string
	for _, path := range required {
		if !present[path] {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		return agent.ArtifactValidation{
			MeaningfulFileCount: meaningful,
			Missing:             missing,
			Reason:              "montage missing required deliverables: " + strings.Join(missing, ", "),
		}
	}
	return agent.ArtifactValidation{Valid: true, MeaningfulFileCount: meaningful}
}

// HandleExecutionFromPayload loads a queued task and starts its durable managed
// execution through the configured runtime dispatcher.
func (s *TaskService) HandleExecutionFromPayload(ctx context.Context, taskID, _ string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task %s: %w", taskID, err)
	}
	if model.IsTerminalTaskStatus(task.Status) {
		return nil
	}
	_, preparation, err := s.preparePendingExecution(ctx, task)
	if err != nil || preparation != pendingExecutionReady {
		return err
	}
	return s.dispatchPendingTask(ctx, task)
}

func (s *TaskService) dispatchPendingTask(ctx context.Context, task *model.Task) error {
	if err := s.dispatchRuntime(ctx, task); err != nil {
		if errors.Is(err, ErrTaskPlanPaused) {
			return nil
		}
		return s.finalizePendingDispatchFailure(task, err)
	}
	return nil
}

func (s *TaskService) finalizePendingDispatchFailure(task *model.Task, dispatchErr error) error {
	wrapped := fmt.Errorf("dispatch runtime: %w", dispatchErr)
	persistCtx, cancel := context.WithTimeout(context.Background(), s.persistTimeout)
	defer cancel()
	won, failErr := s.failPendingAdmittedTaskTransitionWithRetry(persistCtx, task, model.TaskBillingTerminalPlatformError, wrapped.Error())
	if failErr != nil {
		return errors.Join(wrapped, failErr)
	}
	if !won {
		return dispatchErr
	}
	s.logger.Error().Err(wrapped).Str("task_id", task.ID).Msg("pending task runtime dispatch failed")
	return s.finalizeFailedExecutionPostCommit(persistCtx, task, wrapped.Error())
}

func (s *TaskService) preparePendingExecution(ctx context.Context, task *model.Task) (*model.Asset, pendingExecutionPreparation, error) {
	referenceAsset, err := resolveEffectiveReferenceAsset(ctx, s.repo, task)
	if err == nil {
		return referenceAsset, pendingExecutionReady, nil
	}
	wrapped := fmt.Errorf("resolve reference asset: %w", err)
	persistCtx, cancel := context.WithTimeout(context.Background(), s.persistTimeout)
	defer cancel()
	won, failErr := s.failPendingAdmittedTaskTransitionWithRetry(persistCtx, task, model.TaskBillingTerminalPlatformError, wrapped.Error())
	if failErr != nil {
		return nil, pendingExecutionSkipped, failErr
	}
	if !won {
		return nil, pendingExecutionSkipped, nil
	}
	s.logger.Error().Err(wrapped).Str("task_id", task.ID).Msg("pending task reference asset resolution failed")
	if err := s.finalizeFailedExecutionPostCommit(persistCtx, task, wrapped.Error()); err != nil {
		return nil, pendingExecutionTerminalized, err
	}
	return nil, pendingExecutionTerminalized, nil
}

func (s *TaskService) failPendingAdmittedTaskTransitionWithRetry(
	ctx context.Context,
	task *model.Task,
	reason, message string,
) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < pendingFailurePersistenceAttempts; attempt++ {
		won, err := s.failPendingAdmittedTaskTransition(ctx, task, reason, message)
		if err == nil {
			if won || lastErr == nil {
				return won, nil
			}
			committed, verifyErr := s.verifyPendingFailureCommitted(ctx, task, reason, message)
			if verifyErr != nil {
				return false, errors.Join(lastErr, verifyErr)
			}
			return committed, nil
		}
		lastErr = err
		if attempt == pendingFailurePersistenceAttempts-1 {
			break
		}
		timer := time.NewTimer(pendingFailurePersistenceRetryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false, errors.Join(lastErr, ctx.Err())
		case <-timer.C:
		}
	}
	return false, lastErr
}

func (s *TaskService) verifyPendingFailureCommitted(ctx context.Context, task *model.Task, reason, message string) (bool, error) {
	found, err := s.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		return false, err
	}
	if found.Status != model.TaskStatusFailed || found.BillingTerminalReason != reason || found.ErrorMessage != message {
		return false, nil
	}
	if found.BillingChargeID != nil && ShouldReverseTaskCharge(reason, false) {
		chargeID := strings.TrimSpace(*found.BillingChargeID)
		if chargeID != "" {
			if _, err := s.repo.Billing().FindReversal(ctx, chargeID); err == nil {
				return true, nil
			}
			settlement, err := s.repo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", chargeID)
			if err != nil {
				return false, err
			}
			if settlement.Action != model.BillingSettlementActionReverseTask || settlement.Reason != reason {
				return false, fmt.Errorf("pending failure reversal settlement does not match terminal state")
			}
		}
	}
	return true, nil
}

func (s *TaskService) finalizeFailedExecutionPostCommit(ctx context.Context, task *model.Task, errorMsg string) error {
	if task.ProjectID != "" && s.pubsub != nil {
		running, err := s.repo.Tasks().CountRunningByProject(ctx, task.ProjectID)
		if err != nil {
			return fmt.Errorf("count running tasks after failure: %w", err)
		}
		if err := s.pubsub.SyncProjectCount(ctx, task.ProjectID, running); err != nil {
			return fmt.Errorf("sync project slot after failure: %w", err)
		}
	}
	if err := s.notifyTerminalDurable(ctx, task, model.TaskStatusFailed, errorMsg); err != nil {
		return fmt.Errorf("notify terminal task failure: %w", err)
	}
	if task.ProjectID != "" {
		if err := s.DispatchPendingTasks(ctx, task.ProjectID); err != nil {
			return fmt.Errorf("dispatch pending tasks after failure: %w", err)
		}
	}
	return nil
}

// generateTaskID generates a unique task ID using UUID v4.
func generateTaskID() string {
	return uuid.New().String()
}
