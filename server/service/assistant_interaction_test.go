package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
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

type assistantInteractionModeFactory struct {
	mu              sync.Mutex
	starts          int
	specs           []InteractiveRuntimeSpec
	capabilityCheck func(string) error
	commands        []struct {
		method string
		params map[string]any
	}
}

type assistantInteractionModeRuntime struct {
	factory *assistantInteractionModeFactory
}

func (r assistantInteractionModeRuntime) Command(_ context.Context, method string, params any) error {
	if method == "session/start" && r.factory.capabilityCheck != nil {
		r.factory.mu.Lock()
		token := r.factory.specs[len(r.factory.specs)-1].RuntimeToken
		r.factory.mu.Unlock()
		if err := r.factory.capabilityCheck(token); err != nil {
			return err
		}
	}
	r.factory.mu.Lock()
	defer r.factory.mu.Unlock()
	encoded, _ := json.Marshal(params)
	var copied map[string]any
	_ = json.Unmarshal(encoded, &copied)
	r.factory.commands = append(r.factory.commands, struct {
		method string
		params map[string]any
	}{method: method, params: copied})
	return nil
}

func (assistantInteractionModeRuntime) Close() error { return nil }

func (f *assistantInteractionModeFactory) StartSession(_ context.Context, spec InteractiveRuntimeSpec, _ func(string, any)) (Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	f.specs = append(f.specs, spec)
	return assistantInteractionModeRuntime{factory: f}, nil
}

func (f *assistantInteractionModeFactory) Start(ctx context.Context, sessionID, presetID, token string, callback func(string, any)) (Runtime, error) {
	return f.StartSession(ctx, InteractiveRuntimeSpec{SessionID: sessionID, AgentPackID: presetID, RuntimeToken: token}, callback)
}

func (f *assistantInteractionModeFactory) commandSnapshot() []struct {
	method string
	params map[string]any
} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]struct {
		method string
		params map[string]any
	}{}, f.commands...)
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

type assistantInteractionCommandFailureFactory struct {
	mu       sync.Mutex
	starts   int
	commands int
}

type assistantInteractionCommandRecoveryFailureFactory struct {
	mu     sync.Mutex
	starts int
}

type assistantInteractionCommandFailureRuntime struct {
	factory *assistantInteractionCommandFailureFactory
}

func (r assistantInteractionCommandFailureRuntime) Command(_ context.Context, method string, _ any) error {
	if method == "session/start" {
		return nil
	}
	if r.factory != nil {
		r.factory.mu.Lock()
		r.factory.commands++
		r.factory.mu.Unlock()
	}
	return errors.New("runtime command failed")
}

func (assistantInteractionCommandFailureRuntime) Close() error { return nil }

func (f *assistantInteractionCommandFailureFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return assistantInteractionCommandFailureRuntime{factory: f}, nil
}

func (f *assistantInteractionCommandRecoveryFailureFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.starts > 1 {
		return nil, errors.New("replacement runtime unavailable")
	}
	return assistantInteractionCommandFailureRuntime{}, nil
}

func newAssistantInteractionServiceTest(t *testing.T) (*AssistantInteractionService, *assistantInteractionTestFactory) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}, &model.Project{}, &model.ProjectProfileState{}, &model.ProjectProfileRevision{}, &model.Task{}, &model.TaskFile{}, &model.AnalyticsState{}, &model.AnalyticsContent{}, &model.AnalyticsObservation{}); err != nil {
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

func TestAssistantInteractionPrepareDefersRuntimeUntilFirstMessage(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	logger := zerolog.Nop()
	svc.SetTaskService(NewTaskService(svc.repo, nil, nil, &logger, "", nil, nil))
	for _, project := range []*model.Project{{ID: "project-1", UserID: "user-1"}, {ID: "project-2", UserID: "user-1"}} {
		if err := svc.repo.Projects().Create(context.Background(), project); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.repo.Tasks().Create(context.Background(), &model.Task{ID: "content-1", UserID: "user-1", ProjectID: "project-2", Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-1", PrepareOnly: true}, "prepare-1")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Lifecycle != InteractionLifecyclePrepared || prepared.RuntimeStatus != InteractionRuntimeNotStarted {
		t.Fatalf("prepared snapshot = %#v", prepared)
	}
	factory.mu.Lock()
	if len(factory.commands) != 0 {
		t.Fatalf("runtime commands during prepare = %#v, want none", factory.commands)
	}
	factory.mu.Unlock()
	read, err := svc.Snapshot(context.Background(), "user-1", prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Lifecycle != InteractionLifecyclePrepared || read.RuntimeStatus != InteractionRuntimeNotStarted {
		t.Fatalf("prepared read snapshot = %#v", read)
	}
	if err := svc.SendMessage(context.Background(), "user-1", prepared.ID, InteractionMessageInput{
		Generation: prepared.Generation, ClientMessageID: "message-1", Text: "继续", ModeID: interactionStringPtr(AssistantModeReview), ProjectID: interactionStringPtr("project-2"), ContentID: interactionStringPtr("content-1"),
	}); err != nil {
		t.Fatal(err)
	}
	active, err := svc.Snapshot(context.Background(), "user-1", prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.Lifecycle != InteractionLifecycleActive || active.RuntimeStatus != InteractionRuntimeRunning || active.ModeID != AssistantModeReview || active.ProjectID != "project-2" || active.ContentID != "content-1" {
		t.Fatalf("activated snapshot = %#v", active)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.commands) != 2 || factory.commands[0] != "session/start" || factory.commands[1] != "session/prompt" {
		t.Fatalf("runtime commands after activation = %#v", factory.commands)
	}
}

func TestAssistantInteractionExpiredPreparationStartsFirstPromptWithoutGenerationChange(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare-expiry")
	if err != nil {
		t.Fatal(err)
	}
	activatedAt := svc.clock().Add(interactionRuntimeTTL + time.Hour)
	svc.clock = func() time.Time { return activatedAt }
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello"}); err != nil {
		t.Fatalf("first prompt after idle preparation: %v", err)
	}
	current, err := svc.repo.AssistantInteractions().FindByIDForUser(ctx, "user-1", prepared.ID)
	if err != nil || current.Generation != prepared.Generation || !current.ExpiresAt.Equal(activatedAt.Add(interactionRuntimeTTL)) || !current.LastActivityAt.Equal(activatedAt) || !current.UpdatedAt.Equal(activatedAt) {
		t.Fatalf("activation lease = %#v, %v", current, err)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.starts != 1 || !reflect.DeepEqual(factory.commands, []string{"session/start", "session/prompt"}) {
		t.Fatalf("first prompt runtime: starts=%d commands=%v", factory.starts, factory.commands)
	}
}

func interactionStringPtr(value string) *string { return &value }

func TestAssistantInteractionRuntimeCapabilityIsAvailableDuringHandshake(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	factory.capabilityCheck = func(token string) error {
		_, _, _, _, _, _, err := svc.ResolveRuntimeCapability(token)
		return err
	}
	svc.factory = factory
	if _, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "handshake-capability"); err != nil {
		t.Fatalf("create interaction while authenticating MCP during runtime handshake: %v", err)
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
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorCommandOutcomeUnknown {
		t.Fatalf("pending message retry error = %v, want outcome unknown", err)
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
	createdAt := svc.clock()
	svc.clock = func() time.Time { return createdAt.Add(25 * time.Hour) }
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

func TestInteractionModeSwitchUsesOneRuntimeAndModeScopedLogicalSessions(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc.factory = factory

	profile, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "mode-profile")
	if err != nil {
		t.Fatal(err)
	}
	creation, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ResumeInteractionID: profile.ID}, "mode-creation")
	if err != nil {
		t.Fatal(err)
	}
	if creation.ID != profile.ID {
		t.Fatalf("mode switch created a second interaction: profile=%q creation=%q", profile.ID, creation.ID)
	}
	review, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeReview, ResumeInteractionID: profile.ID}, "mode-review")
	if err != nil {
		t.Fatal(err)
	}
	if review.ID != profile.ID {
		t.Fatalf("review switch created a second interaction: profile=%q review=%q", profile.ID, review.ID)
	}
	if _, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ResumeInteractionID: profile.ID}, "mode-creation-again"); err != nil {
		t.Fatal(err)
	}

	factory.mu.Lock()
	starts := factory.starts
	specs := append([]InteractiveRuntimeSpec(nil), factory.specs...)
	factory.mu.Unlock()
	if starts != 1 {
		t.Fatalf("runtime starts = %d, want one shared runtime", starts)
	}
	if len(specs) != 1 || specs[0].WorkspaceID != profile.ID || len(specs[0].PresetIDs) != 3 {
		t.Fatalf("runtime spec = %#v, want interaction workspace and all presets", specs)
	}
	commands := factory.commandSnapshot()
	if len(commands) != 3 {
		t.Fatalf("runtime commands = %#v, want one logical session per mode", commands)
	}
	wantPresets := []string{"project-creator", "creation-planner", "postmortem-review"}
	seen := map[string]string{}
	for index, command := range commands {
		if command.method != "session/start" {
			t.Fatalf("command %d method = %q, want session/start", index, command.method)
		}
		preset, _ := command.params["presetId"].(string)
		if preset != wantPresets[index] {
			t.Fatalf("command %d preset = %q, want %q", index, preset, wantPresets[index])
		}
		sessionID, _ := command.params["sessionId"].(string)
		if sessionID == "" {
			t.Fatalf("command %d has empty logical session", index)
		}
		if previous := seen[preset]; previous != "" && previous != sessionID {
			t.Fatalf("preset %q switched logical sessions: %q then %q", preset, previous, sessionID)
		}
		seen[preset] = sessionID
	}
}

func TestAssistantInteractionCreateOrResumeReusesScope(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	createInteractionContextFixture(t, svc.repo, "project-1", "content-1")
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

func TestAssistantInteractionCreateOrResumeReplaysIdempotencyAfterCompletion(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	createInteractionContextFixture(t, svc.repo, "project-1", "content-1")
	input := AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-1", ContentID: "content-1"}
	first, err := svc.CreateOrResume(context.Background(), "user-1", input, "stable-create")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.AssistantInteractions().UpdateLifecycle(context.Background(), first.ID, model.AssistantInteractionCompleted, svc.clock()); err != nil {
		t.Fatal(err)
	}

	replay, err := svc.CreateOrResume(context.Background(), "user-1", input, "stable-create")
	if err != nil {
		t.Fatalf("idempotent create replay = %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("idempotent create replay id = %q, want %q", replay.ID, first.ID)
	}
}

func TestAssistantInteractionCreateOrResumeRejectsIdempotencyKeyReuseWithDifferentInput(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	createInteractionContextFixture(t, svc.repo, "project-1", "")
	if _, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-1"}, "stable-create"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-2"}, "stable-create")
	if !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("idempotency key reuse error = %v, want ErrInteractiveIdempotency", err)
	}
}

func TestAssistantInteractionCreateOrResumeRejectsUnknownMode(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	_, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: "unknown"}, "unknown-mode")
	if err == nil {
		t.Fatal("unknown mode was accepted")
	}
	structured := NormalizeInteractionError(err)
	if structured.Code != InteractionErrorMessageNotAccepted || structured.Status != 400 {
		t.Fatalf("unknown mode error = %#v, want a safe 400 interaction error", structured)
	}
}

func TestAssistantInteractionModesShareOneInteraction(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	createInteractionContextFixture(t, svc.repo, "project-1", "")
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
	createInteractionContextFixture(t, svc.repo, "project-1", "")
	createInteractionContextFixture(t, svc.repo, "project-2", "content-2")
	logger := zerolog.Nop()
	svc.SetTaskService(NewTaskService(svc.repo, nil, nil, &logger, "", nil, nil))
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

func TestInteractionRuntimeEventsCarryModeMetadataForSharedRuntime(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "event-mode")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := svc.RuntimeBindingForTest(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "assistant.message", map[string]any{"session_id": binding.LogicalSessionID, "text": "只保存在活动流"}) {
		t.Fatal("runtime event was rejected")
	}
	events, err := svc.Events(context.Background(), "user-1", snapshot.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("runtime event was not returned")
	}
	var data map[string]any
	if err := json.Unmarshal(events[len(events)-1].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["mode_id"] != AssistantModeCreation {
		t.Fatalf("runtime event mode metadata = %#v, want %q", data["mode_id"], AssistantModeCreation)
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

func TestAssistantInteractionCommandFailureFencesGenerationAndReplacesRuntime(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionCommandFailureFactory{}
	svc.factory = factory
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "command-failure")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{Generation: snapshot.Generation, ClientMessageID: "message-1", Text: "继续"})
	if err == nil || NormalizeInteractionError(err).Code != "assistant_command_outcome_unknown" {
		t.Fatalf("command failure = %v, want outcome unknown", err)
	}
	recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Generation != snapshot.Generation+1 {
		t.Fatalf("generation after command failure = %d, want %d", recovered.Generation, snapshot.Generation+1)
	}
	attempt, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "message-1")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != model.AssistantInteractionAttemptPending {
		t.Fatalf("attempt status after ambiguous delivery and successful generation replacement = %q, want pending", attempt.Status)
	}
	if svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "assistant.message", map[string]any{"text": "late"}) {
		t.Fatal("late event from failed generation was accepted")
	}
	input := InteractionMessageInput{Generation: snapshot.Generation + 1, ClientMessageID: "message-1", Text: "继续"}
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, input); err == nil || NormalizeInteractionError(err).Code != "assistant_command_outcome_unknown" {
		t.Fatalf("retry after ambiguous delivery = %v, want outcome unknown", err)
	}
	factory.mu.Lock()
	starts := factory.starts
	commands := factory.commands
	factory.mu.Unlock()
	if starts != 2 {
		t.Fatalf("runtime starts = %d, want replacement runtime", starts)
	}
	if commands != 1 {
		t.Fatalf("runtime command deliveries = %d, want no duplicate delivery", commands)
	}
	reloaded, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RecoveryErrorCode != InteractionErrorCommandOutcomeUnknown {
		t.Fatalf("reloaded recovery error = %q, want outcome unknown", reloaded.RecoveryErrorCode)
	}
	if !reflect.DeepEqual(reloaded.AvailableActions, []string{"retry_with_new_attempt", "restart"}) {
		t.Fatalf("reloaded available actions = %#v, want explicit resend actions", reloaded.AvailableActions)
	}
}

func TestAssistantInteractionCommandFailureKeepsAttemptPendingWhenReplacementFails(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionCommandRecoveryFailureFactory{}
	svc.factory = factory
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "command-recovery-failure")
	if err != nil {
		t.Fatal(err)
	}
	input := InteractionMessageInput{Generation: snapshot.Generation, ClientMessageID: "message-unknown-delivery", Text: "继续"}
	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, input)
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorRestartRequired {
		t.Fatalf("command failure with unavailable replacement = %v, want restart_required", err)
	}
	attempt, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, input.ClientMessageID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != model.AssistantInteractionAttemptPending {
		t.Fatalf("attempt status after ambiguous delivery and failed replacement = %q, want pending", attempt.Status)
	}

	input.Generation = snapshot.Generation + 1
	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, input)
	if err == nil || NormalizeInteractionError(err).Code != "assistant_command_outcome_unknown" {
		t.Fatalf("retry with unresolved delivery = %v, want outcome unknown", err)
	}
	factory.mu.Lock()
	starts := factory.starts
	factory.mu.Unlock()
	if starts != 2 {
		t.Fatalf("runtime starts after unresolved retry = %d, want no additional replacement attempt", starts)
	}
}

func TestAssistantInteractionCommandRecoveryFailureIncludesSnapshot(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "command-recovery-snapshot")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a Server restart where the durable binding exists but the new
	// runtime cannot be started. The command error must still carry the
	// interaction snapshot so Studio can preserve the user's recovery state.
	restarted := NewAssistantInteractionService(svc.repo, assistantInteractionFailingFactory{}, svc.clock)
	err = restarted.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{Generation: snapshot.Generation, ClientMessageID: "message-recovery-snapshot", Text: "继续"})
	structured := NormalizeInteractionError(err)
	if structured == nil || structured.Code != InteractionErrorRestartRequired {
		t.Fatalf("runtime recovery failure = %v, want restart_required", err)
	}
	if structured.Snapshot == nil || structured.Snapshot.ID != snapshot.ID {
		t.Fatalf("runtime recovery failure snapshot = %#v, want interaction %q", structured.Snapshot, snapshot.ID)
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
	payload := json.RawMessage(completeInteractionPlan)
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
	createdAt := svc.clock()
	svc.clock = func() time.Time { return createdAt.Add(25 * time.Hour) }
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

func TestAssistantInteractionPreparedReadsAndRejectedCommandsDoNotStartRuntime(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Events(ctx, "user-1", prepared.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.AnswerQuestion(ctx, "user-1", prepared.ID, InteractionAnswerInput{Generation: prepared.Generation, AttemptID: "answer", CallID: "call"}); err == nil {
		t.Fatal("prepared answer accepted")
	}
	for _, input := range []InteractionMessageInput{
		{Generation: prepared.Generation, ClientMessageID: "empty", Text: " "},
		{Generation: prepared.Generation, ClientMessageID: "mode", Text: "hello", ModeID: interactionStringPtr("invalid")},
		{Generation: prepared.Generation, ClientMessageID: "project", Text: "hello", ProjectID: interactionStringPtr("missing")},
	} {
		if err := svc.SendMessage(ctx, "user-1", prepared.ID, input); err == nil {
			t.Fatalf("invalid first message accepted: %#v", input)
		}
	}
	snapshot, err := svc.Snapshot(ctx, "user-1", prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Lifecycle != InteractionLifecyclePrepared || snapshot.ProjectID != "" {
		t.Fatalf("rejected message changed snapshot: %#v", snapshot)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.starts != 0 || len(factory.commands) != 0 {
		t.Fatalf("prepared commands started runtime: %#v", factory.commands)
	}
}

func TestAssistantInteractionPreparedRejectsUnauthorizedContext(t *testing.T) {
	for _, scenario := range []struct{ name, projectOwner, taskOwner, taskProject string }{
		{"project_owner", "other-user", "user-1", "project"},
		{"task_owner", "user-1", "other-user", "project"},
		{"task_project", "user-1", "user-1", "other-project"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			svc, factory := newAssistantInteractionServiceTest(t)
			ctx := context.Background()
			if err := svc.repo.Projects().Create(ctx, &model.Project{ID: "project", UserID: scenario.projectOwner}); err != nil {
				t.Fatal(err)
			}
			if err := svc.repo.Tasks().Create(ctx, &model.Task{ID: "content", UserID: scenario.taskOwner, ProjectID: scenario.taskProject}); err != nil {
				t.Fatal(err)
			}
			prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
			if err != nil {
				t.Fatal(err)
			}
			err = svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello", ModeID: interactionStringPtr(AssistantModeReview), ProjectID: interactionStringPtr("project"), ContentID: interactionStringPtr("content")})
			if !errors.Is(err, ErrInteractiveSessionOwner) {
				t.Fatalf("unauthorized context = %v", err)
			}
			factory.mu.Lock()
			defer factory.mu.Unlock()
			if factory.starts != 0 {
				t.Fatalf("unauthorized context started %d runtimes", factory.starts)
			}
		})
	}
}

func TestAssistantInteractionPreparedRejectsUnfinishedWorkBeforeActivation(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	if err := svc.repo.Projects().Create(ctx, &model.Project{ID: "project", UserID: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.Tasks().Create(ctx, &model.Task{ID: "content", UserID: "user-1", ProjectID: "project", Status: model.TaskStatusPending}); err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello", ModeID: interactionStringPtr(AssistantModeReview), ProjectID: interactionStringPtr("project"), ContentID: interactionStringPtr("content")})
	if !errors.Is(err, ErrInteractiveSessionOwner) {
		t.Fatalf("unfinished work = %v", err)
	}
	snapshot, err := svc.Snapshot(ctx, "user-1", prepared.ID)
	if err != nil || snapshot.Lifecycle != InteractionLifecyclePrepared || snapshot.ProjectID != "" {
		t.Fatalf("rejected context changed prepared interaction: %#v, %v", snapshot, err)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.starts != 0 {
		t.Fatalf("rejected context started %d runtimes", factory.starts)
	}
}

func TestAssistantInteractionPreparedFirstMessageReplayDoesNotMutateContext(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	input := InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello", ModeID: interactionStringPtr(AssistantModeProfile)}
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, input); err != nil {
		t.Fatal(err)
	}
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, input); err != nil {
		t.Fatalf("first message replay: %v", err)
	}
	input.ModeID = interactionStringPtr(AssistantModeReview)
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, input); !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("changed replay = %v", err)
	}
	snapshot, err := svc.Snapshot(ctx, "user-1", prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ModeID != AssistantModeProfile {
		t.Fatalf("replay changed mode: %#v", snapshot)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.starts != 1 || len(factory.commands) != 2 {
		t.Fatalf("duplicate first message started %d runtimes / commands %#v", factory.starts, factory.commands)
	}
}

func TestAssistantInteractionPrepareDoesNotChangeActiveMode(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	active, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "active")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ID != active.ID || prepared.ModeID != active.ModeID {
		t.Fatalf("prepare mutated active interaction: %#v", prepared)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.commands) != 1 {
		t.Fatalf("prepare sent runtime commands: %#v", factory.commands)
	}
}

func TestAssistantInteractionPreparedStartFailureLeavesRecoveryLifecycle(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	svc.factory = assistantInteractionFailingFactory{}
	ctx := context.Background()
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello"})
	var interactionErr *InteractionError
	if !errors.As(err, &interactionErr) || interactionErr.Snapshot == nil {
		t.Fatalf("start failure = %v", err)
	}
	if interactionErr.Snapshot.Lifecycle != InteractionLifecycleReconnecting {
		t.Fatalf("start failure snapshot = %#v", interactionErr.Snapshot)
	}
	factory := &assistantInteractionTestFactory{}
	svc.factory = factory
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello"}); err != nil {
		t.Fatalf("retry first message after start failure: %v", err)
	}
	recovered, err := svc.Snapshot(ctx, "user-1", prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Lifecycle != InteractionLifecycleActive || recovered.RuntimeStatus != InteractionRuntimeRunning {
		t.Fatalf("recovered first message snapshot = %#v", recovered)
	}

}

func TestAssistantInteractionPreparedPendingReplayDoesNotApplyContext(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	modeID := interactionStringPtr(AssistantModeProfile)
	params := map[string]any{
		"contentBlocks":       []map[string]string{{"type": "text", "text": "hello"}},
		"interaction_context": map[string]any{"mode_id": modeID, "project_id": (*string)(nil), "content_id": (*string)(nil)},
	}
	if err := svc.repo.AssistantInteractions().CreateAttempt(ctx, &model.AssistantInteractionAttempt{ID: uuid.NewString(), InteractionID: prepared.ID, UserID: "user-1", AttemptID: "first", Operation: string(InteractionOperationMessage), Generation: prepared.Generation, Status: model.AssistantInteractionAttemptPending, PayloadDigest: interactionCommandDigest("session/prompt", "first", params)}); err != nil {
		t.Fatal(err)
	}
	err = svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello", ModeID: modeID})
	var structured *InteractionError
	if !errors.As(err, &structured) || structured.Code != InteractionErrorCommandOutcomeUnknown {
		t.Fatalf("pending replay = %v", err)
	}
	snapshot, err := svc.Snapshot(ctx, "user-1", prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ModeID != AssistantModeCreation || snapshot.Lifecycle != InteractionLifecyclePrepared {
		t.Fatalf("pending replay mutated prepared context: %#v", snapshot)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.starts != 0 {
		t.Fatal("pending replay started runtime")
	}
}

func TestAssistantInteractionRejectsChangedFirstMessageContextAfterActivation(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	ctx := context.Background()
	if err := svc.repo.Projects().Create(ctx, &model.Project{ID: "project", UserID: "user-1"}); err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
	if err != nil {
		t.Fatal(err)
	}
	first := InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello", ModeID: interactionStringPtr(AssistantModeProfile), ProjectID: interactionStringPtr("project")}
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, first); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name              string
		modeID, projectID *string
	}{
		{"mode", interactionStringPtr(AssistantModeReview), first.ProjectID},
		{"project", first.ModeID, interactionStringPtr("different-project")},
		{"content", first.ModeID, first.ProjectID},
	} {
		t.Run(change.name, func(t *testing.T) {
			input := first
			input.ClientMessageID = "new-" + change.name
			input.ModeID, input.ProjectID = change.modeID, change.projectID
			if change.name == "content" {
				input.ContentID = interactionStringPtr("different-content")
			}
			err := svc.SendMessage(ctx, "user-1", prepared.ID, input)
			var structured *InteractionError
			if !errors.As(err, &structured) || structured.Status != 409 || structured.Snapshot == nil {
				t.Fatalf("changed context error = %v", err)
			}
			if structured.Snapshot.ModeID != AssistantModeProfile || structured.Snapshot.ProjectID != "project" || structured.Snapshot.ContentID != "" {
				t.Fatalf("changed context error snapshot = %#v", structured.Snapshot)
			}
		})
	}
	factory.mu.Lock()
	if len(factory.commands) != 2 {
		t.Fatalf("changed context sent runtime prompts: %#v", factory.commands)
	}
	factory.mu.Unlock()
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, first); err != nil {
		t.Fatalf("accepted first-message replay = %v", err)
	}
	first.ClientMessageID = "new-exact-context"
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, first); err != nil {
		t.Fatalf("matching frozen context = %v", err)
	}
	if err := svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "legacy", Text: "continue"}); err != nil {
		t.Fatalf("legacy message without context = %v", err)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.starts != 1 || len(factory.commands) != 4 {
		t.Fatalf("runtime commands = %#v, starts = %d", factory.commands, factory.starts)
	}
}

type assistantInteractionBlockingFactory struct {
	method  string
	entered chan struct{}
	release chan struct{}
}

type assistantInteractionBlockingRuntime struct {
	factory *assistantInteractionBlockingFactory
	emit    func(string, any)
}

func (f *assistantInteractionBlockingFactory) Start(_ context.Context, _, _, _ string, emit func(string, any)) (Runtime, error) {
	return assistantInteractionBlockingRuntime{factory: f, emit: emit}, nil
}

func (r assistantInteractionBlockingRuntime) Command(_ context.Context, method string, _ any) error {
	if method == r.factory.method {
		r.emit("assistant.message", map[string]any{"text": "working"})
		close(r.factory.entered)
		<-r.factory.release
	}
	return nil
}

func (assistantInteractionBlockingRuntime) Close() error { return nil }

func TestAssistantInteractionEventsRemainReadableDuringFirstMessage(t *testing.T) {
	for _, method := range []string{"session/start", "session/prompt"} {
		t.Run(method, func(t *testing.T) {
			svc, _ := newAssistantInteractionServiceTest(t)
			ctx := context.Background()
			prepared, err := svc.CreateOrResume(ctx, "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, PrepareOnly: true}, "prepare")
			if err != nil {
				t.Fatal(err)
			}
			factory := &assistantInteractionBlockingFactory{method: method, entered: make(chan struct{}), release: make(chan struct{})}
			svc.factory = factory
			sendDone := make(chan error, 1)
			go func() {
				sendDone <- svc.SendMessage(ctx, "user-1", prepared.ID, InteractionMessageInput{Generation: prepared.Generation, ClientMessageID: "first", Text: "hello"})
			}()
			defer func() {
				close(factory.release)
				if err := <-sendDone; err != nil {
					t.Errorf("first message: %v", err)
				}
			}()
			select {
			case <-factory.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("first message did not reach blocked runtime command")
			}
			eventsDone := make(chan error, 1)
			go func() {
				events, err := svc.Events(ctx, "user-1", prepared.ID, 0)
				if err == nil && (len(events) != 1 || events[0].Type != "assistant.message") {
					err = fmt.Errorf("events = %#v, want emitted runtime progress", events)
				}
				eventsDone <- err
			}()
			select {
			case err := <-eventsDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("event polling blocked until runtime command completed")
			}
		})
	}
}
