package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	result, err := buildDispatchClaimUpdate(r.db.WithContext(ctx), id, token, leaseDuration)
	if err != nil {
		return false, err
	}
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func buildDispatchClaimUpdate(db *gorm.DB, id, token string, leaseDuration time.Duration) (*gorm.DB, error) {
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("dispatch lease duration must be positive")
	}
	var now clause.Expr
	var stalePredicate string
	var staleArg int64
	switch db.Dialector.Name() {
	case "mysql":
		now = gorm.Expr("CURRENT_TIMESTAMP(6)")
		stalePredicate = "dispatch_claimed_at <= DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL ? MICROSECOND)"
		staleArg = leaseDuration.Microseconds()
	case "sqlite":
		now = gorm.Expr("STRFTIME('%Y-%m-%d %H:%M:%f', 'now')")
		stalePredicate = "JULIANDAY(dispatch_claimed_at) <= JULIANDAY('now') - (? / 86400000.0)"
		staleArg = leaseDuration.Milliseconds()
		if staleArg == 0 {
			staleArg = 1
		}
	default:
		return nil, fmt.Errorf("dispatch leases are unsupported for database dialect %q", db.Dialector.Name())
	}
	return db.Model(&model.TaskExecution{}).
		Where("id = ?", id).
		Where("status = ? OR (status = ? AND (dispatch_claimed_at IS NULL OR "+stalePredicate+"))",
			model.TaskExecutionCreated, model.TaskExecutionDispatching, staleArg).
		Updates(map[string]any{
			"status":               model.TaskExecutionDispatching,
			"dispatch_claim_token": token,
			"dispatch_claimed_at":  now,
			"updated_at":           now,
		}), nil
}

func (r *taskExecutionRepository) AbandonDispatch(ctx context.Context, id, token string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status = ? AND dispatch_claim_token = ?", id, model.TaskExecutionDispatching, token).
		Updates(map[string]any{
			"status":               model.TaskExecutionCreated,
			"dispatch_claim_token": "",
			"dispatch_claimed_at":  nil,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *taskExecutionRepository) CompleteDispatch(ctx context.Context, id, token string, identity model.RuntimeIdentity) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status = ? AND dispatch_claim_token = ?", id, model.TaskExecutionDispatching, token).
		Updates(map[string]any{
			"status":               model.TaskExecutionStarting,
			"dispatch_claim_token": "",
			"dispatch_claimed_at":  nil,
			"runtime_scope":        identity.Scope,
			"runtime_workload":     identity.Workload,
			"runtime_instance_id":  identity.InstanceID,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *taskExecutionRepository) FailDispatch(
	ctx context.Context,
	id, token, reason string,
	diagnostics, executionResult []byte,
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
			"result":               executionResult,
			"finalization_status":  model.TaskExecutionFinalizationTerminal,
			"cleanup_status":       model.TaskExecutionCleanupPending,
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
		Where("(status IN ? AND updated_at <= ?) OR (status IN ? AND ((finalization_status <> '' AND finalization_status <> ?) OR cleanup_status = ?))", []string{
			model.TaskExecutionCreated,
			model.TaskExecutionDispatching,
			model.TaskExecutionStarting,
			model.TaskExecutionRunning,
		}, before, []string{model.TaskExecutionSucceeded, model.TaskExecutionFailed, model.TaskExecutionCancelled, model.TaskExecutionTimedOut}, model.TaskExecutionFinalizationDone, model.TaskExecutionCleanupPending).
		Order("updated_at ASC").
		Limit(limit).
		Find(&executions).Error
	return executions, err
}

func (r *taskExecutionRepository) SetRuntimeIdentity(ctx context.Context, id string, identity model.RuntimeIdentity) error {
	updates := make(map[string]any, 3)
	if identity.Scope != "" {
		updates["runtime_scope"] = identity.Scope
	}
	if identity.Workload != "" {
		updates["runtime_workload"] = identity.Workload
	}
	if identity.InstanceID != "" {
		updates["runtime_instance_id"] = identity.InstanceID
	}
	if len(updates) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status IN ?", id, []string{model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning}).
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
	if change.RuntimeInstanceID != "" {
		updates["runtime_instance_id"] = change.RuntimeInstanceID
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
	if len(change.Result) > 0 {
		updates["result"] = change.Result
	}
	if change.FinalizationStatus != "" {
		updates["finalization_status"] = change.FinalizationStatus
	}
	if change.CleanupStatus != "" {
		updates["cleanup_status"] = change.CleanupStatus
	}
	if isTerminalTaskExecutionStatus(to) {
		updates["completed_at"] = now
		if change.CleanupStatus == "" {
			updates["cleanup_status"] = model.TaskExecutionCleanupPending
		}
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

func (r *taskExecutionRepository) ClaimFinalization(ctx context.Context, id, token string, lease time.Duration) (bool, error) {
	now, stalePredicate, staleArg, _, err := databaseLeaseClock(r.db, "finalization_at", lease)
	if err != nil {
		return false, fmt.Errorf("finalization lease: %w", err)
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND finalization_status <> ?", id, model.TaskExecutionFinalizationDone).
		Where("finalization_token = '' OR finalization_at IS NULL OR "+stalePredicate, staleArg).
		Updates(map[string]any{"finalization_token": token, "finalization_at": now})
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) AdvanceFinalization(ctx context.Context, id, token, from, to string) (bool, error) {
	now, err := databaseNow(r.db)
	if err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND finalization_token = ? AND finalization_status = ?", id, token, from).
		Updates(map[string]any{"finalization_status": to, "finalization_at": now})
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) RenewFinalizationClaim(ctx context.Context, id, token string) (bool, error) {
	now, err := databaseNow(r.db)
	if err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND finalization_token = ? AND finalization_status <> ?", id, token, model.TaskExecutionFinalizationDone).
		Update("finalization_at", now)
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) ReleaseFinalization(ctx context.Context, id, token string) error {
	return r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND finalization_token = ?", id, token).
		Updates(map[string]any{"finalization_token": "", "finalization_at": nil}).Error
}

func (r *taskExecutionRepository) TransitionPublishing(ctx context.Context, id, from, to string, publishResult []byte) (bool, error) {
	updates := map[string]any{"publishing_status": to}
	if len(publishResult) > 0 {
		updates["publishing_result"] = publishResult
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND publishing_status = ?", id, from).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) ClaimCleanup(ctx context.Context, id, token string, lease time.Duration) (bool, error) {
	now, stalePredicate, staleArg, duePredicate, err := databaseLeaseClock(r.db, "cleanup_at", lease)
	if err != nil {
		return false, fmt.Errorf("cleanup lease: %w", err)
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND cleanup_status = ?", id, model.TaskExecutionCleanupPending).
		Where("cleanup_next_at IS NULL OR "+duePredicate).
		Where("cleanup_token = '' OR cleanup_at IS NULL OR "+stalePredicate, staleArg).
		Updates(map[string]any{"cleanup_token": token, "cleanup_at": now})
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) CompleteCleanup(ctx context.Context, id, token string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND cleanup_status = ? AND cleanup_token = ?", id, model.TaskExecutionCleanupPending, token).
		Updates(map[string]any{"cleanup_status": model.TaskExecutionCleanupDone, "cleanup_token": "", "cleanup_at": nil, "cleanup_next_at": nil})
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) FailCleanup(ctx context.Context, id, token string, backoff time.Duration) (bool, error) {
	next, err := databaseFuture(r.db, backoff)
	if err != nil {
		return false, fmt.Errorf("cleanup retry backoff: %w", err)
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND cleanup_status = ? AND cleanup_token = ?", id, model.TaskExecutionCleanupPending, token).
		Updates(map[string]any{
			"cleanup_token":    "",
			"cleanup_at":       nil,
			"cleanup_next_at":  next,
			"cleanup_attempts": gorm.Expr("cleanup_attempts + 1"),
		})
	return result.RowsAffected == 1, result.Error
}

func databaseNow(db *gorm.DB) (clause.Expr, error) {
	switch db.Dialector.Name() {
	case "mysql":
		return gorm.Expr("CURRENT_TIMESTAMP(6)"), nil
	case "sqlite":
		return gorm.Expr("STRFTIME('%Y-%m-%d %H:%M:%f', 'now')"), nil
	default:
		return clause.Expr{}, fmt.Errorf("leases are unsupported for database dialect %q", db.Dialector.Name())
	}
}

func databaseLeaseClock(db *gorm.DB, column string, lease time.Duration) (clause.Expr, string, int64, string, error) {
	if lease <= 0 {
		return clause.Expr{}, "", 0, "", fmt.Errorf("lease duration must be positive")
	}
	now, err := databaseNow(db)
	if err != nil {
		return clause.Expr{}, "", 0, "", err
	}
	switch db.Dialector.Name() {
	case "mysql":
		return now, column + " <= DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL ? MICROSECOND)", lease.Microseconds(), "cleanup_next_at <= CURRENT_TIMESTAMP(6)", nil
	case "sqlite":
		milliseconds := lease.Milliseconds()
		if milliseconds == 0 {
			milliseconds = 1
		}
		return now, "JULIANDAY(" + column + ") <= JULIANDAY('now') - (? / 86400000.0)", milliseconds, "JULIANDAY(cleanup_next_at) <= JULIANDAY('now')", nil
	default:
		return clause.Expr{}, "", 0, "", fmt.Errorf("leases are unsupported for database dialect %q", db.Dialector.Name())
	}
}

func databaseFuture(db *gorm.DB, delay time.Duration) (clause.Expr, error) {
	if delay <= 0 {
		return clause.Expr{}, fmt.Errorf("retry delay must be positive")
	}
	switch db.Dialector.Name() {
	case "mysql":
		return gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL ? MICROSECOND)", delay.Microseconds()), nil
	case "sqlite":
		return gorm.Expr("STRFTIME('%Y-%m-%d %H:%M:%f', 'now', ?)", fmt.Sprintf("+%.6f seconds", delay.Seconds())), nil
	default:
		return clause.Expr{}, fmt.Errorf("leases are unsupported for database dialect %q", db.Dialector.Name())
	}
}

func (r *taskExecutionRepository) ReleaseCleanup(ctx context.Context, id, token string) error {
	return r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND cleanup_status = ? AND cleanup_token = ?", id, model.TaskExecutionCleanupPending, token).
		Updates(map[string]any{"cleanup_token": "", "cleanup_at": nil}).Error
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
