package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

type taskRepository struct {
	db *gorm.DB
}

func newTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) Create(ctx context.Context, task *model.Task) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *taskRepository) FindByID(ctx context.Context, id string) (*model.Task, error) {
	var task model.Task
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepository) FindByUserID(ctx context.Context, userID string, channelID string, offset, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC")
	if channelID != "" {
		q = q.Where("channel_id = ?", channelID)
	}
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) FindByUserIDAndStatus(ctx context.Context, userID, status string, channelID string, offset, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).Where("user_id = ? AND status = ?", userID, status).Order("created_at DESC")
	if channelID != "" {
		q = q.Where("channel_id = ?", channelID)
	}
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) FindByCreatedAtRange(ctx context.Context, from, to time.Time, offset, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).Where("created_at >= ? AND created_at <= ?", from, to).Order("created_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) FindByUserIDAndCreatedAtRange(ctx context.Context, userID string, from, to time.Time, offset, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).Where("user_id = ? AND created_at >= ? AND created_at <= ?", userID, from, to).Order("created_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) FindRunning(ctx context.Context) ([]*model.Task, error) {
	var tasks []*model.Task
	if err := r.db.WithContext(ctx).Where("status = ?", model.TaskStatusRunning).Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) FindRunningByUser(ctx context.Context, userID string, channelID string) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Where("status = ?", model.TaskStatusRunning)
	if channelID != "" {
		q = q.Where("channel_id = ?", channelID)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// FindCompletedOlderThan finds completed or failed tasks whose completed_at
// is before the given time and which have not yet been cleaned up.
func (r *taskRepository) FindCompletedOlderThan(ctx context.Context, before time.Time) ([]*model.Task, error) {
	var tasks []*model.Task
	err := r.db.WithContext(ctx).
		Where("status IN ?", []string{model.TaskStatusCompleted, model.TaskStatusFailed}).
		Where("completed_at IS NOT NULL AND completed_at < ?", before).
		Where("cleaned_up_at IS NULL").
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("status", status).Error
}

func (r *taskRepository) UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        status,
			"error_message": errorMsg,
		}).Error
}

func (r *taskRepository) UpdateProgressLog(ctx context.Context, id, log string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("progress_log", log).Error
}

// AppendProgressLog atomically appends a message to the progress_log column
// using SQL CONCAT, avoiding read-modify-write races under concurrent callers.
func (r *taskRepository) AppendProgressLog(ctx context.Context, id, message string) error {
	return r.db.WithContext(ctx).
		Exec("UPDATE tasks SET progress_log = CONCAT(COALESCE(progress_log, ''), ?) WHERE id = ?", message+"\n", id).
		Error
}

func (r *taskRepository) UpdateResult(ctx context.Context, id, result string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("result", result).Error
}

// Update saves the full task object.
func (r *taskRepository) Update(ctx context.Context, task *model.Task) error {
	return r.db.WithContext(ctx).Save(task).Error
}

// UpdateCleanedUpAt sets the cleaned_up_at timestamp for a task.
func (r *taskRepository) UpdateCleanedUpAt(ctx context.Context, id string, t time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("cleaned_up_at", t).Error
}

func (r *taskRepository) SetStartedAt(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("started_at", now).Error
}

func (r *taskRepository) SetCompletedAt(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("completed_at", now).Error
}

// validRetryFields restricts which columns IncrementRetryAndSetPending accepts.
var validRetryFields = map[string]bool{
	"retry_count":             true,
	"rate_limit_retry_count":  true,
}

// IncrementRetryAndSetPending atomically increments the given retry counter field
// and sets status to "pending" in a single SQL UPDATE.
// The field parameter must be "retry_count" or "rate_limit_retry_count".
func (r *taskRepository) IncrementRetryAndSetPending(ctx context.Context, taskID string, field string) error {
	if !validRetryFields[field] {
		return fmt.Errorf("invalid retry field: %s", field)
	}
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			field:    gorm.Expr(field + " + 1"),
			"status": model.TaskStatusPending,
		}).Error
}

func (r *taskRepository) UpdateHeartbeat(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("last_heartbeat_at", now).Error
}

func (r *taskRepository) CountByUserID(ctx context.Context, userID string, channelID string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.Task{}).Where("user_id = ?", userID)
	if channelID != "" {
		q = q.Where("channel_id = ?", channelID)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *taskRepository) CountByUserIDAndStatus(ctx context.Context, userID, status string, channelID string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.Task{}).Where("user_id = ? AND status = ?", userID, status)
	if channelID != "" {
		q = q.Where("channel_id = ?", channelID)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *taskRepository) CountRunningByChannel(ctx context.Context, channelID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("channel_id = ? AND status = ?", channelID, model.TaskStatusRunning).
		Count(&count).Error
	return count, err
}

func (r *taskRepository) FindPendingByChannel(ctx context.Context, channelID string, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	err := r.db.WithContext(ctx).
		Where("channel_id = ? AND status = ?", channelID, model.TaskStatusPending).
		Where("NOT (retry_count > 0 AND updated_at > DATE_SUB(NOW(), INTERVAL 2 MINUTE))").
		Order("created_at ASC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// FindTopicsByChannelID returns all topic texts for a channel, ordered by creation time descending.
func (r *taskRepository) FindTopicsByChannelID(ctx context.Context, channelID string) ([]string, error) {
	var topics []string
	err := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("channel_id = ? AND topic != ''", channelID).
		Order("created_at DESC").
		Limit(200).
		Pluck("topic", &topics).Error
	return topics, err
}

// CompareAndSwapStatus atomically transitions task status from expected to newStatus.
// Returns true if the transition succeeded (status was expected and is now newStatus).
func (r *taskRepository) CompareAndSwapStatus(ctx context.Context, taskID, expected, newStatus string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ?", taskID, expected).
		Update("status", newStatus)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// CompareAndSwapStatusAndStartedAt atomically transitions status and sets started_at.
func (r *taskRepository) CompareAndSwapStatusAndStartedAt(ctx context.Context, taskID, expected, newStatus string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ?", taskID, expected).
		Updates(map[string]interface{}{
			"status":     newStatus,
			"started_at": time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// CompareAndSwapStatusAndError atomically transitions status and sets error message.
// Returns true if the transition succeeded (status was expected and is now newStatus).
func (r *taskRepository) CompareAndSwapStatusAndError(ctx context.Context, taskID, expected, newStatus, errorMsg string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ?", taskID, expected).
		Updates(map[string]interface{}{
			"status":        newStatus,
			"error_message": errorMsg,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
