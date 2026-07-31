package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MigratePlanReferenceAttachments moves active plans from the dedicated task
// reference field to the ordered input attachment contract.
func MigratePlanReferenceAttachments(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}

	migrated := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plans []model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status = ? AND TRIM(reference_image_asset_id) <> ''", model.PlanStatusActive).
			Order("id ASC").
			Find(&plans).Error; err != nil {
			return fmt.Errorf("list active plan references: %w", err)
		}

		for i := range plans {
			plan := &plans[i]
			assetID := strings.TrimSpace(plan.ReferenceImageAssetID)
			var asset model.Asset
			if err := tx.Where("id = ? AND user_id = ?", assetID, plan.UserID).First(&asset).Error; err != nil {
				return fmt.Errorf("validate reference asset for plan %q: %w", plan.ID, err)
			}
			if err := validateOwnedReferenceAsset(&asset, plan.UserID, []string{DirectUploadPurposeTaskReference}); err != nil {
				return fmt.Errorf("validate reference asset for plan %q: %w", plan.ID, err)
			}

			attachments := plan.InputAttachments.Data()
			alreadyPresent := false
			for _, attachment := range attachments {
				if strings.TrimSpace(attachment.AssetID) == asset.ID {
					alreadyPresent = true
					break
				}
			}
			if !alreadyPresent {
				migratedAttachment := model.EntryAttachment{
					AssetID: asset.ID, Type: "image", FileName: asset.FileName,
					ContentType: asset.ContentType, Size: asset.Size,
				}
				attachments = append([]model.EntryAttachment{migratedAttachment}, attachments...)
			}

			if err := tx.Model(&model.Plan{}).Where("id = ?", plan.ID).Updates(map[string]any{
				"input_attachments":        datatypes.NewJSONType(attachments),
				"reference_image_asset_id": "",
			}).Error; err != nil {
				return fmt.Errorf("persist reference attachment for plan %q: %w", plan.ID, err)
			}
			migrated++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if migrated > 0 && log != nil {
		log.Info().Int("migrated", migrated).Msg("plan reference attachment migration completed")
	}
	return nil
}
