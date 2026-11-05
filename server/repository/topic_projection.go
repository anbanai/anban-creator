package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// ProjectTopicAssociation runs in the caller's transaction. Admission already
// owns its project lock; this helper never acquires a project/policy/wallet lock.
// Queue history is filled, never replaced.
func (r *contentAssetRepository) ProjectTopicAssociation(ctx context.Context, userID, projectID, assetID, taskID string, production bool, at time.Time) error {
	if projectID == "" || assetID == "" {
		return nil
	}
	asset, err := r.FindByID(ctx, userID, assetID)
	if err != nil {
		return err
	}
	var project model.Project
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", projectID, userID).First(&project).Error; err != nil {
		return err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	row := &model.TopicPool{UserID: userID, ProjectID: projectID, ContentAssetID: &asset.ID, Topic: asset.Title, Status: model.TopicStatusUnused, CreatedAt: at}
	if _, err := newTopicPoolRepository(r.db).AddWithSource(ctx, row); err != nil {
		return err
	}
	if taskID != "" {
		var task model.Task
		if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ? AND project_id = ?", taskID, userID, projectID).First(&task).Error; err != nil {
			return err
		}
		if task.ContentAssetID != "" && task.ContentAssetID != assetID {
			return fmt.Errorf("task has a different canonical topic")
		}
		if task.ContentAssetID == "" {
			result := r.db.WithContext(ctx).Model(&model.Task{}).Where("id = ? AND user_id = ? AND project_id = ? AND (content_asset_id IS NULL OR content_asset_id = '')", taskID, userID, projectID).UpdateColumn("content_asset_id", assetID)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("task canonical topic changed concurrently")
			}
		}
	}
	if !production {
		return nil
	}
	values := map[string]any{"status": model.TopicStatusUsed, "used_at": gorm.Expr("COALESCE(used_at, ?)", at)}
	if taskID != "" {
		values["task_id"] = gorm.Expr("COALESCE(NULLIF(task_id, ''), ?)", taskID)
	}
	return r.db.WithContext(ctx).Model(&model.TopicPool{}).Where("id = ?", row.ID).UpdateColumns(values).Error
}
