package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

type planRepository struct {
	db *gorm.DB
}

func newPlanRepository(db *gorm.DB) PlanRepository {
	return &planRepository{db: db}
}

func (r *planRepository) Create(ctx context.Context, plan *model.Plan) error {
	return r.db.WithContext(ctx).Create(plan).Error
}

func (r *planRepository) FindByID(ctx context.Context, id string) (*model.Plan, error) {
	var plan model.Plan
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *planRepository) FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.Plan, error) {
	var plans []*model.Plan
	q := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC")
	if limit > 0 {
		q = q.Offset(offset).Limit(limit)
	}
	if err := q.Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *planRepository) Update(ctx context.Context, plan *model.Plan) error {
	return r.db.WithContext(ctx).Save(plan).Error
}

func (r *planRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Plan{}).Error
}

func (r *planRepository) ListActive(ctx context.Context) ([]*model.Plan, error) {
	var plans []*model.Plan
	if err := r.db.WithContext(ctx).Where("status = ?", model.PlanStatusActive).Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *planRepository) ListActiveByUserID(ctx context.Context, userID string) ([]*model.Plan, error) {
	var plans []*model.Plan
	if err := r.db.WithContext(ctx).Where("user_id = ? AND status = ?", userID, model.PlanStatusActive).Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *planRepository) CountByUserID(ctx context.Context, userID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Plan{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
