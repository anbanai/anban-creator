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
func (s *TaskService) AppendProgressLog(ctx context.Context, taskID, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	if err := s.repo.Tasks().AppendProgressLog(ctx, taskID, message); err != nil {
		return fmt.Errorf("append progress log: %w", err)
	}
	// Notify subscribers of the new progress message.
	s.publishProgressEvent(ctx, &ProgressEvent{
		TaskID:  taskID,
		Message: message,
	})
	return nil
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
	return nil
}
