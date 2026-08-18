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

var ErrTaskExecutionEvidenceConflict = errors.New("task execution evidence conflict")

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

// UpdateProgressColumn writes the numeric progress percentage (0-100) to the
// progress column. Called by UpdateProgress when a stage-derived or explicit
// percent is available.
func (r *taskRepository) UpdateProgressColumn(ctx context.Context, id string, percent int) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("progress", percent).Error
}

// UpdateLatestProgress writes the latest structured progress payload to the
// dedicated latest_progress JSON column. Called by TaskService.UpdateProgress
// at every stage transition so Studio can render the current stage across
// reloads without parsing the mixed progress_log column.
func (r *taskRepository) UpdateLatestProgress(ctx context.Context, id string, payload model.ProgressPayload) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ?", id).
		Update("latest_progress", datatypes.NewJSONType(payload)).Error
}

// AdvanceStructuredProgress uses the declared stage/state sequence as its CAS
// high-water mark. Numeric progress is retained independently as a maximum so
// Pack stages with equal or zero percentages still advance deterministically.
func (r *taskRepository) AdvanceStructuredProgress(ctx context.Context, id string, sequence int, payload model.ProgressPayload) (advanced bool, persisted model.ProgressPayload, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Task{}).
			Where("id = ? AND progress_sequence < ?", id, sequence).
			Updates(map[string]any{
				"progress_sequence": sequence,
				"progress": gorm.Expr(
					"CASE WHEN progress < ? THEN ? ELSE progress END",
					payload.Percent,
					payload.Percent,
				),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		var finalProgress int
		if err := tx.Model(&model.Task{}).Select("progress").Where("id = ?", id).Scan(&finalProgress).Error; err != nil {
			return err
		}
		payload.Percent = finalProgress
		message, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal structured progress: %w", err)
		}
		if err := tx.Model(&model.Task{}).Where("id = ?", id).Updates(map[string]any{
			"latest_progress": datatypes.NewJSONType(payload),
			"progress_log":    gorm.Expr("CONCAT(COALESCE(progress_log, ''), ?)", string(message)+"\n"),
		}).Error; err != nil {
			return err
		}
		advanced = true
		persisted = payload
		return nil
	})
	return advanced, persisted, err
}

// GetTypeAndProgress loads only the type and progress columns for a task,
// avoiding the longtext progress_log transfer on hot paths.
func (r *taskRepository) GetTypeAndProgress(ctx context.Context, id string) (string, int, error) {
	var row struct {
		Type     string
		Progress int
	}
	if err := r.db.WithContext(ctx).Model(&model.Task{}).
		Select("type, progress").
		Where("id = ?", id).
		Take(&row).Error; err != nil {
		return "", 0, err
	}
	return row.Type, row.Progress, nil
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

// FinalizeLocalTask makes terminal ownership and terminal evidence one CAS.
// Only the request that still owns a running local claim can write any field.
func (r *taskRepository) FinalizeLocalTask(ctx context.Context, id, executionID, status, errorMsg, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error) {
	var won bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		won, err = (&taskRepository{db: tx}).FinalizeLocalTaskInTx(ctx, id, executionID, status, errorMsg, result, usage, costStatus)
		return err
	})
	return won, err
}

func (r *taskRepository) FinalizeLocalTaskInTx(ctx context.Context, id, executionID, status, errorMsg, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND status = ? AND execution_target = ? AND current_execution_id = ?", id, model.TaskStatusRunning, model.ExecutionTargetLocalClaimed, executionID).
		Updates(map[string]any{
			"status": status, "error_message": errorMsg, "completed_at": now, "result": result,
			"terminal_model_usage": datatypes.NewJSONType(usage), "cost_status": costStatus,
		})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	executionStatus := model.TaskExecutionFailed
	if status == model.TaskStatusCompleted {
		executionStatus = model.TaskExecutionSucceeded
	} else if status == model.TaskStatusCancelled {
		executionStatus = model.TaskExecutionCancelled
	}
	executionResult := datatypes.JSON([]byte(result))
	execRes := r.db.WithContext(ctx).Model(&model.TaskExecution{}).
		Where("id = ? AND task_id = ? AND target = ? AND status = ?", executionID, id, model.ExecutionTargetLocalClaimed, model.TaskExecutionRunning).
		Updates(map[string]any{
			"status": executionStatus, "terminal_reason": errorMsg, "result": executionResult,
			"completed_at": now, "finalization_status": model.TaskExecutionFinalizationDone,
			"cleanup_status": model.TaskExecutionCleanupDone,
		})
	if execRes.Error != nil {
		return false, execRes.Error
	}
	if execRes.RowsAffected != 1 {
		return false, fmt.Errorf("local task execution %s is missing or not running", executionID)
	}
	return true, nil
}

func (r *taskRepository) FinalizeTaskForExecution(ctx context.Context, id, executionID, status, errorMsg string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND status = ? AND current_execution_id = ?", id, model.TaskStatusRunning, executionID).
		Updates(map[string]any{
			"status":        status,
			"error_message": errorMsg,
			"completed_at":  time.Now(),
		})
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
		// Exclude tasks awaiting a desktop local-executor claim — those must not
		// be scooped up by cloud DispatchPendingTasks. execution_target defaults
		// to '' (cloud); only "local" tasks are skipped here. "local_claimed"
		// tasks are already status=running so they never match this query.
		Where("execution_target <> ?", model.ExecutionTargetLocal).
		Where("NOT (retry_count > 0 AND updated_at > ?)", time.Now().Add(-2*time.Minute)).
		Order("created_at ASC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// ClaimNextLocalTask atomically claims the oldest pending local-target task for
// the user. The find + conditional CAS run inside a single transaction: the
// UPDATE ... WHERE id=? AND status='pending' guarantees exactly one concurrent
// claimer wins (RowsAffected=1); losers get 0 and surface as (nil, nil). The
// executorInfo blob is recorded for diagnostics. Returns (nil, nil) when no
// task is claimable.
func (r *taskRepository) ClaimNextLocalTask(ctx context.Context, userID string, executorInfo []byte) (*model.Task, error) {
	var claimedID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		findErr := tx.
			Where("user_id = ? AND status = ? AND execution_target = ? AND deleting_at IS NULL", userID, model.TaskStatusPending, model.ExecutionTargetLocal).
			Where("local_claim_deadline IS NULL OR local_claim_deadline >= ?", time.Now()).
			Order("created_at ASC").
			First(&task).Error
		if findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil // nothing claimable
			}
			return findErr
		}

		updates := map[string]interface{}{
			"status":           model.TaskStatusRunning,
			"started_at":       time.Now(),
			"execution_target": model.ExecutionTargetLocalClaimed,
		}
		if len(executorInfo) > 0 {
			updates["executor_info"] = string(executorInfo)
		}
		res := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND deleting_at IS NULL", task.ID, model.TaskStatusPending).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Lost the race to another claimer (or status changed). Treat as
			// nothing-claimable so the caller polls again.
			return nil
		}
		claimedID = task.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	if claimedID == "" {
		return nil, nil
	}
	// Reload so the returned task reflects the CAS (status=running, target set).
	return r.FindByID(ctx, claimedID)
}

// FindExpiredLocalTasks returns IDs of pending local-target tasks past their
// claim deadline — candidates for cloud fallback.
func (r *taskRepository) FindExpiredLocalTasks(ctx context.Context, now time.Time) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("status = ? AND execution_target = ? AND deleting_at IS NULL", model.TaskStatusPending, model.ExecutionTargetLocal).
		Where("local_claim_deadline IS NOT NULL AND local_claim_deadline < ?", now).
		Limit(100).
		Pluck("id", &ids).Error
	return ids, err
}

// ResetLocalTarget atomically clears the local-execution markers so a task is
// eligible for normal cloud dispatch — but ONLY while it is still pending +
// local-target (a guarded CAS, mirroring ClaimNextLocalTask). Returns reset=true
// when the CAS matched; reset=false when the task was claimed or changed since
// the fallback selected it, in which case the caller must NOT re-enqueue (doing
// so would double-run the task on cloud + the desktop that just claimed it).
func (r *taskRepository) ResetLocalTarget(ctx context.Context, taskID string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Task{}).
		Where("id = ? AND status = ? AND execution_target = ? AND deleting_at IS NULL", taskID, model.TaskStatusPending, model.ExecutionTargetLocal).
		Updates(map[string]interface{}{
			"execution_target":     model.ExecutionTargetCloud,
			"local_claim_deadline": nil,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
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

// FailRunningTask guardedly records a pre-start terminal failure. The status,
// diagnostic message, and completion timestamp change atomically.
func (r *taskRepository) FailRunningTask(ctx context.Context, taskID, errorMsg string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ?", taskID, model.TaskStatusRunning).
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

// ResetTerminalTaskForResume atomically persists supplemental inputs, moves a
// terminal task back to pending, and routes it to the cloud executor.
func (r *taskRepository) ResetTerminalTaskForResume(ctx context.Context, taskID string, attachments []model.EntryAttachment) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status IN ?", taskID, model.TerminalTaskStatuses).
		Where("deleting_at IS NULL").
		Where("current_execution_id IS NULL OR EXISTS (SELECT 1 FROM task_executions WHERE task_executions.id = tasks.current_execution_id AND task_executions.finalization_status = ?)", model.TaskExecutionFinalizationDone).
		Updates(map[string]interface{}{
			"status":                 model.TaskStatusPending,
			"started_at":             nil,
			"completed_at":           nil,
			"last_heartbeat_at":      nil,
			"error_message":          "",
			"result":                 nil,
			"terminal_model_usage":   datatypes.NewJSONType([]model.ModelTokenUsage{}),
			"cost_status":            "",
			"progress":               0,
			"progress_sequence":      0,
			"latest_progress":        datatypes.NewJSONType(model.ProgressPayload{}),
			"workflow_status":        nil,
			"publish_approval_state": "",
			"pending_draft_articles": nil,
			"published":              false,
			"published_at":           nil,
			"input_attachments":      datatypes.NewJSONType(attachments),
			"execution_target":       model.ExecutionTargetCloud,
			"local_claim_deadline":   nil,
			"executor_info":          datatypes.NewJSONType(model.ExecutorMeta{}),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// SetPublished toggles the published flag and updates published_at timestamp.
func (r *taskRepository) SetPublished(ctx context.Context, id string, published bool) error {
	updates := map[string]interface{}{"published": published}
	if published {
		now := time.Now()
		updates["published_at"] = &now
	} else {
		updates["published_at"] = nil
	}
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Updates(updates).Error
}

// UpdatePublishApproval writes the publish-approval state column and the frozen
// pending-draft-articles blob for a task. Used by holdPublishForApproval to enter
// the pending state (state=pending, blob=marshaled articles).
func (r *taskRepository) UpdatePublishApproval(ctx context.Context, id, state string, pendingArticles []byte) error {
	updates := map[string]interface{}{"publish_approval_state": state}
	if pendingArticles != nil {
		updates["pending_draft_articles"] = pendingArticles
	}
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Updates(updates).Error
}

// CompareAndSwapPublishApproval atomically transitions publish_approval_state
// from expected to newState, clearing the frozen pending-draft-articles blob in
// the same update when clearArticles is true (spec: approve/reject clear the
// blob). Returns true only if the task was in the expected state and is now
// newState — exactly one concurrent caller wins. Mirrors CompareAndSwapStatus:
// the atomic UPDATE ... WHERE id=? AND publish_approval_state=? guarantees the
// winner is unique, so ApprovePublish can safely gate its publish goroutine on
// the returned bool (no double-publish).
func (r *taskRepository) CompareAndSwapPublishApproval(ctx context.Context, id, expected, newState string, clearArticles bool) (bool, error) {
	updates := map[string]interface{}{"publish_approval_state": newState}
	if clearArticles {
		// datatypes.JSON zero value serializes to SQL NULL → column cleared.
		updates["pending_draft_articles"] = nil
	}
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND publish_approval_state = ?", id, expected).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *taskRepository) UpdateWorkflowStatus(ctx context.Context, id string, workflowStatus string) error {
	return r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ?", id).Update("workflow_status", workflowStatus).Error
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
