package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

// PosterTaskRepository provides access to the poster_tasks table.
type PosterTaskRepository interface {
	Create(ctx context.Context, task *model.PosterTask) error
	FindByID(ctx context.Context, id string) (*model.PosterTask, error)
	FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.PosterTask, error)
	CountByUserID(ctx context.Context, userID string) (int64, error)
	Update(ctx context.Context, task *model.PosterTask) error
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error
	FindCompletedOlderThan(ctx context.Context, before time.Time) ([]*model.PosterTask, error)
	Delete(ctx context.Context, id string) error
}

type posterTaskRepository struct {
	db *gorm.DB
}

func newPosterTaskRepository(db *gorm.DB) PosterTaskRepository {
	return &posterTaskRepository{db: db}
}

func (r *posterTaskRepository) Create(ctx context.Context, task *model.PosterTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *posterTaskRepository) FindByID(ctx context.Context, id string) (*model.PosterTask, error) {
	var task model.PosterTask
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *posterTaskRepository) FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.PosterTask, error) {
	var tasks []*model.PosterTask
	q := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *posterTaskRepository) CountByUserID(ctx context.Context, userID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.PosterTask{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *posterTaskRepository) Update(ctx context.Context, task *model.PosterTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *posterTaskRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).Model(&model.PosterTask{}).Where("id = ?", id).Update("status", status).Error
}

func (r *posterTaskRepository) UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error {
	return r.db.WithContext(ctx).Model(&model.PosterTask{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":        status,
			"error_message": errorMsg,
		}).Error
}

func (r *posterTaskRepository) FindCompletedOlderThan(ctx context.Context, before time.Time) ([]*model.PosterTask, error) {
	var tasks []*model.PosterTask
	err := r.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", "completed", before).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *posterTaskRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.PosterTask{}).Error
}
