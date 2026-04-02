package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type userConfigRepository struct {
	db *gorm.DB
}

func newUserConfigRepository(db *gorm.DB) UserConfigRepository {
	return &userConfigRepository{db: db}
}

func (r *userConfigRepository) FindByUserAndScope(ctx context.Context, userID, scope string) (*model.UserConfig, error) {
	var cfg model.UserConfig
	if err := r.db.WithContext(ctx).Where("user_id = ? AND scope = ?", userID, scope).First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *userConfigRepository) Upsert(ctx context.Context, config *model.UserConfig) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "scope"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"wechat_app_id", "wechat_secret", "name", "keywords",
			"positioning", "style", "theme", "author", "image_api_config", "updated_at",
		}),
	}).Create(config).Error
}

func (r *userConfigRepository) ListByUserID(ctx context.Context, userID string) ([]*model.UserConfig, error) {
	var configs []*model.UserConfig
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

func (r *userConfigRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.UserConfig{}, id).Error
}
