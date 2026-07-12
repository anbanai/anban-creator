package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
)

type taskExecutionRepository struct {
	db *gorm.DB
}

func newTaskExecutionRepository(db *gorm.DB) TaskExecutionRepository {
	return &taskExecutionRepository{db: db}
}

func (r *taskExecutionRepository) Create(ctx context.Context, execution *model.TaskExecution) error {
	return r.db.WithContext(ctx).Create(execution).Error
}

func (r *taskExecutionRepository) NextAttempt(ctx context.Context, taskID string) (int, error) {
	var next int
	err := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Select("COALESCE(MAX(attempt), 0) + 1").
		Where("task_id = ?", taskID).
		Scan(&next).Error
	return next, err
}

func (r *taskExecutionRepository) ClaimDispatch(
	ctx context.Context,
	id, token string,
	leaseDuration time.Duration,
) (bool, error) {
	databaseNow, err := sampleDatabaseTime(ctx, r.db)
	if err != nil {
		return false, err
	}
	staleBefore := databaseNow.Add(-leaseDuration)
	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ?", id).
		Where("status = ? OR (status = ? AND (dispatch_claimed_at IS NULL OR dispatch_claimed_at <= ?))",
			model.TaskExecutionCreated, model.TaskExecutionDispatching, staleBefore).
		Updates(map[string]any{
			"status":               model.TaskExecutionDispatching,
			"dispatch_claim_token": token,
			"dispatch_claimed_at":  databaseNow,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func sampleDatabaseTime(ctx context.Context, db *gorm.DB) (time.Time, error) {
	var raw any
	if err := db.WithContext(ctx).Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&raw); err != nil {
		return time.Time{}, fmt.Errorf("sample database time: %w", err)
	}
	switch value := raw.(type) {
	case time.Time:
		return value, nil
	case string:
		return parseDatabaseTime(db, value)
	case []byte:
		return parseDatabaseTime(db, string(value))
	default:
		return time.Time{}, fmt.Errorf("sample database time: unsupported value %T", raw)
	}
}

func parseDatabaseTime(db *gorm.DB, value string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		location := time.Local
		if db.Dialector.Name() == "sqlite" {
			location = time.UTC
		}
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("sample database time: unsupported timestamp %q", value)
}

func (r *taskExecutionRepository) CompleteDispatch(ctx context.Context, id, token string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status = ? AND dispatch_claim_token = ?", id, model.TaskExecutionDispatching, token).
		Updates(map[string]any{
			"status":               model.TaskExecutionStarting,
			"dispatch_claim_token": "",
			"dispatch_claimed_at":  nil,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *taskExecutionRepository) FailDispatch(
	ctx context.Context,
	id, token, reason string,
	diagnostics []byte,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status = ? AND dispatch_claim_token = ?", id, model.TaskExecutionDispatching, token).
		Updates(map[string]any{
			"status":               model.TaskExecutionFailed,
			"dispatch_claim_token": "",
			"dispatch_claimed_at":  nil,
			"terminal_reason":      reason,
			"diagnostics":          diagnostics,
			"completed_at":         time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *taskExecutionRepository) FindByID(ctx context.Context, id string) (*model.TaskExecution, error) {
	var execution model.TaskExecution
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&execution).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

func (r *taskExecutionRepository) FindCurrentByTaskID(ctx context.Context, taskID string) (*model.TaskExecution, error) {
	var execution model.TaskExecution
	err := r.db.WithContext(ctx).
		Select("task_executions.*").
		Joins("JOIN tasks ON tasks.current_execution_id = task_executions.id").
		Where("tasks.id = ?", taskID).
		First(&execution).Error
	if err != nil {
		return nil, err
	}
	return &execution, nil
}

func (r *taskExecutionRepository) FindReconcilable(ctx context.Context, before time.Time, limit int) ([]*model.TaskExecution, error) {
	var executions []*model.TaskExecution
	err := r.db.WithContext(ctx).
		Where("status IN ? AND updated_at <= ?", []string{
			model.TaskExecutionCreated,
			model.TaskExecutionDispatching,
			model.TaskExecutionStarting,
			model.TaskExecutionRunning,
		}, before).
		Order("updated_at ASC").
		Limit(limit).
		Find(&executions).Error
	return executions, err
}

func (r *taskExecutionRepository) SetRuntimeIdentity(ctx context.Context, id, namespace, jobName, podUID string) error {
	updates := make(map[string]any, 3)
	if namespace != "" {
		updates["namespace"] = namespace
	}
	if jobName != "" {
		updates["job_name"] = jobName
	}
	if podUID != "" {
		updates["pod_uid"] = podUID
	}
	if len(updates) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *taskExecutionRepository) UpdateHeartbeat(ctx context.Context, id string, now time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ?", id).
		Update("last_heartbeat_at", now).Error
}

func (r *taskExecutionRepository) Transition(
	ctx context.Context,
	id string,
	from []string,
	to string,
	change model.ExecutionTransition,
) (bool, error) {
	now := time.Now()
	updates := map[string]any{"status": to}
	if change.Started {
		updates["started"] = true
		updates["started_at"] = now
	}
	if change.PodUID != "" {
		updates["pod_uid"] = change.PodUID
	}
	if change.ManifestStatus != "" {
		updates["manifest_status"] = change.ManifestStatus
	}
	if change.TerminalReason != "" {
		updates["terminal_reason"] = change.TerminalReason
	}
	if len(change.Diagnostics) > 0 {
		updates["diagnostics"] = change.Diagnostics
	}
	if isTerminalTaskExecutionStatus(to) {
		updates["completed_at"] = now
	}

	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status IN ?", id, from).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func isTerminalTaskExecutionStatus(status string) bool {
	switch status {
	case model.TaskExecutionSucceeded,
		model.TaskExecutionFailed,
		model.TaskExecutionCancelled,
		model.TaskExecutionTimedOut:
		return true
	default:
		return false
	}
}
