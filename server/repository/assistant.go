package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errAssistantSessionLeaseOccupied = errors.New("assistant session lease is occupied")

type AssistantCheckpointRepository interface {
	UpsertBySourceSession(ctx context.Context, checkpoint *model.AssistantCheckpoint) error
	FindByIDForUser(ctx context.Context, userID, id string) (*model.AssistantCheckpoint, error)
	FindCommittedByKey(ctx context.Context, userID, key string) (*model.AssistantCheckpoint, error)
	Update(ctx context.Context, checkpoint *model.AssistantCheckpoint) error
	MarkCommittedBySourceSession(ctx context.Context, userID, sessionID string, at time.Time) error
	CommitIfReady(ctx context.Context, userID, id, commitKey string, resultJSON []byte, at time.Time) (bool, error)
	FindLatest(ctx context.Context, userID, modeID, projectID, contentID string, now time.Time) (*model.AssistantCheckpoint, error)
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

type assistantCheckpointRepository struct{ db *gorm.DB }

func newAssistantCheckpointRepository(db *gorm.DB) AssistantCheckpointRepository {
	return &assistantCheckpointRepository{db: db}
}

func (r *assistantCheckpointRepository) UpsertBySourceSession(ctx context.Context, checkpoint *model.AssistantCheckpoint) error {
	checkpoint.PayloadDigest = fmt.Sprintf("%x", sha256.Sum256(checkpoint.Payload))
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source_session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"schema_version", "payload", "summary", "status", "base_profile_revision",
			"base_content_revision", "base_analytics_revision", "expires_at", "updated_at",
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Neq{Column: clause.Column{Name: "status"}, Value: model.AssistantCheckpointCommitted},
		}},
	}).Create(checkpoint).Error
}

func (r *assistantCheckpointRepository) FindByIDForUser(ctx context.Context, userID, id string) (*model.AssistantCheckpoint, error) {
	var checkpoint model.AssistantCheckpoint
	if err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).First(&checkpoint).Error; err != nil {
		return nil, err
	}
	checkpoint.PayloadDigest = fmt.Sprintf("%x", sha256.Sum256(checkpoint.Payload))
	return &checkpoint, nil
}

func (r *assistantCheckpointRepository) FindCommittedByKey(ctx context.Context, userID, key string) (*model.AssistantCheckpoint, error) {
	var checkpoint model.AssistantCheckpoint
	if err := r.db.WithContext(ctx).Where("commit_user_id = ? AND commit_idempotency_key = ? AND status = ?", userID, key, model.AssistantCheckpointCommitted).First(&checkpoint).Error; err != nil {
		return nil, err
	}
	checkpoint.PayloadDigest = fmt.Sprintf("%x", sha256.Sum256(checkpoint.Payload))
	return &checkpoint, nil
}

func (r *assistantCheckpointRepository) Update(ctx context.Context, checkpoint *model.AssistantCheckpoint) error {
	return r.db.WithContext(ctx).Save(checkpoint).Error
}

func (r *assistantCheckpointRepository) MarkCommittedBySourceSession(ctx context.Context, userID, sessionID string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantCheckpoint{}).
		Where("user_id = ? AND source_session_id = ? AND status IN ?", userID, sessionID, []string{model.AssistantCheckpointDraft, model.AssistantCheckpointReady}).
		Updates(map[string]any{"status": model.AssistantCheckpointCommitted, "updated_at": at}).Error
}

func (r *assistantCheckpointRepository) CommitIfReady(ctx context.Context, userID, id, commitKey string, resultJSON []byte, at time.Time) (bool, error) {
	commitUserID := strings.TrimSpace(userID)
	checkpointID := strings.TrimSpace(id)
	key := strings.TrimSpace(commitKey)
	if commitUserID == "" || checkpointID == "" || key == "" || len(resultJSON) == 0 {
		return false, errors.New("assistant checkpoint commit identity is required")
	}
	result := r.db.WithContext(ctx).Model(&model.AssistantCheckpoint{}).
		Where("id = ? AND user_id = ? AND status = ?", checkpointID, commitUserID, model.AssistantCheckpointReady).
		Updates(map[string]any{
			"commit_user_id":         commitUserID,
			"commit_idempotency_key": key,
			"commit_result_json":     resultJSON,
			"status":                 model.AssistantCheckpointCommitted,
			"updated_at":             at,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *assistantCheckpointRepository) FindLatest(ctx context.Context, userID, modeID, projectID, contentID string, now time.Time) (*model.AssistantCheckpoint, error) {
	var checkpoint model.AssistantCheckpoint
	query := r.db.WithContext(ctx).
		Where("user_id = ? AND mode_id = ? AND project_id = ? AND content_id = ? AND expires_at > ?", userID, modeID, projectID, contentID, now).
		Where("status IN ?", []string{model.AssistantCheckpointDraft, model.AssistantCheckpointReady}).
		Order("updated_at DESC").Order("id DESC")
	if err := query.First(&checkpoint).Error; err != nil {
		return nil, err
	}
	checkpoint.PayloadDigest = fmt.Sprintf("%x", sha256.Sum256(checkpoint.Payload))
	return &checkpoint, nil
}

func (r *assistantCheckpointRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("expires_at <= ? AND status <> ?", now, model.AssistantCheckpointCommitted).Delete(&model.AssistantCheckpoint{})
	return result.RowsAffected, result.Error
}

type AssistantSessionRepository interface {
	CreateWithLease(ctx context.Context, session *model.AssistantSession, lease *model.AssistantSessionLease, now time.Time) (bool, error)
	FindByIDForUser(ctx context.Context, userID, sessionID string) (*model.AssistantSession, error)
	FindLeaseBySession(ctx context.Context, sessionID string) (*model.AssistantSessionLease, error)
	RenewLease(ctx context.Context, sessionID string, now, expiresAt time.Time) (bool, error)
	SetStatus(ctx context.Context, sessionID, status string, updatedAt time.Time) error
	SetCheckpoint(ctx context.Context, sessionID, checkpointID string, updatedAt time.Time) error
	Close(ctx context.Context, sessionID, status string, at time.Time) error
	ReapExpired(ctx context.Context, now time.Time) error
	InterruptActive(ctx context.Context, at time.Time) error
}

type assistantSessionRepository struct{ db *gorm.DB }

func newAssistantSessionRepository(db *gorm.DB) AssistantSessionRepository {
	return &assistantSessionRepository{db: db}
}

func (r *assistantSessionRepository) CreateWithLease(ctx context.Context, session *model.AssistantSession, lease *model.AssistantSessionLease, now time.Time) (bool, error) {
	if session == nil || lease == nil || strings.TrimSpace(session.ID) == "" || strings.TrimSpace(session.UserID) == "" || strings.TrimSpace(session.ModeID) == "" || strings.TrimSpace(lease.Scope) == "" || lease.SessionID != session.ID || !lease.ExpiresAt.After(now) {
		return false, errors.New("assistant session and lease scope are required")
	}
	won := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scope = ? AND expires_at <= ?", lease.Scope, now).Delete(&model.AssistantSessionLease{}).Error; err != nil {
			return err
		}
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope"}}, DoNothing: true}).Create(lease)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errAssistantSessionLeaseOccupied
		}
		won = true
		return nil
	})
	if errors.Is(err, errAssistantSessionLeaseOccupied) {
		return false, nil
	}
	return won, err
}

func (r *assistantSessionRepository) FindByIDForUser(ctx context.Context, userID, sessionID string) (*model.AssistantSession, error) {
	var session model.AssistantSession
	if err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, sessionID).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *assistantSessionRepository) FindLeaseBySession(ctx context.Context, sessionID string) (*model.AssistantSessionLease, error) {
	var lease model.AssistantSessionLease
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&lease).Error; err != nil {
		return nil, err
	}
	return &lease, nil
}

func (r *assistantSessionRepository) RenewLease(ctx context.Context, sessionID string, now, expiresAt time.Time) (bool, error) {
	if strings.TrimSpace(sessionID) == "" || !expiresAt.After(now) {
		return false, errors.New("assistant session lease renewal is invalid")
	}
	renewed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var active model.AssistantSession
		if err := tx.Where("id = ? AND closed_at IS NULL", sessionID).First(&active).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		result := tx.Model(&model.AssistantSessionLease{}).
			Where("session_id = ? AND expires_at > ?", sessionID, now).
			Update("expires_at", expiresAt)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		if err := tx.Model(&model.AssistantSession{}).Where("id = ? AND closed_at IS NULL", sessionID).
			Updates(map[string]any{"expires_at": expiresAt, "updated_at": now}).Error; err != nil {
			return err
		}
		renewed = true
		return nil
	})
	return renewed, err
}

func (r *assistantSessionRepository) SetStatus(ctx context.Context, sessionID, status string, updatedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantSession{}).
		Where("id = ? AND closed_at IS NULL", sessionID).
		Updates(map[string]any{"status": status, "updated_at": updatedAt}).Error
}

func (r *assistantSessionRepository) SetCheckpoint(ctx context.Context, sessionID, checkpointID string, updatedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantSession{}).
		Where("id = ? AND closed_at IS NULL", sessionID).
		Updates(map[string]any{"checkpoint_id": checkpointID, "updated_at": updatedAt}).Error
}

func (r *assistantSessionRepository) Close(ctx context.Context, sessionID, status string, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AssistantSession{}).Where("id = ? AND closed_at IS NULL", sessionID).
			Updates(map[string]any{"status": status, "closed_at": at, "updated_at": at}).Error; err != nil {
			return err
		}
		return tx.Where("session_id = ?", sessionID).Delete(&model.AssistantSessionLease{}).Error
	})
}

func (r *assistantSessionRepository) ReapExpired(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AssistantSession{}).
			Where("closed_at IS NULL AND expires_at <= ?", now).
			Updates(map[string]any{"status": "expired", "closed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Where("expires_at <= ?", now).Delete(&model.AssistantSessionLease{}).Error
	})
}

func (r *assistantSessionRepository) InterruptActive(ctx context.Context, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AssistantSession{}).
			Where("closed_at IS NULL").
			Updates(map[string]any{"status": "interrupted", "closed_at": at, "updated_at": at}).Error; err != nil {
			return err
		}
		return tx.Where("scope <> ?", "").Delete(&model.AssistantSessionLease{}).Error
	})
}
