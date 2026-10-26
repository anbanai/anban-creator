package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
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
	if strings.TrimSpace(in.CallID) == "" || strings.TrimSpace(in.AttemptID) == "" || len(in.Answers) == 0 {
		return NewInteractionError(InteractionErrorMessageNotAccepted, 400, "回答信息不完整，请重试", "retry_message")
	}
	return s.runRuntimeCommand(ctx, userID, interactionID, in.Generation, InteractionOperationAnswer, in.AttemptID, "session/answer", map[string]any{"callId": in.CallID, "answers": in.Answers}, len(in.Answers))
}

func (s *AssistantInteractionService) commandErrorWithSnapshot(ctx context.Context, userID string, interaction *model.AssistantInteraction, err error) error {
	if err == nil {
		return nil
	}
	structured := NormalizeInteractionError(err)
	if structured == nil || interaction == nil {
		return structured
	}
	if snapshot, snapshotErr := s.snapshotWithDrafts(ctx, userID, interaction); snapshotErr == nil {
		return structured.WithSnapshot(snapshot)
	}
	return structured.WithSnapshot(s.snapshotFromInteraction(interaction))
}

func (s *AssistantInteractionService) runRuntimeCommand(ctx context.Context, userID, interactionID string, generation int64, operation InteractionOperation, attemptID, method string, params map[string]any, resultSize int) error {
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NewInteractionError(InteractionErrorRestartRequired, 404, "当前对话已结束，已保留可恢复内容", "restore_draft", "restart")
		}
		return NormalizeInteractionError(err)
	}
	if interaction.Generation != generation {
		return NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	if attemptID == "" {
		attemptID = NewInteractionAttemptID()
	}
	payloadDigest := interactionCommandDigest(method, attemptID, params)
	if previous, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil {
		if previous.PayloadDigest != payloadDigest {
			return ErrInteractiveIdempotency
		}
		if previous.Status == model.AssistantInteractionAttemptPending || (previous.Status == "" && len(previous.Result) == 0 && previous.ErrorCode == "") {
			return NewInteractionError(InteractionErrorReconnecting, 409, "正在确认上一条操作，请稍候", "wait_for_recovery")
		}
		if previous.Status != model.AssistantInteractionAttemptFailed && len(previous.Result) > 0 {
			return nil
		}
		if previous.Status != model.AssistantInteractionAttemptFailed {
			return NewInteractionError(InteractionErrorReconnecting, 409, "正在确认上一条操作，请稍候", "wait_for_recovery")
		}
		if err := s.repo.AssistantInteractions().UpdateAttempt(ctx, interactionID, attemptID, model.AssistantInteractionAttemptPending, nil, ""); err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return errors.Join(ErrInteractivePersistence, findErr)
	} else if err := s.repo.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interactionID, AttemptID: attemptID, Operation: string(operation), Generation: generation, PayloadDigest: payloadDigest, Status: model.AssistantInteractionAttemptPending, CreatedAt: s.clock(), ExpiresAt: s.clock().Add(InteractionAttemptRetention)}); err != nil {
		if existing, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil {
			if existing.PayloadDigest != payloadDigest {
				return ErrInteractiveIdempotency
			}
			return NewInteractionError(InteractionErrorReconnecting, 409, "正在确认上一条操作，请稍候", "wait_for_recovery")
		}
		return errors.Join(ErrInteractivePersistence, err)
	}
	binding, err := s.ensureRuntimeLocked(ctx, userID, interaction)
	if err != nil {
		_ = s.repo.AssistantInteractions().UpdateAttempt(ctx, interactionID, attemptID, model.AssistantInteractionAttemptFailed, nil, InteractionErrorMessageNotAccepted)
		return s.commandErrorWithSnapshot(ctx, userID, interaction, err)
	}
	if binding.Generation != generation {
		_ = s.repo.AssistantInteractions().UpdateAttempt(ctx, interactionID, attemptID, model.AssistantInteractionAttemptFailed, nil, InteractionErrorGenerationChanged)
		return s.commandErrorWithSnapshot(ctx, userID, interaction, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot"))
	}
	if binding.runtime == nil {
		_ = s.repo.AssistantInteractions().UpdateAttempt(ctx, interactionID, attemptID, model.AssistantInteractionAttemptFailed, nil, InteractionErrorReconnecting)
		return s.commandErrorWithSnapshot(ctx, userID, interaction, NewInteractionError(InteractionErrorReconnecting, 409, "正在恢复连接，请稍候", "wait_for_recovery"))
	}
	params["attempt_id"] = attemptID
	params["sessionId"] = binding.LogicalSessionID
	params["interaction_id"] = interactionID
	params["generation"] = generation
	if method == "session/answer" {
		params["answer"] = map[string]any{"answers": params["answers"]}
		delete(params, "answers")
	}
	if err := binding.runtime.Command(ctx, method, params); err != nil {
		_ = s.repo.AssistantInteractions().UpdateAttempt(ctx, interactionID, attemptID, model.AssistantInteractionAttemptFailed, nil, InteractionErrorReconnecting)
		_, replacementErr := s.replaceRuntimeLocked(ctx, userID, interaction, generation, "runtime_command_failed")
		if replacementErr != nil {
			return s.commandErrorWithSnapshot(ctx, userID, interaction, replacementErr)
		}
		return s.commandErrorWithSnapshot(ctx, userID, interaction, NewInteractionError(InteractionErrorReconnecting, 409, "正在恢复连接，请稍候", "wait_for_recovery"))
	}
	now := s.clock()
	if err := s.repo.AssistantInteractions().UpdateAttempt(ctx, interactionID, attemptID, model.AssistantInteractionAttemptCompleted, json.RawMessage(`{"accepted":true}`), ""); err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	_, _ = s.repo.AssistantInteractions().AppendEvent(ctx, interactionID, generation, &model.AssistantInteractionEvent{ID: uuid.NewString(), InteractionID: interactionID, Type: "user.message.accepted", Data: json.RawMessage(fmt.Sprintf(`{"length":%d}`, resultSize)), CreatedAt: now})
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
	if s.repo == nil || s.repo.AssistantInteractions() == nil || strings.TrimSpace(req.DraftID) == "" || req.Revision < 1 || strings.TrimSpace(req.IdempotencyKey) == "" || len(strings.TrimSpace(req.IdempotencyKey)) > 121 {
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
	if interaction.Lifecycle == model.AssistantInteractionAbandoned {
		return InteractionCommitResult{}, NewInteractionError(InteractionErrorRestartRequired, 410, "当前对话已结束，已保留可恢复内容", "restore_draft", "restart")
	}
	digest := commitPayloadDigest(req)
	attemptID := "commit:" + req.IdempotencyKey
	if attempt, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil {
		if attempt.PayloadDigest != digest {
			return InteractionCommitResult{}, ErrInteractiveIdempotency
		}
		if len(attempt.Result) > 0 {
			s.metrics.observeCommitDuplicate()
			var replay InteractionCommitResult
			if err := json.Unmarshal(attempt.Result, &replay); err != nil {
				return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
			}
			if replay.Status == InteractionCommitCreated {
				replay.Status = InteractionCommitAlreadyCreated
			}
			return replay, nil
		}
		if attempt.ErrorCode == InteractionErrorDraftRevisionConflict {
			return InteractionCommitResult{}, NewInteractionError(InteractionErrorDraftRevisionConflict, 409, "计划当前不可确认，请恢复最新计划", "reload_snapshot", "restore_draft")
		}
		return InteractionCommitResult{}, NewInteractionError(InteractionErrorCommitReconciliationRequired, 409, "创建结果需要核对，请查看任务状态", "view_task", "reconcile_commit")
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, findErr)
	}
	if interaction.Lifecycle == model.AssistantInteractionCompleted {
		if latest, latestErr := s.repo.AssistantInteractions().FindLatestCommitAttempt(ctx, interactionID); latestErr == nil && len(latest.Result) > 0 {
			s.metrics.observeCommitDuplicate()
			var replay InteractionCommitResult
			if err := json.Unmarshal(latest.Result, &replay); err != nil {
				return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
			}
			if replay.Status == InteractionCommitCreated {
				replay.Status = InteractionCommitAlreadyCreated
			}
			return replay, nil
		}
		return InteractionCommitResult{}, NewInteractionError(InteractionErrorCommitReconciliationRequired, 409, "创建结果需要核对，请查看任务状态", "view_task", "reconcile_commit")
	}
	// A receipt is the authority for a replay. Check it before the draft so
	// cleanup at the shared 30-day retention boundary cannot turn an already
	// applied commit into a false revision conflict.
	draft, err := s.repo.AssistantInteractions().FindDraftForUser(ctx, userID, req.DraftID)
	if err != nil || draft.InteractionID != interaction.ID || draft.Revision != req.Revision || !draft.Ready {
		return InteractionCommitResult{}, NewInteractionError(InteractionErrorDraftRevisionConflict, 409, "计划版本已更新，请恢复最新计划后确认", "reload_snapshot", "restore_draft")
	}
	if draft.ModeID == AssistantModeProfile {
		result, profileErr := s.commitProfileInteraction(ctx, userID, interactionID, interaction.Generation, draft, req, digest)
		if profileErr == nil {
			return result, nil
		}
		if errors.Is(profileErr, ErrAssistantCheckpointInvalid) || errors.Is(profileErr, ErrProjectProfileVersionConflict) {
			return InteractionCommitResult{}, NewInteractionError(InteractionErrorDraftRevisionConflict, 409, "计划当前不可确认，请恢复最新计划", "reload_snapshot", "restore_draft")
		}
		return InteractionCommitResult{}, profileErr
	}

	checkpoint, err := s.commitCheckpoint(ctx, userID, interaction, draft, req.Options)
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
		_ = s.repo.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interactionID, AttemptID: attemptID, Operation: string(InteractionOperationCommit), Generation: interaction.Generation, PayloadDigest: digest, ErrorCode: code, CreatedAt: s.clock(), ExpiresAt: s.clock().Add(InteractionCommitReceiptRetention)})
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
	if err := s.repo.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interactionID, AttemptID: attemptID, Operation: string(InteractionOperationCommit), Generation: interaction.Generation, PayloadDigest: digest, Result: raw, CreatedAt: s.clock(), ExpiresAt: s.clock().Add(InteractionCommitReceiptRetention)}); err != nil {
		if existing, findErr := s.repo.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil && existing.PayloadDigest == digest && len(existing.Result) > 0 {
			s.metrics.observeCommitDuplicate()
			return final, nil
		}
		return InteractionCommitResult{}, errors.Join(ErrInteractivePersistence, err)
	}
	_ = s.repo.AssistantInteractions().UpdateDraftState(ctx, draft.ID, model.AssistantInteractionDraftConsumed, s.clock())
	_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interactionID, model.AssistantInteractionCompleted, s.clock())
	return final, nil
}

type interactionProfileDraftPayload struct {
	Project        InteractiveProject   `json:"project"`
	Profile        model.ProjectProfile `json:"profile"`
	Progress       InteractiveProgress  `json:"progress"`
	MissingFields  []string             `json:"missing_fields"`
	InferredFields []string             `json:"inferred_fields"`
	MemoryAppend   string               `json:"memory_append,omitempty"`
	Ready          bool                 `json:"ready"`
}

// commitProfileInteraction is the Server-owned profile confirmation boundary.
// The runtime has already produced a durable draft; this transaction applies
// only its structured project/profile fields and records the replay receipt
// beside the write so a retry cannot create a second project.
func (s *AssistantInteractionService) commitProfileInteraction(ctx context.Context, userID, interactionID string, generation int64, draft *model.AssistantInteractionDraft, req InteractionCommitRequest, digest string) (InteractionCommitResult, error) {
	var final InteractionCommitResult
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		interaction, err := tx.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if interaction.Generation != generation {
			return NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
		}
		attemptID := "commit:" + req.IdempotencyKey
		if existing, findErr := tx.AssistantInteractions().FindAttempt(ctx, interactionID, attemptID); findErr == nil {
			if existing.PayloadDigest != digest {
				return ErrInteractiveIdempotency
			}
			if len(existing.Result) == 0 {
				return NewInteractionError(InteractionErrorCommitReconciliationRequired, 409, "创建结果需要核对，请查看任务状态", "view_task", "reconcile_commit")
			}
			if err := json.Unmarshal(existing.Result, &final); err != nil {
				return errors.Join(ErrInteractivePersistence, err)
			}
			if final.Status == InteractionCommitCreated {
				final.Status = InteractionCommitAlreadyCreated
			}
			return nil
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return errors.Join(ErrInteractivePersistence, findErr)
		}

		var payload interactionProfileDraftPayload
		if err := json.Unmarshal(draft.Payload, &payload); err != nil {
			return fmt.Errorf("%w: invalid profile draft payload: %v", ErrAssistantCheckpointInvalid, err)
		}
		if !payload.Ready || !draft.Ready {
			return ErrAssistantCheckpointInvalid
		}
		if err := payload.Profile.Validate(); err != nil {
			return fmt.Errorf("%w: invalid profile draft: %v", ErrAssistantCheckpointInvalid, err)
		}

		var project *model.Project
		var profile model.ProjectProfile
		if strings.TrimSpace(interaction.ProjectID) != "" {
			project, err = tx.Projects().FindByIDForUpdate(ctx, interaction.ProjectID)
			if err != nil {
				return errors.Join(ErrInteractivePersistence, err)
			}
			if project.UserID != userID {
				return ErrInteractiveSessionOwner
			}
			current := projectProfileFor(project)
			if payload.Profile.Version > 0 && payload.Profile.Version != current.Version {
				return ErrProjectProfileVersionConflict
			}
			profile = current
			profile.Dimensions = preserveOmittedProfileDimensions(current.Dimensions, payload.Profile.Dimensions)
			profile.AnalysisLimits = append([]string(nil), payload.Profile.AnalysisLimits...)
			profile.FollowUpQuestions = append([]string(nil), payload.Profile.FollowUpQuestions...)
			profile.Status = model.ProfileStatusConfirmed
			profile.ApplicationSource = model.ProfileApplicationSourceUserConfirmation
			if profile.InitializationStatus != model.ProfileInitializationQueued && profile.InitializationStatus != model.ProfileInitializationRunning {
				profile.InitializationStatus = model.ProfileInitializationReady
				profile.LastError = ""
			}
			profile.Version = current.Version + 1
			if err := profile.Validate(); err != nil {
				return fmt.Errorf("%w: invalid confirmed profile: %v", ErrAssistantCheckpointInvalid, err)
			}
			project.Profile = datatypes.NewJSONType(profile)
			if err := tx.Projects().Update(ctx, project); err != nil {
				return errors.Join(ErrInteractivePersistence, err)
			}
		} else {
			profile = payload.Profile
			profile.Status = model.ProfileStatusConfirmed
			profile.ApplicationSource = model.ProfileApplicationSourceUserConfirmation
			profile.InitializationStatus = model.ProfileInitializationReady
			profile.LastError = ""
			profile.Version = 1
			if err := profile.Validate(); err != nil {
				return fmt.Errorf("%w: invalid confirmed profile: %v", ErrAssistantCheckpointInvalid, err)
			}
			project = &model.Project{
				Name:         payload.Project.Name,
				Platform:     payload.Project.Platform,
				Instructions: payload.Project.Instructions,
				Keywords:     payload.Project.Keywords,
				VisualStyle:  payload.Project.VisualStyle,
				Profile:      datatypes.NewJSONType(profile),
			}
			projectSvc := NewProjectService(tx, nil)
			if err := projectSvc.prepareCreate(userID, project); err != nil {
				return fmt.Errorf("%w: %v", ErrAssistantCheckpointInvalid, err)
			}
			if err := tx.Projects().Create(ctx, project); err != nil {
				return errors.Join(ErrInteractivePersistence, err)
			}
			now := s.clock()
			if err := tx.AssistantInteractions().UpdateProject(ctx, interactionID, project.ID, now); err != nil {
				return errors.Join(ErrInteractivePersistence, err)
			}
		}

		dimensions, err := json.Marshal(profile.Dimensions)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		snapshot, err := json.Marshal(profile)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if err := tx.ProjectProfileRevisions().Create(ctx, &model.ProjectProfileRevision{ID: uuid.NewString(), ProjectID: project.ID, Revision: profile.Version, SixDimensions: dimensions, ProfileSnapshot: snapshot, SourceTaskID: profile.AnalysisTaskID}); err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if err := persistProfileState(ctx, tx, project.ID, profile); err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}

		committed := AssistantCommitResult{ModeID: AssistantModeProfile, Status: "committed", Project: project, Profile: &profile}
		resultJSON, err := json.Marshal(committed)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		final = InteractionCommitResult{Status: InteractionCommitCreated, Result: resultJSON}
		receiptJSON, err := json.Marshal(final)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		now := s.clock()
		if err := tx.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: interactionID, AttemptID: attemptID, Operation: string(InteractionOperationCommit), Generation: generation, PayloadDigest: digest, Result: receiptJSON, CreatedAt: now, ExpiresAt: now.Add(InteractionCommitReceiptRetention)}); err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if err := tx.AssistantInteractions().UpdateDraftState(ctx, draft.ID, model.AssistantInteractionDraftConsumed, now); err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		return tx.AssistantInteractions().UpdateLifecycle(ctx, interactionID, model.AssistantInteractionCompleted, now)
	})
	if err != nil {
		return InteractionCommitResult{}, err
	}
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

func (s *AssistantInteractionService) commitCheckpoint(ctx context.Context, userID string, interaction *model.AssistantInteraction, draft *model.AssistantInteractionDraft, options json.RawMessage) (*model.AssistantCheckpoint, error) {
	payload, err := mergeInteractionCommitOptions(draft.ModeID, draft.Payload, options)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	checkpoint := &model.AssistantCheckpoint{ID: draft.ID, UserID: userID, ModeID: draft.ModeID, ProjectID: interaction.ProjectID, ContentID: interaction.ContentID, SchemaVersion: "interaction-v1", Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Status: model.AssistantCheckpointReady, SourceSessionID: interaction.ID, ExpiresAt: draft.ExpiresAt, CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt}
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

// mergeInteractionCommitOptions keeps confirmation-time UI choices separate
// from the Agent-owned durable draft. Only the small set of Server-supported
// creation settings may be overlaid; arbitrary payload injection would turn
// the commit endpoint into a second draft writer.
func mergeInteractionCommitOptions(modeID string, payload, options json.RawMessage) (json.RawMessage, error) {
	if len(options) == 0 || string(options) == "null" {
		return append(json.RawMessage(nil), payload...), nil
	}
	if modeID != AssistantModeCreation {
		return nil, ErrAssistantCheckpointInvalid
	}
	var optionEnvelope map[string]json.RawMessage
	if err := json.Unmarshal(options, &optionEnvelope); err != nil || optionEnvelope == nil || len(optionEnvelope) != 1 {
		return nil, ErrAssistantCheckpointInvalid
	}
	planPatchRaw, ok := optionEnvelope["plan"]
	if !ok {
		return nil, ErrAssistantCheckpointInvalid
	}
	var planPatch map[string]json.RawMessage
	if err := json.Unmarshal(planPatchRaw, &planPatch); err != nil || planPatch == nil {
		return nil, ErrAssistantCheckpointInvalid
	}
	allowed := map[string]struct{}{
		"content_type": {}, "execution_profile": {}, "image_ratio": {}, "image_capability_key": {},
		"assets": {}, "video_edit_brief": {}, "video_edit_preferences": {},
	}
	for key := range planPatch {
		if _, ok := allowed[key]; !ok {
			return nil, ErrAssistantCheckpointInvalid
		}
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope == nil {
		return nil, ErrAssistantCheckpointInvalid
	}
	planRaw, hasNestedPlan := envelope["plan"]
	var plan map[string]json.RawMessage
	if hasNestedPlan {
		if err := json.Unmarshal(planRaw, &plan); err != nil || plan == nil {
			return nil, ErrAssistantCheckpointInvalid
		}
	} else {
		plan = envelope
	}
	for key, value := range planPatch {
		plan[key] = value
	}
	if hasNestedPlan {
		encodedPlan, err := json.Marshal(plan)
		if err != nil {
			return nil, ErrAssistantCheckpointInvalid
		}
		envelope["plan"] = encodedPlan
		return json.Marshal(envelope)
	}
	return json.Marshal(plan)
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
