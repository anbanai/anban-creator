package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

const missingExecutionResultDiagnostic = "agent returned no execution result"

var ErrTaskDeleting = errors.New("task is being deleted")

func normalizeTerminalExecutionResult(result *serveragent.ExecutionResult) *serveragent.ExecutionResult {
	if result != nil {
		return result
	}
	return &serveragent.ExecutionResult{
		Success:    false,
		Error:      missingExecutionResultDiagnostic,
		CostStatus: serveragent.CostStatusUnreconciled,
		CostDiagnostics: []serveragent.CostDiagnostic{{
			Code: serveragent.CostDiagnosticMissingTerminalModelUsage,
		}},
	}
}

// ValidateAgentTaskAccess loads a task and verifies that the authenticated agent
// may act on its behalf. Empty authenticatedUserID means system/admin mode.
func (s *TaskService) ValidateAgentTaskAccess(ctx context.Context, taskID, authenticatedUserID string) (*model.Task, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}

	if authenticatedUserID != "" && authenticatedUserID != "system" && task.UserID != authenticatedUserID {
		return nil, fmt.Errorf("task does not belong to authenticated user")
	}
	if task.DeletingAt != nil {
		return nil, ErrTaskDeleting
	}
	return task, nil
}

// ValidateAgentExecutionAccess rejects stale execution JWTs even when they
// belong to the same user and task as the current attempt.
func (s *TaskService) ValidateAgentExecutionAccess(ctx context.Context, userID, projectID, taskID, executionID string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task.DeletingAt != nil || task.UserID != userID || task.ProjectID != projectID || task.Status != model.TaskStatusRunning || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
		return fmt.Errorf("execution token does not match current task execution")
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return fmt.Errorf("find execution: %w", err)
	}
	if execution.TaskID != taskID || execution.Status != model.TaskExecutionRunning || !execution.Started || execution.CompletedAt != nil {
		return fmt.Errorf("execution token is not authorized for an active execution")
	}
	return nil
}

// ValidateLocalAgentExecutionAccess binds an API-key-authenticated desktop
// request to the active local execution. API keys identify a user, not an
// execution, so the requested execution ID must still match both the task's
// current authority and a running local-claimed execution record.
func (s *TaskService) ValidateLocalAgentExecutionAccess(ctx context.Context, task *model.Task, authenticatedUserID, executionID string) error {
	if task == nil || task.ExecutionTarget != model.ExecutionTargetLocalClaimed {
		return fmt.Errorf("task is not running on a local executor")
	}
	executionID = strings.TrimSpace(executionID)
	if err := s.ValidateAgentExecutionAccess(ctx, authenticatedUserID, task.ProjectID, task.ID, executionID); err != nil {
		return err
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return fmt.Errorf("find local execution: %w", err)
	}
	if execution.Target != model.ExecutionTargetLocalClaimed {
		return fmt.Errorf("execution is not a local claimed execution")
	}
	return nil
}

// UpdateHeartbeat refreshes a task's last_heartbeat_at, marking it as actively
// working. Called by the agent /progress endpoint on every report so long-running
// local-execution tasks are not force-failed by the stuck-task reaper
// (plan_checker.go:reapStuckTasks, 5-min threshold) before they complete. The
// cloud path also refreshes heartbeats via HandleExecution's HeartbeatFunc.
func (s *TaskService) UpdateHeartbeat(ctx context.Context, taskID string) error {
	if err := s.repo.Tasks().UpdateHeartbeat(ctx, taskID); err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}
	return nil
}

// UpdateAgentHeartbeat atomically refreshes both the task-level compatibility
// heartbeat and the durable execution heartbeat used by RuntimeReconciler.
func (s *TaskService) UpdateAgentHeartbeat(ctx context.Context, taskID, executionID string) error {
	if strings.TrimSpace(executionID) == "" {
		return s.UpdateHeartbeat(ctx, taskID)
	}
	now := time.Now()
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := s.updateExecutionHeartbeatFirst(ctx, tx, taskID, executionID, now, false); err != nil {
			return err
		}
		if err := tx.Tasks().UpdateHeartbeat(ctx, taskID); err != nil {
			return fmt.Errorf("update task heartbeat: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("update agent heartbeat: %w", err)
	}
	return nil
}

// updateExecutionHeartbeatFirst centralizes the cross-table lock order shared
// by legacy heartbeat and structured progress transactions. Any transaction
// touching both rows must touch task_executions before tasks.
func (s *TaskService) updateExecutionHeartbeatFirst(ctx context.Context, tx repository.Repository, taskID, executionID string, now time.Time, requireActive bool) error {
	if requireActive {
		matched, err := tx.TaskExecutions().LockActiveForProgress(ctx, executionID, taskID)
		if err != nil {
			return fmt.Errorf("lock execution for structured progress: %w", err)
		}
		if !matched {
			return ErrStaleTaskExecution
		}
	}
	// Do not infer predicate matching from UPDATE RowsAffected. MySQL reports
	// zero when DATETIME(3) already equals now unless clientFoundRows is enabled.
	if err := tx.TaskExecutions().UpdateHeartbeat(ctx, executionID, now); err != nil {
		return fmt.Errorf("update execution heartbeat: %w", err)
	}
	return nil
}

// AppendProgressLog appends one progress line to the task log atomically.
// If Redis pub/sub is available, it also publishes a progress event so
// SSE handlers on any replica can push updates to their clients immediately.
func (s *TaskService) AppendProgressLog(ctx context.Context, taskID, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	if err := s.repo.Tasks().AppendProgressLog(ctx, taskID, message); err != nil {
		return fmt.Errorf("append progress log: %w", err)
	}
	// Publish to Redis for real-time SSE delivery across replicas.
	if s.pubsub != nil {
		s.pubsub.PublishProgress(ctx, taskID, message)
	}
	return nil
}

// UpdateProgress records a compatibility progress update for hosts without
// Claude SDK Task lifecycle hooks, notably Codex and DSH. The MCP
// update_task_progress handler is the compatibility entry point.
//
// When percent is not provided (<=0), the server looks up a default percent
// from legacyStagePercentByType using the task's Type. This keeps legacy host
// workflows advancing without requiring every explicit MCP call to pass
// progress_percent. The resolved percent is persisted on the task row so the
// Studio UI can render it across reloads, and a structured SSE event is
// published for real-time updates.
//
// The persisted percent is monotonic: a stage reporting a lower percent than
// the current value is logged but does not roll the column backward (guards
// against out-of-order or duplicate stage emissions).
func (s *TaskService) UpdateProgress(ctx context.Context, taskID, stage, title, description string, percent int) error {
	taskType, currentProgress, err := s.repo.Tasks().GetTypeAndProgress(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load task meta: %w", err)
	}

	if percent <= 0 {
		if p := legacyDefaultPercentForStage(taskType, stage); p > 0 {
			percent = p
		}
	}
	return s.persistStructuredProgress(ctx, taskID, stage, title, description, percent, currentProgress)
}

// UpdateProgressFromAgent is the authoritative managed Claude progress path.
// It validates events against the immutable Agent Pack contract frozen on the
// execution. State selects the declared percentage and ordinal transition;
// client percent must match it. This path never consults the legacy host stage
// fallback map used by UpdateProgress. Heartbeats, raw logs, and structured
// state commit atomically for the current running execution. Duplicate/lower
// sequence events are idempotent and refresh no heartbeat or log.
func (s *TaskService) UpdateProgressFromAgent(ctx context.Context, taskID, executionID, stage, state, title, description string, percent int, logs ...string) error {
	if strings.TrimSpace(executionID) == "" {
		return ErrAgentProgressExecutionMismatch
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		return fmt.Errorf("load agent progress execution: %w", err)
	}
	if execution.TaskID != taskID {
		return ErrAgentProgressExecutionMismatch
	}
	progressContract, err := resolveFrozenExecutionProgressContract(execution)
	if err != nil {
		return err
	}
	var declaredStage agentpack.ProgressStage
	stageOrdinal := -1
	stageID := strings.TrimSpace(stage)
	for ordinal, candidate := range progressContract {
		if candidate.ID == stageID {
			declaredStage = candidate
			stageOrdinal = ordinal
			break
		}
	}
	if declaredStage.ID == "" {
		return ErrAgentProgressUnknownStage
	}
	if title != declaredStage.Title {
		return ErrAgentProgressTitleMismatch
	}
	state = strings.TrimSpace(state)
	var expectedPercent, sequence int
	switch state {
	case "active":
		expectedPercent = declaredStage.ActivePercent
		sequence = stageOrdinal*2 + 1
	case "complete":
		expectedPercent = declaredStage.CompletePercent
		sequence = stageOrdinal*2 + 2
	default:
		return ErrAgentProgressStateMismatch
	}
	if percent != expectedPercent {
		return ErrAgentProgressPercentMismatch
	}
	payload := model.ProgressPayload{
		Stage:       declaredStage.ID,
		State:       state,
		Title:       declaredStage.Title,
		Description: description,
		Percent:     expectedPercent,
	}
	normalizedLogs := make([]string, 0, len(logs))
	for _, line := range logs {
		if line = strings.TrimSpace(line); line != "" {
			normalizedLogs = append(normalizedLogs, line)
		}
	}
	var advanced bool
	var persisted model.ProgressPayload
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		now := time.Now()
		if err := s.updateExecutionHeartbeatFirst(ctx, tx, taskID, executionID, now, true); err != nil {
			return err
		}
		for _, line := range normalizedLogs {
			if err := tx.Tasks().AppendProgressLog(ctx, taskID, line); err != nil {
				return fmt.Errorf("append structured progress log: %w", err)
			}
		}
		advanced, persisted, err = tx.Tasks().AdvanceStructuredProgress(ctx, taskID, executionID, sequence, payload)
		if err != nil {
			return fmt.Errorf("advance structured progress: %w", err)
		}
		if !advanced {
			task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrStaleTaskExecution
				}
				return fmt.Errorf("load task after structured progress CAS miss: %w", err)
			}
			if task.Status != model.TaskStatusRunning || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
				return ErrStaleTaskExecution
			}
			if task.ProgressSequence >= sequence {
				// Duplicate/lower-sequence reports are idempotent and refresh no
				// heartbeats. The sentinel rolls back execution heartbeat/raw logs.
				return errDuplicateAgentProgress
			}
			return errAgentProgressCASLost
		}
		if err := tx.Tasks().UpdateHeartbeat(ctx, taskID); err != nil {
			return fmt.Errorf("update task heartbeat for structured progress: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errDuplicateAgentProgress) {
			return nil
		}
		return err
	}
	if s.pubsub != nil {
		for _, line := range normalizedLogs {
			s.pubsub.PublishProgress(ctx, taskID, line)
		}
		s.pubsub.PublishProgressStructured(ctx, taskID, persisted.Stage, persisted.State, persisted.Title, persisted.Description, persisted.Percent)
	}
	return nil
}

var errDuplicateAgentProgress = errors.New("duplicate agent structured progress")
var errAgentProgressCASLost = errors.New("agent structured progress CAS lost while task guards remained current")

func (s *TaskService) persistStructuredProgress(ctx context.Context, taskID, stage, title, description string, percent, currentProgress int) error {

	// Monotonic guard: never roll progress backward. Stages may fire out of
	// order (retry, parallel branches); the bar should only advance.
	if percent > currentProgress {
		if err := s.repo.Tasks().UpdateProgressColumn(ctx, taskID, percent); err != nil {
			if s.logger != nil {
				s.logger.Warn().Err(err).Str("task_id", taskID).Msg("persist progress column")
			}
		}
	}

	// Single source of truth for the structured progress payload. The progress_log
	// line, the latest_progress column, and the SSE event all derive from this,
	// so a schema change (e.g. adding a sub_stage field) propagates everywhere
	// without three separate encoding sites drifting apart.
	payload := model.ProgressPayload{
		Stage:       stage,
		Title:       title,
		Description: description,
		Percent:     percent,
	}
	msg, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}

	if err := s.repo.Tasks().AppendProgressLog(ctx, taskID, string(msg)); err != nil {
		return fmt.Errorf("append progress log: %w", err)
	}
	// Persist structured payload to dedicated column so Studio can render the
	// current stage across reloads without parsing the longtext progress_log
	// (which also carries "Using tool: ..." noise from the agent executor).
	// Failure is non-fatal — same precedence as the progress column write above.
	if err := s.repo.Tasks().UpdateLatestProgress(ctx, taskID, payload); err != nil {
		if s.logger != nil {
			s.logger.Warn().Err(err).Str("task_id", taskID).Msg("persist latest_progress")
		}
	}
	if s.pubsub != nil {
		s.pubsub.PublishProgressStructured(ctx, taskID, payload.Stage, payload.State, payload.Title, payload.Description, payload.Percent)
	}
	return nil
}

func marshalExecutionEvidence(result *serveragent.ExecutionResult) (string, error) {
	if result == nil {
		return "", fmt.Errorf("execution result is required")
	}
	if result.CostStatus == "" {
		result.CostStatus = serveragent.CostStatusUnreconciled
		result.CostDiagnostics = []serveragent.CostDiagnostic{{Code: serveragent.CostDiagnosticMissingTerminalModelUsage}}
	}
	publicResult := *result
	publicResult.ModelUsage = nil
	publicResult.CostStatus = ""
	publicResult.CostDiagnostics = nil
	publicResult.Model = ""
	resultJSON, err := json.Marshal(&publicResult)
	if err != nil {
		return "", fmt.Errorf("marshal execution result: %w", err)
	}
	return string(resultJSON), nil
}

// UpdateExecutionResult atomically stores result JSON and typed cost evidence.
func (s *TaskService) UpdateExecutionResult(ctx context.Context, taskID string, result *serveragent.ExecutionResult) error {
	resultJSON, err := marshalExecutionEvidence(result)
	if err != nil {
		return err
	}
	matched, err := s.repo.Tasks().UpdateExecutionEvidence(ctx, taskID, resultJSON, result.ModelUsage, result.CostStatus)
	if err != nil {
		return fmt.Errorf("persist execution evidence: %w", err)
	}
	if !matched {
		return fmt.Errorf("persist execution evidence: task %s not found", taskID)
	}
	return nil
}

func (s *TaskService) updateExecutionResultForExecution(ctx context.Context, taskID, executionID string, result *serveragent.ExecutionResult) error {
	resultJSON, err := marshalExecutionEvidence(result)
	if err != nil {
		return err
	}
	matched, err := s.repo.Tasks().UpdateExecutionEvidenceForExecution(ctx, taskID, executionID, resultJSON, result.ModelUsage, result.CostStatus)
	if err != nil {
		return fmt.Errorf("persist execution evidence for current attempt: %w", err)
	}
	if !matched {
		return ErrStaleTaskExecution
	}
	return nil
}

func (s *TaskService) recordTerminalProviderCost(ctx context.Context, task *model.Task, result *serveragent.ExecutionResult) {
	if s == nil || s.providerCostSvc == nil || task == nil {
		return
	}
	if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) == "" {
		s.logger.Error().Str("task_id", task.ID).Msg("terminal provider cost evidence has no durable execution identity")
		return
	}
	executionID := strings.TrimSpace(*task.CurrentExecutionID)
	if result == nil || len(result.ModelUsage) == 0 {
		if err := s.providerCostSvc.MarkExecutionUnreconciled(ctx, executionID, model.BillingExecutionCostReasonMissingTerminalModelUsage); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Str("execution_id", executionID).Msg("mark terminal provider cost unreconciled")
		}
		return
	}
	if result.CostStatus != serveragent.CostStatusReconciled {
		if err := s.providerCostSvc.MarkExecutionUnreconciled(ctx, executionID, model.BillingExecutionCostReasonInvalidTerminalModelUsage); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Str("execution_id", executionID).Msg("mark invalid terminal provider cost evidence")
		}
		return
	}
	entries := make([]ExecutionTokenCostEntry, 0, len(result.ModelUsage))
	for _, usage := range result.ModelUsage {
		entries = append(entries, ExecutionTokenCostEntry{
			Provider: usage.Provider, Model: usage.Model,
			IdempotencyKey: executionID + "/" + usage.Provider + "/" + usage.Model,
			Usage:          TokenUsage{Input: usage.InputTokens, CacheRead: usage.CacheReadInputTokens, CacheCreation: usage.CacheCreationInputTokens, Output: usage.OutputTokens},
			Source:         string(model.BillingProviderCostSourceClaudeResult),
		})
	}
	if _, err := s.providerCostSvc.FinalizeExecutionTokenCosts(ctx, FinalizeExecutionTokenCostsRequest{
		ExecutionID: executionID, TaskID: task.ID, CatalogID: s.providerCostSvc.catalogID, Entries: entries,
	}); err != nil {
		s.logger.Error().Err(err).Str("task_id", task.ID).Str("execution_id", executionID).Msg("record terminal provider cost; typed evidence remains retryable")
		if markErr := s.providerCostSvc.MarkExecutionUnreconciled(ctx, executionID, model.BillingExecutionCostReasonInvalidTerminalModelUsage); markErr != nil {
			s.logger.Error().Err(markErr).Str("task_id", task.ID).Str("execution_id", executionID).Msg("mark failed terminal provider cost unreconciled")
		}
	}
}
