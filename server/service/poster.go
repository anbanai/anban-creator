package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"

	"gorm.io/gorm"
)

// PosterService handles poster generation business logic.
type PosterService struct {
	repo   repository.Repository
	logger *zerolog.Logger
	// ImageSvc will be set later when image generation is integrated.
}

// NewPosterService creates a new PosterService.
func NewPosterService(repo repository.Repository, logger *zerolog.Logger) *PosterService {
	return &PosterService{repo: repo, logger: logger}
}

// Create creates a new poster task with drafting status.
func (s *PosterService) Create(ctx context.Context, userID string, inputContent json.RawMessage, stylePreference string, templateID *string) (*model.PosterTask, error) {
	task := &model.PosterTask{
		ID:              uuid.New().String(),
		UserID:          userID,
		InputContent:    inputContent,
		StylePreference: stylePreference,
		TemplateID:      templateID,
		Status:          "drafting",
	}

	if err := s.repo.PosterTasks().Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create poster task: %w", err)
	}

	s.logger.Info().
		Str("task_id", task.ID).
		Str("user_id", userID).
		Str("style", stylePreference).
		Msg("poster task created")

	return task, nil
}

// GetByID returns a poster task by ID, verifying ownership.
func (s *PosterService) GetByID(ctx context.Context, id, userID string) (*model.PosterTask, error) {
	task, err := s.repo.PosterTasks().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("poster task not found: %s", id)
		}
		return nil, fmt.Errorf("find poster task by id: %w", err)
	}

	if task.UserID != userID {
		return nil, fmt.Errorf("poster task does not belong to user: %s", id)
	}

	return task, nil
}

// ListByUserID returns paginated poster tasks for a user.
func (s *PosterService) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.PosterTask, int64, error) {
	tasks, err := s.repo.PosterTasks().FindByUserID(ctx, userID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list poster tasks: %w", err)
	}

	total, err := s.repo.PosterTasks().CountByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("count poster tasks: %w", err)
	}

	return tasks, total, nil
}

// UpdateConversation saves the conversation JSON for a poster task.
func (s *PosterService) UpdateConversation(ctx context.Context, id string, conversation json.RawMessage) error {
	task, err := s.repo.PosterTasks().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("poster task not found: %s", id)
		}
		return fmt.Errorf("find poster task for conversation update: %w", err)
	}

	task.Conversation = conversation
	if err := s.repo.PosterTasks().Update(ctx, task); err != nil {
		return fmt.Errorf("update conversation: %w", err)
	}

	return nil
}

// UpdateImages saves the generated images JSON and sets status to "completed".
func (s *PosterService) UpdateImages(ctx context.Context, id string, images json.RawMessage) error {
	task, err := s.repo.PosterTasks().FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("poster task not found: %s", id)
		}
		return fmt.Errorf("find poster task for images update: %w", err)
	}

	task.Images = images
	task.Status = "completed"
	if err := s.repo.PosterTasks().Update(ctx, task); err != nil {
		return fmt.Errorf("update images: %w", err)
	}

	s.logger.Info().Str("task_id", id).Msg("poster task images updated, status completed")
	return nil
}

// StartGeneration sets the poster task status to "generating".
func (s *PosterService) StartGeneration(ctx context.Context, id string) error {
	if err := s.repo.PosterTasks().UpdateStatus(ctx, id, "generating"); err != nil {
		return fmt.Errorf("start generation: %w", err)
	}

	s.logger.Info().Str("task_id", id).Msg("poster generation started")
	return nil
}

// FailGeneration sets the poster task status to "failed" with an error message.
func (s *PosterService) FailGeneration(ctx context.Context, id, errMsg string) error {
	if err := s.repo.PosterTasks().UpdateStatusAndError(ctx, id, "failed", errMsg); err != nil {
		return fmt.Errorf("fail generation: %w", err)
	}

	s.logger.Warn().Str("task_id", id).Str("error", errMsg).Msg("poster generation failed")
	return nil
}

// CleanupOldCompleted deletes poster tasks completed more than 30 days ago.
func (s *PosterService) CleanupOldCompleted(ctx context.Context) error {
	tasks, err := s.repo.PosterTasks().FindCompletedOlderThan(ctx, time.Now().AddDate(0, 0, -30))
	if err != nil {
		return fmt.Errorf("find old poster tasks: %w", err)
	}
	for _, t := range tasks {
		s.logger.Info().Str("task_id", t.ID).Msg("cleaning up old poster task")
		if err := s.repo.PosterTasks().Delete(ctx, t.ID); err != nil {
			s.logger.Warn().Err(err).Str("task_id", t.ID).Msg("failed to delete old poster task")
		}
	}
	return nil
}
