package repository

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrProviderCostConflict      = errors.New("provider cost idempotency conflict")
	ErrProviderCostStatusMissing = errors.New("provider cost execution status missing after upsert")
)

// BillingCostRepository persists the internal provider-cost ledger. It has no
// wallet methods so cost accounting cannot mutate customer balances.
type BillingCostRepository interface {
	AppendEvent(ctx context.Context, event *model.BillingProviderCostEvent) (*model.BillingProviderCostEvent, error)
	AppendEventAndUpsertExecutionCostStatus(ctx context.Context, event *model.BillingProviderCostEvent, status *model.BillingExecutionCostStatus) (*model.BillingProviderCostEvent, error)
	AppendEventsAndUpsertExecutionCostStatus(ctx context.Context, events []*model.BillingProviderCostEvent, status *model.BillingExecutionCostStatus) ([]*model.BillingProviderCostEvent, error)
	FindEventByID(ctx context.Context, id string) (*model.BillingProviderCostEvent, error)
	FindEventByIdempotency(ctx context.Context, scope, key string) (*model.BillingProviderCostEvent, error)
	FindEventByBaseIdentity(ctx context.Context, baseIdentityKey string) (*model.BillingProviderCostEvent, error)
	ListEventsByExecution(ctx context.Context, executionID string) ([]model.BillingProviderCostEvent, error)
	UpsertExecutionCostStatus(ctx context.Context, status *model.BillingExecutionCostStatus) error
	FindExecutionCostStatus(ctx context.Context, executionID string) (*model.BillingExecutionCostStatus, error)
}

type billingCostRepository struct {
	db *gorm.DB
}

func NewBillingCostRepository(db *gorm.DB) BillingCostRepository {
	return &billingCostRepository{db: db}
}

func (r *billingCostRepository) AppendEvent(ctx context.Context, event *model.BillingProviderCostEvent) (*model.BillingProviderCostEvent, error) {
	return r.appendEvent(ctx, event)
}

func (r *billingCostRepository) AppendEventAndUpsertExecutionCostStatus(ctx context.Context, event *model.BillingProviderCostEvent, status *model.BillingExecutionCostStatus) (*model.BillingProviderCostEvent, error) {
	persisted, err := r.AppendEventsAndUpsertExecutionCostStatus(ctx, []*model.BillingProviderCostEvent{event}, status)
	if err != nil {
		return nil, err
	}
	return persisted[0], nil
}

func (r *billingCostRepository) AppendEventsAndUpsertExecutionCostStatus(ctx context.Context, events []*model.BillingProviderCostEvent, status *model.BillingExecutionCostStatus) ([]*model.BillingProviderCostEvent, error) {
	if len(events) == 0 {
		return nil, fmt.Errorf("execution cost finalization requires at least one event")
	}
	if status == nil || strings.TrimSpace(status.ExecutionID) == "" {
		return nil, fmt.Errorf("execution cost finalization requires execution status")
	}
	if err := validateExecutionCostStatus(status); err != nil {
		return nil, err
	}
	for _, event := range events {
		if event == nil || event.IdentityKind != model.BillingProviderCostIdentityExecutionModel {
			return nil, fmt.Errorf("execution cost status requires execution/model provider cost events")
		}
		if strings.TrimSpace(event.ExecutionID) == "" || strings.TrimSpace(event.ExecutionID) != strings.TrimSpace(status.ExecutionID) {
			return nil, fmt.Errorf("provider cost events and execution status identities must match")
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("validate execution provider cost event: %w", err)
		}
	}
	persisted := make([]*model.BillingProviderCostEvent, 0, len(events))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bound := &billingCostRepository{db: tx}
		for _, event := range events {
			stored, err := bound.appendEvent(ctx, event)
			if err != nil {
				return err
			}
			persisted = append(persisted, stored)
		}
		return bound.UpsertExecutionCostStatus(ctx, status)
	})
	if err != nil {
		return nil, err
	}
	return persisted, nil
}

func (r *billingCostRepository) appendEvent(ctx context.Context, event *model.BillingProviderCostEvent) (*model.BillingProviderCostEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("append provider cost event: event is required")
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("append provider cost event: %w", err)
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if result.Error != nil {
		return nil, fmt.Errorf("append provider cost event: %w", result.Error)
	}

	// Always read back by the domain identity. MySQL can report zero affected
	// rows for both a replay and a no-op insert, so RowsAffected is not semantic.
	persisted, err := r.FindEventByIdempotency(ctx, event.IdempotencyScope, event.IdempotencyKey)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if event.EventKind != model.BillingProviderCostEventKindBase || event.BaseIdentityKey == nil {
			return nil, fmt.Errorf("%w: insert collided outside the idempotency identity", ErrProviderCostConflict)
		}
		persisted, err = r.FindEventByBaseIdentity(ctx, *event.BaseIdentityKey)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("%w: insert collided outside the domain identity", ErrProviderCostConflict)
			}
			return nil, err
		}
	}
	if persisted.RequestFingerprint != event.RequestFingerprint {
		return nil, fmt.Errorf("%w: scope %q key %q", ErrProviderCostConflict, event.IdempotencyScope, event.IdempotencyKey)
	}
	return persisted, nil
}

func (r *billingCostRepository) FindEventByBaseIdentity(ctx context.Context, baseIdentityKey string) (*model.BillingProviderCostEvent, error) {
	var event model.BillingProviderCostEvent
	if err := r.db.WithContext(ctx).Where("base_identity_key = ?", baseIdentityKey).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *billingCostRepository) FindEventByID(ctx context.Context, id string) (*model.BillingProviderCostEvent, error) {
	var event model.BillingProviderCostEvent
	if err := r.db.WithContext(ctx).First(&event, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *billingCostRepository) FindEventByIdempotency(ctx context.Context, scope, key string) (*model.BillingProviderCostEvent, error) {
	var event model.BillingProviderCostEvent
	if err := r.db.WithContext(ctx).
		Where("idempotency_scope = ? AND idempotency_key = ?", scope, key).
		First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *billingCostRepository) ListEventsByExecution(ctx context.Context, executionID string) ([]model.BillingProviderCostEvent, error) {
	var events []model.BillingProviderCostEvent
	err := r.db.WithContext(ctx).
		Where("execution_id = ?", executionID).
		Order("created_at ASC, id ASC").
		Find(&events).Error
	return events, err
}

func (r *billingCostRepository) UpsertExecutionCostStatus(ctx context.Context, status *model.BillingExecutionCostStatus) error {
	if err := validateExecutionCostStatus(status); err != nil {
		return err
	}
	// Insert first so an unreconciled late arrival can never overwrite an
	// already-reconciled row through an upsert expression. The guarded update
	// is portable across SQLite and MySQL and also closes insert races.
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(status).Error; err != nil {
		return err
	}
	query := r.db.WithContext(ctx).Model(&model.BillingExecutionCostStatus{}).
		Where("execution_id = ?", status.ExecutionID)
	if status.Status == model.BillingProviderCostStatusReconciled {
		query = query.Where("status <> ? OR finalization_fingerprint = ?", model.BillingProviderCostStatusReconciled, status.FinalizationFingerprint)
	} else {
		query = query.Where("status <> ?", model.BillingProviderCostStatusReconciled)
	}
	if err := query.Updates(map[string]any{
		"task_id":                  gorm.Expr("CASE WHEN ? = '' THEN task_id ELSE ? END", status.TaskID, status.TaskID),
		"status":                   status.Status,
		"reason_code":              status.ReasonCode,
		"finalization_fingerprint": status.FinalizationFingerprint,
		"updated_at":               gorm.Expr("CURRENT_TIMESTAMP"),
	}).Error; err != nil {
		return err
	}
	persisted, err := r.findCurrentExecutionCostStatus(ctx, status.ExecutionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: execution %q", ErrProviderCostStatusMissing, status.ExecutionID)
		}
		return err
	}
	if status.Status == model.BillingProviderCostStatusReconciled && persisted.Status == model.BillingProviderCostStatusReconciled && persisted.FinalizationFingerprint != status.FinalizationFingerprint {
		return fmt.Errorf("%w: execution %q finalized with a different model set", ErrProviderCostConflict, status.ExecutionID)
	}
	return nil
}

func (r *billingCostRepository) findCurrentExecutionCostStatus(ctx context.Context, executionID string) (*model.BillingExecutionCostStatus, error) {
	var status model.BillingExecutionCostStatus
	query := r.db.WithContext(ctx)
	if r.db.Dialector.Name() == "mysql" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&status, "execution_id = ?", executionID).Error; err != nil {
		return nil, err
	}
	return &status, nil
}

func validateExecutionCostStatus(status *model.BillingExecutionCostStatus) error {
	if status == nil || strings.TrimSpace(status.ExecutionID) == "" {
		return fmt.Errorf("execution cost status requires execution_id")
	}
	switch status.Status {
	case model.BillingProviderCostStatusReconciled:
		if status.ReasonCode != "" {
			return fmt.Errorf("reconciled execution cost status cannot contain a reason code")
		}
		if len(status.FinalizationFingerprint) != 64 {
			return fmt.Errorf("reconciled execution cost status requires a 64-character finalization fingerprint")
		}
		if _, err := hex.DecodeString(status.FinalizationFingerprint); err != nil {
			return fmt.Errorf("reconciled execution cost finalization fingerprint must be hexadecimal")
		}
	case model.BillingProviderCostStatusUnreconciled:
		if !status.ReasonCode.Valid() {
			return fmt.Errorf("unreconciled execution cost status requires a supported reason code")
		}
		if status.FinalizationFingerprint != "" {
			return fmt.Errorf("unreconciled execution cost status cannot contain a finalization fingerprint")
		}
	default:
		return fmt.Errorf("unsupported execution cost status %q", status.Status)
	}
	return nil
}

func (r *billingCostRepository) FindExecutionCostStatus(ctx context.Context, executionID string) (*model.BillingExecutionCostStatus, error) {
	var status model.BillingExecutionCostStatus
	if err := r.db.WithContext(ctx).First(&status, "execution_id = ?", executionID).Error; err != nil {
		return nil, err
	}
	return &status, nil
}
