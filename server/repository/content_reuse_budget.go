package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrContentReuseBudgetExceeded      = errors.New("content reuse budget exceeded")
	ErrContentReuseOperationInProgress = errors.New("content reuse operation already in progress")
)

// The policy lock serializes capacity checks for all of one user's runs. The
// caller transaction also owns wallet settlement when consuming a reservation.
func (r *contentAssetRepository) ReserveReuseOperationBudget(ctx context.Context, task *model.Task, key, fingerprint string, amount int64, consumeExisting bool) error {
	if task == nil || task.ReuseRunID == "" {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var policy model.ContentAutopilotPolicy
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", task.UserID).First(&policy).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var run model.ContentReuseRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", task.ReuseRunID, task.UserID).First(&run).Error; err != nil {
			return err
		}
		if !reuseTaskMatchesRun(ctx, tx, task, &run) {
			return ErrContentReuseRunConflict
		}
		var frozen model.ContentAutopilotPolicy
		if err := json.Unmarshal(run.PolicySnapshot, &frozen); err != nil {
			return fmt.Errorf("decode reuse budget policy: %w", err)
		}
		if !frozen.Enabled {
			return nil
		}
		if amount < 0 || frozen.MaxRunCostCredits <= 0 || frozen.DailyBudgetCredits <= 0 {
			return ErrContentReuseBudgetExceeded
		}
		var existing model.ContentReuseOperationBudget
		err := tx.Where("run_id = ? AND operation_key = ?", run.ID, key).First(&existing).Error
		if err == nil {
			if existing.Fingerprint != fingerprint || existing.Amount != amount {
				return ErrContentReuseRunConflict
			}
			if consumeExisting {
				return nil
			}
			return ErrContentReuseOperationInProgress
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if run.Status != model.ContentReuseRunStatusRunning {
			return ErrContentReuseRunConflict
		}
		used, err := reuseRunBudgetUsage(ctx, tx, &run)
		if err != nil {
			return err
		}
		if used > frozen.MaxRunCostCredits || amount > frozen.MaxRunCostCredits-used {
			return ErrContentReuseBudgetExceeded
		}
		day := time.Now().UTC().Truncate(24 * time.Hour)
		dailyUsed, err := reuseDailyBudgetUsage(ctx, tx, run.UserID, "", day)
		if err != nil {
			return err
		}
		if amount > frozen.DailyBudgetCredits-dailyUsed {
			return ErrContentReuseBudgetExceeded
		}
		return tx.Create(&model.ContentReuseOperationBudget{RunID: run.ID, OperationKey: key, Fingerprint: fingerprint, Amount: amount}).Error
	})
}

func reuseRunBudgetUsage(ctx context.Context, db *gorm.DB, run *model.ContentReuseRun) (int64, error) {
	used := run.CostCredits + run.BudgetReserved
	total, hasTasks, err := reuseRunWalletCost(ctx, db, run)
	if err != nil {
		return 0, err
	}
	if hasTasks {
		used = total
	}
	var pending int64
	if err := db.WithContext(ctx).Model(&model.ContentReuseOperationBudget{}).Where("run_id = ?", run.ID).Select("COALESCE(SUM(amount), 0)").Scan(&pending).Error; err != nil {
		return 0, err
	}
	return used + pending, nil
}

func reuseRunWalletCost(ctx context.Context, db *gorm.DB, run *model.ContentReuseRun) (int64, bool, error) {
	var tasks []model.Task
	if err := db.WithContext(ctx).Where("user_id = ? AND (reuse_run_id = ? OR id = ?)", run.UserID, run.ID, run.TaskID).Find(&tasks).Error; err != nil {
		return 0, false, err
	}
	var cost int64
	for _, task := range tasks {
		amount, err := reuseTaskWalletCost(ctx, db, &task)
		if err != nil {
			return 0, false, err
		}
		cost += amount
	}
	return cost, len(tasks) > 0, nil
}

// Admissions retain their original day; independently paid operations use the
// provider invocation day. Pending reservations are counted until wallet posting.
func reuseDailyBudgetUsage(ctx context.Context, db *gorm.DB, userID, excludeRun string, day time.Time) (int64, error) {
	var runs []model.ContentReuseRun
	if err := db.WithContext(ctx).Where("user_id = ? AND id <> ?", userID, excludeRun).Find(&runs).Error; err != nil {
		return 0, err
	}
	var used int64
	for _, run := range runs {
		admissionDay := run.CreatedAt.UTC().Truncate(24 * time.Hour)
		if run.BudgetDate != nil {
			admissionDay = run.BudgetDate.UTC()
		}
		var tasks []model.Task
		if err := db.WithContext(ctx).Where("user_id = ? AND (reuse_run_id = ? OR id = ?)", userID, run.ID, run.TaskID).Find(&tasks).Error; err != nil {
			return 0, err
		}
		if len(tasks) == 0 && admissionDay.Equal(day) {
			used += run.CostCredits + run.BudgetReserved
		}
		for _, task := range tasks {
			charges, err := (&billingRepository{db: db}).ListChargesByTaskIDs(ctx, userID, []string{task.ID})
			if err != nil {
				return 0, err
			}
			reversed := map[string]bool{}
			hasTaskCharge := false
			for _, charge := range charges {
				if charge.ReversalOfID != nil {
					reversed[*charge.ReversalOfID] = true
				}
				if charge.Kind == model.BillingChargeKindTask {
					hasTaskCharge = true
				}
			}
			if !hasTaskCharge && admissionDay.Equal(day) {
				used += task.BillingPriceCredits
			}
			for _, charge := range charges {
				if charge.Kind != model.BillingChargeKindReversal && !reversed[charge.ID] && !charge.CreatedAt.Before(day) && charge.CreatedAt.Before(day.Add(24*time.Hour)) {
					used += charge.PriceCredits
				}
			}
		}
		var pending int64
		if err := db.WithContext(ctx).Model(&model.ContentReuseOperationBudget{}).Where("run_id = ? AND created_at >= ? AND created_at < ?", run.ID, day, day.Add(24*time.Hour)).Select("COALESCE(SUM(amount), 0)").Scan(&pending).Error; err != nil {
			return 0, err
		}
		used += pending
	}
	return used, nil
}

func reuseTaskWalletCost(ctx context.Context, db *gorm.DB, task *model.Task) (int64, error) {
	totals, err := (&billingRepository{db: db}).ListTaskChargeTotals(ctx, task.UserID, []string{task.ID})
	if err != nil {
		return 0, err
	}
	if len(totals) == 0 {
		return task.BillingPriceCredits, nil
	}
	total := totals[0].TotalCredits
	if totals[0].TaskChargeCount == 0 {
		total += task.BillingPriceCredits
	}
	return total, nil
}

func (r *contentAssetRepository) ReleaseReuseOperationBudget(ctx context.Context, runID, key string) error {
	return r.db.WithContext(ctx).Where("run_id = ? AND operation_key = ?", runID, key).Delete(&model.ContentReuseOperationBudget{}).Error
}

func (r *contentAssetRepository) FindReuseOperationBudget(ctx context.Context, runID, key string) (*model.ContentReuseOperationBudget, error) {
	var reservation model.ContentReuseOperationBudget
	err := r.db.WithContext(ctx).Where("run_id = ? AND operation_key = ?", runID, key).First(&reservation).Error
	return &reservation, err
}

func (r *contentAssetRepository) MarkReuseOperationAmbiguous(ctx context.Context, runID, key string) error {
	var userID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if key != "" {
			result := tx.Model(&model.ContentReuseOperationBudget{}).Where("run_id = ? AND operation_key = ?", runID, key).Update("status", "ambiguous")
			if result.Error != nil || result.RowsAffected == 0 {
				return result.Error
			}
		}
		var run model.ContentReuseRun
		if err := tx.Where("id = ?", runID).First(&run).Error; err != nil {
			return err
		}
		userID = run.UserID
		return tx.Model(&model.ContentReuseRun{}).Where("id = ? AND status NOT IN ?", runID, []string{model.ContentReuseRunStatusCancelled, model.ContentReuseRunStatusCompleted}).Updates(map[string]any{"status": model.ContentReuseRunStatusPaused, "error_code": "operation_reconciliation_required", "error_message": "付费请求状态待核实，请先确认供应商结果与计费。", "version": gorm.Expr("version + 1")}).Error
	})
	if err != nil || userID == "" {
		return err
	}
	_, err = r.ReconcileReuseOperationHold(ctx, userID, runID)
	return err
}

func (r *contentAssetRepository) RecoverReuseOperationBudgets(ctx context.Context, now time.Time) error {
	var stale []model.ContentReuseOperationBudget
	if err := r.db.WithContext(ctx).Where("status = ? AND created_at <= ?", "invoking", now.Add(-15*time.Minute)).Limit(100).Find(&stale).Error; err != nil {
		return err
	}
	for _, reservation := range stale {
		var settlements int64
		if err := r.db.WithContext(ctx).Model(&model.BillingSettlementOutbox{}).Where("idempotency_scope = ? AND idempotency_key = ?", "mcp-image-settlement", reservation.OperationKey).Count(&settlements).Error; err != nil {
			return err
		}
		if settlements == 0 {
			if err := r.MarkReuseOperationAmbiguous(ctx, reservation.RunID, reservation.OperationKey); err != nil {
				return err
			}
		} else {
			if err := r.db.WithContext(ctx).Model(&model.ContentReuseOperationBudget{}).Where("run_id = ? AND operation_key = ? AND status = ?", reservation.RunID, reservation.OperationKey, "invoking").Update("status", "awaiting_settlement").Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *contentAssetRepository) SettleReuseOperationBudget(ctx context.Context, task *model.Task, key string) error {
	if task == nil || task.ReuseRunID == "" {
		return nil
	}
	if err := r.ReleaseReuseOperationBudget(ctx, task.ReuseRunID, key); err != nil {
		return err
	}
	var run model.ContentReuseRun
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", task.ReuseRunID, task.UserID).First(&run).Error; err != nil {
		return err
	}
	total, _, err := reuseRunWalletCost(ctx, r.db, &run)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Model(&model.ContentReuseRun{}).Where("id = ? AND user_id = ?", task.ReuseRunID, task.UserID).Updates(map[string]any{"cost_credits": total, "budget_reserved": 0, "version": gorm.Expr("version + 1")}).Error; err != nil {
		return err
	}
	_, err = r.ReconcileReuseOperationHold(ctx, task.UserID, task.ReuseRunID)
	return err
}

// Reconciliation removes only the operation hold. A resolved run stays paused
// until its owner explicitly resumes it; unknown outcomes never permit replay.
func (r *contentAssetRepository) ReconcileReuseOperationHold(ctx context.Context, userID, runID string) (bool, error) {
	resolved := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var policy model.ContentAutopilotPolicy
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&policy).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var run model.ContentReuseRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", runID, userID).First(&run).Error; err != nil {
			return err
		}
		if run.Status == model.ContentReuseRunStatusCancelled || run.Status == model.ContentReuseRunStatusCompleted {
			return nil
		}
		if run.ErrorCode != "operation_reconciliation_required" {
			resolved = true
			return nil
		}
		var pending int64
		if err := tx.Model(&model.ContentReuseOperationBudget{}).Where("run_id = ?", run.ID).Count(&pending).Error; err != nil || pending != 0 {
			return err
		}
		var taskIDs []string
		if err := tx.Model(&model.Task{}).Where("user_id = ? AND (reuse_run_id = ? OR id = ?)", userID, run.ID, run.TaskID).Pluck("id", &taskIDs).Error; err != nil {
			return err
		}
		charges, err := (&billingRepository{db: tx}).ListChargesByTaskIDs(ctx, userID, taskIDs)
		if err != nil {
			return err
		}
		reversed := map[string]bool{}
		for _, charge := range charges {
			if charge.ReversalOfID != nil {
				reversed[*charge.ReversalOfID] = true
			}
		}
		hasOperation := false
		for _, charge := range charges {
			if charge.Kind != model.BillingChargeKindOperation {
				continue
			}
			hasOperation = true
			if reversed[charge.ID] {
				continue
			}
			ok, err := reuseOperationHasDurableResult(ctx, tx, userID, taskIDs, &charge)
			if err != nil || !ok {
				return err
			}
		}
		if !hasOperation {
			return nil
		}
		result := tx.Model(&model.ContentReuseRun{}).Where("id = ? AND user_id = ? AND version = ? AND status = ? AND error_code = ?", run.ID, userID, run.Version, model.ContentReuseRunStatusPaused, "operation_reconciliation_required").Updates(map[string]any{"error_code": "", "error_message": "", "version": gorm.Expr("version + 1")})
		resolved = result.RowsAffected == 1
		return result.Error
	})
	return resolved, err
}

func reuseOperationHasDurableResult(ctx context.Context, db *gorm.DB, userID string, taskIDs []string, charge *model.BillingCharge) (bool, error) {
	switch charge.ResourceType {
	case "search", "fetch":
		var operation model.SearchOperation
		err := db.WithContext(ctx).Where("user_id = ? AND task_id IN ? AND request_fingerprint = ? AND status = ?", userID, taskIDs, charge.RequestFingerprint, model.SearchOperationSucceeded).First(&operation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return err == nil && json.Valid(operation.ResponseSnapshot) && string(operation.ResponseSnapshot) != "null", err
	case "image":
		var settlement model.BillingSettlementOutbox
		err := db.WithContext(ctx).Where("id = ? AND task_id IN ? AND resource_type = ? AND resource_id = ? AND request_fingerprint = ? AND action = ?", charge.IdempotencyKey, taskIDs, "image", charge.ResourceID, charge.RequestFingerprint, model.BillingSettlementActionChargeOperation).First(&settlement).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var snapshot struct {
			Object struct {
				StorageProvider string `json:"storage_provider"`
				OSSKey          string `json:"oss_key"`
				ContentHash     string `json:"content_hash"`
				FileSize        int64  `json:"file_size"`
			} `json:"operation_object"`
		}
		if json.Unmarshal(settlement.ResultSnapshot, &snapshot) != nil {
			return false, nil
		}
		return snapshot.Object.StorageProvider != "" && snapshot.Object.OSSKey != "" && snapshot.Object.ContentHash != "" && snapshot.Object.FileSize > 0, nil
	default:
		return false, nil
	}
}

func (r *contentAssetRepository) ReuseRunWalletCost(ctx context.Context, run *model.ContentReuseRun) (int64, error) {
	total, _, err := reuseRunWalletCost(ctx, r.db, run)
	return total, err
}
