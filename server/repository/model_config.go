package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ModelConfigRepository provides access to the user_model_configs table.
type ModelConfigRepository interface {
	FindByUserID(ctx context.Context, userID string) (*model.UserModelConfig, error)
	Upsert(ctx context.Context, config *model.UserModelConfig) error
	Delete(ctx context.Context, userID string) error
}

type modelConfigRepository struct {
	db *gorm.DB
}

func newModelConfigRepository(db *gorm.DB) ModelConfigRepository {
	return &modelConfigRepository{db: db}
}

func (r *modelConfigRepository) FindByUserID(ctx context.Context, userID string) (*model.UserModelConfig, error) {
	var cfg model.UserModelConfig
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&cfg).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *modelConfigRepository) Upsert(ctx context.Context, config *model.UserModelConfig) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"text_config_json",
			"image_config_json",
			"updated_at",
		}),
	}).Create(config).Error
}

func (r *modelConfigRepository) Delete(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.UserModelConfig{}).Error
}
