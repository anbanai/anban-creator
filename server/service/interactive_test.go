package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/rs/zerolog"
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

type interactiveRecordingRuntime struct {
	mu      sync.Mutex
	methods []string
}

type interactiveAsyncRuntime struct {
	mu       sync.Mutex
	emit     func(string, any)
	commands int
}

func (r *interactiveAsyncRuntime) Command(_ context.Context, method string, _ any) error {
	if method == "session/prompt" {
		r.mu.Lock()
		r.commands++
		r.mu.Unlock()
	}
	return nil
}
func (*interactiveAsyncRuntime) Close() error { return nil }

type interactiveBlockingRuntime struct {
	started chan struct{}
	release chan struct{}
}

func (r *interactiveBlockingRuntime) Command(_ context.Context, _ string, _ any) error {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-r.release
	return nil
}
func (*interactiveBlockingRuntime) Close() error { return nil }

func (r *interactiveRecordingRuntime) Command(_ context.Context, method string, _ any) error {
	r.mu.Lock()
	r.methods = append(r.methods, method)
	r.mu.Unlock()
	return nil
}
func (*interactiveRecordingRuntime) Close() error { return nil }

type interactiveRecordingRuntimeFactory struct{ runtime *interactiveRecordingRuntime }

func (f interactiveRecordingRuntimeFactory) Start(_ context.Context, _ string, _ string, _ string, _ func(string, any)) (Runtime, error) {
	return f.runtime, nil
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

type interactiveCommandFailingRuntimeFactory struct{}

func (interactiveCommandFailingRuntimeFactory) Start(context.Context, string, string, string, func(string, any)) (Runtime, error) {
	return &interactiveTestRuntime{commandErr: errors.New("bridge failed to initialize")}, nil
}

type interactiveShutdownAckRuntimeFactory struct {
	started chan struct{}
	release chan struct{}
}

func (f interactiveShutdownAckRuntimeFactory) Start(_ context.Context, _ string, _ string, _ string, emit func(string, any)) (Runtime, error) {
	return &interactiveShutdownAckRuntime{emit: emit, started: f.started, release: f.release}, nil
}

type interactiveShutdownAckRuntime struct {
	emit    func(string, any)
	started chan struct{}
	release chan struct{}
}

func (r *interactiveShutdownAckRuntime) Command(_ context.Context, method string, _ any) error {
	if method == "session/start" {
		close(r.started)
		<-r.release
		return errors.New("bridge failed to initialize")
	}
	return nil
}

func (r *interactiveShutdownAckRuntime) Close() error {
	r.emit("session.closed", map[string]any{})
	return nil
}

type interactiveExitAfterStartRuntimeFactory struct{}

func (interactiveExitAfterStartRuntimeFactory) Start(_ context.Context, _ string, _ string, _ string, emit func(string, any)) (Runtime, error) {
	return &interactiveExitAfterStartRuntime{emit: emit}, nil
}

type interactiveExitAfterStartRuntime struct {
	emit func(string, any)
}

func (r *interactiveExitAfterStartRuntime) Command(_ context.Context, method string, _ any) error {
	if method == "session/start" {
		r.emit("runtime.exit", map[string]any{"error": "bridge exited after session/start"})
	}
	return nil
}

func (interactiveExitAfterStartRuntime) Close() error { return nil }

type retryInteractiveMemory struct {
	failWrites bool
}

func (m *retryInteractiveMemory) ReadProject(context.Context, string) (projectmemory.ProjectView, error) {
	return projectmemory.ProjectView{}, nil
}

func (m *retryInteractiveMemory) EnsureProject(context.Context, string) error { return nil }

func (m *retryInteractiveMemory) CreateMarkdownFileIfMissing(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func (m *retryInteractiveMemory) WriteMarkdownFile(context.Context, string, string, string) error {
	if m.failWrites {
		return errors.New("memory projection unavailable")
	}
	return nil
}

func (m *retryInteractiveMemory) AppendMarkdownFileIfUnchanged(context.Context, string, string, string, string) error {
	return nil
}

type missingInteractiveMemory struct {
	ensured bool
}

func (m *missingInteractiveMemory) ReadProject(context.Context, string) (projectmemory.ProjectView, error) {
	if !m.ensured {
		return projectmemory.ProjectView{}, projectmemory.ErrProjectMemoryMissing
	}
	return projectmemory.ProjectView{}, nil
}

func (m *missingInteractiveMemory) EnsureProject(context.Context, string) error {
	m.ensured = true
	return nil
}

func (m *missingInteractiveMemory) CreateMarkdownFileIfMissing(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func (m *missingInteractiveMemory) WriteMarkdownFile(context.Context, string, string, string) error {
	return nil
}

func (m *missingInteractiveMemory) AppendMarkdownFileIfUnchanged(context.Context, string, string, string, string) error {
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

func TestInteractiveSessionTTLCanBeConfigured(t *testing.T) {
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, interactiveTestRuntimeFactory{}, []InteractiveAgent{{ID: "project-creator", Available: true, Intents: []string{"create-project"}}})
	svc.SetEnabled(true)
	svc.idleTimeout = 3 * time.Hour
	svc.SetSessionTTL(2 * time.Hour)
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	remaining := time.Until(sess.ExpiresAt)
	if remaining < 2*time.Hour-time.Second || remaining > 2*time.Hour+time.Second {
		t.Fatalf("session TTL = %v, want approximately 2h", remaining)
	}
	_ = svc.Close("user-1", sess.ID)
}

func TestInteractiveRuntimeHeartbeatRenewsIdleLease(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.CreatedAt = time.Now()
	sess.ExpiresAt = sess.CreatedAt.Add(30 * time.Minute)
	svc := newInteractiveTestService(sess)
	svc.ttl = 24 * time.Hour
	svc.idleTimeout = 30 * time.Minute
	defer svc.closeSession(sess.ID, false)

	before := sess.ExpiresAt
	time.Sleep(10 * time.Millisecond)
	svc.runtimeEvent(sess.ID, "session.heartbeat", nil)

	sess.mu.Lock()
	after := sess.ExpiresAt
	closed := sess.Closed
	sess.mu.Unlock()
	if !after.After(before) {
		t.Fatalf("heartbeat expiry = %s, want later than %s", after, before)
	}
	if closed {
		t.Fatal("heartbeat closed the session")
	}
	if len(sess.events) != 0 {
		t.Fatalf("heartbeat was exposed as an interactive event: %#v", sess.events)
	}
}

func TestInteractiveClientHeartbeatRenewsIdleLease(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.CreatedAt = time.Now()
	sess.ExpiresAt = sess.CreatedAt.Add(30 * time.Minute)
	svc := newInteractiveTestService(sess)
	svc.ttl = 24 * time.Hour
	svc.idleTimeout = 30 * time.Minute
	defer svc.closeSession(sess.ID, false)

	before := sess.ExpiresAt
	time.Sleep(10 * time.Millisecond)
	if err := svc.TouchSession(sess.UserID, sess.ID); err != nil {
		t.Fatalf("TouchSession() error = %v", err)
	}

	sess.mu.Lock()
	after := sess.ExpiresAt
	closed := sess.Closed
	sess.mu.Unlock()
	if !after.After(before) {
		t.Fatalf("client heartbeat expiry = %s, want later than %s", after, before)
	}
	if closed {
		t.Fatal("client heartbeat closed the session")
	}
}

func TestInteractiveClientHeartbeatCanExtendBeyondRuntimeLifetime(t *testing.T) {
	now := time.Now()
	sess := newInteractiveTestSession()
	sess.CreatedAt = now.Add(-time.Minute)
	sess.ExpiresAt = now.Add(30 * time.Millisecond)
	svc := newInteractiveTestService(sess)
	svc.ttl = 50 * time.Millisecond
	svc.idleTimeout = 100 * time.Millisecond
	sess.mu.Lock()
	svc.scheduleExpiryLocked(sess)
	sess.mu.Unlock()
	defer svc.closeSession(sess.ID, false)

	if err := svc.TouchSession(sess.UserID, sess.ID); err != nil {
		t.Fatalf("TouchSession() error = %v", err)
	}

	sess.mu.Lock()
	after := sess.ExpiresAt
	closed := sess.Closed
	sess.mu.Unlock()
	if !after.After(now.Add(50 * time.Millisecond)) {
		t.Fatalf("client heartbeat expiry = %s, want beyond runtime lifetime %s", after, now.Add(50*time.Millisecond))
	}
	if closed {
		t.Fatal("client heartbeat closed the session")
	}
}

func TestInteractiveClientHeartbeatValidatesSessionOwnership(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	svc.idleTimeout = time.Minute
	if err := svc.TouchSession("another-user", sess.ID); !errors.Is(err, ErrInteractiveSessionOwner) {
		t.Fatalf("TouchSession() error = %v, want ownership error", err)
	}
}

func TestInteractiveClientHeartbeatIntervalRespectsShortIdleLease(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	svc.idleTimeout = 3 * time.Second
	if interval := svc.ClientHeartbeatInterval(); interval != time.Second {
		t.Fatalf("ClientHeartbeatInterval() = %s, want 1s", interval)
	}
	svc.idleTimeout = time.Minute
	if interval := svc.ClientHeartbeatInterval(); interval != 15*time.Second {
		t.Fatalf("ClientHeartbeatInterval() = %s, want capped 15s", interval)
	}
}

func TestInteractiveRuntimeHeartbeatCannotExtendAbsoluteLifetime(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.CreatedAt = time.Now().Add(-23*time.Hour - 59*time.Minute)
	sess.ExpiresAt = sess.CreatedAt.Add(24 * time.Hour)
	svc := newInteractiveTestService(sess)
	svc.ttl = 24 * time.Hour
	svc.idleTimeout = 30 * time.Minute
	defer svc.closeSession(sess.ID, false)

	svc.runtimeEvent(sess.ID, "session.heartbeat", nil)

	sess.mu.Lock()
	after := sess.ExpiresAt
	sess.mu.Unlock()
	if limit := sess.CreatedAt.Add(svc.ttl); !after.Equal(limit) {
		t.Fatalf("heartbeat expiry = %s, want absolute deadline %s", after, limit)
	}
}

func TestInteractiveIdleLeaseResetsTimerAndExpiresAfterHeartbeatsStop(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.CreatedAt = time.Now()
	sess.ExpiresAt = sess.CreatedAt.Add(500 * time.Millisecond)
	svc := newInteractiveTestService(sess)
	svc.ttl = time.Second
	svc.idleTimeout = 500 * time.Millisecond
	sess.mu.Lock()
	svc.scheduleExpiryLocked(sess)
	sess.mu.Unlock()
	defer svc.closeSession(sess.ID, false)

	time.Sleep(200 * time.Millisecond)
	svc.runtimeEvent(sess.ID, "session.heartbeat", nil)
	time.Sleep(200 * time.Millisecond)
	sess.mu.Lock()
	closedBeforeIdleDeadline := sess.Closed
	sess.mu.Unlock()
	if closedBeforeIdleDeadline {
		t.Fatal("session closed at its original idle deadline after a heartbeat")
	}

	sess.mu.Lock()
	deadline := sess.ExpiresAt
	sess.mu.Unlock()
	time.Sleep(time.Until(deadline) + 100*time.Millisecond)
	sess.mu.Lock()
	closed := sess.Closed
	sess.mu.Unlock()
	if !closed {
		t.Fatal("session remained open after heartbeats stopped and idle lease expired")
	}
}

func TestInteractiveStaleExpiryCallbackCannotCloseRenewedLease(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.CreatedAt = time.Now()
	sess.ExpiresAt = sess.CreatedAt.Add(time.Minute)
	svc := newInteractiveTestService(sess)
	svc.ttl = time.Hour
	svc.idleTimeout = time.Minute

	sess.mu.Lock()
	svc.scheduleExpiryLocked(sess)
	staleGeneration := sess.expiryGeneration
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		svc.closeSessionAtDeadline(sess.ID, staleGeneration, true)
		close(done)
	}()
	<-started
	sess.ExpiresAt = time.Now().Add(2 * time.Minute)
	svc.scheduleExpiryLocked(sess)
	sess.mu.Unlock()
	defer svc.closeSession(sess.ID, false)

	<-done
	sess.mu.Lock()
	closed := sess.Closed
	sess.mu.Unlock()
	if closed {
		t.Fatal("stale expiry callback closed the renewed session")
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

func TestValidateStageRetainsAllProfileDimensionReasons(t *testing.T) {
	profile := model.NewProjectProfile()
	profile.Dimensions.Identity.Content = nil
	profile.Dimensions.Style.Sources = nil
	profile.Dimensions.Audience.Evidence = nil
	profile.Dimensions.Platforms.MissingFields = nil
	profile.Dimensions.Preferences.Content = nil
	profile.Dimensions.Memory.Content = nil

	err := validateStage(InteractiveStageInput{
		Project:  InteractiveProject{Name: "Project Creator", Platform: model.PlatformWechat},
		Profile:  profile,
		Progress: InteractiveProgress{"identity": "collected", "style": "collected", "audience": "collected", "platforms": "collected", "preferences": "collected", "memory": "collected"},
	})
	if err == nil || !strings.Contains(err.Error(), "interactive project draft is invalid") {
		t.Fatalf("validateStage() error = %v, want draft error", err)
	}
	for _, dimension := range model.ProfileDimensions() {
		if !strings.Contains(err.Error(), "profile dimension "+dimension) {
			t.Fatalf("validateStage() error = %v, missing %s detail", err, dimension)
		}
	}
}

func TestInteractiveProgressRejectsFieldsInStableOrder(t *testing.T) {
	for i := 0; i < 20; i++ {
		var progress InteractiveProgress
		err := json.Unmarshal([]byte(`{"zeta": "x", "alpha": []}`), &progress)
		if err == nil || !strings.Contains(err.Error(), `unknown progress field "alpha"`) {
			t.Fatalf("progress decode error = %v, want deterministic alpha field error", err)
		}
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

func TestInteractiveUpdateSessionBootstrapsMissingProjectMemory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "interactive-project-memory.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.FeedbackLease{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	project := &model.Project{ID: "project-1", UserID: "user-1", Name: "Existing project", Platform: model.PlatformWechat, Profile: datatypes.NewJSONType(model.NewProjectProfile())}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	memory := &missingInteractiveMemory{}
	svc := NewInteractiveServiceWithRuntime(repo, interactiveTestRuntimeFactory{})
	svc.SetEnabled(true)
	svc.SetProjectMemory(memory)
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "update-project-profile", "project-1")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	defer func() { _ = svc.Close("user-1", sess.ID) }()
	if !memory.ensured {
		t.Fatal("CreateSession() did not bootstrap missing project memory")
	}
}

func TestInteractiveInitialContextIgnoresStaleProfileProjection(t *testing.T) {
	project := &model.Project{ID: "project-1", Name: "当前项目", Platform: "wechat", Instructions: "数据库定位"}
	profile := model.NewProjectProfile()
	profile.Status = model.ProfileStatusConfirmed
	profile.Version = 8
	profile.Dimensions.Identity.Content["summary"] = "数据库最新画像"

	stale, err := ProfileDimensionMarkdownAtVersion("identity", model.ProfileDimension{Content: map[string]any{"summary": "NAS旧画像"}}, 7)
	if err != nil {
		t.Fatal(err)
	}
	contextJSON, _ := interactiveInitialContext(project, profile, projectmemory.ProjectView{Files: []projectmemory.FileView{
		{Path: "profile/identity.md", Content: stale},
	}})
	if strings.Contains(contextJSON, "NAS旧画像") || !strings.Contains(contextJSON, "数据库最新画像") {
		t.Fatalf("initial context did not prefer current database profile: %s", contextJSON)
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

func TestInteractiveCreateSessionIdempotencyReplaysOneSession(t *testing.T) {
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, interactiveTestRuntimeFactory{}, []InteractiveAgent{{
		ID: "project-creator", DisplayName: "Project Creator", Available: true, Intents: []string{"create-project"},
	}})
	svc.SetEnabled(true)
	first, err := svc.CreateSessionWithIdempotency(context.Background(), "user-1", "project-creator", "create-project", "create-key-1")
	if err != nil {
		t.Fatalf("first CreateSessionWithIdempotency() error = %v", err)
	}
	defer func() { _ = svc.Close("user-1", first.ID) }()
	second, err := svc.CreateSessionWithIdempotency(context.Background(), "user-1", "project-creator", "create-project", "create-key-1")
	if err != nil {
		t.Fatalf("replay CreateSessionWithIdempotency() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay session id = %q, want %q", second.ID, first.ID)
	}
	if _, err := svc.CreateSessionWithIdempotency(context.Background(), "user-1", "project-creator", "update-project-profile", "create-key-1", "project-1"); !errors.Is(err, ErrInteractiveIdempotency) {
		t.Fatalf("mismatched replay error = %v, want ErrInteractiveIdempotency", err)
	}
}

func TestInteractiveMessageRejectsConcurrentCommand(t *testing.T) {
	sess := newInteractiveTestSession()
	runtime := &interactiveBlockingRuntime{started: make(chan struct{}), release: make(chan struct{})}
	sess.runtime = runtime
	svc := newInteractiveTestService(sess)

	firstDone := make(chan error, 1)
	go func() { firstDone <- svc.Message(context.Background(), sess.UserID, sess.ID, "first") }()
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("first message did not reach runtime")
	}
	if err := svc.Message(context.Background(), sess.UserID, sess.ID, "second"); !errors.Is(err, ErrInteractiveBusy) {
		t.Fatalf("concurrent Message() error = %v, want ErrInteractiveBusy", err)
	}
	close(runtime.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Message() error = %v", err)
	}
}

func TestInteractiveMessageRejectsCommandDuringCommit(t *testing.T) {
	sess := newInteractiveTestSession()
	runtime := &interactiveRecordingRuntime{}
	sess.runtime = runtime
	sess.committing = true
	svc := newInteractiveTestService(sess)

	if err := svc.Message(context.Background(), sess.UserID, sess.ID, "continue editing"); !errors.Is(err, ErrInteractiveBusy) {
		t.Fatalf("Message() during commit error = %v, want ErrInteractiveBusy", err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.methods) != 0 {
		t.Fatalf("runtime commands during commit = %#v, want none", runtime.methods)
	}
}

func TestInteractiveMessageStaysBusyUntilRuntimeTurnEnds(t *testing.T) {
	sess := newInteractiveTestSession()
	runtime := &interactiveAsyncRuntime{}
	sess.runtime = runtime
	svc := newInteractiveTestService(sess)

	if err := svc.Message(context.Background(), sess.UserID, sess.ID, "first"); err != nil {
		t.Fatalf("first Message() error = %v", err)
	}
	if err := svc.Message(context.Background(), sess.UserID, sess.ID, "second"); !errors.Is(err, ErrInteractiveBusy) {
		t.Fatalf("second Message() error = %v, want ErrInteractiveBusy until turn/end", err)
	}

	svc.runtimeEvent(sess.ID, "turn.end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}})
	if err := svc.Message(context.Background(), sess.UserID, sess.ID, "second"); err != nil {
		t.Fatalf("second Message() after turn/end error = %v", err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.commands != 2 {
		t.Fatalf("prompt command count = %d, want 2", runtime.commands)
	}
}

func TestInteractiveAnswerStaysBusyUntilRuntimeTurnEnds(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.runtime = &interactiveAsyncRuntime{}
	sess.pendingCall = "call-1"
	sess.pendingQuestions = map[string]InteractiveQuestion{
		"q-1": {ID: "q-1", Question: "Choose", Options: []InteractiveOption{{Label: "one"}}},
	}
	svc := newInteractiveTestService(sess)

	if err := svc.Answer(context.Background(), sess.UserID, sess.ID, "call-1", []InteractiveAnswer{{ID: "q-1", Selected: []string{"one"}}}); err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	if err := svc.Message(context.Background(), sess.UserID, sess.ID, "follow-up"); !errors.Is(err, ErrInteractiveBusy) {
		t.Fatalf("Message() error = %v, want ErrInteractiveBusy until turn/end", err)
	}
	svc.runtimeEvent(sess.ID, "turn.end", map[string]any{"turn": 2, "reason": map[string]any{"kind": "completed"}})
	if err := svc.Message(context.Background(), sess.UserID, sess.ID, "follow-up"); err != nil {
		t.Fatalf("Message() after turn/end error = %v", err)
	}
}

func TestInteractiveAnswerRejectsQuestionDuringCommit(t *testing.T) {
	sess := newInteractiveTestSession()
	runtime := &interactiveRecordingRuntime{}
	sess.runtime = runtime
	sess.committing = true
	sess.pendingCall = "follow-up-question"
	sess.pendingQuestions = map[string]InteractiveQuestion{
		"q-1": {ID: "q-1", Question: "还需要确认一个细节吗？"},
	}
	svc := newInteractiveTestService(sess)

	if err := svc.Answer(context.Background(), sess.UserID, sess.ID, "follow-up-question", []InteractiveAnswer{{ID: "q-1"}}); !errors.Is(err, ErrInteractiveBusy) {
		t.Fatalf("Answer() during commit error = %v, want ErrInteractiveBusy", err)
	}
	if sess.pendingCall != "follow-up-question" {
		t.Fatalf("pending call after rejected answer = %q, want unchanged follow-up question", sess.pendingCall)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.methods) != 0 {
		t.Fatalf("runtime commands during commit = %#v, want none", runtime.methods)
	}
}

func TestInteractiveRuntimeReadReportsMalformedStdoutWithoutLeakingLine(t *testing.T) {
	runtime := &execRuntime{}
	var events []struct {
		typ  string
		data any
	}
	err := runtime.read(strings.NewReader("not-json\n{\"unexpected\":true}\n"), func(typ string, data any) {
		events = append(events, struct {
			typ  string
			data any
		}{typ: typ, data: data})
	})
	if err != nil {
		t.Fatalf("read() error = %v", err)
	}
	if len(events) != 1 || events[0].typ != "runtime.diagnostic" {
		t.Fatalf("events = %#v, want one runtime.diagnostic", events)
	}
	diagnostic, ok := events[0].data.(map[string]any)
	if !ok || diagnostic["malformed_stdout_lines"] != 1 || diagnostic["unknown_stdout_messages"] != 1 {
		t.Fatalf("diagnostic = %#v, want bounded counters", events[0].data)
	}
}

func TestInteractiveTurnErrorIsLoggedAndReplayable(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	svc.SetLogger(&logger)
	svc.SetServerInstanceID("server-1")

	svc.runtimeEvent(sess.ID, "session.error", map[string]any{
		"status":              "runtime_failed",
		"code":                "PROVIDER_AUTH",
		"message":             "provider rejected credentials",
		"provider_status":     401,
		"provider_request_id": "req-123",
	})

	history, _, closeFn, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	closeFn()
	if len(history) != 1 || history[0].Type != "session.error" {
		t.Fatalf("history = %#v, want replayable session.error", history)
	}
	for _, field := range []string{`"session_id":"session-1"`, `"server_instance":"server-1"`, `"error_code":"PROVIDER_AUTH"`, `"error_message":"provider rejected credentials"`, `"provider_status":401`, `"provider_request_id":"req-123"`} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("logs %q do not contain %s", logs.String(), field)
		}
	}
	if !strings.Contains(logs.String(), `"level":"error"`) {
		t.Fatalf("logs %q do not mark session.error as error", logs.String())
	}
}

func TestInteractiveRuntimeSendsSystemOpeningPromptOnce(t *testing.T) {
	runtime := &interactiveRecordingRuntime{}
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, interactiveRecordingRuntimeFactory{runtime: runtime}, []InteractiveAgent{{
		ID: "project-creator", DisplayName: "Project Creator", Available: true, Intents: []string{"create-project"},
	}})
	svc.SetEnabled(true)
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close("user-1", sess.ID) })
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		methods := append([]string(nil), runtime.methods...)
		runtime.mu.Unlock()
		if len(methods) >= 2 {
			if methods[0] != "session/start" || methods[1] != "session/prompt" {
				t.Fatalf("runtime methods = %#v, want session/start then session/prompt", methods)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.methods) != 2 {
		t.Fatalf("runtime methods = %#v, want one opening prompt", runtime.methods)
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

func TestInteractiveRuntimeInitializationFailureIncludesBridgeError(t *testing.T) {
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, interactiveCommandFailingRuntimeFactory{}, []InteractiveAgent{{
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
		if errors.Is(statusErr, ErrInteractiveSessionClosed) || status == "error" {
			break
		}
		if statusErr != nil {
			t.Fatalf("SessionStatus() error = %v", statusErr)
		}
		time.Sleep(5 * time.Millisecond)
	}

	history, _, closeEvents, err := svc.Events("user-1", sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer closeEvents()
	if len(history) != 1 {
		t.Fatalf("replayed history = %#v, want one session.error event", history)
	}
	data, ok := history[0].Data.(map[string]any)
	message, _ := data["message"].(string)
	if !ok || !strings.Contains(message, "bridge failed to initialize") {
		t.Fatalf("session.error data = %#v, want bridge diagnostic", history[0].Data)
	}
}

func TestInteractiveRuntimeInitializationFailureDoesNotExposeShutdownAck(t *testing.T) {
	factory := interactiveShutdownAckRuntimeFactory{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, factory, []InteractiveAgent{{
		ID: "project-creator", DisplayName: "Project Creator", Available: true, Intents: []string{"create-project"},
	}})
	svc.SetEnabled(true)
	svc.tombstoneTTL = time.Minute
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(factory.release) }) }
	defer release()
	select {
	case <-factory.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session/start")
	}

	history, events, closeEvents, err := svc.Events("user-1", sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer closeEvents()
	if len(history) != 0 {
		t.Fatalf("initial event history = %#v, want no events before session/start fails", history)
	}

	release()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event stream closed before reporting the startup failure")
		}
		if event.Type != "session.error" {
			t.Fatalf("first event = %q, want session.error before the Bridge shutdown acknowledgement", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for startup failure")
	}
	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("unexpected event after startup failure: %q", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("event stream remained open after startup failure")
	}
}

func TestInteractiveRuntimeExitAfterStartResponseIsReplayable(t *testing.T) {
	svc := NewInteractiveServiceWithRuntimeAndAgents(nil, interactiveExitAfterStartRuntimeFactory{}, []InteractiveAgent{{
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
		status, statusErr := svc.SessionState("user-1", sess.ID)
		if statusErr != nil {
			t.Fatalf("SessionState() error = %v", statusErr)
		}
		if status == "error" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	history, _, closeEvents, err := svc.Events("user-1", sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v, want replayable failed session", err)
	}
	defer closeEvents()
	if len(history) != 1 || history[0].Type != "session.error" {
		t.Fatalf("replayed history = %#v, want one session.error event", history)
	}
	data, ok := history[0].Data.(map[string]any)
	message, _ := data["message"].(string)
	if !ok || !strings.Contains(message, "bridge exited after session/start") {
		t.Fatalf("session.error data = %#v, want bridge exit diagnostic", history[0].Data)
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
	if len(history) != 3 || history[0].ID != 3 || history[1].ID != 4 || history[2].Type != "session.snapshot" {
		t.Fatalf("retained history = %#v, want events 3 and 4 followed by a session snapshot", history)
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

func TestInteractiveSnapshotRecoversReadyDraftAfterHistoryCompaction(t *testing.T) {
	sess := newInteractiveTestSession()
	svc := newInteractiveTestService(sess)
	progress := InteractiveProgress{
		"identity": "collected", "style": "collected", "audience": "collected",
		"platforms": "collected", "preferences": "collected", "memory": "collected",
	}
	if err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project: InteractiveProject{Name: "Recoverable project", Platform: model.PlatformWechat},
		Profile: model.NewProjectProfile(), Progress: progress, Ready: true,
	}); err != nil {
		t.Fatalf("StageDraftForSession() error = %v", err)
	}

	var readyEventID uint64
	var issuedToken string
	for _, event := range sess.events {
		if event.Type != "draft.ready" {
			continue
		}
		readyEventID = event.ID
		if draft, ok := event.Data.(InteractiveDraft); ok {
			issuedToken = draft.ConfirmationToken
		}
	}
	if readyEventID == 0 || issuedToken == "" {
		t.Fatal("ready draft event did not contain its event ID and confirmation token")
	}

	svc.maxHistory = 2
	for i := 0; i < 4; i++ {
		svc.emit(sess, "session.status", map[string]string{"status": "running"})
	}

	history, _, closeFn, err := svc.Events(sess.UserID, sess.ID, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer closeFn()

	var snapshot map[string]any
	for _, event := range history {
		if event.Type == "session.snapshot" {
			snapshot, _ = event.Data.(map[string]any)
		}
	}
	if snapshot == nil {
		t.Fatal("history did not include a session.snapshot")
	}
	draft, ok := snapshot["draft"].(InteractiveDraft)
	if !ok || !draft.Ready || draft.ConfirmationToken != issuedToken {
		t.Fatalf("snapshot draft = %#v, want ready draft with its current confirmation token", snapshot["draft"])
	}
	if got := snapshot["draft_event_id"]; got != readyEventID {
		t.Fatalf("snapshot draft_event_id = %#v, want %d", got, readyEventID)
	}
	if !confirmationMatches(draft.ConfirmationToken, sess.confirmationTokenHash) {
		t.Fatal("snapshot confirmation token cannot authorize the current draft")
	}
}

func TestInteractiveEventsIncludeCurrentSessionSnapshotAfterReplayCursor(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.Status = "waiting_answer"
	sess.pendingCall = "call-1"
	sess.pendingQuestions = map[string]InteractiveQuestion{
		"q-2": {ID: "q-2", Question: "Second question"},
		"q-1": {ID: "q-1", Question: "First question"},
	}
	svc := newInteractiveTestService(sess)
	svc.emit(sess, "turn.end", map[string]any{"turn": 1})

	history, _, closeFn, err := svc.Events(sess.UserID, sess.ID, sess.nextEvent)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer closeFn()
	if len(history) != 1 || history[0].Type != "session.snapshot" || history[0].ID != sess.nextEvent {
		t.Fatalf("snapshot history = %#v, want one current session.snapshot", history)
	}
	data, ok := history[0].Data.(map[string]any)
	if !ok || data["status"] != "waiting_answer" || data["pending_call_id"] != "call-1" {
		t.Fatalf("snapshot data = %#v, want waiting_answer with pending call", history[0].Data)
	}
	questions, ok := data["questions"].([]InteractiveQuestion)
	if !ok || len(questions) != 2 || questions[0].ID != "q-1" || questions[1].ID != "q-2" {
		t.Fatalf("snapshot questions = %#v, want sorted pending questions", data["questions"])
	}
}

func TestInteractiveEventsSnapshotIncludesBusyTurn(t *testing.T) {
	sess := newInteractiveTestSession()
	sess.Status = "running"
	sess.busy = true
	svc := newInteractiveTestService(sess)
	svc.emit(sess, "turn.start", map[string]any{"turn": 1})

	history, _, closeFn, err := svc.Events(sess.UserID, sess.ID, sess.nextEvent)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer closeFn()
	if len(history) != 1 || history[0].Type != "session.snapshot" {
		t.Fatalf("snapshot history = %#v, want one session.snapshot", history)
	}
	data, ok := history[0].Data.(map[string]any)
	if !ok || data["busy"] != true {
		t.Fatalf("snapshot data = %#v, want busy=true", history[0].Data)
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

func TestInteractiveCommitAcceptsReadyDraftDuringAgentFollowUp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "interactive.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}, &model.InteractiveCommitReceipt{}); err != nil {
		t.Fatal(err)
	}
	sess := newInteractiveTestSession()
	runtimeClosed := false
	sess.runtime = &interactiveTestRuntime{closeCheck: func() { runtimeClosed = true }}
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
	sess.busy = true
	sess.pendingCall = "follow-up-question"
	sess.pendingQuestions = map[string]InteractiveQuestion{
		"q-1": {ID: "q-1", Question: "还需要确认一个细节吗？"},
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
	if _, err := svc.Commit(context.Background(), sess.UserID, sess.ID, InteractiveCommitRequest{
		ConfirmationToken: replayedToken,
		IdempotencyKey:    "interactive-stale-commit",
	}); !errors.Is(err, ErrInteractiveToken) {
		t.Fatalf("Commit() with superseded draft token error = %v, want ErrInteractiveToken", err)
	}
	select {
	case event := <-events:
		if event.Type != "draft.updated" {
			t.Fatalf("live event type = %q, want draft.updated", event.Type)
		}
		if draft, ok := event.Data.(InteractiveDraft); !ok || draft.ConfirmationToken != "" {
			t.Fatalf("draft.updated payload = %#v, want no confirmation token", event.Data)
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
	if !runtimeClosed || !sess.Closed || sess.Status != "committed" {
		t.Fatalf("session closed=%v status=%q runtimeClosed=%v, want committed session with closed runtime", sess.Closed, sess.Status, runtimeClosed)
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

func TestAssistantProfileCommitConfirmsNewProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "assistant-profile-commit.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}, &model.InteractiveCommitReceipt{}, &model.AssistantCheckpoint{}, &model.AssistantSession{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	sess := newInteractiveTestSession()
	sess.ModeID = AssistantModeProfile
	svc := newInteractiveTestService(sess)
	svc.repo = repo
	if err := svc.StageDraftForSession(sess.ID, InteractiveStageInput{
		Project: InteractiveProject{Name: "Assistant profile", Platform: model.PlatformWechat},
		Profile: model.NewProjectProfile(), Progress: InteractiveProgress{
			"identity": "collected", "style": "collected", "audience": "collected",
			"platforms": "collected", "preferences": "collected", "memory": "collected",
		}, Ready: true,
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

	result, err := svc.Commit(context.Background(), sess.UserID, sess.ID, InteractiveCommitRequest{
		ConfirmationToken: token,
		IdempotencyKey:    "assistant-profile-commit",
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	restarted := NewInteractiveService(repo)
	replay, err := restarted.CommitAssistant(context.Background(), sess.UserID, sess.ID, AssistantCommitRequest{ConfirmationToken: token, IdempotencyKey: "assistant-profile-commit"}, nil, repo)
	if err != nil || replay.Project == nil || replay.Project.ID != result.Project.ID {
		t.Fatalf("profile receipt replay after restart = %#v, %v", replay, err)
	}
	if !result.Profile.IsConfirmed() || result.Profile.ApplicationSource != model.ProfileApplicationSourceUserConfirmation {
		t.Fatalf("Assistant profile = %#v, want confirmed user profile", result.Profile)
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
