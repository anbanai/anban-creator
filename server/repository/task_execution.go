package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrRuntimeIdentityConflict reports attempted drift from a persisted non-empty identity member.
	ErrRuntimeIdentityConflict = errors.New("runtime identity conflict")
	// ErrRuntimeIdentityInactive reports identity writes rejected after an execution leaves its active states.
	ErrRuntimeIdentityInactive = errors.New("runtime identity cannot be updated for inactive execution")
)

var activeTaskExecutionStatuses = []string{
	model.TaskExecutionCreated,
	model.TaskExecutionDispatching,
	model.TaskExecutionStarting,
	model.TaskExecutionRunning,
}

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
	if err := validateLeaseToken("dispatch", token); err != nil {
		return false, err
	}
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

func (r *taskExecutionRepository) RefreshDispatchClaim(ctx context.Context, id, token string) (bool, error) {
	if err := validateLeaseToken("dispatch", token); err != nil {
		return false, err
	}
	now, err := databaseNow(r.db)
	if err != nil {
		return false, fmt.Errorf("refresh dispatch claim: %w", err)
	}
	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND dispatch_claim_token = ?", id, token).
		Update("dispatch_claimed_at", now)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	current, err := r.findRuntimeIdentityState(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current.DispatchClaimToken == token, nil
}

func (r *taskExecutionRepository) DispatchClaimActive(ctx context.Context, id string, leaseDuration time.Duration) (bool, error) {
	_, stalePredicate, staleArg, _, err := databaseLeaseClock(r.db, "dispatch_claimed_at", leaseDuration)
	if err != nil {
		return false, fmt.Errorf("dispatch barrier: %w", err)
	}
	var count int64
	err = r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND dispatch_claim_token <> ''", id).
		Where("dispatch_claimed_at IS NULL OR NOT ("+stalePredicate+")", staleArg).
		Count(&count).Error
	return count == 1, err
}

func (r *taskExecutionRepository) AbandonDispatch(ctx context.Context, id, token string) (bool, error) {
	if err := validateLeaseToken("dispatch", token); err != nil {
		return false, err
	}
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
	if err := validateLeaseToken("dispatch", token); err != nil {
		return false, err
	}
	updates := map[string]any{
		"status":               model.TaskExecutionStarting,
		"dispatch_claim_token": "",
		"dispatch_claimed_at":  nil,
	}
	query := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status = ? AND dispatch_claim_token = ?", id, model.TaskExecutionDispatching, token)
	query = addRuntimeIdentityUpdate(query, updates, identity)
	result := query.Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}

	current, err := r.findRuntimeIdentityState(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current.Status != model.TaskExecutionDispatching || current.DispatchClaimToken != token {
		return false, nil
	}
	if err := runtimeIdentityConflict(current, identity); err != nil {
		return false, err
	}
	return false, nil
}

func (r *taskExecutionRepository) FailDispatch(
	ctx context.Context,
	id, token, reason string,
	diagnostics, executionResult []byte,
) (bool, error) {
	if err := validateLeaseToken("dispatch", token); err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status = ? AND dispatch_claim_token = ?", id, model.TaskExecutionDispatching, token).
		Updates(map[string]any{
			"status":              model.TaskExecutionFailed,
			"terminal_reason":     reason,
			"diagnostics":         diagnostics,
			"result":              executionResult,
			"finalization_status": model.TaskExecutionFinalizationTerminal,
			"cleanup_status":      model.TaskExecutionCleanupPending,
			"completed_at":        time.Now(),
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

func (r *taskExecutionRepository) FindByIDForUpdate(ctx context.Context, id string) (*model.TaskExecution, error) {
	var execution model.TaskExecution
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&execution).Error; err != nil {
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
	if identity.Scope == "" && identity.Workload == "" && identity.InstanceID == "" {
		return nil
	}

	updates := make(map[string]any, 3)
	query := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND status IN ?", id, activeTaskExecutionStatuses)
	query = addRuntimeIdentityUpdate(query, updates, identity)
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	current, err := r.findRuntimeIdentityState(ctx, id)
	if err != nil {
		return err
	}
	if !isActiveTaskExecutionStatus(current.Status) {
		return fmt.Errorf("%w: execution %s has status %s", ErrRuntimeIdentityInactive, id, current.Status)
	}
	return runtimeIdentityConflict(current, identity)
}

func (r *taskExecutionRepository) SetCleanupRuntimeIdentity(ctx context.Context, id, token string, identity model.RuntimeIdentity) (bool, error) {
	if err := validateLeaseToken("cleanup", token); err != nil {
		return false, err
	}
	if identity.Scope == "" || identity.Workload == "" || identity.InstanceID == "" {
		return false, fmt.Errorf("complete recovered runtime identity is required")
	}

	updates := make(map[string]any, 3)
	query := r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ? AND cleanup_status = ? AND cleanup_token = ?", id, model.TaskExecutionCleanupPending, token)
	query = addRuntimeIdentityUpdate(query, updates, identity)
	result := query.Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}

	current, err := r.findCleanupRuntimeIdentityState(ctx, id)
	if err != nil {
		return false, err
	}
	if current.CleanupStatus != model.TaskExecutionCleanupPending || current.CleanupToken != token {
		return false, nil
	}
	if err := runtimeIdentityConflict(current, identity); err != nil {
		return false, err
	}
	return true, nil
}

func addRuntimeIdentityUpdate(query *gorm.DB, updates map[string]any, identity model.RuntimeIdentity) *gorm.DB {
	for _, member := range []struct {
		column string
		value  string
	}{
		{column: "runtime_scope", value: identity.Scope},
		{column: "runtime_workload", value: identity.Workload},
		{column: "runtime_instance_id", value: identity.InstanceID},
	} {
		if member.value == "" {
			continue
		}
		updates[member.column] = member.value
		query = query.Where("("+member.column+" IS NULL OR "+member.column+" = '' OR "+member.column+" = ?)", member.value)
	}
	return query
}

func (r *taskExecutionRepository) findRuntimeIdentityState(ctx context.Context, id string) (*model.TaskExecution, error) {
	var execution model.TaskExecution
	err := r.db.WithContext(ctx).
		Select("id", "status", "dispatch_claim_token", "runtime_scope", "runtime_workload", "runtime_instance_id").
		Where("id = ?", id).
		First(&execution).Error
	if err != nil {
		return nil, err
	}
	return &execution, nil
}

func (r *taskExecutionRepository) findCleanupRuntimeIdentityState(ctx context.Context, id string) (*model.TaskExecution, error) {
	var execution model.TaskExecution
	err := r.db.WithContext(ctx).
		Select("id", "cleanup_status", "cleanup_token", "runtime_scope", "runtime_workload", "runtime_instance_id").
		Where("id = ?", id).
		First(&execution).Error
	if err != nil {
		return nil, err
	}
	return &execution, nil
}

func runtimeIdentityConflict(current *model.TaskExecution, identity model.RuntimeIdentity) error {
	for _, member := range []struct {
		name      string
		persisted string
		supplied  string
	}{
		{name: "scope", persisted: current.RuntimeScope, supplied: identity.Scope},
		{name: "workload", persisted: current.RuntimeWorkload, supplied: identity.Workload},
		{name: "instance", persisted: current.RuntimeInstanceID, supplied: identity.InstanceID},
	} {
		if member.supplied != "" && member.persisted != "" && member.persisted != member.supplied {
			return fmt.Errorf("%w: %s is %q, received %q", ErrRuntimeIdentityConflict, member.name, member.persisted, member.supplied)
		}
	}
	return nil
}

func isActiveTaskExecutionStatus(status string) bool {
	for _, active := range activeTaskExecutionStatuses {
		if status == active {
			return true
		}
	}
	return false
}

func (r *taskExecutionRepository) UpdateHeartbeat(ctx context.Context, id string, now time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.TaskExecution{}).
		Where("id = ?", id).
		Update("last_heartbeat_at", now).Error
}

func (r *taskExecutionRepository) LockActiveForProgress(ctx context.Context, id, taskID string) (bool, error) {
	return lockActiveTaskExecutionFirst(ctx, r.db, id, taskID, activeTaskExecutionLock{
		requireStarted:    true,
		requireIncomplete: true,
	})
}

func (r *taskExecutionRepository) LockCurrentForArtifactMutation(ctx context.Context, id, taskID string) (bool, error) {
	return lockActiveTaskExecutionFirst(ctx, r.db, id, taskID, activeTaskExecutionLock{
		requireStarted:    true,
		requireIncomplete: true,
	})
}

// Cross-table lock-order invariant: lock an existing task_execution before its
// task. Completion and structured progress use this locked validation; legacy
// heartbeat acquires the same row first via its execution UPDATE. Artifact
// mutations enforce the same order in lockCurrentArtifactExecution.
type activeTaskExecutionLock struct {
	target            string
	requireStarted    bool
	requireIncomplete bool
}

func lockActiveTaskExecutionFirst(ctx context.Context, db *gorm.DB, id, taskID string, guard activeTaskExecutionLock) (bool, error) {
	var execution model.TaskExecution
	query := db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("id = ? AND task_id = ? AND status = ?", id, taskID, model.TaskExecutionRunning)
	if guard.target != "" {
		query = query.Where("target = ?", guard.target)
	}
	if guard.requireStarted {
		query = query.Where("started = ?", true)
	}
	if guard.requireIncomplete {
		query = query.Where("completed_at IS NULL")
	}
	err := query.Take(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
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

func (r *taskExecutionRepository) TransitionDraftDelivery(ctx context.Context, id, from, to string, deliveryResult []byte) (bool, error) {
	updates := map[string]any{"draft_delivery_status": to}
	if len(deliveryResult) > 0 {
		updates["draft_delivery_result"] = deliveryResult
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND draft_delivery_status = ?", id, from).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) ClaimCleanup(ctx context.Context, id, token string, lease time.Duration) (bool, error) {
	if err := validateLeaseToken("cleanup", token); err != nil {
		return false, err
	}
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
	if err := validateLeaseToken("cleanup", token); err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND cleanup_status = ? AND cleanup_token = ?", id, model.TaskExecutionCleanupPending, token).
		Updates(map[string]any{
			"cleanup_status": model.TaskExecutionCleanupDone, "cleanup_token": "", "cleanup_at": nil, "cleanup_next_at": nil,
			"dispatch_claim_token": "", "dispatch_claimed_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *taskExecutionRepository) FailCleanup(ctx context.Context, id, token string, backoff time.Duration) (bool, error) {
	if err := validateLeaseToken("cleanup", token); err != nil {
		return false, err
	}
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
	if err := validateLeaseToken("cleanup", token); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND cleanup_status = ? AND cleanup_token = ?", id, model.TaskExecutionCleanupPending, token).
		Updates(map[string]any{"cleanup_token": "", "cleanup_at": nil}).Error
}

func validateLeaseToken(kind, token string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("%s lease token is required", kind)
	}
	return nil
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
