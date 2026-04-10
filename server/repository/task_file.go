package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

type taskFileRepository struct {
	db *gorm.DB
}

func newTaskFileRepository(db *gorm.DB) TaskFileRepository {
	return &taskFileRepository{db: db}
}

func (r *taskFileRepository) Create(ctx context.Context, file *model.TaskFile) error {
	if file.ID == "" {
		file.ID = uuid.New().String()
	}
	return r.db.WithContext(ctx).Create(file).Error
}

func (r *taskFileRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error) {
	var files []*model.TaskFile
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (r *taskFileRepository) BatchCreate(ctx context.Context, files []*model.TaskFile) error {
	if len(files) == 0 {
		return nil
	}
	for _, f := range files {
		if f.ID == "" {
			f.ID = uuid.New().String()
		}
	}
	return r.db.WithContext(ctx).Create(files).Error
}

func (r *taskFileRepository) FindByID(ctx context.Context, id string) (*model.TaskFile, error) {
	var file model.TaskFile
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&file).Error; err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *taskFileRepository) FindByTaskIDAndRole(ctx context.Context, taskID, role string) ([]*model.TaskFile, error) {
	var files []*model.TaskFile
	if err := r.db.WithContext(ctx).Where("task_id = ? AND role = ?", taskID, role).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}
