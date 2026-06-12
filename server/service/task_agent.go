package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	serveragent "github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
)

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
	return task, nil
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

// UpdateProgress records a structured progress update for a task stage.
func (s *TaskService) UpdateProgress(ctx context.Context, taskID, stage, title, description string, percent int) error {
	payload := map[string]any{
		"stage": stage,
		"title": title,
	}
	if description != "" {
		payload["description"] = description
	}
	if percent > 0 {
		payload["percent"] = percent
	}
	msg, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}
	return s.AppendProgressLog(ctx, taskID, string(msg))
}

// UpdateExecutionResult stores the latest execution result JSON for a task.
func (s *TaskService) UpdateExecutionResult(ctx context.Context, taskID string, result *serveragent.ExecutionResult) error {
	if result == nil {
		return nil
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal execution result: %w", err)
	}
	if err := s.repo.Tasks().UpdateResult(ctx, taskID, string(resultJSON)); err != nil {
		return fmt.Errorf("persist execution result: %w", err)
	}

	// Populate denormalized token/cost columns for efficient aggregation.
	// Only write when we have usage data — skip to keep columns NULL for tasks without results.
	if result.TokenUsage != nil {
		var costUSD float64
		if result.TotalCostUSD != nil {
			costUSD = *result.TotalCostUSD
		}
		if err := s.repo.Tasks().UpdateTokenUsage(ctx, taskID,
			int64(result.TokenUsage.InputTokens),
			int64(result.TokenUsage.OutputTokens),
			int64(result.TokenUsage.CacheReadTokens),
			int64(result.TokenUsage.CacheCreationTokens),
			costUSD); err != nil {
			s.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to update denormalized token usage columns")
		}
	}

	return nil
}
