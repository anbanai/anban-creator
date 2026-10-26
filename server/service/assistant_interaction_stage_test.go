package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func TestStageDraftForInteractionPersistsAndReplaysByAttempt(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
	digest := sha256.Sum256(payload)
	input := InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-1", ModeID: "creation", Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true}
	first, err := svc.StageDraftForInteraction(context.Background(), "user-1", input)
	if err != nil || !first.Durable || first.DraftID == "" || first.Revision != 1 {
		t.Fatalf("first stage = %#v, %v", first, err)
	}
	replay, err := svc.StageDraftForInteraction(context.Background(), "user-1", input)
	if err != nil || replay.DraftID != first.DraftID || replay.Revision != first.Revision {
		t.Fatalf("replay = %#v, %v", replay, err)
	}
	input.AttemptID = "stage-2"
	input.Payload = json.RawMessage(`{"plan":{"goal":"different"}}`)
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", input); !errors.Is(err, ErrInteractiveIdempotency) && !errors.Is(err, ErrInteractiveDraftInvalid) {
		t.Fatalf("different payload error = %v", err)
	}
}

func TestStageDraftForInteractionRejectsStaleGeneration(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation, "runtime_closed"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-stale", ModeID: "creation", Payload: json.RawMessage(`{"plan":{}}`), PayloadDigest: "digest", Ready: true})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
		t.Fatalf("stale stage error = %v", err)
	}
}
