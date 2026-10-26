package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInteractionAttemptConflict = errors.New("assistant interaction attempt payload conflict")

type AssistantInteractionRepository interface {
	Create(context.Context, *model.AssistantInteraction) error
	FindByID(context.Context, string) (*model.AssistantInteraction, error)
	FindByIDForUser(context.Context, string, string) (*model.AssistantInteraction, error)
	FindByScopeForUpdate(context.Context, string, string, string, string) (*model.AssistantInteraction, error)
	UpdateLifecycle(context.Context, string, string, time.Time) error
	CompareAndSwapGeneration(context.Context, string, int64, int64, time.Time) (bool, error)
	CreateRuntimeBinding(context.Context, *model.AssistantInteractionRuntime) error
	FindCurrentRuntime(context.Context, string, int64) (*model.AssistantInteractionRuntime, error)
	CloseRuntime(context.Context, string, time.Time) error
	CreateDraftIfAttemptNew(context.Context, *model.AssistantInteractionDraft, *model.AssistantInteractionAttempt) (*model.AssistantInteractionDraft, bool, error)
	FindDraftForUser(context.Context, string, string) (*model.AssistantInteractionDraft, error)
	FindLatestDraft(context.Context, string, string, string) (*model.AssistantInteractionDraft, error)
	CreateEvent(context.Context, *model.AssistantInteractionEvent) error
	ListEventsAfter(context.Context, string, uint64, int) ([]*model.AssistantInteractionEvent, error)
	CreateAttempt(context.Context, *model.AssistantInteractionAttempt) error
	FindAttempt(context.Context, string, string) (*model.AssistantInteractionAttempt, error)
	ExpireAttempts(context.Context, time.Time) (int64, error)
	ExpireEvents(context.Context, time.Time) (int64, error)
}

type assistantInteractionRepository struct{ db *gorm.DB }

func newAssistantInteractionRepository(db *gorm.DB) AssistantInteractionRepository {
	return &assistantInteractionRepository{db: db}
}

func (r *assistantInteractionRepository) Create(ctx context.Context, value *model.AssistantInteraction) error {
	if value == nil || strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.UserID) == "" {
		return errors.New("assistant interaction identity is required")
	}
	if strings.TrimSpace(value.Scope) == "" {
		value.Scope = value.UserID + ":" + value.ModeID + ":" + value.ProjectID + ":" + value.ContentID
	}
	if value.ActiveScope == "" && value.Lifecycle != model.AssistantInteractionAbandoned && value.Lifecycle != model.AssistantInteractionCompleted {
		value.ActiveScope = value.Scope
	}
	return r.db.WithContext(ctx).Create(value).Error
}
func (r *assistantInteractionRepository) FindByIDForUser(ctx context.Context, userID, id string) (*model.AssistantInteraction, error) {
	var value model.AssistantInteraction
	if err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).First(&value).Error; err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) FindByID(ctx context.Context, id string) (*model.AssistantInteraction, error) {
	var value model.AssistantInteraction
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&value).Error; err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) FindByScopeForUpdate(ctx context.Context, userID, modeID, projectID, contentID string) (*model.AssistantInteraction, error) {
	var value model.AssistantInteraction
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND mode_id = ? AND project_id = ? AND content_id = ? AND closed_at IS NULL", userID, modeID, projectID, contentID).Where("lifecycle IN ?", []string{model.AssistantInteractionActive, model.AssistantInteractionReconnecting, model.AssistantInteractionNeedsConfirmation}).Order("updated_at DESC").First(&value).Error
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) UpdateLifecycle(ctx context.Context, id, lifecycle string, at time.Time) error {
	var value model.AssistantInteraction
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&value).Error; err != nil {
		return err
	}
	activeScope := ""
	if lifecycle != model.AssistantInteractionAbandoned && lifecycle != model.AssistantInteractionCompleted {
		activeScope = value.Scope
	}
	return r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).Where("id = ?", id).Updates(map[string]any{"lifecycle": lifecycle, "active_scope": activeScope, "last_activity_at": at, "updated_at": at}).Error
}
func (r *assistantInteractionRepository) CompareAndSwapGeneration(ctx context.Context, id string, expected, next int64, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).Where("id = ? AND generation = ? AND closed_at IS NULL", id, expected).Updates(map[string]any{"generation": next, "last_activity_at": at, "updated_at": at})
	return result.RowsAffected == 1, result.Error
}
func (r *assistantInteractionRepository) CreateRuntimeBinding(ctx context.Context, value *model.AssistantInteractionRuntime) error {
	if value == nil || strings.TrimSpace(value.InteractionID) == "" || value.Generation < 1 {
		return errors.New("assistant interaction runtime identity is required")
	}
	return r.db.WithContext(ctx).Create(value).Error
}
func (r *assistantInteractionRepository) FindCurrentRuntime(ctx context.Context, interactionID string, generation int64) (*model.AssistantInteractionRuntime, error) {
	var value model.AssistantInteractionRuntime
	err := r.db.WithContext(ctx).Where("interaction_id = ? AND generation = ? AND status IN ?", interactionID, generation, []string{model.AssistantInteractionRuntimeStarting, model.AssistantInteractionRuntimeRunning}).Order("created_at DESC").First(&value).Error
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) CloseRuntime(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantInteractionRuntime{}).Where("id = ? AND closed_at IS NULL", id).Updates(map[string]any{"status": model.AssistantInteractionRuntimeClosed, "closed_at": at, "updated_at": at}).Error
}
func (r *assistantInteractionRepository) CreateDraftIfAttemptNew(ctx context.Context, draft *model.AssistantInteractionDraft, attempt *model.AssistantInteractionAttempt) (*model.AssistantInteractionDraft, bool, error) {
	if draft == nil || attempt == nil {
		return nil, false, errors.New("assistant interaction draft and attempt are required")
	}
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, false, tx.Error
	}
	var existing model.AssistantInteractionAttempt
	err := tx.Where("interaction_id = ? AND attempt_id = ?", attempt.InteractionID, attempt.AttemptID).First(&existing).Error
	if err == nil {
		if existing.PayloadDigest != attempt.PayloadDigest {
			_ = tx.Rollback()
			return nil, false, ErrInteractionAttemptConflict
		}
		var result model.AssistantInteractionDraft
		if decodeErr := json.Unmarshal(existing.Result, &result); decodeErr != nil {
			_ = tx.Rollback()
			return nil, false, decodeErr
		}
		_ = tx.Rollback()
		return &result, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		_ = tx.Rollback()
		return nil, false, err
	}
	if err := tx.Create(draft).Error; err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	attempt.Result = encoded
	if err := tx.Create(attempt).Error; err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, false, err
	}
	return draft, true, nil
}
func (r *assistantInteractionRepository) FindDraftForUser(ctx context.Context, userID, id string) (*model.AssistantInteractionDraft, error) {
	var value model.AssistantInteractionDraft
	err := r.db.WithContext(ctx).Table("assistant_interaction_drafts").Joins("JOIN assistant_interactions ON assistant_interactions.id = assistant_interaction_drafts.interaction_id").Where("assistant_interactions.user_id = ? AND assistant_interaction_drafts.id = ?", userID, id).First(&value).Error
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) FindLatestDraft(ctx context.Context, userID, interactionID, modeID string) (*model.AssistantInteractionDraft, error) {
	var value model.AssistantInteractionDraft
	err := r.db.WithContext(ctx).Table("assistant_interaction_drafts").Joins("JOIN assistant_interactions ON assistant_interactions.id = assistant_interaction_drafts.interaction_id").Where("assistant_interactions.user_id = ? AND assistant_interaction_drafts.interaction_id = ? AND assistant_interaction_drafts.mode_id = ?", userID, interactionID, modeID).Order("revision DESC").First(&value).Error
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) CreateEvent(ctx context.Context, value *model.AssistantInteractionEvent) error {
	return r.db.WithContext(ctx).Create(value).Error
}
func (r *assistantInteractionRepository) ListEventsAfter(ctx context.Context, interactionID string, after uint64, limit int) ([]*model.AssistantInteractionEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	var values []*model.AssistantInteractionEvent
	err := r.db.WithContext(ctx).Where("interaction_id = ? AND sequence > ?", interactionID, after).Order("sequence ASC").Limit(limit).Find(&values).Error
	return values, err
}
func (r *assistantInteractionRepository) CreateAttempt(ctx context.Context, value *model.AssistantInteractionAttempt) error {
	return r.db.WithContext(ctx).Create(value).Error
}
func (r *assistantInteractionRepository) FindAttempt(ctx context.Context, interactionID, attemptID string) (*model.AssistantInteractionAttempt, error) {
	var value model.AssistantInteractionAttempt
	if err := r.db.WithContext(ctx).Where("interaction_id = ? AND attempt_id = ?", interactionID, attemptID).First(&value).Error; err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) ExpireAttempts(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("expires_at <= ?", before).Delete(&model.AssistantInteractionAttempt{})
	return result.RowsAffected, result.Error
}
func (r *assistantInteractionRepository) ExpireEvents(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("created_at <= ?", before).Delete(&model.AssistantInteractionEvent{})
	return result.RowsAffected, result.Error
}
