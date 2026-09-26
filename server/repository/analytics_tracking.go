package repository

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
)

func (r *AnalyticsRepository) TrackingObservations(ctx context.Context, taskID, source string) ([]model.AnalyticsObservation, error) {
	var rows []model.AnalyticsObservation
	inner := r.db.WithContext(ctx).Table("analytics_observations AS o").Select("o.*, ROW_NUMBER() OVER (PARTITION BY o.content_id,o.tracking_id,o.stat_date ORDER BY o.effective_at DESC,o.sequence DESC) AS winner_rank").Joins("JOIN analytics_contents c ON c.id=o.content_id AND c.project_id=o.project_id").Where("c.task_id = ? AND o.source = ? AND o.revoked_at IS NULL AND o.metric_basis = ?", taskID, source, "cumulative")
	err := r.db.WithContext(ctx).Table("(?) AS winners", inner).Where("winner_rank = 1").Order("stat_date ASC, sequence ASC").Scan(&rows).Error
	return rows, err
}

func (r *AnalyticsRepository) SeednotePostForTracking(ctx context.Context, t *model.SeednotePostTracking) (*model.SeednotePost, error) {
	var posts []model.SeednotePost
	err := r.db.WithContext(ctx).Where("project_id = ? AND task_id = ? AND (note_id = ? OR note_url = ?)", t.ProjectID, t.TaskID, t.NoteID, t.NoteURL).Order("created_at DESC,id DESC").Limit(1).Find(&posts).Error
	if err != nil {
		return nil, err
	}
	if len(posts) > 0 {
		return &posts[0], nil
	}
	post := &model.SeednotePost{ID: t.ID, ProjectID: t.ProjectID, UserID: t.UserID, TaskID: t.TaskID, NoteID: t.NoteID, NoteURL: t.NoteURL, Title: t.NoteTitle, Genre: "unknown", CreatedAt: t.PublishedMarkedAt}
	if err = r.db.WithContext(ctx).Create(post).Error; err != nil {
		return nil, err
	}
	return post, nil
}
