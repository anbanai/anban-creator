package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProviderCostConflict = errors.New("provider cost idempotency conflict")

// BillingCostRepository persists the internal provider-cost ledger. It has no
// wallet methods so cost accounting cannot mutate customer balances.
type BillingCostRepository interface {
	AppendEvent(ctx context.Context, event *model.BillingProviderCostEvent) (*model.BillingProviderCostEvent, error)
	AppendEventAndUpsertExecutionCostStatus(ctx context.Context, event *model.BillingProviderCostEvent, status *model.BillingExecutionCostStatus) (*model.BillingProviderCostEvent, error)
	FindEventByID(ctx context.Context, id string) (*model.BillingProviderCostEvent, error)
	FindEventByIdempotency(ctx context.Context, scope, key string) (*model.BillingProviderCostEvent, error)
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
	if event == nil || event.IdentityKind != model.BillingProviderCostIdentityExecutionModel {
		return nil, fmt.Errorf("execution cost status requires an execution/model provider cost event")
	}
	if status == nil || strings.TrimSpace(event.ExecutionID) == "" || strings.TrimSpace(event.ExecutionID) != strings.TrimSpace(status.ExecutionID) {
		return nil, fmt.Errorf("provider cost event and execution status identities must match")
	}
	var persisted *model.BillingProviderCostEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bound := &billingCostRepository{db: tx}
		var err error
		persisted, err = bound.appendEvent(ctx, event)
		if err != nil {
			return err
		}
		return bound.UpsertExecutionCostStatus(ctx, status)
	})
	return persisted, err
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: insert collided outside the domain identity", ErrProviderCostConflict)
		}
		return nil, err
	}
	if persisted.RequestFingerprint != event.RequestFingerprint {
		return nil, fmt.Errorf("%w: scope %q key %q", ErrProviderCostConflict, event.IdempotencyScope, event.IdempotencyKey)
	}
	return persisted, nil
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
	if status == nil || strings.TrimSpace(status.ExecutionID) == "" {
		return fmt.Errorf("execution cost status requires execution_id")
	}
	switch status.Status {
	case model.BillingProviderCostStatusReconciled:
		if strings.TrimSpace(status.Reason) != "" {
			return fmt.Errorf("reconciled execution cost status cannot contain a reason")
		}
	case model.BillingProviderCostStatusUnreconciled:
		if strings.TrimSpace(status.Reason) == "" {
			return fmt.Errorf("unreconciled execution cost status requires a reason")
		}
	default:
		return fmt.Errorf("unsupported execution cost status %q", status.Status)
	}
	// Insert first so an unreconciled late arrival can never overwrite an
	// already-reconciled row through an upsert expression. The guarded update
	// is portable across SQLite and MySQL and also closes insert races.
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(status).Error; err != nil {
		return err
	}
	query := r.db.WithContext(ctx).Model(&model.BillingExecutionCostStatus{}).
		Where("execution_id = ?", status.ExecutionID)
	if status.Status == model.BillingProviderCostStatusUnreconciled {
		query = query.Where("status <> ?", model.BillingProviderCostStatusReconciled)
	}
	return query.Updates(map[string]any{
		"task_id":    gorm.Expr("CASE WHEN ? = '' THEN task_id ELSE ? END", status.TaskID, status.TaskID),
		"status":     status.Status,
		"reason":     status.Reason,
		"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
	}).Error
}

func (r *billingCostRepository) FindExecutionCostStatus(ctx context.Context, executionID string) (*model.BillingExecutionCostStatus, error) {
	var status model.BillingExecutionCostStatus
	if err := r.db.WithContext(ctx).First(&status, "execution_id = ?", executionID).Error; err != nil {
		return nil, err
	}
	return &status, nil
}
