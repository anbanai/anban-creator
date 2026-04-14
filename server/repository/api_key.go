package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

// APIKeyRepository provides access to the api_keys table.
type APIKeyRepository interface {
	Create(ctx context.Context, key *model.APIKey) error
	FindByHash(ctx context.Context, keyHash string) (*model.APIKey, error)
	FindByUserID(ctx context.Context, userID string) ([]*model.APIKey, error)
	FindByID(ctx context.Context, id string) (*model.APIKey, error)
	UpdateLastUsed(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

type apiKeyRepository struct {
	db *gorm.DB
}

func newAPIKeyRepository(db *gorm.DB) APIKeyRepository {
	return &apiKeyRepository{db: db}
}

func (r *apiKeyRepository) Create(ctx context.Context, key *model.APIKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *apiKeyRepository) FindByHash(ctx context.Context, keyHash string) (*model.APIKey, error) {
	var key model.APIKey
	err := r.db.WithContext(ctx).Where("key_hash = ?", keyHash).First(&key).Error
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *apiKeyRepository) FindByUserID(ctx context.Context, userID string) ([]*model.APIKey, error) {
	var keys []*model.APIKey
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&keys).Error
	return keys, err
}

func (r *apiKeyRepository) FindByID(ctx context.Context, id string) (*model.APIKey, error) {
	var key model.APIKey
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&key).Error
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *apiKeyRepository) UpdateLastUsed(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.APIKey{}).
		Where("id = ?", id).
		Update("last_used_at", now).Error
}

func (r *apiKeyRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.APIKey{}, "id = ?", id).Error
}
