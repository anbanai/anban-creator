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

type assistantInteractionModeFactory struct {
	mu       sync.Mutex
	starts   int
	specs    []InteractiveRuntimeSpec
	commands []struct {
		method string
		params map[string]any
	}
}

type assistantInteractionModeRuntime struct {
	factory *assistantInteractionModeFactory
}

func (r assistantInteractionModeRuntime) Command(_ context.Context, method string, params any) error {
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

type assistantInteractionBlockingFactory struct {
	starts  atomic.Int32
	entered chan struct{}
	release <-chan struct{}
}

func (f *assistantInteractionBlockingFactory) Start(ctx context.Context, _, _, _ string, _ func(string, any)) (Runtime, error) {
	f.starts.Add(1)
	select {
	case f.entered <- struct{}{}:
	default:
	}
	select {
	case <-f.release:
		return assistantInteractionTestRuntime{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type assistantInteractionCommandFailureFactory struct {
	mu     sync.Mutex
	starts int
}

type assistantInteractionCloseTrackingRuntime struct {
	closeErr   error
	closeCalls int
}

func (assistantInteractionCloseTrackingRuntime) Command(context.Context, string, any) error {
	return nil
}

func (r *assistantInteractionCloseTrackingRuntime) Close() error {
	r.closeCalls++
	return r.closeErr
}

type assistantInteractionCloseTrackingFactory struct {
	closeErr error
	runtimes []*assistantInteractionCloseTrackingRuntime
}

func (f *assistantInteractionCloseTrackingFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	runtime := &assistantInteractionCloseTrackingRuntime{closeErr: f.closeErr}
	f.runtimes = append(f.runtimes, runtime)
	return runtime, nil
}

type assistantInteractionCASFailingRepository struct {
	repository.Repository
	interactions repository.AssistantInteractionRepository
}

func (r *assistantInteractionCASFailingRepository) AssistantInteractions() repository.AssistantInteractionRepository {
	return r.interactions
}

type assistantInteractionCASFailingRepositoryOperations struct {
	repository.AssistantInteractionRepository
	compareAndSwapErr error
}

func (r *assistantInteractionCASFailingRepositoryOperations) CompareAndSwapGeneration(ctx context.Context, id string, expected, next int64, at time.Time) (bool, error) {
	if r.compareAndSwapErr != nil {
		return false, r.compareAndSwapErr
	}
	return r.AssistantInteractionRepository.CompareAndSwapGeneration(ctx, id, expected, next, at)
}

type assistantInteractionCommandFailureRuntime struct{}

func (assistantInteractionCommandFailureRuntime) Command(_ context.Context, method string, _ any) error {
	if method == "session/start" || method == "interaction/capability-refresh" {
		return nil
	}
	return errors.New("runtime command failed")
}

func (assistantInteractionCommandFailureRuntime) Close() error { return nil }

func (f *assistantInteractionCommandFailureFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return assistantInteractionCommandFailureRuntime{}, nil
}

func newAssistantInteractionServiceTest(t *testing.T) (*AssistantInteractionService, *assistantInteractionTestFactory) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}, &model.Project{}, &model.ProjectProfileState{}, &model.ProjectProfileRevision{}, &model.Asset{}, &model.UserMediaAsset{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	factory := &assistantInteractionTestFactory{}
	clock := func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return NewAssistantInteractionService(repository.New(db), factory, clock), factory
}

func TestExpiredInteractionReplacesRuntimeAndRenewsCapability(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	now := svc.clock()
	svc.clock = func() time.Time { return now }
	first, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "expiry-create")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(25 * time.Hour)
	resumed, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{
		ModeID: AssistantModeCreation, ResumeInteractionID: first.ID,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Generation != first.Generation+1 {
		t.Fatalf("resumed generation = %d, want %d after runtime lease expiry", resumed.Generation, first.Generation+1)
	}
	interaction, err := svc.repo.AssistantInteractions().FindByIDForUser(context.Background(), "user-1", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !interaction.ExpiresAt.After(now) {
		t.Fatalf("resumed interaction expiry = %s, want an active lease after %s", interaction.ExpiresAt, now)
	}
	svc.runtimeMu.RLock()
	defer svc.runtimeMu.RUnlock()
	for _, capability := range svc.capabilities {
		if capability.InteractionID == first.ID && capability.Generation == resumed.Generation && !capability.ExpiresAt.After(now) {
			t.Fatalf("resumed runtime capability expired at %s", capability.ExpiresAt)
		}
	}
}

func TestConcurrentServersReserveRuntimeBeforeStartingIt(t *testing.T) {
	base := newAssistantInteractionCommitRecoveryServiceTest(t)
	snapshot, err := base.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "concurrent-runtime-start")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(closeRelease)
	firstFactory := &assistantInteractionBlockingFactory{entered: make(chan struct{}, 1), release: release}
	secondFactory := &assistantInteractionBlockingFactory{entered: make(chan struct{}, 1), release: release}
	firstServer := NewAssistantInteractionService(base.repo, firstFactory, base.clock)
	firstServer.SetServerInstanceID("runtime-server-a")
	secondServer := NewAssistantInteractionService(base.repo, secondFactory, base.clock)
	secondServer.SetServerInstanceID("runtime-server-b")

	firstDone := make(chan error, 1)
	go func() {
		_, ensureErr := firstServer.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
		firstDone <- ensureErr
	}()
	select {
	case <-firstFactory.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first server did not begin runtime startup")
	}
	reserved, err := base.repo.AssistantInteractions().FindCurrentRuntime(context.Background(), snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatalf("runtime binding while startup is blocked: %v", err)
	}
	if reserved.ServerInstance != "runtime-server-a" || reserved.Status != model.AssistantInteractionRuntimeStarting {
		t.Fatalf("runtime reservation while startup is blocked = %#v, want first owner in starting state", reserved)
	}

	secondDone := make(chan error, 1)
	go func() {
		_, ensureErr := secondServer.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
		secondDone <- ensureErr
	}()
	select {
	case secondErr := <-secondDone:
		if secondErr == nil || NormalizeInteractionError(secondErr).Code != InteractionErrorReconnecting {
			t.Fatalf("second server runtime error = %v, want reconnecting while startup lease is active", secondErr)
		}
	case <-secondFactory.entered:
		closeRelease()
		<-firstDone
		<-secondDone
		t.Fatal("second server started another runtime for the same generation")
	case <-time.After(2 * time.Second):
		closeRelease()
		<-firstDone
		<-secondDone
		t.Fatal("second server did not resolve the active startup lease")
	}
	closeRelease()
	if err := <-firstDone; err != nil {
		t.Fatalf("first server runtime startup: %v", err)
	}
	if got := secondFactory.starts.Load(); got != 0 {
		t.Fatalf("second server starts = %d, want 0", got)
	}
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
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "handshake-message", Text: "继续"}); err != nil {
		t.Fatal(err)
	}
	factory.mu.Lock()
	commands = append([]string(nil), factory.commands...)
	factory.mu.Unlock()
	if len(commands) != 3 || commands[1] != "interaction/capability-refresh" || commands[2] != "session/prompt" {
		t.Fatalf("runtime commands after message = %#v, want session/start, capability-refresh, then session/prompt", commands)
	}
}

func TestInteractionMessageCarriesOnlyVerifiedMediaLibraryReferencesToRuntime(t *testing.T) {
	base, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc := NewAssistantInteractionService(base.repo, factory, base.clock)
	assetID := uuid.NewString()
	mediaID := uuid.NewString()
	if err := svc.repo.Assets().Create(context.Background(), &model.Asset{ID: assetID, UserID: "user-1", Purpose: DirectUploadPurposeMediaLibrary, StorageKey: "media/tea.mp4", FileName: "茶品展示.mp4", ContentType: "video/mp4", Size: 4096, ETag: "tea"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.UserMediaAssets().Create(context.Background(), &model.UserMediaAsset{ID: mediaID, UserID: "user-1", AssetID: assetID, MediaType: "video", Status: model.UserMediaAssetStatusAvailable, Description: "展示茶叶冲泡过程"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "message-with-media")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{
		ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "media-message", Text: "继续使用刚才选中的素材",
		Attachments: []InteractionMessageAttachment{{AssetID: mediaID}},
	}); err != nil {
		t.Fatal(err)
	}
	commands := factory.commandSnapshot()
	var prompt map[string]any
	for _, command := range commands {
		if command.method == "session/prompt" {
			prompt = command.params
		}
	}
	if prompt == nil {
		t.Fatalf("runtime commands = %#v, want session/prompt", commands)
	}
	blocks, ok := prompt["contentBlocks"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("prompt contentBlocks = %#v, want text plus verified media references", prompt["contentBlocks"])
	}
	encoded, err := json.Marshal(blocks[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{mediaID, "茶品展示.mp4", "展示茶叶冲泡过程"} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("runtime media reference %s missing %q", encoded, expected)
		}
	}

	if err := svc.SendMessage(context.Background(), "user-2", snapshot.ID, InteractionMessageInput{
		ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "foreign-media-message", Text: "不要接受其他用户的素材",
		Attachments: []InteractionMessageAttachment{{AssetID: mediaID}},
	}); err == nil || NormalizeInteractionError(err).Code != InteractionErrorMessageNotAccepted {
		t.Fatalf("cross-user media reference error = %v, want safe message-not-accepted", err)
	}
}

func TestInteractionMessageAttemptConflictsWhenPayloadChanges(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "message-digest")
	if err != nil {
		t.Fatal(err)
	}
	input := InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-1", Text: "第一条需求"}
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, input); err != nil {
		t.Fatal(err)
	}
	input.Text = "另一条需求"
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, input); !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("changed message payload error = %v", err)
	}
}

func TestInteractionMessageRetryReplacesRuntimeForPendingAttemptAfterRestart(t *testing.T) {
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
	restartFactory := &assistantInteractionTestFactory{}
	restarted := NewAssistantInteractionService(svc.repo, restartFactory, svc.clock)
	err = restarted.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-1", Text: "需求"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
		t.Fatalf("pending message retry error = %v, want generation changed after runtime replacement", err)
	}
	current, err := svc.repo.AssistantInteractions().FindByIDForUser(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != snapshot.Generation+1 {
		t.Fatalf("interaction generation = %d, want %d", current.Generation, snapshot.Generation+1)
	}
	attempt, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "message-1")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != model.AssistantInteractionAttemptFailed || attempt.ErrorCode != InteractionErrorGenerationChanged {
		t.Fatalf("stale pending attempt = %#v, want failed generation-change receipt", attempt)
	}
	beforeReplay := len(restartFactory.commands)
	err = restarted.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: current.Generation, ClientMessageID: "message-1", Text: "需求"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
		t.Fatalf("same-attempt retry after generation change = %v, want generation changed", err)
	}
	if got := len(restartFactory.commands); got != beforeReplay {
		t.Fatalf("same-attempt retry sent a duplicate runtime command: before=%d after=%d", beforeReplay, got)
	}
	if err := restarted.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: current.Generation, ClientMessageID: "message-2", Text: "需求"}); err != nil {
		t.Fatalf("message after runtime replacement = %v", err)
	}
}

func TestInteractionMessagePendingAttemptIsNotReplayedWhenRuntimeStillExists(t *testing.T) {
	svc, factory := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "pending-message-same-runtime")
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"contentBlocks": []map[string]string{{"type": "text", "text": "需求"}}}
	digest := interactionCommandDigest("session/prompt", "message-pending", params)
	if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, AttemptID: "message-pending", Operation: string(InteractionOperationMessage), Generation: snapshot.Generation,
		PayloadDigest: digest, Status: model.AssistantInteractionAttemptPending, CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionAttemptRetention),
	}); err != nil {
		t.Fatal(err)
	}
	factory.mu.Lock()
	countPrompts := func() int {
		count := 0
		for _, command := range factory.commands {
			if command == "session/prompt" {
				count++
			}
		}
		return count
	}
	before := countPrompts()
	factory.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-pending", Text: "需求"})
		if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
			t.Fatalf("pending attempt retry %d = %v, want generation changed after fencing the uncertain runtime", attempt, err)
		}
	}
	factory.mu.Lock()
	after := countPrompts()
	factory.mu.Unlock()
	if after != before {
		t.Fatalf("pending attempt replayed prompt after fencing: before=%d after=%d", before, after)
	}
	attempt, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "message-pending")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != model.AssistantInteractionAttemptFailed || attempt.ErrorCode != InteractionErrorGenerationChanged {
		t.Fatalf("pending attempt receipt = %#v, want non-replayable generation-change receipt", attempt)
	}
	current, err := svc.repo.AssistantInteractions().FindByIDForUser(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != snapshot.Generation+1 {
		t.Fatalf("runtime generation = %d, want fenced generation %d", current.Generation, snapshot.Generation+1)
	}
}

func TestPendingAttemptDoesNotFenceRuntimeWhenGenerationCASFails(t *testing.T) {
	closeErr := errors.New("runtime close failed")
	casErr := errors.New("generation CAS unavailable")
	factory := &assistantInteractionCloseTrackingFactory{closeErr: closeErr}
	svc, _ := newAssistantInteractionServiceTest(t)
	svc.factory = factory
	var err error
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "cas-failure")
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"contentBlocks": []map[string]string{{"type": "text", "text": "需求"}}}
	digest := interactionCommandDigest("session/prompt", "message-cas-failure", params)
	if err := svc.repo.AssistantInteractions().CreateAttempt(context.Background(), &model.AssistantInteractionAttempt{
		ID: uuid.NewString(), InteractionID: snapshot.ID, UserID: "user-1", AttemptID: "message-cas-failure",
		Operation: string(InteractionOperationMessage), Generation: snapshot.Generation, PayloadDigest: digest,
		Status: model.AssistantInteractionAttemptPending, CreatedAt: svc.clock(), ExpiresAt: svc.clock().Add(InteractionAttemptRetention),
	}); err != nil {
		t.Fatal(err)
	}
	baseInteractions := svc.repo.AssistantInteractions()
	casRepository := &assistantInteractionCASFailingRepositoryOperations{AssistantInteractionRepository: baseInteractions, compareAndSwapErr: casErr}
	svc.repo = &assistantInteractionCASFailingRepository{Repository: svc.repo, interactions: casRepository}

	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{
		ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-cas-failure", Text: "需求",
	})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorReconnecting {
		t.Fatalf("CAS failure result = %v, want retryable reconnecting error", err)
	}
	if got := factory.runtimes[0].closeCalls; got != 0 {
		t.Fatalf("runtime close calls after failed CAS = %d, want 0", got)
	}
	if _, err := baseInteractions.FindCurrentRuntime(context.Background(), snapshot.ID, snapshot.Generation); err != nil {
		t.Fatalf("current runtime binding after failed CAS = %v, want current binding preserved", err)
	}
	attempt, err := baseInteractions.FindAttempt(context.Background(), snapshot.ID, "message-cas-failure")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != model.AssistantInteractionAttemptPending || attempt.ErrorCode != "" {
		t.Fatalf("attempt after failed CAS = %#v, want pending receipt", attempt)
	}

	casRepository.compareAndSwapErr = nil
	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{
		ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-cas-failure", Text: "需求",
	})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
		t.Fatalf("retry after CAS recovery = %v, want generation changed after fencing", err)
	}
	current, err := baseInteractions.FindByIDForUser(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != snapshot.Generation+1 {
		t.Fatalf("generation after CAS retry = %d, want %d", current.Generation, snapshot.Generation+1)
	}
	if got := factory.runtimes[0].closeCalls; got != 1 {
		t.Fatalf("old runtime close calls after successful CAS = %d, want 1", got)
	}
}

func TestServerRestartKeepsRuntimeBindingWhenGenerationCASFails(t *testing.T) {
	casErr := errors.New("generation CAS unavailable")
	original, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := original.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "restart-cas-failure")
	if err != nil {
		t.Fatal(err)
	}
	baseRepository := original.repo
	baseInteractions := baseRepository.AssistantInteractions()
	restarted := NewAssistantInteractionService(baseRepository, &assistantInteractionCloseTrackingFactory{}, original.clock)
	casRepository := &assistantInteractionCASFailingRepositoryOperations{AssistantInteractionRepository: baseInteractions, compareAndSwapErr: casErr}
	restarted.repo = &assistantInteractionCASFailingRepository{Repository: baseRepository, interactions: casRepository}

	_, err = restarted.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorReconnecting {
		t.Fatalf("runtime recovery after CAS failure = %v, want retryable reconnecting error", err)
	}
	current, err := baseInteractions.FindCurrentRuntime(context.Background(), snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatalf("runtime binding after CAS failure = %v, want binding preserved", err)
	}
	if current.Status != model.AssistantInteractionRuntimeRunning {
		t.Fatalf("runtime status after CAS failure = %q, want running", current.Status)
	}
	interaction, err := baseInteractions.FindByIDForUser(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if interaction.Generation != snapshot.Generation {
		t.Fatalf("generation after CAS failure = %d, want %d", interaction.Generation, snapshot.Generation)
	}
}

func TestInteractionAnswerRequiresAttemptIDForRetrySafety(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "answer-idempotency")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.AnswerQuestion(context.Background(), "user-1", snapshot.ID, InteractionAnswerInput{
		ModeID:     snapshot.ModeID,
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

func TestSendMessageRefreshesInteractionCapabilityBeforePrompt(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc.factory = factory
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "capability-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "capability-refresh-message", Text: "继续"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AnswerQuestion(context.Background(), "user-1", snapshot.ID, InteractionAnswerInput{
		ModeID:     snapshot.ModeID,
		Generation: snapshot.Generation, AttemptID: "capability-refresh-answer", CallID: "question-1",
		Answers: []InteractiveAnswer{{ID: "question-1", Selected: []string{"continue"}}},
	}); err != nil {
		t.Fatal(err)
	}
	commands := factory.commandSnapshot()
	want := []string{"session/start", "interaction/capability-refresh", "session/prompt", "interaction/capability-refresh", "session/answer"}
	if len(commands) != len(want) {
		t.Fatalf("runtime command count = %#v, want %v", commands, want)
	}
	for index, method := range want {
		if commands[index].method != method {
			t.Fatalf("runtime command %d = %q, want %q", index, commands[index].method, method)
		}
	}
	if commands[1].params["interaction_id"] != snapshot.ID || commands[1].params["generation"] != float64(snapshot.Generation) {
		t.Fatalf("capability refresh params = %#v, want current interaction and generation", commands[1].params)
	}
	if commands[3].params["interaction_id"] != snapshot.ID || commands[3].params["generation"] != float64(snapshot.Generation) {
		t.Fatalf("answer capability refresh params = %#v, want current interaction and generation", commands[3].params)
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

func TestAssistantInteractionRuntimeBindingUsesConfiguredServerInstance(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	svc.SetServerInstanceID("server-instance-42")
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "runtime-owner")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := svc.repo.AssistantInteractions().FindCurrentRuntime(context.Background(), snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ServerInstance != "server-instance-42" {
		t.Fatalf("runtime server instance = %q, want configured instance %q", runtime.ServerInstance, "server-instance-42")
	}
}

func TestAssistantInteractionDoesNotReplaceAnotherServerRuntime(t *testing.T) {
	owner, _ := newAssistantInteractionServiceTest(t)
	now := owner.clock()
	owner.clock = func() time.Time { return now }
	owner.SetServerInstanceID("server-a")
	snapshot, err := owner.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "multi-server-owner")
	if err != nil {
		t.Fatal(err)
	}

	foreignFactory := &assistantInteractionTestFactory{}
	otherServer := NewAssistantInteractionService(owner.repo, foreignFactory, owner.clock)
	otherServer.SetServerInstanceID("server-b")
	_, err = otherServer.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorReconnecting {
		t.Fatalf("foreign runtime ensure error = %v, want retryable reconnecting", err)
	}

	current, err := owner.repo.AssistantInteractions().FindByIDForUser(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != snapshot.Generation {
		t.Fatalf("foreign server changed generation to %d, want %d", current.Generation, snapshot.Generation)
	}
	binding, err := owner.repo.AssistantInteractions().FindCurrentRuntime(context.Background(), snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if binding.ServerInstance != "server-a" || foreignFactory.starts != 0 {
		t.Fatalf("binding owner = %q, foreign runtime starts = %d; want server-a and 0", binding.ServerInstance, foreignFactory.starts)
	}

	now = now.Add(model.AssistantInteractionRuntimeOwnerLease + time.Second)
	if err := owner.HeartbeatRuntimeBindings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := otherServer.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation); err == nil || NormalizeInteractionError(err).Code != InteractionErrorReconnecting {
		t.Fatalf("heartbeat-protected runtime error = %v, want reconnecting", err)
	}
	now = now.Add(model.AssistantInteractionRuntimeOwnerLease + time.Second)
	replaced, err := otherServer.EnsureRuntime(context.Background(), "user-1", snapshot.ID, snapshot.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Generation != snapshot.Generation+1 || foreignFactory.starts != 1 {
		t.Fatalf("expired runtime replacement = %#v, starts=%d, want next generation and one runtime", replaced, foreignFactory.starts)
	}
}

func TestAssistantInteractionCreateAttemptUsesAttemptRetention(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "short-lived-create")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, interactionCreateAttemptID("short-lived-create"))
	if err != nil {
		t.Fatal(err)
	}
	wantExpiry := svc.clock().Add(InteractionAttemptRetention)
	if !attempt.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("create attempt expiry = %s, want %s", attempt.ExpiresAt, wantExpiry)
	}
}

func TestAssistantInteractionCreateOrResumeReplaysIdempotencyAfterCompletion(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
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
	first, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-1"}, "stable-create")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation, ProjectID: "project-2"}, "stable-create")
	if !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("idempotency key reuse error = %v, want ErrInteractiveIdempotency", err)
	}
	current, findErr := svc.repo.AssistantInteractions().FindByIDForUser(context.Background(), "user-1", first.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if current.Lifecycle == model.AssistantInteractionAbandoned {
		t.Fatalf("idempotency conflict abandoned the original interaction %q", first.ID)
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
	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "late-message", Text: "late"}); err == nil || NormalizeInteractionError(err).Code != InteractionErrorRestartRequired {
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
	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-1", Text: "继续"})
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorReconnecting {
		t.Fatalf("command failure = %v, want reconnecting", err)
	}
	recovered, err := svc.Snapshot(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Generation != snapshot.Generation+1 {
		t.Fatalf("generation after command failure = %d, want %d", recovered.Generation, snapshot.Generation+1)
	}
	if svc.AcceptRuntimeEvent(snapshot.ID, snapshot.Generation, "assistant.message", map[string]any{"text": "late"}) {
		t.Fatal("late event from failed generation was accepted")
	}
	factory.mu.Lock()
	starts := factory.starts
	factory.mu.Unlock()
	if starts != 2 {
		t.Fatalf("runtime starts = %d, want replacement runtime", starts)
	}
}

func TestCommandFailureKeepsAttemptPendingUntilGenerationIsFenced(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionCommandFailureFactory{}
	svc.factory = factory
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "command-cas-failure")
	if err != nil {
		t.Fatal(err)
	}
	baseInteractions := svc.repo.AssistantInteractions()
	casRepository := &assistantInteractionCASFailingRepositoryOperations{
		AssistantInteractionRepository: baseInteractions,
		compareAndSwapErr:              errors.New("generation CAS unavailable"),
	}
	svc.repo = &assistantInteractionCASFailingRepository{Repository: svc.repo, interactions: casRepository}
	input := InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-command-cas-failure", Text: "继续"}

	if err := svc.SendMessage(context.Background(), "user-1", snapshot.ID, input); err == nil {
		t.Fatal("command failure with unavailable CAS unexpectedly succeeded")
	}
	attempt, err := baseInteractions.FindAttempt(context.Background(), snapshot.ID, input.ClientMessageID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != model.AssistantInteractionAttemptPending || attempt.ErrorCode != "" {
		t.Fatalf("attempt after unavailable fence = %#v, want an unresolved pending receipt", attempt)
	}

	casRepository.compareAndSwapErr = nil
	err = svc.SendMessage(context.Background(), "user-1", snapshot.ID, input)
	if err == nil || NormalizeInteractionError(err).Code != InteractionErrorGenerationChanged {
		t.Fatalf("retry after CAS recovery = %v, want generation changed after fencing", err)
	}
	current, err := baseInteractions.FindByIDForUser(context.Background(), "user-1", snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != snapshot.Generation+1 {
		t.Fatalf("generation after retry = %d, want %d", current.Generation, snapshot.Generation+1)
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
	err = restarted.SendMessage(context.Background(), "user-1", snapshot.ID, InteractionMessageInput{ModeID: snapshot.ModeID, Generation: snapshot.Generation, ClientMessageID: "message-recovery-snapshot", Text: "继续"})
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

func TestAssistantInteractionRuntimeRecoveryRestoresClientConversationContext(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeCreation}, "recover-context")
	if err != nil {
		t.Fatal(err)
	}

	contextJSON := json.RawMessage(`{"version":1,"interaction_id":"` + snapshot.ID + `","mode_id":"creation","messages":[{"role":"user","text":"我想做乌龙茶品鉴装"},{"role":"assistant","text":"我们继续补充规格。"}],"questions":[]}`)
	restartedFactory := &assistantInteractionModeFactory{}
	restarted := NewAssistantInteractionService(svc.repo, restartedFactory, svc.clock)
	recovered, err := restarted.Recover(context.Background(), "user-1", snapshot.ID, snapshot.Generation, contextJSON, false)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Generation != snapshot.Generation+1 {
		t.Fatalf("recovered generation = %d, want %d", recovered.Generation, snapshot.Generation+1)
	}
	if len(restartedFactory.specs) != 1 || !strings.Contains(restartedFactory.specs[0].InitialContext, "我想做乌龙茶品鉴装") {
		t.Fatalf("runtime initial context = %#v, want the preserved conversation", restartedFactory.specs)
	}
	events, err := svc.repo.AssistantInteractions().ListEventsAfter(context.Background(), snapshot.ID, recovered.Generation, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(string(event.Data), "我想做乌龙茶品鉴装") {
			t.Fatalf("recovery context leaked into durable event: %s", event.Data)
		}
	}
}

func TestAssistantInteractionCanContinueConversationWithReadyDraft(t *testing.T) {
	svc, _ := newAssistantInteractionServiceTest(t)
	factory := &assistantInteractionModeFactory{}
	svc.factory = factory
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "continue-ready-draft")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"ready":true,"project":{"name":"茶饮账号"}}`)
	digest := sha256.Sum256(payload)
	stage, err := svc.StageDraftForInteraction(context.Background(), "user-1", InteractionStageInput{
		InteractionID: snapshot.ID,
		Generation:    snapshot.Generation,
		AttemptID:     "continue-ready-draft-stage",
		ModeID:        AssistantModeProfile,
		Payload:       payload,
		PayloadDigest: hex.EncodeToString(digest[:]),
		Ready:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	contextJSON := json.RawMessage(`{"version":1,"interaction_id":"` + snapshot.ID + `","mode_id":"profile","messages":[{"role":"user","text":"之前确定了乌龙茶品鉴装的方向"}],"questions":[]}`)
	recovered, err := svc.Recover(context.Background(), "user-1", snapshot.ID, snapshot.Generation, contextJSON, true)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Generation <= snapshot.Generation || recovered.Lifecycle != model.AssistantInteractionActive || recovered.RuntimeStatus != model.AssistantInteractionRuntimeRunning {
		t.Fatalf("continued snapshot = %#v, want active runtime on a fresh generation", recovered)
	}
	if len(recovered.Drafts) != 1 || recovered.Drafts[0].ID != stage.DraftID || recovered.Drafts[0].Revision != stage.Revision {
		t.Fatalf("durable draft after continuation = %#v, want original receipt retained", recovered.Drafts)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.specs) < 2 || !strings.Contains(factory.specs[len(factory.specs)-1].InitialContext, "之前确定了乌龙茶品鉴装的方向") {
		t.Fatalf("replacement runtime context = %#v, want cached conversation", factory.specs)
	}
}

func TestAssistantInteractionRuntimeRecoveryStartsSharedRuntimeModeWithItsContext(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AssistantInteraction{}, &model.AssistantInteractionRuntime{}, &model.AssistantInteractionDraft{}, &model.AssistantInteractionEvent{}, &model.AssistantInteractionAttempt{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	factory := &assistantInteractionModeFactory{}
	svc := NewAssistantInteractionService(repository.New(db), factory, func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })
	snapshot, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{ModeID: AssistantModeProfile}, "shared-mode-context")
	if err != nil {
		t.Fatal(err)
	}
	creationContext := json.RawMessage(`{"version":1,"interaction_id":"` + snapshot.ID + `","mode_id":"creation","messages":[{"role":"user","text":"创作模式上下文"}],"questions":[]}`)
	if _, err := svc.CreateOrResume(context.Background(), "user-1", AssistantInteractionInput{
		ModeID: AssistantModeCreation, ResumeInteractionID: snapshot.ID, RecoveryContext: creationContext,
	}, ""); err != nil {
		t.Fatal(err)
	}
	commands := factory.commandSnapshot()
	var found bool
	for _, command := range commands {
		if command.method == "session/start" && strings.Contains(command.params["initialContext"].(string), "创作模式上下文") {
			found = true
		}
	}
	if !found {
		t.Fatalf("runtime commands = %#v, want creation mode session initialized with its context", commands)
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
