package repository

import (
	"context"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
)

type uploadSessionRepository struct {
	db *gorm.DB
}

func newUploadSessionRepository(db *gorm.DB) UploadSessionRepository {
	return &uploadSessionRepository{db: db}
}

func (r *uploadSessionRepository) Create(ctx context.Context, session *model.UploadSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *uploadSessionRepository) FindByID(ctx context.Context, id string) (*model.UploadSession, error) {
	var session model.UploadSession
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, model.ErrUploadSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *uploadSessionRepository) ClaimFinalization(ctx context.Context, id, token string, claimedAt, claimStaleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND ((status = ? AND expires_at > ?) OR (status = ? AND finalization_claimed_at <= ?))",
			id, model.UploadSessionPending, claimedAt, model.UploadSessionFinalizing, claimStaleBefore).
		Updates(map[string]any{
			"status":                  model.UploadSessionFinalizing,
			"finalization_token":      token,
			"finalization_claimed_at": claimedAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) CompleteFinalization(ctx context.Context, id, token, assetID string, finalizedAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND finalization_token = ?", id, model.UploadSessionFinalizing, token).
		Updates(map[string]any{
			"status":                  model.UploadSessionFinalized,
			"asset_id":                assetID,
			"finalized_at":            finalizedAt,
			"finalization_token":      "",
			"finalization_claimed_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) ReleaseFinalization(ctx context.Context, id, token string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND finalization_token = ?", id, model.UploadSessionFinalizing, token).
		Updates(map[string]any{
			"status":                  model.UploadSessionPending,
			"finalization_token":      "",
			"finalization_claimed_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) FindForCleanup(ctx context.Context, expiredBefore, claimStaleBefore time.Time, limit int) ([]*model.UploadSession, error) {
	var sessions []*model.UploadSession
	err := r.db.WithContext(ctx).
		Where("(status = ? AND expires_at <= ?) OR (status = ? AND cleanup_claimed_at <= ?)",
			model.UploadSessionPending, expiredBefore,
			model.UploadSessionExpiring, claimStaleBefore).
		Order("expires_at ASC").
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

func (r *uploadSessionRepository) ClaimExpiration(ctx context.Context, id, claimID string, claimedAt, claimStaleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND ((status = ? AND expires_at <= ?) OR (status = ? AND cleanup_claimed_at <= ?))",
			id, model.UploadSessionPending, claimedAt, model.UploadSessionExpiring, claimStaleBefore).
		Updates(map[string]any{
			"status":             model.UploadSessionExpiring,
			"cleanup_claim_id":   claimID,
			"cleanup_claimed_at": claimedAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) CompleteExpiration(ctx context.Context, id, claimID string, expiredAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND cleanup_claim_id = ?", id, model.UploadSessionExpiring, claimID).
		Updates(map[string]any{
			"status":             model.UploadSessionExpired,
			"expired_at":         expiredAt,
			"cleanup_claim_id":   "",
			"cleanup_claimed_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) ReopenExpiration(ctx context.Context, id, claimID string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND cleanup_claim_id = ?", id, model.UploadSessionExpiring, claimID).
		Updates(map[string]any{
			"status":             model.UploadSessionPending,
			"cleanup_claim_id":   "",
			"cleanup_claimed_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}
