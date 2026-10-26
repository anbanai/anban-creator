package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InteractionMessageInput struct {
	Generation      int64  `json:"generation"`
	ClientMessageID string `json:"client_message_id"`
	Text            string `json:"text"`
}

type InteractionAnswerInput struct {
	Generation int64
	AttemptID  string
	CallID     string
	Answers    []InteractiveAnswer
}

func (s *AssistantInteractionService) SendMessage(ctx context.Context, userID, interactionID string, in InteractionMessageInput) error {
	text := strings.TrimSpace(in.Text)
	if text == "" || strings.TrimSpace(in.ClientMessageID) == "" {
		return NewInteractionError(InteractionErrorMessageNotAccepted, 400, "请输入有效内容", "retry_message")
	}
	return s.runRuntimeCommand(ctx, userID, interactionID, in.Generation, InteractionOperationMessage, in.ClientMessageID, "session/prompt", map[string]any{"contentBlocks": []map[string]string{{"type": "text", "text": text}}}, len([]rune(text)))
}

func (s *AssistantInteractionService) AnswerQuestion(ctx context.Context, userID, interactionID string, in InteractionAnswerInput) error {
	if strings.TrimSpace(in.CallID) == "" || len(in.Answers) == 0 {
		return ErrInteractiveDraftInvalid
	}
	return s.runRuntimeCommand(ctx, userID, interactionID, in.Generation, InteractionOperationAnswer, in.AttemptID, "session/answer", map[string]any{"callId": in.CallID, "answers": in.Answers}, len(in.Answers))
}

func (s *AssistantInteractionService) runRuntimeCommand(ctx context.Context, userID, interactionID string, generation int64, operation InteractionOperation, attemptID, method string, params map[string]any, resultSize int) error {
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return NormalizeInteractionError(err)
	}
	if interaction.Generation != generation {
		return NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	if attemptID == "" {
		attemptID = NewInteractionAttemptID()
	}
	digest := sha256.Sum256([]byte(method + ":" + attemptID))
	payloadDigest := hex.EncodeToString(digest[:])
	if previous, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil {
		if previous.PayloadDigest != payloadDigest {
			return ErrInteractiveIdempotency
		}
		return nil
	}
	binding, err := s.ensureRuntimeLocked(ctx, userID, interaction)
	if err != nil {
		return NormalizeInteractionError(err)
	}
	if binding.runtime == nil {
		return NewInteractionError(InteractionErrorReconnecting, 409, "正在恢复连接，请稍候", "wait_for_recovery")
	}
	params["sessionId"] = binding.LogicalSessionID
	if method == "session/answer" {
		params["answer"] = map[string]any{"answers": params["answers"]}
		delete(params, "answers")
	}
	if err := binding.runtime.Command(ctx, method, params); err != nil {
		return NewInteractionError(InteractionErrorMessageNotAccepted, 409, "这次消息未送出，当前内容已保留", "retry_message").WithSnapshot(s.snapshotFromInteraction(interaction))
	}
	now := s.clock()
	if err := s.repo.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interactionID, AttemptID: attemptID, Operation: string(operation), Generation: generation, PayloadDigest: payloadDigest, Result: json.RawMessage(`{"accepted":true}`), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}); err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	sequence, sequenceErr := s.repo.AssistantInteractions().NextEventSequence(ctx, interactionID)
	if sequenceErr == nil {
		_ = s.repo.AssistantInteractions().CreateEvent(ctx, &model.AssistantInteractionEvent{ID: uuid.NewString(), InteractionID: interactionID, Sequence: sequence, Type: "user.message.accepted", Data: json.RawMessage(fmt.Sprintf(`{"length":%d}`, resultSize)), CreatedAt: now})
	}
	return nil
}

const (
	InteractionCommitCreated             = "created"
	InteractionCommitAlreadyCreated      = "already_created"
	InteractionCommitNeedsReconciliation = "needs_reconciliation"
)

// SetTaskService wires the existing Server-owned task creator into the
// interaction commit boundary. Runtime/session state is deliberately absent.
func (s *AssistantInteractionService) SetTaskService(tasks *TaskService) { s.taskSvc = tasks }

func (s *AssistantInteractionService) CommitInteraction(ctx context.Context, userID, interactionID string, req InteractionCommitRequest) (InteractionCommitResult, error) {
	if s.repo == nil || s.repo.AssistantInteractions() == nil || strings.TrimSpace(req.DraftID) == "" || req.Revision < 1 || strings.TrimSpace(req.IdempotencyKey) == "" || len(req.IdempotencyKey) > 200 {
		return InteractionCommitResult{}, ErrInteractiveDraftInvalid
	}
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InteractionCommitResult{}, NewInteractionError(InteractionErrorRestartRequired, 410, "当前连接已结束，已保留可恢复内容", "restore_draft", "restart")
		}
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	draft, err := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, req.DraftID)
	if err != nil || draft.InteractionID != interaction.ID || draft.Revision != req.Revision || !draft.Ready {
		return InteractionCommitResult{}, NewInteractionError(InteractionErrorDraftRevisionConflict, 409, "计划版本已更新，请恢复最新计划后确认", "reload_snapshot", "restore_draft")
	}
	digest := commitPayloadDigest(req)
	attemptID := "commit:" + req.IdempotencyKey
	if attempt, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil {
		if attempt.PayloadDigest != digest {
			return InteractionCommitResult{}, ErrInteractiveIdempotency
		}
		if len(attempt.Result) > 0 {
			var replay InteractionCommitResult
			if err := json.Unmarshal(attempt.Result, &replay); err != nil {
				return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
			}
			if replay.Status == InteractionCommitCreated {
				replay.Status = InteractionCommitAlreadyCreated
			}
			return replay, nil
		}
		return InteractionCommitResult{}, NewInteractionError(InteractionErrorCommitReconciliationRequired, 409, "创建结果需要核对，请查看任务状态", "view_task", "reconcile_commit")
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, findErr)
	}

	checkpoint, err := s.commitCheckpoint(ctx, userID, interaction, draft)
	if err != nil {
		return InteractionCommitResult{}, err
	}
	var result AssistantCommitResult
	switch draft.ModeID {
	case AssistantModeCreation:
		result, err = commitAssistantCreation(ctx, userID, checkpoint, s.taskSvc, s.repo)
	case AssistantModeReview:
		result, err = commitAssistantReview(ctx, userID, checkpoint, s.repo)
	default:
		err = ErrAssistantCheckpointInvalid
	}
	if err != nil {
		code := InteractionErrorCommitReconciliationRequired
		if errors.Is(err, ErrAssistantCheckpointInvalid) || errors.Is(err, ErrAssistantCreationInputsMissing) || errors.Is(err, ErrAssistantCheckpointConflict) {
			code = InteractionErrorDraftRevisionConflict
		}
		_ = s.repo.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interactionID, AttemptID: attemptID, Operation: string(InteractionOperationCommit), Generation: interaction.Generation, PayloadDigest: digest, ErrorCode: code, CreatedAt: s.clock(), ExpiresAt: s.clock().Add(24 * time.Hour)})
		if code == InteractionErrorDraftRevisionConflict {
			return InteractionCommitResult{}, NewInteractionError(code, 409, "计划当前不可确认，请恢复最新计划", "reload_snapshot", "restore_draft")
		}
		return InteractionCommitResult{}, NewInteractionError(code, 409, "创建结果需要核对，请查看任务状态", "view_task", "reconcile_commit")
	}
	raw, err := json.Marshal(InteractionCommitResult{Status: InteractionCommitCreated, TaskID: assistantCommitTaskID(result), Result: mustAssistantCommitJSON(result)})
	if err != nil {
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	final := InteractionCommitResult{Status: InteractionCommitCreated, TaskID: assistantCommitTaskID(result), Result: mustAssistantCommitJSON(result)}
	if err := s.repo.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interactionID, AttemptID: attemptID, Operation: string(InteractionOperationCommit), Generation: interaction.Generation, PayloadDigest: digest, Result: raw, CreatedAt: s.clock(), ExpiresAt: s.clock().Add(24 * time.Hour)}); err != nil {
		if existing, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil && existing.PayloadDigest == digest && len(existing.Result) > 0 {
			return final, nil
		}
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	_ = s.repo.AssistantInteractions().UpdateDraftState(ctx, draft.ID, model.AssistantInteractionDraftConsumed, s.clock())
	_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interactionID, model.AssistantInteractionCompleted, s.clock())
	return final, nil
}

func commitPayloadDigest(req InteractionCommitRequest) string {
	encoded, _ := json.Marshal(struct {
		DraftID  string
		Revision int64
		Options  json.RawMessage
	}{req.DraftID, req.Revision, req.Options})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (s *AssistantInteractionService) commitCheckpoint(ctx context.Context, userID string, interaction *model.AssistantInteraction, draft *model.AssistantInteractionDraft) (*model.AssistantCheckpoint, error) {
	checkpoint := &model.AssistantCheckpoint{ID: draft.ID, UserID: userID, ModeID: draft.ModeID, ProjectID: interaction.ProjectID, ContentID: interaction.ContentID, SchemaVersion: "interaction-v1", Payload: append(json.RawMessage(nil), draft.Payload...), PayloadDigest: draft.PayloadDigest, Status: model.AssistantCheckpointReady, SourceSessionID: interaction.ID, ExpiresAt: draft.ExpiresAt, CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt}
	if interaction.ProjectID != "" && s.repo.Projects() != nil {
		project, err := s.repo.Projects().FindByID(ctx, interaction.ProjectID)
		if err != nil {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		checkpoint.BaseProfileRevision = projectProfileFor(project).Version
	}
	if draft.ModeID == AssistantModeReview && s.repo.Analytics() != nil {
		if state, err := s.repo.Analytics().State(ctx, interaction.ProjectID); err == nil && state != nil {
			checkpoint.BaseAnalyticsRevision = state.Revision
		}
	}
	return checkpoint, nil
}

func assistantCommitTaskID(result AssistantCommitResult) string {
	if result.Task != nil {
		return result.Task.ID
	}
	return ""
}

func mustAssistantCommitJSON(result AssistantCommitResult) json.RawMessage {
	encoded, _ := json.Marshal(result)
	return encoded
}
