package service

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MigrateTopicPoolToContentAssets backfills legacy topic_pool rows into the
// user-scoped canonical asset table, preserving queue consumption and lineage.
func MigrateTopicPoolToContentAssets(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil || !db.Migrator().HasTable("topic_pool") || !db.Migrator().HasTable("content_assets") {
		return nil
	}
	var assets []model.ContentAsset
	if err := db.WithContext(ctx).Where("title_key IS NULL OR title_key = ''").Find(&assets).Error; err != nil {
		return err
	}
	for _, asset := range assets {
		if err := db.WithContext(ctx).Model(&model.ContentAsset{}).Where("id = ?", asset.ID).UpdateColumn("title_key", model.NormalizeTopicDedupeKey(asset.Title)).Error; err != nil {
			return err
		}
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
		var assetID string
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Lock all matching legacy rows so concurrent startup migrations cannot
			// create duplicate assets for the same user and title.
			var pending []struct{ ID uint }
			if err := tx.Table("topic_pool").Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ? AND topic = ? AND (content_asset_id IS NULL OR content_asset_id = '')", row.UserID, row.Topic).
				Find(&pending).Error; err != nil {
				return fmt.Errorf("lock legacy topics for %d: %w", row.ID, err)
			}
			if len(pending) == 0 {
				return nil
			}
			var asset model.ContentAsset
			err := tx.Where("user_id = ? AND title = ?", row.UserID, row.Topic).First(&asset).Error
			if err != nil {
				if err != gorm.ErrRecordNotFound {
					return fmt.Errorf("find asset for topic %d: %w", row.ID, err)
				}
				asset = *model.NewContentAsset(row.UserID, row.Topic)
				if err := tx.Create(&asset).Error; err != nil {
					return fmt.Errorf("create asset for topic %d: %w", row.ID, err)
				}
			}
			assetID = asset.ID
			if err := tx.Table("topic_pool").Where("user_id = ? AND topic = ? AND (content_asset_id IS NULL OR content_asset_id = '')", row.UserID, row.Topic).Update("content_asset_id", asset.ID).Error; err != nil {
				return fmt.Errorf("link topic %d to asset: %w", row.ID, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if log != nil && assetID != "" {
			log.Info().Uint("topic_id", row.ID).Str("asset_id", assetID).Msg("backfilled topic pool row into content asset")
		}
	}
	if err := repository.RekeyTopicQueues(ctx, db, "", ""); err != nil {
		return err
	}
	return migrateUnifiedTopicHistory(ctx, db)
}
