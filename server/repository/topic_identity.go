package repository

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// Queue uniqueness follows asset identity, never its historical title. Retain
// duplicate historical rows with NULL keys so timestamps and task IDs survive.
func RekeyTopicQueues(ctx context.Context, db *gorm.DB, userID, assetID string) error {
	q := db.WithContext(ctx).Model(&model.TopicPool{}).Where("content_asset_id IS NOT NULL AND content_asset_id <> ''")
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if assetID != "" {
		q = q.Where("content_asset_id = ?", assetID)
	}
	var rows []model.TopicPool
	if err := q.Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return err
	}
	groups := map[string][]model.TopicPool{}
	for _, row := range rows {
		key := row.ProjectID + ":" + *row.ContentAssetID
		groups[key] = append(groups[key], row)
	}
	for _, group := range groups {
		key := "asset:" + *group[0].ContentAssetID
		clean := group[0].DedupeKey == key
		for _, row := range group[1:] {
			clean = clean && row.DedupeKey == ""
		}
		if clean {
			continue
		}
		ids := make([]uint, len(group))
		for i, row := range group {
			ids[i] = row.ID
		}
		if err := db.WithContext(ctx).Model(&model.TopicPool{}).Where("id IN ?", ids).UpdateColumn("dedupe_key", nil).Error; err != nil {
			return err
		}
		if err := db.WithContext(ctx).Model(&model.TopicPool{}).Where("id = ?", group[0].ID).UpdateColumn("dedupe_key", key).Error; err != nil {
			return err
		}
	}
	return nil
}

func rekeyTopicScripts(ctx context.Context, db *gorm.DB, userID, assetID string) error {
	var jobs []model.TopicScriptJob
	if err := db.WithContext(ctx).Where("user_id = ? AND content_asset_id = ?", userID, assetID).Order("CASE WHEN status = 'succeeded' THEN 0 WHEN status IN ('running','queued') THEN 1 ELSE 2 END, created_at ASC").Find(&jobs).Error; err != nil {
		return err
	}
	for _, job := range jobs {
		fingerprint := model.TopicScriptFingerprint(job.UserID, job.ProjectID, job.ContentAssetID, job.TargetPlatform, job.DurationSeconds, job.AspectRatio)
		var count int64
		if err := db.Model(&model.TopicScriptJob{}).Where("fingerprint = ?", fingerprint).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := db.Model(&model.TopicScriptJob{}).Where("id = ?", job.ID).UpdateColumn("fingerprint", fingerprint).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
