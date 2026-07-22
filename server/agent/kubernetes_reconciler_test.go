package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type reconcileTestDispatcher struct {
	mu         sync.Mutex
	states     map[string]*RuntimeExecutionState
	errs       map[string]error
	deleteErrs []error
	deletes    int
}

func (*reconcileTestDispatcher) ResolveRuntime(string) srvconfig.RuntimeImageSelection {
	return srvconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:test"}
}

func (*reconcileTestDispatcher) Scope() string { return "kubernetes" }

func (*reconcileTestDispatcher) Dispatch(_ context.Context, execution *model.TaskExecution, _ *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "anban", Workload: "job-" + execution.ID}, nil
}
func (d *reconcileTestDispatcher) Delete(context.Context, *model.TaskExecution) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deletes++
	if len(d.deleteErrs) == 0 {
		return nil
	}
	err := d.deleteErrs[0]
	d.deleteErrs = d.deleteErrs[1:]
	return err
}
func (*reconcileTestDispatcher) DeleteProjectMemory(context.Context, string) error { return nil }
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

func TestKubernetesTerminalReasonPrecedence(t *testing.T) {
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
		{"job", RuntimePhaseFailed, "BackoffLimitExceeded", "job_failed", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := kubernetesTerminalReason(&RuntimeExecutionState{Phase: tc.phase, Reason: tc.reason, ExitCode: tc.exit})
			if got != tc.want {
				t.Fatalf("reason=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestKubernetesReconcilerGracesAndItemIsolation(t *testing.T) {
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
			"success-expired": {Phase: RuntimePhaseSucceeded, InstanceID: "pod-1"},
			"bad-item":        {Phase: RuntimePhaseFailed},
		},
		errs: map[string]error{"missing-grace": notFound, "missing-expired": notFound},
	}
	service := &reconcileTestService{executions: executions, failID: "bad-item"}
	reconciler := NewKubernetesReconciler(dispatcher, service, KubernetesReconcilerConfig{
		Concurrency: 2, BatchSize: 10, CompletionGrace: 30 * time.Second, MissingResourceGrace: 30 * time.Second,
	}, nil)
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
	if got["success-expired"] != "missing_completion" || got["missing-expired"] != "job_missing" {
		t.Fatalf("failures=%v", service.failures)
	}
	if _, ok := got["success-grace"]; ok {
		t.Fatal("completion grace was ignored")
	}
	if _, ok := got["missing-grace"]; ok {
		t.Fatal("missing-resource grace was ignored")
	}
	if len(service.resumed) != 1 || service.resumed[0] != "terminal" || service.instances["success-expired"] != "pod-1" {
		t.Fatalf("resumed=%v instances=%v", service.resumed, service.instances)
	}
}

func TestKubernetesReconcilerUsesExecutionHeartbeat(t *testing.T) {
	now := time.Now()
	started := now.Add(-10 * time.Minute)
	heartbeat := now.Add(-4 * time.Minute)
	execution := &model.TaskExecution{ID: "stale-heartbeat", Status: model.TaskExecutionRunning, Started: true, StartedAt: &started, LastHeartbeatAt: &heartbeat}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{
		execution.ID: {Phase: RuntimePhaseRunning},
	}, errs: map[string]error{}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewKubernetesReconciler(dispatcher, service, KubernetesReconcilerConfig{HeartbeatTimeout: 3 * time.Minute}, nil)
	reconciler.now = func() time.Time { return now }
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.failures) != 1 || service.failures[0].reason != "heartbeat_timeout" || service.failures[0].status != model.TaskExecutionTimedOut {
		t.Fatalf("failures = %#v", service.failures)
	}
}

func TestKubernetesReconcilerFailsAndCleansUpEmptyInspection(t *testing.T) {
	execution := &model.TaskExecution{ID: "empty-inspection", Status: model.TaskExecutionRunning}
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*RuntimeExecutionState{execution.ID: nil},
		errs:   map[string]error{},
	}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewKubernetesReconciler(dispatcher, service, KubernetesReconcilerConfig{}, nil)

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
	deletes := dispatcher.deletes
	dispatcher.mu.Unlock()
	if deletes != 1 {
		t.Fatalf("deletes = %d, want 1", deletes)
	}
}

func TestKubernetesReconcilerRunStopsWithContext(t *testing.T) {
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{}, errs: map[string]error{}}
	service := &reconcileTestService{}
	reconciler := NewKubernetesReconciler(dispatcher, service, KubernetesReconcilerConfig{Interval: time.Millisecond}, nil)
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

func TestKubernetesReconcilerResumesCreatedDispatch(t *testing.T) {
	execution := &model.TaskExecution{ID: "replacement", Status: model.TaskExecutionCreated}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{}, errs: map[string]error{}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewKubernetesReconciler(dispatcher, service, KubernetesReconcilerConfig{}, nil)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(service.dispatched) != 1 || service.dispatched[0] != execution.ID {
		t.Fatalf("dispatched=%v", service.dispatched)
	}
}

func TestKubernetesReconcilerDoesNotInferBootstrapFromContainerState(t *testing.T) {
	now := time.Now()
	execution := &model.TaskExecution{ID: "started-repair", Status: model.TaskExecutionStarting, CreatedAt: now.Add(-time.Minute)}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{
		execution.ID: {Phase: RuntimePhaseFailed, InstanceID: "pod-1", Reason: "Error"},
	}, errs: map[string]error{}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewKubernetesReconciler(dispatcher, service, KubernetesReconcilerConfig{}, nil)
	reconciler.now = func() time.Time { return now }
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.instances[execution.ID] != "pod-1" || len(service.failures) != 1 {
		t.Fatalf("instances=%v failures=%v", service.instances, service.failures)
	}
}

func TestKubernetesReconcilerRetriesDurableCleanup(t *testing.T) {
	execution := &model.TaskExecution{ID: "cleanup", Status: model.TaskExecutionFailed, FinalizationStatus: model.TaskExecutionFinalizationDone, CleanupStatus: model.TaskExecutionCleanupPending}
	dispatcher := &reconcileTestDispatcher{states: map[string]*RuntimeExecutionState{}, errs: map[string]error{}, deleteErrs: []error{errors.New("delete failed"), nil}}
	service := &reconcileTestService{executions: []*model.TaskExecution{execution}}
	reconciler := NewKubernetesReconciler(dispatcher, service, KubernetesReconcilerConfig{}, nil)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	dispatcher.mu.Lock()
	deletes := dispatcher.deletes
	dispatcher.mu.Unlock()
	service.mu.Lock()
	cleanup := service.cleanup[execution.ID]
	service.mu.Unlock()
	if deletes != 2 || cleanup != "done" {
		t.Fatalf("deletes=%d cleanup=%q", deletes, cleanup)
	}
}
