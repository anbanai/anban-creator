package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
)

// HandleExecution is called by the async worker to execute a task.
// It calls the agent executor and updates status in the DB.
func (s *TaskService) HandleExecution(ctx context.Context, task *model.Task, channel *model.Channel) error {
	taskID := task.ID
	userID := task.UserID

	s.logger.Info().Str("task_id", taskID).Msg("starting task execution")

	// Set status to running.
	if err := s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusRunning); err != nil {
		return fmt.Errorf("set running status: %w", err)
	}
	if err := s.repo.Tasks().SetStartedAt(ctx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set started_at")
	}

	// Load channel if not provided.
	if channel == nil {
		if task.ChannelID == "" {
			return fmt.Errorf("task has no channel_id, cannot execute")
		}
		ch, err := s.repo.Channels().FindByID(ctx, task.ChannelID)
		if err != nil {
			s.logger.Error().Err(err).
				Str("task_id", taskID).
				Str("channel_id", task.ChannelID).
				Msg("failed to load channel for task")
			return fmt.Errorf("load channel: %w", err)
		}
		if ch.UserID != userID {
			return fmt.Errorf("channel not owned by user")
		}
		channel = ch
	}

	// Execute via agent.
	result, execErr := s.executor.Execute(ctx, &agent.ExecutionOptions{
		Task:    task,
		Channel: channel,
		OnProgress: func(id string, message string) {
			current, err := s.repo.Tasks().FindByID(ctx, id)
			if err != nil {
				s.logger.Error().Err(err).Str("task_id", id).Msg("failed to read task for progress update")
				return
			}
			newLog := current.ProgressLog + message + "\n"
			if err := s.repo.Tasks().UpdateProgressLog(ctx, id, newLog); err != nil {
				s.logger.Error().Err(err).Str("task_id", id).Msg("failed to update progress log")
			}
		},
	})

	// Store result.
	resultJSON, _ := json.Marshal(result)
	_ = s.repo.Tasks().UpdateResult(ctx, taskID, string(resultJSON))

	if execErr != nil {
		s.logger.Error().Err(execErr).Str("task_id", taskID).Msg("task execution failed")
		return s.HandleExecutionFailure(ctx, task, execErr)
	}

	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "execution returned unsuccessful result"
		}
		s.logger.Error().Str("task_id", taskID).Str("error", errMsg).Msg("task execution returned failure")
		return s.HandleExecutionFailure(ctx, task, fmt.Errorf("%s", errMsg))
	}

	// Success.
	s.logger.Info().Str("task_id", taskID).Str("work_dir", result.WorkDir).Msg("task completed successfully")
	_ = s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusCompleted)
	_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)

	// Upload generated files to storage.
	if result.WorkDir != "" {
		if err := s.UploadTaskFiles(ctx, taskID, userID, result.WorkDir); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("file upload failed")
		}
	}

	return nil
}

// HandleExecutionFromPayload is a convenience method that loads the task from the DB
// by ID and delegates to HandleExecution.
func (s *TaskService) HandleExecutionFromPayload(ctx context.Context, taskID, userID string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task %s: %w", taskID, err)
	}

	if task.Status == model.TaskStatusRunning {
		s.logger.Warn().Str("task_id", taskID).Msg("task already running, skipping")
		return nil
	}

	if task.Status != model.TaskStatusPending {
		s.logger.Warn().Str("task_id", taskID).Str("status", task.Status).Msg("task not in pending state, skipping")
		return nil
	}

	return s.HandleExecution(ctx, task, nil)
}

// generateTaskID generates a unique task ID using UUID v4.
func generateTaskID() string {
	return uuid.New().String()
}

// retryBackoffs defines exponential backoff delays for retries.
var retryBackoffs = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// HandleExecutionFailure handles task execution failures with retry logic.
// If the task has not exceeded max retries, it schedules a delayed retry.
// Otherwise, it marks the task as permanently failed.
func (s *TaskService) HandleExecutionFailure(ctx context.Context, task *model.Task, execErr error) error {
	taskID := task.ID

	// Set defaults for retry configuration.
	if task.MaxRetries <= 0 {
		task.MaxRetries = model.DefaultRetries
	}

	// Check if retry is possible.
	if task.RetryCount >= task.MaxRetries {
		s.logger.Error().
			Err(execErr).
			Str("task_id", taskID).
			Int("retry_count", task.RetryCount).
			Int("max_retries", task.MaxRetries).
			Msg("task permanently failed after max retries")
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, execErr.Error())
		_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)

		// Refund credits for failed task.
		if s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForTask(ctx, taskID); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund credits")
			}
		}

		return execErr
	}

	// Increment retry count.
	task.RetryCount++
	_ = s.repo.Tasks().Update(ctx, task)

	// Calculate backoff delay.
	idx := task.RetryCount - 1
	if idx >= len(retryBackoffs) {
		idx = len(retryBackoffs) - 1
	}
	delay := retryBackoffs[idx]

	s.logger.Info().
		Err(execErr).
		Str("task_id", taskID).
		Int("retry_count", task.RetryCount).
		Dur("backoff", delay).
		Msg("scheduling task retry")

	// Schedule retry via enqueuer with delay.
	if s.enqueuer != nil {
		payload, _ := json.Marshal(map[string]string{
			"task_id": task.ID,
			"user_id": task.UserID,
		})
		_ = s.enqueuer.EnqueueIn(TypeContentGenerate, payload, delay)
	}

	// Update status back to pending so it will be picked up.
	return s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusPending)
}

// CleanupExpiredWorkspaces cleans up workspace directories for completed/failed
// tasks that are older than the specified threshold.
func (s *TaskService) CleanupExpiredWorkspaces(ctx context.Context) error {
	threshold := time.Now().Add(-1 * time.Hour)
	tasks, err := s.repo.Tasks().FindCompletedOlderThan(ctx, threshold)
	if err != nil {
		return fmt.Errorf("find completed tasks for cleanup: %w", err)
	}

	s.logger.Info().Int("count", len(tasks)).Msg("found tasks eligible for cleanup")

	for _, task := range tasks {
		workDir := fmt.Sprintf("/tmp/anbanwriter/%s", task.ID)
		if err := os.RemoveAll(workDir); err != nil {
			s.logger.Warn().Err(err).Str("task_id", task.ID).Str("path", workDir).Msg("failed to remove workspace directory")
			continue
		}

		// Also delete uploaded storage objects for this task.
		if s.store != nil {
			files, err := s.repo.TaskFiles().FindByTaskID(ctx, task.ID)
			if err != nil {
				s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("failed to list task files for storage cleanup")
			} else {
				for _, f := range files {
					if err := s.store.Delete(ctx, f.OSSKey); err != nil {
						s.logger.Warn().Err(err).Str("task_id", task.ID).Str("oss_key", f.OSSKey).Msg("failed to delete storage object")
					}
				}
			}
		}

		now := time.Now()
		if err := s.repo.Tasks().UpdateCleanedUpAt(ctx, task.ID, now); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("failed to update cleaned_up_at")
			continue
		}

		s.logger.Info().Str("task_id", task.ID).Str("path", workDir).Msg("cleaned up workspace directory")
	}

	return nil
}
