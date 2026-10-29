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
var ErrInteractionGenerationChanged = errors.New("assistant interaction generation changed")
var ErrInteractionTerminal = errors.New("assistant interaction is terminal")

type AssistantInteractionRepository interface {
	Create(context.Context, *model.AssistantInteraction) error
	FindOrCreateActive(context.Context, *model.AssistantInteraction) (*model.AssistantInteraction, bool, error)
	FindByID(context.Context, string) (*model.AssistantInteraction, error)
	FindByIDForUser(context.Context, string, string) (*model.AssistantInteraction, error)
	FindByScopeForUpdate(context.Context, string, string, string, string) (*model.AssistantInteraction, error)
	UpdateLifecycle(context.Context, string, string, time.Time) error
	MarkNeedsConfirmation(context.Context, string, time.Time) error
	UpdateMode(context.Context, string, string, time.Time) error
	UpdateContext(context.Context, string, string, string, string, time.Time) error
	UpdateProject(context.Context, string, string, time.Time) error
	CompareAndSwapGeneration(context.Context, string, int64, int64, time.Time, time.Time) (bool, error)
	CreateRuntimeBinding(context.Context, *model.AssistantInteractionRuntime) error
	FindCurrentRuntime(context.Context, string, int64) (*model.AssistantInteractionRuntime, error)
	CloseRuntime(context.Context, string, time.Time) error
	CreateDraftIfAttemptNew(context.Context, *model.AssistantInteractionDraft, *model.AssistantInteractionAttempt) (*model.AssistantInteractionDraft, bool, error)
	CreateDraftIfAttemptNewAtGeneration(context.Context, *model.AssistantInteractionDraft, *model.AssistantInteractionAttempt, int64) (*model.AssistantInteractionDraft, bool, error)
	CreateDraftAttemptEventAtGeneration(context.Context, *model.AssistantInteractionDraft, *model.AssistantInteractionAttempt, *model.AssistantInteractionEvent, int64) (*model.AssistantInteractionDraft, *model.AssistantInteractionEvent, bool, error)
	FindDraftForUser(context.Context, string, string) (*model.AssistantInteractionDraft, error)
	FindLatestDraft(context.Context, string, string, string) (*model.AssistantInteractionDraft, error)
	ListDrafts(context.Context, string, string) ([]*model.AssistantInteractionDraft, error)
	ExpireDrafts(context.Context, time.Time) (int64, error)
	UpdateDraftState(context.Context, string, string, time.Time) error
	CreateEvent(context.Context, *model.AssistantInteractionEvent) error
	AppendEvent(context.Context, string, int64, *model.AssistantInteractionEvent) (bool, error)
	NextEventSequence(context.Context, string) (uint64, error)
	ListEventsAfter(context.Context, string, int64, uint64, int) ([]*model.AssistantInteractionEvent, error)
	CreateAttempt(context.Context, *model.AssistantInteractionAttempt) error
	FindAttempt(context.Context, string, string) (*model.AssistantInteractionAttempt, error)
	FindCreateAttempt(context.Context, string, string) (*model.AssistantInteractionAttempt, error)
	UpdateAttempt(context.Context, string, string, string, json.RawMessage, string) error
	FindLatestCommitAttempt(context.Context, string) (*model.AssistantInteractionAttempt, error)
	FindLatestCommandOutcomeUnknown(context.Context, string) (*model.AssistantInteractionAttempt, error)
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
		value.Scope = value.UserID + ":" + value.ProjectID + ":" + value.ContentID
	}
	if value.ActiveScope == nil && value.Lifecycle != model.AssistantInteractionAbandoned && value.Lifecycle != model.AssistantInteractionCompleted {
		scope := value.Scope
		value.ActiveScope = &scope
	}
	return r.db.WithContext(ctx).Create(value).Error
}

// FindOrCreateActive uses the unique active-scope index as the cross-process
// coordination boundary. The service mutex still serializes calls within one
// process, while the duplicate-key lookup makes a concurrent second creator
// recover the row created by another Server instance.
func (r *assistantInteractionRepository) FindOrCreateActive(ctx context.Context, value *model.AssistantInteraction) (*model.AssistantInteraction, bool, error) {
	if value == nil || strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.UserID) == "" || strings.TrimSpace(value.ModeID) == "" {
		return nil, false, errors.New("assistant interaction identity is required")
	}
	if strings.TrimSpace(value.Scope) == "" {
		value.Scope = value.UserID + ":" + value.ProjectID + ":" + value.ContentID
	}
	activeScope := value.Scope
	value.ActiveScope = &activeScope
	var existing model.AssistantInteraction
	err := r.db.WithContext(ctx).Where("active_scope = ?", activeScope).Order("updated_at DESC").First(&existing).Error
	if err == nil {
		return &existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if err := r.db.WithContext(ctx).Create(value).Error; err == nil {
		return value, true, nil
	} else if !isAssistantInteractionDuplicate(err) {
		return nil, false, err
	}
	if err := r.db.WithContext(ctx).Where("active_scope = ?", activeScope).Order("updated_at DESC").First(&existing).Error; err != nil {
		return nil, false, err
	}
	return &existing, false, nil
}

func isAssistantInteractionDuplicate(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint") || strings.Contains(message, "unique failed")
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
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND project_id = ? AND content_id = ? AND closed_at IS NULL", userID, projectID, contentID).Where("lifecycle IN ?", []string{model.AssistantInteractionActive, model.AssistantInteractionReconnecting, model.AssistantInteractionNeedsConfirmation}).Order("updated_at DESC").First(&value).Error
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
	var activeScope *string
	if lifecycle != model.AssistantInteractionAbandoned && lifecycle != model.AssistantInteractionCompleted {
		activeScope = &value.Scope
	}
	return r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).Where("id = ?", id).Updates(map[string]any{"lifecycle": lifecycle, "active_scope": activeScope, "last_activity_at": at, "updated_at": at}).Error
}

// MarkNeedsConfirmation is deliberately conditional so a late stage response
// cannot move an interaction back from a completed or abandoned commit.
func (r *assistantInteractionRepository) MarkNeedsConfirmation(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).
		Where("id = ? AND lifecycle NOT IN ?", id, []string{model.AssistantInteractionCompleted, model.AssistantInteractionAbandoned}).
		Updates(map[string]any{"lifecycle": model.AssistantInteractionNeedsConfirmation, "last_activity_at": at, "updated_at": at}).Error
}
func (r *assistantInteractionRepository) UpdateMode(ctx context.Context, id, mode string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).Where("id = ?", id).Updates(map[string]any{"mode_id": mode, "last_activity_at": at, "updated_at": at}).Error
}
func (r *assistantInteractionRepository) UpdateContext(ctx context.Context, id, mode, projectID, contentID string, at time.Time) error {
	var value model.AssistantInteraction
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&value).Error; err != nil {
		return err
	}
	scope := value.UserID + ":" + projectID + ":" + contentID
	updates := map[string]any{"mode_id": mode, "project_id": projectID, "content_id": contentID, "scope": scope, "last_activity_at": at, "updated_at": at}
	if value.ActiveScope != nil {
		updates["active_scope"] = scope
	}
	return r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).Where("id = ?", id).Updates(updates).Error
}
func (r *assistantInteractionRepository) UpdateProject(ctx context.Context, id, projectID string, at time.Time) error {
	var value model.AssistantInteraction
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&value).Error; err != nil {
		return err
	}
	scope := value.UserID + ":" + projectID + ":" + value.ContentID
	updates := map[string]any{"project_id": projectID, "scope": scope, "last_activity_at": at, "updated_at": at}
	if value.ActiveScope != nil {
		updates["active_scope"] = scope
	}
	return r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).Where("id = ?", id).Updates(updates).Error
}
func (r *assistantInteractionRepository) CompareAndSwapGeneration(ctx context.Context, id string, expected, next int64, at, expiresAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).Where("id = ? AND generation = ? AND closed_at IS NULL", id, expected).Updates(map[string]any{"generation": next, "expires_at": expiresAt, "last_activity_at": at, "updated_at": at})
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
	persisted, _, created, err := r.createDraftIfAttemptNew(ctx, draft, attempt, nil, 0)
	return persisted, created, err
}

func (r *assistantInteractionRepository) CreateDraftIfAttemptNewAtGeneration(ctx context.Context, draft *model.AssistantInteractionDraft, attempt *model.AssistantInteractionAttempt, generation int64) (*model.AssistantInteractionDraft, bool, error) {
	persisted, _, created, err := r.createDraftIfAttemptNew(ctx, draft, attempt, nil, generation)
	return persisted, created, err
}

func (r *assistantInteractionRepository) CreateDraftAttemptEventAtGeneration(ctx context.Context, draft *model.AssistantInteractionDraft, attempt *model.AssistantInteractionAttempt, event *model.AssistantInteractionEvent, generation int64) (*model.AssistantInteractionDraft, *model.AssistantInteractionEvent, bool, error) {
	return r.createDraftIfAttemptNew(ctx, draft, attempt, event, generation)
}

func (r *assistantInteractionRepository) createDraftIfAttemptNew(ctx context.Context, draft *model.AssistantInteractionDraft, attempt *model.AssistantInteractionAttempt, event *model.AssistantInteractionEvent, generation int64) (*model.AssistantInteractionDraft, *model.AssistantInteractionEvent, bool, error) {
	if draft == nil || attempt == nil {
		return nil, nil, false, errors.New("assistant interaction draft and attempt are required")
	}
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, nil, false, tx.Error
	}
	if generation > 0 {
		var interaction model.AssistantInteraction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", draft.InteractionID).First(&interaction).Error; err != nil {
			_ = tx.Rollback()
			return nil, nil, false, err
		}
		if interaction.Generation != generation {
			_ = tx.Rollback()
			return nil, nil, false, ErrInteractionGenerationChanged
		}
		if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned {
			_ = tx.Rollback()
			return nil, nil, false, ErrInteractionTerminal
		}
	}
	var existing model.AssistantInteractionAttempt
	err := tx.Where("interaction_id = ? AND attempt_id = ?", attempt.InteractionID, attempt.AttemptID).First(&existing).Error
	if err == nil {
		if existing.Generation != attempt.Generation || existing.Operation != attempt.Operation || existing.PayloadDigest != attempt.PayloadDigest {
			_ = tx.Rollback()
			return nil, nil, false, ErrInteractionAttemptConflict
		}
		var result assistantInteractionDraftReceipt
		if decodeErr := json.Unmarshal(existing.Result, &result); decodeErr != nil {
			_ = tx.Rollback()
			return nil, nil, false, decodeErr
		}
		_ = tx.Rollback()
		return &model.AssistantInteractionDraft{ID: result.DraftID, Revision: result.Revision, Ready: result.Ready, ConfirmationState: result.ConfirmationState}, nil, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		_ = tx.Rollback()
		return nil, nil, false, err
	}
	if draft.Revision <= 0 {
		var revision struct{ Value int64 }
		if err := tx.Model(&model.AssistantInteractionDraft{}).Where("interaction_id = ? AND mode_id = ?", draft.InteractionID, draft.ModeID).Select("COALESCE(MAX(revision), 0) AS value").Scan(&revision).Error; err != nil {
			_ = tx.Rollback()
			return nil, nil, false, err
		}
		draft.Revision = revision.Value + 1
	}
	if err := tx.Create(draft).Error; err != nil {
		_ = tx.Rollback()
		return nil, nil, false, err
	}
	if event != nil {
		var interaction model.AssistantInteraction
		if err := tx.Model(&model.AssistantInteraction{}).Where("id = ?", draft.InteractionID).UpdateColumn("command_sequence", gorm.Expr("command_sequence + 1")).Error; err != nil {
			_ = tx.Rollback()
			return nil, nil, false, err
		}
		if err := tx.Select("command_sequence").Where("id = ?", draft.InteractionID).First(&interaction).Error; err != nil {
			_ = tx.Rollback()
			return nil, nil, false, err
		}
		event.InteractionID = draft.InteractionID
		event.Generation = attempt.Generation
		event.Sequence = interaction.CommandSequence
		if len(event.Data) == 0 {
			event.Data, err = json.Marshal(map[string]any{"durable": true, "draft_id": draft.ID, "revision": draft.Revision, "mode_id": draft.ModeID, "ready": draft.Ready, "payload_digest": draft.PayloadDigest})
			if err != nil {
				_ = tx.Rollback()
				return nil, nil, false, err
			}
		}
		if err := tx.Create(event).Error; err != nil {
			_ = tx.Rollback()
			return nil, nil, false, err
		}
	}
	// Attempts are replay receipts, not a second draft store. Keep only the
	// identifiers needed to return an idempotent stage result; the full payload
	// remains in assistant_interaction_drafts where the user-owned draft lives.
	encoded, err := json.Marshal(assistantInteractionDraftReceipt{DraftID: draft.ID, Revision: draft.Revision, ModeID: draft.ModeID, Ready: draft.Ready, ConfirmationState: draft.ConfirmationState})
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, false, err
	}
	attempt.Result = encoded
	if err := tx.Create(attempt).Error; err != nil {
		_ = tx.Rollback()
		return nil, nil, false, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, nil, false, err
	}
	return draft, event, true, nil
}

type assistantInteractionDraftReceipt struct {
	DraftID           string `json:"draft_id"`
	Revision          int64  `json:"revision"`
	ModeID            string `json:"mode_id"`
	Ready             bool   `json:"ready"`
	ConfirmationState string `json:"confirmation_state"`
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
func (r *assistantInteractionRepository) ListDrafts(ctx context.Context, userID, interactionID string) ([]*model.AssistantInteractionDraft, error) {
	var values []*model.AssistantInteractionDraft
	err := r.db.WithContext(ctx).Table("assistant_interaction_drafts").
		Joins("JOIN assistant_interactions ON assistant_interactions.id = assistant_interaction_drafts.interaction_id").
		Where("assistant_interactions.user_id = ? AND assistant_interaction_drafts.interaction_id = ?", userID, interactionID).
		Order("assistant_interaction_drafts.mode_id ASC, assistant_interaction_drafts.revision DESC").Find(&values).Error
	return values, err
}
func (r *assistantInteractionRepository) ExpireDrafts(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("expires_at <= ?", before).Delete(&model.AssistantInteractionDraft{})
	return result.RowsAffected, result.Error
}
func (r *assistantInteractionRepository) UpdateDraftState(ctx context.Context, id, state string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantInteractionDraft{}).Where("id = ?", id).Updates(map[string]any{"confirmation_state": state, "updated_at": at}).Error
}
func (r *assistantInteractionRepository) CreateEvent(ctx context.Context, value *model.AssistantInteractionEvent) error {
	return r.db.WithContext(ctx).Create(value).Error
}

// AppendEvent fences the generation check, sequence allocation, and insert in
// one transaction. A late runtime event therefore cannot slip between a
// generation read and the replacement CAS, and concurrent writers cannot
// allocate the same sequence number.
func (r *assistantInteractionRepository) AppendEvent(ctx context.Context, interactionID string, generation int64, value *model.AssistantInteractionEvent) (bool, error) {
	if value == nil || strings.TrimSpace(interactionID) == "" || generation < 1 {
		return false, errors.New("assistant interaction event identity is required")
	}
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return false, tx.Error
	}
	rollback := func(err error) (bool, error) {
		_ = tx.Rollback()
		return false, err
	}
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", interactionID).First(&interaction).Error; err != nil {
		return rollback(err)
	}
	if interaction.Generation != generation {
		return rollback(nil)
	}
	if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned {
		return rollback(ErrInteractionTerminal)
	}
	if err := tx.Model(&model.AssistantInteraction{}).Where("id = ?", interactionID).UpdateColumn("command_sequence", gorm.Expr("command_sequence + 1")).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Select("command_sequence").Where("id = ?", interactionID).First(&interaction).Error; err != nil {
		return rollback(err)
	}
	value.InteractionID = interactionID
	value.Generation = generation
	value.Sequence = interaction.CommandSequence
	if err := tx.Create(value).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Commit().Error; err != nil {
		return false, err
	}
	return true, nil
}
func (r *assistantInteractionRepository) NextEventSequence(ctx context.Context, interactionID string) (uint64, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	if err := tx.Model(&model.AssistantInteraction{}).Where("id = ?", interactionID).UpdateColumn("command_sequence", gorm.Expr("command_sequence + 1")).Error; err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	var value model.AssistantInteraction
	if err := tx.Select("command_sequence").Where("id = ?", interactionID).First(&value).Error; err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return value.CommandSequence, nil
}
func (r *assistantInteractionRepository) ListEventsAfter(ctx context.Context, interactionID string, generation int64, after uint64, limit int) ([]*model.AssistantInteractionEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	var values []*model.AssistantInteractionEvent
	err := r.db.WithContext(ctx).Where("interaction_id = ? AND generation = ? AND sequence > ?", interactionID, generation, after).Order("sequence ASC").Limit(limit).Find(&values).Error
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

// FindCreateAttempt resolves the create idempotency receipt by user rather
// than interaction. The interaction ID is not known until the first create
// request wins the active-scope race, so this lookup is the durable boundary
// that also covers completed interactions whose active scope is nullable.
func (r *assistantInteractionRepository) FindCreateAttempt(ctx context.Context, userID, attemptID string) (*model.AssistantInteractionAttempt, error) {
	var value model.AssistantInteractionAttempt
	err := r.db.WithContext(ctx).
		Table("assistant_interaction_attempts").
		Joins("JOIN assistant_interactions ON assistant_interactions.id = assistant_interaction_attempts.interaction_id").
		Where("assistant_interactions.user_id = ? AND assistant_interaction_attempts.attempt_id = ? AND assistant_interaction_attempts.operation = ?", userID, attemptID, "create").
		First(&value).Error
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *assistantInteractionRepository) UpdateAttempt(ctx context.Context, interactionID, attemptID, status string, result json.RawMessage, errorCode string) error {
	return r.db.WithContext(ctx).Model(&model.AssistantInteractionAttempt{}).
		Where("interaction_id = ? AND attempt_id = ?", interactionID, attemptID).
		Updates(map[string]any{"status": status, "result": result, "error_code": errorCode}).Error
}

func (r *assistantInteractionRepository) FindLatestCommitAttempt(ctx context.Context, interactionID string) (*model.AssistantInteractionAttempt, error) {
	var value model.AssistantInteractionAttempt
	if err := r.db.WithContext(ctx).
		Where("interaction_id = ? AND operation = ?", interactionID, "commit").
		Order("created_at DESC").First(&value).Error; err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) FindLatestCommandOutcomeUnknown(ctx context.Context, interactionID string) (*model.AssistantInteractionAttempt, error) {
	var value model.AssistantInteractionAttempt
	if err := r.db.WithContext(ctx).
		Where("interaction_id = ? AND operation IN ?", interactionID, []string{"message", "answer"}).
		Order("created_at DESC").Order("id DESC").First(&value).Error; err != nil {
		return nil, err
	}
	if value.Status != model.AssistantInteractionAttemptPending || value.ErrorCode != "assistant_command_outcome_unknown" {
		return nil, gorm.ErrRecordNotFound
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
