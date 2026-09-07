package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var (
	ErrTaskFeedbackForbidden      = errors.New("task feedback forbidden")
	ErrTaskFeedbackNotCompleted   = errors.New("task feedback requires completed task")
	ErrTaskFeedbackInvalidRating  = errors.New("task feedback rating must be between 1 and 5")
	ErrTaskFeedbackContentTooLong = errors.New("task feedback content must be 1000 characters or less")
)

func (s *FeedbackService) GetTaskFeedback(ctx context.Context, userID, taskID string) (*model.TaskFeedback, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, ErrTaskFeedbackForbidden
	}
	if task.Status != model.TaskStatusCompleted {
		return nil, ErrTaskFeedbackNotCompleted
	}
	return s.repo.TaskFeedbacks().FindByTaskAndUser(ctx, taskID, userID)
}

func (s *FeedbackService) UpsertTaskFeedback(ctx context.Context, userID, taskID string, rating int, content string) (*model.TaskFeedback, error) {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, ErrTaskFeedbackForbidden
	}
	if task.Status != model.TaskStatusCompleted {
		return nil, ErrTaskFeedbackNotCompleted
	}
	if rating < 1 || rating > 5 {
		return nil, ErrTaskFeedbackInvalidRating
	}
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) > 1000 {
		return nil, ErrTaskFeedbackContentTooLong
	}
	feedback := &model.TaskFeedback{
		ID: uuid.NewString(), TaskID: taskID, UserID: userID, Rating: rating, Content: content,
	}
	if err := s.repo.TaskFeedbacks().Upsert(ctx, feedback); err != nil {
		return nil, fmt.Errorf("upsert task feedback: %w", err)
	}
	if s.logger != nil {
		s.logger.Info().Str("task_id", taskID).Str("user_id", userID).Int("rating", rating).Msg("task feedback saved")
	}
	return feedback, nil
}
