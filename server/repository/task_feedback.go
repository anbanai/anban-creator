package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type taskFeedbackRepository struct{ db *gorm.DB }

func newTaskFeedbackRepository(db *gorm.DB) TaskFeedbackRepository {
	return &taskFeedbackRepository{db: db}
}

func (r *taskFeedbackRepository) FindByTaskAndUser(ctx context.Context, taskID, userID string) (*model.TaskFeedback, error) {
	var feedback model.TaskFeedback
	if err := r.db.WithContext(ctx).Where("task_id = ? AND user_id = ?", taskID, userID).First(&feedback).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &feedback, nil
}

func (r *taskFeedbackRepository) Upsert(ctx context.Context, feedback *model.TaskFeedback) error {
	db := r.db.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"rating", "content", "updated_at"}),
	}).Create(feedback).Error; err != nil {
		return err
	}
	var canonical model.TaskFeedback
	if err := db.Where("task_id = ? AND user_id = ?", feedback.TaskID, feedback.UserID).First(&canonical).Error; err != nil {
		return err
	}
	*feedback = canonical
	return nil
}
