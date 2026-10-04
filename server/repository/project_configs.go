package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// ProjectChannelConfigRepository persists project-scoped channel settings.
type ProjectChannelConfigRepository interface {
	Get(ctx context.Context, projectID, channel string) (*model.ProjectChannelConfig, error)
	List(ctx context.Context, projectID string) ([]*model.ProjectChannelConfig, error)
	Upsert(ctx context.Context, config *model.ProjectChannelConfig) error
	Delete(ctx context.Context, projectID, channel string) error
}

type gormProjectChannelConfigRepository struct{ db *gorm.DB }

func newProjectChannelConfigRepository(db *gorm.DB) ProjectChannelConfigRepository {
	return &gormProjectChannelConfigRepository{db: db}
}

func (r *gormProjectChannelConfigRepository) Get(ctx context.Context, projectID, channel string) (*model.ProjectChannelConfig, error) {
	var config model.ProjectChannelConfig
	if err := r.db.WithContext(ctx).Where("project_id = ? AND channel = ?", projectID, channel).First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *gormProjectChannelConfigRepository) List(ctx context.Context, projectID string) ([]*model.ProjectChannelConfig, error) {
	var configs []*model.ProjectChannelConfig
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("channel ASC").Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

func (r *gormProjectChannelConfigRepository) Upsert(ctx context.Context, config *model.ProjectChannelConfig) error {
	var existing model.ProjectChannelConfig
	err := r.db.WithContext(ctx).Where("project_id = ? AND channel = ?", config.ProjectID, config.Channel).First(&existing).Error
	if err == nil {
		config.ID = existing.ID
		config.CreatedAt = existing.CreatedAt
		return r.db.WithContext(ctx).Save(config).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return r.db.WithContext(ctx).Create(config).Error
}

func (r *gormProjectChannelConfigRepository) Delete(ctx context.Context, projectID, channel string) error {
	return r.db.WithContext(ctx).Where("project_id = ? AND channel = ?", projectID, channel).Delete(&model.ProjectChannelConfig{}).Error
}
