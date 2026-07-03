package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
)

type pendingUploadRepository struct {
	db *gorm.DB
}

func newPendingUploadRepository(db *gorm.DB) PendingUploadRepository {
	return &pendingUploadRepository{db: db}
}

func (r *pendingUploadRepository) CreatePendingUpload(ctx context.Context, upload *model.PendingUpload) error {
	return r.db.WithContext(ctx).Create(upload).Error
}

func (r *pendingUploadRepository) FindPendingUploadByID(ctx context.Context, id string) (*model.PendingUpload, error) {
	var upload model.PendingUpload
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&upload).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, model.ErrPendingUploadNotFound
	}
	if err != nil {
		return nil, err
	}
	return &upload, nil
}

func (r *pendingUploadRepository) FinalizePendingUploads(ctx context.Context, ids []string, finalizedAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.PendingUpload{}).
		Where("id IN ? AND status = ?", ids, model.PendingUploadStatusPending).
		Updates(map[string]any{
			"status":       model.PendingUploadStatusFinalized,
			"finalized_at": finalizedAt,
		}).Error
}

func (r *pendingUploadRepository) FindExpiredPendingUploads(ctx context.Context, before time.Time, limit int) ([]*model.PendingUpload, error) {
	var uploads []*model.PendingUpload
	err := r.db.WithContext(ctx).
		Where("status = ? AND expires_at <= ?", model.PendingUploadStatusPending, before).
		Order("expires_at ASC").
		Limit(limit).
		Find(&uploads).Error
	return uploads, err
}

func (r *pendingUploadRepository) MarkPendingUploadExpired(ctx context.Context, id string, expiredAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.PendingUpload{}).
		Where("id = ? AND status = ?", id, model.PendingUploadStatusPending).
		Updates(map[string]any{
			"status":     model.PendingUploadStatusExpired,
			"expired_at": expiredAt,
		}).Error
}
