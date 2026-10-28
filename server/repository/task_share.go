package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type TaskShareRepository interface {
	FindByTaskID(context.Context, string) (*model.TaskShare, error)
	FindByToken(context.Context, string) (*model.TaskShare, error)
	Save(context.Context, *model.TaskShare) error
}

type taskShareRepository struct{ db *gorm.DB }

func newTaskShareRepository(db *gorm.DB) TaskShareRepository { return &taskShareRepository{db: db} }

func (r *taskShareRepository) find(ctx context.Context, column, value string) (*model.TaskShare, error) {
	var share model.TaskShare
	result := r.db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Where(column+" = ?", value).Limit(1).Find(&share)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &share, nil
}

func (r *taskShareRepository) FindByTaskID(ctx context.Context, id string) (*model.TaskShare, error) {
	return r.find(ctx, "task_id", id)
}

func (r *taskShareRepository) FindByToken(ctx context.Context, token string) (*model.TaskShare, error) {
	return r.find(ctx, "token", token)
}

func (r *taskShareRepository) Save(ctx context.Context, share *model.TaskShare) error {
	// SQL tracing includes bound token values even for failed writes.
	return r.db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Save(share).Error
}
