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
	if strings.TrimSpace(in.InteractionID) == "" || strings.TrimSpace(in.ModeID) == "" || strings.TrimSpace(in.AttemptID) == "" || len(in.Payload) == 0 {
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
		return InteractionStageResult{}, err
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
	requestedReady := in.Ready
	if in.ModeID == AssistantModeCreation {
		issues, validationErr := validateInteractionCreation(in.Payload)
		if validationErr != nil {
			return InteractionStageResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationDraftError(InteractionErrorDraftInvalid, nil))
		}
		in.Ready = in.Ready && len(issues) == 0
	}
	if current, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, in.InteractionID, in.AttemptID); findErr == nil {
		if current.Generation != in.Generation || current.Operation != string(InteractionOperationStage) || current.PayloadDigest != in.PayloadDigest {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrInteractiveIdempotency)
		}
		var replay struct {
			DraftID        string `json:"draft_id"`
			Revision       int64  `json:"revision"`
			ModeID         string `json:"mode_id"`
			Ready          bool   `json:"ready"`
			RequestedReady *bool  `json:"requested_ready"`
		}
		if err := json.Unmarshal(current.Result, &replay); err != nil {
			return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, err)
		}
		replayReady := replay.Ready
		if replay.RequestedReady != nil {
			replayReady = *replay.RequestedReady
		}
		if replay.ModeID != in.ModeID || replayReady != requestedReady {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrInteractiveIdempotency)
		}
		if replay.Ready && in.ModeID != AssistantModeCreation {
			_ = s.repo.AssistantInteractions().MarkNeedsConfirmation(ctx, in.InteractionID, s.clock())
		}
		stageOutcome = "replay"
		return s.interactionStageResult(ctx, userID, interaction, replay.DraftID, replay.Revision, replay.Ready)
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
	attempt.Result, _ = json.Marshal(map[string]bool{"requested_ready": requestedReady})
	durableEvent := &model.AssistantInteractionEvent{ID: uuid.NewString(), InteractionID: in.InteractionID, Type: "draft.ready", CreatedAt: now}
	persisted, durableEvent, created, err := s.repo.AssistantInteractions().CreateDraftAttemptEventAtGeneration(ctx, draft, attempt, durableEvent, in.Generation)
	if err != nil {
		if errors.Is(err, repository.ErrInteractionGenerationChanged) {
			s.metrics.observeStageRetryAfterSessionLoss()
			return InteractionStageResult{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
		}
		if errors.Is(err, repository.ErrInteractionTerminal) {
			if in.ModeID == AssistantModeCreation {
				return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, err)
			}
			return InteractionStageResult{}, interactionRuntimeTerminalError(interaction.Lifecycle)
		}
		if errors.Is(err, repository.ErrInteractionAttemptConflict) {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, ErrInteractiveIdempotency)
		}
		if errors.Is(err, repository.ErrInteractionCommitClaimed) {
			return InteractionStageResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, err)
		}
		return InteractionStageResult{}, draftSavedRetryableError(err)
	}
	if !created {
		// The durable draft and attempt receipt are the authority on replay. Do
		// not append a second event when the original response was retried.
		if full, findErr := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, persisted.ID); findErr == nil {
			persisted = full
		}
		return s.interactionStageResult(ctx, userID, interaction, persisted.ID, persisted.Revision, persisted.Ready)
	}
	// Keep the rich draft available to an already-connected Studio stream. The
	// same event sequence is replaced with this process-local form; after a
	// restart, Snapshot supplies the durable draft payload instead.
	view := interactionDraftView(persisted, true)
	if persisted.ModeID == AssistantModeCreation {
		view.StateSequence = durableEvent.Sequence
	}
	s.cacheLiveEvent(in.InteractionID, in.Generation, durableEvent, map[string]any{
		"durable": true,
		"mode_id": persisted.ModeID,
		"draft":   view,
	}, now)
	if in.Ready && in.ModeID != AssistantModeCreation {
		_ = s.repo.AssistantInteractions().MarkNeedsConfirmation(ctx, in.InteractionID, now)
	}
	stageOutcome = "durable"
	return s.interactionStageResult(ctx, userID, interaction, persisted.ID, persisted.Revision, persisted.Ready)
}

func (s *AssistantInteractionService) interactionStageResult(ctx context.Context, userID string, interaction *model.AssistantInteraction, id string, revision int64, ready bool) (InteractionStageResult, error) {
	result := InteractionStageResult{DraftID: id, Revision: revision, ConfirmationRequired: ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}
	snapshot, err := s.snapshotWithDrafts(ctx, userID, interaction)
	if err != nil {
		return InteractionStageResult{}, err
	}
	for _, view := range snapshot.Drafts {
		if view.ID == id {
			result.Draft, result.ConfirmationRequired = &view, view.Ready
			if view.ModeID == AssistantModeCreation {
				result.AvailableActions = creationDraftActions(view)
				if snapshot.CommitResult != nil || snapshot.RecoveryErrorCode == InteractionErrorCommitReconciliationRequired || snapshot.Lifecycle == model.AssistantInteractionCompleted || snapshot.Lifecycle == model.AssistantInteractionAbandoned {
					result.AvailableActions = snapshot.AvailableActions
				}
			}
			break
		}
	}
	return result, nil
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
