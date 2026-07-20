package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// AgentFeedbackService handles agent feedback CRUD.
type AgentFeedbackService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewAgentFeedbackService creates a new AgentFeedbackService.
func NewAgentFeedbackService(repo repository.Repository, logger *zerolog.Logger) *AgentFeedbackService {
	return &AgentFeedbackService{repo: repo, logger: logger}
}

// Create idempotently stores the latest feedback for one task/agent run.
func (s *AgentFeedbackService) Create(ctx context.Context, taskID, agentName, scores, errors, optimizations, summary string) (*model.AgentFeedback, error) {
	taskID = strings.TrimSpace(taskID)
	agentName = strings.TrimSpace(agentName)
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if !isLocalFeedbackTaskID(taskID) {
		if _, err := s.repo.Tasks().FindByID(ctx, taskID); err != nil {
			return nil, fmt.Errorf("task not found: %w", err)
		}
	} else if s.logger != nil {
		s.logger.Info().Str("task_id", taskID).Str("agent_name", agentName).Msg("accepting local agent feedback without persisted task")
	}
	if agentName == "" {
		return nil, fmt.Errorf("agent_name is required")
	}
	if scores != "" && !json.Valid([]byte(scores)) {
		return nil, fmt.Errorf("scores must be valid JSON")
	}

	feedback := &model.AgentFeedback{
		ID:            uuid.New().String(),
		TaskID:        taskID,
		AgentName:     agentName,
		Scores:        scores,
		Errors:        errors,
		Optimizations: optimizations,
		Summary:       summary,
	}
	if err := s.repo.AgentFeedbacks().Create(ctx, feedback); err != nil {
		return nil, err
	}
	return feedback, nil
}

func isLocalFeedbackTaskID(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	return strings.HasPrefix(taskID, "local-") || strings.HasPrefix(taskID, "video-local-")
}

// FindByTaskID returns all feedback entries for a task.
func (s *AgentFeedbackService) FindByTaskID(ctx context.Context, taskID string) ([]*model.AgentFeedback, error) {
	return s.repo.AgentFeedbacks().FindByTaskID(ctx, strings.TrimSpace(taskID))
}
