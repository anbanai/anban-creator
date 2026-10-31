package repository

import (
	"context"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type topicSubscriptionRepository struct{ db *gorm.DB }
type topicDigestRepository struct{ db *gorm.DB }
type topicSuggestionRepository struct{ db *gorm.DB }
type topicAdoptionRepository struct{ db *gorm.DB }
type topicScriptJobRepository struct{ db *gorm.DB }
type topicNotificationChannelRepository struct{ db *gorm.DB }
type topicNotificationDeliveryRepository struct{ db *gorm.DB }

func newTopicSubscriptionRepository(db *gorm.DB) TopicSubscriptionRepository {
	return &topicSubscriptionRepository{db: db}
}
func newTopicDigestRepository(db *gorm.DB) TopicDigestRepository {
	return &topicDigestRepository{db: db}
}
func newTopicSuggestionRepository(db *gorm.DB) TopicSuggestionRepository {
	return &topicSuggestionRepository{db: db}
}
func newTopicAdoptionRepository(db *gorm.DB) TopicSuggestionAdoptionRepository {
	return &topicAdoptionRepository{db: db}
}
func newTopicScriptJobRepository(db *gorm.DB) TopicScriptJobRepository {
	return &topicScriptJobRepository{db: db}
}
func newTopicNotificationChannelRepository(db *gorm.DB) TopicNotificationChannelRepository {
	return &topicNotificationChannelRepository{db: db}
}
func newTopicNotificationDeliveryRepository(db *gorm.DB) TopicNotificationDeliveryRepository {
	return &topicNotificationDeliveryRepository{db: db}
}

func (r *topicSubscriptionRepository) Create(ctx context.Context, item *model.TopicSubscription) error {
	return r.db.WithContext(ctx).Create(item).Error
}
func (r *topicSubscriptionRepository) FindByID(ctx context.Context, id string) (*model.TopicSubscription, error) {
	var item model.TopicSubscription
	err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error
	return &item, err
}
func (r *topicSubscriptionRepository) LockByID(ctx context.Context, id string) (*model.TopicSubscription, error) {
	var item model.TopicSubscription
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error
	return &item, err
}
func (r *topicSubscriptionRepository) ListByProject(ctx context.Context, userID, projectID string) ([]*model.TopicSubscription, error) {
	var items []*model.TopicSubscription
	err := r.db.WithContext(ctx).Where("user_id = ? AND project_id = ? AND deleted_at IS NULL", userID, projectID).Order("created_at DESC").Find(&items).Error
	return items, err
}
func (r *topicSubscriptionRepository) FindDue(ctx context.Context, now time.Time, limit int) ([]*model.TopicSubscription, error) {
	var items []*model.TopicSubscription
	err := r.db.WithContext(ctx).Where("deleted_at IS NULL AND enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).Order("next_run_at ASC").Limit(limit).Find(&items).Error
	return items, err
}
func (r *topicSubscriptionRepository) Update(ctx context.Context, item *model.TopicSubscription) error {
	return r.db.WithContext(ctx).Save(item).Error
}
func (r *topicSubscriptionRepository) Delete(ctx context.Context, id, userID, projectID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.TopicSubscription{}).
		Where("id = ? AND user_id = ? AND project_id = ? AND deleted_at IS NULL", id, userID, projectID).
		Updates(map[string]any{"deleted_at": now, "enabled": false, "next_run_at": nil}).Error
}

// Patch changes only explicitly supplied settings. Scheduler timestamps are never
// copied back from a stale API read unless the user changed the schedule itself.
func (r *topicSubscriptionRepository) Patch(ctx context.Context, item *model.TopicSubscription, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&model.TopicSubscription{}).
		Where("id = ? AND user_id = ? AND project_id = ? AND deleted_at IS NULL", item.ID, item.UserID, item.ProjectID).
		Select(fields).Updates(item)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ClaimTask must be called in the transaction inserting the task. The owner
// predicate prevents a stale worker from replacing another task or a terminal job.
func (r *topicDigestRepository) ClaimTask(ctx context.Context, id string, task *model.Task) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicDigest{}).
		Where("id = ? AND user_id = ? AND project_id = ? AND agent_task_id IS NULL AND status IN ?", id, task.UserID, task.ProjectID, []string{model.TopicDigestStatusQueued, model.TopicDigestStatusPreparing}).
		Updates(map[string]any{"agent_task_id": task.ID, "status": model.TopicDigestStatusPreparing})
	return result.RowsAffected == 1, result.Error
}

func (r *topicScriptJobRepository) ClaimTask(ctx context.Context, id string, task *model.Task) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicScriptJob{}).
		Where("id = ? AND user_id = ? AND project_id = ? AND task_id IS NULL AND status = ?", id, task.UserID, task.ProjectID, model.TopicScriptJobQueued).
		Update("task_id", task.ID)
	return result.RowsAffected == 1, result.Error
}

func (r *topicDigestRepository) BindExecution(ctx context.Context, id, taskID, executionID string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicDigest{}).
		Where("id = ? AND agent_task_id = ? AND status IN ?", id, taskID, []string{model.TopicDigestStatusPreparing, model.TopicDigestStatusDispatched, model.TopicDigestStatusRunning}).
		Updates(map[string]any{"agent_execution_id": executionID, "status": model.TopicDigestStatusRunning})
	return result.RowsAffected == 1, result.Error
}

func (r *topicScriptJobRepository) BindExecution(ctx context.Context, id, taskID, executionID string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicScriptJob{}).
		Where("id = ? AND task_id = ? AND status IN ?", id, taskID, []string{model.TopicScriptJobQueued, model.TopicScriptJobRunning}).
		Updates(map[string]any{"execution_id": executionID, "status": model.TopicScriptJobRunning})
	return result.RowsAffected == 1, result.Error
}

func (r *topicDigestRepository) Create(ctx context.Context, item *model.TopicDigest) error {
	return r.db.WithContext(ctx).Create(item).Error
}
func (r *topicDigestRepository) FindByID(ctx context.Context, id string) (*model.TopicDigest, error) {
	var item model.TopicDigest
	err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error
	return &item, err
}
func (r *topicDigestRepository) FindByOccurrence(ctx context.Context, key string) (*model.TopicDigest, error) {
	var item model.TopicDigest
	err := r.db.WithContext(ctx).First(&item, "occurrence_key = ?", key).Error
	return &item, err
}
func (r *topicDigestRepository) FindActiveBySubscription(ctx context.Context, subscriptionID string) (*model.TopicDigest, error) {
	var item model.TopicDigest
	err := r.db.WithContext(ctx).Where("subscription_id = ? AND status IN ?", subscriptionID, []string{model.TopicDigestStatusQueued, model.TopicDigestStatusPreparing, model.TopicDigestStatusDispatched, model.TopicDigestStatusRunning, model.TopicDigestStatusFinalizing}).Order("created_at DESC").First(&item).Error
	return &item, err
}
func (r *topicDigestRepository) ListBySubscription(ctx context.Context, subscriptionID string, limit int) ([]*model.TopicDigest, error) {
	var items []*model.TopicDigest
	err := r.db.WithContext(ctx).Where("subscription_id = ?", subscriptionID).Order("planned_at DESC").Limit(limit).Find(&items).Error
	return items, err
}
func (r *topicDigestRepository) Update(ctx context.Context, item *model.TopicDigest) error {
	return r.db.WithContext(ctx).Save(item).Error
}
func (r *topicDigestRepository) CreateIfAbsent(ctx context.Context, item *model.TopicDigest) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "occurrence_key"}}, DoNothing: true}).Create(item).Error
}

func (r *topicSuggestionRepository) CreateBatch(ctx context.Context, items []*model.TopicSuggestion) error {
	return r.db.WithContext(ctx).Create(items).Error
}
func (r *topicSuggestionRepository) ListByDigest(ctx context.Context, digestID string) ([]*model.TopicSuggestion, error) {
	var items []*model.TopicSuggestion
	err := r.db.WithContext(ctx).Where("digest_id = ?", digestID).Order("rank ASC").Find(&items).Error
	return items, err
}
func (r *topicSuggestionRepository) FindByID(ctx context.Context, id string) (*model.TopicSuggestion, error) {
	var item model.TopicSuggestion
	err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error
	return &item, err
}
func (r *topicSuggestionRepository) Update(ctx context.Context, item *model.TopicSuggestion) error {
	return r.db.WithContext(ctx).Save(item).Error
}

func (r *topicAdoptionRepository) CreateBatch(ctx context.Context, items []*model.TopicSuggestionAdoption) error {
	return r.db.WithContext(ctx).Create(items).Error
}
func (r *topicAdoptionRepository) FindByID(ctx context.Context, id string) (*model.TopicSuggestionAdoption, error) {
	var item model.TopicSuggestionAdoption
	err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error
	return &item, err
}

func (r *topicAdoptionRepository) FindBySuggestion(ctx context.Context, suggestionID string) (*model.TopicSuggestionAdoption, error) {
	var item model.TopicSuggestionAdoption
	err := r.db.WithContext(ctx).First(&item, "suggestion_id = ?", suggestionID).Error
	return &item, err
}
func (r *topicAdoptionRepository) Claim(ctx context.Context, id, token string, until time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicSuggestionAdoption{}).Where("id = ? AND status IN ? AND (lease_until IS NULL OR lease_until < ?)", id, []string{model.TopicAdoptionPending, model.TopicAdoptionFailed, model.TopicAdoptionProcessing}, time.Now()).Updates(map[string]any{"status": model.TopicAdoptionProcessing, "lease_token": token, "lease_until": until, "attempt": gorm.Expr("attempt + 1")})
	return result.RowsAffected == 1, result.Error
}
func (r *topicAdoptionRepository) Update(ctx context.Context, item *model.TopicSuggestionAdoption) error {
	return r.db.WithContext(ctx).Save(item).Error
}
func (r *topicAdoptionRepository) ListRecoverable(ctx context.Context, now time.Time, limit int) ([]*model.TopicSuggestionAdoption, error) {
	var items []*model.TopicSuggestionAdoption
	err := r.db.WithContext(ctx).Where("status IN ? OR (status = ? AND lease_until < ?)", []string{model.TopicAdoptionPending, model.TopicAdoptionFailed}, model.TopicAdoptionProcessing, now).Limit(limit).Find(&items).Error
	return items, err
}

func (r *topicScriptJobRepository) Create(ctx context.Context, item *model.TopicScriptJob) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "fingerprint"}}, DoNothing: true}).Create(item).Error
}
func (r *topicScriptJobRepository) FindByID(ctx context.Context, id string) (*model.TopicScriptJob, error) {
	var item model.TopicScriptJob
	err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error
	return &item, err
}
func (r *topicScriptJobRepository) FindByFingerprint(ctx context.Context, fingerprint string) (*model.TopicScriptJob, error) {
	var item model.TopicScriptJob
	err := r.db.WithContext(ctx).First(&item, "fingerprint = ?", fingerprint).Error
	return &item, err
}
func (r *topicScriptJobRepository) Update(ctx context.Context, item *model.TopicScriptJob) error {
	return r.db.WithContext(ctx).Save(item).Error
}
func (r *topicScriptJobRepository) UpdateIfNonterminal(ctx context.Context, item *model.TopicScriptJob) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicScriptJob{}).
		Where("id = ? AND status NOT IN ? AND COALESCE(task_id, '') = ? AND COALESCE(execution_id, '') = ? AND attempt = ?", item.ID, []string{model.TopicScriptJobSucceeded, model.TopicScriptJobFailed}, topicOwnerID(item.TaskID), topicOwnerID(item.ExecutionID), item.Attempt).
		Updates(map[string]any{
			"status":          item.Status,
			"artifact_path":   item.ArtifactPath,
			"artifact_sha256": item.ArtifactSHA256,
			"error":           item.Error,
			"finished_at":     item.FinishedAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *topicNotificationChannelRepository) Create(ctx context.Context, item *model.TopicNotificationChannel) error {
	return r.db.WithContext(ctx).Create(item).Error
}
func (r *topicNotificationChannelRepository) FindByID(ctx context.Context, id string) (*model.TopicNotificationChannel, error) {
	var item model.TopicNotificationChannel
	err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error
	return &item, err
}
func (r *topicNotificationChannelRepository) ListByProject(ctx context.Context, userID, projectID string) ([]*model.TopicNotificationChannel, error) {
	var items []*model.TopicNotificationChannel
	err := r.db.WithContext(ctx).Where("user_id = ? AND project_id = ?", userID, projectID).Order("created_at DESC").Find(&items).Error
	return items, err
}
func (r *topicNotificationChannelRepository) Update(ctx context.Context, item *model.TopicNotificationChannel) error {
	return r.db.WithContext(ctx).Save(item).Error
}
func (r *topicNotificationChannelRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.TopicNotificationChannel{}, "id = ?", id).Error
}

func (r *topicNotificationDeliveryRepository) CreateBatch(ctx context.Context, items []*model.TopicNotificationDelivery) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "digest_id"}, {Name: "channel_id"}, {Name: "payload_version"}}, DoNothing: true}).Create(items).Error
}
func (r *topicNotificationDeliveryRepository) FindByDigestChannel(ctx context.Context, digestID, channelID string) (*model.TopicNotificationDelivery, error) {
	var item model.TopicNotificationDelivery
	err := r.db.WithContext(ctx).Where("digest_id = ? AND channel_id = ?", digestID, channelID).First(&item).Error
	return &item, err
}
func (r *topicNotificationDeliveryRepository) FindByID(ctx context.Context, id string) (*model.TopicNotificationDelivery, error) {
	var item model.TopicNotificationDelivery
	err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error
	return &item, err
}
func (r *topicNotificationDeliveryRepository) Claim(ctx context.Context, id, token string, until time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.TopicNotificationDelivery{}).
		Where("id = ? AND attempt < 8 AND (next_attempt_at IS NULL OR next_attempt_at <= ?) AND (status IN ? OR (status = ? AND lease_until < ?))", id, time.Now(), []string{model.TopicNotificationPending, model.TopicNotificationFailed}, model.TopicNotificationSending, time.Now()).
		Updates(map[string]any{"status": model.TopicNotificationSending, "lease_token": token, "lease_until": until, "attempt": gorm.Expr("attempt + 1")})
	return result.RowsAffected == 1, result.Error
}
func (r *topicNotificationDeliveryRepository) UpdateClaimed(ctx context.Context, item *model.TopicNotificationDelivery, token string) error {
	result := r.db.WithContext(ctx).Model(&model.TopicNotificationDelivery{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_until > ?", item.ID, model.TopicNotificationSending, token, time.Now()).Updates(map[string]any{"status": item.Status, "last_error": item.LastError, "sent_at": item.SentAt, "next_attempt_at": item.NextAttemptAt, "lease_token": "", "lease_until": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("notification delivery lease lost")
	}
	return nil
}
func (r *topicNotificationDeliveryRepository) Update(ctx context.Context, item *model.TopicNotificationDelivery) error {
	return r.db.WithContext(ctx).Save(item).Error
}
func (r *topicNotificationDeliveryRepository) ListRecoverable(ctx context.Context, now time.Time, limit int) ([]*model.TopicNotificationDelivery, error) {
	var items []*model.TopicNotificationDelivery
	err := r.db.WithContext(ctx).Where("attempt < 8 AND (next_attempt_at IS NULL OR next_attempt_at <= ?) AND (status IN ? OR (status = ? AND lease_until < ?))", now, []string{model.TopicNotificationPending, model.TopicNotificationFailed}, model.TopicNotificationSending, now).Order("updated_at ASC").Limit(limit).Find(&items).Error
	return items, err
}

var _ = errors.Is
