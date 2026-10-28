package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

var (
	ErrTaskRetryCompleted   = errors.New("completed tasks cannot be retried")
	ErrTaskRetryNotTerminal = errors.New("only failed or cancelled tasks can be retried")
	ErrTaskRetryConflict    = errors.New("task retry conflict")
)

// Retry requeues a failed or cancelled task in place, preserving its original
// inputs, frozen configuration, task ID, and billing identity.
func (s *TaskService) Retry(ctx context.Context, userID, taskID string) (*model.Task, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("task not found")
	}
	if task.DeletingAt != nil {
		return nil, ErrTaskRetryNotTerminal
	}
	if task.Status == model.TaskStatusCompleted {
		return nil, ErrTaskRetryCompleted
	}
	if task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled {
		return nil, ErrTaskRetryNotTerminal
	}
	if err := validateTaskIdentity(task); err != nil {
		return nil, err
	}
	if taskUsesFrozenImageCapability(task.Type) {
		if strings.TrimSpace(task.ImageCapabilityKey) == "" || task.ImageCapabilitySnapshot.Data().Key != strings.TrimSpace(task.ImageCapabilityKey) {
			return nil, ErrTaskResumeImageCapabilityMissing
		}
		if s.imageCapabilities == nil {
			return nil, fmt.Errorf("revalidate frozen task image capability: %w", ErrImageCapabilityResolverUnavailable)
		}
		if err := s.imageCapabilities.ValidateFrozenImageCapability(ctx, task.UserID, task.ImageCapabilitySnapshot.Data()); err != nil {
			return nil, fmt.Errorf("revalidate frozen task image capability: %w", err)
		}
	}
	if err := s.validateFrozenTaskProfileRuntime(task); err != nil {
		return nil, err
	}

	attachments := task.InputAttachments.Data()
	cleanAttachments := make([]model.EntryAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if !model.IsResumeEntryAttachment(attachment) {
			cleanAttachments = append(cleanAttachments, attachment)
		}
	}
	swapped, err := s.repo.Tasks().ResetRetryableTask(ctx, task.ID, cleanAttachments, true)
	if err != nil {
		return nil, fmt.Errorf("reset task for retry: %w", err)
	}
	if !swapped {
		return nil, ErrTaskRetryConflict
	}
	s.deleteRemoteResumeAttachments(ctx, attachments)
	applyResumedTaskState(task, cleanAttachments)
	task.Lifecycle = datatypes.NewJSONType(model.TaskLifecycle{})
	if err := s.EnqueueExecution(ctx, task, nil); err != nil {
		errMsg := "failed to enqueue retried task: " + err.Error()
		recoveryTimeout := s.persistTimeout
		if recoveryTimeout <= 0 {
			recoveryTimeout = 15 * time.Second
		}
		recoveryCtx, cancel := context.WithTimeout(context.Background(), recoveryTimeout)
		defer cancel()
		if updateErr := s.failPendingAdmittedTask(recoveryCtx, task, model.TaskBillingTerminalPlatformError, errMsg); updateErr != nil {
			s.logger.Error().Err(updateErr).Str("task_id", task.ID).Msg("failed to recover unqueued retried task")
		}
		if task.ProjectID != "" && s.pubsub != nil {
			s.pubsub.ReleaseSlot(recoveryCtx, task.ProjectID)
		}
		return nil, fmt.Errorf("enqueue retried task: %w", err)
	}
	return task, nil
}
