package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

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
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorCommitReconciliationRequired || result.Status != "" {
		t.Fatalf("commit = %#v, %v", result, err)
	}
	receipt, receiptErr := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "commit:commit-1")
	if receiptErr != nil {
		t.Fatal(receiptErr)
	}
	if !receipt.ExpiresAt.Equal(svc.clock().Add(InteractionCommitReceiptRetention)) {
		t.Fatalf("commit receipt expiry = %s, want %s", receipt.ExpiresAt, svc.clock().Add(InteractionCommitReceiptRetention).Format(time.RFC3339))
	}
	replay, replayErr := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "commit-1"})
	if replayErr == nil || NormalizeInteractionError(replayErr).Code != InteractionErrorCommitReconciliationRequired || replay.Status != "" {
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
