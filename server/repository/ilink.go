package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IlinkBindingRepository interface {
	Create(ctx context.Context, binding *model.IlinkBinding) error
	FindByUserID(ctx context.Context, userID string) (*model.IlinkBinding, error)
	FindByContact(ctx context.Context, platformAccountID, externalUserID string) (*model.IlinkBinding, error)
	FindByBindCode(ctx context.Context, code string, now time.Time) (*model.IlinkBinding, error)
	Update(ctx context.Context, binding *model.IlinkBinding) error
	UpdateDefaultProject(ctx context.Context, userID, projectID string) error
	Delete(ctx context.Context, userID string) error
}

type IlinkNotificationRepository interface {
	Enqueue(ctx context.Context, item *model.IlinkNotification) error
	ListDue(ctx context.Context, limit int) ([]*model.IlinkNotification, error)
	ClaimDue(ctx context.Context, limit int, leaseUntil time.Time) ([]*model.IlinkNotification, error)
	MarkDelivered(ctx context.Context, id string) error
	MarkRetry(ctx context.Context, id string, attempts int, next time.Time, errMsg string) error
	MarkFailed(ctx context.Context, id string, attempts int, errMsg string) error
}

type ilinkBindingRepository struct{ db *gorm.DB }

func newIlinkBindingRepository(db *gorm.DB) IlinkBindingRepository {
	return &ilinkBindingRepository{db: db}
}

func (r *ilinkBindingRepository) Create(ctx context.Context, binding *model.IlinkBinding) error {
	return r.db.WithContext(ctx).Create(binding).Error
}

func (r *ilinkBindingRepository) FindByUserID(ctx context.Context, userID string) (*model.IlinkBinding, error) {
	var b model.IlinkBinding
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *ilinkBindingRepository) FindByContact(ctx context.Context, platformAccountID, externalUserID string) (*model.IlinkBinding, error) {
	var b model.IlinkBinding
	if err := r.db.WithContext(ctx).
		Where("platform_account_id = ? AND external_user_id = ? AND status = ?", platformAccountID, externalUserID, model.IlinkBindingStatusActive).
		First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *ilinkBindingRepository) FindByBindCode(ctx context.Context, code string, now time.Time) (*model.IlinkBinding, error) {
	var b model.IlinkBinding
	if err := r.db.WithContext(ctx).
		Where("bind_code = ? AND bind_code_expires_at > ?", code, now).
		First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *ilinkBindingRepository) Update(ctx context.Context, binding *model.IlinkBinding) error {
	return r.db.WithContext(ctx).Save(binding).Error
}

func (r *ilinkBindingRepository) UpdateDefaultProject(ctx context.Context, userID, projectID string) error {
	return r.db.WithContext(ctx).Model(&model.IlinkBinding{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{"default_project_id": projectID, "updated_at": time.Now()}).Error
}

func (r *ilinkBindingRepository) Delete(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.IlinkBinding{}).Error
}

type ilinkNotificationRepository struct{ db *gorm.DB }

func newIlinkNotificationRepository(db *gorm.DB) IlinkNotificationRepository {
	return &ilinkNotificationRepository{db: db}
}

func (r *ilinkNotificationRepository) Enqueue(ctx context.Context, item *model.IlinkNotification) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}, {Name: "task_status"}},
		DoNothing: true,
	}).Create(item).Error
}

func (r *ilinkNotificationRepository) ListDue(ctx context.Context, limit int) ([]*model.IlinkNotification, error) {
	if limit <= 0 {
		limit = 50
	}
	var items []*model.IlinkNotification
	if err := r.db.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ?", model.IlinkNotificationStatusPending, time.Now()).
		Order("next_attempt_at, created_at").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ilinkNotificationRepository) ClaimDue(ctx context.Context, limit int, leaseUntil time.Time) ([]*model.IlinkNotification, error) {
	items, err := r.ListDue(ctx, limit)
	if err != nil || len(items) == 0 {
		return items, err
	}
	claimed := make([]*model.IlinkNotification, 0, len(items))
	for _, item := range items {
		res := r.db.WithContext(ctx).Model(&model.IlinkNotification{}).
			Where("id = ? AND status = ? AND next_attempt_at <= ?", item.ID, model.IlinkNotificationStatusPending, time.Now()).
			Updates(map[string]any{"next_attempt_at": leaseUntil, "updated_at": time.Now()})
		if res.Error != nil {
			return claimed, res.Error
		}
		if res.RowsAffected == 1 {
			claimed = append(claimed, item)
		}
	}
	return claimed, nil
}

func (r *ilinkNotificationRepository) MarkDelivered(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.IlinkNotification{}).Where("id = ?", id).
		Updates(map[string]any{"status": model.IlinkNotificationStatusDelivered, "delivered_at": &now, "updated_at": now}).Error
}

func (r *ilinkNotificationRepository) MarkRetry(ctx context.Context, id string, attempts int, next time.Time, errMsg string) error {
	return r.db.WithContext(ctx).Model(&model.IlinkNotification{}).Where("id = ?", id).
		Updates(map[string]any{"attempts": attempts, "next_attempt_at": next, "last_error": errMsg, "updated_at": time.Now()}).Error
}

func (r *ilinkNotificationRepository) MarkFailed(ctx context.Context, id string, attempts int, errMsg string) error {
	return r.db.WithContext(ctx).Model(&model.IlinkNotification{}).Where("id = ?", id).
		Updates(map[string]any{"status": model.IlinkNotificationStatusFailed, "attempts": attempts, "last_error": errMsg, "updated_at": time.Now()}).Error
}
