package repository

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
)

func (r *topicAdoptionRepository) ListByDigest(ctx context.Context, id string) ([]*model.TopicSuggestionAdoption, error) {
	items := make([]*model.TopicSuggestionAdoption, 0)
	err := r.db.WithContext(ctx).Where("digest_id = ?", id).Order("created_at ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *topicNotificationDeliveryRepository) ListByDigest(ctx context.Context, id string) ([]*model.TopicNotificationDelivery, error) {
	items := make([]*model.TopicNotificationDelivery, 0)
	err := r.db.WithContext(ctx).Where("digest_id = ?", id).Order("created_at ASC, id ASC").Find(&items).Error
	return items, err
}
