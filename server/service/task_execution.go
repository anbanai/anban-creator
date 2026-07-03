package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
)

// HandleExecution is called by the async worker to execute a task.
// It calls the agent executor and updates status in the DB.
func (s *TaskService) HandleExecution(ctx context.Context, task *model.Task, project *model.Project) error {
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

	// Load project if not provided.
	if project == nil {
		if task.ProjectID == "" {
			errMsg := "task has no project_id"
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, errMsg)
			_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)
			s.notifyTerminal(ctx, task, model.TaskStatusFailed, errMsg)
			if s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ProjectID)
			}
			return fmt.Errorf("task has no project_id, cannot execute")
		}
		ch, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
		if err != nil {
			s.logger.Error().Err(err).
				Str("task_id", taskID).
				Str("project_id", task.ProjectID).
				Msg("failed to load project for task")
			errMsg := "failed to load project"
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, errMsg)
			_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)
			s.notifyTerminal(ctx, task, model.TaskStatusFailed, errMsg)
			if task.ProjectID != "" && s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ProjectID)
			}
			return fmt.Errorf("load project: %w", err)
		}
		if ch.UserID != userID {
			errMsg := "project not owned by user"
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, errMsg)
			_ = s.repo.Tasks().SetCompletedAt(ctx, taskID)
			s.notifyTerminal(ctx, task, model.TaskStatusFailed, errMsg)
			if task.ProjectID != "" && s.pubsub != nil {
				s.pubsub.ReleaseSlot(ctx, task.ProjectID)
			}
			return fmt.Errorf("project not owned by user")
		}
		project = ch
	}

	// Execute via agent.
	opts := &agent.ExecutionOptions{
		Task:      task,
		Project:   project,
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

	// Post-execution persistence uses a fresh, bounded context that is decoupled
	// from the asynq execution ctx. If the pipeline overruns the asynq deadline,
	// execCtx/ctx expires and the agent's already-completed work (result, files,
	// terminal status) would otherwise fail to persist — losing everything.
	persistCtx, persistCancel := context.WithTimeout(context.Background(), s.persistTimeout)
	defer persistCancel()

	// Store result.
	if err := s.UpdateExecutionResult(persistCtx, taskID, result); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to persist task result")
	}

	// Upload workspace files from the host side regardless of agent success.
	// The agent binary's in-container upload may fail (e.g. Docker networking),
	// so we always attempt host-side upload as a safety net. Files already
	// recorded via agent upload or MCP tool calls are skipped.
	if result.WorkDir != "" {
		if err := s.uploadMissingTaskFiles(persistCtx, taskID, userID, result.WorkDir); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("workspace file upload failed")
		}
		if err := s.RebuildWorkflowStatus(persistCtx, taskID); err != nil {
			s.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to rebuild workflow status")
		}
		// Schedule workspace cleanup after all processing is done.
		cleanupWorkDir = result.WorkDir
	}

	// If the execution context was cancelled (shutdown or user cancel),
	// mark as cancelled rather than attempting retry or marking as failed.
	// This handles both CancelAllRunning (shutdown) and Cancel (user-initiated).
	if execCtx.Err() != nil {
		errMsg := "task cancelled: " + execCtx.Err().Error()
		s.logger.Info().Str("task_id", taskID).Msg(errMsg)
		swapped, _ := s.repo.Tasks().CompareAndSwapStatusAndError(
			persistCtx, taskID, model.TaskStatusRunning, model.TaskStatusCancelled, errMsg,
		)
		if swapped {
			if err := s.repo.Tasks().SetCompletedAt(persistCtx, taskID); err != nil {
				s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at on cancelled task")
			}
			// Re-read the task so refundTaskByMode sees the final status
			// (goal-mode tasks skip refund; normal tasks full-refund).
			if t, err := s.repo.Tasks().FindByID(persistCtx, taskID); err == nil {
				s.refundTaskByMode(persistCtx, t, "取消")
			} else {
				s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to reload task for refund")
			}
			// Notify the task owner's WeChat of the cancellation (best-effort).
			s.notifyTerminal(persistCtx, task, model.TaskStatusCancelled, errMsg)
			if task.ProjectID != "" && s.pubsub != nil {
				s.pubsub.ReleaseSlot(persistCtx, task.ProjectID)
			}
			if task.ProjectID != "" {
				if derr := s.DispatchPendingTasks(persistCtx, task.ProjectID); derr != nil {
					s.logger.Warn().Err(derr).Str("project_id", task.ProjectID).Msg("failed to dispatch pending tasks after cancellation")
				}
			}
		}
		return nil
	}

	if execErr != nil {
		s.logger.Error().Err(execErr).Str("task_id", taskID).Msg("task execution failed")
		_ = s.HandleExecutionFailure(persistCtx, task, execErr)
		return nil // retry handled internally, don't trigger Asynq retry
	}

	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "execution returned unsuccessful result"
		}
		s.logger.Error().Str("task_id", taskID).Str("error", errMsg).Msg("task execution returned failure")
		_ = s.HandleExecutionFailure(persistCtx, task, fmt.Errorf("%s", errMsg))
		return nil // retry handled internally, don't trigger Asynq retry
	}

	if agent.IsNestedAgentDelegationOnly(result.ToolUseSummary) {
		errMsg := agent.NestedAgentDelegationError
		s.logger.Error().
			Str("task_id", taskID).
			Str("model", result.Model).
			Str("user_id", userID).
			Int("num_turns", result.NumTurns).
			Int("tool_use_count", result.ToolUseCount).
			Str("tools_used", formatToolUseSummary(result.ToolUseSummary)).
			Msg(errMsg)
		_ = s.HandleExecutionFailure(persistCtx, task, fmt.Errorf("%s", errMsg))
		return nil
	}

	artifactValidation := agent.ArtifactValidation{Valid: true}
	if task.Type == model.PlatformVideo {
		var err error
		artifactValidation, err = s.validateVideoCompletionArtifacts(persistCtx, task)
		if err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("list video task files for artifact validation")
			_ = s.HandleExecutionFailure(persistCtx, task, fmt.Errorf("list video task files: %w", err))
			return nil
		}
	} else if result.WorkDir != "" || task.Type == model.PlatformSeednote {
		artifactValidation = agent.ValidateTaskArtifactsFromWorkDir(task, result.WorkDir)
	}
	if !artifactValidation.Valid {
		errMsg := artifactValidation.Error()
		errEvt := s.logger.Error().
			Str("task_id", taskID).
			Str("model", result.Model).
			Str("user_id", userID).
			Int("num_turns", result.NumTurns).
			Int("tool_use_count", result.ToolUseCount).
			Int("meaningful_files", artifactValidation.MeaningfulFileCount).
			Strs("missing_files", artifactValidation.Missing)
		if toolSummaryStr := formatToolUseSummary(result.ToolUseSummary); toolSummaryStr != "" {
			errEvt = errEvt.Str("tools_used", toolSummaryStr)
		}
		if files, listErr := agent.ListWorkDirFiles(result.WorkDir); listErr == nil {
			errEvt = errEvt.Interface("files", files)
		}
		errEvt.Msg(errMsg)
		_ = s.HandleExecutionFailure(persistCtx, task, fmt.Errorf("%s", errMsg))
		return nil
	}
	meaningfulFileCount := artifactValidation.MeaningfulFileCount

	// Check if task was cancelled during execution before marking as completed.
	finalTask, err := s.repo.Tasks().FindByID(persistCtx, taskID)
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

	// Auto-publish if project has publishing enabled (non-blocking).
	// Extract article data BEFORE launching goroutine to avoid race with
	// workspace cleanup defer.
	if s.publishingSvc != nil && project != nil && project.GetEnablePublishing() {
		published := wasPublishedByAgent(result.LogText)
		var articles []DraftArticleInput
		if !published && task.Type == model.ScopeArticle && result.WorkDir != "" {
			var extractErr error
			articles, extractErr = extractArticleDraftFromWorkspace(result.WorkDir)
			if extractErr != nil {
				s.logger.Error().Err(extractErr).Str("task_id", taskID).Msg("failed to extract article draft for auto-publish")
			}
		}

		// Publish-approval gate (Batch 4A): when the project requires human
		// review before publishing, freeze the extracted draft data into the
		// task and enter the "pending" approval state instead of auto-publishing
		// — the user must explicitly approve (ApprovePublish) to land it in the
		// WeChat draft box. Only applies when the server is the publisher (the
		// agent has not already published) and there is extractable article
		// data; otherwise fall through to the immediate auto-publish path.
		if project.GetRequirePublishApproval() && !published && len(articles) > 0 {
			s.holdPublishForApproval(persistCtx, taskID, articles)
		} else if published || len(articles) > 0 {
			publishCtx, publishCancel := context.WithTimeout(context.Background(), 60*time.Second)
			taskCopy := *task
			projectCopy := *project
			logTextCopy := result.LogText
			go func() {
				defer publishCancel()
				s.autoPublishWithData(publishCtx, &taskCopy, &projectCopy, articles, logTextCopy)
			}()
		}
	}

	// Goal-mode evaluation (when enabled) ran inside Claude Code's built-in
	// /goal loop. The server just observes the final result — no retry,
	// no CAS, no special terminal status.

	if err := s.repo.Tasks().UpdateStatus(persistCtx, taskID, model.TaskStatusCompleted); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status to completed")
	}
	if err := s.repo.Tasks().SetCompletedAt(persistCtx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at")
	}

	// Notify the task owner's WeChat of the terminal success (best-effort).
	s.notifyTerminal(persistCtx, task, model.TaskStatusCompleted, "")

	// Release concurrency slot.
	if task.ProjectID != "" {
		if s.pubsub != nil {
			s.pubsub.ReleaseSlot(persistCtx, task.ProjectID)
		}
	}

	// Dispatch pending tasks for the same project now that a slot opened.
	if task.ProjectID != "" {
		if err := s.DispatchPendingTasks(persistCtx, task.ProjectID); err != nil {
			s.logger.Warn().Err(err).Str("project_id", task.ProjectID).Msg("failed to dispatch pending tasks after completion")
		}
	}

	return nil
}

func (s *TaskService) validateVideoCompletionArtifacts(ctx context.Context, task *model.Task) (agent.ArtifactValidation, error) {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, task.ID)
	if err != nil {
		return agent.ArtifactValidation{}, err
	}
	if !requiresGeneratedVideoTaskFile(task) {
		return agent.ValidateTaskArtifactsFromTaskFiles(task, files), nil
	}
	gen, err := s.videoGenerationForTask(ctx, task)
	if err != nil {
		return agent.ArtifactValidation{}, err
	}
	if gen == nil {
		return agent.ArtifactValidation{Reason: "video missing generated video task file"}, nil
	}
	var ids map[string]string
	if len(gen.TaskFileIDs) > 0 {
		_ = json.Unmarshal(gen.TaskFileIDs, &ids)
	}
	generatedID := strings.TrimSpace(ids["generated_video"])
	if generatedID == "" {
		return agent.ArtifactValidation{Reason: "video missing generated video task file"}, nil
	}
	for _, file := range files {
		if file != nil && file.ID == generatedID {
			if !isVideoTaskFile(file) {
				return agent.ArtifactValidation{Reason: "video missing generated video task file"}, nil
			}
			return agent.ArtifactValidation{Valid: true, MeaningfulFileCount: 1}, nil
		}
	}
	return agent.ArtifactValidation{Reason: "video missing generated video task file"}, nil
}

func requiresGeneratedVideoTaskFile(task *model.Task) bool {
	if task == nil || task.Type != model.PlatformVideo {
		return false
	}
	return strings.TrimSpace(task.VideoGenerationID) != "" || task.VideoEstimatedCredits > 0 || task.VideoCreditsCharged > 0
}

func isVideoTaskFile(file *model.TaskFile) bool {
	if file == nil || file.FileSize <= 0 {
		return false
	}
	mime := strings.ToLower(strings.TrimSpace(file.MimeType))
	name := strings.ToLower(strings.TrimSpace(file.FileName))
	path := strings.ToLower(strings.TrimSpace(file.FilePath))
	if strings.HasPrefix(mime, "video/") {
		return true
	}
	for _, value := range []string{name, path} {
		switch filepath.Ext(value) {
		case ".mp4", ".mov", ".webm", ".m4v":
			return true
		}
	}
	return false
}

func (s *TaskService) videoGenerationForTask(ctx context.Context, task *model.Task) (*model.VideoGeneration, error) {
	if s == nil || s.repo == nil || s.repo.VideoGenerations() == nil || task == nil {
		return nil, nil
	}
	if id := strings.TrimSpace(task.VideoGenerationID); id != "" {
		gen, err := s.repo.VideoGenerations().FindByID(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return gen, nil
	}
	gen, err := s.repo.VideoGenerations().FindLatestByTaskID(ctx, task.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return gen, nil
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
		if s.pubsub != nil && task.ProjectID != "" {
			s.pubsub.ReleaseSlot(ctx, task.ProjectID)
		}
		return nil
	}

	return s.HandleExecution(ctx, task, nil)
}

// generateTaskID generates a unique task ID using UUID v4.
func generateTaskID() string {
	return uuid.New().String()
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

// HandleExecutionFailure marks agent execution failures as terminal. Recovery
// is intentionally manual: users can choose "重新执行" to clone the task into a
// fresh billed run, while the original task remains a clear failed artifact.
func (s *TaskService) HandleExecutionFailure(ctx context.Context, task *model.Task, execErr error) error {
	taskID := task.ID

	reason := "execution_failed"
	if isPermanentAuthError(execErr) {
		reason = "auth_error"
	}

	s.logger.Error().
		Err(execErr).
		Str("task_id", taskID).
		Msg("task failed; waiting for manual rerun")
	if err := s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, execErr.Error()); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to update task status to failed")
	}
	if err := s.repo.Tasks().SetCompletedAt(ctx, taskID); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set completed_at on failure")
	}

	if task.ProjectID != "" && s.pubsub != nil {
		s.pubsub.ReleaseSlot(ctx, task.ProjectID)
	}

	s.refundTaskByMode(ctx, task, reason)
	s.notifyTerminal(ctx, task, model.TaskStatusFailed, execErr.Error())

	if task.ProjectID != "" {
		if derr := s.DispatchPendingTasks(ctx, task.ProjectID); derr != nil {
			s.logger.Warn().Err(derr).Str("project_id", task.ProjectID).Msg("failed to dispatch pending tasks after failure")
		}
	}

	return execErr
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
			workDir = agent.DefaultWorkspaceDir(task.ID)
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

// autoPublishWithData publishes pre-extracted article data, avoiding filesystem
// access that could race with workspace cleanup.
func (s *TaskService) autoPublishWithData(ctx context.Context, task *model.Task, project *model.Project, articles []DraftArticleInput, logText string) {
	taskID := task.ID

	if wasPublishedByAgent(logText) {
		s.logger.Info().Str("task_id", taskID).Msg("agent already published, setting published flag")
		if err := s.setPublishedAndMaybeTrack(ctx, task.UserID, task, true); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set published flag")
		}
		return
	}

	s.logger.Info().Str("task_id", taskID).Msg("auto-publishing with pre-extracted articles")
	result, err := s.publishingSvc.PublishDraft(ctx, task.UserID, project.ID, articles)
	if err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("auto-publish article draft failed")
		return
	}
	s.logger.Info().Str("task_id", taskID).Str("media_id", result.MediaID).Msg("auto-published article draft")

	if err := s.setPublishedAndMaybeTrack(ctx, task.UserID, task, true); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set published flag after auto-publish")
	}
}

// wasPublishedByAgent checks the agent's log text for evidence that the agent
// already called a publish MCP tool during execution.
func wasPublishedByAgent(logText string) bool {
	return strings.Contains(logText, "publish_draft")
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

// formatToolUseSummary returns a compact string like "generate_image(12), Bash(8)"
// from a tool name -> count map, limited to the top 5 tools by count.
func formatToolUseSummary(summary map[string]int) string {
	if len(summary) == 0 {
		return ""
	}
	type toolCount struct {
		name  string
		count int
	}
	var tc []toolCount
	for name, count := range summary {
		tc = append(tc, toolCount{name, count})
	}
	sort.Slice(tc, func(i, j int) bool { return tc[i].count > tc[j].count })
	if len(tc) > 5 {
		tc = tc[:5]
	}
	parts := make([]string, len(tc))
	for i, t := range tc {
		parts[i] = fmt.Sprintf("%s(%d)", t.name, t.count)
	}
	return strings.Join(parts, ", ")
}

// buildNoOutputFilesError returns a diagnostic error message when agent produces no output files.
// Differentiates between "no tool uses" (model/agent issue) and "tool uses but no files" (MCP tool errors).
func buildNoOutputFilesError(result *agent.ExecutionResult) string {
	toolSummary := formatToolUseSummary(result.ToolUseSummary)
	if result.ToolErrorCount > 0 && result.LastToolError != "" {
		tool := result.LastToolErrorTool
		if tool == "" {
			tool = "unknown_tool"
		}
		extra := ""
		if toolSummary != "" {
			extra = fmt.Sprintf("; tools used: %s", toolSummary)
		}
		return fmt.Sprintf(
			"agent execution produced no output files (model=%s, num_turns=%d, tool_uses=%d, tool_errors=%d, output_files=0); "+
				"last MCP tool error: %s failed: %s%s",
			result.Model, result.NumTurns, result.ToolUseCount, result.ToolErrorCount, tool, result.LastToolError, extra,
		)
	}
	if result.ToolUseCount > 0 {
		extra := ""
		if toolSummary != "" {
			extra = fmt.Sprintf("; tools used: %s", toolSummary)
		}
		return fmt.Sprintf(
			"agent execution produced no output files (model=%s, num_turns=%d, tool_uses=%d, output_files=0); "+
				"agent used tools but produced no output files; files may have been written to an unexpected location%s",
			result.Model, result.NumTurns, result.ToolUseCount, extra,
		)
	}
	return fmt.Sprintf(
		"agent execution produced no output files (model=%s, num_turns=%d, tool_uses=%d, output_files=0); "+
			"agent definition may not have loaded, or model does not support tool use",
		result.Model, result.NumTurns, result.ToolUseCount,
	)
}
