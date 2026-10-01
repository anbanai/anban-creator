package repository

import (
	"context"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

func isMissingProjectConfigTable(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "no such table") || strings.Contains(m, "doesn't exist") || strings.Contains(m, "does not exist")
}

// ProjectAgentConfigRepository persists project-scoped Agent settings.
type ProjectAgentConfigRepository interface {
	Get(ctx context.Context, projectID, agentID string) (*model.ProjectAgentConfig, error)
	List(ctx context.Context, projectID string) ([]*model.ProjectAgentConfig, error)
	Upsert(ctx context.Context, config *model.ProjectAgentConfig) error
	Delete(ctx context.Context, projectID, agentID string) error
}

type gormProjectAgentConfigRepository struct{ db *gorm.DB }

func newProjectAgentConfigRepository(db *gorm.DB) ProjectAgentConfigRepository {
	return &gormProjectAgentConfigRepository{db: db}
}

func (r *gormProjectAgentConfigRepository) Get(ctx context.Context, projectID, agentID string) (*model.ProjectAgentConfig, error) {
	var config model.ProjectAgentConfig
	if err := r.db.WithContext(ctx).Where("project_id = ? AND agent_id = ?", projectID, agentID).First(&config).Error; err != nil {
		if isMissingProjectConfigTable(err) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return &config, nil
}

func (r *gormProjectAgentConfigRepository) List(ctx context.Context, projectID string) ([]*model.ProjectAgentConfig, error) {
	var configs []*model.ProjectAgentConfig
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("agent_id ASC").Find(&configs).Error; err != nil {
		if isMissingProjectConfigTable(err) {
			return configs, nil
		}
		return nil, err
	}
	return configs, nil
}

func (r *gormProjectAgentConfigRepository) Upsert(ctx context.Context, config *model.ProjectAgentConfig) error {
	var existing model.ProjectAgentConfig
	err := r.db.WithContext(ctx).Where("project_id = ? AND agent_id = ?", config.ProjectID, config.AgentID).First(&existing).Error
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

func (r *gormProjectAgentConfigRepository) Delete(ctx context.Context, projectID, agentID string) error {
	return r.db.WithContext(ctx).Where("project_id = ? AND agent_id = ?", projectID, agentID).Delete(&model.ProjectAgentConfig{}).Error
}

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
		if isMissingProjectConfigTable(err) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return &config, nil
}

func (r *gormProjectChannelConfigRepository) List(ctx context.Context, projectID string) ([]*model.ProjectChannelConfig, error) {
	var configs []*model.ProjectChannelConfig
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("channel ASC").Find(&configs).Error; err != nil {
		if isMissingProjectConfigTable(err) {
			return configs, nil
		}
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
