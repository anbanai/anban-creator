package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInteractionAttemptConflict = errors.New("assistant interaction attempt payload conflict")
var ErrInteractionGenerationChanged = errors.New("assistant interaction generation changed")
var ErrInteractionTerminal = errors.New("assistant interaction is terminal")
var ErrInteractionCommitInProgress = errors.New("assistant interaction commit is in progress")
var ErrInteractionDraftRevisionChanged = errors.New("assistant interaction draft revision changed")
var ErrInteractionCommitClaimLost = errors.New("assistant interaction commit claim was lost")

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
	CompareAndSwapGeneration(context.Context, string, int64, int64, time.Time) (bool, error)
	CompareAndSwapStaleRuntimeGeneration(context.Context, string, int64, int64, string, time.Time, time.Time) (bool, error)
	CreateRuntimeBinding(context.Context, *model.AssistantInteractionRuntime) error
	ActivateRuntimeBinding(context.Context, string, time.Time) error
	FindCurrentRuntime(context.Context, string, int64) (*model.AssistantInteractionRuntime, error)
	TouchRuntimeBindings(context.Context, string, time.Time) error
	CloseRuntime(context.Context, string, time.Time) error
	CreateDraftIfAttemptNew(context.Context, *model.AssistantInteractionDraft, *model.AssistantInteractionAttempt) (*model.AssistantInteractionDraft, bool, error)
	CreateDraftIfAttemptNewAtGeneration(context.Context, *model.AssistantInteractionDraft, *model.AssistantInteractionAttempt, int64) (*model.AssistantInteractionDraft, bool, error)
	UpdateDraftIfAttemptNewAtGeneration(context.Context, *model.AssistantInteractionDraft, string, *model.AssistantInteractionAttempt, int64, int64, time.Time) (*model.AssistantInteractionDraft, bool, error)
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
	ClaimInteractionCommit(context.Context, string, string, string, int64, *model.AssistantInteractionAttempt) (*model.AssistantInteractionAttempt, bool, error)
	ReleaseInteractionCommit(context.Context, string, string, string, string, string, time.Time) error
	FailInteractionCommit(context.Context, string, string, string, string) error
	CompleteInteractionCommit(context.Context, string, string, string, string, json.RawMessage, time.Time) error
	CompleteInteractionCommitInTransaction(context.Context, string, string, string, string, json.RawMessage, time.Time) error
	FindAttempt(context.Context, string, string) (*model.AssistantInteractionAttempt, error)
	FindCreateAttempt(context.Context, string, string) (*model.AssistantInteractionAttempt, error)
	UpdateAttempt(context.Context, string, string, string, json.RawMessage, string) error
	FindLatestCommitAttempt(context.Context, string) (*model.AssistantInteractionAttempt, error)
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
		Where("id = ? AND lifecycle NOT IN ?", id, []string{model.AssistantInteractionCompleted, model.AssistantInteractionAbandoned, model.AssistantInteractionCommitting}).
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
func (r *assistantInteractionRepository) CompareAndSwapGeneration(ctx context.Context, id string, expected, next int64, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.AssistantInteraction{}).
		Where("id = ? AND generation = ? AND closed_at IS NULL AND lifecycle NOT IN ?", id, expected, []string{model.AssistantInteractionCommitting, model.AssistantInteractionCompleted, model.AssistantInteractionAbandoned}).
		Updates(map[string]any{"generation": next, "expires_at": at.Add(model.AssistantInteractionRuntimeLease), "last_activity_at": at, "updated_at": at})
	if result.Error != nil || result.RowsAffected == 1 {
		return result.RowsAffected == 1, result.Error
	}
	var current model.AssistantInteraction
	if err := r.db.WithContext(ctx).Select("generation", "lifecycle", "closed_at").Where("id = ?", id).First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if current.Generation == expected {
		switch current.Lifecycle {
		case model.AssistantInteractionCommitting:
			return false, ErrInteractionCommitInProgress
		case model.AssistantInteractionCompleted, model.AssistantInteractionAbandoned:
			return false, ErrInteractionTerminal
		}
	}
	return false, nil
}
func (r *assistantInteractionRepository) CompareAndSwapStaleRuntimeGeneration(ctx context.Context, id string, expected, next int64, owner string, staleBefore, at time.Time) (bool, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return false, tx.Error
	}
	rollback := func(err error) (bool, error) {
		_ = tx.Rollback()
		return false, err
	}
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND generation = ? AND closed_at IS NULL", id, expected).First(&interaction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return rollback(nil)
		}
		return rollback(err)
	}
	if interaction.Lifecycle == model.AssistantInteractionCommitting {
		return rollback(ErrInteractionCommitInProgress)
	}
	if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned {
		return rollback(ErrInteractionTerminal)
	}
	var runtime model.AssistantInteractionRuntime
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("interaction_id = ? AND generation = ? AND server_instance = ? AND status IN ?", id, expected, owner, []string{model.AssistantInteractionRuntimeStarting, model.AssistantInteractionRuntimeRunning}).First(&runtime).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return rollback(nil)
		}
		return rollback(err)
	}
	if runtime.UpdatedAt.After(staleBefore) {
		return rollback(nil)
	}
	if err := tx.Model(&model.AssistantInteraction{}).Where("id = ? AND generation = ?", id, expected).Updates(map[string]any{"generation": next, "expires_at": at.Add(model.AssistantInteractionRuntimeLease), "last_activity_at": at, "updated_at": at}).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Model(&model.AssistantInteractionRuntime{}).Where("id = ? AND status IN ?", runtime.ID, []string{model.AssistantInteractionRuntimeStarting, model.AssistantInteractionRuntimeRunning}).Updates(map[string]any{"status": model.AssistantInteractionRuntimeClosed, "closed_at": at, "updated_at": at}).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Commit().Error; err != nil {
		return false, err
	}
	return true, nil
}
func (r *assistantInteractionRepository) CreateRuntimeBinding(ctx context.Context, value *model.AssistantInteractionRuntime) error {
	if value == nil || strings.TrimSpace(value.InteractionID) == "" || value.Generation < 1 {
		return errors.New("assistant interaction runtime identity is required")
	}
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	rollback := func(err error) error { _ = tx.Rollback(); return err }
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND generation = ? AND closed_at IS NULL", value.InteractionID, value.Generation).First(&interaction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return rollback(ErrInteractionGenerationChanged)
		}
		return rollback(err)
	}
	if err := interactionRuntimeMutationError(interaction.Lifecycle); err != nil {
		return rollback(err)
	}
	if err := tx.Create(value).Error; err != nil {
		return rollback(err)
	}
	return tx.Commit().Error
}
func (r *assistantInteractionRepository) ActivateRuntimeBinding(ctx context.Context, id string, at time.Time) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	rollback := func(err error) error { _ = tx.Rollback(); return err }
	var binding model.AssistantInteractionRuntime
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", id, model.AssistantInteractionRuntimeStarting).First(&binding).Error; err != nil {
		return rollback(err)
	}
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND generation = ? AND closed_at IS NULL", binding.InteractionID, binding.Generation).First(&interaction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return rollback(ErrInteractionGenerationChanged)
		}
		return rollback(err)
	}
	if err := interactionRuntimeMutationError(interaction.Lifecycle); err != nil {
		return rollback(err)
	}
	if err := tx.Model(&model.AssistantInteractionRuntime{}).Where("id = ? AND status = ?", id, model.AssistantInteractionRuntimeStarting).
		Updates(map[string]any{"status": model.AssistantInteractionRuntimeRunning, "started_at": at, "updated_at": at}).Error; err != nil {
		return rollback(err)
	}
	return tx.Commit().Error
}

func interactionRuntimeMutationError(lifecycle string) error {
	switch lifecycle {
	case model.AssistantInteractionCommitting:
		return ErrInteractionCommitInProgress
	case model.AssistantInteractionCompleted, model.AssistantInteractionAbandoned:
		return ErrInteractionTerminal
	default:
		return nil
	}
}
func (r *assistantInteractionRepository) FindCurrentRuntime(ctx context.Context, interactionID string, generation int64) (*model.AssistantInteractionRuntime, error) {
	var value model.AssistantInteractionRuntime
	err := r.db.WithContext(ctx).Where("interaction_id = ? AND generation = ? AND status IN ?", interactionID, generation, []string{model.AssistantInteractionRuntimeStarting, model.AssistantInteractionRuntimeRunning}).Order("created_at DESC").First(&value).Error
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func (r *assistantInteractionRepository) TouchRuntimeBindings(ctx context.Context, serverInstance string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantInteractionRuntime{}).
		Where("server_instance = ? AND status IN ?", serverInstance, []string{model.AssistantInteractionRuntimeStarting, model.AssistantInteractionRuntimeRunning}).
		UpdateColumn("updated_at", at).Error
}
func (r *assistantInteractionRepository) CloseRuntime(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AssistantInteractionRuntime{}).Where("id = ? AND closed_at IS NULL", id).Updates(map[string]any{"status": model.AssistantInteractionRuntimeClosed, "closed_at": at, "updated_at": at}).Error
}
func (r *assistantInteractionRepository) CreateDraftIfAttemptNew(ctx context.Context, draft *model.AssistantInteractionDraft, attempt *model.AssistantInteractionAttempt) (*model.AssistantInteractionDraft, bool, error) {
	return r.createDraftIfAttemptNew(ctx, draft, attempt, 0)
}

func (r *assistantInteractionRepository) CreateDraftIfAttemptNewAtGeneration(ctx context.Context, draft *model.AssistantInteractionDraft, attempt *model.AssistantInteractionAttempt, generation int64) (*model.AssistantInteractionDraft, bool, error) {
	return r.createDraftIfAttemptNew(ctx, draft, attempt, generation)
}

func (r *assistantInteractionRepository) createDraftIfAttemptNew(ctx context.Context, draft *model.AssistantInteractionDraft, attempt *model.AssistantInteractionAttempt, generation int64) (*model.AssistantInteractionDraft, bool, error) {
	if draft == nil || attempt == nil {
		return nil, false, errors.New("assistant interaction draft and attempt are required")
	}
	attempt = interactionAttemptForStorage(attempt)
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, false, tx.Error
	}
	if generation > 0 {
		var interaction model.AssistantInteraction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", draft.InteractionID).First(&interaction).Error; err != nil {
			_ = tx.Rollback()
			return nil, false, err
		}
		if interaction.Generation != generation {
			_ = tx.Rollback()
			return nil, false, ErrInteractionGenerationChanged
		}
		if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned || interaction.Lifecycle == model.AssistantInteractionCommitting {
			_ = tx.Rollback()
			return nil, false, ErrInteractionTerminal
		}
	}
	var existing model.AssistantInteractionAttempt
	err := tx.Where("interaction_id = ? AND attempt_id = ?", attempt.InteractionID, attempt.AttemptID).First(&existing).Error
	if err == nil {
		if existing.Generation != attempt.Generation || existing.Operation != attempt.Operation || existing.PayloadDigest != attempt.PayloadDigest {
			_ = tx.Rollback()
			return nil, false, ErrInteractionAttemptConflict
		}
		var result assistantInteractionDraftReceipt
		if decodeErr := json.Unmarshal(existing.Result, &result); decodeErr != nil {
			_ = tx.Rollback()
			return nil, false, decodeErr
		}
		_ = tx.Rollback()
		return &model.AssistantInteractionDraft{ID: result.DraftID, Revision: result.Revision, Ready: result.Ready, ConfirmationState: result.ConfirmationState}, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		_ = tx.Rollback()
		return nil, false, err
	}
	if draft.Revision <= 0 {
		var revision struct{ Value int64 }
		if err := tx.Model(&model.AssistantInteractionDraft{}).Where("interaction_id = ? AND mode_id = ?", draft.InteractionID, draft.ModeID).Select("COALESCE(MAX(revision), 0) AS value").Scan(&revision).Error; err != nil {
			_ = tx.Rollback()
			return nil, false, err
		}
		draft.Revision = revision.Value + 1
	}
	if err := tx.Create(draft).Error; err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	// Attempts are replay receipts, not a second draft store. Keep only the
	// identifiers needed to return an idempotent stage result; the full payload
	// remains in assistant_interaction_drafts where the user-owned draft lives.
	encoded, err := json.Marshal(assistantInteractionDraftReceipt{DraftID: draft.ID, Revision: draft.Revision, ModeID: draft.ModeID, Ready: draft.Ready, ConfirmationState: draft.ConfirmationState})
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

// UpdateDraftIfAttemptNewAtGeneration creates the next immutable draft
// revision only when the supplied receipt is still current. The interaction
// row lock serializes this compare-and-swap across Server instances.
func (r *assistantInteractionRepository) UpdateDraftIfAttemptNewAtGeneration(ctx context.Context, draft *model.AssistantInteractionDraft, previousDraftID string, attempt *model.AssistantInteractionAttempt, expectedRevision, generation int64, now time.Time) (*model.AssistantInteractionDraft, bool, error) {
	if draft == nil || attempt == nil || strings.TrimSpace(previousDraftID) == "" || expectedRevision < 1 || generation < 1 {
		return nil, false, errors.New("assistant interaction draft update identity is required")
	}
	if strings.TrimSpace(draft.ID) == "" {
		draft.ID = uuid.NewString()
	}
	attempt = interactionAttemptForStorage(attempt)
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, false, tx.Error
	}
	rollback := func(err error) (*model.AssistantInteractionDraft, bool, error) {
		_ = tx.Rollback()
		return nil, false, err
	}
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", draft.InteractionID).First(&interaction).Error; err != nil {
		return rollback(err)
	}
	if interaction.Generation != generation {
		return rollback(ErrInteractionGenerationChanged)
	}
	if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned || interaction.Lifecycle == model.AssistantInteractionCommitting {
		return rollback(ErrInteractionTerminal)
	}
	var existing model.AssistantInteractionAttempt
	err := tx.Where("interaction_id = ? AND attempt_id = ?", attempt.InteractionID, attempt.AttemptID).First(&existing).Error
	if err == nil {
		if existing.Generation != attempt.Generation || existing.Operation != attempt.Operation || existing.PayloadDigest != attempt.PayloadDigest {
			return rollback(ErrInteractionAttemptConflict)
		}
		var receipt assistantInteractionDraftReceipt
		if decodeErr := json.Unmarshal(existing.Result, &receipt); decodeErr != nil {
			return rollback(decodeErr)
		}
		var replay model.AssistantInteractionDraft
		if findErr := tx.Where("id = ? AND interaction_id = ?", receipt.DraftID, draft.InteractionID).First(&replay).Error; findErr != nil {
			return rollback(findErr)
		}
		_ = tx.Rollback()
		return &replay, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return rollback(err)
	}
	var previous model.AssistantInteractionDraft
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("interaction_id = ? AND mode_id = ?", draft.InteractionID, draft.ModeID).Order("revision DESC").First(&previous).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return rollback(ErrInteractionDraftRevisionChanged)
		}
		return rollback(err)
	}
	if previous.ID != previousDraftID || previous.Revision != expectedRevision || !previous.Ready || previous.ConfirmationState != model.AssistantInteractionDraftPending {
		return rollback(ErrInteractionDraftRevisionChanged)
	}
	draft.Revision = expectedRevision + 1
	if err := tx.Create(draft).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Model(&model.AssistantInteractionDraft{}).Where("id = ?", previous.ID).Updates(map[string]any{"confirmation_state": model.AssistantInteractionDraftSuperseded, "updated_at": now}).Error; err != nil {
		return rollback(err)
	}
	encoded, err := json.Marshal(assistantInteractionDraftReceipt{DraftID: draft.ID, Revision: draft.Revision, ModeID: draft.ModeID, Ready: draft.Ready, ConfirmationState: draft.ConfirmationState})
	if err != nil {
		return rollback(err)
	}
	attempt.Result = encoded
	if err := tx.Create(attempt).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Commit().Error; err != nil {
		return nil, false, err
	}
	return draft, true, nil
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
	if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned || interaction.Lifecycle == model.AssistantInteractionCommitting {
		return rollback(ErrInteractionTerminal)
	}
	var existing model.AssistantInteractionEvent
	if err := tx.Where("id = ?", value.ID).First(&existing).Error; err == nil {
		if existing.InteractionID != interactionID || existing.Generation != generation || existing.Type != value.Type || !bytes.Equal(existing.Data, value.Data) {
			return rollback(ErrInteractionAttemptConflict)
		}
		_ = tx.Rollback()
		*value = existing
		return true, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return rollback(err)
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
	if value == nil || strings.TrimSpace(value.InteractionID) == "" || strings.TrimSpace(value.AttemptID) == "" {
		return errors.New("assistant interaction attempt identity is required")
	}
	return r.db.WithContext(ctx).Create(interactionAttemptForStorage(value)).Error
}

func (r *assistantInteractionRepository) ClaimInteractionCommit(ctx context.Context, userID, interactionID, draftID string, revision int64, attempt *model.AssistantInteractionAttempt) (*model.AssistantInteractionAttempt, bool, error) {
	if attempt == nil || attempt.Operation != "commit" || attempt.InteractionID != interactionID || strings.TrimSpace(attempt.AttemptID) == "" || strings.TrimSpace(attempt.ClaimToken) == "" || revision < 1 {
		return nil, false, errors.New("assistant interaction commit claim is incomplete")
	}
	storedAttempt := interactionAttemptForStorage(attempt)
	now := attempt.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	claimExpiresAt := now.Add(model.AssistantInteractionCommitClaimLease)
	storedAttempt.ClaimExpiresAt = &claimExpiresAt
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, false, tx.Error
	}
	rollback := func(err error) (*model.AssistantInteractionAttempt, bool, error) {
		_ = tx.Rollback()
		return nil, false, err
	}
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", interactionID, userID).First(&interaction).Error; err != nil {
		return rollback(err)
	}
	var existing model.AssistantInteractionAttempt
	err := tx.Where("interaction_id = ? AND attempt_id = ?", interactionID, storedAttempt.AttemptID).First(&existing).Error
	reclaiming := false
	if err == nil {
		if existing.Operation != storedAttempt.Operation || existing.PayloadDigest != storedAttempt.PayloadDigest {
			return rollback(ErrInteractionAttemptConflict)
		}
		switch {
		case existing.Status == model.AssistantInteractionAttemptPending || existing.ErrorCode == "assistant_commit_reconciliation_required":
			claimExpiresAt := existing.ClaimExpiresAt
			if claimExpiresAt == nil {
				legacyExpiry := existing.CreatedAt.Add(model.AssistantInteractionCommitClaimLease)
				claimExpiresAt = &legacyExpiry
			}
			if claimExpiresAt.After(now) {
				return rollback(ErrInteractionCommitInProgress)
			}
			reclaiming = true
		case existing.Status == model.AssistantInteractionAttemptFailed && existing.ErrorCode == "assistant_commit_retryable":
		default:
			_ = tx.Rollback()
			return &existing, false, nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return rollback(err)
	}
	if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned {
		return rollback(ErrInteractionTerminal)
	}
	if interaction.Lifecycle == model.AssistantInteractionCommitting && !reclaiming {
		return rollback(ErrInteractionCommitInProgress)
	}
	if reclaiming && interaction.Lifecycle != model.AssistantInteractionCommitting {
		return rollback(ErrInteractionCommitInProgress)
	}
	var draft model.AssistantInteractionDraft
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND interaction_id = ?", draftID, interactionID).First(&draft).Error; err != nil {
		return rollback(err)
	}
	var latest model.AssistantInteractionDraft
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("interaction_id = ? AND mode_id = ?", interactionID, draft.ModeID).Order("revision DESC").First(&latest).Error; err != nil {
		return rollback(err)
	}
	wantDraftState := model.AssistantInteractionDraftPending
	if reclaiming {
		wantDraftState = model.AssistantInteractionDraftConfirmed
	}
	if latest.ID != draft.ID || latest.Revision != revision || !draft.Ready || draft.ConfirmationState != wantDraftState {
		return rollback(ErrInteractionDraftRevisionChanged)
	}
	if !reclaiming {
		if err := tx.Model(&model.AssistantInteractionDraft{}).Where("id = ?", draft.ID).Updates(map[string]any{"confirmation_state": model.AssistantInteractionDraftConfirmed, "updated_at": now}).Error; err != nil {
			return rollback(err)
		}
		if err := tx.Model(&model.AssistantInteraction{}).Where("id = ?", interactionID).Updates(map[string]any{"lifecycle": model.AssistantInteractionCommitting, "updated_at": now}).Error; err != nil {
			return rollback(err)
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.Create(storedAttempt).Error; err != nil {
			return rollback(err)
		}
	} else if err := tx.Model(&model.AssistantInteractionAttempt{}).Where("interaction_id = ? AND attempt_id = ?", interactionID, storedAttempt.AttemptID).Updates(map[string]any{
		"status": model.AssistantInteractionAttemptPending, "result": nil, "error_code": "", "claim_token": storedAttempt.ClaimToken, "claim_expires_at": claimExpiresAt,
	}).Error; err != nil {
		return rollback(err)
	}
	if err := tx.Commit().Error; err != nil {
		return nil, false, err
	}
	return storedAttempt, true, nil
}

func (r *assistantInteractionRepository) ReleaseInteractionCommit(ctx context.Context, interactionID, draftID, attemptID, claimToken, errorCode string, at time.Time) error {
	if strings.TrimSpace(claimToken) == "" {
		return ErrInteractionCommitClaimLost
	}
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	rollback := func(err error) error { _ = tx.Rollback(); return err }
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", interactionID).First(&interaction).Error; err != nil {
		return rollback(err)
	}
	if interaction.Lifecycle != model.AssistantInteractionCommitting {
		return rollback(ErrInteractionCommitInProgress)
	}
	attemptKey := interactionAttemptStorageID(interactionID, attemptID)
	claimed := tx.Model(&model.AssistantInteractionAttempt{}).
		Where("interaction_id = ? AND attempt_id = ? AND operation = ? AND status = ? AND claim_token = ?", interactionID, attemptKey, "commit", model.AssistantInteractionAttemptPending, claimToken).
		Updates(map[string]any{"status": model.AssistantInteractionAttemptFailed, "result": nil, "error_code": errorCode, "claim_token": "", "claim_expires_at": nil})
	if claimed.Error != nil {
		return rollback(claimed.Error)
	}
	if claimed.RowsAffected != 1 {
		return rollback(ErrInteractionCommitClaimLost)
	}
	draftUpdate := tx.Model(&model.AssistantInteractionDraft{}).Where("id = ? AND interaction_id = ? AND confirmation_state = ?", draftID, interactionID, model.AssistantInteractionDraftConfirmed).
		Updates(map[string]any{"confirmation_state": model.AssistantInteractionDraftPending, "updated_at": at})
	if draftUpdate.Error != nil {
		return rollback(draftUpdate.Error)
	}
	if draftUpdate.RowsAffected != 1 {
		return rollback(ErrInteractionCommitClaimLost)
	}
	interactionUpdate := tx.Model(&model.AssistantInteraction{}).Where("id = ? AND lifecycle = ?", interactionID, model.AssistantInteractionCommitting).
		Updates(map[string]any{"lifecycle": model.AssistantInteractionNeedsConfirmation, "updated_at": at})
	if interactionUpdate.Error != nil {
		return rollback(interactionUpdate.Error)
	}
	if interactionUpdate.RowsAffected != 1 {
		return rollback(ErrInteractionCommitClaimLost)
	}
	return tx.Commit().Error
}

func (r *assistantInteractionRepository) FailInteractionCommit(ctx context.Context, interactionID, attemptID, claimToken, errorCode string) error {
	if strings.TrimSpace(claimToken) == "" {
		return ErrInteractionCommitClaimLost
	}
	result := r.db.WithContext(ctx).Model(&model.AssistantInteractionAttempt{}).
		Where("interaction_id = ? AND attempt_id = ? AND operation = ? AND status = ? AND claim_token = ?", interactionID, interactionAttemptStorageID(interactionID, attemptID), "commit", model.AssistantInteractionAttemptPending, claimToken).
		Updates(map[string]any{"status": model.AssistantInteractionAttemptFailed, "result": nil, "error_code": errorCode, "claim_token": "", "claim_expires_at": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrInteractionCommitClaimLost
	}
	return nil
}

func (r *assistantInteractionRepository) CompleteInteractionCommit(ctx context.Context, interactionID, draftID, attemptID, claimToken string, result json.RawMessage, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return (&assistantInteractionRepository{db: tx}).completeInteractionCommit(ctx, interactionID, draftID, attemptID, claimToken, result, at)
	})
}

func (r *assistantInteractionRepository) CompleteInteractionCommitInTransaction(ctx context.Context, interactionID, draftID, attemptID, claimToken string, result json.RawMessage, at time.Time) error {
	return r.completeInteractionCommit(ctx, interactionID, draftID, attemptID, claimToken, result, at)
}

func (r *assistantInteractionRepository) completeInteractionCommit(ctx context.Context, interactionID, draftID, attemptID, claimToken string, result json.RawMessage, at time.Time) error {
	if strings.TrimSpace(claimToken) == "" {
		return ErrInteractionCommitClaimLost
	}
	tx := r.db.WithContext(ctx)
	var interaction model.AssistantInteraction
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", interactionID).First(&interaction).Error; err != nil {
		return err
	}
	if interaction.Lifecycle != model.AssistantInteractionCommitting {
		return ErrInteractionCommitInProgress
	}
	attemptKey := interactionAttemptStorageID(interactionID, attemptID)
	attemptUpdate := tx.Model(&model.AssistantInteractionAttempt{}).
		Where("interaction_id = ? AND attempt_id = ? AND operation = ? AND status = ? AND claim_token = ?", interactionID, attemptKey, "commit", model.AssistantInteractionAttemptPending, claimToken).
		Updates(map[string]any{"status": model.AssistantInteractionAttemptCompleted, "result": result, "error_code": "", "claim_token": "", "claim_expires_at": nil})
	if attemptUpdate.Error != nil {
		return attemptUpdate.Error
	}
	if attemptUpdate.RowsAffected != 1 {
		return ErrInteractionCommitClaimLost
	}
	draftUpdate := tx.Model(&model.AssistantInteractionDraft{}).
		Where("id = ? AND interaction_id = ? AND confirmation_state = ?", draftID, interactionID, model.AssistantInteractionDraftConfirmed).
		Updates(map[string]any{"confirmation_state": model.AssistantInteractionDraftConsumed, "updated_at": at})
	if draftUpdate.Error != nil {
		return draftUpdate.Error
	}
	if draftUpdate.RowsAffected != 1 {
		return ErrInteractionCommitClaimLost
	}
	interactionUpdate := tx.Model(&model.AssistantInteraction{}).
		Where("id = ? AND lifecycle = ?", interactionID, model.AssistantInteractionCommitting).
		Updates(map[string]any{"lifecycle": model.AssistantInteractionCompleted, "active_scope": nil, "last_activity_at": at, "updated_at": at})
	if interactionUpdate.Error != nil {
		return interactionUpdate.Error
	}
	if interactionUpdate.RowsAffected != 1 {
		return ErrInteractionCommitClaimLost
	}
	return nil
}

func (r *assistantInteractionRepository) FindAttempt(ctx context.Context, interactionID, attemptID string) (*model.AssistantInteractionAttempt, error) {
	var value model.AssistantInteractionAttempt
	db := r.db.WithContext(ctx)
	err := db.Where("interaction_id = ? AND attempt_id = ?", interactionID, interactionAttemptStorageID(interactionID, attemptID)).First(&value).Error
	if err == nil {
		return &value, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// Create receipts keep their stable key so a retry can resolve them before
	// it knows which interaction won the active-scope race.
	err = db.Where("interaction_id = ? AND attempt_id = ? AND operation = ?", interactionID, attemptID, "create").First(&value).Error
	if err != nil {
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
	updates := map[string]any{"status": status, "result": result, "error_code": errorCode}
	if status != model.AssistantInteractionAttemptPending {
		updates["claim_expires_at"] = nil
	}
	return r.db.WithContext(ctx).Model(&model.AssistantInteractionAttempt{}).
		Where("interaction_id = ? AND attempt_id = ?", interactionID, interactionAttemptStorageID(interactionID, attemptID)).
		Updates(updates).Error
}

func interactionAttemptForStorage(value *model.AssistantInteractionAttempt) *model.AssistantInteractionAttempt {
	stored := *value
	if stored.Operation != "create" {
		stored.AttemptID = interactionAttemptStorageID(stored.InteractionID, stored.AttemptID)
	}
	return &stored
}

func interactionAttemptStorageID(interactionID, attemptID string) string {
	digest := sha256.Sum256([]byte(interactionID + "\x00" + attemptID))
	return "interaction:" + hex.EncodeToString(digest[:])
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
func (r *assistantInteractionRepository) ExpireAttempts(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("expires_at <= ?", before).Delete(&model.AssistantInteractionAttempt{})
	return result.RowsAffected, result.Error
}
func (r *assistantInteractionRepository) ExpireEvents(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("created_at <= ?", before).Delete(&model.AssistantInteractionEvent{})
	return result.RowsAffected, result.Error
}
