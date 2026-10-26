package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
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
