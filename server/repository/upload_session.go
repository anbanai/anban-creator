package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
)

var ErrTaskArtifactSessionInvalid = errors.New("task artifact upload session is invalid")

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

func (r *uploadSessionRepository) RecordFinalizationETag(ctx context.Context, id, token, etag string) (bool, error) {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return false, nil
	}
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND finalization_token = ?", id, model.UploadSessionFinalizing, token).
		Update("finalization_etag", etag)
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) RecordPromotionSourceETag(ctx context.Context, id, token, etag string) (bool, error) {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return false, nil
	}
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND finalization_token = ?", id, model.UploadSessionFinalizing, token).
		Update("promotion_source_etag", etag)
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) ClaimFinalizationRecovery(ctx context.Context, id, token string, claimedAt, claimStaleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND (finalization_etag <> '' OR promotion_source_etag <> '') AND (status = ? OR (status = ? AND expires_at <= ?) OR (status = ? AND finalization_claimed_at <= ?))",
			id, model.UploadSessionExpired, model.UploadSessionPending, claimedAt, model.UploadSessionFinalizing, claimStaleBefore).
		Updates(map[string]any{
			"status":                  model.UploadSessionFinalizing,
			"finalization_token":      token,
			"finalization_claimed_at": claimedAt,
			"cleanup_claim_id":        "",
			"cleanup_claimed_at":      nil,
			"expired_at":              nil,
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
	err := buildUploadSessionCleanupQuery(r.db.WithContext(ctx).Model(&model.UploadSession{}), expiredBefore, claimStaleBefore).
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

func buildUploadSessionCleanupQuery(db *gorm.DB, expiredBefore, claimStaleBefore time.Time) *gorm.DB {
	return db.
		Where("(status = ? AND expires_at <= ? AND (next_cleanup_at IS NULL OR next_cleanup_at <= ?)) OR (status = ? AND cleanup_claimed_at <= ?) OR (status = ? AND expires_at <= ? AND finalization_claimed_at <= ?)",
			model.UploadSessionPending, expiredBefore, expiredBefore,
			model.UploadSessionExpiring, claimStaleBefore,
			model.UploadSessionFinalizing, expiredBefore, claimStaleBefore).
		Order("COALESCE(next_cleanup_at, expires_at) ASC, expires_at ASC, id ASC")
}

func (r *uploadSessionRepository) ClaimExpiration(ctx context.Context, id, claimID string, claimedAt, claimStaleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND ((status = ? AND expires_at <= ? AND (next_cleanup_at IS NULL OR next_cleanup_at <= ?)) OR (status = ? AND cleanup_claimed_at <= ?) OR (status = ? AND expires_at <= ? AND finalization_claimed_at <= ?))",
			id, model.UploadSessionPending, claimedAt, claimedAt,
			model.UploadSessionExpiring, claimStaleBefore,
			model.UploadSessionFinalizing, claimedAt, claimStaleBefore).
		Updates(map[string]any{
			"status":                  model.UploadSessionExpiring,
			"cleanup_claim_id":        claimID,
			"cleanup_claimed_at":      claimedAt,
			"finalization_token":      "",
			"finalization_claimed_at": nil,
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
			"next_cleanup_at":    nil,
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

func (r *uploadSessionRepository) RescheduleExpiration(ctx context.Context, id, claimID string, nextCleanupAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND cleanup_claim_id = ?", id, model.UploadSessionExpiring, claimID).
		Updates(map[string]any{
			"status":             model.UploadSessionPending,
			"cleanup_claim_id":   "",
			"cleanup_claimed_at": nil,
			"next_cleanup_at":    nextCleanupAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) ScheduleTaskArtifactExpiration(ctx context.Context, id string, expiresAt time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" || expiresAt.IsZero() {
		return fmt.Errorf("%w: stream attempt expiration is incomplete", ErrTaskArtifactSessionInvalid)
	}
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND purpose = ? AND status IN ?", id, "task_artifact", []string{model.UploadSessionPending, model.UploadSessionFinalized}).
		Updates(map[string]any{
			"status":                  model.UploadSessionPending,
			"expires_at":              expiresAt,
			"next_cleanup_at":         nil,
			"asset_id":                "",
			"finalized_at":            nil,
			"finalization_token":      "",
			"finalization_claimed_at": nil,
			"cleanup_claim_id":        "",
			"cleanup_claimed_at":      nil,
			"expired_at":              nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: stream attempt cannot be scheduled for expiration", ErrTaskArtifactSessionInvalid)
	}
	return nil
}

func (r *uploadSessionRepository) ScheduleTaskArtifactPrefixExpiration(ctx context.Context, userID, stagingPrefix string, expiresAt time.Time) error {
	userID = strings.TrimSpace(userID)
	stagingPrefix = strings.TrimSpace(stagingPrefix)
	if userID == "" || stagingPrefix == "" || !strings.HasSuffix(stagingPrefix, "/") || expiresAt.IsZero() {
		return fmt.Errorf("%w: stream attempt prefix expiration is incomplete", ErrTaskArtifactSessionInvalid)
	}
	return r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("user_id = ? AND purpose = ? AND status IN ? AND staging_key LIKE ?", userID, "task_artifact", []string{model.UploadSessionPending, model.UploadSessionFinalized}, stagingPrefix+"%").
		Updates(map[string]any{
			"status":                  model.UploadSessionPending,
			"expires_at":              expiresAt,
			"next_cleanup_at":         nil,
			"asset_id":                "",
			"finalized_at":            nil,
			"finalization_token":      "",
			"finalization_claimed_at": nil,
			"cleanup_claim_id":        "",
			"cleanup_claimed_at":      nil,
			"expired_at":              nil,
		}).Error
}
