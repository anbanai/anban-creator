package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StageDraftForInteraction persists a structured draft independently of the
// runtime that produced it. The interaction lock makes revision allocation
// and event ordering deterministic for concurrent tabs.
func (s *AssistantInteractionService) StageDraftForInteraction(ctx context.Context, userID string, in InteractionStageInput) (InteractionStageResult, error) {
	startedAt := time.Now()
	stageOutcome := "failed"
	defer func() { s.metrics.observeDraftStage(stageOutcome, time.Since(startedAt)) }()
	if s.repo == nil || s.repo.AssistantInteractions() == nil {
		return InteractionStageResult{}, ErrInteractivePersistence
	}
	if strings.TrimSpace(in.InteractionID) == "" || strings.TrimSpace(in.ModeID) == "" || strings.TrimSpace(in.AttemptID) == "" || len(in.AttemptID) > 128 || in.Generation < 1 || len(in.Payload) == 0 || len(in.Payload) > assistantCheckpointMaxBytes || !json.Valid(in.Payload) {
		return InteractionStageResult{}, ErrInteractiveDraftInvalid
	}
	switch in.ModeID {
	case AssistantModeProfile, AssistantModeCreation, AssistantModeReview:
	default:
		return InteractionStageResult{}, ErrInteractiveDraftInvalid
	}
	lock := s.lockFor(in.InteractionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, in.InteractionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InteractionStageResult{}, NewInteractionError(InteractionErrorRestartRequired, 410, "当前连接已结束，已保留可恢复内容", "restore_draft", "restart")
		}
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	if interaction.Generation != in.Generation {
		s.metrics.observeStageRetryAfterSessionLoss()
		return InteractionStageResult{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	if terminalErr := interactionRuntimeTerminalError(interaction.Lifecycle); terminalErr != nil {
		return InteractionStageResult{}, terminalErr
	}
	digest := sha256.Sum256(in.Payload)
	if in.PayloadDigest == "" || !strings.EqualFold(in.PayloadDigest, hex.EncodeToString(digest[:])) {
		return InteractionStageResult{}, ErrInteractiveDraftInvalid
	}
	if current, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, in.InteractionID, in.AttemptID); findErr == nil {
		if current.Generation != in.Generation || current.Operation != string(InteractionOperationStage) || current.PayloadDigest != in.PayloadDigest {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrInteractiveIdempotency)
		}
		var replay struct {
			DraftID  string `json:"draft_id"`
			Revision int64  `json:"revision"`
			ModeID   string `json:"mode_id"`
			Ready    bool   `json:"ready"`
		}
		if err := json.Unmarshal(current.Result, &replay); err != nil {
			return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, err)
		}
		if replay.ModeID != in.ModeID || replay.Ready != in.Ready {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrInteractiveIdempotency)
		}
		persisted, findErr := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, replay.DraftID)
		if findErr != nil || persisted.InteractionID != in.InteractionID {
			return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, findErr)
		}
		if err := s.appendInteractionDraftEvent(ctx, interaction, in.AttemptID, persisted, "draft.ready", s.clock()); err != nil {
			return InteractionStageResult{}, draftSavedRetryableError(err)
		}
		if replay.Ready {
			_ = s.repo.AssistantInteractions().MarkNeedsConfirmation(ctx, in.InteractionID, s.clock())
		}
		stageOutcome = "replay"
		return InteractionStageResult{DraftID: replay.DraftID, Revision: replay.Revision, ConfirmationRequired: replay.Ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, findErr)
	}
	// Revision allocation happens inside the repository transaction while the
	// interaction row is locked. This prevents two Server instances from
	// assigning the same revision to different stage attempts.
	revision := int64(0)
	now := s.clock()
	draft := &model.AssistantInteractionDraft{ID: uuid.NewString(), InteractionID: in.InteractionID, ModeID: in.ModeID, Revision: revision, Payload: append(json.RawMessage(nil), in.Payload...), PayloadDigest: in.PayloadDigest, Ready: in.Ready, ConfirmationState: model.AssistantInteractionDraftPending, SourceGeneration: in.Generation, ExpiresAt: now.Add(InteractionDraftRetention), CreatedAt: now, UpdatedAt: now}
	attempt := &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: in.InteractionID, UserID: userID, AttemptID: in.AttemptID, Operation: string(InteractionOperationStage), Generation: in.Generation, PayloadDigest: in.PayloadDigest, CreatedAt: now, ExpiresAt: now.Add(InteractionAttemptRetention)}
	persisted, created, err := s.repo.AssistantInteractions().CreateDraftIfAttemptNewAtGeneration(ctx, draft, attempt, in.Generation)
	if err != nil {
		if errors.Is(err, repository.ErrInteractionGenerationChanged) {
			s.metrics.observeStageRetryAfterSessionLoss()
			return InteractionStageResult{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
		}
		if errors.Is(err, repository.ErrInteractionTerminal) {
			return InteractionStageResult{}, interactionRuntimeTerminalError(interaction.Lifecycle)
		}
		if errors.Is(err, repository.ErrInteractionAttemptConflict) {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrInteractiveIdempotency)
		}
		return InteractionStageResult{}, draftSavedRetryableError(err)
	}
	if !created {
		if full, findErr := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, persisted.ID); findErr == nil {
			persisted = full
		}
		if err := s.appendInteractionDraftEvent(ctx, interaction, in.AttemptID, persisted, "draft.ready", s.clock()); err != nil {
			return InteractionStageResult{}, draftSavedRetryableError(err)
		}
		return InteractionStageResult{DraftID: persisted.ID, Revision: persisted.Revision, ConfirmationRequired: persisted.Ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
	}
	if err := s.appendInteractionDraftEvent(ctx, interaction, in.AttemptID, persisted, "draft.ready", now); err != nil {
		return InteractionStageResult{}, draftSavedRetryableError(err)
	}
	if in.Ready {
		_ = s.repo.AssistantInteractions().MarkNeedsConfirmation(ctx, in.InteractionID, now)
	}
	stageOutcome = "durable"
	return InteractionStageResult{DraftID: persisted.ID, Revision: persisted.Revision, ConfirmationRequired: persisted.Ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
}

// UpdateDraftForInteraction persists confirmation-time Studio choices as a
// new immutable draft revision. Commit can then depend solely on its receipt.
func (s *AssistantInteractionService) UpdateDraftForInteraction(ctx context.Context, userID, interactionID, draftID string, in InteractionDraftUpdateInput) (InteractionStageResult, error) {
	if s.repo == nil || s.repo.AssistantInteractions() == nil || strings.TrimSpace(interactionID) == "" || strings.TrimSpace(draftID) == "" || strings.TrimSpace(in.AttemptID) == "" || len(in.AttemptID) > 128 || in.Generation < 1 || in.Revision < 1 || len(in.Payload) == 0 || len(in.Payload) > assistantCheckpointMaxBytes || !json.Valid(in.Payload) {
		return InteractionStageResult{}, ErrInteractiveDraftInvalid
	}
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InteractionStageResult{}, NewInteractionError(InteractionErrorRestartRequired, 410, "当前连接已结束，已保留可恢复内容", "restore_draft", "restart")
		}
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	if interaction.Generation != in.Generation {
		return InteractionStageResult{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	if terminalErr := interactionRuntimeTerminalError(interaction.Lifecycle); terminalErr != nil {
		return InteractionStageResult{}, terminalErr
	}
	payloadHash := sha256.Sum256(in.Payload)
	payloadDigest := hex.EncodeToString(payloadHash[:])
	requestDigest := interactionCommandDigest(string(InteractionOperationDraftUpdate), in.AttemptID, map[string]any{
		"draft_id": draftID, "revision": in.Revision, "payload_digest": payloadDigest,
	})
	if previousAttempt, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, in.AttemptID); findErr == nil {
		if previousAttempt.Generation != in.Generation || previousAttempt.Operation != string(InteractionOperationDraftUpdate) || previousAttempt.PayloadDigest != requestDigest {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrInteractiveIdempotency)
		}
		var receipt struct {
			DraftID  string `json:"draft_id"`
			Revision int64  `json:"revision"`
			Ready    bool   `json:"ready"`
		}
		if err := json.Unmarshal(previousAttempt.Result, &receipt); err != nil {
			return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, err)
		}
		persisted, findErr := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, receipt.DraftID)
		if findErr != nil || persisted.InteractionID != interactionID {
			return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, findErr)
		}
		if err := s.appendInteractionDraftEvent(ctx, interaction, in.AttemptID, persisted, "draft.updated", s.clock()); err != nil {
			return InteractionStageResult{}, draftSavedRetryableError(err)
		}
		return InteractionStageResult{DraftID: receipt.DraftID, Revision: receipt.Revision, ConfirmationRequired: receipt.Ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, findErr)
	}
	previous, err := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, draftID)
	if err != nil || previous.InteractionID != interactionID || previous.Revision != in.Revision {
		return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrAssistantCheckpointConflict)
	}
	if !previous.Ready || previous.ConfirmationState == model.AssistantInteractionDraftConsumed || previous.ConfirmationState == model.AssistantInteractionDraftSuperseded {
		return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrAssistantCheckpointConflict)
	}
	now := s.clock()
	updated := &model.AssistantInteractionDraft{
		ID: uuid.NewString(), InteractionID: interactionID, ModeID: previous.ModeID,
		Payload: append(json.RawMessage(nil), in.Payload...), PayloadDigest: payloadDigest, Ready: previous.Ready,
		ConfirmationState: model.AssistantInteractionDraftPending, SourceGeneration: interaction.Generation,
		ExpiresAt: now.Add(InteractionDraftRetention), CreatedAt: now, UpdatedAt: now,
	}
	attempt := &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: interactionID, UserID: userID, AttemptID: in.AttemptID,
		Operation: string(InteractionOperationDraftUpdate), Generation: in.Generation, PayloadDigest: requestDigest,
		CreatedAt: now, ExpiresAt: now.Add(InteractionAttemptRetention),
	}
	persisted, created, err := s.repo.AssistantInteractions().UpdateDraftIfAttemptNewAtGeneration(ctx, updated, draftID, attempt, in.Revision, in.Generation, now)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrInteractionGenerationChanged):
			return InteractionStageResult{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
		case errors.Is(err, repository.ErrInteractionTerminal):
			return InteractionStageResult{}, interactionRuntimeTerminalError(interaction.Lifecycle)
		case errors.Is(err, repository.ErrInteractionDraftRevisionChanged), errors.Is(err, repository.ErrInteractionAttemptConflict):
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrAssistantCheckpointConflict)
		default:
			return InteractionStageResult{}, draftSavedRetryableError(err)
		}
	}
	if !created {
		if full, findErr := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, persisted.ID); findErr == nil {
			persisted = full
		}
		if err := s.appendInteractionDraftEvent(ctx, interaction, in.AttemptID, persisted, "draft.updated", now); err != nil {
			return InteractionStageResult{}, draftSavedRetryableError(err)
		}
		return InteractionStageResult{DraftID: persisted.ID, Revision: persisted.Revision, ConfirmationRequired: persisted.Ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
	}
	if err := s.appendInteractionDraftEvent(ctx, interaction, in.AttemptID, persisted, "draft.updated", now); err != nil {
		return InteractionStageResult{}, draftSavedRetryableError(err)
	}
	_ = s.repo.AssistantInteractions().MarkNeedsConfirmation(ctx, interactionID, now)
	return InteractionStageResult{DraftID: persisted.ID, Revision: persisted.Revision, ConfirmationRequired: true, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
}

func (s *AssistantInteractionService) appendInteractionDraftEvent(ctx context.Context, interaction *model.AssistantInteraction, attemptID string, draft *model.AssistantInteractionDraft, eventType string, now time.Time) error {
	if interaction == nil || draft == nil {
		return errors.New("interaction and draft are required for event persistence")
	}
	eventData, err := json.Marshal(map[string]any{
		"durable": true, "draft_id": draft.ID, "revision": draft.Revision, "mode_id": draft.ModeID,
		"ready": draft.Ready, "payload_digest": draft.PayloadDigest,
	})
	if err != nil {
		return err
	}
	eventID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("assistant-interaction:"+interaction.ID+":"+eventType+":"+attemptID)).String()
	durableEvent := &model.AssistantInteractionEvent{ID: eventID, InteractionID: interaction.ID, Generation: interaction.Generation, Type: eventType, Data: eventData, CreatedAt: now}
	accepted, err := s.repo.AssistantInteractions().AppendEvent(ctx, interaction.ID, interaction.Generation, durableEvent)
	if err != nil {
		return err
	}
	if !accepted {
		return errors.New("interaction event persistence failed")
	}
	s.cacheLiveEvent(interaction.ID, interaction.Generation, durableEvent, map[string]any{
		"durable": true,
		"mode_id": draft.ModeID,
		"draft": map[string]any{
			"draft_id": draft.ID, "mode_id": draft.ModeID, "revision": draft.Revision,
			"payload": draft.Payload, "payload_digest": draft.PayloadDigest,
			"ready": draft.Ready, "confirmation_state": draft.ConfirmationState,
		},
	}, now)
	return nil
}

func (s *AssistantInteractionService) stageDraftRevisionConflictError(ctx context.Context, userID string, interaction *model.AssistantInteraction, cause error) error {
	structured := NewInteractionError(InteractionErrorDraftRevisionConflict, 409, "计划版本已更新，请恢复最新计划后重试", "reload_snapshot", "restore_draft")
	structured.cause = cause
	if interaction == nil {
		return structured
	}
	if snapshot, snapshotErr := s.snapshotWithDrafts(ctx, userID, interaction); snapshotErr == nil {
		return structured.WithSnapshot(snapshot)
	}
	return structured.WithSnapshot(s.snapshotFromInteraction(interaction))
}

func draftSavedRetryableError(cause error) *InteractionError {
	return &InteractionError{
		Code:             InteractionErrorDraftSavedRetryable,
		Status:           503,
		Message:          "计划已保留，但暂存未完成，请重试",
		AvailableActions: []string{"retry_stage", "confirm_draft"},
		cause:            errors.Join(ErrInteractivePersistence, cause),
	}
}
