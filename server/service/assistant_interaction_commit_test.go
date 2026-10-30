package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestCommitInteractionUsesDurableDraftAfterRuntimeLoss(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	payload := json.RawMessage(completeInteractionPlan)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-1", ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	// No runtime or session is needed to reach the durable commit boundary.
	restarted := NewAssistantInteractionService(svc.repo, nil, svc.clock)
	restarted.SetTaskService(svc.taskSvc)
	result, err := restarted.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "commit-1"})
	if err != nil || result.Status != InteractionCommitCreated {
		t.Fatalf("commit = %#v, %v", result, err)
	}
	receipt, receiptErr := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "commit:commit-1")
	if receiptErr != nil {
		t.Fatal(receiptErr)
	}
	if receipt.ExpiresAt.Sub(receipt.CreatedAt) != InteractionCommitReceiptRetention {
		t.Fatalf("commit receipt expiry = %s, want %s", receipt.ExpiresAt, svc.clock().Add(InteractionCommitReceiptRetention).Format(time.RFC3339))
	}
	replay, replayErr := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "commit-1"})
	if replayErr != nil || replay.Status != InteractionCommitAlreadyCreated || replay.TaskID != result.TaskID {
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
