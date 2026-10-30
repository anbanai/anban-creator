package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

func TestCreationInvalidFinalImageRatioAllowsCorrection(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "invalid-ratio", Options: json.RawMessage(`{"plan":{"image_ratio":"invalid"}}`)}
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	structured := NormalizeInteractionError(err)
	if structured == nil || structured.Code != InteractionErrorDraftInvalid || structured.Status != 422 || structured.Snapshot == nil || len(structured.BlockingIssues) == 0 || structured.BlockingIssues[0].Field != "plan.image_ratio" {
		t.Fatalf("invalid ratio = %#v, %v", structured, err)
	}
	if len(structured.AvailableActions) != 1 || structured.AvailableActions[0] != "continue_editing" {
		t.Fatalf("invalid ratio actions = %v", structured.AvailableActions)
	}
	if _, err := svc.repo.Tasks().FindByAssistantCheckpointID(context.Background(), "user-1", stage.DraftID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("invalid ratio task = %v", err)
	}
	if _, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "commit:"+req.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("invalid ratio receipt = %v", err)
	}
	draft, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", stage.DraftID)
	if err != nil || draft.ConfirmationState != model.AssistantInteractionDraftPending {
		t.Fatalf("invalid ratio draft = %#v, %v", draft, err)
	}
	if !structured.Snapshot.Drafts[0].Ready || len(structured.Snapshot.Drafts[0].BlockingIssues) != 0 || string(structured.Snapshot.Drafts[0].Payload) != completeInteractionPlan {
		t.Fatalf("option error changed immutable draft = %#v", structured.Snapshot.Drafts[0])
	}
	corrected := req
	corrected.IdempotencyKey = "corrected-ratio"
	corrected.Options = json.RawMessage(`{"plan":{"image_ratio":"1:1"}}`)
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, corrected)
	if err != nil || result.TaskID == "" {
		t.Fatalf("corrected same-draft commit = %#v, %v", result, err)
	}
	task, err := svc.repo.Tasks().FindByAssistantCheckpointID(context.Background(), "user-1", stage.DraftID)
	if err != nil || task.ID != result.TaskID || task.ImageRatio != "1:1" {
		t.Fatalf("corrected options = %#v, %v", task, err)
	}
	replay, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, corrected)
	if err != nil || replay.TaskID != result.TaskID || replay.Status != InteractionCommitAlreadyCreated {
		t.Fatalf("corrected replay = %#v, %v", replay, err)
	}
	corrected.Options = json.RawMessage(`{"plan":{"image_ratio":"16:9"}}`)
	if _, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, corrected); err == nil {
		t.Fatal("reused key accepted different final options")
	}

}

func TestCreationUnavailableImageCapabilityPreservesBusinessError(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	resolver := svc.taskSvc.imageCapabilities
	svc.taskSvc.SetImageCapabilityResolver(nil)
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "capability"}
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	if !errors.Is(err, ErrImageCapabilityResolverUnavailable) {
		t.Fatalf("unavailable image capability = %v", err)
	}
	var structured *InteractionError
	if errors.As(err, &structured) {
		t.Fatalf("business error replaced by interaction outcome: %#v", structured)
	}
	draft, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", stage.DraftID)
	if err != nil || draft.ConfirmationState != model.AssistantInteractionDraftPending {
		t.Fatalf("unavailable capability claim = %#v, %v", draft, err)
	}
	svc.taskSvc.SetImageCapabilityResolver(resolver)
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	if err != nil || result.TaskID == "" {
		t.Fatalf("restored capability commit = %#v, %v", result, err)
	}
}

type failingConfirmationAdmissionRepository struct{ repository.Repository }

func (r failingConfirmationAdmissionRepository) WithTx(context.Context, func(repository.Repository) error) error {
	return ErrAgentProfileAccessDenied
}

func TestCreationErrorAfterEnteringPersistenceRetainsClaim(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	svc.taskSvc.repo = failingConfirmationAdmissionRepository{svc.taskSvc.repo}
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "admission-error"}
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	structured := NormalizeInteractionError(err)
	if structured == nil || structured.Code != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("persistence outcome = %#v, %v", structured, err)
	}
	draft, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", stage.DraftID)
	if err != nil || draft.ConfirmationState != model.AssistantInteractionDraftConfirmed {
		t.Fatalf("persistence claim = %#v, %v", draft, err)
	}
	recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil || recovered.StateSequence <= stage.Draft.StateSequence || recovered.Drafts[0].Ready || recovered.RecoveryErrorCode != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("uncertain ordered snapshot = %#v, %v", recovered, err)
	}
	if _, err := svc.repo.AssistantInteractions().AppendEvent(context.Background(), snapshot.ID, snapshot.Generation, &model.AssistantInteractionEvent{ID: "uncertain-progress", Type: "progress", Data: json.RawMessage(`{}`), CreatedAt: svc.clock()}); err != nil {
		t.Fatal(err)
	}
	newer, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil || newer.StateSequence <= recovered.StateSequence || newer.Drafts[0].Ready || newer.RecoveryErrorCode != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("newer uncertain snapshot = %#v, %v", newer, err)
	}
	if _, err := svc.ContinueInteraction(context.Background(), "user-1", snapshot.ID, InteractionContinueRequest{DraftID: stage.DraftID, Revision: stage.Revision}); err == nil {
		t.Fatal("uncertain persistence claim was withdrawn")
	}
	if _, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req); NormalizeInteractionError(err).Code != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("persistence replay = %v", err)
	}
}

// A second Server/window can observe the provisional claim before preflight fails.
func TestCreationObservedClaimReleaseHasAuthoritativeOrdering(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	sequence := func(value any) uint64 {
		t.Helper()
		data, _ := json.Marshal(value)
		var fields struct {
			StateSequence uint64 `json:"state_sequence"`
		}
		_ = json.Unmarshal(data, &fields)
		return fields.StateSequence
	}
	if sequence(stage.Draft) == 0 {
		t.Fatal("staged creation draft has no ordering metadata")
	}
	blocked := &blockingConfirmationTasks{TaskRepository: svc.repo.Tasks(), reached: make(chan struct{}), release: make(chan struct{})}
	first := NewAssistantInteractionService(blockingConfirmationRepository{Repository: svc.repo, tasks: blocked}, nil, svc.clock)
	first.SetTaskService(svc.taskSvc)
	other := NewAssistantInteractionService(svc.repo, nil, svc.clock)
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "invalid-observed", Options: json.RawMessage(`{"plan":{"image_ratio":"invalid"}}`)}
	done := make(chan error, 1)
	go func() {
		_, err := first.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
		done <- err
	}()
	select {
	case <-blocked.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("commit never reached task boundary")
	}
	defer close(blocked.release)
	claimed, err := other.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil || claimed.RecoveryErrorCode != InteractionErrorCommitReconciliationRequired || sequence(claimed) <= sequence(stage.Draft) {
		t.Fatalf("observed claim = %#v, %v", claimed, err)
	}
	blocked.release <- struct{}{}
	structured := NormalizeInteractionError(<-done)
	if structured == nil || structured.Code != InteractionErrorDraftInvalid || structured.Snapshot == nil {
		t.Fatalf("released failure = %#v", structured)
	}
	released, err := other.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil || released.RecoveryErrorCode != "" || !released.Drafts[0].Ready || released.Drafts[0].ConfirmationState != model.AssistantInteractionDraftPending || sequence(released) <= sequence(claimed) || sequence(released.Drafts[0]) != sequence(released) {
		t.Fatalf("released snapshot = %#v, %v", released, err)
	}
	if sequence(structured.Snapshot) != sequence(released) {
		t.Fatal("error snapshot does not expose released state")
	}
	events, err := other.Events(context.Background(), "user-1", snapshot.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	releasedEvents, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), snapshot.ID, snapshot.Generation, sequence(stage.Draft), 100)
	if err != nil || len(releasedEvents) != 2 {
		t.Fatalf("claim/release cursor events = %d, %v", len(releasedEvents), err)
	}
	for i, event := range releasedEvents {
		if event.Sequence != sequence(stage.Draft)+uint64(i)+1 {
			t.Fatalf("state transition event gap: %#v", releasedEvents)
		}
	}
	for _, event := range events {
		if event.Type == "draft.ready" {
			var data struct {
				Draft InteractionDraftView `json:"draft"`
			}
			_ = json.Unmarshal(event.Data, &data)
			if !data.Draft.Ready || sequence(data.Draft) != sequence(released) {
				t.Fatalf("replayed release = %s", event.Data)
			}
		}
	}
	later := stageReadinessPlan(t, svc, snapshot, "later", completeInteractionPlan)
	if later.Revision <= stage.Revision || sequence(later.Draft) <= sequence(released) {
		t.Fatalf("later stage = %#v", later)
	}
	withdrawn, err := svc.ContinueInteraction(context.Background(), "user-1", snapshot.ID, InteractionContinueRequest{DraftID: later.DraftID, Revision: later.Revision})
	if err != nil || withdrawn.StateSequence <= sequence(later.Draft) {
		t.Fatalf("withdraw ordering = %#v, %v", withdrawn, err)
	}
	final := stageReadinessPlan(t, svc, snapshot, "final", completeInteractionPlan)
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: final.DraftID, Revision: final.Revision, IdempotencyKey: "final"})
	if err != nil || result.TaskID == "" {
		t.Fatalf("final commit = %#v, %v", result, err)
	}
	completed, err := other.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil || completed.Lifecycle != model.AssistantInteractionCompleted || completed.StateSequence <= final.Draft.StateSequence {
		t.Fatalf("completion ordering = %#v, %v", completed, err)
	}
	events, err = other.Events(context.Background(), "user-1", snapshot.ID, sequence(stage.Draft))
	if err != nil {
		t.Fatal(err)
	}
	seenCompletion := false
	for i, event := range events {
		if event.Sequence != sequence(stage.Draft)+uint64(i)+1 {
			t.Fatalf("claim/withdraw/release/finish cursor gap at %d: %#v", i, event)
		}
		if event.Type == "draft.ready" {
			var data struct {
				Draft InteractionDraftView `json:"draft"`
			}
			_ = json.Unmarshal(event.Data, &data)
			if data.Draft.Ready || data.Draft.StateSequence != completed.StateSequence {
				t.Fatalf("stale completion replay = %s", event.Data)
			}
			seenCompletion = seenCompletion || data.Draft.ConfirmationState == model.AssistantInteractionDraftConsumed
		}
	}
	if !seenCompletion {
		t.Fatal("finish has no consumed event")
	}
}

type releaseBeforeEventsRepository struct {
	repository.Repository
	interactions repository.AssistantInteractionRepository
}

func (r releaseBeforeEventsRepository) AssistantInteractions() repository.AssistantInteractionRepository {
	return r.interactions
}

type releaseBeforeEvents struct {
	repository.AssistantInteractionRepository
	release func()
}

func (r releaseBeforeEvents) ListEventsAfter(ctx context.Context, interactionID string, generation int64, after uint64, limit int) ([]*model.AssistantInteractionEvent, error) {
	r.release()
	return r.AssistantInteractionRepository.ListEventsAfter(ctx, interactionID, generation, after, limit)
}
func TestCreationReleaseDuringEventReadUsesReleasedSnapshot(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	repo := svc.repo.AssistantInteractions()
	attempt := &model.AssistantInteractionAttempt{ID: "event-read-claim", InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:event-read", Operation: "commit", Status: model.AssistantInteractionAttemptPending, Generation: snapshot.Generation, CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(time.Hour)}
	if err := repo.TransitionCreationDraft(context.Background(), "user-1", snapshot.ID, stage.DraftID, stage.Revision, attempt, svc.clock()); err != nil {
		t.Fatal(err)
	}
	svc.repo = releaseBeforeEventsRepository{Repository: svc.repo, interactions: releaseBeforeEvents{AssistantInteractionRepository: repo, release: func() {
		if err := repo.FinishCreationCommit(context.Background(), snapshot.ID, stage.DraftID, attempt.AttemptID, nil, InteractionErrorDraftInvalid, true, svc.clock()); err != nil {
			t.Fatal(err)
		}
	}}}
	events, err := svc.Events(context.Background(), "user-1", snapshot.ID, stage.Draft.StateSequence)
	if err != nil || len(events) != 2 {
		t.Fatalf("release events = %#v, %v", events, err)
	}
	last := events[len(events)-1]
	var data struct {
		Draft InteractionDraftView `json:"draft"`
	}
	_ = json.Unmarshal(last.Data, &data)
	if !data.Draft.Ready || data.Draft.ConfirmationState != model.AssistantInteractionDraftPending || data.Draft.StateSequence < last.Sequence {
		t.Fatalf("release event consumed with obsolete claim view = %s", last.Data)
	}
}
