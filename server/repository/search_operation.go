package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type searchOperationRepository struct{ db *gorm.DB }

func newSearchOperationRepository(db *gorm.DB) SearchOperationRepository {
	return &searchOperationRepository{db: db}
}

func (r *searchOperationRepository) FindByFingerprint(ctx context.Context, fingerprint string) (*model.SearchOperation, error) {
	var operation model.SearchOperation
	err := r.db.WithContext(ctx).Where("request_fingerprint = ?", fingerprint).First(&operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &operation, err
}

func (r *searchOperationRepository) Claim(ctx context.Context, candidate *model.SearchOperation, now time.Time, lease time.Duration) (*model.SearchOperation, bool, error) {
	if candidate == nil || lease <= 0 {
		return nil, false, errors.New("search operation candidate and lease are required")
	}
	expires := now.Add(lease)
	candidate.Status = model.SearchOperationRunning
	candidate.LeaseExpiresAt = &expires
	candidate.CreatedAt = now
	candidate.UpdatedAt = now
	if candidate.ID == "" {
		candidate.ID = uuid.NewString()
	}
	if err := candidate.Validate(); err != nil {
		return nil, false, err
	}

	var existing model.SearchOperation
	err := r.db.WithContext(ctx).Where("request_fingerprint = ?", candidate.RequestFingerprint).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "request_fingerprint"}}, DoNothing: true}).Create(candidate)
		if result.Error != nil {
			return nil, false, result.Error
		}
		if result.RowsAffected == 1 {
			return candidate, true, nil
		}
		err = r.db.WithContext(ctx).Where("request_fingerprint = ?", candidate.RequestFingerprint).First(&existing).Error
	}
	if err != nil {
		return nil, false, err
	}
	if existing.Status == model.SearchOperationSucceeded {
		return &existing, false, nil
	}
	if existing.Status == model.SearchOperationRunning && existing.LeaseExpiresAt != nil && existing.LeaseExpiresAt.After(now) {
		return &existing, false, nil
	}
	result := r.db.WithContext(ctx).Model(&model.SearchOperation{}).
		Where("id = ? AND (status = ? OR (status = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)))", existing.ID, model.SearchOperationFailed, model.SearchOperationRunning, now).
		Updates(map[string]any{"user_id": candidate.UserID, "project_id": candidate.ProjectID, "task_id": candidate.TaskID, "execution_id": candidate.ExecutionID, "attempt_id": candidate.AttemptID, "provider_request_id": "", "status": model.SearchOperationRunning, "response_snapshot": nil, "error_code": "", "lease_expires_at": expires, "updated_at": now})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		candidate.ID = existing.ID
		return candidate, true, nil
	}
	latest, findErr := r.FindByFingerprint(ctx, candidate.RequestFingerprint)
	return latest, false, findErr
}

func (r *searchOperationRepository) MarkSucceeded(ctx context.Context, id, attemptID, providerRequestID string, response []byte, now time.Time) error {
	if len(response) == 0 || !jsonValid(response) {
		return errors.New("search operation response snapshot must be valid JSON")
	}
	result := r.db.WithContext(ctx).Model(&model.SearchOperation{}).
		Where("id = ? AND attempt_id = ? AND status = ?", id, attemptID, model.SearchOperationRunning).
		Updates(map[string]any{"status": model.SearchOperationSucceeded, "provider_request_id": providerRequestID, "response_snapshot": response, "error_code": "", "lease_expires_at": nil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	return errors.New("search operation claim was lost before completion")
}

func (r *searchOperationRepository) MarkFailed(ctx context.Context, id, attemptID, errorCode string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&model.SearchOperation{}).
		Where("id = ? AND attempt_id = ? AND status = ?", id, attemptID, model.SearchOperationRunning).
		Updates(map[string]any{"status": model.SearchOperationFailed, "error_code": errorCode, "lease_expires_at": nil, "updated_at": now})
	return result.Error
}

func jsonValid(data []byte) bool {
	var value any
	return json.Unmarshal(data, &value) == nil
}
