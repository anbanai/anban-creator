package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
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

func TestInteractiveEventsRejectsHistoryGapOnFirstConnection(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	for i := 0; i < 4; i++ {
		svc.emit(sess, "session.status", map[string]string{"status": "running"})
	}
	svc.maxHistory = 2
	sess.mu.Lock()
	sess.events = trimInteractiveEvents(sess.events, svc.maxHistory)
	sess.mu.Unlock()

	if _, _, _, err := svc.Events(sess.UserID, sess.ID, 0); !errors.Is(err, ErrInteractiveEventGap) {
		t.Fatalf("Events() error = %v, want ErrInteractiveEventGap", err)
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
}
