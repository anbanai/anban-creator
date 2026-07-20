package repository

import (
	"context"
	"errors"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
)

type assetRepository struct {
	db *gorm.DB
}

func newAssetRepository(db *gorm.DB) AssetRepository {
	return &assetRepository{db: db}
}

func (r *assetRepository) Create(ctx context.Context, asset *model.Asset) error {
	return r.db.WithContext(ctx).Create(asset).Error
}

func (r *assetRepository) FindByID(ctx context.Context, id string) (*model.Asset, error) {
	return r.find(ctx, "id = ?", id)
}

func (r *assetRepository) FindOwnedByID(ctx context.Context, id, userID string) (*model.Asset, error) {
	return r.find(ctx, "id = ? AND user_id = ?", id, userID)
}

func (r *assetRepository) find(ctx context.Context, query string, args ...any) (*model.Asset, error) {
	var asset model.Asset
	err := r.db.WithContext(ctx).Where(query, args...).First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, model.ErrAssetNotFound
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}
