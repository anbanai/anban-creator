package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/datatypes"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
)

// evaluateGoalAndMaybeRetry runs the post-execution goal evaluation.
//
// Returns (stop, err):
//   - stop=true when the task was transitioned out of the normal "completed"
//     path — either to pending (will retry) or to goal_not_met (terminal).
//     Callers must skip the normal UpdateStatus(completed) sequence.
//   - stop=false when the task should proceed to normal completion. This
//     happens when goal mode is disabled, the evaluator is unset, the goal
//     was achieved, or a soft error forced a fallback.
//
// The error is logged but never blocks the completion path — users should
// not lose credits because the evaluator crashed.
func (s *TaskService) evaluateGoalAndMaybeRetry(ctx context.Context, task *model.Task, result *agent.ExecutionResult) (bool, error) {
	if task == nil {
		return false, nil
	}
	if !task.GoalMode || strings.TrimSpace(task.Goal) == "" {
		return false, nil
	}
	if s.goalEvaluator == nil {
		// Evaluator not wired (e.g. tests or degraded mode). Treat as achieved
		// so the task still completes normally; the upfront ×N charge is kept.
		s.logger.Warn().Str("task_id", task.ID).Msg("goal mode enabled but evaluator not wired, defaulting to achieved")
		return false, nil
	}

	maxAttempts := task.GoalMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = s.GoalMaxAttempts()
	}

	// Extract produced content for evaluation. Falls back to result.LogText
	// when no specific file is found so the evaluator has *something* to read.
	content := s.extractMainContent(ctx, task.ID, task.Type)
	if strings.TrimSpace(content) == "" && result != nil {
		content = result.LogText
	}

	eval, evalErr := s.goalEvaluator.Evaluate(ctx, task.Goal, task.Prompt, content)
	attempt := task.GoalAttempts + 1

	// Append a log entry regardless of success/failure so users can see what happened.
	entry := model.GoalEvaluationEntry{
		Attempt:     attempt,
		EvaluatedAt: timeNowUTC(),
	}
	if evalErr != nil {
		// Evaluator call failed — preserve user credits by treating as achieved.
		entry.Error = evalErr.Error()
		entry.Achieved = true
		entry.Reason = "评估器故障，默认达成"
		eval = &GoalEvaluation{Achieved: true, Reason: entry.Reason}
	} else if eval == nil {
		entry.Achieved = true
		entry.Reason = "评估器返回空结果，默认达成"
		eval = &GoalEvaluation{Achieved: true, Reason: entry.Reason}
	} else {
		entry.Achieved = eval.Achieved
		entry.Reason = eval.Reason
	}
	s.appendGoalEvaluationLog(ctx, task.ID, entry)

	// Persist achieved flag + increment attempts counter atomically.
	achieved := entry.Achieved
	if err := s.repo.Tasks().UpdateGoalFields(ctx, task.ID, &achieved, 1, ""); err != nil {
		s.logger.Error().Err(err).Str("task_id", task.ID).Msg("failed to update goal fields")
	}

	if entry.Achieved {
		// Goal met — fall through to normal completed transition.
		return false, nil
	}

	// Goal not achieved: retry or terminate.
	if attempt < maxAttempts {
		// Retry: running→pending. The caller (HandleExecution) evaluates the
		// goal BEFORE transitioning to completed, so the task is still in the
		// "running" state at this point. CompareAndSwapStatusAndError makes the
		// transition atomic against concurrent cancel/complete attempts.
		errMsg := fmt.Sprintf("目标未达成，准备第 %d/%d 次尝试: %s", attempt+1, maxAttempts, eval.Reason)
		swapped, casErr := s.repo.Tasks().CompareAndSwapStatusAndError(
			ctx, task.ID, model.TaskStatusRunning, model.TaskStatusPending, errMsg,
		)
		if casErr != nil {
			s.logger.Error().Err(casErr).Str("task_id", task.ID).Msg("goal retry CAS failed")
			return false, casErr
		}
		if !swapped {
			// Task is no longer "running" — a concurrent cancel or completion
			// interleaved. Skip retry; let the current state stand.
			s.logger.Warn().Str("task_id", task.ID).Msg("goal retry CAS did not swap, task status changed concurrently")
			return false, nil
		}

		// Release the concurrency slot so EnqueueExecution can re-reserve it.
		if task.ChannelID != "" && s.pubsub != nil {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
		}

		// Re-enqueue for the next attempt.
		var channel *model.Channel
		if task.ChannelID != "" {
			if ch, err := s.repo.Channels().FindByID(ctx, task.ChannelID); err == nil {
				channel = ch
			}
		}
		if err := s.EnqueueExecution(ctx, task, channel); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("goal retry re-enqueue failed")
		}
		return true, nil
	}

	// Max attempts reached — terminal goal_not_met state. Refund the unused
	// portion (zero in the all-attempts-consumed case but non-zero when the
	// final attempt is interrupted before billing).
	if s.creditSvc != nil {
		if refundErr := s.creditSvc.RefundForGoalTask(ctx, task.ID, attempt, maxAttempts, "未达目标"); refundErr != nil {
			s.logger.Error().Err(refundErr).Str("task_id", task.ID).Msg("failed to refund for goal_not_met task")
		}
	}

	finalErr := fmt.Sprintf("目标未达成（共尝试 %d 次）: %s", attempt, eval.Reason)
	if err := s.repo.Tasks().UpdateStatusAndError(ctx, task.ID, model.TaskStatusGoalNotMet, finalErr); err != nil {
		s.logger.Error().Err(err).Str("task_id", task.ID).Msg("failed to set goal_not_met status")
	}
	if err := s.repo.Tasks().SetCompletedAt(ctx, task.ID); err != nil {
		s.logger.Error().Err(err).Str("task_id", task.ID).Msg("failed to set completed_at for goal_not_met")
	}

	// Release concurrency slot and dispatch pending tasks for the channel.
	if task.ChannelID != "" {
		if s.pubsub != nil {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
		}
		if err := s.DispatchPendingTasks(ctx, task.ChannelID); err != nil {
			s.logger.Warn().Err(err).Str("channel_id", task.ChannelID).Msg("failed to dispatch pending tasks after goal_not_met")
		}
	}
	return true, nil
}

// extractMainContent returns the textual body of the task's primary output
// (final markdown / draft / HTML) by reading from the TaskFiles table.
// Returns empty string when no suitable file is found.
func (s *TaskService) extractMainContent(ctx context.Context, taskID, taskType string) string {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		s.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to list task files for goal evaluation")
		return ""
	}

	// Role preference order — final_markdown is the canonical writeup.
	preferredRoles := preferredGoalRoles(taskType)

	pick := pickGoalFile(files, preferredRoles)
	if pick == nil {
		return ""
	}
	data, err := s.getFileContent(ctx, pick)
	if err != nil {
		s.logger.Warn().Err(err).Str("task_id", taskID).Str("file", pick.FileName).Msg("failed to read task file for goal evaluation")
		return ""
	}
	return string(data)
}

// preferredGoalRoles returns the role lookup order for a given task type.
func preferredGoalRoles(taskType string) []string {
	// Both article and seednote end with a final_markdown writeup; fall back
	// to draft, then plain markdown, then HTML (HTML is least preferred
	// because it mixes content with CSS/markup).
	switch taskType {
	case model.ScopeArticle:
		return []string{model.FileRoleFinalMarkdown, model.FileRoleDraft, model.FileRoleMarkdown, model.FileRoleHTML}
	case model.ScopeSeednote:
		return []string{model.FileRoleFinalMarkdown, model.FileRoleDraft, model.FileRoleMarkdown}
	default:
		return []string{model.FileRoleFinalMarkdown, model.FileRoleDraft, model.FileRoleMarkdown}
	}
}

// pickGoalFile selects the first file matching the preferred role order.
func pickGoalFile(files []*model.TaskFile, preferredRoles []string) *model.TaskFile {
	byRole := make(map[string][]*model.TaskFile, len(files))
	for _, f := range files {
		byRole[f.Role] = append(byRole[f.Role], f)
	}
	for _, role := range preferredRoles {
		if group := byRole[role]; len(group) > 0 {
			return group[0]
		}
	}
	return nil
}

// appendGoalEvaluationLog serialises an entry and appends it to the task's
// goal_evaluation_log JSON array via the repository helper.
func (s *TaskService) appendGoalEvaluationLog(ctx context.Context, taskID string, entry model.GoalEvaluationEntry) {
	raw, err := json.Marshal(entry)
	if err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to marshal goal evaluation entry")
		return
	}
	if err := s.repo.Tasks().AppendGoalEvaluation(ctx, taskID, raw); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to append goal evaluation log")
	}
}

// LastGoalFailureReason returns the reason from the most recent failed
// evaluation for the task (used to inject feedback into the next attempt's
// prompt). Empty when no failed evaluation exists yet.
//
// Deprecated: prefer parseLastGoalFailureReason to avoid the extra DB
// round-trip when the caller already holds the task struct.
func (s *TaskService) LastGoalFailureReason(ctx context.Context, taskID string) string {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil || task == nil {
		return ""
	}
	return parseLastGoalFailureReason(task.GoalEvaluationLog)
}

// parseLastGoalFailureReason extracts the most recent failed-evaluation reason
// from a task's goal_evaluation_log. Empty when no failed evaluation exists yet.
func parseLastGoalFailureReason(log datatypes.JSONSlice[model.GoalEvaluationEntry]) string {
	for i := len(log) - 1; i >= 0; i-- {
		if !log[i].Achieved {
			return log[i].Reason
		}
	}
	return ""
}
