package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type assistantInteractionEventFailOnceRepository struct {
	repository.Repository
	interactions *assistantInteractionEventFailOnceOperations
}

func (r *assistantInteractionEventFailOnceRepository) AssistantInteractions() repository.AssistantInteractionRepository {
	return r.interactions
}

type assistantInteractionEventFailOnceOperations struct {
	repository.AssistantInteractionRepository
	failNext bool
}

func (r *assistantInteractionEventFailOnceOperations) AppendEvent(ctx context.Context, interactionID string, generation int64, event *model.AssistantInteractionEvent) (bool, error) {
	if r.failNext {
		r.failNext = false
		return false, errors.New("event store temporarily unavailable")
	}
	return r.AssistantInteractionRepository.AppendEvent(ctx, interactionID, generation, event)
}

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
	events, err := svc.Events(context.Background(), "user-1", snapshot.ID, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("stage events = %#v, %v, want one event after replay", events, err)
	}
	var liveData map[string]any
	if err := json.Unmarshal(events[len(events)-1].Data, &liveData); err != nil {
		t.Fatal(err)
	}
	if _, ok := liveData["draft"]; !ok || !strings.Contains(string(events[len(events)-1].Data), "write") {
		t.Fatalf("live stage event = %s, want rich draft payload", events[len(events)-1].Data)
	}
	input.AttemptID = "stage-2"
	input.Payload = json.RawMessage(`{"plan":{"goal":"different"}}`)
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", input); !errors.Is(err, ErrInteractiveIdempotency) && !errors.Is(err, ErrInteractiveDraftInvalid) {
		t.Fatalf("different payload error = %v", err)
	}
	conflictingPayload := json.RawMessage(`{"plan":{"goal":"conflict"}}`)
	conflictingDigest := sha256.Sum256(conflictingPayload)
	conflict := input
	conflict.AttemptID = "stage-1"
	conflict.Payload = conflictingPayload
	conflict.PayloadDigest = hex.EncodeToString(conflictingDigest[:])
	var conflictErr *InteractionError
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", conflict); !errors.As(err, &conflictErr) || conflictErr.Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("same attempt different payload error = %v, want draft revision conflict", err)
	}
	attempt, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "stage-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(attempt.Result), "write") || strings.Contains(string(attempt.Result), "payload") {
		t.Fatalf("stage attempt receipt contains draft content: %s", attempt.Result)
	}
	metrics := svc.Metrics()
	if metrics.DraftStage["durable"] != 1 || metrics.DraftStage["replay"] != 1 {
		t.Fatalf("stage metrics = %#v, want one durable and one replay", metrics)
	}
}

func TestUpdateInteractionDraftPersistsUserSettingsAndReplays(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "draft-update")
	if err != nil {
		t.Fatal(err)
	}
	initialPayload := json.RawMessage(`{"plan":{"goal":"write"}}`)
	initialDigest := sha256.Sum256(initialPayload)
	staged, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-before-update", ModeID: AssistantModeCreation,
		Payload: initialPayload, PayloadDigest: hex.EncodeToString(initialDigest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedPayload := json.RawMessage(`{"plan":{"goal":"write","content_type":"wechat-article","execution_profile":"quality"}}`)
	update := InteractionDraftUpdateInput{Generation: snapshot.Generation, Revision: staged.Revision, AttemptID: "update-user-settings", Payload: updatedPayload}
	first, err := svc.UpdateDraftForInteraction(context.Background(), "user-1", snapshot.ID, staged.DraftID, update)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Durable || first.Revision != staged.Revision+1 || first.DraftID == staged.DraftID {
		t.Fatalf("updated draft receipt = %#v, want a new durable revision", first)
	}
	replay, err := svc.UpdateDraftForInteraction(context.Background(), "user-1", snapshot.ID, staged.DraftID, update)
	if err != nil || replay.DraftID != first.DraftID || replay.Revision != first.Revision {
		t.Fatalf("update replay = %#v, %v", replay, err)
	}
	persisted, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", first.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted.Payload) != string(updatedPayload) || !persisted.Ready {
		t.Fatalf("persisted updated draft = %#v, want exact ready payload", persisted)
	}
	oldDraft, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", staged.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	if oldDraft.ConfirmationState != InteractionDraftSuperseded {
		t.Fatalf("previous draft state = %q, want superseded", oldDraft.ConfirmationState)
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

func TestStageDraftForInteractionRejectsUnknownMode(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "create-1")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{}}`)
	digest := sha256.Sum256(payload)
	_, err = svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-unknown-mode", ModeID: "unknown",
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	})
	if !errors.Is(err, ErrInteractiveDraftInvalid) {
		t.Fatalf("unknown mode error = %v", err)
	}
}

func TestStageDraftForInteractionRejectsAttemptReuseAcrossModeOrReadyState(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "stage-identity")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
	digest := sha256.Sum256(payload)
	input := InteractionStageInput{
		InteractionID: snapshot.ID,
		Generation:    snapshot.Generation,
		AttemptID:     "same-attempt",
		ModeID:        AssistantModeCreation,
		Payload:       payload,
		PayloadDigest: hex.EncodeToString(digest[:]),
		Ready:         false,
	}
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", input); err != nil {
		t.Fatal(err)
	}

	modeReuse := input
	modeReuse.ModeID = AssistantModeReview
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", modeReuse); !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("attempt reuse across modes error = %v, want ErrInteractiveIdempotency", err)
	}

	readyReuse := input
	readyReuse.Ready = true
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", readyReuse); !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("attempt reuse across ready state error = %v, want ErrInteractiveIdempotency", err)
	}
}

func TestStageDraftAttemptIDIsScopedToInteraction(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	first, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-1"}, "create-one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-2"}, "create-two")
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []InteractionSnapshot{first, second} {
		payload := json.RawMessage(`{"plan":{"goal":"write"}}`)
		digest := sha256.Sum256(payload)
		_, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
			InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-retry-1", ModeID: AssistantModeCreation,
			Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
		})
		if err != nil {
			t.Fatalf("stage interaction %s with reused scoped attempt: %v", snapshot.ID, err)
		}
	}
}

func TestStageDraftRejectsInvalidOrOversizedPayload(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "invalid-stage-payload")
	if err != nil {
		t.Fatal(err)
	}
	oversized := json.RawMessage(`{"plan":{"goal":"` + strings.Repeat("x", assistantCheckpointMaxBytes) + `"}}`)
	for i, payload := range []json.RawMessage{json.RawMessage(`{"plan":`), oversized} {
		digest := sha256.Sum256(payload)
		_, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
			InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "invalid-payload-" + string(rune('1'+i)),
			ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: false,
		})
		if !errors.Is(err, ErrInteractiveDraftInvalid) {
			t.Fatalf("invalid payload case %d error = %v, want ErrInteractiveDraftInvalid", i, err)
		}
	}
}

func TestStageDraftRetryRepairsEventPersistenceFailure(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "stage-event-retry")
	if err != nil {
		t.Fatal(err)
	}
	base := svc.repo
	wrappedInteractions := &assistantInteractionEventFailOnceOperations{AssistantInteractionRepository: base.AssistantInteractions(), failNext: true}
	svc.repo = &assistantInteractionEventFailOnceRepository{Repository: base, interactions: wrappedInteractions}
	payload := json.RawMessage(`{"plan":{"goal":"retry event"}}`)
	digest := sha256.Sum256(payload)
	input := InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "stage-event-retry-1", ModeID: AssistantModeCreation,
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	}
	first, err := svc.StageDraftForInteraction(context.Background(), "user-1", input)
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftSavedRetryable || first.Durable {
		t.Fatalf("first stage after event failure = %#v, %v; want retryable event error", first, err)
	}
	replay, err := svc.StageDraftForInteraction(context.Background(), "user-1", input)
	if err != nil || !replay.Durable || replay.DraftID == "" {
		t.Fatalf("stage replay = %#v, %v", replay, err)
	}
	events, err := svc.Events(context.Background(), "user-1", snapshot.ID, 0)
	if err != nil || len(events) != 1 || events[0].Type != "draft.ready" {
		t.Fatalf("events after stage replay = %#v, %v; want one recovered draft event", events, err)
	}
}

func TestDraftPersistenceFailureUsesRetryableRecoveryContract(t *testing.T) {
	err := draftSavedRetryableError(errors.New("database unavailable"))
	if err.Code != InteractionErrorDraftSavedRetryable || err.Status != 503 {
		t.Fatalf("draft persistence error = %#v", err)
	}
	if strings.Join(err.AvailableActions, ",") != "retry_stage,confirm_draft" {
		t.Fatalf("draft persistence actions = %v", err.AvailableActions)
	}
	if strings.Contains(err.Message, "database") || strings.Contains(err.Error(), "database") {
		t.Fatalf("draft persistence error leaked cause: %s", err)
	}
}
