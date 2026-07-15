package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

func (r *pendingUploadRepository) FinalizePendingUploads(ctx context.Context, ids []string, finalizedAt time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Model(&model.PendingUpload{}).
		Where("id IN ? AND status = ? AND expires_at > ?", ids, model.PendingUploadStatusPending, finalizedAt).
		Updates(map[string]any{
			"status":       model.PendingUploadStatusFinalized,
			"finalized_at": finalizedAt,
		})
	return result.RowsAffected, result.Error
}

func (r *pendingUploadRepository) FinalizePendingUploadClaims(ctx context.Context, claims []model.PendingUploadClaim, finalizedAt time.Time) error {
	if len(claims) == 0 {
		return nil
	}
	ordered := append([]model.PendingUploadClaim(nil), claims...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UploadID < ordered[j].UploadID })
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, claim := range ordered {
			var upload model.PendingUpload
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", claim.UploadID).First(&upload).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.ErrPendingUploadClaimRejected
			}
			if err != nil {
				return err
			}
			if !pendingUploadMatchesClaim(&upload, claim) {
				return model.ErrPendingUploadClaimRejected
			}
			switch upload.Status {
			case model.PendingUploadStatusFinalized:
				if upload.FinalizedKey == "" || upload.FinalizedKey != claim.FinalizedKey {
					return model.ErrPendingUploadClaimRejected
				}
				continue
			case model.PendingUploadStatusPending:
				if upload.FinalizedKey != "" || !upload.ExpiresAt.After(finalizedAt) {
					return model.ErrPendingUploadClaimRejected
				}
				result := tx.Model(&model.PendingUpload{}).
					Where("id = ? AND status = ? AND finalized_key = ? AND expires_at > ?", upload.ID, model.PendingUploadStatusPending, "", finalizedAt).
					Updates(map[string]any{"status": model.PendingUploadStatusFinalized, "finalized_at": finalizedAt, "finalized_key": claim.FinalizedKey})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					var current model.PendingUpload
					if err := tx.Where("id = ?", upload.ID).First(&current).Error; err != nil {
						return err
					}
					if current.Status != model.PendingUploadStatusFinalized || current.FinalizedKey != claim.FinalizedKey || !pendingUploadMatchesClaim(&current, claim) {
						return model.ErrPendingUploadClaimRejected
					}
				}
			default:
				return model.ErrPendingUploadClaimRejected
			}
		}
		return nil
	})
}

func pendingUploadMatchesClaim(upload *model.PendingUpload, claim model.PendingUploadClaim) bool {
	if upload == nil || upload.ID != claim.UploadID || upload.UserID != claim.UserID || upload.Key != claim.Key || claim.FinalizedKey == "" {
		return false
	}
	for _, purpose := range claim.AllowedPurposes {
		if upload.Purpose == purpose {
			return true
		}
	}
	return false
}

func (r *pendingUploadRepository) FindPendingUploadsForCleanup(ctx context.Context, expiredBefore, claimStaleBefore time.Time, limit int) ([]*model.PendingUpload, error) {
	var uploads []*model.PendingUpload
	err := r.db.WithContext(ctx).
		Where("(status = ? AND expires_at <= ?) OR (status = ? AND cleanup_claimed_at <= ?)",
			model.PendingUploadStatusPending, expiredBefore,
			model.PendingUploadStatusExpiring, claimStaleBefore).
		Order("expires_at ASC").
		Limit(limit).
		Find(&uploads).Error
	return uploads, err
}

func (r *pendingUploadRepository) ClaimPendingUploadExpiration(ctx context.Context, id, claimID string, claimedAt, claimStaleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.PendingUpload{}).
		Where("id = ? AND ((status = ? AND expires_at <= ?) OR (status = ? AND cleanup_claimed_at <= ?))",
			id, model.PendingUploadStatusPending, claimedAt, model.PendingUploadStatusExpiring, claimStaleBefore).
		Updates(map[string]any{
			"cleanup_claim_id":   claimID,
			"status":             model.PendingUploadStatusExpiring,
			"cleanup_claimed_at": claimedAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *pendingUploadRepository) CompletePendingUploadExpiration(ctx context.Context, id, claimID string, expiredAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.PendingUpload{}).
		Where("id = ? AND status = ? AND cleanup_claim_id = ?", id, model.PendingUploadStatusExpiring, claimID).
		Updates(map[string]any{
			"status":             model.PendingUploadStatusExpired,
			"expired_at":         expiredAt,
			"cleanup_claim_id":   "",
			"cleanup_claimed_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *pendingUploadRepository) ReopenPendingUploadExpiration(ctx context.Context, id, claimID string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.PendingUpload{}).
		Where("id = ? AND status = ? AND cleanup_claim_id = ?", id, model.PendingUploadStatusExpiring, claimID).
		Updates(map[string]any{
			"status":             model.PendingUploadStatusPending,
			"cleanup_claim_id":   "",
			"cleanup_claimed_at": nil,
		})
	return result.RowsAffected == 1, result.Error
}
