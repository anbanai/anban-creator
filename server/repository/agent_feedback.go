package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type agentFeedbackRepository struct {
	db *gorm.DB
}

func newAgentFeedbackRepository(db *gorm.DB) AgentFeedbackRepository {
	return &agentFeedbackRepository{db: db}
}

func (r *agentFeedbackRepository) Create(ctx context.Context, feedback *model.AgentFeedback) error {
	db := r.db.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}, {Name: "agent_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"scores", "errors", "optimizations", "summary", "updated_at",
		}),
	}).Create(feedback).Error; err != nil {
		return err
	}

	var canonical model.AgentFeedback
	if err := db.Where("task_id = ? AND agent_name = ?", feedback.TaskID, feedback.AgentName).First(&canonical).Error; err != nil {
		return err
	}
	*feedback = canonical
	return nil
}

func (r *agentFeedbackRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.AgentFeedback, error) {
	var feedbacks []*model.AgentFeedback
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at DESC").Find(&feedbacks).Error; err != nil {
		return nil, err
	}
	return feedbacks, nil
}
