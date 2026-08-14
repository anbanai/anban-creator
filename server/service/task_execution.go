package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
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
	hasFinal := false
	hasManifest := false
	meaningful := 0
	for _, file := range files {
		if file == nil || file.FileSize <= 0 {
			continue
		}
		role := strings.TrimSpace(file.Role)
		name := strings.ToLower(strings.TrimSpace(file.FileName))
		path := strings.ToLower(filepath.ToSlash(strings.TrimSpace(file.FilePath)))
		switch {
		case role == "final_video" || isMontageFinalVideoPath(name) || isMontageFinalVideoPath(path):
			hasFinal = true
			meaningful++
		case role == "delivery_manifest" || name == "delivery-manifest.json" || strings.HasSuffix(path, "/delivery-manifest.json"):
			hasManifest = true
			meaningful++
		}
	}
	var missing []string
	if !hasFinal {
		missing = append(missing, "final_video")
	}
	if !hasManifest {
		missing = append(missing, "delivery-manifest.json")
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

func isMontageFinalVideoPath(path string) bool {
	switch filepath.Base(path) {
	case "final.mp4", "final_video.mp4", "final-video.mp4":
		return true
	default:
		return false
	}
}

// HandleExecutionFromPayload loads a queued task and starts its durable managed
// execution through the configured runtime dispatcher.
func (s *TaskService) HandleExecutionFromPayload(ctx context.Context, taskID, _ string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task %s: %w", taskID, err)
	}
	if isPreDispatchTerminalFailure(task) {
		return s.finalizeFailedExecutionPostCommit(ctx, task, task.ErrorMessage)
	}
	_, preparation, err := s.preparePendingExecution(ctx, task)
	if err != nil || preparation != pendingExecutionReady {
		return err
	}
	return s.dispatchPendingTask(ctx, task)
}

func (s *TaskService) dispatchPendingTask(ctx context.Context, task *model.Task) error {
	if err := s.dispatchRuntime(ctx, task); err != nil {
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

func isPreDispatchTerminalFailure(task *model.Task) bool {
	return task != nil && task.Status == model.TaskStatusFailed && task.CurrentExecutionID == nil && task.BillingTerminalReason == model.TaskBillingTerminalPlatformError
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

// autoPublishWithData publishes pre-extracted article data from persisted task files.
func (s *TaskService) autoPublishWithData(ctx context.Context, task *model.Task, project *model.Project, articles []DraftArticleInput, logText string) {
	taskID := task.ID

	if wasPublishedByAgent(logText) {
		s.logger.Info().Str("task_id", taskID).Msg("agent already published, setting published flag")
		if err := s.repo.Tasks().SetPublished(ctx, taskID, true); err != nil {
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

	if err := s.repo.Tasks().SetPublished(ctx, taskID, true); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to set published flag after auto-publish")
	}
}

// wasPublishedByAgent checks the agent's log text for evidence that the agent
// already called a publish MCP tool during execution.
func wasPublishedByAgent(logText string) bool {
	return strings.Contains(logText, "publish_draft")
}

func (s *TaskService) extractArticleDraftFromTaskFiles(ctx context.Context, taskID string) ([]DraftArticleInput, error) {
	if s.store == nil {
		return nil, fmt.Errorf("storage provider is not available")
	}
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task files: %w", err)
	}
	sortTaskFilesForDraftExtraction(files)

	for _, file := range files {
		if normalizedTaskFileBase(file) != "draft.json" {
			continue
		}
		data, err := s.getFileContent(ctx, file)
		if err != nil {
			return nil, fmt.Errorf("read draft.json task file: %w", err)
		}
		var draft struct {
			Articles []DraftArticleInput `json:"articles"`
		}
		if err := json.Unmarshal(data, &draft); err != nil {
			return nil, fmt.Errorf("parse draft.json task file: %w", err)
		}
		if len(draft.Articles) > 0 {
			return draft.Articles, nil
		}
	}

	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(normalizedTaskFilePath(file)))
		if ext != ".html" && ext != ".htm" {
			continue
		}
		data, err := s.getFileContent(ctx, file)
		if err != nil {
			return nil, fmt.Errorf("read HTML task file: %w", err)
		}
		content := string(data)
		return []DraftArticleInput{{
			Title:   extractTitleFromHTMLContent(content),
			Content: content,
		}}, nil
	}

	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(normalizedTaskFilePath(file)))
		if ext != ".md" && ext != ".markdown" {
			continue
		}
		data, err := s.getFileContent(ctx, file)
		if err != nil {
			return nil, fmt.Errorf("read Markdown task file: %w", err)
		}
		content := string(data)
		return []DraftArticleInput{{
			Title:   extractTitleFromMarkdownContent(content),
			Content: content,
		}}, nil
	}

	return nil, fmt.Errorf("no draft.json, HTML, or Markdown task file found")
}

func sortTaskFilesForDraftExtraction(files []*model.TaskFile) {
	sort.Slice(files, func(i, j int) bool {
		return normalizedTaskFilePath(files[i]) < normalizedTaskFilePath(files[j])
	})
}

func normalizedTaskFilePath(file *model.TaskFile) string {
	if file == nil {
		return ""
	}
	path := strings.TrimSpace(file.FilePath)
	if path == "" {
		path = file.FileName
	}
	return strings.ToLower(filepath.ToSlash(path))
}

func normalizedTaskFileBase(file *model.TaskFile) string {
	return filepath.Base(normalizedTaskFilePath(file))
}

func extractTitleFromHTMLContent(content string) string {
	if m := h1Re.FindStringSubmatch(content); len(m) > 1 {
		if t := cleanTitle(m[1]); t != "" {
			return t
		}
	}
	if m := titleRe.FindStringSubmatch(content); len(m) > 1 {
		if t := cleanTitle(m[1]); t != "" {
			return t
		}
	}
	return ""
}

func extractTitleFromMarkdownContent(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if m := headingRe.FindStringSubmatch(line); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}
