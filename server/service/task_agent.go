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

// AppendProgressLog appends one progress line to the task log.
func (s *TaskService) AppendProgressLog(ctx context.Context, taskID, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}

	current, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("read task for progress update: %w", err)
	}

	newLog := current.ProgressLog
	if newLog != "" && !strings.HasSuffix(newLog, "\n") {
		newLog += "\n"
	}
	newLog += message + "\n"
	if err := s.repo.Tasks().UpdateProgressLog(ctx, taskID, newLog); err != nil {
		return fmt.Errorf("update progress log: %w", err)
	}
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
