package repository

import (
	"context"
	"errors"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func (r *topicPoolRepository) FindByProject(ctx context.Context, projectID, status string, offset, limit int) ([]*model.TopicPool, int64, error) {
	var topics []*model.TopicPool
	var total int64

	q := r.db.WithContext(ctx).Model(&model.TopicPool{}).Where("project_id = ?", projectID)
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

// ClaimOne atomically claims the earliest unused topic using SELECT FOR UPDATE
// to prevent race conditions when multiple plans trigger simultaneously.
func (r *topicPoolRepository) ClaimOne(ctx context.Context, userID, projectID string) (*model.TopicPool, error) {
	var topic model.TopicPool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND project_id = ? AND status = ?", userID, projectID, model.TopicStatusUnused).
			Order("created_at ASC").
			Limit(1).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&topic).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		return tx.Model(&topic).Updates(map[string]any{
			"status":  model.TopicStatusUsed,
			"used_at": time.Now(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	if topic.ID == 0 {
		return nil, nil
	}
	return &topic, nil
}

// ClaimWithTask atomically claims a topic and associates it with a task using
// SELECT FOR UPDATE to prevent race conditions.
//
// Idempotent per task: if taskID already has a topic bound to it, that same
// topic is returned and no second topic is consumed. This is the server-side
// anti-double-consume guard — the server pre-claims a topic into the task prompt
// and a research skill may independently call claim_topic; both must resolve to
// the one topic already assigned to the task rather than burning a second.
func (r *topicPoolRepository) ClaimWithTask(ctx context.Context, userID, projectID, taskID string) (*model.TopicPool, error) {
	var topic model.TopicPool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Re-claim short-circuit: a topic already bound to this task wins.
		var existing model.TopicPool
		if err := tx.Where("task_id = ? AND status = ?", taskID, model.TopicStatusUsed).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&existing).Error; err == nil {
			topic = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err := tx.Where("user_id = ? AND project_id = ? AND status = ?", userID, projectID, model.TopicStatusUnused).
			Order("created_at ASC").
			Limit(1).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&topic).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		return tx.Model(&topic).Updates(map[string]any{
			"status":  model.TopicStatusUsed,
			"task_id": taskID,
			"used_at": time.Now(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	if topic.ID == 0 {
		return nil, nil
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

// ResetByTask releases the topic bound to the given task back to the pool
// (status unused, task_id/used_at cleared). Used when a task that pre-claimed a
// topic fails to persist, so the topic isn't permanently orphaned. A no-op
// (rows=0, no error) when no topic is bound to the task.
func (r *topicPoolRepository) ResetByTask(ctx context.Context, taskID string) error {
	return r.db.WithContext(ctx).Model(&model.TopicPool{}).
		Where("task_id = ? AND status = ?", taskID, model.TopicStatusUsed).
		Updates(map[string]any{
			"status":  model.TopicStatusUnused,
			"task_id": nil,
			"used_at": nil,
		}).Error
}

func (r *topicPoolRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.TopicPool{}).Error
}
