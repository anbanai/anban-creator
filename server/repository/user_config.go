package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"
	"gorm.io/gorm"
)

// newUserConfigRepository creates a UserConfigRepository backed by GORM.
func newUserConfigRepository(db *gorm.DB) UserConfigRepository {
	return &userConfigRepo{db: db}
}

type userConfigRepo struct {
	db *gorm.DB
}

func (r *userConfigRepo) FindByUserAndScope(ctx context.Context, userID, scope string) (*model.UserConfig, error) {
	var cfg model.UserConfig
	if err := r.db.WithContext(ctx).Where("user_id = ? AND scope = ?", userID, scope).First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *userConfigRepo) Upsert(ctx context.Context, config *model.UserConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

func (r *userConfigRepo) ListByUserID(ctx context.Context, userID string) ([]*model.UserConfig, error) {
	var configs []*model.UserConfig
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

func (r *userConfigRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.UserConfig{}, "id = ?", id).Error
}
