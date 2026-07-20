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
	ErrTaskFileManifestState       = errors.New("task file manifest state conflict")
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

func (r *taskFileRepository) FindCollectedByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error) {
	var files []*model.TaskFile
	if err := r.db.WithContext(ctx).Where("task_id = ? AND state = ?", taskID, model.TaskFileStateCollected).Find(&files).Error; err != nil {
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
		task, execution, err := lockCurrentArtifactExecution(tx, taskID, executionID)
		if err != nil {
			return err
		}
		if execution.ManifestStatus == model.TaskExecutionManifestPublished {
			return nil
		}
		if err := requirePublishableArtifactExecution(task, execution); err != nil {
			return err
		}
		if execution.ManifestStatus == model.TaskExecutionManifestDiscarded {
			return ErrTaskFileManifestState
		}
		if execution.ManifestStatus != model.TaskExecutionManifestPending {
			return ErrNoPendingExecutionArtifacts
		}
		var pending int64
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).Count(&pending).Error; err != nil {
			return err
		}
		if pending == 0 {
			return ErrNoPendingExecutionArtifacts
		}
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id <> ? AND state = ?", taskID, executionID, model.TaskFileStatePublished).Update("state", model.TaskFileStateSuperseded).Error; err != nil {
			return err
		}
		result := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).Update("state", model.TaskFileStatePublished)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != pending {
			return fmt.Errorf("%w: published %d of %d rows", ErrTaskFileManifestState, result.RowsAffected, pending)
		}
		statusResult := tx.Model(&model.TaskExecution{}).Where("id = ? AND manifest_status = ?", executionID, model.TaskExecutionManifestPending).Update("manifest_status", model.TaskExecutionManifestPublished)
		if statusResult.Error != nil {
			return statusResult.Error
		}
		if statusResult.RowsAffected != 1 {
			return ErrTaskFileManifestState
		}
		return nil
	})
}

// CollectCurrentExecution retains a terminal failed attempt's artifacts for
// diagnosis without replacing the successful published set.
func (r *taskFileRepository) CollectCurrentExecution(ctx context.Context, taskID, executionID string) error {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(executionID) == "" {
		return ErrTaskFileExecutionNotCurrent
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, execution, err := lockCurrentArtifactExecution(tx, taskID, executionID)
		if err != nil {
			return err
		}
		if execution.ManifestStatus == model.TaskExecutionManifestCollected {
			return nil
		}
		if !isCollectableArtifactExecution(execution) {
			return ErrTaskFileTaskNotRunning
		}
		if execution.ManifestStatus != "" && execution.ManifestStatus != model.TaskExecutionManifestPending {
			return ErrTaskFileManifestState
		}
		if err := tx.Model(&model.TaskFile{}).
			Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).
			Update("state", model.TaskFileStateCollected).Error; err != nil {
			return err
		}
		statusResult := tx.Model(&model.TaskExecution{}).
			Where("id = ? AND manifest_status = ?", executionID, execution.ManifestStatus).
			Update("manifest_status", model.TaskExecutionManifestCollected)
		if statusResult.Error != nil {
			return statusResult.Error
		}
		if statusResult.RowsAffected != 1 {
			return ErrTaskFileManifestState
		}
		return nil
	})
}

// DiscardCurrentExecution retains audit metadata while making pending rows permanently invisible.
func (r *taskFileRepository) DiscardCurrentExecution(ctx context.Context, taskID, executionID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, execution, err := lockCurrentArtifactExecution(tx, taskID, executionID)
		if err != nil {
			return err
		}
		if execution.ManifestStatus == model.TaskExecutionManifestPublished {
			return ErrTaskFileManifestState
		}
		if execution.ManifestStatus == model.TaskExecutionManifestDiscarded {
			return nil
		}
		if err := requireDiscardableArtifactExecution(task, execution); err != nil {
			return err
		}
		if execution.ManifestStatus != "" && execution.ManifestStatus != model.TaskExecutionManifestPending {
			return ErrTaskFileManifestState
		}
		if err := tx.Model(&model.TaskFile{}).Where("task_id = ? AND execution_id = ? AND state = ?", taskID, executionID, model.TaskFileStatePending).Update("state", model.TaskFileStateSuperseded).Error; err != nil {
			return err
		}
		statusResult := tx.Model(&model.TaskExecution{}).Where("id = ? AND manifest_status = ?", executionID, execution.ManifestStatus).Update("manifest_status", model.TaskExecutionManifestDiscarded)
		if statusResult.Error != nil {
			return statusResult.Error
		}
		if statusResult.RowsAffected != 1 {
			return ErrTaskFileManifestState
		}
		return nil
	})
}

// UpsertPendingCurrentExecution registers one server-produced artifact without
// replacing the Job's pending workspace manifest. The task and execution rows
// are locked in the same order as the other artifact mutations.
func (r *taskFileRepository) UpsertPendingCurrentExecution(ctx context.Context, taskID, executionID string, file *model.TaskFile) (*model.TaskFile, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(executionID) == "" || file == nil {
		return nil, fmt.Errorf("task_id, execution_id, and task file are required")
	}
	file.TaskID, file.ExecutionID, file.State = taskID, executionID, model.TaskFileStatePending
	if err := validateTaskFileMutation(file); err != nil {
		return nil, err
	}
	if err := validateTaskFileRelativePath(file.FilePath); err != nil {
		return nil, err
	}

	var persisted model.TaskFile
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, execution, err := lockCurrentArtifactExecution(tx, taskID, executionID)
		if err != nil {
			return err
		}
		if execution.ManifestStatus != "" && execution.ManifestStatus != model.TaskExecutionManifestPending {
			return ErrTaskFileManifestState
		}
		if err := requireRunningArtifactExecution(task, execution); err != nil {
			return err
		}
		if file.ID == "" {
			file.ID = uuid.NewString()
		}
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "task_id"}, {Name: "execution_id"}, {Name: "file_path"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"state", "file_name", "mime_type", "file_size", "oss_key", "oss_url",
				"storage_provider", "role", "content_hash", "media_id", "wechat_url",
			}),
		}).Create(file)
		if result.Error != nil {
			return result.Error
		}
		if execution.ManifestStatus == "" {
			statusResult := tx.Model(&model.TaskExecution{}).
				Where("id = ? AND manifest_status = ''", executionID).
				Update("manifest_status", model.TaskExecutionManifestPending)
			if statusResult.Error != nil {
				return statusResult.Error
			}
			if statusResult.RowsAffected != 1 {
				return ErrTaskFileManifestState
			}
		}
		return tx.Where("task_id = ? AND execution_id = ? AND file_path = ?", taskID, executionID, file.FilePath).
			First(&persisted).Error
	})
	if err != nil {
		return nil, err
	}
	return &persisted, nil
}

// ReplacePendingCurrentExecution atomically replaces only the current running attempt's unpublished manifest.
func (r *taskFileRepository) ReplacePendingCurrentExecution(ctx context.Context, taskID, executionID string, files []*model.TaskFile) error {
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
		task, execution, err := lockCurrentArtifactExecution(tx, taskID, executionID)
		if err != nil {
			return err
		}
		if execution.ManifestStatus != "" && execution.ManifestStatus != model.TaskExecutionManifestPending {
			return ErrTaskFileManifestState
		}
		if err := requireRunningArtifactExecution(task, execution); err != nil {
			return err
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
		statusResult := tx.Model(&model.TaskExecution{}).Where("id = ? AND manifest_status = ?", executionID, execution.ManifestStatus).Update("manifest_status", model.TaskExecutionManifestPending)
		if statusResult.Error != nil {
			return statusResult.Error
		}
		if statusResult.RowsAffected != 1 {
			return ErrTaskFileManifestState
		}
		return nil
	})
}

func lockCurrentArtifactExecution(tx *gorm.DB, taskID, executionID string) (*model.Task, *model.TaskExecution, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(executionID) == "" {
		return nil, nil, ErrTaskFileExecutionNotCurrent
	}
	var task model.Task
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, nil, err
	}
	if task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
		return nil, nil, ErrTaskFileExecutionNotCurrent
	}
	var execution model.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ?", executionID, taskID).First(&execution).Error; err != nil {
		return nil, nil, err
	}
	return &task, &execution, nil
}

func requireRunningArtifactExecution(task *model.Task, execution *model.TaskExecution) error {
	if task.Status != model.TaskStatusRunning || execution.Status != model.TaskExecutionRunning {
		return ErrTaskFileTaskNotRunning
	}
	return nil
}

func requirePublishableArtifactExecution(task *model.Task, execution *model.TaskExecution) error {
	if task.Status == model.TaskStatusRunning &&
		(execution.Status == model.TaskExecutionRunning || execution.Status == model.TaskExecutionSucceeded) {
		return nil
	}
	return ErrTaskFileTaskNotRunning
}

func requireDiscardableArtifactExecution(task *model.Task, execution *model.TaskExecution) error {
	if task.Status == model.TaskStatusRunning {
		switch execution.Status {
		case model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning:
			return nil
		}
	}
	if execution.Status == model.TaskExecutionFailed || execution.Status == model.TaskExecutionCancelled || execution.Status == model.TaskExecutionTimedOut {
		if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
			return nil
		}
	}
	return ErrTaskFileTaskNotRunning
}

func isCollectableArtifactExecution(execution *model.TaskExecution) bool {
	switch execution.Status {
	case model.TaskExecutionFailed, model.TaskExecutionCancelled, model.TaskExecutionTimedOut:
		return true
	default:
		return false
	}
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
	case model.TaskFileStatePending, model.TaskFileStatePublished, model.TaskFileStateCollected, model.TaskFileStateSuperseded:
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
	if err := r.db.WithContext(ctx).Where("id = ? AND state IN ?", id, []string{model.TaskFileStatePublished, model.TaskFileStateCollected}).First(&file).Error; err != nil {
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
	if err := r.db.WithContext(ctx).Model(&model.TaskFile{}).Where("id = ? AND task_id = ? AND state IN ?", fileID, taskID, []string{model.TaskFileStatePublished, model.TaskFileStateCollected}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
