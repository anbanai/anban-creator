package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// Upsert inserts a task file or updates the existing record on (task_id, file_path) conflict.
// The original ID is preserved when a conflict occurs.
func (r *taskFileRepository) Upsert(ctx context.Context, file *model.TaskFile) (*model.TaskFile, error) {
	if file.ID == "" {
		file.ID = uuid.New().String()
	}

	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}, {Name: "file_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"file_name", "mime_type", "file_size", "oss_key", "oss_url",
			"storage_provider", "role", "content_hash",
		}),
	}).Create(file)

	if result.Error != nil {
		return nil, result.Error
	}

	persisted, err := r.FindExisting(ctx, file.TaskID, file.FilePath)
	if err != nil {
		return nil, err
	}
	if persisted != nil {
		return persisted, nil
	}
	return file, nil
}

// FindExisting returns an existing task file record matching (taskID, filePath), or nil if none exists.
func (r *taskFileRepository) FindExisting(ctx context.Context, taskID, filePath string) (*model.TaskFile, error) {
	var file model.TaskFile
	if err := r.db.WithContext(ctx).Where("task_id = ? AND file_path = ?", taskID, filePath).First(&file).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
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

// FindByTaskIDAndContentHash returns a task file with matching content hash for the given task, or nil.
func (r *taskFileRepository) FindByTaskIDAndContentHash(ctx context.Context, taskID, contentHash string) (*model.TaskFile, error) {
	var file model.TaskFile
	if err := r.db.WithContext(ctx).
		Where("task_id = ? AND content_hash = ?", taskID, contentHash).
		First(&file).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

func (r *taskFileRepository) DeleteByTaskID(ctx context.Context, taskID string) error {
	return r.db.WithContext(ctx).Where("task_id = ?", taskID).Delete(&model.TaskFile{}).Error
}

func (r *taskFileRepository) ExistsByTaskIDAndID(ctx context.Context, taskID, fileID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.TaskFile{}).Where("id = ? AND task_id = ?", fileID, taskID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
