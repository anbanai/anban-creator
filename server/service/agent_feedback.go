package service

import (
	"context"
	"encoding/json"
	"fmt"

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

// Create stores agent feedback for a task.
func (s *AgentFeedbackService) Create(ctx context.Context, taskID, agentName, scores, errors, optimizations, summary string) (*model.AgentFeedback, error) {
	if _, err := s.repo.Tasks().FindByID(ctx, taskID); err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
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

// FindByTaskID returns all feedback entries for a task.
func (s *AgentFeedbackService) FindByTaskID(ctx context.Context, taskID string) ([]*model.AgentFeedback, error) {
	return s.repo.AgentFeedbacks().FindByTaskID(ctx, taskID)
}
