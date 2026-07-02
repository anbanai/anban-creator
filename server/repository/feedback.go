package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type feedbackRepository struct {
	db *gorm.DB
}

func newFeedbackRepository(db *gorm.DB) FeedbackRepository {
	return &feedbackRepository{db: db}
}

func (r *feedbackRepository) Create(ctx context.Context, feedback *model.Feedback) error {
	return r.db.WithContext(ctx).Create(feedback).Error
}
