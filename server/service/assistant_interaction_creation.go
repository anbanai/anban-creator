package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type assistantCreationPreflightError struct{ error }

func (e *assistantCreationPreflightError) Unwrap() error { return e.error }

func knownCreationPreflightFailure(err error) bool {
	var preflight *assistantCreationPreflightError
	var taskPreflight *taskCreationPreflightError
	return errors.As(err, &preflight) || errors.As(err, &taskPreflight)
}

func (s *AssistantInteractionService) creationCommitAuthority(ctx context.Context, userID string, interaction *model.AssistantInteraction) (InteractionCommitResult, bool, error) {
	receipt, err := s.repo.AssistantInteractions().FindLatestCommitAttempt(ctx, interaction.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return InteractionCommitResult{}, false, nil
	}
	if err != nil {
		return InteractionCommitResult{}, false, errors.Join(ErrInteractivePersistence, err)
	}
	if len(receipt.Result) > 0 {
		var result InteractionCommitResult
		if err := json.Unmarshal(receipt.Result, &result); err != nil {
			return InteractionCommitResult{}, true, errors.Join(ErrInteractivePersistence, err)
		}
		if result.Status == InteractionCommitCreated {
			result.Status = InteractionCommitAlreadyCreated
		}
		return result, true, nil
	}
	if receipt.ErrorCode == InteractionErrorCommitReconciliationRequired || (receipt.Status == model.AssistantInteractionAttemptPending && receipt.ErrorCode == "") {
		return InteractionCommitResult{}, true, s.creationErrorWithSnapshot(ctx, userID, interaction, NewInteractionError(InteractionErrorCommitReconciliationRequired, 409, "创建结果需要核对，请查看任务状态", "view_task", "reconcile_commit"))
	}
	return InteractionCommitResult{}, false, nil
}

func (s *AssistantInteractionService) ContinueInteraction(ctx context.Context, userID, interactionID string, req InteractionContinueRequest) (InteractionSnapshot, error) {
	if strings.TrimSpace(req.DraftID) == "" || req.Revision < 1 {
		return InteractionSnapshot{}, ErrInteractiveDraftInvalid
	}
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return InteractionSnapshot{}, err
	}
	if err := s.repo.AssistantInteractions().TransitionCreationDraft(ctx, userID, interactionID, req.DraftID, req.Revision, nil, s.clock()); err != nil {
		if errors.Is(err, repository.ErrInteractionDraftState) || errors.Is(err, gorm.ErrRecordNotFound) {
			return InteractionSnapshot{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, err)
		}
		return InteractionSnapshot{}, errors.Join(ErrInteractivePersistence, err)
	}
	interaction.Lifecycle = model.AssistantInteractionActive
	return s.snapshotWithDrafts(ctx, userID, interaction)
}

func (s *AssistantInteractionService) commitCreationInteraction(ctx context.Context, userID string, interaction *model.AssistantInteraction, draft *model.AssistantInteractionDraft, req InteractionCommitRequest, digest string) (InteractionCommitResult, error) {
	latest, err := s.repo.AssistantInteractions().FindLatestDraft(ctx, userID, interaction.ID, AssistantModeCreation)
	if err != nil {
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	if latest.ID != draft.ID || latest.Revision != req.Revision || draft.ConfirmationState != model.AssistantInteractionDraftPending || !draft.ExpiresAt.After(s.clock()) {
		return InteractionCommitResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, repository.ErrInteractionDraftState)
	}
	issues, validationErr := validateInteractionCreation(draft.Payload)
	if validationErr != nil {
		return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationDraftError(InteractionErrorDraftInvalid, nil))
	}
	if len(issues) > 0 {
		return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationIssuesError(issues))
	}
	if !draft.Ready {
		return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationDraftError(InteractionErrorDraftNotReady, nil))
	}
	payload, err := mergeInteractionCommitOptions(draft.ModeID, draft.Payload, req.Options)
	if err != nil {
		return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationDraftError(InteractionErrorDraftInvalid, nil))
	}
	issues, validationErr = validateInteractionCreation(payload)
	if validationErr != nil {
		return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationDraftError(InteractionErrorDraftInvalid, nil))
	}
	if len(issues) > 0 {
		return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationIssuesError(issues))
	}
	checkpoint, err := s.commitCheckpoint(ctx, userID, interaction, draft, req.Options)
	if err != nil {
		return InteractionCommitResult{}, err
	}
	attemptID := "commit:" + req.IdempotencyKey
	attempt := &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interaction.ID, UserID: userID, AttemptID: attemptID, Operation: string(InteractionOperationCommit), Generation: interaction.Generation, PayloadDigest: digest, Status: model.AssistantInteractionAttemptPending, CreatedAt: s.clock(), ExpiresAt: s.clock().Add(InteractionCommitReceiptRetention)}
	if err := s.repo.AssistantInteractions().TransitionCreationDraft(ctx, userID, interaction.ID, draft.ID, draft.Revision, attempt, s.clock()); err != nil {
		if errors.Is(err, repository.ErrInteractionDraftState) {
			if result, handled, authorityErr := s.creationCommitAuthority(ctx, userID, interaction); handled || authorityErr != nil {
				return result, authorityErr
			}
			return InteractionCommitResult{}, s.stageDraftRevisionConflictError(ctx, userID, interaction, err)
		}
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	result, err := commitAssistantCreation(ctx, userID, checkpoint, s.taskSvc, s.repo)
	if err != nil {
		known := knownCreationPreflightFailure(err)
		code := InteractionErrorCommitReconciliationRequired
		if known {
			code = InteractionErrorDraftInvalid
		}
		if errors.Is(err, ErrAssistantCreationInputsMissing) {
			code = InteractionErrorCreationInputsMissing
		}
		if persistErr := s.repo.AssistantInteractions().FinishCreationCommit(ctx, interaction.ID, draft.ID, attemptID, nil, code, known, s.clock()); persistErr != nil {
			return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, persistErr)
		}
		if known {
			var inputErr *taskCreationInputError
			if errors.As(err, &inputErr) {
				structured := creationDraftError(InteractionErrorDraftInvalid, []InteractionBlockingIssue{{Code: "invalid", Field: inputErr.field, Message: inputErr.Error()}})
				structured.cause = err
				return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, structured)
			}
			if !errors.Is(err, ErrAssistantCheckpointInvalid) && !errors.Is(err, ErrAssistantCreationInputsMissing) {
				return InteractionCommitResult{}, err
			}
			return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, creationDraftError(code, nil))
		}
		return InteractionCommitResult{}, s.creationErrorWithSnapshot(ctx, userID, interaction, NewInteractionError(code, 409, "创建结果需要核对，请查看任务状态", "view_task", "reconcile_commit"))
	}
	final := InteractionCommitResult{Status: InteractionCommitCreated, TaskID: assistantCommitTaskID(result), Result: mustAssistantCommitJSON(result)}
	raw, err := json.Marshal(final)
	if err != nil {
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	if err := s.repo.AssistantInteractions().FinishCreationCommit(ctx, interaction.ID, draft.ID, attemptID, raw, "", false, s.clock()); err != nil {
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	return final, nil
}

// Only immutable validation and an absence of contradictory task evidence may
// turn an old generic conflict into an actionable pre-creation failure.
func (s *AssistantInteractionService) legacyCreationFailure(ctx context.Context, userID string, draft *model.AssistantInteractionDraft) *InteractionError {
	if s.repo.Tasks() == nil {
		return nil
	}
	if _, err := s.repo.Tasks().FindByAssistantCheckpointID(ctx, userID, draft.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	issues, err := validateInteractionCreation(draft.Payload)
	if err != nil {
		return creationDraftError(InteractionErrorDraftInvalid, nil)
	}
	if len(issues) > 0 {
		return creationIssuesError(issues)
	}
	return nil
}
