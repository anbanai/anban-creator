package service

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateTopicPoolToContentAssets backfills legacy topic_pool rows into the
// user-scoped reusable asset table. It is idempotent and leaves the legacy row
// available for older clients while linking it to the canonical asset.
func MigrateTopicPoolToContentAssets(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil || !db.Migrator().HasTable("topic_pool") || !db.Migrator().HasTable("content_assets") {
		return nil
	}
	var rows []struct {
		ID             uint
		UserID         string
		Topic          string
		ContentAssetID *string
	}
	if err := db.WithContext(ctx).Table("topic_pool").Where("content_asset_id IS NULL OR content_asset_id = ''").Find(&rows).Error; err != nil {
		return fmt.Errorf("list legacy topics: %w", err)
	}
	for _, row := range rows {
		var asset model.ContentAsset
		err := db.WithContext(ctx).Where("user_id = ? AND title = ?", row.UserID, row.Topic).First(&asset).Error
		if err != nil {
			if err != gorm.ErrRecordNotFound {
				return fmt.Errorf("find asset for topic %d: %w", row.ID, err)
			}
			asset = *model.NewContentAsset(row.UserID, row.Topic)
			if err := db.WithContext(ctx).Create(&asset).Error; err != nil {
				return fmt.Errorf("create asset for topic %d: %w", row.ID, err)
			}
		}
		if err := db.WithContext(ctx).Table("topic_pool").Where("id = ?", row.ID).Update("content_asset_id", asset.ID).Error; err != nil {
			return fmt.Errorf("link topic %d to asset: %w", row.ID, err)
		}
		if log != nil {
			log.Info().Uint("topic_id", row.ID).Str("asset_id", asset.ID).Msg("backfilled topic pool row into content asset")
		}
	}
	return nil
}
