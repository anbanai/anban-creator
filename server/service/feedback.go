package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// FeedbackService handles feedback business logic.
type FeedbackService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewFeedbackService creates a new FeedbackService.
func NewFeedbackService(repo repository.Repository, logger *zerolog.Logger) *FeedbackService {
	return &FeedbackService{repo: repo, logger: logger}
}

// Create validates and persists a new feedback entry.
func (s *FeedbackService) Create(ctx context.Context, userID, feedbackType, content string) (*model.Feedback, error) {
	if feedbackType != model.FeedbackTypeBug && feedbackType != model.FeedbackTypeSuggestion {
		return nil, fmt.Errorf("invalid feedback type: %s", feedbackType)
	}
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if len(content) > 1000 {
		return nil, fmt.Errorf("content must be 1000 characters or less")
	}

	feedback := &model.Feedback{
		ID:      uuid.New().String(),
		UserID:  userID,
		Type:    feedbackType,
		Content: content,
	}

	if err := s.repo.Feedbacks().Create(ctx, feedback); err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}

	s.logger.Info().
		Str("feedback_id", feedback.ID).
		Str("user_id", userID).
		Str("type", feedbackType).
		Msg("feedback created")

	return feedback, nil
}
