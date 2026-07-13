package repository

import (
	"context"
	"fmt"

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

// Upsert inserts a task file or updates the existing record on
// (task_id, execution_id, file_path) conflict.
// The original ID is preserved when a conflict occurs.
func (r *taskFileRepository) Upsert(ctx context.Context, file *model.TaskFile) (*model.TaskFile, error) {
	if file.ID == "" {
		file.ID = uuid.New().String()
	}

	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}, {Name: "execution_id"}, {Name: "file_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"file_name", "mime_type", "file_size", "oss_key", "oss_url",
			"storage_provider", "role", "content_hash", "media_id", "wechat_url",
		}),
	}).Create(file)

	if result.Error != nil {
		return nil, result.Error
	}

	var persisted model.TaskFile
	err := r.db.WithContext(ctx).Where("task_id = ? AND execution_id = ? AND file_path = ?", file.TaskID, file.ExecutionID, file.FilePath).First(&persisted).Error
	if err != nil {
		return nil, err
	}
	return &persisted, nil
}

// FindExisting returns an existing task file record matching (taskID, filePath), or nil if none exists.
func (r *taskFileRepository) FindExisting(ctx context.Context, taskID, filePath string) (*model.TaskFile, error) {
	var file model.TaskFile
	if err := r.db.WithContext(ctx).Where("task_id = ? AND file_path = ? AND state = ?", taskID, filePath, model.TaskFileStatePublished).First(&file).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &file, nil
}

func (r *taskFileRepository) FindByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error) {
	var files []*model.TaskFile
	if err := r.db.WithContext(ctx).Where("task_id = ? AND state = ?", taskID, model.TaskFileStatePublished).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (r *taskFileRepository) FindByExecutionID(ctx context.Context, executionID string) ([]*model.TaskFile, error) {
	var files []*model.TaskFile
	if err := r.db.WithContext(ctx).Where("execution_id = ?", executionID).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

// PublishExecution atomically replaces a task's published artifact set. The
// caller must validate that executionID is still the task's current execution.
func (r *taskFileRepository) PublishExecution(ctx context.Context, taskID, executionID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id <> ? AND state = ?", taskID, executionID, model.TaskFileStatePublished).Update("state", model.TaskFileStateSuperseded).Error; err != nil {
			return err
		}
		return tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).Update("state", model.TaskFileStatePublished).Error
	})
}

// DiscardExecution retains audit metadata while making pending rows permanently invisible.
func (r *taskFileRepository) DiscardExecution(ctx context.Context, executionID string) error {
	return r.db.WithContext(ctx).Model(&model.TaskFile{}).Where("execution_id = ? AND state = ?", executionID, model.TaskFileStatePending).Update("state", model.TaskFileStateSuperseded).Error
}

// ReplacePendingExecution atomically replaces only one attempt's unpublished manifest.
func (r *taskFileRepository) ReplacePendingExecution(ctx context.Context, taskID, executionID string, files []*model.TaskFile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var published int64
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePublished).Count(&published).Error; err != nil {
			return err
		}
		if published > 0 {
			return fmt.Errorf("execution artifacts are already published")
		}
		if err := tx.Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).Delete(&model.TaskFile{}).Error; err != nil {
			return err
		}
		for _, file := range files {
			if file.ID == "" {
				file.ID = uuid.NewString()
			}
			if err := tx.Create(file).Error; err != nil {
				return err
			}
		}
		return nil
	})
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
	if err := r.db.WithContext(ctx).Where("id = ? AND state = ?", id, model.TaskFileStatePublished).First(&file).Error; err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *taskFileRepository) FindByTaskIDAndRole(ctx context.Context, taskID, role string) ([]*model.TaskFile, error) {
	var files []*model.TaskFile
	if err := r.db.WithContext(ctx).Where("task_id = ? AND role = ? AND state = ?", taskID, role, model.TaskFileStatePublished).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

// FindByTaskIDAndContentHash returns a task file with matching content hash for the given task, or nil.
func (r *taskFileRepository) FindByTaskIDAndContentHash(ctx context.Context, taskID, contentHash string) (*model.TaskFile, error) {
	var file model.TaskFile
	if err := r.db.WithContext(ctx).
		Where("task_id = ? AND content_hash = ? AND state = ?", taskID, contentHash, model.TaskFileStatePublished).
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
	if err := r.db.WithContext(ctx).Model(&model.TaskFile{}).Where("id = ? AND task_id = ? AND state = ?", fileID, taskID, model.TaskFileStatePublished).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
