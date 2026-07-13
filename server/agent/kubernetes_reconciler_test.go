package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/anbanai/anban-creator/server/model"
)

type reconcileTestDispatcher struct {
	states map[string]*KubernetesExecutionState
	errs   map[string]error
}

func (*reconcileTestDispatcher) Dispatch(context.Context, *model.TaskExecution, *model.Task) error {
	return nil
}
func (*reconcileTestDispatcher) Delete(context.Context, *model.TaskExecution) error { return nil }
func (*reconcileTestDispatcher) DeleteProjectMemory(context.Context, string) error  { return nil }
func (d *reconcileTestDispatcher) Inspect(_ context.Context, execution *model.TaskExecution) (*KubernetesExecutionState, error) {
	return d.states[execution.ID], d.errs[execution.ID]
}

type reconcileFailure struct{ id, status, reason string }
type reconcileTestService struct {
	mu         sync.Mutex
	executions []*model.TaskExecution
	failures   []reconcileFailure
	resumed    []string
	pods       map[string]string
	failID     string
}

func (s *reconcileTestService) FindReconcilableExecutions(context.Context, time.Time, int) ([]*model.TaskExecution, error) {
	return s.executions, nil
}
func (s *reconcileTestService) RecordExecutionPod(_ context.Context, id, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pods == nil {
		s.pods = map[string]string{}
	}
	s.pods[id] = uid
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
		{"deadline before generic failure", kubernetesPhaseFailed, "DeadlineExceeded", "deadline_exceeded", nil},
		{"oom", kubernetesPhaseFailed, "", "oom_killed", &exit137},
		{"scheduling", kubernetesPhasePending, "FailedScheduling", "scheduling_failed", nil},
		{"mount", kubernetesPhasePending, "FailedMount", "volume_mount_failed", nil},
		{"image", kubernetesPhasePending, "ImagePullBackOff", "image_pull_failed", nil},
		{"job", kubernetesPhaseFailed, "BackoffLimitExceeded", "job_failed", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := kubernetesTerminalReason(&KubernetesExecutionState{Phase: tc.phase, Reason: tc.reason, ExitCode: tc.exit})
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
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "batch", Resource: "jobs"}, "gone")
	dispatcher := &reconcileTestDispatcher{
		states: map[string]*KubernetesExecutionState{
			"success-grace":   {Phase: kubernetesPhaseSucceeded},
			"success-expired": {Phase: kubernetesPhaseSucceeded, PodUID: "pod-1"},
			"bad-item":        {Phase: kubernetesPhaseFailed},
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
	if len(service.resumed) != 1 || service.resumed[0] != "terminal" || service.pods["success-expired"] != "pod-1" {
		t.Fatalf("resumed=%v pods=%v", service.resumed, service.pods)
	}
}

func TestKubernetesReconcilerRunStopsWithContext(t *testing.T) {
	dispatcher := &reconcileTestDispatcher{states: map[string]*KubernetesExecutionState{}, errs: map[string]error{}}
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
