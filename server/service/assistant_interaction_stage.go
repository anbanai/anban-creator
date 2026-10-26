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
	if s.repo == nil || s.repo.AssistantInteractions() == nil {
		return InteractionStageResult{}, ErrInteractivePersistence
	}
	if strings.TrimSpace(in.InteractionID) == "" || strings.TrimSpace(in.ModeID) == "" || strings.TrimSpace(in.AttemptID) == "" || len(in.Payload) == 0 {
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
		return InteractionStageResult{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	digest := sha256.Sum256(in.Payload)
	if in.PayloadDigest == "" || !strings.EqualFold(in.PayloadDigest, hex.EncodeToString(digest[:])) {
		return InteractionStageResult{}, ErrInteractiveDraftInvalid
	}
	if current, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, in.InteractionID, in.AttemptID); findErr == nil {
		if current.PayloadDigest != in.PayloadDigest {
			return InteractionStageResult{}, ErrInteractiveIdempotency
		}
		var replay model.AssistantInteractionDraft
		if err := json.Unmarshal(current.Result, &replay); err != nil {
			return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, err)
		}
		return InteractionStageResult{DraftID: replay.ID, Revision: replay.Revision, ConfirmationRequired: replay.Ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, findErr)
	}
	revision := int64(1)
	if current, findErr := s.repo.AssistantInteractions().FindLatestDraft(ctx, userID, in.InteractionID, in.ModeID); findErr == nil {
		revision = current.Revision + 1
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, findErr)
	}
	now := s.clock()
	draft := &model.AssistantInteractionDraft{ID: uuid.NewString(), InteractionID: in.InteractionID, ModeID: in.ModeID, Revision: revision, Payload: append(json.RawMessage(nil), in.Payload...), PayloadDigest: in.PayloadDigest, Ready: in.Ready, ConfirmationState: model.AssistantInteractionDraftPending, SourceGeneration: in.Generation, ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now}
	attempt := &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: in.InteractionID, AttemptID: in.AttemptID, Operation: string(InteractionOperationStage), Generation: in.Generation, PayloadDigest: in.PayloadDigest, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	persisted, _, err := s.repo.AssistantInteractions().CreateDraftIfAttemptNew(ctx, draft, attempt)
	if err != nil {
		if errors.Is(err, repository.ErrInteractionAttemptConflict) {
			return InteractionStageResult{}, ErrInteractiveIdempotency
		}
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	sequence, sequenceErr := s.repo.AssistantInteractions().NextEventSequence(ctx, in.InteractionID)
	eventData, _ := json.Marshal(map[string]any{"durable": true, "draft_id": persisted.ID, "revision": persisted.Revision, "mode_id": persisted.ModeID, "ready": persisted.Ready, "payload_digest": persisted.PayloadDigest})
	if sequenceErr != nil || s.repo.AssistantInteractions().CreateEvent(ctx, &model.AssistantInteractionEvent{ID: uuid.NewString(), InteractionID: in.InteractionID, Sequence: sequence, Type: "draft.ready", Data: eventData, CreatedAt: now}) != nil {
		return InteractionStageResult{}, errors.Join(ErrInteractivePersistence, errors.New("interaction event persistence failed"))
	}
	if in.Ready {
		_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, in.InteractionID, model.AssistantInteractionNeedsConfirmation, now)
	}
	return InteractionStageResult{DraftID: persisted.ID, Revision: persisted.Revision, ConfirmationRequired: persisted.Ready, AvailableActions: []string{"confirm_draft", "continue_editing"}, Durable: true}, nil
}
