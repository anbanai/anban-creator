package service

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type interactiveTestRuntime struct {
	commandErr error
	closeCh    chan struct{}
	closeOnce  sync.Once
	closeCheck func()
}

func (r *interactiveTestRuntime) Command(context.Context, string, any) error { return r.commandErr }
func (r *interactiveTestRuntime) Close() error {
	if r.closeCheck != nil {
		r.closeCheck()
	}
	if r.closeCh != nil {
		r.closeOnce.Do(func() { close(r.closeCh) })
	}
	return nil
}

type interactiveTestRuntimeFactory struct{}

func (interactiveTestRuntimeFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	return &interactiveTestRuntime{}, nil
}

type interactiveFailingRuntimeFactory struct{}

func (interactiveFailingRuntimeFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	return nil, errors.New("bridge failed to start")
}

type retryInteractiveMemory struct {
	failWrites bool
}

func (m *retryInteractiveMemory) ReadProject(context.Context, string) (projectmemory.ProjectView, error) {
	return projectmemory.ProjectView{}, nil
}

func (m *retryInteractiveMemory) EnsureProject(context.Context, string) error { return nil }

func (m *retryInteractiveMemory) WriteMarkdownFile(context.Context, string, string, string) error {
	if m.failWrites {
		return errors.New("memory projection unavailable")
	}
	return nil
}

func (m *retryInteractiveMemory) AppendMarkdownFileIfUnchanged(context.Context, string, string, string, string) error {
	return nil
}

func newInteractiveTestService(sess *InteractiveSession) *InteractiveService {
	return &InteractiveService{
		sessions:   map[string]*InteractiveSession{sess.ID: sess},
		maxHistory: 8,
	}
}

func newInteractiveTestSession() *InteractiveSession {
	return &InteractiveSession{
		ID:               "session-1",
		UserID:           "user-1",
		ExpiresAt:        time.Now().Add(time.Minute),
		watchers:         map[*interactiveWatcher]struct{}{},
		pendingQuestions: map[string]InteractiveQuestion{},
	}
}

func TestInteractiveCloseSessionWithShortHistory(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)

	history, events, closeFn, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("initial history length = %d, want 0", len(history))
	}

	svc.closeSession(sess.ID, true)

	select {
	case event := <-events:
		if event.Type != "session.expired" {
			t.Fatalf("event type = %q, want session.expired", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for expiry event")
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("events channel remained open after close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for events channel close")
	}
	closeFn()
}

func TestInteractiveCreateDraftRejectsMemoryAppend(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	progress := InteractiveProgress{
		"identity": "collected", "style": "collected", "audience": "collected",
		"platforms": "collected", "preferences": "collected", "memory": "collected",
	}
	err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project:      InteractiveProject{Name: "Create project", Platform: model.PlatformSeednote},
		Profile:      model.NewProjectProfile(),
		Progress:     progress,
		MemoryAppend: "should not be written before a project exists",
		Ready:        true,
	})
	if !errors.Is(err, ErrInteractiveDraftInvalid) {
		t.Fatalf("StageDraftForSession() error = %v, want ErrInteractiveDraftInvalid", err)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.Draft.Revision != 0 || sess.committing {
		t.Fatalf("rejected create draft mutated session: revision=%d committing=%v", sess.Draft.Revision, sess.committing)
	}
}

func TestInteractiveShutdownClosesLiveRuntimeBeforeCancellingIt(t *testing.T) {
	sess := newInteractiveTestSession()
	cancelled := false
	runtime := &interactiveTestRuntime{
		closeCh: make(chan struct{}),
		closeCheck: func() {
			if cancelled {
				t.Error("runtime context was cancelled before graceful shutdown")
			}
		},
	}
	sess.runtime = runtime
	sess.cancel = func() { cancelled = true }
	svc := newInteractiveTestService(sess)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-runtime.closeCh:
	default:
		t.Fatal("Shutdown() did not close the runtime")
	}
	if !cancelled {
		t.Fatal("Shutdown() did not cancel the runtime context after close")
	}
	if !sess.Closed {
		t.Fatal("Shutdown() left the session open")
	}
}

func TestInteractiveCreateSessionHonorsAdminOnlyForRuntimeBackedService(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "interactive-admin.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	userID := "interactive-member"
	if err := repository.New(db).Users().Create(context.Background(), &model.User{ID: userID, Email: "member@example.com", IsAdmin: false}); err != nil {
		t.Fatal(err)
	}
	svc := NewInteractiveServiceWithRuntime(repository.New(db), interactiveTestRuntimeFactory{})
	svc.SetAdminOnly(true)
	if _, err := svc.CreateSession(context.Background(), userID, "project-creator", "create-project"); !errors.Is(err, ErrInteractiveAgent) {
		t.Fatalf("CreateSession() error = %v, want ErrInteractiveAgent", err)
	}
}

func TestInteractiveCreateSessionAppliesPerUserLimit(t *testing.T) {
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, interactiveTestRuntimeFactory{}, []InteractiveAgent{{
		ID: "project-creator", DisplayName: "Project Creator", Available: true, Intents: []string{"create-project"},
	}})
	svc.SetEnabled(true)
	first, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatalf("first CreateSession() error = %v", err)
	}
	defer func() { _ = svc.Close("user-1", first.ID) }()
	second, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatalf("second CreateSession() error = %v", err)
	}
	defer func() { _ = svc.Close("user-1", second.ID) }()
	if _, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project"); !errors.Is(err, ErrInteractiveBusy) {
		t.Fatalf("third CreateSession() error = %v, want ErrInteractiveBusy", err)
	}
}

func TestInteractiveRuntimeStartFailureIsReplayableOverSSE(t *testing.T) {
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, interactiveFailingRuntimeFactory{}, []InteractiveAgent{{
		ID: "project-creator", DisplayName: "Project Creator", Available: true, Intents: []string{"create-project"},
	}})
	svc.SetEnabled(true)
	svc.tombstoneTTL = time.Minute
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, statusErr := svc.SessionStatus("user-1", sess.ID)
		if errors.Is(statusErr, ErrInteractiveSessionClosed) {
			break
		}
		if statusErr != nil {
			t.Fatalf("SessionStatus() error = %v", statusErr)
		}
		if status == "error" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	history, events, closeEvents, err := svc.Events("user-1", sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v, want replayable failed session", err)
	}
	defer closeEvents()
	if len(history) != 1 || history[0].Type != "session.error" {
		t.Fatalf("replayed history = %#v, want one session.error event", history)
	}
	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("failed session event stream unexpectedly yielded %q", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("failed session event stream did not close")
	}
}

func TestInteractiveSessionStateIncludesFastRuntimeFailure(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.Status = "error"
	sess.Closed = true
	svc := newInteractiveTestService(sess)
	status, err := svc.SessionState(sess.UserID, sess.ID)
	if err != nil {
		t.Fatalf("SessionState() error = %v", err)
	}
	if status != "error" {
		t.Fatalf("SessionState() = %q, want error", status)
	}
}

func TestInteractiveClosedSessionTombstoneIsCollected(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	svc.tombstoneTTL = 10 * time.Millisecond
	svc.closeSession(sess.ID, false)
	time.Sleep(30 * time.Millisecond)
	svc.mu.RLock()
	_, ok := svc.sessions[sess.ID]
	svc.mu.RUnlock()
	if ok {
		t.Fatal("closed interactive session remained after tombstone TTL")
	}
}

func TestInteractiveEventsRejectsClosedSession(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.Closed = true
	svc := newInteractiveTestService(sess)

	_, _, _, err := svc.Events(sess.UserID, sess.ID, 0)
	if !errors.Is(err, ErrInteractiveSessionClosed) {
		t.Fatalf("Events() error = %v, want ErrInteractiveSessionClosed", err)
	}
}

func TestInteractiveEventsRejectsHistoryGap(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	for i := 0; i < 4; i++ {
		svc.emit(sess, "session.status", map[string]string{"status": "running"})
	}
	svc.maxHistory = 2
	// Re-trim after lowering the test history cap to model an already compacted log.
	sess.mu.Lock()
	sess.events = trimInteractiveEvents(sess.events, svc.maxHistory)
	sess.mu.Unlock()
	if _, _, _, err := svc.Events(sess.UserID, sess.ID, 1); !errors.Is(err, ErrInteractiveEventGap) {
		t.Fatalf("Events() error = %v, want ErrInteractiveEventGap", err)
	}
}

func TestInteractiveEventsAllowsFreshConnectionAfterHistoryCompaction(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	for i := 0; i < 4; i++ {
		svc.emit(sess, "session.status", map[string]string{"status": "running"})
	}
	svc.maxHistory = 2
	sess.mu.Lock()
	sess.events = trimInteractiveEvents(sess.events, svc.maxHistory)
	sess.mu.Unlock()

	history, events, closeFn, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v, want fresh connection to receive retained history", err)
	}
	closeFn()
	if len(history) != 2 || history[0].ID != 3 || history[1].ID != 4 {
		t.Fatalf("retained history = %#v, want events 3 and 4", history)
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("fresh connection channel unexpectedly yielded an event")
		}
	case <-time.After(time.Second):
		t.Fatal("fresh connection channel did not close after cleanup")
	}
}

func TestInteractiveAnswerDoesNotEmitOnRuntimeFailure(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.runtime = &interactiveTestRuntime{commandErr: errors.New("runtime unavailable")}
	sess.pendingCall = "call-1"
	sess.pendingQuestions = map[string]InteractiveQuestion{
		"q-1": {ID: "q-1", Question: "Choose", Options: []InteractiveOption{{Label: "one"}}},
	}
	svc := newInteractiveTestService(sess)
	_, events, closeFn, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer closeFn()

	err = svc.Answer(context.Background(), sess.UserID, sess.ID, "call-1", []InteractiveAnswer{{ID: "q-1", Selected: []string{"one"}}})
	if err == nil {
		t.Fatal("Answer() error = nil, want runtime error")
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected event after failed answer: %s", event.Type)
	case <-time.After(20 * time.Millisecond):
	}
	if sess.pendingCall != "call-1" {
		t.Fatalf("pending call = %q, want call-1 after failed answer", sess.pendingCall)
	}
}

func TestInteractiveAnswerAllowsCustomTextWithMultiSelect(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.runtime = &interactiveTestRuntime{}
	sess.pendingCall = "call-1"
	sess.pendingQuestions = map[string]InteractiveQuestion{
		"q-1": {ID: "q-1", Question: "偏好", MultiSelect: true, Options: []InteractiveOption{{Label: "图文"}}},
	}
	svc := newInteractiveTestService(sess)
	if err := svc.Answer(context.Background(), sess.UserID, sess.ID, "call-1", []InteractiveAnswer{{ID: "q-1", Selected: []string{"图文"}, Custom: "希望保留轻量叙事"}}); err != nil {
		t.Fatalf("Answer() error = %v, want multi-select custom text accepted", err)
	}
	if sess.pendingCall != "" {
		t.Fatalf("pending call = %q, want cleared after accepted answer", sess.pendingCall)
	}
}

func TestInteractiveAnswerAllowsExplicitlySkippedQuestion(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.runtime = &interactiveTestRuntime{}
	sess.pendingCall = "call-1"
	sess.pendingQuestions = map[string]InteractiveQuestion{
		"q-1": {ID: "q-1", Question: "可选偏好", Options: []InteractiveOption{{Label: "图文"}}},
	}
	svc := newInteractiveTestService(sess)
	if err := svc.Answer(context.Background(), sess.UserID, sess.ID, "call-1", []InteractiveAnswer{{ID: "q-1"}}); err != nil {
		t.Fatalf("Answer() error = %v, want explicit skip accepted", err)
	}
}

func TestInteractiveRuntimeCanStageWhileUserTurnIsBusy(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.busy = true
	svc := newInteractiveTestService(sess)
	progress := InteractiveProgress{
		"identity": "collected", "style": "collected", "audience": "collected",
		"platforms": "collected", "preferences": "collected", "memory": "collected",
	}
	if err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project:  InteractiveProject{Name: "Project Creator", Platform: model.PlatformWechat},
		Profile:  model.NewProjectProfile(),
		Progress: progress,
		Ready:    false,
	}); err != nil {
		t.Fatalf("StageDraftForSession() while busy error = %v", err)
	}
	if sess.Draft.Revision != 1 {
		t.Fatalf("draft revision = %d, want 1", sess.Draft.Revision)
	}
}

func TestInteractiveCommitAcceptsTokenIssuedByReadyDraft(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "interactive.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}, &model.InteractiveCommitReceipt{}); err != nil {
		t.Fatal(err)
	}
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	svc.repo = repository.New(db)
	progress := InteractiveProgress{
		"identity": "collected", "style": "collected", "audience": "collected",
		"platforms": "collected", "preferences": "collected", "memory": "collected",
	}
	if err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project: InteractiveProject{Name: "Project Creator test", Platform: model.PlatformSeednote},
		Profile: model.NewProjectProfile(), Progress: progress, Ready: true,
	}); err != nil {
		t.Fatalf("StageDraftForSession() error = %v", err)
	}
	history, events, closeEvents, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer closeEvents()
	var replayedToken string
	for _, event := range history {
		if event.Type == "draft.ready" {
			if draft, ok := event.Data.(InteractiveDraft); ok {
				replayedToken = draft.ConfirmationToken
			}
		}
	}
	if replayedToken == "" {
		t.Fatal("late subscriber could not recover the ready draft confirmation token")
	}
	var token string
	if err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project: InteractiveProject{Name: "Project Creator test 2", Platform: model.PlatformSeednote},
		Profile: model.NewProjectProfile(), Progress: progress, Ready: true,
	}); err != nil {
		t.Fatalf("StageDraftForSession() second error = %v", err)
	}
	select {
	case event := <-events:
		if event.Type != "draft.updated" {
			t.Fatalf("live event type = %q, want draft.updated", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live draft event")
	}
	select {
	case event := <-events:
		if event.Type != "draft.ready" {
			t.Fatalf("live ready event type = %q, want draft.ready", event.Type)
		}
		if draft, ok := event.Data.(InteractiveDraft); ok {
			token = draft.ConfirmationToken
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live ready event")
	}

	// Use the second live token for the commit below; the first token was also
	// available from the owner-authenticated replay history above.
	if token == "" {
		t.Fatal("ready draft did not issue a confirmation token")
	}
	result, err := svc.Commit(context.Background(), sess.UserID, sess.ID, InteractiveCommitRequest{
		ConfirmationToken: token,
		IdempotencyKey:    "interactive-test-commit",
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if result.Project == nil || result.Project.Name != "Project Creator test 2" {
		t.Fatalf("Commit() result = %#v", result)
	}
	if result.Project.ImageRatio != model.DefaultImageRatio(model.PlatformSeednote) {
		t.Fatalf("interactive project image ratio = %q, want regular project default %q", result.Project.ImageRatio, model.DefaultImageRatio(model.PlatformSeednote))
	}
	if !reflect.DeepEqual(result.Project.AgentIDs, model.DefaultProjectAgentIDs(model.PlatformSeednote)) {
		t.Fatalf("interactive project agent ids = %#v, want regular project defaults %#v", result.Project.AgentIDs, model.DefaultProjectAgentIDs(model.PlatformSeednote))
	}
	if state, stateErr := svc.repo.ProjectProfileStates().FindByProjectID(context.Background(), result.Project.ID); stateErr != nil || state.Status != model.ProfileInitializationReady || state.Revision != 1 {
		t.Fatalf("interactive profile state = %#v, err = %v, want ready revision 1", state, stateErr)
	}
}

func TestInteractiveCommitMatchesProjectCreateDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "interactive-parity.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}, &model.InteractiveCommitReceipt{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	if err := db.AutoMigrate(&model.InteractiveCommitReceipt{}); err != nil {
		t.Fatal(err)
	}
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	svc.repo = repo
	progress := InteractiveProgress{
		"identity": "collected", "style": "collected", "audience": "collected",
		"platforms": "collected", "preferences": "collected", "memory": "collected",
	}
	if err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project: InteractiveProject{Name: "Parity project", Platform: model.PlatformMoments, VisualStyle: "minimal editorial"},
		Profile: model.NewProjectProfile(), Progress: progress, Ready: true,
	}); err != nil {
		t.Fatalf("StageDraftForSession() error = %v", err)
	}
	history, _, closeEvents, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer closeEvents()
	var token string
	for _, event := range history {
		if event.Type == "draft.ready" {
			if draft, ok := event.Data.(InteractiveDraft); ok {
				token = draft.ConfirmationToken
			}
		}
	}
	if token == "" {
		t.Fatal("ready draft did not issue a confirmation token")
	}
	result, err := svc.Commit(context.Background(), sess.UserID, sess.ID, InteractiveCommitRequest{ConfirmationToken: token, IdempotencyKey: "interactive-parity"})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Run the same input through the regular preparation path. Interactive
	// creation intentionally changes profile lifecycle state, but its project
	// identity, capability defaults, ratio defaults, and visual-style source
	// must remain identical to the ordinary project API.
	expected := &model.Project{
		Name: "Parity project", Platform: model.PlatformMoments,
		VisualStyle: "minimal editorial", Profile: datatypes.NewJSONType(model.NewProjectProfile()),
	}
	if err := NewProjectService(repo, nil).prepareCreate(sess.UserID, expected); err != nil {
		t.Fatalf("regular project preparation error = %v", err)
	}
	if result.Project.UserID != expected.UserID || result.Project.Status != expected.Status ||
		result.Project.ImageRatio != expected.ImageRatio || result.Project.VisualStyleSource != expected.VisualStyleSource ||
		!reflect.DeepEqual(result.Project.AgentIDs, expected.AgentIDs) {
		t.Fatalf("interactive project defaults = %#v, regular defaults = %#v", result.Project, expected)
	}
}

func TestInteractiveCommitRejectsMemoryAppendForNewProjectWithoutStickingBusy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "interactive-memory-append.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	if err := db.AutoMigrate(&model.InteractiveCommitReceipt{}); err != nil {
		t.Fatal(err)
	}
	sess := newInteractiveTestSession()
	sess.Draft.Ready = true
	sess.Draft.MemoryAppend = "should only update an existing project"
	sess.confirmationTokenHash = confirmationHash("token")
	svc := newInteractiveTestService(sess)
	svc.repo = repo
	req := InteractiveCommitRequest{ConfirmationToken: "token", IdempotencyKey: "memory-append-new-project"}
	if _, err := svc.Commit(context.Background(), sess.UserID, sess.ID, req); !errors.Is(err, ErrInteractiveDraftInvalid) {
		t.Fatalf("first Commit() error = %v, want ErrInteractiveDraftInvalid", err)
	}
	sess.mu.Lock()
	stuck := sess.committing
	sess.mu.Unlock()
	if stuck {
		t.Fatal("session remained in committing state after rejected draft")
	}
	if _, err := svc.Commit(context.Background(), sess.UserID, sess.ID, req); !errors.Is(err, ErrInteractiveDraftInvalid) {
		t.Fatalf("retry Commit() error = %v, want ErrInteractiveDraftInvalid", err)
	}
}

func TestInteractiveCommitReplayRetriesFailedMemoryProjection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "interactive-replay.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}, &model.InteractiveCommitReceipt{}); err != nil {
		t.Fatal(err)
	}
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	svc.repo = repository.New(db)
	memory := &retryInteractiveMemory{failWrites: true}
	svc.SetProjectMemory(memory)
	progress := InteractiveProgress{
		"identity": "collected", "style": "collected", "audience": "collected",
		"platforms": "collected", "preferences": "collected", "memory": "collected",
	}
	if err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project: InteractiveProject{Name: "Replay project", Platform: model.PlatformSeednote},
		Profile: model.NewProjectProfile(), Progress: progress, Ready: true,
	}); err != nil {
		t.Fatalf("StageDraftForSession() error = %v", err)
	}
	history, _, closeEvents, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer closeEvents()
	var token string
	for _, event := range history {
		if event.Type == "draft.ready" {
			if draft, ok := event.Data.(InteractiveDraft); ok {
				token = draft.ConfirmationToken
			}
		}
	}
	if token == "" {
		t.Fatal("ready draft did not issue a confirmation token")
	}
	req := InteractiveCommitRequest{ConfirmationToken: token, IdempotencyKey: "interactive-replay"}
	if _, err := svc.Commit(context.Background(), sess.UserID, sess.ID, req); !errors.Is(err, ErrInteractivePersistence) {
		t.Fatalf("first Commit() error = %v, want projection persistence error", err)
	}
	receipt, err := svc.repo.InteractiveCommitReceipts().FindByUserAndKey(context.Background(), sess.UserID, req.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.MemoryAppliedAt != nil || receipt.MemoryError == "" {
		t.Fatalf("failed projection receipt = %+v, want unapplied error marker", receipt)
	}
	memory.failWrites = false
	result, err := svc.Commit(context.Background(), sess.UserID, sess.ID, req)
	if err != nil {
		t.Fatalf("replay Commit() error = %v", err)
	}
	if result.Project == nil {
		t.Fatal("replay returned no project")
	}
	receipt, err = svc.repo.InteractiveCommitReceipts().FindByUserAndKey(context.Background(), sess.UserID, req.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.MemoryAppliedAt == nil || receipt.MemoryError != "" {
		t.Fatalf("replayed receipt = %+v, want applied projection", receipt)
	}
}
