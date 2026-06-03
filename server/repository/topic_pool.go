package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

type topicPoolRepository struct {
	db *gorm.DB
}

func newTopicPoolRepository(db *gorm.DB) TopicPoolRepository {
	return &topicPoolRepository{db: db}
}

func (r *topicPoolRepository) Create(ctx context.Context, topic *model.TopicPool) error {
	return r.db.WithContext(ctx).Create(topic).Error
}

func (r *topicPoolRepository) CreateBatch(ctx context.Context, topics []*model.TopicPool) error {
	return r.db.WithContext(ctx).Create(topics).Error
}

func (r *topicPoolRepository) FindByID(ctx context.Context, id uint) (*model.TopicPool, error) {
	var topic model.TopicPool
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&topic).Error; err != nil {
		return nil, err
	}
	return &topic, nil
}

func (r *topicPoolRepository) FindByChannel(ctx context.Context, channelID, status string, offset, limit int) ([]*model.TopicPool, int64, error) {
	var topics []*model.TopicPool
	var total int64

	q := r.db.WithContext(ctx).Model(&model.TopicPool{}).Where("channel_id = ?", channelID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := q.Order("created_at ASC").Offset(offset).Limit(limit).Find(&topics).Error; err != nil {
		return nil, 0, err
	}
	return topics, total, nil
}

// ClaimOne atomically claims the earliest unused topic using a subquery to
// prevent race conditions when multiple plans trigger simultaneously.
func (r *topicPoolRepository) ClaimOne(ctx context.Context, userID, channelID string) (*model.TopicPool, error) {
	now := time.Now()

	// Subquery: find the earliest unused topic ID.
	subQuery := r.db.WithContext(ctx).
		Model(&model.TopicPool{}).
		Select("id").
		Where("user_id = ? AND channel_id = ? AND status = ?", userID, channelID, model.TopicStatusUnused).
		Order("created_at ASC").
		Limit(1)

	// Atomic UPDATE with RowsAffected check.
	result := r.db.WithContext(ctx).
		Model(&model.TopicPool{}).
		Where("id = (?) AND status = ?", subQuery, model.TopicStatusUnused).
		Updates(map[string]any{
			"status":  model.TopicStatusUsed,
			"used_at": now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	// Retrieve the claimed topic by matching the exact update timestamp.
	var topic model.TopicPool
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND channel_id = ? AND status = ? AND used_at = ?", userID, channelID, model.TopicStatusUsed, now).
		First(&topic).Error; err != nil {
		return nil, err
	}
	return &topic, nil
}

// ClaimWithTask atomically claims a topic and associates it with a task in
// a single UPDATE statement, avoiding the two-step non-atomic approach.
func (r *topicPoolRepository) ClaimWithTask(ctx context.Context, userID, channelID, taskID string) (*model.TopicPool, error) {
	now := time.Now()

	subQuery := r.db.WithContext(ctx).
		Model(&model.TopicPool{}).
		Select("id").
		Where("user_id = ? AND channel_id = ? AND status = ?", userID, channelID, model.TopicStatusUnused).
		Order("created_at ASC").
		Limit(1)

	result := r.db.WithContext(ctx).
		Model(&model.TopicPool{}).
		Where("id = (?) AND status = ?", subQuery, model.TopicStatusUnused).
		Updates(map[string]any{
			"status":  model.TopicStatusUsed,
			"task_id": taskID,
			"used_at": now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	var topic model.TopicPool
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND channel_id = ? AND status = ? AND used_at = ? AND task_id = ?", userID, channelID, model.TopicStatusUsed, now, taskID).
		First(&topic).Error; err != nil {
		return nil, err
	}
	return &topic, nil
}

func (r *topicPoolRepository) MarkUsed(ctx context.Context, id uint, taskID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.TopicPool{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":  model.TopicStatusUsed,
			"task_id": taskID,
			"used_at": now,
		}).Error
}

func (r *topicPoolRepository) ResetStatus(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.TopicPool{}).
		Where("id = ? AND status = ?", id, model.TopicStatusUsed).
		Updates(map[string]any{
			"status":  model.TopicStatusUnused,
			"task_id": nil,
			"used_at": nil,
		}).Error
}

func (r *topicPoolRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.TopicPool{}).Error
}
