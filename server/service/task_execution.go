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

	// Derive a cancellable context so that Cancel() can signal this execution.
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.deregisterCancel(taskID)
	s.registerCancel(taskID, cancel)

	// Clean up workspace and task log files after execution completes.
	// In K8s with local executor, files accumulate inside the container,
	// so we remove them immediately once the task is done.
	var cleanupWorkDir string
	var cleanupLogPath string
	defer func() {
		if cleanupWorkDir != "" {
			if err := os.RemoveAll(cleanupWorkDir); err != nil {
				s.logger.Warn().Err(err).Str("task_id", taskID).Str("path", cleanupWorkDir).Msg("failed to cleanup workspace after execution")
			} else {
				s.logger.Info().Str("task_id", taskID).Str("path", cleanupWorkDir).Msg("cleaned up workspace directory")
			}
		}
		if cleanupLogPath != "" {
			if err := os.Remove(cleanupLogPath); err != nil && !os.IsNotExist(err) {
				s.logger.Warn().Err(err).Str("task_id", taskID).Str("path", cleanupLogPath).Msg("failed to cleanup task log file")
			}
		}
	}()

	// Create per-task log writer if task_log_dir is configured.
	var taskLogWriter *agent.TaskLogWriter
	if s.taskLogDir != "" {
		logPath := filepath.Join(s.taskLogDir, taskID+".log")
		cleanupLogPath = logPath
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

	// Note: status was already set to "running" and started_at set by
	// HandleExecutionFromPayload via atomic CAS.

	// Load channel if not provided.
	if channel == nil {
		if task.ChannelID == "" {
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "task has no channel_id")
			if s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ChannelID)
			}
			return fmt.Errorf("task has no channel_id, cannot execute")
		}
		ch, err := s.repo.Channels().FindByID(ctx, task.ChannelID)
		if err != nil {
			s.logger.Error().Err(err).
				Str("task_id", taskID).
				Str("channel_id", task.ChannelID).
				Msg("failed to load channel for task")
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "failed to load channel")
			if task.ChannelID != "" && s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ChannelID)
			}
			return fmt.Errorf("load channel: %w", err)
		}
		if ch.UserID != userID {
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "channel not owned by user")
			if task.ChannelID != "" && s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ChannelID)
			}
			return fmt.Errorf("channel not owned by user")
		}
		channel = ch
	}

	// Execute via agent.
	opts := &agent.ExecutionOptions{
		Task:      task,
		Channel:   channel,
		LogWriter: taskLogWriter,
		OnProgress: func(id string, message string) {
			if err := s.AppendProgressLog(ctx, id, message); err != nil {
				s.logger.Error().Err(err).Str("task_id", id).Msg("failed to update progress log")
			}
		},
		HeartbeatFunc: func(id string) {
			if err := s.repo.Tasks().UpdateHeartbeat(ctx, id); err != nil {
				s.logger.Warn().Err(err).Str("task_id", id).Msg("failed to update task heartbeat")
			}
		},
	}

	result, execErr := s.executor.Execute(execCtx, opts)

	// Store result.
	if err := s.UpdateExecutionResult(ctx, taskID, result); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to persist task result")
	}

	// Upload workspace files from the host side regardless of agent success.
	// The agent binary's in-container upload may fail (e.g. Docker networking),
	// so we always attempt host-side upload as a safety net. Files already
	// recorded via agent upload or MCP tool calls are skipped.
	if result.WorkDir != "" {
		if err := s.uploadMissingTaskFiles(ctx, taskID, userID, result.WorkDir); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("workspace file upload failed")
		}
		if err := s.RebuildWorkflowStatus(ctx, taskID); err != nil {
			s.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to rebuild workflow status")
		}
		// Schedule workspace cleanup after all processing is done.
		cleanupWorkDir = result.WorkDir
		if title := ExtractTitleFromWorkspace(result.WorkDir); title != "" {
			if err := s.repo.Tasks().UpdateTitle(ctx, taskID, title); err != nil {
				s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task title")
			} else {
				s.logger.Info().Str("task_id", taskID).Str("title", title).Msg("extracted task title from output files")
			}
		}
	}

	// If the execution context was cancelled (shutdown or user cancel),
	// mark as cancelled rather than attempting retry or marking as failed.
	// This handles both CancelAllRunning (shutdown) and Cancel (user-initiated).
	if execCtx.Err() != nil {
		errMsg := "task cancelled: " + execCtx.Err().Error()
		s.logger.Info().Str("task_id", taskID).Msg(errMsg)
		swapped, _ := s.repo.Tasks().CompareAndSwapStatusAndError(
			ctx, taskID, model.TaskStatusRunning, model.TaskStatusCancelled, errMsg,
		)
		if swapped {
			if err := s.repo.Tasks().SetCompletedAt(ctx, taskID); err != nil {
				s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at on cancelled task")
			}
			if s.creditSvc != nil {
				if refundErr := s.creditSvc.RefundForTask(ctx, taskID, "cancel"); refundErr != nil {
					s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund credits for cancelled task")
				}
			}
			if task.ChannelID != "" && s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ChannelID)
			}
			if task.ChannelID != "" {
				if derr := s.DispatchPendingTasks(ctx, task.ChannelID); derr != nil {
					s.logger.Warn().Err(derr).Str("channel_id", task.ChannelID).Msg("failed to dispatch pending tasks after cancellation")
				}
			}
		}
		return nil
	}

	if execErr != nil {
		s.logger.Error().Err(execErr).Str("task_id", taskID).Msg("task execution failed")
		_ = s.HandleExecutionFailure(ctx, task, execErr)
		return nil // retry handled internally, don't trigger Asynq retry
	}

	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "execution returned unsuccessful result"
		}
		s.logger.Error().Str("task_id", taskID).Str("error", errMsg).Msg("task execution returned failure")
		_ = s.HandleExecutionFailure(ctx, task, fmt.Errorf("%s", errMsg))
		return nil // retry handled internally, don't trigger Asynq retry
	}

	var meaningfulFileCount int
	// Always count meaningful files when a workspace exists.
	if result.WorkDir != "" {
		meaningfulFileCount = agent.CountMeaningfulFiles(result.WorkDir)
	}

	// Log workspace contents for diagnostics when no output files are found.
	if meaningfulFileCount == 0 && result.WorkDir != "" {
		if files, listErr := agent.ListWorkDirFiles(result.WorkDir); listErr == nil {
			s.logger.Info().
				Str("task_id", taskID).
				Int("file_count", len(files)).
				Interface("files", files).
				Msg("workspace contents on failure (no output files)")
		}
	}

	// Treat as failure when the agent likely failed (standalone agent binary).
	if result.AgentLikelyFailed {
		if meaningfulFileCount == 0 && result.WorkDir != "" {
			errMsg := buildNoOutputFilesError(result)
			s.logger.Error().
				Str("task_id", taskID).
				Str("model", result.Model).
				Str("user_id", userID).
				Int("num_turns", result.NumTurns).
				Int("tool_use_count", result.ToolUseCount).
				Int("meaningful_files", meaningfulFileCount).
				Msg(errMsg)
			_ = s.HandleExecutionFailure(ctx, task, fmt.Errorf("%s", errMsg))
			return nil
		}
		// Agent reported failure but produced output files — allow completion with a warning.
		s.logger.Warn().
			Str("task_id", taskID).
			Str("model", result.Model).
			Str("user_id", userID).
			Int("num_turns", result.NumTurns).
			Int("meaningful_files", meaningfulFileCount).
			Msg("agent likely failed but produced output files, marking as completed")
	}

	// Treat as failure when workspace has zero meaningful files
	// (agent wrote to wrong directory or produced no output).
	if meaningfulFileCount == 0 && result.WorkDir != "" {
		errMsg := buildNoOutputFilesError(result)
		s.logger.Error().
			Str("task_id", taskID).
			Str("model", result.Model).
			Str("user_id", userID).
			Int("num_turns", result.NumTurns).
			Int("tool_use_count", result.ToolUseCount).
			Int("meaningful_files", meaningfulFileCount).
			Msg(errMsg)
		_ = s.HandleExecutionFailure(ctx, task, fmt.Errorf("%s", errMsg))
		return nil
	}

	// Check if task was cancelled during execution before marking as completed.
	finalTask, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err == nil && finalTask.Status == model.TaskStatusCancelled {
		s.logger.Info().Str("task_id", taskID).Msg("task was cancelled during execution, skipping completion")
		return nil
	}

	// Success.
	s.logger.Info().Str("task_id", taskID).Str("work_dir", result.WorkDir).Msg("task completed successfully")

	// Log workspace file count for diagnostics.
	if result.WorkDir != "" {
		s.logger.Info().
			Str("task_id", taskID).
			Int("file_count", meaningfulFileCount).
			Msg("workspace contains output files")
	}

	// Auto-publish if channel has publishing enabled (non-blocking).
	// Uses a detached context with timeout so it never blocks task completion.
	if s.publishingSvc != nil && channel != nil && channel.GetEnablePublishing() {
		publishCtx, publishCancel := context.WithTimeout(context.Background(), 60*time.Second)
		taskCopy := *task
		channelCopy := *channel
		workDirCopy := result.WorkDir
		logTextCopy := result.LogText
		go func() {
			defer publishCancel()
			s.autoPublishIfNeeded(publishCtx, &taskCopy, &channelCopy, workDirCopy, logTextCopy)
		}()
	}

	if err := s.repo.Tasks().UpdateStatus(ctx, taskID, model.TaskStatusCompleted); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status to completed")
	}
	if err := s.repo.Tasks().SetCompletedAt(ctx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at")
	}

	// Release concurrency slot.
	if task.ChannelID != "" {
		if s.pubsub != nil {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
		}
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

	// Atomic CAS: pending → running (with started_at). Eliminates TOCTOU race.
	swapped, err := s.repo.Tasks().CompareAndSwapStatusAndStartedAt(ctx, taskID, model.TaskStatusPending, model.TaskStatusRunning)
	if err != nil {
		return fmt.Errorf("CAS task status: %w", err)
	}
	if !swapped {
		s.logger.Warn().Str("task_id", taskID).Str("status", task.Status).Msg("task not in pending state, skipping")
		// Slot was reserved in EnqueueExecution but task won't run — release it.
		if s.pubsub != nil && task.ChannelID != "" {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
		}
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

// isPermanentAuthError checks for upstream authentication/authorization errors.
// These are configuration problems (bad token, forbidden model, wrong endpoint)
// and retrying the same task will not make them succeed.
func isPermanentAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "failed to authenticate") ||
		strings.Contains(s, "api error: 401") ||
		strings.Contains(s, "api error: 403") ||
		strings.Contains(s, "\"type\":\"forbidden\"") ||
		strings.Contains(s, "\"type\":\"unauthorized\"") ||
		strings.Contains(s, "request not allowed") ||
		strings.Contains(s, "invalid api key") ||
		strings.Contains(s, "invalid auth") ||
		strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "forbidden")
}

// HandleExecutionFailure handles task execution failures with retry logic.
// If the task has not exceeded max retries, it schedules a delayed retry.
// Rate limit (429) errors use a separate counter with longer backoff.
// Otherwise, it marks the task as permanently failed.
//
// The DB status is always updated BEFORE enqueueing. This ensures that if the
// enqueue fails, the task stays in "pending" and will be picked up by the plan
// checker — never stuck in "running".
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

			// Release concurrency slot.
			if task.ChannelID != "" && s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ChannelID)
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

		newCount := task.RateLimitRetryCount + 1

		idx := newCount - 1
		if idx >= len(rateLimitBackoffs) {
			idx = len(rateLimitBackoffs) - 1
		}
		delay := rateLimitBackoffs[idx]

		s.logger.Info().
			Err(execErr).
			Str("task_id", taskID).
			Int("rate_limit_retry_count", newCount).
			Dur("backoff", delay).
			Msg("rate limit detected, scheduling task retry with extended backoff")

		// Set status to pending first, then enqueue. If enqueue fails,
		// the task stays pending and will be picked up by the plan checker.
		// Release the concurrency slot since the task is no longer running.
		if task.ChannelID != "" && s.pubsub != nil {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
		}
		if err := s.repo.Tasks().IncrementRetryAndSetPending(ctx, taskID, "rate_limit_retry_count"); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task rate limit retry count")
		}

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

		return nil
	}

	if isPermanentAuthError(execErr) {
		s.logger.Error().
			Err(execErr).
			Str("task_id", taskID).
			Msg("task permanently failed due to agent authentication/authorization error")
		if err := s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, execErr.Error()); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status to failed")
		}
		if err := s.repo.Tasks().SetCompletedAt(ctx, taskID); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at on failure")
		}
		if task.ChannelID != "" && s.pubsub != nil {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
		}
		if s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForTask(ctx, taskID); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund credits")
			}
		}
		if task.ChannelID != "" {
			if derr := s.DispatchPendingTasks(ctx, task.ChannelID); derr != nil {
				s.logger.Warn().Err(derr).Str("channel_id", task.ChannelID).Msg("failed to dispatch pending tasks after failure")
			}
		}
		return execErr
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

		// Release concurrency slot.
		if task.ChannelID != "" && s.pubsub != nil {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
		}

		// Refund credits for failed task.
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

	// Calculate backoff delay based on next retry count.
	newCount := task.RetryCount + 1
	idx := newCount - 1
	if idx >= len(retryBackoffs) {
		idx = len(retryBackoffs) - 1
	}
	delay := retryBackoffs[idx]

	s.logger.Info().
		Err(execErr).
		Str("task_id", taskID).
		Int("retry_count", newCount).
		Dur("backoff", delay).
		Msg("scheduling task retry")

	// Set status to pending first, then enqueue. If enqueue fails,
	// the task stays pending and will be picked up by the plan checker.
	// Release the concurrency slot since the task is no longer running.
	if task.ChannelID != "" && s.pubsub != nil {
		s.pubsub.ReleaseSlot(ctx, task.ChannelID)
	}
	if err := s.repo.Tasks().IncrementRetryAndSetPending(ctx, taskID, "retry_count"); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task retry count")
	}

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

	return nil
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
		var workDir string
		if s.workspaceDir != "" {
			workDir = filepath.Join(s.workspaceDir, task.ID)
		} else {
			workDir = filepath.Join(os.TempDir(), "abwriter", task.ID)
		}
		if err := os.RemoveAll(workDir); err != nil {
			s.logger.Warn().Err(err).Str("task_id", task.ID).Str("path", workDir).Msg("failed to remove workspace directory")
			continue
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

// autoPublishIfNeeded checks if the channel has publishing enabled and
// attempts to publish the task output as a WeChat draft.
// If the agent already published during execution (detected via log text),
// it only sets the published flag. Otherwise it publishes from workspace files.
// Publishing failure is logged but does not affect task completion.
func (s *TaskService) autoPublishIfNeeded(ctx context.Context, task *model.Task, channel *model.Channel, workDir, logText string) {
	taskID := task.ID

	if wasPublishedByAgent(logText) {
		s.logger.Info().Str("task_id", taskID).Msg("agent already published, setting published flag")
		if err := s.repo.Tasks().SetPublished(ctx, taskID, true); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set published flag")
		}
		return
	}

	s.logger.Info().Str("task_id", taskID).Msg("agent did not publish, attempting server-side auto-publish")

	var published bool

	switch task.Type {
	case model.ScopeArticle:
		articles, err := extractArticleDraftFromWorkspace(workDir)
		if err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to extract article draft from workspace")
			return
		}
		result, err := s.publishingSvc.PublishDraft(ctx, task.UserID, channel.ID, articles)
		if err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("auto-publish article draft failed")
			return
		}
		s.logger.Info().Str("task_id", taskID).Str("media_id", result.MediaID).Msg("auto-published article draft")
		published = true

	case model.ScopeXls:
		title := task.Title
		if title == "" {
			title = ExtractTitleFromWorkspace(workDir)
		}
		xlsReq, err := extractXlsDraftFromWorkspace(workDir, title)
		if err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to extract XLS data from workspace")
			return
		}
		result, err := s.publishingSvc.PublishXls(ctx, task.UserID, channel.ID, *xlsReq)
		if err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("auto-publish XLS draft failed")
			return
		}
		s.logger.Info().Str("task_id", taskID).Str("media_id", result.MediaID).Msg("auto-published XLS draft")
		published = true

	default:
		return
	}

	if published {
		if err := s.repo.Tasks().SetPublished(ctx, taskID, true); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set published flag after auto-publish")
		}
	}
}

// wasPublishedByAgent checks the agent's log text for evidence that the agent
// already called a publish MCP tool during execution.
func wasPublishedByAgent(logText string) bool {
	return strings.Contains(logText, "publish_draft") || strings.Contains(logText, "publish_xls_draft")
}

// extractArticleDraftFromWorkspace reads the agent's draft.json from the workspace
// and returns articles suitable for PublishingService.PublishDraft.
// Falls back to finding an HTML file and constructing a minimal article.
func extractArticleDraftFromWorkspace(workDir string) ([]DraftArticleInput, error) {
	scanDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		scanDir = filepath.Join(workDir, "output")
	}

	// Try draft.json first (created by the agent's publishing skill).
	draftPath := filepath.Join(scanDir, "draft.json")
	if data, err := os.ReadFile(draftPath); err == nil {
		var draft struct {
			Articles []DraftArticleInput `json:"articles"`
		}
		if json.Unmarshal(data, &draft) == nil && len(draft.Articles) > 0 {
			return draft.Articles, nil
		}
	}

	// Fallback: find the first HTML file in the workspace.
	var htmlPath string
	_ = filepath.WalkDir(scanDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || htmlPath != "" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".html" || ext == ".htm" {
			htmlPath = path
		}
		return nil
	})

	if htmlPath == "" {
		return nil, fmt.Errorf("no draft.json or HTML file found in workspace")
	}

	content, err := os.ReadFile(htmlPath)
	if err != nil {
		return nil, fmt.Errorf("read HTML file: %w", err)
	}

	title := ExtractTitleFromWorkspace(workDir)
	return []DraftArticleInput{{
		Title:   title,
		Content: string(content),
	}}, nil
}

// extractXlsDraftFromWorkspace collects images from the workspace and constructs
// an XlsPublishRequest for PublishingService.PublishXls.
func extractXlsDraftFromWorkspace(workDir, title string) (*XlsPublishRequest, error) {
	scanDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		scanDir = filepath.Join(workDir, "output")
	}

	var images []string
	_ = filepath.WalkDir(scanDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
			images = append(images, path)
		}
		return nil
	})

	if len(images) == 0 {
		return nil, fmt.Errorf("no images found in workspace for XLS publish")
	}

	if title == "" {
		title = "小绿书图片帖"
	}

	return &XlsPublishRequest{
		Title:  title,
		Images: images,
	}, nil
}

// buildNoOutputFilesError returns a diagnostic error message when agent produces no output files.
// Differentiates between "no tool uses" (model/agent issue) and "tool uses but no files" (MCP tool errors).
func buildNoOutputFilesError(result *agent.ExecutionResult) string {
	if result.ToolUseCount > 0 {
		return fmt.Sprintf(
			"agent execution produced no output files (model=%s, num_turns=%d, tool_uses=%d, output_files=0); "+
				"agent used tools but produced no output files — MCP tools may have returned errors "+
				"(check user model config in Studio), or files were written to an unexpected location",
			result.Model, result.NumTurns, result.ToolUseCount,
		)
	}
	return fmt.Sprintf(
		"agent execution produced no output files (model=%s, num_turns=%d, tool_uses=%d, output_files=0); "+
			"agent definition may not have loaded, or model does not support tool use",
		result.Model, result.NumTurns, result.ToolUseCount,
	)
}
