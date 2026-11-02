package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const completeInteractionPlan = `{"plan":{"goal":"write","content_type":"wechat-article","topic":"topic","angle":"angle","expression_form":"article","profile_context":[],"temporary_overrides":["User approved placeholders; add contact later"],"constraints":["Do not invent contact details"]}}`

func stageReadinessPlan(t *testing.T, svc *AssistantInteractionService, snapshot InteractionSnapshot, attempt, payload string) InteractionStageResult {
	t.Helper()
	raw := json.RawMessage(payload)
	digest := sha256.Sum256(raw)
	result, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: attempt, ModeID: AssistantModeCreation, Payload: raw, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCreationReadinessConvergesAcrossStageSnapshotEventAndCommit(t *testing.T) {
	for _, tc := range []struct{ name, payload, code string }{
		{"missing", strings.Replace(completeInteractionPlan, `"constraints":`, `"missing_inputs":["contact"],"constraints":`, 1), "assistant_creation_inputs_missing"},
		{"incomplete", `{"plan":{"goal":"write"}}`, "assistant_draft_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newAssistantInteractionServiceTest(t)
			snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			stage := stageReadinessPlan(t, svc, snapshot, "stage", tc.payload)
			if stage.ConfirmationRequired {
				t.Fatal("Agent ready=true bypassed Server validation")
			}
			if stage.Draft == nil || stage.Draft.Ready || len(stage.Draft.BlockingIssues) == 0 {
				t.Fatalf("authoritative stage draft = %#v", stage.Draft)
			}
			recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(recovered.Drafts) != 1 || recovered.Drafts[0].Ready {
				t.Fatalf("snapshot = %#v", recovered)
			}
			events, err := svc.Events(context.Background(), "user-1", snapshot.ID, 0)
			if err != nil || len(events) == 0 {
				t.Fatalf("events = %#v, %v", events, err)
			}
			var event struct {
				Draft struct {
					Ready  bool              `json:"ready"`
					Issues []json.RawMessage `json:"blocking_issues"`
				} `json:"draft"`
			}
			if err := json.Unmarshal(events[len(events)-1].Data, &event); err != nil {
				t.Fatal(err)
			}
			if event.Draft.Ready || len(event.Draft.Issues) == 0 {
				t.Fatalf("event = %s", events[len(events)-1].Data)
			}
			_, err = svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "commit"})
			if err == nil || NormalizeInteractionError(err).Code != tc.code {
				t.Fatalf("commit error = %v", err)
			}
			if _, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "commit:commit"); err == nil {
				t.Fatal("known preflight failure created a commit receipt")
			}
		})
	}
}

type blockingConfirmationRepository struct {
	repository.Repository
	tasks repository.TaskRepository
}

type terminalDuringStageRepository struct{ repository.Repository }

func (r terminalDuringStageRepository) AssistantInteractions() repository.AssistantInteractionRepository {
	return terminalDuringStageInteractions{r.Repository.AssistantInteractions()}
}

type terminalDuringStageInteractions struct {
	repository.AssistantInteractionRepository
}

func (r terminalDuringStageInteractions) CreateDraftAttemptEventAtGeneration(context.Context, *model.AssistantInteractionDraft, *model.AssistantInteractionAttempt, *model.AssistantInteractionEvent, int64) (*model.AssistantInteractionDraft, *model.AssistantInteractionEvent, bool, error) {
	return nil, nil, false, repository.ErrInteractionTerminal
}

func TestCreationStageRejectsCommitCompletingAfterInitialRead(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	svc.repo = terminalDuringStageRepository{svc.repo}
	payload := json.RawMessage(completeInteractionPlan)
	digest := sha256.Sum256(payload)
	result, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "late", ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
	if structured := NormalizeInteractionError(err); structured == nil || structured.Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("terminal race stage = %#v, %v", result, err)
	}
}

func (r blockingConfirmationRepository) Tasks() repository.TaskRepository { return r.tasks }

type blockingConfirmationTasks struct {
	repository.TaskRepository
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingConfirmationTasks) FindByAssistantCheckpointID(ctx context.Context, userID, checkpointID string) (*model.Task, error) {
	r.once.Do(func() { close(r.reached); <-r.release })
	return r.TaskRepository.FindByAssistantCheckpointID(ctx, userID, checkpointID)
}

func TestCreationClaimFencesCompetingServerOperationsBeforeTaskCreation(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	blocked := &blockingConfirmationTasks{TaskRepository: svc.repo.Tasks(), reached: make(chan struct{}), release: make(chan struct{})}
	first := NewAssistantInteractionService(blockingConfirmationRepository{Repository: svc.repo, tasks: blocked}, nil, svc.clock)
	first.SetTaskService(svc.taskSvc)
	other := NewAssistantInteractionService(svc.repo, nil, svc.clock)
	other.SetTaskService(svc.taskSvc)
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "claim"}
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
	if _, err := other.ContinueInteraction(context.Background(), "user-1", snapshot.ID, InteractionContinueRequest{DraftID: stage.DraftID, Revision: stage.Revision}); err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("competing continue = %v", err)
	}
	payload := json.RawMessage(completeInteractionPlan)
	digest := sha256.Sum256(payload)
	if _, err := other.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "replacement", ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true}); err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("competing stage = %v", err)
	}
	if _, err := other.CommitInteraction(context.Background(), "user-1", snapshot.ID, req); err == nil || NormalizeInteractionError(err).Code != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("competing commit = %v", err)
	}
	// Release and wait here so the test observes the real task creation result.
	blocked.release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	result, err := other.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	if err != nil || result.Status != InteractionCommitAlreadyCreated || result.TaskID == "" {
		t.Fatalf("claim replay = %#v, %v", result, err)
	}
}

func TestCreationMalformedStageRetainsPreviousDraft(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, _ := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "malformed")
	first := stageReadinessPlan(t, svc, snapshot, "valid", completeInteractionPlan)
	for _, payload := range []string{`{"plan":`, `{"plan":{"goal":"write","surprise":true}}`, `{"plan":null}`} {
		digest := sha256.Sum256([]byte(payload))
		_, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: payload, ModeID: AssistantModeCreation, Payload: json.RawMessage(payload), PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
		if err == nil || NormalizeInteractionError(err).Code != "assistant_draft_invalid" {
			t.Fatalf("malformed stage = %v", err)
		}
	}
	latest, _ := svc.repo.AssistantInteractions().FindLatestDraft(context.Background(), "user-1", snapshot.ID, AssistantModeCreation)
	if latest.ID != first.DraftID {
		t.Fatal("malformed draft replaced the prior draft")
	}
}

func TestCreationOldReadyDraftCannotBeConfirmedAfterIncompleteReplacement(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, _ := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "replacement")
	old := stageReadinessPlan(t, svc, snapshot, "first", completeInteractionPlan)
	stageReadinessPlan(t, svc, snapshot, "second", `{"plan":{"goal":"still editing"}}`)
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: old.DraftID, Revision: old.Revision, IdempotencyKey: "old"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftRevisionConflict {
		t.Fatalf("old commit = %v", err)
	}
}

func TestCreationHistoricalMissingInputsOverrideStoredReady(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, _ := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "historical")
	payload := strings.Replace(completeInteractionPlan, `"constraints":`, `"missing_inputs":["contact"],"constraints":`, 1)
	_, _, err := svc.repo.AssistantInteractions().CreateDraftIfAttemptNewAtGeneration(context.Background(), &model.AssistantInteractionDraft{ID: uuid.NewString(), InteractionID: snapshot.ID, ModeID: AssistantModeCreation, Payload: json.RawMessage(payload), PayloadDigest: "legacy", Ready: true, ConfirmationState: model.AssistantInteractionDraftPending, ExpiresAt: svc.clock().Add(time.Hour), CreatedAt: svc.clock(), UpdatedAt: svc.clock()}, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: snapshot.ID, AttemptID: "legacy", Operation: "stage", Generation: snapshot.Generation, PayloadDigest: "legacy", CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(time.Hour)}, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _ := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
	if len(recovered.Drafts) == 0 || recovered.Drafts[0].Ready {
		t.Fatalf("historical snapshot = %#v", recovered)
	}
}

func TestCreationLegacyFailureNeverHidesTaskResultOrUncertainReceipt(t *testing.T) {
	for _, evidence := range []string{"task", "result", "uncertain"} {
		t.Run(evidence, func(t *testing.T) {
			svc, snapshot := newCreationConfirmationTestService(t)
			stage := stageReadinessPlan(t, svc, snapshot, "missing", strings.Replace(completeInteractionPlan, `"constraints":`, `"missing_inputs":["contact"],"constraints":`, 1))
			req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "legacy"}
			if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:legacy", Operation: "commit", Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(req), ErrorCode: InteractionErrorDraftRevisionConflict, CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			switch evidence {
			case "task":
				if err := svc.repo.Tasks().Create(context.Background(), &model.Task{ID: uuid.NewString(), UserID: "user-1", ProjectID: snapshot.ProjectID, AssistantCheckpointID: &stage.DraftID, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusPending}); err != nil {
					t.Fatal(err)
				}
			case "result", "uncertain":
				attempt := &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:authority", Operation: "commit", Generation: snapshot.Generation, PayloadDigest: "authority", ErrorCode: InteractionErrorCommitReconciliationRequired, CreatedAt: svc.clock().Add(-time.Minute), ExpiresAt: svc.clock().Add(time.Hour)}
				if evidence == "result" {
					attempt.Result = json.RawMessage(`{"status":"created","task_id":"existing-task"}`)
					attempt.ErrorCode = ""
				}
				if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), attempt); err != nil {
					t.Fatal(err)
				}
			}
			result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
			if evidence == "result" {
				if err != nil || result.TaskID != "existing-task" {
					t.Fatalf("authority = %#v, %v", result, err)
				}
			} else {
				want := InteractionErrorDraftRevisionConflict
				if evidence == "uncertain" {
					want = InteractionErrorCommitReconciliationRequired
				}
				if err == nil || NormalizeInteractionError(err).Code != want {
					t.Fatalf("evidence %s = %#v, %v", evidence, result, err)
				}
			}
			recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
			if err != nil {
				t.Fatal(err)
			}
			if evidence == "uncertain" && (recovered.RecoveryErrorCode != InteractionErrorCommitReconciliationRequired || recovered.Drafts[0].Ready) {
				t.Fatalf("uncertain snapshot = %#v", recovered)
			}
			if evidence == "result" && (recovered.CommitResult == nil || recovered.CommitResult.TaskID != "existing-task" || recovered.Drafts[0].Ready) {
				t.Fatalf("success snapshot = %#v", recovered)
			}
		})
	}
}

func TestCreationCompleteDraftStillRequiresAgentReadinessRequest(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	payload := json.RawMessage(completeInteractionPlan)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "editing", ModeID: AssistantModeCreation, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: false})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "not-ready"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorDraftNotReady {
		t.Fatalf("not ready = %v", err)
	}
}

func TestCreationRecoveryDoesNotOfferEditingForCompletedOrClaimedDraft(t *testing.T) {
	for _, state := range []string{model.AssistantInteractionDraftConfirmed, model.AssistantInteractionDraftConsumed} {
		t.Run(state, func(t *testing.T) {
			svc, snapshot := newCreationConfirmationTestService(t)
			stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
			if err := svc.repo.AssistantInteractions().UpdateDraftState(context.Background(), stage.DraftID, state, svc.clock()); err != nil {
				t.Fatal(err)
			}
			if state == model.AssistantInteractionDraftConsumed {
				if err := svc.repo.AssistantInteractions().UpdateLifecycle(context.Background(), snapshot.ID, model.AssistantInteractionCompleted, svc.clock()); err != nil {
					t.Fatal(err)
				}
			}
			recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.Drafts[0].Ready || strings.Contains(strings.Join(recovered.AvailableActions, ","), "send_message") || strings.Contains(strings.Join(recovered.AvailableActions, ","), "continue_editing") {
				t.Fatalf("terminal snapshot = %#v", recovered)
			}
			if state == model.AssistantInteractionDraftConfirmed && recovered.RecoveryErrorCode != InteractionErrorCommitReconciliationRequired {
				t.Fatalf("claim snapshot = %#v", recovered)
			}
		})
	}
}

func TestCreationContinueIsDurableAndRejectsStaleOrClaimedDraft(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, _ := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "continue")
	stage := stageReadinessPlan(t, svc, snapshot, "first", completeInteractionPlan)
	restarted := NewAssistantInteractionService(svc.repo, assistantInteractionFailingFactory{}, svc.clock)
	request := InteractionContinueRequest{DraftID: stage.DraftID, Revision: stage.Revision}
	for range 2 {
		withdrawn, err := restarted.ContinueInteraction(context.Background(), "user-1", snapshot.ID, request)
		if err != nil || len(withdrawn.Drafts) != 1 || withdrawn.Drafts[0].Ready || withdrawn.Drafts[0].ConfirmationState != model.AssistantInteractionDraftSuperseded {
			t.Fatalf("continue = %#v, %v", withdrawn, err)
		}
	}
	recovered, err := restarted.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil || recovered.Drafts[0].Ready || !strings.Contains(strings.Join(recovered.AvailableActions, ","), "send_message") {
		t.Fatalf("refresh = %#v, %v", recovered, err)
	}
	next := stageReadinessPlan(t, svc, snapshot, "next", completeInteractionPlan)
	if next.Revision != stage.Revision+1 {
		t.Fatal("revision did not advance")
	}
	if _, err := restarted.ContinueInteraction(context.Background(), "user-1", snapshot.ID, request); err == nil {
		t.Fatal("stale withdrawal accepted")
	}
	_ = svc.repo.AssistantInteractions().UpdateDraftState(context.Background(), next.DraftID, model.AssistantInteractionDraftConfirmed, svc.clock())
	if _, err := restarted.ContinueInteraction(context.Background(), "user-1", snapshot.ID, InteractionContinueRequest{DraftID: next.DraftID, Revision: next.Revision}); err == nil {
		t.Fatal("claimed draft withdrawn")
	}
}

func newCreationConfirmationTestService(t *testing.T) (*AssistantInteractionService, InteractionSnapshot) {
	t.Helper()
	db := setupTaskTestDB(t)
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}, &model.AssistantCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	repo := repository.New(db)
	if err := repo.Users().Create(context.Background(), &model.User{ID: "user-1", Email: uuid.NewString() + "@example.com", InviteCode: uuid.NewString(), Tier: model.TierEnterprise}); err != nil {
		t.Fatal(err)
	}
	project := createAssistantProject(t, repo, "user-1", "confirmation")
	logger := zerolog.New(io.Discard)
	tasks := newTestTaskService(repo, &mockEnqueuer{}, &fakeTaskStorage{}, &logger, "", nil, nil)
	svc := NewAssistantInteractionService(repo, &assistantInteractionTestFactory{}, func() time.Time { return time.Now().UTC() })
	svc.SetTaskService(tasks)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: project.ID}, "creation")
	if err != nil {
		t.Fatal(err)
	}
	return svc, snapshot
}

func TestCreationConfirmationCreatesOneTaskWithApprovedSupplementsAndFinalSettings(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "complete", completeInteractionPlan)
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "create", Options: json.RawMessage(`{"plan":{"execution_profile":"quality","image_ratio":"1:1","image_capability_key":"standard"}}`)}
	first, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	if err != nil || first.TaskID == "" {
		t.Fatalf("commit = %#v, %v", first, err)
	}
	restarted := NewAssistantInteractionService(svc.repo, nil, svc.clock)
	for _, key := range []string{"create", "another-key"} {
		req.IdempotencyKey = key
		replay, err := restarted.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
		if err != nil || replay.Status != InteractionCommitAlreadyCreated || replay.TaskID != first.TaskID {
			t.Fatalf("replay = %#v, %v", replay, err)
		}
	}
	req.IdempotencyKey = "create"
	req.Options = json.RawMessage(`{"plan":{"image_ratio":"16:9"}}`)
	if _, err := restarted.CommitInteraction(context.Background(), "user-1", snapshot.ID, req); !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("changed final options reused receipt: %v", err)
	}
	task, err := svc.repo.Tasks().FindByAssistantCheckpointID(context.Background(), "user-1", stage.DraftID)
	if err != nil || !strings.Contains(task.Prompt, "User approved placeholders") || !strings.Contains(task.Prompt, "Do not invent contact details") || task.ExecutionProfile != "quality" || task.ImageRatio != "1:1" {
		t.Fatalf("task = %#v, %v", task, err)
	}
}

func TestCreationLegacyConflictReclassifiesAndCorrectedRevisionRecovers(t *testing.T) {
	for _, tc := range []struct {
		name         string
		options      json.RawMessage
		recoveryCode string
	}{
		{name: "no-options", recoveryCode: InteractionErrorCreationInputsMissing},
		{name: "with-options", options: json.RawMessage(`{"plan":{"execution_profile":"effective"}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, snapshot := newCreationConfirmationTestService(t)
			stage := stageReadinessPlan(t, svc, snapshot, "missing", strings.Replace(completeInteractionPlan, `"constraints":`, `"missing_inputs":["contact"],"constraints":`, 1))
			req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "legacy", Options: tc.options}
			if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "commit:legacy", Operation: "commit", Generation: snapshot.Generation, PayloadDigest: commitPayloadDigest(req), ErrorCode: InteractionErrorDraftRevisionConflict, CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionCommitReceiptRetention)}); err != nil {
				t.Fatal(err)
			}
			_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
			if err == nil || NormalizeInteractionError(err).Code != InteractionErrorCreationInputsMissing {
				t.Fatalf("legacy = %v", err)
			}
			recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
			if err != nil || recovered.RecoveryErrorCode != tc.recoveryCode || len(recovered.Drafts[0].BlockingIssues) == 0 || !strings.Contains(strings.Join(recovered.AvailableActions, ","), "send_message") {
				t.Fatalf("legacy snapshot = %#v, %v", recovered, err)
			}
			corrected := stageReadinessPlan(t, svc, snapshot, "corrected", completeInteractionPlan)
			if corrected.Draft == nil || !corrected.Draft.Ready {
				t.Fatalf("corrected = %#v", corrected)
			}
			recovered, err = svc.Snapshot(context.Background(), "user-1", snapshot.ID)
			if err != nil || recovered.RecoveryErrorCode != "" {
				t.Fatalf("corrected recovery = %#v, %v", recovered, err)
			}
			result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: corrected.DraftID, Revision: corrected.Revision, IdempotencyKey: "fixed"})
			if err != nil || result.TaskID == "" {
				t.Fatalf("corrected commit = %#v, %v", result, err)
			}
		})
	}
}

func TestCreationKnownPreflightFailureAllowsCorrection(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	project, _ := svc.repo.Projects().FindByID(context.Background(), snapshot.ProjectID)
	project.Status = model.ProjectStatusArchived
	if err := svc.repo.Projects().Update(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "inactive"})
	if !errors.Is(err, ErrTaskCreationProjectInactive) {
		t.Fatalf("preflight = %v", err)
	}
	corrected := stageReadinessPlan(t, svc, snapshot, "corrected", completeInteractionPlan)
	if corrected.Draft == nil || !corrected.Draft.Ready {
		t.Fatalf("correction = %#v", corrected)
	}
}
