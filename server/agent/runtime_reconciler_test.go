package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

type reconcileTestDispatcher struct {
	mu                sync.Mutex
	states            map[string]*RuntimeExecutionState
	errs              map[string]error
	activateErrs      map[string]error
	activated         []string
	deleteErrs        []error
	deleted           []string
	deletedIdentities []model.RuntimeIdentity
}

var _ RuntimeDispatcher = (*reconcileTestDispatcher)(nil)

func (*reconcileTestDispatcher) ResolveRuntime(string) srvconfig.RuntimeImageSelection {
	return srvconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:test"}
}

func (*reconcileTestDispatcher) Scope() string { return "test" }

func (*reconcileTestDispatcher) Prepare(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "test", Workload: "runtime-" + execution.ID}, nil
}
func (*reconcileTestDispatcher) ResolvePrepared(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "test", Workload: "runtime-" + execution.ID, InstanceID: "instance-" + execution.ID}, nil
}
func (d *reconcileTestDispatcher) Activate(_ context.Context, execution *model.TaskExecution) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activated = append(d.activated, execution.ID)
	return d.activateErrs[execution.ID]
}
func (d *reconcileTestDispatcher) Delete(_ context.Context, execution *model.TaskExecution) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, execution.ID)
	d.deletedIdentities = append(d.deletedIdentities, model.RuntimeIdentity{
		Scope: execution.RuntimeScope, Workload: execution.RuntimeWorkload, InstanceID: execution.RuntimeInstanceID,
	})
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
	mu                 sync.Mutex
	executions         []*model.TaskExecution
	failures           []reconcileFailure
	resumed            []string
	dispatched         []string
	instances          map[string]string
	cleanup            map[string]string
	diagnostics        map[string][]byte
	failID             string
	cleanupResolutions map[string]*model.TaskExecution
	cleanupResolveErr  error
	resolvedCleanup    []string
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
func (s *reconcileTestService) ResolveExecutionCleanupRuntime(_ context.Context, id, token string) (*model.TaskExecution, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolvedCleanup = append(s.resolvedCleanup, id)
	if s.cleanupResolveErr != nil {
		return nil, false, s.cleanupResolveErr
	}
	if resolved := s.cleanupResolutions[id]; resolved != nil {
		copy := *resolved
		return &copy, true, nil
	}
	for _, execution := range s.executions {
		if execution != nil && execution.ID == id {
			copy := *execution
			return &copy, true, nil
		}
	}
	return nil, false, nil
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
func (s *reconcileTestService) ReconcileExecutionFailure(_ context.Context, id, status, reason string, diagnostics []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.failID {
		return errors.New("injected")
	}
	s.failures = append(s.failures, reconcileFailure{id, status, reason})
	if s.diagnostics == nil {
		s.diagnostics = map[string][]byte{}
	}
	s.diagnostics[id] = append([]byte(nil), diagnostics...)
	return nil
}

func TestRuntimeTerminalReasonPrecedence(t *testing.T) {
	exit2 := int32(2)
	exit137 := int32(137)
	tests := []struct {
		name, phase, reason, wantReason, wantStatus string
		exit                                        *int32
	}{
		{"deadline", RuntimePhaseFailed, "DeadlineExceeded", "deadline_exceeded", model.TaskExecutionTimedOut, nil},
		{"runtime deadline", RuntimePhaseFailed, "runtime deadline exceeded", "deadline_exceeded", model.TaskExecutionTimedOut, nil},
		{"oom reason", RuntimePhaseFailed, "OOMKilled", "oom_killed", model.TaskExecutionFailed, nil},
		{"oom exit code", RuntimePhaseFailed, "Failed", "oom_killed", model.TaskExecutionFailed, &exit137},
		{"completion report", RuntimePhaseFailed, "Failed", "completion_report_failed", model.TaskExecutionFailed, &exit2},
		{"scheduling", RuntimePhaseFailed, "FailedScheduling", "runtime_failed", model.TaskExecutionFailed, nil},
		{"mount", RuntimePhaseFailed, "FailedMount", "runtime_failed", model.TaskExecutionFailed, nil},
		{"attach volume", RuntimePhaseFailed, "FailedAttachVolume", "runtime_failed", model.TaskExecutionFailed, nil},
		{"image pull", RuntimePhaseFailed, "ErrImagePull", "runtime_failed", model.TaskExecutionFailed, nil},
		{"image pull backoff", RuntimePhaseFailed, "ImagePullBackOff", "runtime_failed", model.TaskExecutionFailed, nil},
		{"crash loop", RuntimePhaseFailed, "CrashLoopBackOff", "runtime_failed", model.TaskExecutionFailed, nil},
		{"run container", RuntimePhaseFailed, "RunContainerError", "runtime_failed", model.TaskExecutionFailed, nil},
		{"backoff limit", RuntimePhaseFailed, "BackoffLimitExceeded", "runtime_failed", model.TaskExecutionFailed, nil},
		{"timeout", RuntimePhaseFailed, "Timeout", "deadline_exceeded", model.TaskExecutionTimedOut, nil},
		{"timed out", RuntimePhaseFailed, "TimedOut", "deadline_exceeded", model.TaskExecutionTimedOut, nil},
		{"plain failed", RuntimePhaseFailed, "Failed", "runtime_failed", model.TaskExecutionFailed, nil},
		{"provider reason cannot terminalize pending state", RuntimePhasePending, "FailedScheduling", "", "", nil},
		{"timeout cannot terminalize pending state", RuntimePhasePending, "Timeout", "", "", nil},
		{"timeout cannot terminalize running state", RuntimePhaseRunning, "TimedOut", "", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotReason, gotStatus := runtimeTerminalReason(&RuntimeExecutionState{Phase: tc.phase, Reason: tc.reason, ExitCode: tc.exit})
			if gotReason != tc.wantReason || gotStatus != tc.wantStatus {
				t.Fatalf("reason/status = %q/%q, want %q/%q", gotReason, gotStatus, tc.wantReason, tc.wantStatus)
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
	failures := append([]reconcileFailure(nil), service.failures...)
	diagnostics := append([]byte(nil), service.diagnostics[execution.ID]...)
	service.mu.Unlock()
	if len(failures) != 1 || failures[0].reason != "deadline_exceeded" || failures[0].status != model.TaskExecutionTimedOut {
		t.Fatalf("failures = %#v", failures)
	}
	var diagnostic map[string]any
	if err := json.Unmarshal(diagnostics, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if diagnostic["reason"] != "deadline_exceeded" || diagnostic["runtime_reason"] != "" {
		t.Fatalf("diagnostics = %#v, want synthetic deadline with empty runtime reason", diagnostic)
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

func TestRuntimeReconcilerActivatesPersistedPendingWorkload(t *testing.T) {
	execution := &model.TaskExecution{
		ID: "prepared", Status: model.TaskExecutionStarting,
		RuntimeScope: "test", RuntimeWorkload: "runtime-prepared", RuntimeInstanceID: "instance-prepared",
	}
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*RuntimeExecutionState{execution.ID: {Phase: RuntimePhasePending}}, errs: map[string]error{},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if len(dispatcher.activated) != 1 || dispatcher.activated[0] != execution.ID {
		t.Fatalf("activated = %v, want crash recovery activation", dispatcher.activated)
	}
}

func TestRuntimeReconcilerPermanentActivationFailureTerminalizesAndCleansUp(t *testing.T) {
	execution := &model.TaskExecution{
		ID: "activation-failed", Status: model.TaskExecutionStarting,
		RuntimeScope: "test", RuntimeWorkload: "runtime-activation-failed", RuntimeInstanceID: "instance-activation-failed",
	}
	dispatcher := &reconcileTestDispatcher{
		states:       map[string]*RuntimeExecutionState{execution.ID: {Phase: RuntimePhasePending}},
		errs:         map[string]error{},
		activateErrs: map[string]error{execution.ID: NewPermanentDispatchError(errors.New("activation identity drift"))},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.failures) != 1 || service.failures[0].reason != "activation_failed" || service.cleanup[execution.ID] != "done" {
		t.Fatalf("failures=%v cleanup=%v", service.failures, service.cleanup)
	}
}

func TestRuntimeReconcilerActiveDeadlineTimesOutNeverBootstrappedWorkload(t *testing.T) {
	now := time.Now()
	execution := &model.TaskExecution{
		ID: "never-bootstrapped", Status: model.TaskExecutionStarting, CreatedAt: now.Add(-11 * time.Minute),
		RuntimeScope: "test", RuntimeWorkload: "runtime-never-bootstrapped", RuntimeInstanceID: "instance-never-bootstrapped",
	}
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*RuntimeExecutionState{execution.ID: {Phase: RuntimePhasePending}}, errs: map[string]error{},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	cfg := RuntimeReconcilerConfig{}
	field := reflect.ValueOf(&cfg).Elem().FieldByName("ActiveDeadline")
	if !field.IsValid() || !field.CanSet() {
		t.Fatal("RuntimeReconcilerConfig has no ActiveDeadline hard cap")
	}
	field.Set(reflect.ValueOf(10 * time.Minute))
	reconciler := NewRuntimeReconciler(dispatcher, service, cfg, zerolog.Nop())
	reconciler.now = func() time.Time { return now }
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.failures) != 1 || service.failures[0].status != model.TaskExecutionTimedOut || service.failures[0].reason != "deadline_exceeded" || service.cleanup[execution.ID] != "done" {
		t.Fatalf("failures=%v cleanup=%v", service.failures, service.cleanup)
	}
}

func TestRuntimeReconcilerActiveDeadlineRecoversIdentityBeforeExactDeleteWithoutActivation(t *testing.T) {
	now := time.Now()
	execution := &model.TaskExecution{
		ID: "prepared-before-deadline", Status: model.TaskExecutionDispatching, CreatedAt: now.Add(-11 * time.Minute),
		CleanupStatus: model.TaskExecutionCleanupPending,
	}
	resolved := *execution
	resolved.Status = model.TaskExecutionTimedOut
	resolved.RuntimeScope = "test"
	resolved.RuntimeWorkload = "runtime-prepared-before-deadline"
	resolved.RuntimeInstanceID = "instance-prepared-before-deadline"
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{}, errs: map[string]error{}}
	service := &reconcileTestService{
		executions:         []*model.TaskExecution{execution},
		cleanupResolutions: map[string]*model.TaskExecution{execution.ID: &resolved},
	}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{ActiveDeadline: 10 * time.Minute}, zerolog.Nop())
	reconciler.now = func() time.Time { return now }

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	dispatcher.mu.Lock()
	activated := append([]string(nil), dispatcher.activated...)
	deleted := append([]model.RuntimeIdentity(nil), dispatcher.deletedIdentities...)
	dispatcher.mu.Unlock()
	service.mu.Lock()
	resolvedCalls := append([]string(nil), service.resolvedCleanup...)
	service.mu.Unlock()
	want := model.RuntimeIdentity{Scope: resolved.RuntimeScope, Workload: resolved.RuntimeWorkload, InstanceID: resolved.RuntimeInstanceID}
	if len(activated) != 0 || len(resolvedCalls) != 1 || resolvedCalls[0] != execution.ID || len(deleted) != 1 || deleted[0] != want {
		t.Fatalf("activated=%v resolved=%v deleted=%#v, want exact %#v", activated, resolvedCalls, deleted, want)
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
			execution.ID: {Phase: RuntimePhaseFailed, InstanceID: "container-id", Reason: "BackoffLimitExceeded", Message: "runtime exited", ExitCode: &exitCode},
		},
		errs: map[string]error{},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewRuntimeReconciler(dispatcher, service, RuntimeReconcilerConfig{}, zerolog.Nop())

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	service.mu.Lock()
	failures := append([]reconcileFailure(nil), service.failures...)
	diagnostics := append([]byte(nil), service.diagnostics[execution.ID]...)
	service.mu.Unlock()
	if len(failures) != 1 || failures[0].reason != "runtime_failed" {
		t.Fatalf("failures = %#v, want runtime_failed", failures)
	}
	var diagnostic map[string]any
	if err := json.Unmarshal(diagnostics, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if diagnostic["reason"] != "runtime_failed" || diagnostic["runtime_reason"] != "BackoffLimitExceeded" || diagnostic["message"] != "runtime exited" {
		t.Fatalf("diagnostics = %#v", diagnostic)
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if len(dispatcher.deleted) != 1 || dispatcher.deleted[0] != execution.ID {
		t.Fatalf("deleted = %v, want [%s]", dispatcher.deleted, execution.ID)
	}
}
