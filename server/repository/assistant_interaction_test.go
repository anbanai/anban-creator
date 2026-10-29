package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAssistantInteractionTestRepository(t *testing.T) (Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return New(db), db
}

func TestAssistantInteractionRepositoryCASAndRuntime(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	interaction := &model.AssistantInteraction{ID: "interaction-1", UserID: "user-1", ModeID: "creation", ProjectID: "project-1", Lifecycle: model.AssistantInteractionActive, Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.AssistantInteractions().Create(context.Background(), interaction); err != nil {
		t.Fatal(err)
	}
	expiresAt := now.Add(24 * time.Hour)
	if ok, err := repo.AssistantInteractions().CompareAndSwapGeneration(context.Background(), interaction.ID, 1, 2, now, expiresAt); err != nil || !ok {
		t.Fatalf("CAS = %v, %v", ok, err)
	}
	if ok, err := repo.AssistantInteractions().CompareAndSwapGeneration(context.Background(), interaction.ID, 1, 3, now, expiresAt.Add(time.Hour)); err != nil || ok {
		t.Fatalf("stale CAS = %v, %v", ok, err)
	}
	renewed, err := repo.AssistantInteractions().FindByID(context.Background(), interaction.ID)
	if err != nil || renewed.Generation != 2 || !renewed.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("generation and expiry must renew atomically: %#v, %v", renewed, err)
	}
	runtime := &model.AssistantInteractionRuntime{ID: "runtime-1", InteractionID: interaction.ID, Generation: 2, ServerInstance: "server-1", RuntimeID: "runtime-1", LogicalSession: "logical-1", TokenHash: "hash", Status: model.AssistantInteractionRuntimeRunning, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.AssistantInteractions().CreateRuntimeBinding(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	got, err := repo.AssistantInteractions().FindCurrentRuntime(context.Background(), interaction.ID, 2)
	if err != nil || got.ID != runtime.ID {
		t.Fatalf("runtime = %#v, %v", got, err)
	}
}

func TestAssistantInteractionRepositoryAllowsOnlyOneActiveScope(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	first := &model.AssistantInteraction{ID: "scope-1", UserID: "user-1", ModeID: "creation", ProjectID: "project-1", Lifecycle: model.AssistantInteractionActive, Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.AssistantInteractions().Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := *first
	second.ID = "scope-2"
	if err := repo.AssistantInteractions().Create(context.Background(), &second); err == nil {
		t.Fatal("duplicate active scope was accepted")
	}
	if err := repo.AssistantInteractions().UpdateLifecycle(context.Background(), first.ID, model.AssistantInteractionAbandoned, now); err != nil {
		t.Fatal(err)
	}
	second.ID = "scope-2"
	if err := repo.AssistantInteractions().Create(context.Background(), &second); err != nil {
		t.Fatalf("new scope after abandonment: %v", err)
	}
}

func TestAssistantInteractionRepositoryFindOrCreateActiveReturnsExistingScope(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	first := &model.AssistantInteraction{ID: "scope-first", UserID: "user-1", ModeID: "profile", ProjectID: "project-1", Lifecycle: model.AssistantInteractionActive, Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
	got, created, err := repo.AssistantInteractions().FindOrCreateActive(context.Background(), first)
	if err != nil || !created || got.ID != first.ID {
		t.Fatalf("first FindOrCreateActive = %#v, created=%v, err=%v", got, created, err)
	}
	second := *first
	second.ID = "scope-second"
	second.ModeID = "creation"
	got, created, err = repo.AssistantInteractions().FindOrCreateActive(context.Background(), &second)
	if err != nil || created || got.ID != first.ID {
		t.Fatalf("duplicate FindOrCreateActive = %#v, created=%v, err=%v", got, created, err)
	}
}

func TestAssistantInteractionRepositoryAppendEventFencesGenerationAndAllocatesSequence(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	interaction := &model.AssistantInteraction{ID: "event-interaction", UserID: "user-1", ModeID: "creation", Lifecycle: model.AssistantInteractionActive, Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.AssistantInteractions().Create(context.Background(), interaction); err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= 2; sequence++ {
		accepted, err := repo.AssistantInteractions().AppendEvent(context.Background(), interaction.ID, 1, interaction.ProjectID, &model.AssistantInteractionEvent{ID: uuid.NewString(), Type: "runtime.ready", Data: json.RawMessage(`{}`), CreatedAt: now})
		if err != nil || !accepted {
			t.Fatalf("append event %d = accepted=%v, err=%v", sequence, accepted, err)
		}
	}
	if ok, err := repo.AssistantInteractions().CompareAndSwapGeneration(context.Background(), interaction.ID, 1, 2, now, now.Add(time.Hour)); err != nil || !ok {
		t.Fatalf("generation CAS = %v, %v", ok, err)
	}
	accepted, err := repo.AssistantInteractions().AppendEvent(context.Background(), interaction.ID, 1, interaction.ProjectID, &model.AssistantInteractionEvent{ID: uuid.NewString(), Type: "late", Data: json.RawMessage(`{}`), CreatedAt: now})
	if err != nil || accepted {
		t.Fatalf("stale append = accepted=%v, err=%v", accepted, err)
	}
	accepted, err = repo.AssistantInteractions().AppendEvent(context.Background(), interaction.ID, 2, interaction.ProjectID, &model.AssistantInteractionEvent{ID: uuid.NewString(), Type: "runtime.replaced", Data: json.RawMessage(`{}`), CreatedAt: now})
	if err != nil || !accepted {
		t.Fatalf("current append = accepted=%v, err=%v", accepted, err)
	}
	oldEvents, err := repo.AssistantInteractions().ListEventsAfter(context.Background(), interaction.ID, 1, 0, 10)
	if err != nil || len(oldEvents) != 2 || oldEvents[0].Generation != 1 || oldEvents[1].Generation != 1 {
		t.Fatalf("generation 1 events = %#v, err=%v", oldEvents, err)
	}
	currentEvents, err := repo.AssistantInteractions().ListEventsAfter(context.Background(), interaction.ID, 2, 0, 10)
	if err != nil || len(currentEvents) != 1 || currentEvents[0].Generation != 2 || currentEvents[0].Type != "runtime.replaced" {
		t.Fatalf("generation 2 events = %#v, err=%v", currentEvents, err)
	}
}

func TestAssistantInteractionRepositoryMarkNeedsConfirmationDoesNotReopenTerminal(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	interaction := &model.AssistantInteraction{ID: "terminal-interaction", UserID: "user-1", ModeID: "creation", Lifecycle: model.AssistantInteractionCompleted, Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.AssistantInteractions().Create(context.Background(), interaction); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssistantInteractions().MarkNeedsConfirmation(context.Background(), interaction.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := repo.AssistantInteractions().FindByID(context.Background(), interaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != model.AssistantInteractionCompleted {
		t.Fatalf("terminal lifecycle = %q, want completed", got.Lifecycle)
	}
}

func TestAssistantInteractionRepositoryAllowsMultipleClosedScopes(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	for _, id := range []string{"closed-1", "closed-2"} {
		if err := repo.AssistantInteractions().Create(context.Background(), &model.AssistantInteraction{ID: id, UserID: "user-1", ModeID: "creation", ProjectID: "project-1", Lifecycle: model.AssistantInteractionCompleted, Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create closed interaction %s: %v", id, err)
		}
	}
}

func TestAssistantInteractionRepositoryDraftAttemptIsIdempotent(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	draft := &model.AssistantInteractionDraft{ID: "draft-1", InteractionID: "interaction-1", ModeID: "creation", Revision: 1, Payload: json.RawMessage(`{"plan":{"goal":"write"}}`), PayloadDigest: "digest-1", Ready: true, ConfirmationState: model.AssistantInteractionDraftPending, SourceGeneration: 1, ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now}
	attempt := &model.AssistantInteractionAttempt{ID: "attempt-row-1", InteractionID: draft.InteractionID, AttemptID: "attempt-1", Operation: "stage", Generation: 1, PayloadDigest: draft.PayloadDigest, Result: json.RawMessage(`{"draft_id":"draft-1"}`), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	first, created, err := repo.AssistantInteractions().CreateDraftIfAttemptNew(context.Background(), draft, attempt)
	if err != nil || !created || first.ID != draft.ID {
		t.Fatalf("first stage = %#v, %v, %v", first, created, err)
	}
	replay, created, err := repo.AssistantInteractions().CreateDraftIfAttemptNew(context.Background(), draft, attempt)
	if err != nil || created || replay.ID != draft.ID {
		t.Fatalf("replay = %#v, %v, %v", replay, created, err)
	}
	conflict := *attempt
	conflict.ID = "attempt-row-2"
	conflict.PayloadDigest = "different"
	if _, _, err := repo.AssistantInteractions().CreateDraftIfAttemptNew(context.Background(), draft, &conflict); !errors.Is(err, ErrInteractionAttemptConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestAssistantInteractionRepositoryIgnoresSupersededUnknownCommand(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	base := time.Now().UTC()
	if err := repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: "attempt-unknown", InteractionID: "interaction-1", AttemptID: "message-unknown", Operation: "message",
		Generation: 1, PayloadDigest: "digest-unknown", Status: model.AssistantInteractionAttemptPending,
		ErrorCode: "assistant_command_outcome_unknown", CreatedAt: base, ExpiresAt: base.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: "attempt-retry", InteractionID: "interaction-1", AttemptID: "message-retry", Operation: "message",
		Generation: 2, PayloadDigest: "digest-retry", Status: model.AssistantInteractionAttemptCompleted,
		Result: json.RawMessage(`{"accepted":true}`), CreatedAt: base.Add(time.Second), ExpiresAt: base.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AssistantInteractions().FindLatestCommandOutcomeUnknown(context.Background(), "interaction-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("superseded unknown command = %v, want record not found", err)
	}
}

func TestAssistantInteractionRepositoryRetention(t *testing.T) {
	repo, db := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	if err := repo.AssistantInteractions().CreateEvent(context.Background(), &model.AssistantInteractionEvent{ID: "event-old", InteractionID: "interaction-1", Sequence: 1, Type: "message", Data: json.RawMessage(`{}`), CreatedAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{ID: "attempt-old", InteractionID: "interaction-1", AttemptID: "old", Operation: "message", PayloadDigest: "digest", CreatedAt: old, ExpiresAt: old}); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.AssistantInteractions().ExpireEvents(context.Background(), now.Add(-24*time.Hour)); err != nil || count != 1 {
		t.Fatalf("events expired = %d, %v", count, err)
	}
	if count, err := repo.AssistantInteractions().ExpireAttempts(context.Background(), now.Add(-24*time.Hour)); err != nil || count != 1 {
		t.Fatalf("attempts expired = %d, %v", count, err)
	}
	if err := db.Create(&model.AssistantInteractionDraft{ID: "draft-old", InteractionID: "interaction-1", ModeID: "creation", Revision: 1, Payload: json.RawMessage(`{}`), PayloadDigest: "digest", ConfirmationState: model.AssistantInteractionDraftPending, ExpiresAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := repo.AssistantInteractions().ExpireDrafts(context.Background(), now); err != nil || count != 1 {
		t.Fatalf("drafts expired = %d, %v", count, err)
	}
}
