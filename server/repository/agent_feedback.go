package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"
	"gorm.io/gorm"
)

type agentFeedbackRepository struct {
	db *gorm.DB
}

func newAgentFeedbackRepository(db *gorm.DB) AgentFeedbackRepository {
	return &agentFeedbackRepository{db: db}
}

func (r *agentFeedbackRepository) Create(ctx context.Context, feedback *model.AgentFeedback) error {
	return r.db.WithContext(ctx).Create(feedback).Error
}

func (r *agentFeedbackRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.AgentFeedback, error) {
	var feedbacks []*model.AgentFeedback
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at DESC").Find(&feedbacks).Error; err != nil {
		return nil, err
	}
	return feedbacks, nil
}
