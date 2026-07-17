package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	billingSettlementStatusPending    = "pending"
	billingSettlementStatusProcessing = "processing"
	billingSettlementStatusRetry      = "retry"
	billingSettlementStatusProcessed  = "processed"
	billingSettlementClaimLease       = 5 * time.Minute
)

// ErrBillingClaimRequiresTransaction means a MySQL settlement claim was
// attempted outside Repository.WithTx.
var ErrBillingClaimRequiresTransaction = errors.New("mysql billing settlement claim requires caller transaction")

// BillingRepository provides transaction-aware persistence for the fixed-price
// wallet. Balance and overdraft decisions belong to the billing service.
type BillingRepository interface {
	FindAccount(ctx context.Context, userID string) (*model.BillingWalletAccount, error)
	LockAccount(ctx context.Context, userID string) (*model.BillingWalletAccount, error)
	CreateAccount(ctx context.Context, account *model.BillingWalletAccount) error
	UpdateAccount(ctx context.Context, account *model.BillingWalletAccount) error

	ListSpendableLots(ctx context.Context, userID, kind string, now time.Time) ([]model.BillingCreditLot, error)
	LockLotByID(ctx context.Context, lotID string) (*model.BillingCreditLot, error)
	CreateLot(ctx context.Context, lot *model.BillingCreditLot) error
	UpdateLot(ctx context.Context, lot *model.BillingCreditLot) error
	FindLotBySource(ctx context.Context, sourceType, sourceID string) (*model.BillingCreditLot, error)

	AppendEntry(ctx context.Context, entry *model.BillingWalletEntry) error
	FindEntryByKey(ctx context.Context, scope, key string) (*model.BillingWalletEntry, error)
	FindEntryBySource(ctx context.Context, sourceType, sourceID string) (*model.BillingWalletEntry, error)
	ListEntriesByUser(ctx context.Context, userID string, offset, limit int) ([]model.BillingWalletEntry, error)

	CreateCatalogVersion(ctx context.Context, catalog *model.BillingCatalogVersion) error
	FindCatalogVersion(ctx context.Context, catalogID string) (*model.BillingCatalogVersion, error)
	FindLatestPublishedCatalog(ctx context.Context) (*model.BillingCatalogVersion, error)
	CreateSKUs(ctx context.Context, skus []model.BillingSKU) error
	FindSKU(ctx context.Context, catalogID, skuID string) (*model.BillingSKU, error)
	FindSKUByOperation(ctx context.Context, catalogID, operation, route string) (*model.BillingSKU, error)

	CreateQuote(ctx context.Context, quote *model.BillingQuote) error
	FindQuoteByKey(ctx context.Context, scope, key string) (*model.BillingQuote, error)
	LockQuote(ctx context.Context, quoteID string) (*model.BillingQuote, error)
	MarkQuoteConsumed(ctx context.Context, quoteID string, consumedAt time.Time, resourceType, resourceID string) (bool, error)

	CreateCharge(ctx context.Context, charge *model.BillingCharge, allocations []model.BillingChargeAllocation) error
	ListChargeAllocations(ctx context.Context, chargeID string) ([]model.BillingChargeAllocation, error)
	FindChargeByID(ctx context.Context, chargeID string) (*model.BillingCharge, error)
	FindChargeByKey(ctx context.Context, scope, key string) (*model.BillingCharge, error)
	FindChargeByTask(ctx context.Context, taskID string) (*model.BillingCharge, error)
	FindChargeByOperation(ctx context.Context, taskID, attemptID, toolCallID, catalogID, skuID string) (*model.BillingCharge, error)
	FindReversal(ctx context.Context, originalChargeID string) (*model.BillingCharge, error)

	EnqueueSettlement(ctx context.Context, settlement *model.BillingSettlementOutbox) error
	FindSettlementByKey(ctx context.Context, scope, key string) (*model.BillingSettlementOutbox, error)
	// ClaimSettlements must be called on txRepo.Billing() for MySQL. SQLite uses
	// an atomic UPDATE ... RETURNING statement and can also run on the root repo.
	ClaimSettlements(ctx context.Context, now time.Time, limit int) ([]model.BillingSettlementOutbox, error)
	MarkSettlementProcessed(ctx context.Context, settlementID string, attempts int, processedAt time.Time) error
	MarkSettlementRetry(ctx context.Context, settlementID string, attempts int, nextAttemptAt time.Time, lastError string) error

	CreateReferralIssue(ctx context.Context, issue *model.BillingReferralIssue) error
	FindReferralIssue(ctx context.Context, inviteeUserID, programID string) (*model.BillingReferralIssue, error)
	UpdateReferralIssue(ctx context.Context, issue *model.BillingReferralIssue) error
	CountIssuedReferrals(ctx context.Context, inviterUserID, programID string) (int64, error)
}

type billingRepository struct {
	db               *gorm.DB
	transactionBound bool
}

func newBillingRepository(db *gorm.DB) BillingRepository {
	return &billingRepository{db: db}
}

func newTxBillingRepository(db *gorm.DB) BillingRepository {
	return &billingRepository{db: db, transactionBound: true}
}

func (r *billingRepository) FindAccount(ctx context.Context, userID string) (*model.BillingWalletAccount, error) {
	var account model.BillingWalletAccount
	if err := r.db.WithContext(ctx).First(&account, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *billingRepository) LockAccount(ctx context.Context, userID string) (*model.BillingWalletAccount, error) {
	var account model.BillingWalletAccount
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&account, "user_id = ?", userID).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *billingRepository) CreateAccount(ctx context.Context, account *model.BillingWalletAccount) error {
	return r.db.WithContext(ctx).Create(account).Error
}

func (r *billingRepository) UpdateAccount(ctx context.Context, account *model.BillingWalletAccount) error {
	return r.db.WithContext(ctx).
		Model(&model.BillingWalletAccount{}).
		Where("user_id = ?", account.UserID).
		Updates(map[string]any{
			"paid_credits":        account.PaidCredits,
			"promotional_credits": account.PromotionalCredits,
			"debt_credits":        account.DebtCredits,
			"version":             account.Version,
			"updated_at":          account.UpdatedAt,
		}).Error
}

func (r *billingRepository) ListSpendableLots(ctx context.Context, userID, kind string, now time.Time) ([]model.BillingCreditLot, error) {
	var lots []model.BillingCreditLot
	query := r.db.WithContext(ctx).
		Where("user_id = ? AND kind = ? AND available_credits > 0", userID, kind).
		Where("expires_at IS NULL OR expires_at > ?", now)
	if kind == string(model.BillingCreditLotKindPromotional) {
		query = query.Order("expires_at ASC, created_at ASC, id ASC")
	} else {
		query = query.Order("created_at ASC, id ASC")
	}
	err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Find(&lots).Error
	return lots, err
}

func (r *billingRepository) LockLotByID(ctx context.Context, lotID string) (*model.BillingCreditLot, error) {
	var lot model.BillingCreditLot
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&lot, "id = ?", lotID).Error
	if err != nil {
		return nil, err
	}
	return &lot, nil
}

func (r *billingRepository) CreateLot(ctx context.Context, lot *model.BillingCreditLot) error {
	return r.db.WithContext(ctx).Create(lot).Error
}

func (r *billingRepository) UpdateLot(ctx context.Context, lot *model.BillingCreditLot) error {
	return r.db.WithContext(ctx).
		Model(&model.BillingCreditLot{}).
		Where("id = ?", lot.ID).
		Updates(map[string]any{
			"available_credits": lot.AvailableCredits,
			"consumed_credits":  lot.ConsumedCredits,
			"expired_credits":   lot.ExpiredCredits,
		}).Error
}

func (r *billingRepository) FindLotBySource(ctx context.Context, sourceType, sourceID string) (*model.BillingCreditLot, error) {
	var lot model.BillingCreditLot
	if err := r.db.WithContext(ctx).Where("source_type = ? AND source_id = ?", sourceType, sourceID).First(&lot).Error; err != nil {
		return nil, err
	}
	return &lot, nil
}

func (r *billingRepository) AppendEntry(ctx context.Context, entry *model.BillingWalletEntry) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

func (r *billingRepository) FindEntryByKey(ctx context.Context, scope, key string) (*model.BillingWalletEntry, error) {
	var entry model.BillingWalletEntry
	if err := r.db.WithContext(ctx).Where("idempotency_scope = ? AND idempotency_key = ?", scope, key).First(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *billingRepository) FindEntryBySource(ctx context.Context, sourceType, sourceID string) (*model.BillingWalletEntry, error) {
	var entry model.BillingWalletEntry
	if err := r.db.WithContext(ctx).Where("source_type = ? AND source_id = ?", sourceType, sourceID).First(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *billingRepository) ListEntriesByUser(ctx context.Context, userID string, offset, limit int) ([]model.BillingWalletEntry, error) {
	var entries []model.BillingWalletEntry
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(limit).
		Find(&entries).Error
	return entries, err
}

func (r *billingRepository) CreateCatalogVersion(ctx context.Context, catalog *model.BillingCatalogVersion) error {
	return r.db.WithContext(ctx).Create(catalog).Error
}

func (r *billingRepository) FindCatalogVersion(ctx context.Context, catalogID string) (*model.BillingCatalogVersion, error) {
	var catalog model.BillingCatalogVersion
	if err := r.db.WithContext(ctx).First(&catalog, "catalog_id = ?", catalogID).Error; err != nil {
		return nil, err
	}
	return &catalog, nil
}

func (r *billingRepository) FindLatestPublishedCatalog(ctx context.Context) (*model.BillingCatalogVersion, error) {
	var catalog model.BillingCatalogVersion
	err := r.db.WithContext(ctx).
		Where("status = ?", "published").
		Order("published_at DESC, catalog_id DESC").
		First(&catalog).Error
	if err != nil {
		return nil, err
	}
	return &catalog, nil
}

func (r *billingRepository) CreateSKUs(ctx context.Context, skus []model.BillingSKU) error {
	if len(skus) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&skus).Error
}

func (r *billingRepository) FindSKU(ctx context.Context, catalogID, skuID string) (*model.BillingSKU, error) {
	var sku model.BillingSKU
	if err := r.db.WithContext(ctx).
		Where(&model.BillingSKU{CatalogID: catalogID, SKUID: skuID}).
		First(&sku).Error; err != nil {
		return nil, err
	}
	return &sku, nil
}

func (r *billingRepository) FindSKUByOperation(ctx context.Context, catalogID, operation, route string) (*model.BillingSKU, error) {
	var sku model.BillingSKU
	err := r.db.WithContext(ctx).
		Where("catalog_id = ? AND operation = ? AND route = ?", catalogID, operation, route).
		First(&sku).Error
	if err != nil {
		return nil, err
	}
	return &sku, nil
}

func (r *billingRepository) CreateQuote(ctx context.Context, quote *model.BillingQuote) error {
	return r.db.WithContext(ctx).Create(quote).Error
}

func (r *billingRepository) FindQuoteByKey(ctx context.Context, scope, key string) (*model.BillingQuote, error) {
	var quote model.BillingQuote
	if err := r.db.WithContext(ctx).Where("idempotency_scope = ? AND idempotency_key = ?", scope, key).First(&quote).Error; err != nil {
		return nil, err
	}
	return &quote, nil
}

func (r *billingRepository) LockQuote(ctx context.Context, quoteID string) (*model.BillingQuote, error) {
	var quote model.BillingQuote
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&quote, "id = ?", quoteID).Error
	if err != nil {
		return nil, err
	}
	return &quote, nil
}

func (r *billingRepository) MarkQuoteConsumed(ctx context.Context, quoteID string, consumedAt time.Time, resourceType, resourceID string) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.BillingQuote{}).
		Where("id = ? AND consumed_at IS NULL", quoteID).
		Updates(map[string]any{
			"consumed_at":   consumedAt,
			"resource_type": resourceType,
			"resource_id":   resourceID,
		})
	return result.RowsAffected == 1, result.Error
}

// CreateCharge deliberately reuses r.db. Callers wrap it in Repository.WithTx
// so the charge and allocations participate in the existing wallet transaction.
func (r *billingRepository) CreateCharge(ctx context.Context, charge *model.BillingCharge, allocations []model.BillingChargeAllocation) error {
	if err := r.db.WithContext(ctx).Create(charge).Error; err != nil {
		return err
	}
	if len(allocations) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&allocations).Error
}

func (r *billingRepository) ListChargeAllocations(ctx context.Context, chargeID string) ([]model.BillingChargeAllocation, error) {
	var allocations []model.BillingChargeAllocation
	err := r.db.WithContext(ctx).
		Where("charge_id = ?", chargeID).
		Order("id ASC").
		Find(&allocations).Error
	return allocations, err
}

func (r *billingRepository) FindChargeByID(ctx context.Context, chargeID string) (*model.BillingCharge, error) {
	var charge model.BillingCharge
	if err := r.db.WithContext(ctx).First(&charge, "id = ?", chargeID).Error; err != nil {
		return nil, err
	}
	return &charge, nil
}

func (r *billingRepository) FindChargeByKey(ctx context.Context, scope, key string) (*model.BillingCharge, error) {
	var charge model.BillingCharge
	if err := r.db.WithContext(ctx).Where("idempotency_scope = ? AND idempotency_key = ?", scope, key).First(&charge).Error; err != nil {
		return nil, err
	}
	return &charge, nil
}

func (r *billingRepository) FindChargeByTask(ctx context.Context, taskID string) (*model.BillingCharge, error) {
	var charge model.BillingCharge
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND charge_kind = ?", taskID, model.BillingChargeKindTask).
		First(&charge).Error
	if err != nil {
		return nil, err
	}
	return &charge, nil
}

func (r *billingRepository) FindChargeByOperation(ctx context.Context, taskID, attemptID, toolCallID, catalogID, skuID string) (*model.BillingCharge, error) {
	var charge model.BillingCharge
	err := r.db.WithContext(ctx).
		Where("operation_task_id = ? AND attempt_id = ? AND tool_call_id = ? AND catalog_id = ? AND sku_id = ?", taskID, attemptID, toolCallID, catalogID, skuID).
		First(&charge).Error
	if err != nil {
		return nil, err
	}
	return &charge, nil
}

func (r *billingRepository) FindReversal(ctx context.Context, originalChargeID string) (*model.BillingCharge, error) {
	var charge model.BillingCharge
	err := r.db.WithContext(ctx).
		Where("reversal_of_id = ? AND charge_kind = ?", originalChargeID, model.BillingChargeKindReversal).
		First(&charge).Error
	if err != nil {
		return nil, err
	}
	return &charge, nil
}

func (r *billingRepository) EnqueueSettlement(ctx context.Context, settlement *model.BillingSettlementOutbox) error {
	return r.db.WithContext(ctx).Create(settlement).Error
}

func (r *billingRepository) FindSettlementByKey(ctx context.Context, scope, key string) (*model.BillingSettlementOutbox, error) {
	var settlement model.BillingSettlementOutbox
	err := r.db.WithContext(ctx).
		Where("idempotency_scope = ? AND idempotency_key = ?", scope, key).
		First(&settlement).Error
	if err != nil {
		return nil, err
	}
	return &settlement, nil
}

func (r *billingRepository) ClaimSettlements(ctx context.Context, now time.Time, limit int) ([]model.BillingSettlementOutbox, error) {
	if limit <= 0 {
		return []model.BillingSettlementOutbox{}, nil
	}
	dialect := r.db.Dialector.Name()
	if dialect != "mysql" && dialect != "sqlite" {
		return nil, errors.New("billing settlement claims require mysql or sqlite")
	}
	if dialect == "sqlite" {
		return r.claimSQLiteSettlements(ctx, now, limit)
	}
	if !r.transactionBound {
		return nil, ErrBillingClaimRequiresTransaction
	}

	claim := func(tx *gorm.DB) ([]model.BillingSettlementOutbox, error) {
		var candidates []model.BillingSettlementOutbox
		staleBefore := now.Add(-billingSettlementClaimLease)
		query := tx.WithContext(ctx).
			Where("(status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at <= ?)",
				[]string{billingSettlementStatusPending, billingSettlementStatusRetry}, now,
				billingSettlementStatusProcessing, staleBefore).
			Order("created_at ASC, id ASC").
			Limit(limit)
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		if err := query.Find(&candidates).Error; err != nil {
			return nil, err
		}

		claimed := make([]model.BillingSettlementOutbox, 0, len(candidates))
		for i := range candidates {
			result := tx.WithContext(ctx).
				Model(&model.BillingSettlementOutbox{}).
				Where("id = ?", candidates[i].ID).
				Where("(status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at <= ?)",
					[]string{billingSettlementStatusPending, billingSettlementStatusRetry}, now,
					billingSettlementStatusProcessing, staleBefore).
				Updates(map[string]any{
					"status":          billingSettlementStatusProcessing,
					"attempts":        gorm.Expr("attempts + 1"),
					"next_attempt_at": nil,
					"last_error":      "",
					"processed_at":    nil,
					"updated_at":      now,
				})
			if result.Error != nil {
				return nil, result.Error
			}
			if result.RowsAffected != 1 {
				continue
			}
			candidates[i].Status = billingSettlementStatusProcessing
			candidates[i].Attempts++
			candidates[i].NextAttemptAt = nil
			candidates[i].LastError = ""
			candidates[i].ProcessedAt = nil
			candidates[i].UpdatedAt = now
			claimed = append(claimed, candidates[i])
		}
		return claimed, nil
	}

	// MySQL row locks are meaningful only inside the caller's transaction. This
	// repository is transaction-bound by Repository.WithTx and must not create a
	// nested transaction/savepoint of its own.
	return claim(r.db)
}

// SQLite has no row-level FOR UPDATE/SKIP LOCKED. A single UPDATE ... RETURNING
// statement makes selection and state transition one atomic writer operation;
// concurrent claimers reevaluate the due-state predicate after SQLite serializes
// them, so a row cannot be returned twice.
func (r *billingRepository) claimSQLiteSettlements(ctx context.Context, now time.Time, limit int) ([]model.BillingSettlementOutbox, error) {
	staleBefore := now.Add(-billingSettlementClaimLease)
	var claimed []model.BillingSettlementOutbox
	err := r.db.WithContext(ctx).Raw(`
UPDATE billing_settlement_outbox
SET status = ?,
    attempts = attempts + 1,
    next_attempt_at = NULL,
    last_error = '',
    processed_at = NULL,
    updated_at = ?
WHERE id IN (
    SELECT id
    FROM billing_settlement_outbox
    WHERE (status IN (?, ?) AND (next_attempt_at IS NULL OR next_attempt_at <= ?))
       OR (status = ? AND updated_at <= ?)
    ORDER BY created_at ASC, id ASC
    LIMIT ?
)
RETURNING *`,
		billingSettlementStatusProcessing, now,
		billingSettlementStatusPending, billingSettlementStatusRetry, now,
		billingSettlementStatusProcessing, staleBefore, limit,
	).Scan(&claimed).Error
	if err != nil {
		return nil, err
	}
	sort.Slice(claimed, func(i, j int) bool {
		if claimed[i].CreatedAt.Equal(claimed[j].CreatedAt) {
			return claimed[i].ID < claimed[j].ID
		}
		return claimed[i].CreatedAt.Before(claimed[j].CreatedAt)
	})
	return claimed, nil
}

func (r *billingRepository) MarkSettlementProcessed(ctx context.Context, settlementID string, attempts int, processedAt time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&model.BillingSettlementOutbox{}).
		Where("id = ? AND status = ? AND attempts = ?", settlementID, billingSettlementStatusProcessing, attempts).
		Updates(map[string]any{
			"status":          billingSettlementStatusProcessed,
			"processed_at":    processedAt,
			"next_attempt_at": nil,
			"last_error":      "",
		})
	return billingRequireOneRow(result)
}

func (r *billingRepository) MarkSettlementRetry(ctx context.Context, settlementID string, attempts int, nextAttemptAt time.Time, lastError string) error {
	result := r.db.WithContext(ctx).
		Model(&model.BillingSettlementOutbox{}).
		Where("id = ? AND status = ? AND attempts = ?", settlementID, billingSettlementStatusProcessing, attempts).
		Updates(map[string]any{
			"status":          billingSettlementStatusRetry,
			"next_attempt_at": nextAttemptAt,
			"last_error":      lastError,
			"processed_at":    nil,
		})
	return billingRequireOneRow(result)
}

func billingRequireOneRow(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *billingRepository) CreateReferralIssue(ctx context.Context, issue *model.BillingReferralIssue) error {
	return r.db.WithContext(ctx).Create(issue).Error
}

func (r *billingRepository) FindReferralIssue(ctx context.Context, inviteeUserID, programID string) (*model.BillingReferralIssue, error) {
	var issue model.BillingReferralIssue
	err := r.db.WithContext(ctx).
		Where("invitee_user_id = ? AND program_id = ?", inviteeUserID, programID).
		First(&issue).Error
	if err != nil {
		return nil, err
	}
	return &issue, nil
}

func (r *billingRepository) UpdateReferralIssue(ctx context.Context, issue *model.BillingReferralIssue) error {
	return r.db.WithContext(ctx).
		Model(&model.BillingReferralIssue{}).
		Where("id = ?", issue.ID).
		Updates(map[string]any{
			"invitee_lot_id": issue.InviteeLotID,
			"inviter_lot_id": issue.InviterLotID,
			"status":         issue.Status,
			"issued_at":      issue.IssuedAt,
			"updated_at":     issue.UpdatedAt,
		}).Error
}

func (r *billingRepository) CountIssuedReferrals(ctx context.Context, inviterUserID, programID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.BillingReferralIssue{}).
		Where("inviter_user_id = ? AND program_id = ? AND status = ?", inviterUserID, programID, "issued").
		Count(&count).Error
	return count, err
}
