package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"

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

func (r *planRepository) FindByUserID(ctx context.Context, userID string, projectID string, offset, limit int) ([]*model.Plan, error) {
	var plans []*model.Plan
	q := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC")
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
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

func (r *planRepository) UpdateIfReferenceImageAssetID(ctx context.Context, plan *model.Plan, expectedID string) (bool, error) {
	query := r.db.WithContext(ctx).
		Model(&model.Plan{}).
		Where("id = ?", plan.ID)
	if expectedID == "" {
		query = query.Where("(reference_image_asset_id = ? OR reference_image_asset_id IS NULL)", "")
	} else {
		query = query.Where("reference_image_asset_id = ?", expectedID)
	}
	result := query.
		Select("*").
		Updates(plan)
	return result.RowsAffected == 1, result.Error
}

func (r *planRepository) UpdateNextRunAtIf(ctx context.Context, id string, nextRunAt, expectedNextRunAt *time.Time) (bool, error) {
	query := r.db.WithContext(ctx).
		Model(&model.Plan{}).
		Where("id = ?", id)
	if expectedNextRunAt == nil {
		query = query.Where("next_run_at IS NULL")
	} else {
		query = query.Where("next_run_at = ?", *expectedNextRunAt)
	}
	result := query.Updates(map[string]interface{}{
		"next_run_at": nextRunAt,
		"updated_at":  time.Now(),
	})
	return result.RowsAffected == 1, result.Error
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

func (r *planRepository) ListActiveByUserID(ctx context.Context, userID string, projectID string) ([]*model.Plan, error) {
	var plans []*model.Plan
	q := r.db.WithContext(ctx).Where("user_id = ? AND status = ?", userID, model.PlanStatusActive)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err := q.Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *planRepository) CountByUserID(ctx context.Context, userID string, projectID string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.Plan{}).Where("user_id = ?", userID)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ListDue returns all active plans whose next_run_at is at or before the given time.
func (r *planRepository) ListDue(ctx context.Context, now time.Time) ([]*model.Plan, error) {
	var plans []*model.Plan
	if err := r.db.WithContext(ctx).
		Where("status = ? AND next_run_at IS NOT NULL AND next_run_at <= ?",
			model.PlanStatusActive, now).
		Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}
