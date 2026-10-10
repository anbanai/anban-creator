package repository

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InteractiveCommitReceiptRepository interface {
	Create(ctx context.Context, receipt *model.InteractiveCommitReceipt) error
	Update(ctx context.Context, receipt *model.InteractiveCommitReceipt) error
	FindByUserAndKey(ctx context.Context, userID, key string) (*model.InteractiveCommitReceipt, error)
	FindByUserAndKeyForUpdate(ctx context.Context, userID, key string) (*model.InteractiveCommitReceipt, error)
}

type interactiveCommitReceiptRepository struct{ db *gorm.DB }

func newInteractiveCommitReceiptRepository(db *gorm.DB) InteractiveCommitReceiptRepository {
	return &interactiveCommitReceiptRepository{db: db}
}
func (r *interactiveCommitReceiptRepository) Create(ctx context.Context, v *model.InteractiveCommitReceipt) error {
	return r.db.WithContext(ctx).Create(v).Error
}
func (r *interactiveCommitReceiptRepository) Update(ctx context.Context, v *model.InteractiveCommitReceipt) error {
	return r.db.WithContext(ctx).Save(v).Error
}
func (r *interactiveCommitReceiptRepository) FindByUserAndKey(ctx context.Context, userID, key string) (*model.InteractiveCommitReceipt, error) {
	var v model.InteractiveCommitReceipt
	if err := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}
func (r *interactiveCommitReceiptRepository) FindByUserAndKeyForUpdate(ctx context.Context, userID, key string) (*model.InteractiveCommitReceipt, error) {
	var v model.InteractiveCommitReceipt
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}
