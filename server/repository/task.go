package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTaskExecutionEvidenceConflict = errors.New("task execution evidence conflict")
	ErrCloudTaskExecutionCASLost     = errors.New("cloud task execution finalization CAS lost")
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

func (r *taskRepository) FindByIDForUpdate(ctx context.Context, id string) (*model.Task, error) {
	var task model.Task
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepository) FindByUserID(ctx context.Context, userID string, projectID string, planID string, offset, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC")
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if planID != "" {
		q = q.Where("plan_id = ?", planID)
	}
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) FindByUserIDAndStatus(ctx context.Context, userID, status string, projectID string, planID string, offset, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).Where("user_id = ? AND status = ?", userID, status).Order("created_at DESC")
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if planID != "" {
		q = q.Where("plan_id = ?", planID)
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

func (r *taskRepository) FindRunningByUser(ctx context.Context, userID string, projectID string) ([]*model.Task, error) {
	var tasks []*model.Task
	q := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Where("status = ?", model.TaskStatusRunning)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err := q.Find(&tasks).Error; err != nil {
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

// UpdateLifecycle persists a full lifecycle snapshot only for the current
// execution. Callers hold execution/task locks and own revision assignment.
func (r *taskRepository) UpdateLifecycle(ctx context.Context, id, executionID string, lifecycle model.TaskLifecycle) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND current_execution_id = ?", id, executionID).
		Update("lifecycle", datatypes.NewJSONType(lifecycle))
	return result.RowsAffected == 1, result.Error
}

// UpdateExecutionEvidence persists the JSON result and its typed cost evidence
// with one statement. RowsAffected distinguishes a missing task from success.
func (r *taskRepository) UpdateExecutionEvidence(ctx context.Context, id, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"result":               result,
			"terminal_model_usage": datatypes.NewJSONType(usage),
			"cost_status":          costStatus,
		})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil
	}
	return r.classifyUnchangedExecutionEvidence(ctx, id, "", result, usage, costStatus)
}

// UpdateExecutionEvidenceForExecution prevents a stale cloud attempt from
// replacing the task evidence owned by the current durable execution.
func (r *taskRepository) UpdateExecutionEvidenceForExecution(ctx context.Context, id, executionID, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND current_execution_id = ?", id, executionID).
		Updates(map[string]any{
			"result":               result,
			"terminal_model_usage": datatypes.NewJSONType(usage),
			"cost_status":          costStatus,
		})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil
	}
	return r.classifyUnchangedExecutionEvidence(ctx, id, executionID, result, usage, costStatus)
}

func (r *taskRepository) classifyUnchangedExecutionEvidence(ctx context.Context, id, executionID, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error) {
	var task model.Task
	err := r.db.WithContext(ctx).Model(&model.Task{}).
		Select("id", "current_execution_id", "result", "terminal_model_usage", "cost_status").
		Where("id = ?", id).
		Take(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read back unchanged execution evidence: %w", err)
	}
	if executionID != "" && (task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID) {
		return false, nil
	}
	if task.Result != nil && jsonValuesEqual(*task.Result, result) &&
		reflect.DeepEqual(task.TerminalModelUsage.Data(), usage) && task.CostStatus == costStatus {
		return true, nil
	}
	return false, ErrTaskExecutionEvidenceConflict
}

func jsonValuesEqual(left, right string) bool {
	decode := func(value string) (any, error) {
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return nil, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("multiple JSON values")
			}
			return nil, err
		}
		return decoded, nil
	}
	leftValue, leftErr := decode(left)
	rightValue, rightErr := decode(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(leftValue, rightValue)
}

// FinalizeCloudTaskWithArtifactsInTx owns the atomic cloud terminal state:
// current-execution authority, artifact visibility, terminal evidence, and
// task status. The caller persists billing in the same outer transaction.
func (r *taskRepository) FinalizeCloudTaskWithArtifactsInTx(ctx context.Context, id, executionID, status, errorMsg, result string, usage []model.ModelTokenUsage, costStatus string, artifactAction CloudTaskArtifactAction) (bool, error) {
	task, execution, err := lockCurrentArtifactExecution(r.db.WithContext(ctx), id, executionID)
	if errors.Is(err, ErrTaskFileExecutionNotCurrent) {
		return false, ErrCloudTaskExecutionCASLost
	}
	if err != nil {
		return false, fmt.Errorf("lock cloud task execution for finalization: %w", err)
	}
	expectedStatus, terminal := cloudTaskStatusForExecution(execution.Status)
	if strings.TrimSpace(execution.Target) == "" || !terminal || execution.CompletedAt == nil || status != expectedStatus {
		return false, ErrCloudTaskExecutionCASLost
	}

	if task.Status != model.TaskStatusRunning {
		if !cloudTaskTerminalStateMatches(task, execution, status, errorMsg, result, usage, costStatus, artifactAction) {
			return false, ErrCloudTaskExecutionCASLost
		}
		return false, nil
	}

	switch artifactAction {
	case CloudTaskArtifactsDeliver:
		if status != model.TaskStatusCompleted || execution.Status != model.TaskExecutionSucceeded {
			return false, fmt.Errorf("publishing cloud artifacts requires succeeded execution and completed task status")
		}
		if err := deliverLockedExecutionManifest(ctx, r.db, task, execution); err != nil {
			return false, err
		}
	case CloudTaskArtifactsRetain:
		if status != model.TaskStatusFailed && status != model.TaskStatusCancelled {
			return false, fmt.Errorf("collecting cloud artifacts requires failed or cancelled task status")
		}
		if !isCollectableArtifactExecution(execution) {
			return false, fmt.Errorf("collecting cloud artifacts requires failed, cancelled, or timed out execution")
		}
		if err := retainLockedExecutionManifest(ctx, r.db, execution); err != nil && !errors.Is(err, ErrNoPendingExecutionArtifacts) {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported cloud artifact action %q", artifactAction)
	}

	now := time.Now()
	lifecycle, lifecycleChanged := model.NormalizeTaskLifecycleTerminal(task.Lifecycle.Data(), status, errorMsg, now)
	updates := map[string]any{
		"status": status, "error_message": errorMsg, "completed_at": now, "result": result,
		"terminal_model_usage": datatypes.NewJSONType(usage), "cost_status": costStatus,
	}
	if lifecycleChanged {
		updates["lifecycle"] = datatypes.NewJSONType(lifecycle)
	}
	res := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND status = ? AND current_execution_id = ?", id, model.TaskStatusRunning, executionID).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected != 1 {
		return false, ErrCloudTaskExecutionCASLost
	}
	return true, nil
}

func cloudTaskStatusForExecution(executionStatus string) (string, bool) {
	switch executionStatus {
	case model.TaskExecutionSucceeded:
		return model.TaskStatusCompleted, true
	case model.TaskExecutionCancelled:
		return model.TaskStatusCancelled, true
	case model.TaskExecutionFailed, model.TaskExecutionTimedOut:
		return model.TaskStatusFailed, true
	default:
		return "", false
	}
}

func cloudTaskTerminalStateMatches(task *model.Task, execution *model.TaskExecution, status, errorMsg, result string, usage []model.ModelTokenUsage, costStatus string, artifactAction CloudTaskArtifactAction) bool {
	if task == nil || execution == nil || task.Status != status || task.ErrorMessage != errorMsg ||
		task.CompletedAt == nil || task.Result == nil || !jsonValuesEqual(*task.Result, result) ||
		!reflect.DeepEqual(task.TerminalModelUsage.Data(), usage) || task.CostStatus != costStatus {
		return false
	}
	switch artifactAction {
	case CloudTaskArtifactsDeliver:
		return execution.Status == model.TaskExecutionSucceeded && execution.ManifestStatus == model.TaskExecutionManifestDelivered
	case CloudTaskArtifactsRetain:
		return isCollectableArtifactExecution(execution) && execution.ManifestStatus == model.TaskExecutionManifestRetained
	default:
		return false
	}
}

func (r *taskRepository) FinalizeTaskForExecution(ctx context.Context, id, executionID, status, errorMsg string) (bool, error) {
	var current model.Task
	if err := r.db.WithContext(ctx).Select("id", "lifecycle").Where("id = ? AND status = ? AND current_execution_id = ?", id, model.TaskStatusRunning, executionID).Take(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	now := time.Now()
	lifecycle, lifecycleChanged := model.NormalizeTaskLifecycleTerminal(current.Lifecycle.Data(), status, errorMsg, now)
	updates := map[string]any{
		"status": status, "error_message": errorMsg, "completed_at": now,
	}
	if lifecycleChanged {
		updates["lifecycle"] = datatypes.NewJSONType(lifecycle)
	}
	res := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND status = ? AND current_execution_id = ?", id, model.TaskStatusRunning, executionID).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *taskRepository) UpdateBillingTerminalReason(ctx context.Context, id, reason string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("billing_terminal_reason", reason).Error
}

// Update saves the full task object.
func (r *taskRepository) Update(ctx context.Context, task *model.Task) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *taskRepository) UpdateInputAttachments(ctx context.Context, id string, attachments []model.EntryAttachment) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Update("input_attachments", datatypes.NewJSONType(attachments)).Error
}

func (r *taskRepository) UpdateTitle(ctx context.Context, id string, title string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("title", title).Error
}

func (r *taskRepository) SetStartedAt(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("started_at", now).Error
}

func (r *taskRepository) SetCompletedAt(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("completed_at", now).Error
}

func (r *taskRepository) UpdateHeartbeat(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("last_heartbeat_at", now).Error
}

func (r *taskRepository) CountByUserID(ctx context.Context, userID string, projectID string, planID string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.Task{}).Where("user_id = ?", userID)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if planID != "" {
		q = q.Where("plan_id = ?", planID)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *taskRepository) CountByUserIDAndStatus(ctx context.Context, userID, status string, projectID string, planID string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.Task{}).Where("user_id = ? AND status = ?", userID, status)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if planID != "" {
		q = q.Where("plan_id = ?", planID)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *taskRepository) CountRunningByProject(ctx context.Context, projectID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("project_id = ? AND status = ?", projectID, model.TaskStatusRunning).
		Count(&count).Error
	return count, err
}

func (r *taskRepository) FindPendingByProject(ctx context.Context, projectID string, limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND status = ? AND deleting_at IS NULL", projectID, model.TaskStatusPending).
		Where("(plan_id IS NULL OR EXISTS (SELECT 1 FROM plans WHERE plans.id = tasks.plan_id AND plans.status = ?))", model.PlanStatusActive).
		Where("NOT (retry_count > 0 AND updated_at > ?)", time.Now().Add(-2*time.Minute)).
		Order("created_at ASC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (r *taskRepository) FindPendingByPlanID(ctx context.Context, planID string) ([]*model.Task, error) {
	var tasks []*model.Task
	err := r.db.WithContext(ctx).
		Where("plan_id = ? AND status = ? AND current_execution_id IS NULL AND deleting_at IS NULL", planID, model.TaskStatusPending).
		Order("created_at ASC").
		Find(&tasks).Error
	return tasks, err
}

func (r *taskRepository) BeginDelete(ctx context.Context, taskID string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND deleting_at IS NULL", taskID).
		Update("deleting_at", time.Now())
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *taskRepository) DeleteIfDeleting(ctx context.Context, taskID string) (bool, error) {
	result := r.db.WithContext(ctx).
		Where("id = ? AND deleting_at IS NOT NULL", taskID).
		Delete(&model.Task{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// FindTitlesByProjectID returns all recorded titles for a project, ordered by creation time descending.
func (r *taskRepository) FindTitlesByProjectID(ctx context.Context, projectID string) ([]string, error) {
	var titles []string
	err := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("project_id = ? AND title != ''", projectID).
		Group("title").
		Order("MAX(created_at) DESC").
		Limit(200).
		Pluck("title", &titles).Error
	return titles, err
}

func (r *taskRepository) FindTitleTasksByProjectID(ctx context.Context, projectID string) ([]*model.Task, error) {
	var tasks []*model.Task
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND title != ''", projectID).
		Order("created_at DESC").
		Limit(200).
		Find(&tasks).Error
	return tasks, err
}

func (r *taskRepository) ClearTitles(ctx context.Context, titles []string) (int64, error) {
	if len(titles) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("title IN ?", titles).
		Update("title", "")
	return result.RowsAffected, result.Error
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

// CompareAndSwapStatusForUser atomically transitions a task only when it belongs
// to the supplied user. External user-facing paths must use this guard to avoid
// cross-account cancellation.
func (r *taskRepository) CompareAndSwapStatusForUser(ctx context.Context, taskID, userID, expected, newStatus string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND user_id = ? AND status = ?", taskID, userID, expected).
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
		Where("id = ? AND status = ? AND deleting_at IS NULL", taskID, expected).
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

// SetCurrentExecution records the active durable attempt only while the task is
// running. Callers create the execution and update this pointer in one transaction.
func (r *taskRepository) SetCurrentExecution(ctx context.Context, taskID, executionID string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ? AND deleting_at IS NULL", taskID, model.TaskStatusRunning).
		Update("current_execution_id", executionID)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// FailStaleRunningTask terminalizes only a legacy running task with no durable
// execution whose latest heartbeat (or start time before the first heartbeat)
// is still at or before the caller's cutoff.
func (r *taskRepository) FailStaleRunningTask(ctx context.Context, taskID string, staleBefore time.Time, errorMsg string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ? AND current_execution_id IS NULL AND deleting_at IS NULL", taskID, model.TaskStatusRunning).
		Where("(last_heartbeat_at IS NOT NULL AND last_heartbeat_at <= ?) OR (last_heartbeat_at IS NULL AND started_at IS NOT NULL AND started_at <= ?)", staleBefore, staleBefore).
		Updates(map[string]interface{}{
			"status":        model.TaskStatusFailed,
			"error_message": errorMsg,
			"completed_at":  time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// FailPendingTask atomically records a terminal enqueue failure only while the
// task is still pending, so it cannot stamp a newer execution attempt.
func (r *taskRepository) FailPendingTask(ctx context.Context, taskID, errorMsg string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ?", taskID, model.TaskStatusPending).
		Updates(map[string]interface{}{
			"status":        model.TaskStatusFailed,
			"error_message": errorMsg,
			"completed_at":  time.Now(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// CancelPendingTask atomically cancels an admitted task only while it remains
// pending and has not acquired an execution. Callers use this for plan pause
// backlog cancellation and must settle any admission charge in the same tx.
func (r *taskRepository) CancelPendingTask(ctx context.Context, taskID, errorMsg string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ? AND current_execution_id IS NULL AND deleting_at IS NULL", taskID, model.TaskStatusPending).
		Updates(map[string]interface{}{
			"status":                  model.TaskStatusCancelled,
			"error_message":           errorMsg,
			"completed_at":            time.Now(),
			"billing_terminal_reason": model.TaskBillingTerminalPlanPaused,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ResetRetryableTaskForResume atomically persists supplemental inputs, moves a
// failed or cancelled task back to pending, and routes it to the cloud executor.
func (r *taskRepository) ResetRetryableTaskForResume(ctx context.Context, taskID string, attachments []model.EntryAttachment) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status IN ?", taskID, []string{model.TaskStatusFailed, model.TaskStatusCancelled}).
		Where("deleting_at IS NULL").
		Where("current_execution_id IS NULL OR EXISTS (SELECT 1 FROM task_executions WHERE task_executions.id = tasks.current_execution_id AND task_executions.finalization_status = ?)", model.TaskExecutionFinalizationDone).
		Updates(map[string]interface{}{
			"status":               model.TaskStatusPending,
			"started_at":           nil,
			"completed_at":         nil,
			"last_heartbeat_at":    nil,
			"error_message":        "",
			"result":               nil,
			"outcome":              nil,
			"terminal_model_usage": datatypes.NewJSONType([]model.ModelTokenUsage{}),
			"cost_status":          "",
			"workflow_status":      nil,
			"input_attachments":    datatypes.NewJSONType(attachments),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *taskRepository) UpdateWorkflowStatus(ctx context.Context, id string, workflowStatus string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("workflow_status", workflowStatus).Error
}

func (r *taskRepository) UpdateOutcomeForExecution(ctx context.Context, id, executionID string, outcome model.TaskOutcome) (bool, error) {
	encoded, err := json.Marshal(outcome)
	if err != nil {
		return false, fmt.Errorf("marshal task outcome: %w", err)
	}
	result := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND current_execution_id = ?", id, executionID).
		Update("outcome", datatypes.JSON(encoded))
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	var persisted struct {
		ID                 string
		CurrentExecutionID *string
		Outcome            datatypes.JSON
	}
	err = r.db.WithContext(ctx).Table("tasks").
		Select("id", "current_execution_id", "outcome").
		Where("id = ?", id).
		Take(&persisted).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read back unchanged task outcome: %w", err)
	}
	if persisted.CurrentExecutionID == nil || *persisted.CurrentExecutionID != executionID {
		return false, nil
	}
	if jsonValuesEqual(string(persisted.Outcome), string(encoded)) {
		return true, nil
	}
	return false, ErrTaskExecutionEvidenceConflict
}

func (r *taskRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.Task{}, "id = ?", id).Error
}

// usageStatuses is the set of task statuses counted toward usage statistics.
// Cancelled tasks are excluded because they may not have consumed meaningful
// resources.
var usageStatuses = []string{
	model.TaskStatusCompleted,
	model.TaskStatusFailed,
}

// AggregateUsageByUser returns SQL-level SUM aggregates for token usage.
func (r *taskRepository) AggregateUsageByUser(ctx context.Context, userID string, from, to time.Time, projectID string) (totalTasks int64, totalInput, totalOutput, totalCacheRead, totalCacheCreation int64, err error) {
	type row struct {
		Tasks        int64
		Input        int64
		Output       int64
		CacheRead    int64
		CacheCreated int64
	}
	var r2 row
	q := r.db.WithContext(ctx).Model(&model.Task{}).
		Select(
			"COUNT(*) AS tasks",
			"COALESCE(SUM(input_tokens), 0) AS input",
			"COALESCE(SUM(output_tokens), 0) AS output",
			"COALESCE(SUM(cache_read_tokens), 0) AS cache_read",
			"COALESCE(SUM(cache_creation_tokens), 0) AS cache_created",
		).
		Where("user_id = ?", userID).
		Where("created_at >= ? AND created_at <= ?", from, to).
		Where("status IN ?", usageStatuses)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err = q.Scan(&r2).Error; err != nil {
		return
	}
	return r2.Tasks, r2.Input, r2.Output, r2.CacheRead, r2.CacheCreated, nil
}

// TypeUsageRow holds per-type aggregated usage data from a SQL GROUP BY query.
type TypeUsageRow struct {
	Type                string
	Count               int64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// AggregateUsageByType returns per-type token usage via SQL GROUP BY.
func (r *taskRepository) AggregateUsageByType(ctx context.Context, userID string, from, to time.Time, projectID string) ([]TypeUsageRow, error) {
	var rows []TypeUsageRow
	q := r.db.WithContext(ctx).Model(&model.Task{}).
		Select(
			"type",
			"COUNT(*) AS count",
			"COALESCE(SUM(input_tokens), 0) AS input_tokens",
			"COALESCE(SUM(output_tokens), 0) AS output_tokens",
			"COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens",
			"COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens",
		).
		Where("user_id = ?", userID).
		Where("created_at >= ? AND created_at <= ?", from, to).
		Where("status IN ?", usageStatuses)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err := q.Group("type").Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
