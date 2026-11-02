package repository

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"time"
)

func (r *topicSubscriptionRepository) AdvanceSchedule(ctx context.Context, item *model.TopicSubscription, next time.Time) error {
	return r.db.WithContext(ctx).Model(&model.TopicSubscription{}).Where("id = ? AND deleted_at IS NULL AND enabled = ? AND next_run_at = ? AND updated_at = ?", item.ID, true, item.NextRunAt, item.UpdatedAt).Updates(map[string]any{"next_run_at": next, "last_run_at": item.NextRunAt}).Error
}
func (r *topicDigestRepository) MarkDispatch(ctx context.Context, id, status, message string) error {
	return r.db.WithContext(ctx).Model(&model.TopicDigest{}).Where("id = ? AND agent_task_id IS NULL AND status = ?", id, model.TopicDigestStatusQueued).Updates(map[string]any{"dispatch_status": status, "error": message}).Error
}
func (r *topicDigestRepository) ListDispatchable(ctx context.Context, limit int) ([]*model.TopicDigest, error) {
	var items []*model.TopicDigest
	err := r.db.WithContext(ctx).Where("status NOT IN ? AND (agent_task_id IS NULL OR agent_task_id IN (?))", []string{model.TopicDigestStatusSucceeded, model.TopicDigestStatusFailed}, r.db.Model(&model.Task{}).Select("id").Where("status IN ?", []string{model.TaskStatusPending, model.TaskStatusFailed, model.TaskStatusCancelled, model.TaskStatusCompleted})).Order("updated_at ASC").Limit(limit).Find(&items).Error
	return items, err
}
func (r *topicScriptJobRepository) ListDispatchable(ctx context.Context, limit int) ([]*model.TopicScriptJob, error) {
	var items []*model.TopicScriptJob
	err := r.db.WithContext(ctx).Where("status NOT IN ? AND (task_id IS NULL OR task_id IN (?))", []string{model.TopicScriptJobSucceeded, model.TopicScriptJobFailed}, r.db.Model(&model.Task{}).Select("id").Where("status IN ?", []string{model.TaskStatusPending, model.TaskStatusFailed, model.TaskStatusCancelled, model.TaskStatusCompleted})).Order("updated_at ASC").Limit(limit).Find(&items).Error
	return items, err
}
func (r *topicScriptJobRepository) RetryFailed(ctx context.Context, item *model.TopicScriptJob) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicScriptJob{}).Where("id = ? AND status = ? AND attempt = ?", item.ID, model.TopicScriptJobFailed, item.Attempt).Updates(map[string]any{"status": model.TopicScriptJobQueued, "attempt": gorm.Expr("attempt + 1"), "task_id": nil, "execution_id": nil, "artifact_path": "", "artifact_sha256": "", "error": "", "started_at": nil, "finished_at": nil})
	return result.RowsAffected == 1, result.Error
}

func (r *topicDigestRepository) ListPendingEffects(ctx context.Context, limit int) ([]*model.TopicDigest, error) {
	var items []*model.TopicDigest
	err := r.db.WithContext(ctx).Where("status = ? AND effects_queued_at IS NULL", model.TopicDigestStatusSucceeded).Order("updated_at ASC").Limit(limit).Find(&items).Error
	return items, err
}
func (r *topicDigestRepository) MarkEffectsQueued(ctx context.Context, id string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&model.TopicDigest{}).Where("id = ? AND status = ? AND effects_queued_at IS NULL", id, model.TopicDigestStatusSucceeded).Update("effects_queued_at", now).Error
}
func (r *topicScriptJobRepository) ListByDigest(ctx context.Context, id string) ([]*model.TopicScriptJob, error) {
	var items []*model.TopicScriptJob
	err := r.db.WithContext(ctx).Where("digest_id = ?", id).Order("created_at ASC").Find(&items).Error
	return items, err
}
