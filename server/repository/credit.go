package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
)

// CreditRepository provides access to the credit_transactions table.
type CreditRepository interface {
	CreateTransaction(ctx context.Context, tx *model.CreditTransaction) error
	FindTodaySignIn(ctx context.Context, userID string) (*model.CreditTransaction, error)
	FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.CreditTransaction, error)
	CountByUserID(ctx context.Context, userID string) (int64, error)
	FindDeductionByTaskID(ctx context.Context, taskID string) (*model.CreditTransaction, error)
	FindRefundByTaskID(ctx context.Context, taskID string) (*model.CreditTransaction, error)
	FindByTaskIDAndUserID(ctx context.Context, taskID, userID string) ([]*model.CreditTransaction, error)
	FindByOperationID(ctx context.Context, operationID string) (*model.CreditTransaction, error)
	FindDeductionByOperationID(ctx context.Context, operationID string) (*model.CreditTransaction, error)
	FindRefundByOperationID(ctx context.Context, operationID string) (*model.CreditTransaction, error)
}

type creditRepository struct {
	db *gorm.DB
}

func newCreditRepository(db *gorm.DB) CreditRepository {
	return &creditRepository{db: db}
}

func (r *creditRepository) CreateTransaction(ctx context.Context, tx *model.CreditTransaction) error {
	return r.db.WithContext(ctx).Create(tx).Error
}

func (r *creditRepository) FindTodaySignIn(ctx context.Context, userID string) (*model.CreditTransaction, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var tx model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND type = ? AND created_at >= ?", userID, model.CreditTypeSignIn, startOfDay).
		First(&tx).Error
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *creditRepository) FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.CreditTransaction, error) {
	var transactions []*model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&transactions).Error
	return transactions, err
}

func (r *creditRepository) CountByUserID(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.CreditTransaction{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
}

func (r *creditRepository) FindDeductionByTaskID(ctx context.Context, taskID string) (*model.CreditTransaction, error) {
	var tx model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND type = ?", taskID, model.CreditTypeTaskDeduct).
		First(&tx).Error
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *creditRepository) FindRefundByTaskID(ctx context.Context, taskID string) (*model.CreditTransaction, error) {
	var tx model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND type = ?", taskID, model.CreditTypeTaskRefund).
		First(&tx).Error
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *creditRepository) FindByTaskIDAndUserID(ctx context.Context, taskID, userID string) ([]*model.CreditTransaction, error) {
	var transactions []*model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND user_id = ?", taskID, userID).
		Order("created_at ASC, id ASC").
		Find(&transactions).Error
	return transactions, err
}

func (r *creditRepository) FindByOperationID(ctx context.Context, operationID string) (*model.CreditTransaction, error) {
	var tx model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("operation_id = ?", operationID).
		First(&tx).Error
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *creditRepository) FindDeductionByOperationID(ctx context.Context, operationID string) (*model.CreditTransaction, error) {
	var tx model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("operation_id = ? AND amount < 0", operationID).
		First(&tx).Error
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *creditRepository) FindRefundByOperationID(ctx context.Context, operationID string) (*model.CreditTransaction, error) {
	var tx model.CreditTransaction
	err := r.db.WithContext(ctx).
		Where("operation_id = ? AND amount > 0", operationID).
		First(&tx).Error
	if err != nil {
		return nil, err
	}
	return &tx, nil
}
