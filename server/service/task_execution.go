package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// Create per-task log writer if task_log_dir is configured.
	var taskLogWriter *agent.TaskLogWriter
	if s.taskLogDir != "" {
		logPath := filepath.Join(s.taskLogDir, taskID+".log")
		var err error
		taskLogWriter, err = agent.NewTaskLogWriter(logPath, taskID)
		if err != nil {
			s.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to create task log writer, continuing without log file")
		} else {
			defer taskLogWriter.Close()
		}
	}

	// Re-read task to check if it was cancelled while waiting in queue.
	currentTask, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("re-read task: %w", err)
	}
	if currentTask.Status == model.TaskStatusCancelled {
		s.logger.Info().Str("task_id", taskID).Msg("task was cancelled, skipping execution")
		return nil
	}

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
		Task:      task,
		Channel:   channel,
		LogWriter: taskLogWriter,
		OnProgress: func(id string, message string) {
			if err := s.AppendProgressLog(ctx, id, message); err != nil {
				s.logger.Error().Err(err).Str("task_id", id).Msg("failed to update progress log")
			}
		},
	})

	// Store result.
	if err := s.UpdateExecutionResult(ctx, taskID, result); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to persist task result")
	}

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

	var meaningfulFileCount int
	// Check if agent produced no output — treat as failure when num_turns <= 1
	// and workspace has zero meaningful files (agent definition likely didn't load).
	if result.AgentLikelyFailed || (result.NumTurns <= 1 && result.WorkDir != "") {
		meaningfulFileCount = agent.CountMeaningfulFiles(result.WorkDir)
		if meaningfulFileCount == 0 {
			errMsg := fmt.Sprintf(
				"agent execution produced no output files (num_turns=%d, output_files=0); agent definition may not have loaded or model does not support tool use",
				result.NumTurns,
			)
			s.logger.Error().
				Str("task_id", taskID).
				Int("num_turns", result.NumTurns).
				Int("meaningful_files", meaningfulFileCount).
				Bool("agent_likely_failed", result.AgentLikelyFailed).
				Msg(errMsg)
			return s.HandleExecutionFailure(ctx, task, fmt.Errorf("%s", errMsg))
		}
	}

	// Check if task was cancelled during execution before marking as completed.
	finalTask, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err == nil && finalTask.Status == model.TaskStatusCancelled {
		s.logger.Info().Str("task_id", taskID).Msg("task was cancelled during execution, skipping completion")
		return nil
	}

	// Success.
	s.logger.Info().Str("task_id", taskID).Str("work_dir", result.WorkDir).Msg("task completed successfully")

	// Log workspace file count for diagnostics (reuse count from above if already computed).
	if result.WorkDir != "" {
		if meaningfulFileCount == 0 {
			meaningfulFileCount = agent.CountMeaningfulFiles(result.WorkDir)
		}
		s.logger.Info().
			Str("task_id", taskID).
			Int("file_count", meaningfulFileCount).
			Msg("workspace contains output files")
	}
	// In the remote-agent architecture, the agent uploads files through
	// /api/v1/agent/upload before completion. Keep a local-executor fallback so
	// existing non-agent paths still persist outputs if nothing was uploaded.
	if existingFiles, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID); err != nil {
		s.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to check uploaded task files")
	} else if len(existingFiles) == 0 && result.WorkDir != "" {
		if err := s.UploadTaskFiles(ctx, taskID, userID, result.WorkDir); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("fallback file upload failed")
		}
	} else if len(existingFiles) == 0 {
		s.logger.Warn().Str("task_id", taskID).Msg("task completed with no uploaded files and no work directory")
	}

	if err := s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusCompleted); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status to completed")
	}
	if err := s.repo.Tasks().SetCompletedAt(ctx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at")
	}

	// Dispatch pending tasks for the same channel now that a slot opened.
	if task.ChannelID != "" {
		if err := s.DispatchPendingTasks(ctx, task.ChannelID); err != nil {
			s.logger.Warn().Err(err).Str("channel_id", task.ChannelID).Msg("failed to dispatch pending tasks after completion")
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

// rateLimitBackoffs defines longer backoff delays for rate limit (429) retries.
var rateLimitBackoffs = []time.Duration{
	15 * time.Minute,
	30 * time.Minute,
	45 * time.Minute,
	60 * time.Minute,
	60 * time.Minute,
}

// isRateLimitError checks if the error is caused by an upstream rate limit (429).
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "API Error: 429") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "Too Many Requests") ||
		strings.Contains(s, "\"1302\"")
}

// HandleExecutionFailure handles task execution failures with retry logic.
// If the task has not exceeded max retries, it schedules a delayed retry.
// Rate limit (429) errors use a separate counter with longer backoff.
// Otherwise, it marks the task as permanently failed.
func (s *TaskService) HandleExecutionFailure(ctx context.Context, task *model.Task, execErr error) error {
	taskID := task.ID

	// Set defaults for retry configuration.
	if task.MaxRetries <= 0 {
		task.MaxRetries = model.DefaultRetries
	}

	rateLimited := isRateLimitError(execErr)

	if rateLimited {
		// Rate limit errors use a separate retry counter with longer backoff.
		if task.RateLimitRetryCount >= model.MaxRateLimitRetries {
			s.logger.Error().
				Err(execErr).
				Str("task_id", taskID).
				Int("rate_limit_retry_count", task.RateLimitRetryCount).
				Int("max_rate_limit_retries", model.MaxRateLimitRetries).
				Msg("task permanently failed after max rate limit retries")
			if err := s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, execErr.Error()); err != nil {
				s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status to failed")
			}
			if err := s.repo.Tasks().SetCompletedAt(ctx, taskID); err != nil {
				s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at on failure")
			}

			if s.creditSvc != nil {
				if refundErr := s.creditSvc.RefundForTask(ctx, taskID); refundErr != nil {
					s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund credits")
				}
			}

			// A slot opened on this channel — dispatch pending tasks.
			if task.ChannelID != "" {
				if derr := s.DispatchPendingTasks(ctx, task.ChannelID); derr != nil {
					s.logger.Warn().Err(derr).Str("channel_id", task.ChannelID).Msg("failed to dispatch pending tasks after failure")
				}
			}

			return execErr
		}

		task.RateLimitRetryCount++

		idx := task.RateLimitRetryCount - 1
		if idx >= len(rateLimitBackoffs) {
			idx = len(rateLimitBackoffs) - 1
		}
		delay := rateLimitBackoffs[idx]

		if err := s.repo.Tasks().Update(ctx, task); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task rate limit retry count")
		}

		s.logger.Info().
			Err(execErr).
			Str("task_id", taskID).
			Int("rate_limit_retry_count", task.RateLimitRetryCount).
			Dur("backoff", delay).
			Msg("rate limit detected, scheduling task retry with extended backoff")

		if s.enqueuer != nil {
			payload, _ := json.Marshal(map[string]string{
				"task_id": task.ID,
				"user_id": task.UserID,
			})
			if err := s.enqueuer.EnqueueIn(TypeContentGenerate, payload, delay); err != nil {
				s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to enqueue rate limit retry, marking task as failed")
				if updateErr := s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "retry enqueue failed: "+err.Error()); updateErr != nil {
					s.logger.Error().Err(updateErr).Str("task_id", taskID).Msg("failed to mark task as failed after enqueue failure")
				}
				return err
			}
		} else {
			s.logger.Warn().Str("task_id", taskID).Msg("no enqueuer available, task set to pending but will not be retried automatically")
		}

		return s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusPending)
	}

	// Non-rate-limit errors use the standard retry logic.
	// Check if retry is possible.
	if task.RetryCount >= task.MaxRetries {
		s.logger.Error().
			Err(execErr).
			Str("task_id", taskID).
			Int("retry_count", task.RetryCount).
			Int("max_retries", task.MaxRetries).
			Msg("task permanently failed after max retries")
		if err := s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, execErr.Error()); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status to failed")
		}
		if err := s.repo.Tasks().SetCompletedAt(ctx, taskID); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at on failure")
		}

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
	if err := s.repo.Tasks().Update(ctx, task); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task retry count")
	}

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
		if err := s.enqueuer.EnqueueIn(TypeContentGenerate, payload, delay); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to enqueue retry, marking task as failed")
			if updateErr := s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "retry enqueue failed: "+err.Error()); updateErr != nil {
				s.logger.Error().Err(updateErr).Str("task_id", taskID).Msg("failed to mark task as failed after enqueue failure")
			}
			return err
		}
	} else {
		s.logger.Warn().Str("task_id", taskID).Msg("no enqueuer available, task set to pending but will not be retried automatically")
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
		workDir := filepath.Join(os.TempDir(), "abwriter", task.ID)
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
