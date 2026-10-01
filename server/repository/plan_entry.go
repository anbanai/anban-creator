package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type planEntryRepository struct{ db *gorm.DB }

func newPlanEntryRepository(db *gorm.DB) PlanEntryRepository {
	return &planEntryRepository{db: db}
}

func (r *planEntryRepository) Create(ctx context.Context, entry *model.PlanEntry) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

func (r *planEntryRepository) FindByID(ctx context.Context, id string) (*model.PlanEntry, error) {
	var entry model.PlanEntry
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *planEntryRepository) FindByPlanIDAndAgentID(ctx context.Context, planID, agentID string) (*model.PlanEntry, error) {
	var entry model.PlanEntry
	if err := r.db.WithContext(ctx).Where("plan_id = ? AND agent_id = ?", planID, agentID).First(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *planEntryRepository) ListByPlanID(ctx context.Context, planID string) ([]*model.PlanEntry, error) {
	var entries []*model.PlanEntry
	if err := r.db.WithContext(ctx).Where("plan_id = ?", planID).Order("created_at ASC, id ASC").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *planEntryRepository) Update(ctx context.Context, entry *model.PlanEntry) error {
	return r.db.WithContext(ctx).Save(entry).Error
}

func (r *planEntryRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.PlanEntry{}).Error
}

func (r *planEntryRepository) DeleteByPlanID(ctx context.Context, planID string) error {
	return r.db.WithContext(ctx).Where("plan_id = ?", planID).Delete(&model.PlanEntry{}).Error
}
