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
	if ok, err := repo.AssistantInteractions().CompareAndSwapGeneration(context.Background(), interaction.ID, 1, 2, now); err != nil || !ok {
		t.Fatalf("CAS = %v, %v", ok, err)
	}
	if ok, err := repo.AssistantInteractions().CompareAndSwapGeneration(context.Background(), interaction.ID, 1, 3, now); err != nil || ok {
		t.Fatalf("stale CAS = %v, %v", ok, err)
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

func TestAssistantInteractionRepositoryFencesRuntimeStartDuringCommit(t *testing.T) {
	repo, _ := newAssistantInteractionTestRepository(t)
	now := time.Now().UTC()
	interaction := &model.AssistantInteraction{ID: "commit-runtime-race", UserID: "user-1", ModeID: "creation", Lifecycle: model.AssistantInteractionCommitting, Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.AssistantInteractions().Create(context.Background(), interaction); err != nil {
		t.Fatal(err)
	}
	binding := &model.AssistantInteractionRuntime{ID: "commit-runtime-binding", InteractionID: interaction.ID, Generation: 1, ServerInstance: "server-1", RuntimeID: "runtime-1", LogicalSession: "logical-1", TokenHash: "hash", Status: model.AssistantInteractionRuntimeStarting, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.AssistantInteractions().CreateRuntimeBinding(context.Background(), binding); !errors.Is(err, ErrInteractionCommitInProgress) {
		t.Fatalf("create runtime binding during commit = %v, want commit-in-progress", err)
	}
	interaction.Lifecycle = model.AssistantInteractionActive
	if err := repo.AssistantInteractions().UpdateLifecycle(context.Background(), interaction.ID, model.AssistantInteractionActive, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssistantInteractions().CreateRuntimeBinding(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssistantInteractions().UpdateLifecycle(context.Background(), interaction.ID, model.AssistantInteractionCommitting, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssistantInteractions().ActivateRuntimeBinding(context.Background(), binding.ID, now); !errors.Is(err, ErrInteractionCommitInProgress) {
		t.Fatalf("activate runtime binding during commit = %v, want commit-in-progress", err)
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

func TestAssistantInteractionCommitClaimGetsNewOwnerTokenAfterLeaseExpiry(t *testing.T) {
	repo, db := newAssistantInteractionTestRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	interaction := &model.AssistantInteraction{
		ID: "claim-fencing-interaction", UserID: "user-1", ModeID: "profile", Lifecycle: model.AssistantInteractionActive,
		Generation: 1, ExpiresAt: now.Add(time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.AssistantInteractions().Create(ctx, interaction); err != nil {
		t.Fatal(err)
	}
	draft := &model.AssistantInteractionDraft{
		ID: "claim-fencing-draft", InteractionID: interaction.ID, ModeID: "profile", Revision: 1,
		Payload: json.RawMessage(`{"ready":true}`), PayloadDigest: "digest", Ready: true,
		ConfirmationState: model.AssistantInteractionDraftPending, SourceGeneration: 1,
		ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(draft).Error; err != nil {
		t.Fatal(err)
	}
	claim := func(id string, at time.Time) *model.AssistantInteractionAttempt {
		return &model.AssistantInteractionAttempt{
			ID: id, InteractionID: interaction.ID, UserID: "user-1", AttemptID: "commit:lease-recovery",
			Operation: "commit", Generation: 1, PayloadDigest: "commit-digest", Status: model.AssistantInteractionAttemptPending, ClaimToken: uuid.NewString(),
			CreatedAt: at, ExpiresAt: at.Add(30 * 24 * time.Hour),
		}
	}
	first, claimed, err := repo.AssistantInteractions().ClaimInteractionCommit(ctx, "user-1", interaction.ID, draft.ID, draft.Revision, claim("claim-owner-one", now))
	if err != nil || !claimed {
		t.Fatalf("first claim = %#v, claimed=%v, err=%v", first, claimed, err)
	}
	second, claimed, err := repo.AssistantInteractions().ClaimInteractionCommit(ctx, "user-1", interaction.ID, draft.ID, draft.Revision, claim("claim-owner-two", now.Add(model.AssistantInteractionCommitClaimLease+time.Second)))
	if err != nil || !claimed {
		t.Fatalf("reclaimed claim = %#v, claimed=%v, err=%v", second, claimed, err)
	}
	if first.ClaimToken == "" || second.ClaimToken == "" || first.ClaimToken == second.ClaimToken {
		t.Fatalf("claim owner tokens = %q then %q, want distinct non-empty tokens", first.ClaimToken, second.ClaimToken)
	}
	if err := repo.AssistantInteractions().ReleaseInteractionCommit(ctx, interaction.ID, draft.ID, "commit:lease-recovery", first.ClaimToken, "stale-release", now.Add(model.AssistantInteractionCommitClaimLease+2*time.Second)); !errors.Is(err, ErrInteractionCommitClaimLost) {
		t.Fatalf("expired owner release = %v, want claim-lost", err)
	}
	if err := repo.AssistantInteractions().CompleteInteractionCommit(ctx, interaction.ID, draft.ID, "commit:lease-recovery", first.ClaimToken, json.RawMessage(`{"stale":true}`), now.Add(model.AssistantInteractionCommitClaimLease+2*time.Second)); !errors.Is(err, ErrInteractionCommitClaimLost) {
		t.Fatalf("expired owner completion = %v, want claim-lost", err)
	}
	if err := repo.WithTx(ctx, func(tx Repository) error {
		if err := tx.AssistantInteractions().UpdateProject(ctx, interaction.ID, "stale-project", now.Add(model.AssistantInteractionCommitClaimLease+2*time.Second)); err != nil {
			return err
		}
		return tx.AssistantInteractions().CompleteInteractionCommitInTransaction(ctx, interaction.ID, draft.ID, "commit:lease-recovery", first.ClaimToken, json.RawMessage(`{"stale":true}`), now.Add(model.AssistantInteractionCommitClaimLease+2*time.Second))
	}); !errors.Is(err, ErrInteractionCommitClaimLost) {
		t.Fatalf("stale profile transaction completion = %v, want claim-lost", err)
	}
	current, err := repo.AssistantInteractions().FindAttempt(ctx, interaction.ID, "commit:lease-recovery")
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.AssistantInteractionAttemptPending || current.ClaimToken != second.ClaimToken || len(current.Result) != 0 {
		t.Fatalf("current claim after stale owner writes = %#v", current)
	}
	currentInteraction, err := repo.AssistantInteractions().FindByID(ctx, interaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentInteraction.ProjectID != "" {
		t.Fatalf("stale profile transaction left project %q committed", currentInteraction.ProjectID)
	}
	if err := repo.AssistantInteractions().CompleteInteractionCommit(ctx, interaction.ID, draft.ID, "commit:lease-recovery", second.ClaimToken, json.RawMessage(`{"created":true}`), now.Add(model.AssistantInteractionCommitClaimLease+3*time.Second)); err != nil {
		t.Fatalf("current owner completion = %v", err)
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
		accepted, err := repo.AssistantInteractions().AppendEvent(context.Background(), interaction.ID, 1, &model.AssistantInteractionEvent{ID: uuid.NewString(), Type: "runtime.ready", Data: json.RawMessage(`{}`), CreatedAt: now})
		if err != nil || !accepted {
			t.Fatalf("append event %d = accepted=%v, err=%v", sequence, accepted, err)
		}
	}
	if ok, err := repo.AssistantInteractions().CompareAndSwapGeneration(context.Background(), interaction.ID, 1, 2, now); err != nil || !ok {
		t.Fatalf("generation CAS = %v, %v", ok, err)
	}
	accepted, err := repo.AssistantInteractions().AppendEvent(context.Background(), interaction.ID, 1, &model.AssistantInteractionEvent{ID: uuid.NewString(), Type: "late", Data: json.RawMessage(`{}`), CreatedAt: now})
	if err != nil || accepted {
		t.Fatalf("stale append = accepted=%v, err=%v", accepted, err)
	}
	accepted, err = repo.AssistantInteractions().AppendEvent(context.Background(), interaction.ID, 2, &model.AssistantInteractionEvent{ID: uuid.NewString(), Type: "runtime.replaced", Data: json.RawMessage(`{}`), CreatedAt: now})
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
