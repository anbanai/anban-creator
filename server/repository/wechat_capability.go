package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type wechatCapabilityRepository struct{ db *gorm.DB }

func newWechatCapabilityRepository(db *gorm.DB) WechatCapabilityRepository {
	return &wechatCapabilityRepository{db: db}
}

func (r *wechatCapabilityRepository) ListByProject(ctx context.Context, projectID string) ([]*model.WechatAccountCapability, error) {
	var items []*model.WechatAccountCapability
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("capability ASC").Find(&items).Error
	return items, err
}

func (r *wechatCapabilityRepository) Upsert(ctx context.Context, capability *model.WechatAccountCapability) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "capability"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "last_checked_at", "last_wechat_code", "updated_at"}),
	}).Create(capability).Error
}
