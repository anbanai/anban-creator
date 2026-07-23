package repository

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	err := r.db.WithContext(ctx).
		Where("(status = ? AND expires_at <= ?) OR (status = ? AND cleanup_claimed_at <= ?) OR (status = ? AND expires_at <= ? AND finalization_claimed_at <= ?)",
			model.UploadSessionPending, expiredBefore,
			model.UploadSessionExpiring, claimStaleBefore,
			model.UploadSessionFinalizing, expiredBefore, claimStaleBefore).
		Order("expires_at ASC").
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

func (r *uploadSessionRepository) ClaimExpiration(ctx context.Context, id, claimID string, claimedAt, claimStaleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND ((status = ? AND expires_at <= ?) OR (status = ? AND cleanup_claimed_at <= ?) OR (status = ? AND expires_at <= ? AND finalization_claimed_at <= ?))",
			id, model.UploadSessionPending, claimedAt,
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
		})
	return result.RowsAffected == 1, result.Error
}

func (r *uploadSessionRepository) DeferExpiration(ctx context.Context, id, claimID string, retryAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id = ? AND status = ? AND cleanup_claim_id = ?", id, model.UploadSessionExpiring, claimID).
		Updates(map[string]any{
			"status":             model.UploadSessionPending,
			"expires_at":         retryAt,
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

func (r *uploadSessionRepository) AdoptTaskArtifactManifest(ctx context.Context, userID, executionPrefix string, claims []TaskArtifactSessionClaim, now time.Time) error {
	userID = strings.TrimSpace(userID)
	executionPrefix = strings.TrimSpace(executionPrefix)
	if userID == "" || executionPrefix == "" || !strings.HasSuffix(executionPrefix, "/") || now.IsZero() {
		return fmt.Errorf("%w: manifest identity is incomplete", ErrTaskArtifactSessionInvalid)
	}

	claimByID := make(map[string]TaskArtifactSessionClaim, len(claims))
	ids := make([]string, 0, len(claims))
	pendingIDs := make([]string, 0, len(claims))
	for _, claim := range claims {
		claim.ID = strings.TrimSpace(claim.ID)
		claim.StagingKey = strings.TrimSpace(claim.StagingKey)
		claim.FileName = strings.TrimSpace(claim.FileName)
		claim.ContentType = strings.TrimSpace(claim.ContentType)
		parsed, err := uuid.Parse(claim.ID)
		if err != nil || parsed.String() != claim.ID || claim.StagingKey == "" || claim.FileName == "" || claim.ContentType == "" || claim.Size <= 0 ||
			!strings.HasPrefix(claim.StagingKey, executionPrefix) || path.Base(claim.StagingKey) != claim.ID || path.Base(path.Dir(claim.StagingKey)) != "attempts" {
			return fmt.Errorf("%w: malformed stream attempt claim", ErrTaskArtifactSessionInvalid)
		}
		if _, duplicate := claimByID[claim.ID]; duplicate {
			return fmt.Errorf("%w: duplicate stream attempt claim", ErrTaskArtifactSessionInvalid)
		}
		claimByID[claim.ID] = claim
		ids = append(ids, claim.ID)
	}

	if len(ids) > 0 {
		var sessions []model.UploadSession
		if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Find(&sessions).Error; err != nil {
			return err
		}
		if len(sessions) != len(ids) {
			return fmt.Errorf("%w: stream attempt is not registered", ErrTaskArtifactSessionInvalid)
		}
		for _, session := range sessions {
			claim, ok := claimByID[session.ID]
			if !ok || session.UserID != userID || session.Purpose != "task_artifact" || session.StagingKey != claim.StagingKey ||
				session.FileName != claim.FileName || strings.TrimSpace(session.ContentType) != claim.ContentType || session.Size != claim.Size {
				return fmt.Errorf("%w: stream attempt metadata does not match", ErrTaskArtifactSessionInvalid)
			}
			switch session.Status {
			case model.UploadSessionPending:
				if !session.ExpiresAt.After(now) {
					return fmt.Errorf("%w: stream attempt has expired", ErrTaskArtifactSessionInvalid)
				}
				pendingIDs = append(pendingIDs, session.ID)
			case model.UploadSessionFinalized:
			default:
				return fmt.Errorf("%w: stream attempt is being cleaned", ErrTaskArtifactSessionInvalid)
			}
		}
	}

	displaced := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("user_id = ? AND purpose = ? AND status = ? AND staging_key LIKE ?", userID, "task_artifact", model.UploadSessionFinalized, executionPrefix+"%")
	if len(ids) > 0 {
		displaced = displaced.Where("id NOT IN ?", ids)
	}
	if err := displaced.Updates(map[string]any{
		"status":                  model.UploadSessionPending,
		"expires_at":              now,
		"asset_id":                "",
		"finalized_at":            nil,
		"finalization_token":      "",
		"finalization_claimed_at": nil,
		"cleanup_claim_id":        "",
		"cleanup_claimed_at":      nil,
		"expired_at":              nil,
	}).Error; err != nil {
		return err
	}
	if len(pendingIDs) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&model.UploadSession{}).
		Where("id IN ? AND status = ?", pendingIDs, model.UploadSessionPending).
		Updates(map[string]any{
			"status":                  model.UploadSessionFinalized,
			"finalized_at":            now,
			"finalization_token":      "",
			"finalization_claimed_at": nil,
			"cleanup_claim_id":        "",
			"cleanup_claimed_at":      nil,
			"expired_at":              nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != int64(len(pendingIDs)) {
		return fmt.Errorf("%w: stream attempt adoption lost its state fence", ErrTaskArtifactSessionInvalid)
	}
	return nil
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
			"asset_id":                "",
			"finalized_at":            nil,
			"finalization_token":      "",
			"finalization_claimed_at": nil,
			"cleanup_claim_id":        "",
			"cleanup_claimed_at":      nil,
			"expired_at":              nil,
		}).Error
}
