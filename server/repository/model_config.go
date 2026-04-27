package repository

import (
	"context"

	"github.com/royalrick/anbanwriter/server/model"
	"gorm.io/gorm"
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
	// Use native ON DUPLICATE KEY UPDATE for atomic upsert (avoids race conditions).
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO user_model_configs (id, user_id, text_config_encrypted, image_config_encrypted, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(), NOW())
		ON DUPLICATE KEY UPDATE
			text_config_encrypted = VALUES(text_config_encrypted),
			image_config_encrypted = VALUES(image_config_encrypted),
			updated_at = NOW()
	`, config.ID, config.UserID, config.TextConfigEncrypted, config.ImageConfigEncrypted).Error
}

func (r *modelConfigRepository) Delete(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.UserModelConfig{}).Error
}
