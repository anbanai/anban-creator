package repository

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNoPendingExecutionArtifacts = errors.New("no pending execution artifacts")
	ErrTaskFileExecutionNotCurrent = errors.New("task file execution is not current")
	ErrTaskFileTaskNotRunning      = errors.New("task is not running for artifact publication")
)

type taskFileRepository struct {
	db *gorm.DB
}

func newTaskFileRepository(db *gorm.DB) TaskFileRepository {
	return &taskFileRepository{db: db}
}

func (r *taskFileRepository) Create(ctx context.Context, file *model.TaskFile) error {
	if err := validateTaskFileMutation(file); err != nil {
		return err
	}
	if file.ID == "" {
		file.ID = uuid.New().String()
	}
	return r.db.WithContext(ctx).Create(file).Error
}

// Upsert inserts a task file or updates the existing record on
// (task_id, execution_id, file_path) conflict.
// The original ID is preserved when a conflict occurs.
func (r *taskFileRepository) Upsert(ctx context.Context, file *model.TaskFile) (*model.TaskFile, error) {
	if err := validateTaskFileMutation(file); err != nil {
		return nil, err
	}
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

// PublishCurrentExecution is the guarded Task 8 publication contract. It locks
// the task row and validates the current running attempt in the same transaction
// that swaps the published artifact set.
func (r *taskFileRepository) PublishCurrentExecution(ctx context.Context, taskID, executionID string) error {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(executionID) == "" {
		return ErrTaskFileExecutionNotCurrent
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		if task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
			return ErrTaskFileExecutionNotCurrent
		}
		if task.Status != model.TaskStatusRunning {
			return ErrTaskFileTaskNotRunning
		}
		var pending, published int64
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).Count(&pending).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePublished).Count(&published).Error; err != nil {
			return err
		}
		if pending == 0 {
			if published > 0 {
				return nil
			}
			return ErrNoPendingExecutionArtifacts
		}
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id <> ? AND state = ?", taskID, executionID, model.TaskFileStatePublished).Update("state", model.TaskFileStateSuperseded).Error; err != nil {
			return err
		}
		return tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).Update("state", model.TaskFileStatePublished).Error
	})
}

// DiscardExecution retains audit metadata while making pending rows permanently invisible.
func (r *taskFileRepository) DiscardExecution(ctx context.Context, executionID string) error {
	if strings.TrimSpace(executionID) == "" {
		return fmt.Errorf("execution_id is required")
	}
	return r.db.WithContext(ctx).Model(&model.TaskFile{}).Where("execution_id = ? AND state = ?", executionID, model.TaskFileStatePending).Update("state", model.TaskFileStateSuperseded).Error
}

// ReplacePendingExecution atomically replaces only one attempt's unpublished manifest.
func (r *taskFileRepository) ReplacePendingExecution(ctx context.Context, taskID, executionID string, files []*model.TaskFile) error {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(executionID) == "" {
		return fmt.Errorf("task_id and execution_id are required")
	}
	for _, file := range files {
		if file == nil {
			return fmt.Errorf("task file is required")
		}
		file.TaskID, file.ExecutionID, file.State = taskID, executionID, model.TaskFileStatePending
		if err := validateTaskFileMutation(file); err != nil {
			return err
		}
		if err := validateTaskFileRelativePath(file.FilePath); err != nil {
			return err
		}
	}
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
		if err := validateTaskFileMutation(f); err != nil {
			return err
		}
		if f.ID == "" {
			f.ID = uuid.New().String()
		}
	}
	return r.db.WithContext(ctx).Create(files).Error
}

func validateTaskFileMutation(file *model.TaskFile) error {
	if file == nil {
		return fmt.Errorf("task file is required")
	}
	if strings.TrimSpace(file.TaskID) == "" {
		return fmt.Errorf("task file task_id is required")
	}
	if file.State == "" {
		file.State = model.TaskFileStatePublished
	}
	switch file.State {
	case model.TaskFileStatePending, model.TaskFileStatePublished, model.TaskFileStateSuperseded:
	default:
		return fmt.Errorf("invalid task file state %q", file.State)
	}
	if strings.TrimSpace(file.FilePath) == "" {
		return fmt.Errorf("invalid task file path %q", file.FilePath)
	}
	return nil
}

func validateTaskFileRelativePath(filePath string) error {
	rawPath := strings.TrimSpace(strings.ReplaceAll(filePath, "\\", "/"))
	cleaned := path.Clean(rawPath)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || cleaned != rawPath || rawPath != filePath {
		return fmt.Errorf("invalid task file path %q", filePath)
	}
	return nil
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
