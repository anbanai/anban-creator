package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

const missingExecutionResultDiagnostic = "agent returned no execution result"

var (
	ErrTaskDeleting      = errors.New("task is being deleted")
	ErrAgentAccessDenied = errors.New("agent access denied")
)

func agentAccessDenied(message string) error {
	return fmt.Errorf("%w: %s", ErrAgentAccessDenied, message)
}

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

func cloneTerminalExecutionResult(result *serveragent.ExecutionResult) (*serveragent.ExecutionResult, error) {
	if result == nil {
		return normalizeTerminalExecutionResult(nil), nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal terminal execution result: %w", err)
	}
	var cloned serveragent.ExecutionResult
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil, fmt.Errorf("clone terminal execution result: %w", err)
	}
	return &cloned, nil
}

// ValidateAgentTaskAccess loads a task and verifies that the authenticated agent
// may act on its behalf. Empty authenticatedUserID means system/admin mode.
func (s *TaskService) ValidateAgentTaskAccess(ctx context.Context, taskID, authenticatedUserID string) (*model.Task, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, agentAccessDenied("task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}

	if authenticatedUserID != "" && authenticatedUserID != "system" && task.UserID != authenticatedUserID {
		return nil, agentAccessDenied("task does not belong to authenticated user")
	}
	if task.DeletingAt != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgentAccessDenied, ErrTaskDeleting)
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

// ValidateAgentCompletionAccess binds a completion credential to the
// current execution identity without requiring that execution to remain active.
// This lets a response-loss retry reach the idempotent completion finalizer.
func (s *TaskService) ValidateAgentCompletionAccess(ctx context.Context, userID, projectID, taskID, executionID string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentAccessDenied("task not found")
	}
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	executionID = strings.TrimSpace(executionID)
	if task.DeletingAt != nil || task.UserID != userID || task.ProjectID != projectID ||
		task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
		return agentAccessDenied("execution token does not match current task execution")
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, executionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentAccessDenied("execution not found")
	}
	if err != nil {
		return fmt.Errorf("find execution: %w", err)
	}
	if execution.TaskID != taskID {
		return agentAccessDenied("execution token does not match execution identity")
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
		s.pubsub.PublishLog(ctx, taskID, message)
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

func (s *TaskService) recordTerminalProviderCost(ctx context.Context, task *model.Task, result *serveragent.ExecutionResult) error {
	if s == nil || s.providerCostSvc == nil || task == nil {
		return nil
	}
	if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) == "" {
		s.logger.Error().Str("task_id", task.ID).Msg("terminal provider cost evidence has no durable execution identity")
		return fmt.Errorf("terminal provider cost evidence has no durable execution identity")
	}
	executionID := strings.TrimSpace(*task.CurrentExecutionID)
	if result == nil || len(result.ModelUsage) == 0 {
		return s.providerCostSvc.MarkExecutionUnreconciled(ctx, executionID, model.BillingExecutionCostReasonMissingTerminalModelUsage)
	}
	if result.CostStatus != serveragent.CostStatusReconciled {
		return s.providerCostSvc.MarkExecutionUnreconciled(ctx, executionID, model.BillingExecutionCostReasonInvalidTerminalModelUsage)
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
	_, err := s.providerCostSvc.FinalizeExecutionTokenCosts(ctx, FinalizeExecutionTokenCostsRequest{
		ExecutionID: executionID, TaskID: task.ID, CatalogID: s.providerCostSvc.catalogID, Entries: entries,
	})
	if err != nil {
		if markErr := s.providerCostSvc.MarkExecutionUnreconciled(ctx, executionID, model.BillingExecutionCostReasonInvalidTerminalModelUsage); markErr != nil {
			return errors.Join(err, fmt.Errorf("mark failed terminal provider cost unreconciled: %w", markErr))
		}
	}
	return err
}
