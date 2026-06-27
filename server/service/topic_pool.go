package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// TopicPoolService manages the topic pool per project.
type TopicPoolService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewTopicPoolService creates a new TopicPoolService.
func NewTopicPoolService(repo repository.Repository, logger *zerolog.Logger) *TopicPoolService {
	return &TopicPoolService{repo: repo, logger: logger}
}

const maxTopicLength = 500

// Add creates one or more topics in the pool for the given project.
func (s *TopicPoolService) Add(ctx context.Context, userID, projectID string, topics []string) ([]*model.TopicPool, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if len(topics) == 0 {
		return nil, fmt.Errorf("at least one topic is required")
	}
	if err := s.validateProjectOwnership(ctx, userID, projectID); err != nil {
		return nil, err
	}

	items := make([]*model.TopicPool, 0, len(topics))
	for _, t := range topics {
		if t == "" {
			continue
		}
		if len([]rune(t)) > maxTopicLength {
			return nil, fmt.Errorf("topic exceeds %d character limit: %.50q...", maxTopicLength, t)
		}
		items = append(items, &model.TopicPool{
			UserID:    userID,
			ProjectID: projectID,
			Topic:     t,
			Status:    model.TopicStatusUnused,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one non-empty topic is required")
	}

	if err := s.repo.TopicPools().CreateBatch(ctx, items); err != nil {
		return nil, fmt.Errorf("create topics: %w", err)
	}
	return items, nil
}

// List returns topics for a project with optional status filter and pagination.
func (s *TopicPoolService) List(ctx context.Context, userID, projectID, status string, offset, limit int) ([]*model.TopicPool, int64, error) {
	if err := s.validateProjectOwnership(ctx, userID, projectID); err != nil {
		return nil, 0, err
	}
	return s.repo.TopicPools().FindByProject(ctx, projectID, status, offset, limit)
}

// Claim atomically claims the next unused topic for the project.
// Returns the topic text and its ID, or empty string/0 if none available.
func (s *TopicPoolService) Claim(ctx context.Context, userID, projectID string) (string, uint, error) {
	topic, err := s.repo.TopicPools().ClaimOne(ctx, userID, projectID)
	if err != nil {
		return "", 0, fmt.Errorf("claim topic: %w", err)
	}
	if topic == nil {
		return "", 0, nil
	}
	s.logger.Info().Uint("topic_id", topic.ID).Str("project_id", projectID).Str("topic", topic.Topic).Msg("claimed topic from pool")
	return topic.Topic, topic.ID, nil
}

// ClaimForTask atomically claims a topic and associates it with the given task ID.
func (s *TopicPoolService) ClaimForTask(ctx context.Context, userID, projectID, taskID string) (string, error) {
	topic, err := s.repo.TopicPools().ClaimWithTask(ctx, userID, projectID, taskID)
	if err != nil {
		return "", fmt.Errorf("claim topic: %w", err)
	}
	if topic == nil {
		return "", nil
	}
	s.logger.Info().Uint("topic_id", topic.ID).Str("project_id", projectID).Str("task_id", taskID).Msg("claimed topic from pool for task")
	return topic.Topic, nil
}

// ReleaseForTask releases the topic bound to the given task back to the pool.
// Called when a task that pre-claimed a topic (server-side ClaimForTask) fails to
// persist, so the topic is reclaimable instead of permanently orphaned. Idempotent
// and safe to call when no topic is bound to the task.
func (s *TopicPoolService) ReleaseForTask(ctx context.Context, taskID string) error {
	if err := s.repo.TopicPools().ResetByTask(ctx, taskID); err != nil {
		return fmt.Errorf("release topic for task: %w", err)
	}
	return nil
}

// Reset marks a used topic as unused again.
func (s *TopicPoolService) Reset(ctx context.Context, userID, projectID string, id uint) error {
	topic, err := s.repo.TopicPools().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find topic: %w", err)
	}
	if topic.UserID != userID {
		return fmt.Errorf("topic not owned by user")
	}
	if topic.ProjectID != projectID {
		return fmt.Errorf("topic does not belong to this project")
	}
	if topic.Status != model.TopicStatusUsed {
		return fmt.Errorf("only used topics can be reset")
	}
	return s.repo.TopicPools().ResetStatus(ctx, id)
}

// Delete removes a topic from the pool.
func (s *TopicPoolService) Delete(ctx context.Context, userID, projectID string, id uint) error {
	topic, err := s.repo.TopicPools().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find topic: %w", err)
	}
	if topic.UserID != userID {
		return fmt.Errorf("topic not owned by user")
	}
	if topic.ProjectID != projectID {
		return fmt.Errorf("topic does not belong to this project")
	}
	return s.repo.TopicPools().Delete(ctx, id)
}

func (s *TopicPoolService) validateProjectOwnership(ctx context.Context, userID, projectID string) error {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return fmt.Errorf("project not owned by user")
	}
	return nil
}
