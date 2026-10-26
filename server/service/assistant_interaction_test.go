package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type assistantInteractionTestRuntime struct {
	factory *assistantInteractionTestFactory
}

func (r assistantInteractionTestRuntime) Command(_ context.Context, method string, _ any) error {
	if r.factory != nil {
		r.factory.mu.Lock()
		r.factory.commands = append(r.factory.commands, method)
		r.factory.mu.Unlock()
	}
	return nil
}
func (assistantInteractionTestRuntime) Close() error { return nil }

type assistantInteractionTestFactory struct {
	mu        sync.Mutex
	starts    int
	callbacks map[string]func(string, any)
	commands  []string
}

func (f *assistantInteractionTestFactory) Start(_ context.Context, sessionID, _ string, _ string, callback func(string, any)) (Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.callbacks == nil {
		f.callbacks = map[string]func(string, any){}
	}
	f.starts++
	f.callbacks[sessionID] = callback
	return assistantInteractionTestRuntime{factory: f}, nil
}

func (f *assistantInteractionTestFactory) callback(sessionID string) func(string, any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callbacks[sessionID]
}

type assistantInteractionFailingFactory struct{}

func (assistantInteractionFailingFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	return nil, errors.New("runtime unavailable")
}

func newAssistantInteractionServiceTest(t *testing.T) (*AssistantInteractionService, *assistantInteractionTestFactory) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}, &model.Project{}, &model.ProjectProfileState{}, &model.ProjectProfileRevision{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	factory := &assistantInteractionTestFactory{}
	clock := func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return NewAssistantInteractionService(repository.New(db), factory, clock), factory
}

func TestCommitInteractionCreatesProfileProjectAndReplaysDurableReceipt(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "profile-create")
	if err != nil {
		t.Fatal(err)
	}
	profile := model.NewProjectProfile()
	profile.Dimensions.Identity.Content["name"] = "茶饮账号"
	payload, err := json.Marshal(map[string]any{
		"project": map[string]any{
			"name":         "茶饮账号",
			"platform":     "wechat",
			"instructions": "分享真实体验",
		},
		"profile":         profile,
		"progress":        map[string]string{"stage": "ready"},
		"missing_fields":  []string{},
		"inferred_fields": []string{},
		"ready":           true,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "profile-stage-1", ModeID: AssistantModeProfile,
		Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "profile-commit-1"})
	if err != nil {
		t.Fatalf("profile commit = %#v, %v", result, err)
	}
	if result.Status != InteractionCommitCreated || len(result.Result) == 0 {
		t.Fatalf("profile commit result = %#v", result)
	}
	var committed AssistantCommitResult
	if err := json.Unmarshal(result.Result, &committed); err != nil {
		t.Fatal(err)
	}
	if committed.Project == nil || committed.Project.ID == "" || committed.Profile == nil || committed.Profile.Status != model.ProfileStatusConfirmed {
		t.Fatalf("committed profile result = %#v", committed)
	}

	replay, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "profile-commit-1"})
	if err != nil {
		t.Fatalf("profile commit replay = %#v, %v", replay, err)
	}
	if replay.Status != InteractionCommitAlreadyCreated {
		t.Fatalf("profile commit replay status = %q", replay.Status)
	}
	recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatalf("snapshot after commit = %v", err)
	}
	if recovered.CommitResult == nil || recovered.CommitResult.Status != InteractionCommitCreated || recovered.CommitResult.Result == nil {
		t.Fatalf("snapshot commit receipt = %#v, want durable commit result", recovered.CommitResult)
	}
	var recoveredCommit AssistantCommitResult
	if err := json.Unmarshal(recovered.CommitResult.Result, &recoveredCommit); err != nil {
		t.Fatal(err)
	}
	if recoveredCommit.Project == nil || recoveredCommit.Project.ID != committed.Project.ID {
		t.Fatalf("snapshot commit project = %#v, want %q", recoveredCommit.Project, committed.Project.ID)
	}
	interaction, err := svc.repo.AssistantInteractions().FindByIDForUser(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if interaction.ProjectID != committed.Project.ID {
		t.Fatalf("interaction project id = %q, want %q", interaction.ProjectID, committed.Project.ID)
	}
	projects, err := svc.repo.Projects().ListByUserID(context.Background(), "user-1", repository.ProjectListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != committed.Project.ID {
		t.Fatalf("projects after replay = %#v", projects)
	}
	revisions, err := svc.repo.ProjectProfileRevisions().ListByProject(context.Background(), committed.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || revisions[0].Revision != 1 {
		t.Fatalf("profile revisions after replay = %#v", revisions)
	}
}

func TestAssistantInteractionStartsLogicalSessionBeforeCommands(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "handshake")
	if err != nil {
		t.Fatal(err)
	}
	factory.mu.Lock()
	commands := append([]string(nil), factory.commands...)
	factory.mu.Unlock()
	if len(commands) != 1 || commands[0] != "session/start" {
		t.Fatalf("runtime commands = %#v, want one session/start before interaction commands", commands)
	}
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{Generation: snapshot.Generation, ClientMessageID: "handshake-message", Text: "继续"}); err != nil {
		t.Fatal(err)
	}
	factory.mu.Lock()
	commands = append([]string(nil), factory.commands...)
	factory.mu.Unlock()
	if len(commands) != 2 || commands[1] != "session/prompt" {
		t.Fatalf("runtime commands after message = %#v, want session/start then session/prompt", commands)
	}
}

func TestInteractionMessageAttemptConflictsWhenPayloadChanges(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "message-digest")
	if err != nil {
		t.Fatal(err)
	}
	input := InteractionMessageInput{Generation: snapshot.Generation, ClientMessageID: "message-1", Text: "第一条需求"}
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, input); err != nil {
		t.Fatal(err)
	}
	input.Text = "另一条需求"
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, input); !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("changed message payload error = %v", err)
	}
}

func TestInteractionMessageDoesNotReplayPendingAttempt(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "pending-message")
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"contentBlocks": []map[string]string{{"type": "text", "text": "需求"}}}
	digest := interactionCommandDigest("session/prompt", "message-1", params)
	if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, AttemptID: "message-1", Operation: string(InteractionOperationMessage), Generation: snapshot.Generation,
		PayloadDigest: digest, Status: model.AssistantInteractionAttemptPending, CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionAttemptRetention),
	}); err != nil {
		t.Fatal(err)
	}
	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{Generation: snapshot.Generation, ClientMessageID: "message-1", Text: "需求"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorReconnecting {
		t.Fatalf("pending message retry error = %v, want reconnecting", err)
	}
}

func TestInteractionAnswerRequiresAttemptIDForRetrySafety(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "answer-idempotency")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.AnswerQuestion(context.Background(), "user-1", snapshot.ID, InteractionAnswerInput{
		Generation: snapshot.Generation,
		CallID:     "call-1",
		Answers:    []InteractiveAnswer{{ID: "q-1", Selected: []string{"option-1"}}},
	})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorMessageNotAccepted {
		t.Fatalf("missing answer attempt error = %v, want %s", err, InteractionErrorMessageNotAccepted)
	}
}

func TestCompletedInteractionSnapshotDoesNotRestartRuntime(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "completed-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.AssistantInteractions().UpdateLifecycle(context.Background(), snapshot.ID, model.AssistantInteractionCompleted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	svc.runtimeMu.Lock()
	svc.runtimes = map[string]Runtime{}
	svc.runtimeMu.Unlock()
	if _, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	factory.mu.Lock()
	starts := factory.starts
	factory.mu.Unlock()
	if starts != 1 {
		t.Fatalf("completed snapshot restarted runtime: starts=%d", starts)
	}
}

func TestAssistantInteractionCreateOrResumeReusesScope(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	input := AssistantInteractionInput{ModeID: "creation", ProjectID: "project-1", ContentID: "content-1", AgentID: "project-creator", Intent: "create"}
	first, err := svc.CreateOrResume(context.Background(), "user-1", input, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateOrResume(context.Background(), "user-1", input, "attempt-2")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Generation != 1 || factory.starts != 1 {
		t.Fatalf("first=%#v second=%#v starts=%d", first, second, factory.starts)
	}
}

func TestAssistantInteractionModesShareOneInteraction(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	first, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-1"}, "create")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeReview, ProjectID: "project-1"}, "review")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("mode switch created a second interaction: %s vs %s", first.ID, second.ID)
	}
	if second.ModeID != AssistantModeReview {
		t.Fatalf("current mode = %q", second.ModeID)
	}
}

func TestAssistantInteractionResumeUpdatesModeAndContextOnSameInteraction(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	first, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-1"}, "create")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{
		ModeID:              AssistantModeReview,
		ProjectID:           "project-2",
		ContentID:           "content-2",
		ResumeInteractionID: first.ID,
	}, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != first.ID || resumed.ModeID != AssistantModeReview || resumed.ProjectID != "project-2" || resumed.ContentID != "content-2" {
		t.Fatalf("resumed snapshot = %#v, want same interaction with updated context", resumed)
	}
}

func TestCompletedInteractionRejectsRuntimeWritesAndReplaysExistingCommit(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "terminal")
	if err != nil {
		t.Fatal(err)
	}
	profile := model.NewProjectProfile()
	payload, err := json.Marshal(map[string]any{
		"project": map[string]any{"name": "终态测试", "platform": "wechat"},
		"profile": profile, "progress": map[string]string{"stage": "ready"},
		"missing_fields": []string{}, "inferred_fields": []string{}, "ready": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "terminal-stage", ModeID: AssistantModeProfile, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "terminal-commit"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != InteractionCommitCreated {
		t.Fatalf("initial commit status = %q", created.Status)
	}
	if svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "assistant.message", map[string]any{"text": "late"}) {
		t.Fatal("late runtime event was accepted after completion")
	}
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{Generation: snapshot.Generation, ClientMessageID: "late-message", Text: "late"}); err == nil || NormalizeInteractionError(err).Code != InteractionErrorRestartRequired {
		t.Fatalf("late message error = %v, want restart_required", err)
	}
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{InteractionID: snapshot.ID, Generation: snapshot.Generation, AttemptID: "late-stage", ModeID: AssistantModeProfile, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), Ready: true}); err == nil || NormalizeInteractionError(err).Code != InteractionErrorRestartRequired {
		t.Fatalf("late stage error = %v, want restart_required", err)
	}
	replay, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "different-key"})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Status != InteractionCommitAlreadyCreated || replay.TaskID != created.TaskID {
		t.Fatalf("post-completion replay = %#v, want existing receipt", replay)
	}
}

func TestAssistantInteractionGenerationRejectsStaleCommand(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	input := AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", input, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplaceRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation, "runtime_closed"); err != nil {
		t.Fatal(err)
	}
	err = svc.RunCommand(context.Background(), "user-1", snapshot.ID, snapshot.Generation, InteractionOperationMessage, "attempt-message", func(RuntimeBindingView) error { return nil })
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
		t.Fatalf("stale command error = %v", err)
	}
}

func TestAssistantInteractionCommandsAreSerialized(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	var active int32
	var maxActive int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_ = svc.RunCommand(context.Background(), "user-1", snapshot.ID, snapshot.Generation, InteractionOperationMessage, NewInteractionAttemptID(), func(RuntimeBindingView) error {
				current := atomic.AddInt32(&active, 1)
				for {
					previous := atomic.LoadInt32(&maxActive)
					if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return nil
			})
		}(i)
	}
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("max concurrent commands = %d, want 1", maxActive)
	}
}

func TestAssistantInteractionReplacementDropsOldRuntimeCallbacks(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	oldRuntime, err := svc.RuntimeBindingForTest(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	oldCallback := factory.callback(oldRuntime.LogicalSessionID)
	if oldCallback == nil {
		t.Fatal("missing old runtime callback")
	}
	if _, err := svc.ReplaceRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation, "runtime_closed"); err != nil {
		t.Fatal(err)
	}
	if accepted := svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "draft.ready", map[string]any{"ready": true}); accepted {
		t.Fatal("stale runtime event was accepted")
	}
	if accepted := svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation+1, "runtime.ready", map[string]any{}); !accepted {
		t.Fatal("current runtime event was rejected")
	}
	events, err := svc.Events(context.Background(), "user-1", snapshot.ID, 0)
	if err != nil || len(events) != 1 || events[0].Generation != snapshot.Generation+1 {
		t.Fatalf("current generation events = %#v, err=%v", events, err)
	}
	oldCallback("draft.ready", map[string]any{"ready": true})
}

func TestAssistantInteractionRuntimeEventsPersistMetadataOnly(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "event-redaction")
	if err != nil {
		t.Fatal(err)
	}
	secretBody := "用户不应进入 durable event 的正文"
	materialURL := "https://assets.example/material/private.png"
	if !svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "assistant.message", map[string]any{"text": secretBody, "material_url": materialURL, "runtime_token": "secret-token"}) {
		t.Fatal("runtime event was not accepted")
	}
	events, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), snapshot.ID, snapshot.Generation, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d", len(events))
	}
	encoded := string(events[0].Data)
	for _, forbidden := range []string{secretBody, materialURL, "secret-token"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("durable event leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(encoded, "text_length") {
		t.Fatalf("durable event omitted safe metadata: %s", encoded)
	}
}

func TestAssistantInteractionReplacesStaleBindingAfterServerRestart(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}

	// A fresh service instance has the durable binding but none of the old
	// process-local runtime handles. It must fence that binding and create the
	// next generation before accepting work.
	restartedFactory := &assistantInteractionTestFactory{}
	restarted := NewAssistantInteractionService(svc.repo, restartedFactory, svc.clock)
	binding, err := restarted.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Generation != snapshot.Generation+1 || binding.LogicalSessionID == "" {
		t.Fatalf("restarted binding = %#v, want generation %d with a new session", binding, snapshot.Generation+1)
	}
}

func TestAssistantInteractionSnapshotRecoversAfterServerRestart(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "creation", AgentID: "project-creator", Intent: "create"}, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewAssistantInteractionService(svc.repo, &assistantInteractionTestFactory{}, svc.clock)
	recovered, err := restarted.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Generation != snapshot.Generation+1 {
		t.Fatalf("recovered snapshot generation = %d, want %d", recovered.Generation, snapshot.Generation+1)
	}
}

func TestAssistantInteractionSnapshotKeepsReadyDraftWhenRuntimeReplacementFails(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "ready-draft")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"plan":{"goal":"runtime independent confirmation"}}`)
	digest := sha256.Sum256(payload)
	if _, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID,
		Generation:    snapshot.Generation,
		AttemptID:     "ready-draft-stage",
		ModeID:        AssistantModeCreation,
		Payload:       payload,
		PayloadDigest: hex.EncodeToString(digest[:]),
		Ready:         true,
	}); err != nil {
		t.Fatal(err)
	}

	// The durable draft must remain readable even if the old runtime is gone
	// and the replacement cannot start. Confirmation no longer depends on it.
	restarted := NewAssistantInteractionService(svc.repo, assistantInteractionFailingFactory{}, svc.clock)
	recovered, err := restarted.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatalf("snapshot with failed runtime replacement = %v", err)
	}
	if recovered.Lifecycle != model.AssistantInteractionNeedsConfirmation || len(recovered.Drafts) != 1 || !recovered.Drafts[0].Ready {
		t.Fatalf("recovered snapshot = %#v, want ready durable draft", recovered)
	}
}

func TestAssistantInteractionSnapshotReportsStartingWhenRuntimeIsNotConfigured(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	svc.factory = nil
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "starting-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RuntimeStatus != InteractionRuntimeStarting {
		t.Fatalf("runtime status = %q, want %q", snapshot.RuntimeStatus, InteractionRuntimeStarting)
	}
}

func TestResolveRuntimeCapabilityHandlesMissingRepository(t *testing.T) {
	svc := NewAssistantInteractionService(nil, nil, time.Now)
	token := "runtime-token"
	hash := confirmationHash(token)
	svc.runtimeMu.Lock()
	svc.capabilities[hash] = interactionRuntimeCapability{
		UserID:        "user-1",
		InteractionID: "interaction-1",
		Generation:    1,
		ExpiresAt:     time.Now().Add(time.Hour),
	}
	svc.runtimeMu.Unlock()
	if _, _, _, _, _, _, err := svc.ResolveRuntimeCapability(token); err != ErrInteractiveToken {
		t.Fatalf("missing repository error = %v, want %v", err, ErrInteractiveToken)
	}
}
