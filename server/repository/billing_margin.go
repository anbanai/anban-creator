package repository

import (
	"context"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBillingMarginConflict = errors.New("billing margin fact conflict")

type BillingMarginRepository interface {
	AppendFact(ctx context.Context, fact *model.BillingMarginFact) (*model.BillingMarginFact, error)
	FindFactBySource(ctx context.Context, sourceKind, sourceID string) (*model.BillingMarginFact, error)
	ListFacts(ctx context.Context, from, to time.Time) ([]model.BillingMarginFact, error)
	ListUnprojectedCharges(ctx context.Context) ([]model.BillingCharge, error)
	CountPostedCharges(ctx context.Context) (int64, error)
	FindCharge(ctx context.Context, id string) (*model.BillingCharge, error)
	ListUnprojectedProviderCostEvents(ctx context.Context) ([]model.BillingProviderCostEvent, error)
	CountProviderCostEvents(ctx context.Context) (int64, error)
	FindProviderCostEvent(ctx context.Context, id string) (*model.BillingProviderCostEvent, error)
	ListUnprojectedTopUpEntries(ctx context.Context) ([]model.BillingWalletEntry, error)
	CountTopUpEntries(ctx context.Context) (int64, error)
	FindTopUpEntry(ctx context.Context, id string) (*model.BillingWalletEntry, error)
	SumDebtRepaymentForTopUp(ctx context.Context, sourceEntryID string) (int64, error)
	CountUnreconciledExecutionStatuses(ctx context.Context) (int64, error)
	CountUnsettledOutbox(ctx context.Context) (int64, error)
}

type billingMarginRepository struct {
	db *gorm.DB
}

func NewBillingMarginRepository(db *gorm.DB) BillingMarginRepository {
	return &billingMarginRepository{db: db}
}

func (r *billingMarginRepository) AppendFact(ctx context.Context, fact *model.BillingMarginFact) (*model.BillingMarginFact, error) {
	if fact == nil {
		return nil, errors.New("billing margin fact is required")
	}
	if err := fact.Validate(); err != nil {
		return nil, err
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_kind"}, {Name: "source_id"}},
		DoNothing: true,
	}).Create(fact)
	if result.Error != nil {
		return nil, result.Error
	}
	persisted, err := r.FindFactBySource(ctx, fact.SourceKind, fact.SourceID)
	if err != nil {
		return nil, err
	}
	if persisted.SourceFingerprint != fact.SourceFingerprint {
		return nil, ErrBillingMarginConflict
	}
	return persisted, nil
}

func (r *billingMarginRepository) FindFactBySource(ctx context.Context, sourceKind, sourceID string) (*model.BillingMarginFact, error) {
	var fact model.BillingMarginFact
	if err := r.db.WithContext(ctx).Where("source_kind = ? AND source_id = ?", sourceKind, sourceID).First(&fact).Error; err != nil {
		return nil, err
	}
	return &fact, nil
}

func (r *billingMarginRepository) ListFacts(ctx context.Context, from, to time.Time) ([]model.BillingMarginFact, error) {
	query := r.db.WithContext(ctx).Order("occurred_at ASC, id ASC")
	if !from.IsZero() {
		query = query.Where("occurred_at >= ?", from)
	}
	if !to.IsZero() {
		query = query.Where("occurred_at < ?", to)
	}
	var facts []model.BillingMarginFact
	return facts, query.Find(&facts).Error
}

func (r *billingMarginRepository) ListUnprojectedCharges(ctx context.Context) ([]model.BillingCharge, error) {
	var charges []model.BillingCharge
	return charges, r.db.WithContext(ctx).
		Where("status = ?", model.BillingChargeStatusPosted).
		Where("NOT EXISTS (SELECT 1 FROM billing_margin_facts AS facts WHERE facts.source_kind = ? AND facts.source_id = billing_charges.id)", "charge").
		Order("created_at ASC, id ASC").Find(&charges).Error
}

func (r *billingMarginRepository) CountPostedCharges(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BillingCharge{}).Where("status = ?", model.BillingChargeStatusPosted).Count(&count).Error
	return count, err
}

func (r *billingMarginRepository) FindCharge(ctx context.Context, id string) (*model.BillingCharge, error) {
	var charge model.BillingCharge
	if err := r.db.WithContext(ctx).First(&charge, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &charge, nil
}

func (r *billingMarginRepository) ListUnprojectedProviderCostEvents(ctx context.Context) ([]model.BillingProviderCostEvent, error) {
	var events []model.BillingProviderCostEvent
	return events, r.db.WithContext(ctx).
		Where("NOT EXISTS (SELECT 1 FROM billing_margin_facts AS facts WHERE facts.source_kind = ? AND facts.source_id = billing_provider_cost_events.id)", "provider_cost_event").
		Order("created_at ASC, id ASC").Find(&events).Error
}

func (r *billingMarginRepository) CountProviderCostEvents(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BillingProviderCostEvent{}).Count(&count).Error
	return count, err
}

func (r *billingMarginRepository) FindProviderCostEvent(ctx context.Context, id string) (*model.BillingProviderCostEvent, error) {
	var event model.BillingProviderCostEvent
	if err := r.db.WithContext(ctx).First(&event, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *billingMarginRepository) ListUnprojectedTopUpEntries(ctx context.Context) ([]model.BillingWalletEntry, error) {
	var entries []model.BillingWalletEntry
	err := r.db.WithContext(ctx).
		Where("event_kind = ?", model.BillingWalletEventKindTopUp).
		Where("NOT EXISTS (SELECT 1 FROM billing_margin_facts AS facts WHERE facts.source_kind = ? AND facts.source_id = billing_wallet_entries.id)", "topup_entry").
		Order("created_at ASC, id ASC").
		Find(&entries).Error
	return entries, err
}

func (r *billingMarginRepository) CountTopUpEntries(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BillingWalletEntry{}).
		Where("event_kind = ?", model.BillingWalletEventKindTopUp).Count(&count).Error
	return count, err
}

func (r *billingMarginRepository) FindTopUpEntry(ctx context.Context, id string) (*model.BillingWalletEntry, error) {
	var entry model.BillingWalletEntry
	err := r.db.WithContext(ctx).
		Where("id = ? AND event_kind = ?", id, model.BillingWalletEventKindTopUp).
		First(&entry).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *billingMarginRepository) SumDebtRepaymentForTopUp(ctx context.Context, sourceEntryID string) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).
		Model(&model.BillingDebtAllocation{}).
		Select("COALESCE(SUM(credits), 0)").
		Where("source_entry_id = ?", sourceEntryID).
		Scan(&total).Error
	return total, err
}

func (r *billingMarginRepository) CountUnreconciledExecutionStatuses(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BillingExecutionCostStatus{}).
		Where("status = ?", model.BillingProviderCostStatusUnreconciled).Count(&count).Error
	return count, err
}

func (r *billingMarginRepository) CountUnsettledOutbox(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.BillingSettlementOutbox{}).
		Where("status <> ?", "processed").Count(&count).Error
	return count, err
}
