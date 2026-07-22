package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

type reconcileTestDispatcher struct {
	mu         sync.Mutex
	states     map[string]*RuntimeExecutionState
	errs       map[string]error
	deleteErrs []error
	deleted    []string
}

var _ RuntimeDispatcher = (*reconcileTestDispatcher)(nil)

func (*reconcileTestDispatcher) ResolveRuntime(string) srvconfig.RuntimeImageSelection {
	return srvconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:test"}
}

func (*reconcileTestDispatcher) Scope() string { return "test" }

func (*reconcileTestDispatcher) Dispatch(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "test", Workload: "runtime-" + execution.ID}, nil
}
func (d *reconcileTestDispatcher) Delete(_ context.Context, execution *model.TaskExecution) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, execution.ID)
	if len(d.deleteErrs) == 0 {
		return nil
	}
	err := d.deleteErrs[0]
	d.deleteErrs = d.deleteErrs[1:]
	return err
}
func (d *reconcileTestDispatcher) Inspect(_ context.Context, execution *model.TaskExecution) (*RuntimeExecutionState, error) {
	return d.states[execution.ID], d.errs[execution.ID]
}

type reconcileFailure struct{ id, status, reason string }
type reconcileTestService struct {
	mu         sync.Mutex
	executions []*model.TaskExecution
	failures   []reconcileFailure
	resumed    []string
	dispatched []string
	instances  map[string]string
	cleanup    map[string]string
	failID     string
}

func (s *reconcileTestService) FindReconcilableExecutions(context.Context, time.Time, int) ([]*model.TaskExecution, error) {
	return s.executions, nil
}
func (s *reconcileTestService) RecordExecutionInstance(_ context.Context, id, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.instances == nil {
		s.instances = map[string]string{}
	}
	s.instances[id] = uid
	return nil
}
func (s *reconcileTestService) ResumeExecutionDispatch(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispatched = append(s.dispatched, id)
	return nil
}
func (s *reconcileTestService) ClaimExecutionCleanup(_ context.Context, id, token string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanup == nil {
		s.cleanup = map[string]string{}
	}
	if s.cleanup[id] != "" {
		return false, nil
	}
	s.cleanup[id] = token
	return true, nil
}
func (s *reconcileTestService) CompleteExecutionCleanup(_ context.Context, id, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanup[id] != token {
		return false, nil
	}
	s.cleanup[id] = "done"
	return true, nil
}
func (s *reconcileTestService) FailExecutionCleanup(_ context.Context, id, token string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanup[id] != token {
		return false, nil
	}
	s.cleanup[id] = ""
	return true, nil
}
func (s *reconcileTestService) ReleaseExecutionCleanup(_ context.Context, id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanup[id] == token {
		s.cleanup[id] = ""
	}
	return nil
}
func (s *reconcileTestService) ResumeExecutionFinalization(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resumed = append(s.resumed, id)
	return nil
}
func (s *reconcileTestService) ReconcileExecutionFailure(_ context.Context, id, status, reason string, _ []byte, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.failID {
		return errors.New("injected")
	}
	s.failures = append(s.failures, reconcileFailure{id, status, reason})
	return nil
}

func TestRuntimeTerminalReasonPrecedence(t *testing.T) {
	exit137 := int32(137)
	tests := []struct {
		name, phase, reason, want string
		exit                      *int32
	}{
		{"deadline before generic failure", RuntimePhaseFailed, "DeadlineExceeded", "deadline_exceeded", nil},
		{"oom", RuntimePhaseFailed, "", "oom_killed", &exit137},
		{"scheduling", RuntimePhasePending, "FailedScheduling", "scheduling_failed", nil},
		{"mount", RuntimePhasePending, "FailedMount", "volume_mount_failed", nil},
		{"image", RuntimePhasePending, "ImagePullBackOff", "image_pull_failed", nil},
		{"runtime", RuntimePhaseFailed, "BackoffLimitExceeded", "runtime_failed", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := runtimeTerminalReason(&RuntimeExecutionState{Phase: tc.phase, Reason: tc.reason, ExitCode: tc.exit})
			if got != tc.want {
				t.Fatalf("reason=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRuntimeReconcilerGracesAndItemIsolation(t *testing.T) {
	now := time.Now()
	executions := []*model.TaskExecution{
		{ID: "success-grace", Status: model.TaskExecutionRunning, UpdatedAt: now.Add(-5 * time.Second), CreatedAt: now.Add(-time.Minute)},
		{ID: "success-expired", Status: model.TaskExecutionRunning, UpdatedAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Minute)},
		{ID: "missing-grace", Status: model.TaskExecutionStarting, CreatedAt: now.Add(-5 * time.Second)},
		{ID: "missing-expired", Status: model.TaskExecutionStarting, CreatedAt: now.Add(-time.Minute)},
		{ID: "bad-item", Status: model.TaskExecutionRunning, UpdatedAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Minute)},
		{ID: "terminal", Status: model.TaskExecutionFailed, FinalizationStatus: model.TaskExecutionFinalizationTask},
	}
	notFound := ErrRuntimeWorkloadNotFound
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*RuntimeExecutionState{
			"success-grace":   {Phase: RuntimePhaseSucceeded},
			"success-expired": {Phase: RuntimePhaseSucceeded, InstanceID: "container-1"},
			"bad-item":        {Phase: RuntimePhaseFailed},
		},
		errs: map[string]error{"missing-grace": notFound, "missing-expired": notFound},
	}
	service := &reconcileTestService{executions: executions, failID: "bad-item"}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{
		Concurrency: 2, BatchSize: 10, CompletionGrace: 30 * time.Second, MissingResourceGrace: 30 * time.Second,
	}, zerolog.Nop())
	reconciler.now = func() time.Time { return now }
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	got := map[string]string{}
	for _, failure := range service.failures {
		got[failure.id] = failure.reason
	}
	if got["success-expired"] != "missing_completion" || got["missing-expired"] != "runtime_missing" {
		t.Fatalf("failures=%v", service.failures)
	}
	if _, ok := got["success-grace"]; ok {
		t.Fatal("completion grace was ignored")
	}
	if _, ok := got["missing-grace"]; ok {
		t.Fatal("missing-resource grace was ignored")
	}
	if len(service.resumed) != 1 || service.resumed[0] != "terminal" || service.instances["success-expired"] != "container-1" {
		t.Fatalf("resumed=%v instances=%v", service.resumed, service.instances)
	}
}

func TestRuntimeReconcilerUsesExecutionHeartbeat(t *testing.T) {
	now := time.Now()
	started := now.Add(-10 * time.Minute)
	heartbeat := now.Add(-4 * time.Minute)
	execution := &model.TaskExecution{ID: "stale-heartbeat", Status: model.TaskExecutionRunning, Started: true, StartedAt: &started, LastHeartbeatAt: &heartbeat}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{
		execution.ID: {Phase: RuntimePhaseRunning},
	}, errs: map[string]error{}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{HeartbeatTimeout: 3 * time.Minute}, zerolog.Nop())
	reconciler.now = func() time.Time { return now }
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.failures) != 1 || service.failures[0].reason != "deadline_exceeded" || service.failures[0].status != model.TaskExecutionTimedOut {
		t.Fatalf("failures = %#v", service.failures)
	}
}

func TestRuntimeReconcilerFinalizesMissingStartedWorkload(t *testing.T) {
	now := time.Now()
	execution := &model.TaskExecution{
		ID:                "missing-started",
		Status:            model.TaskExecutionRunning,
		Started:           true,
		RuntimeInstanceID: "container-id",
		UpdatedAt:         now.Add(-time.Minute),
	}
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*RuntimeExecutionState{},
		errs:   map[string]error{execution.ID: ErrRuntimeWorkloadNotFound},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{MissingResourceGrace: 30 * time.Second}, zerolog.Nop())
	reconciler.now = func() time.Time { return now }

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.failures) != 1 || service.failures[0].reason != "runtime_missing" || service.failures[0].status != model.TaskExecutionFailed {
		t.Fatalf("failures = %#v, want runtime_missing failure", service.failures)
	}
	if len(service.dispatched) != 0 {
		t.Fatalf("dispatch recovery = %v, want none after confirmed start", service.dispatched)
	}
}

func TestRuntimeReconcilerFailsAndCleansUpEmptyInspection(t *testing.T) {
	execution := &model.TaskExecution{ID: "empty-inspection", Status: model.TaskExecutionRunning}
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*RuntimeExecutionState{execution.ID: nil},
		errs:   map[string]error{},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.failures) != 1 || service.failures[0].id != execution.ID || service.failures[0].status != model.TaskExecutionFailed || service.failures[0].reason != "inspection_empty" {
		t.Fatalf("failures = %#v, want inspection_empty failure", service.failures)
	}
	if service.cleanup[execution.ID] != "done" {
		t.Fatalf("cleanup = %q, want done", service.cleanup[execution.ID])
	}
	dispatcher.mu.Lock()
	deletes := len(dispatcher.deleted)
	dispatcher.mu.Unlock()
	if deletes != 1 {
		t.Fatalf("deletes = %d, want 1", deletes)
	}
}

func TestRuntimeReconcilerRunStopsWithContext(t *testing.T) {
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{}, errs: map[string]error{}}
	service := &reconcileTestService{}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{Interval: time.Millisecond}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); reconciler.Run(ctx) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not stop")
	}
}

func TestRuntimeReconcilerResumesCreatedDispatch(t *testing.T) {
	execution := &model.TaskExecution{ID: "replacement", Status: model.TaskExecutionCreated}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{}, errs: map[string]error{}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(service.dispatched) != 1 || service.dispatched[0] != execution.ID {
		t.Fatalf("dispatched=%v", service.dispatched)
	}
}

func TestRuntimeReconcilerDoesNotInferBootstrapFromRuntimeState(t *testing.T) {
	now := time.Now()
	execution := &model.TaskExecution{ID: "started-repair", Status: model.TaskExecutionStarting, CreatedAt: now.Add(-time.Minute)}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{
		execution.ID: {Phase: RuntimePhaseFailed, InstanceID: "container-1", Reason: "Error"},
	}, errs: map[string]error{}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())
	reconciler.now = func() time.Time { return now }
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.instances[execution.ID] != "container-1" || len(service.failures) != 1 {
		t.Fatalf("instances=%v failures=%v", service.instances, service.failures)
	}
}

func TestRuntimeReconcilerRetriesDurableCleanup(t *testing.T) {
	execution := &model.TaskExecution{ID: "cleanup", Status: model.TaskExecutionFailed, FinalizationStatus: model.TaskExecutionFinalizationDone, CleanupStatus: model.TaskExecutionCleanupPending}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{}, errs: map[string]error{}, deleteErrs: []error{errors.New("delete failed"), nil}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	dispatcher.mu.Lock()
	deletes := len(dispatcher.deleted)
	dispatcher.mu.Unlock()
	service.mu.Lock()
	cleanup := service.cleanup[execution.ID]
	service.mu.Unlock()
	if deletes != 2 || cleanup != "done" {
		t.Fatalf("deletes=%d cleanup=%q", deletes, cleanup)
	}
}

func TestRuntimeReconcilerFinalizesExitedContainer(t *testing.T) {
	exitCode := int32(1)
	execution := &model.TaskExecution{ID: "execution-1", Status: model.TaskExecutionRunning, Started: true}
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*RuntimeExecutionState{
			execution.ID: {Phase: RuntimePhaseFailed, InstanceID: "container-id", Reason: "Exited", ExitCode: &exitCode},
		},
		errs: map[string]error{},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	service.mu.Lock()
	if len(service.failures) != 1 || service.failures[0].reason != "runtime_failed" {
		service.mu.Unlock()
		t.Fatalf("failures = %#v, want runtime_failed", service.failures)
	}
	service.mu.Unlock()
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if len(dispatcher.deleted) != 1 || dispatcher.deleted[0] != execution.ID {
		t.Fatalf("deleted = %v, want [%s]", dispatcher.deleted, execution.ID)
	}
}
