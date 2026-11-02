package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestAssistantInteractionRecoversExpiredRuntimeLease(t *testing.T) {
	for _, tc := range []struct {
		name          string
		age           time.Duration
		entry         string
		liveRuntime   bool
		serverRestart bool
	}{
		{name: "unexpired", age: 23 * time.Hour},
		{name: "unexpired_server_restart", age: 23 * time.Hour, liveRuntime: true, serverRestart: true},
		{name: "expired_without_binding", age: 25 * time.Hour},
		{name: "expired_create_replay", age: 25 * time.Hour, entry: "replay"},
		{name: "expired_explicit_resume", age: 25 * time.Hour, entry: "resume"},
		{name: "expired_snapshot", age: 25 * time.Hour, entry: "snapshot"},
		{name: "expired_live_runtime", age: 25 * time.Hour, liveRuntime: true},
		{name: "expiry_boundary", age: 24 * time.Hour, liveRuntime: true},
		{name: "expired_server_restart", age: 25 * time.Hour, liveRuntime: true, serverRestart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, _ := newAssistantInteractionServiceTest(t)
			createdAt := svc.clock()
			factory := &assistantInteractionModeFactory{}
			factory.capabilityCheck = func(token string) error {
				_, _, _, _, _, _, err := svc.ResolveRuntimeCapability(token)
				return err
			}
			svc.factory = nil
			if tc.liveRuntime {
				svc.factory = factory
			}
			input := AssistantInteractionInput{ModeID: AssistantModeCreation}
			original, err := svc.CreateOrResume(ctx, "user-1", input, "original")
			if err != nil {
				t.Fatal(err)
			}
			payload := json.RawMessage(`{"plan":{"topic":"保留草稿"}}`)
			digest := sha256.Sum256(payload)
			stage, err := svc.StageDraftForInteraction(ctx, "user-1", InteractionStageInput{
				InteractionID: original.ID, Generation: original.Generation, ModeID: AssistantModeCreation,
				AttemptID: "draft", Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]),
			})
			if err != nil {
				t.Fatal(err)
			}
			svc.clock = func() time.Time { return createdAt.Add(tc.age) }
			if tc.liveRuntime && tc.age >= 24*time.Hour {
				if _, _, _, _, _, _, err := svc.ResolveRuntimeCapability(factory.specs[0].RuntimeToken); !errors.Is(err, ErrInteractiveToken) {
					t.Fatalf("expired credential accepted before recovery: %v", err)
				}
			}
			if tc.serverRestart {
				svc = NewAssistantInteractionService(svc.repo, factory, svc.clock)
			}
			svc.factory = factory
			var recovered InteractionSnapshot
			switch tc.entry {
			case "snapshot":
				recovered, err = svc.Snapshot(ctx, "user-1", original.ID)
			case "replay":
				recovered, err = svc.CreateOrResume(ctx, "user-1", input, "original")
			default:
				if tc.entry == "resume" {
					input.ResumeInteractionID = original.ID
				}
				recovered, err = svc.CreateOrResume(ctx, "user-1", input, "retry")
			}
			if err != nil {
				t.Fatalf("recover interaction and authenticate MCP during handshake: %v", err)
			}
			wantGeneration := original.Generation
			wantExpiry := createdAt.Add(24 * time.Hour)
			if tc.age >= 24*time.Hour || tc.serverRestart {
				wantGeneration++
				wantExpiry = svc.clock().Add(24 * time.Hour)
			}
			if recovered.ID != original.ID || recovered.Generation != wantGeneration || recovered.ExpiresAt != wantExpiry.Format(time.RFC3339Nano) {
				t.Fatalf("recovered = %#v, want original interaction, generation %d, expiry %s", recovered, wantGeneration, wantExpiry)
			}
			if len(recovered.Drafts) != 1 || recovered.Drafts[0].ID != stage.DraftID || string(recovered.Drafts[0].Payload) != string(payload) {
				t.Fatalf("draft was not preserved: %#v", recovered.Drafts)
			}
			if _, err := svc.repo.AssistantInteractions().FindAttempt(ctx, original.ID, "draft"); err != nil {
				t.Fatalf("draft receipt was not preserved: %v", err)
			}
			latest := factory.specs[len(factory.specs)-1]
			_, id, _, _, generation, expiresAt, err := svc.ResolveRuntimeCapability(latest.RuntimeToken)
			if err != nil || id != original.ID || generation != wantGeneration || !expiresAt.Equal(wantExpiry) {
				t.Fatalf("new runtime credential: id=%s generation=%d expiry=%s err=%v", id, generation, expiresAt, err)
			}
			if tc.liveRuntime {
				if len(factory.specs) != 2 {
					t.Fatalf("runtime starts = %d, want replacement", len(factory.specs))
				}
				if _, _, _, _, _, _, err := svc.ResolveRuntimeCapability(factory.specs[0].RuntimeToken); !errors.Is(err, ErrInteractiveToken) {
					t.Fatalf("old credential accepted: %v", err)
				}
			}
			if wantGeneration != original.Generation && svc.AcceptRuntimeEvent(original.ID, original.Generation, "assistant.message", map[string]any{"text": "late"}) {
				t.Fatal("late event from expired runtime was accepted")
			}
			starts := factory.starts
			if _, err := svc.Snapshot(ctx, "user-1", original.ID); err != nil || factory.starts != starts {
				t.Fatalf("repeated snapshot must reuse recovered runtime: starts=%d err=%v", factory.starts, err)
			}
		})
	}
}

func TestAssistantInteractionExpiredCommandCanRetryAfterGenerationReload(t *testing.T) {
	ctx := context.Background()
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc.factory = factory
	original, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "create")
	if err != nil {
		t.Fatal(err)
	}
	createdAt := svc.clock()
	svc.clock = func() time.Time { return createdAt.Add(25 * time.Hour) }
	input := InteractionMessageInput{Generation: original.Generation, ClientMessageID: "message", Text: "继续"}
	err = svc.SendMessage(ctx, "user-1", original.ID, input)
	var changed *InteractionError
	if !errors.As(err, &changed) || changed.Code != InteractionErrorGenerationChanged || changed.Snapshot == nil {
		t.Fatalf("expired command must return a new generation snapshot: %v", err)
	}
	if changed.Snapshot.Generation != original.Generation+1 {
		t.Fatalf("recovered generation = %d", changed.Snapshot.Generation)
	}
	for _, command := range factory.commandSnapshot() {
		if command.method == "session/prompt" {
			t.Fatal("command was delivered before the client reloaded its generation")
		}
	}
	input.Generation = changed.Snapshot.Generation
	if err := svc.SendMessage(ctx, "user-1", original.ID, input); err != nil {
		t.Fatalf("retry an unaccepted message after generation reload: %v", err)
	}
	prompts := 0
	for _, command := range factory.commandSnapshot() {
		if command.method == "session/prompt" {
			prompts++
		}
	}
	if prompts != 1 {
		t.Fatalf("prompt deliveries = %d, want exactly one", prompts)
	}
}
