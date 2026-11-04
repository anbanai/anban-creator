package repository

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
)

// Three batch queries for the whole page, never one query per topic.
func (r *contentAssetRepository) PopulateTopicSummaries(ctx context.Context, userID string, assets []*model.ContentAsset) error {
	if len(assets) == 0 {
		return nil
	}
	ids := make([]string, 0, len(assets))
	byID := map[string]*model.ContentAsset{}
	for _, a := range assets {
		ids = append(ids, a.ID)
		byID[a.ID] = a
		a.ProjectUses = []model.TopicProjectUse{}
		a.Outputs = []model.TopicOutput{}
	}
	var uses []model.TopicProjectUse
	if err := r.db.WithContext(ctx).Table("topic_pool p").Select("p.content_asset_id AS asset_id, p.project_id, projects.name AS project_name, p.status, p.task_id, p.used_at").Joins("JOIN projects ON projects.id = p.project_id AND projects.user_id = p.user_id").Where("p.user_id = ? AND p.content_asset_id IN ?", userID, ids).Order("p.created_at, p.id").Scan(&uses).Error; err != nil {
		return err
	}
	// Historical duplicate queue rows are summarized once per persona, preserving usage.
	for _, u := range uses {
		a := byID[u.AssetID]
		found := false
		for i, old := range a.ProjectUses {
			if old.ProjectID == u.ProjectID {
				if u.Status == model.TopicStatusUsed {
					a.ProjectUses[i] = u
				}
				found = true
				break
			}
		}
		if !found {
			a.ProjectUses = append(a.ProjectUses, u)
		}
	}
	var outputs []model.TopicOutput
	if err := r.db.WithContext(ctx).Model(&model.Task{}).Select("content_asset_id AS asset_id, id AS task_id, project_id, type, status, title").Where("user_id = ? AND content_asset_id IN ?", userID, ids).Order("created_at DESC").Scan(&outputs).Error; err != nil {
		return err
	}
	var scripts []model.TopicOutput
	if err := r.db.WithContext(ctx).Table("topic_script_jobs j").Select("j.content_asset_id AS asset_id, j.id AS job_id, j.task_id, j.project_id, 'topic-script' AS type, j.status, j.error, a.title").Joins("JOIN content_assets a ON a.id=j.content_asset_id AND a.user_id=j.user_id").Where("j.user_id = ? AND j.content_asset_id IN ?", userID, ids).Order("j.created_at DESC").Scan(&scripts).Error; err != nil {
		return err
	}
	scriptTasks := map[string]bool{}
	for _, o := range scripts {
		if a := byID[o.AssetID]; a != nil {
			a.Outputs = append(a.Outputs, o)
		}
		if o.TaskID != "" {
			scriptTasks[o.TaskID] = true
		}
	}
	for _, o := range outputs {
		if !scriptTasks[o.TaskID] {
			if a := byID[o.AssetID]; a != nil {
				a.Outputs = append(a.Outputs, o)
			}
		}
	}
	return nil
}
