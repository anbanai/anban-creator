package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAssistantInteractionCommitRecoveryServiceTest(t *testing.T) *AssistantInteractionService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}, &model.Project{}, &model.Task{}, &model.ProjectProfileState{}, &model.ProjectProfileRevision{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	clock := func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	svc := NewAssistantInteractionService(repository.New(db), nil, clock)
	svc.SetTaskService(NewTaskService(svc.repo, nil, nil, nil, "", nil, nil))
	return svc
}

func TestCommitInteractionReconcilesTaskCreatedBeforeReceipt(t *testing.T) {
	svc := newAssistantInteractionCommitRecoveryServiceTest(t)
	ctx := context.Background()
	project := &model.Project{ID: uuid.NewString(), UserID: "user-1", Platform: model.PlatformWechat, Name: "任务确认恢复", Status: model.ProjectStatusActive}
	if err := svc.repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: project.ID}, "commit-recovery-create")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write","content_type":"wechat-article","topic":"恢复中断的任务","angle":"从已确认计划继续","expression_form":"article","profile_context":[],"temporary_overrides":[]}}`)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(ctx, "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "commit-recovery-stage", ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	request := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "commit-recovery-key"}
	claim := &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:" + request.IdempotencyKey,
		Operation: string(InteractionOperationCommit), Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(request),
		Status: model.AssistantInteractionAttemptPending, ClaimToken: uuid.NewString(), CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}
	if _, claimed, err := svc.repo.AssistantInteractions().ClaimInteractionCommit(ctx, "user-1", snapshot.ID, stage.DraftID, stage.Revision, claim); err != nil || !claimed {
		t.Fatalf("seed commit claim claimed=%v err=%v", claimed, err)
	}
	now := svc.clock()
	svc.clock = func() time.Time { return now.Add(model.AssistantInteractionCommitClaimLease + time.Second) }
	_, planJSON, err := decodeAssistantCreationPlan(payload)
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := stage.DraftID
	task := &model.Task{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: project.ID, AssistantCheckpointID: &checkpointID,
		Type: model.TaskTypeWechatArticle, Status: model.TaskStatusPending,
		Prompt: "请根据以下经用户确认的本次创作需求完成内容创作。项目画像和平台信息由 Server 作为独立冻结上下文提供。用户未指定的内容生产细节由你根据能力和上下文决定；不得超出明确的要求、限制或素材范围。\n\n" + string(planJSON),
	}
	if err := svc.repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	result, err := svc.CommitInteraction(ctx, "user-1", snapshot.ID, request)
	if err != nil {
		t.Fatalf("reconcile interrupted commit = %#v, %v", result, err)
	}
	if result.Status != InteractionCommitCreated || result.TaskID != task.ID {
		t.Fatalf("reconciled result = %#v, want existing task %q", result, task.ID)
	}
	receipt, err := svc.repo.AssistantInteractions().FindAttempt(ctx, snapshot.ID, "commit:"+request.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != model.AssistantInteractionAttemptCompleted || len(receipt.Result) == 0 {
		t.Fatalf("commit receipt = %#v, want completed durable receipt", receipt)
	}
}

func TestCommitInteractionDoesNotStealAnActivePendingClaim(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	snapshot, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "profile-active-claim")
	if err != nil {
		t.Fatal(err)
	}
	profile := model.NewProjectProfile()
	profile.Dimensions.Identity.Content["name"] = "并发确认账号"
	payload, err := json.Marshal(map[string]any{
		"project": map[string]string{"name": "并发确认账号", "platform": "wechat"},
		"profile": profile, "progress": map[string]string{"stage": "ready"},
		"missing_fields": []string{}, "inferred_fields": []string{}, "ready": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(ctx, "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "profile-active-claim-stage", ModeID: AssistantModeProfile,
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "profile-active-claim-commit"}
	claim := &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:" + request.IdempotencyKey,
		Operation: string(InteractionOperationCommit), Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(request),
		Status: model.AssistantInteractionAttemptPending, ClaimToken: uuid.NewString(), CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}
	if _, claimed, err := svc.repo.AssistantInteractions().ClaimInteractionCommit(ctx, "user-1", snapshot.ID, stage.DraftID, stage.Revision, claim); err != nil || !claimed {
		t.Fatalf("seed active commit claim claimed=%v err=%v", claimed, err)
	}

	otherServer := NewAssistantInteractionService(svc.repo, nil, svc.clock)
	result, err := otherServer.CommitInteraction(ctx, "user-1", snapshot.ID, request)
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("second server commit = %#v, %v; want reconciliation while the first claim is active", result, err)
	}
	current, err := svc.repo.AssistantInteractions().FindByIDForUser(ctx, "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ProjectID != "" {
		t.Fatalf("second server created project %q while the claim was active", current.ProjectID)
	}
}

func TestCommitInteractionUsesDurableDraftAfterRuntimeLoss(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write","content_type":"wechat-article"}}`)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-1", ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	// No runtime or session is needed to reach the durable commit boundary.
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "commit-1"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorCommitRetryable || result.Status != "" {
		t.Fatalf("commit = %#v, %v", result, err)
	}
	receipt, receiptErr := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "commit:commit-1")
	if receiptErr != nil {
		t.Fatal(receiptErr)
	}
	if !receipt.ExpiresAt.Equal(svc.clock().Add(InteractionCommitReceiptRetention)) {
		t.Fatalf("commit receipt expiry = %s, want %s", receipt.ExpiresAt, svc.clock().Add(InteractionCommitReceiptRetention).Format(time.RFC3339))
	}
	if receipt.Status != model.AssistantInteractionAttemptFailed || receipt.ErrorCode != InteractionErrorCommitRetryable {
		t.Fatalf("preflight commit receipt = %#v, want retryable and released", receipt)
	}
	replay, replayErr := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "commit-1"})
	if replayErr == nil || NormalizeInteractionError(replayErr).Code != InteractionErrorCommitRetryable || replay.Status != "" {
		t.Fatalf("replay = %#v, %v", replay, replayErr)
	}
}

func TestCommitInteractionRejectsStaleDraftRevision(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-1", ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision + 1, IdempotencyKey: "commit-stale"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("stale revision error = %v", err)
	}
}

func TestCommitInteractionRejectsSupersededReadyDraft(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "create-superseded")
	if err != nil {
		t.Fatal(err)
	}
	firstPayload := json.RawMessage(`{"plan":{"goal":"first version"}}`)
	firstDigest := sha256.Sum256(firstPayload)
	first, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-first", ModeID: AssistantModeCreation,
		Payload: firstPayload, PayloadDigest: hex.EncodeToString(firstDigest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondPayload := json.RawMessage(`{"plan":{"goal":"second version"}}`)
	second, err := svc.UpdateDraftForInteraction(context.Background(), "user-1", snapshot.ID, first.DraftID, InteractionDraftUpdateInput{
		Generation: snapshot.Generation, Revision: first.Revision, AttemptID: "update-first", Payload: secondPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{
		DraftID: first.DraftID, Revision: first.Revision, IdempotencyKey: "commit-superseded",
	})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("commit superseded draft = %v, want draft revision conflict (latest revision %d)", err, second.Revision)
	}
}

func TestCommitClaimPreventsConcurrentDifferentDrafts(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "create-claim")
	if err != nil {
		t.Fatal(err)
	}
	stageDraft := func(attemptID, modeID string) InteractionStageResult {
		t.Helper()
		payload := json.RawMessage(`{"plan":{"goal":"` + attemptID + `"}}`)
		digest := sha256.Sum256(payload)
		result, stageErr := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
			InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: attemptID, ModeID: modeID,
			Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
		})
		if stageErr != nil {
			t.Fatal(stageErr)
		}
		return result
	}
	first := stageDraft("claim-stage-one", AssistantModeCreation)
	second := stageDraft("claim-stage-two", AssistantModeProfile)
	digest := commitPayloadDigest(InteractionCommitRequest{DraftID: first.DraftID, Revision: first.Revision, IdempotencyKey: "commit-claim-one"})
	firstAttempt := &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:commit-claim-one",
		Operation: string(InteractionOperationCommit), Generation: snapshot.Generation, PayloadDigest: digest,
		ClaimToken: uuid.NewString(), CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}
	if _, created, err := svc.repo.AssistantInteractions().ClaimInteractionCommit(context.Background(), "user-1", snapshot.ID, first.DraftID, first.Revision, firstAttempt); err != nil || !created {
		t.Fatalf("first commit claim created=%v err=%v", created, err)
	}
	secondRequest := InteractionCommitRequest{DraftID: second.DraftID, Revision: second.Revision, IdempotencyKey: "commit-claim-two"}
	secondAttempt := &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:commit-claim-two",
		Operation: string(InteractionOperationCommit), Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(secondRequest),
		ClaimToken: uuid.NewString(), CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}
	if _, _, err := svc.repo.AssistantInteractions().ClaimInteractionCommit(context.Background(), "user-1", snapshot.ID, second.DraftID, second.Revision, secondAttempt); !errors.Is(err, repository.ErrInteractionCommitInProgress) {
		t.Fatalf("second commit claim error = %v, want commit in progress", err)
	}
}

func TestCommitInteractionRejectsSameIdempotencyKeyForDifferentDraft(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "commit-key-draft-conflict")
	if err != nil {
		t.Fatal(err)
	}
	stageDraft := func(attemptID, modeID string) InteractionStageResult {
		t.Helper()
		payload := json.RawMessage(`{"plan":{"goal":"` + attemptID + `"}}`)
		digest := sha256.Sum256(payload)
		result, stageErr := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
			InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: attemptID, ModeID: modeID,
			Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
		})
		if stageErr != nil {
			t.Fatal(stageErr)
		}
		return result
	}
	first := stageDraft("commit-key-first", AssistantModeCreation)
	second := stageDraft("commit-key-second", AssistantModeProfile)
	request := InteractionCommitRequest{DraftID: first.DraftID, Revision: first.Revision, IdempotencyKey: "same-commit-key"}
	if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:" + request.IdempotencyKey,
		Operation: string(InteractionOperationCommit), Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(request),
		CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{
		DraftID: second.DraftID, Revision: second.Revision, IdempotencyKey: request.IdempotencyKey,
	})
	if !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("same key with another draft error = %v, want ErrInteractiveIdempotency", err)
	}
}

func TestCommitInteractionRejectsIdempotencyKeyThatCannotFitAttemptReceipt(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: "draft-1", Revision: 1, IdempotencyKey: strings.Repeat("x", 122)})
	if !errors.Is(err, ErrInteractiveDraftInvalid) {
		t.Fatalf("long idempotency key error = %v, want ErrInteractiveDraftInvalid", err)
	}
}

func TestCommitInteractionProfileDraftSurvivesServerRestart(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "profile-restart")
	if err != nil {
		t.Fatal(err)
	}
	profile := model.NewProjectProfile()
	profile.Dimensions.Identity.Content["name"] = "重启后确认"
	payload, err := json.Marshal(map[string]any{
		"project":        map[string]string{"name": "重启后确认", "platform": "wechat"},
		"profile":        profile,
		"progress":       map[string]string{"stage": "ready"},
		"missing_fields": []string{}, "inferred_fields": []string{}, "ready": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "profile-restart-stage", ModeID: AssistantModeProfile,
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	restarted := NewAssistantInteractionService(svc.repo, nil, svc.clock)
	result, err := restarted.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "profile-restart-commit"})
	if err != nil {
		t.Fatalf("restart commit = %#v, %v", result, err)
	}
	if result.Status != InteractionCommitCreated {
		t.Fatalf("restart commit status = %q", result.Status)
	}
}

func TestCommitInteractionPersistsProfileVideoEditDefaultsFromDurableDraft(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "profile-video-defaults")
	if err != nil {
		t.Fatal(err)
	}
	profile := model.NewProjectProfile()
	profile.Dimensions.Identity.Content["name"] = "剪辑账号"
	defaults := model.VideoEditDefaults{
		Preferences:     model.VideoEditPreferences{QualityTier: "quality", SubtitleMode: "off", MaxDuration: 45},
		DeliveryTargets: []string{"portrait-short"},
	}
	payload, err := json.Marshal(map[string]any{
		"project": map[string]string{"name": "剪辑账号", "platform": model.PlatformVideoEdit},
		"profile": profile, "progress": map[string]string{"stage": "ready"},
		"missing_fields": []string{}, "inferred_fields": []string{}, "ready": true,
		"video_edit_defaults": defaults,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "profile-video-defaults-stage", ModeID: AssistantModeProfile,
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "profile-video-defaults-commit"})
	if err != nil {
		t.Fatalf("profile commit = %#v, %v", result, err)
	}
	var committed AssistantCommitResult
	if err := json.Unmarshal(result.Result, &committed); err != nil {
		t.Fatal(err)
	}
	if committed.Project == nil || !reflect.DeepEqual(committed.Project.VideoEditDefaults.Data(), defaults) {
		t.Fatalf("committed video-edit defaults = %#v, want %#v", committed.Project, defaults)
	}
}

func TestInteractionCommitOptionsApplyAllowedCreationSettings(t *testing.T) {
	base := json.RawMessage(`{"plan":{"goal":"write","content_type":"wechat-article"}}`)
	options := json.RawMessage(`{"plan":{"execution_profile":"quality","image_ratio":"1:1","image_capability_key":"standard"}}`)
	merged, err := mergeInteractionCommitOptions(AssistantModeCreation, base, options)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatal(err)
	}
	plan := payload["plan"]
	if plan["goal"] != "write" || plan["content_type"] != "wechat-article" || plan["execution_profile"] != "quality" || plan["image_ratio"] != "1:1" || plan["image_capability_key"] != "standard" {
		t.Fatalf("merged payload = %#v", payload)
	}
}

func TestInteractionCommitOptionsRejectUnknownCreationSettings(t *testing.T) {
	_, err := mergeInteractionCommitOptions(AssistantModeCreation, json.RawMessage(`{"plan":{"goal":"write"}}`), json.RawMessage(`{"plan":{"unknown":"value"}}`))
	if !errors.Is(err, ErrAssistantCheckpointInvalid) {
		t.Fatalf("unknown commit option error = %v, want checkpoint invalid", err)
	}
}

func TestCommitInteractionReplaysReceiptAfterDraftRetentionBoundary(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "receipt-replay")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "receipt-stage", ModeID: AssistantModeCreation,
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "receipt-replay-key"}
	receipt := InteractionCommitResult{Status: InteractionCommitCreated, TaskID: "task-1"}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, AttemptID: "commit:" + request.IdempotencyKey, Operation: string(InteractionOperationCommit),
		Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(request), Result: receiptJSON,
		CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.AssistantInteractions().ExpireDrafts(context.Background(), svc.clock().Add(InteractionDraftRetention+time.Hour)); err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, request)
	if err != nil {
		t.Fatalf("replay after draft expiry = %#v, %v", replayed, err)
	}
	if replayed.Status != InteractionCommitAlreadyCreated || replayed.TaskID != "task-1" {
		t.Fatalf("replayed receipt = %#v", replayed)
	}
}

func TestCommitInteractionReplaysDeterministicFailureCode(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "failure-replay")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "failure-stage", ModeID: AssistantModeCreation,
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "failure-replay-key"}
	if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, AttemptID: "commit:" + request.IdempotencyKey, Operation: string(InteractionOperationCommit),
		Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(request), ErrorCode: InteractionErrorDraftRevisionConflict,
		CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, request)
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("replayed deterministic failure = %v", err)
	}
}

func TestInteractionSnapshotSurfacesDurableCommitReconciliation(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "reconcile-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, AttemptID: "commit:reconcile-key", Operation: string(InteractionOperationCommit),
		Generation: snapshot.Generation, PayloadDigest: "digest", ErrorCode: InteractionErrorCommitReconciliationRequired,
		CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention),
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.snapshotWithDrafts(context.Background(), "user-1", &model.AssistantInteraction{ID: snapshot.ID, UserID: "user-1", ModeID: AssistantModeCreation, Lifecycle: model.AssistantInteractionActive, Generation: snapshot.Generation, ExpiresAt: svc.clock().Add(time.Hour), LastActivityAt: svc.clock()})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RecoveryErrorCode != InteractionErrorCommitReconciliationRequired || len(recovered.AvailableActions) != 2 || recovered.AvailableActions[1] != "reconcile_commit" {
		t.Fatalf("recovered snapshot = %#v, want reconciliation action", recovered)
	}
}
