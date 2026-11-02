package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/anbanai/anban-creator/server/service"
)

type startupRuntimeFactory struct{}

func (*startupRuntimeFactory) Start(context.Context, string, string, string, func(string, any)) (service.Runtime, error) {
	return nil, errors.New("startup must not admit a runtime")
}

func TestInitializeInteractiveRuntimeRetriesBeforeAdmittingSessions(t *testing.T) {
	for _, failedPhase := range []string{"cleanup", "recovery"} {
		t.Run(failedPhase, func(t *testing.T) {
			factory := &startupRuntimeFactory{}
			svc := service.NewInteractiveServiceWithRuntime(nil, factory)
			svc.SetEnabled(true)
			var events []string
			cleanupCalls, recoveryCalls := 0, 0
			checkAttempt := func(ctx context.Context) {
				t.Helper()
				if svc.Enabled() {
					t.Fatal("session admission enabled before recovery succeeded")
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 30*time.Second {
					t.Fatalf("attempt lacks bounded timeout: %v, %v", deadline, ok)
				}
			}
			got, err := initializeInteractiveRuntime(context.Background(), svc, factory, func(ctx context.Context) error {
				checkAttempt(ctx)
				events = append(events, "cleanup")
				cleanupCalls++
				if failedPhase == "cleanup" && cleanupCalls == 1 {
					return errors.New("private-token")
				}
				return nil
			}, func(ctx context.Context) error {
				checkAttempt(ctx)
				events = append(events, "recovery")
				recoveryCalls++
				if failedPhase == "recovery" && recoveryCalls == 1 {
					return errors.New("private-token")
				}
				return nil
			}, func(ctx context.Context, delay time.Duration) error {
				if delay != 5*time.Second {
					t.Fatalf("retry delay = %v", delay)
				}
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
				events = append(events, "wait")
				return nil
			})
			if err != nil || got != factory || !svc.Enabled() {
				t.Fatalf("ready factory = %v, enabled = %v, error = %v", got, svc.Enabled(), err)
			}
			want := []string{"cleanup", "wait", "cleanup", "recovery"}
			if failedPhase == "recovery" {
				want = []string{"cleanup", "recovery", "wait", "cleanup", "recovery"}
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("startup operations = %v, want %v (no cleanup after readiness)", events, want)
			}
		})
	}
}

func TestInitializeInteractiveRuntimeExhaustionWithholdsFactory(t *testing.T) {
	for _, failedPhase := range []string{"cleanup", "recovery"} {
		t.Run(failedPhase, func(t *testing.T) {
			factory := &startupRuntimeFactory{}
			svc := service.NewInteractiveServiceWithRuntime(nil, factory)
			cleanupCalls, recoveryCalls, waits := 0, 0, 0
			got, err := initializeInteractiveRuntime(context.Background(), svc, factory, func(context.Context) error {
				cleanupCalls++
				if failedPhase == "cleanup" {
					return errors.New("private-token")
				}
				return nil
			}, func(context.Context) error { recoveryCalls++; return errors.New("database-password") }, func(context.Context, time.Duration) error { waits++; return nil })
			if got != nil || svc.Enabled() || err == nil {
				t.Fatalf("failed startup admitted sessions: factory = %v, enabled = %v, err = %v", got, svc.Enabled(), err)
			}
			if cleanupCalls != 3 || waits != 2 || (failedPhase == "cleanup" && recoveryCalls != 0) || (failedPhase == "recovery" && recoveryCalls != 3) {
				t.Fatalf("attempt counts: cleanup=%d, recovery=%d, waits=%d", cleanupCalls, recoveryCalls, waits)
			}
			if !strings.Contains(err.Error(), failedPhase) || !strings.Contains(err.Error(), "3 attempts") || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "database-password") {
				t.Fatalf("unsafe/inaccurate startup reason: %v", err)
			}
		})
	}
}

func TestInitializeInteractiveRuntimeCancellation(t *testing.T) {
	for _, cancelAt := range []string{"before startup", "cleanup", "recovery", "wait"} {
		t.Run(cancelAt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelAt == "before startup" {
				cancel()
			}
			factory := &startupRuntimeFactory{}
			svc := service.NewInteractiveServiceWithRuntime(nil, factory)
			cleanupCalls, recoveryCalls, waits := 0, 0, 0
			got, err := initializeInteractiveRuntime(ctx, svc, factory, func(context.Context) error {
				cleanupCalls++
				if cancelAt == "cleanup" {
					cancel()
					return nil
				}
				if cancelAt == "recovery" {
					return nil
				}
				return errors.New("transient failure")
			}, func(context.Context) error {
				recoveryCalls++
				if cancelAt == "recovery" {
					cancel()
				}
				return nil
			}, func(ctx context.Context, _ time.Duration) error { waits++; cancel(); return ctx.Err() })
			if got != nil || svc.Enabled() || !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled startup = %v, %v, %v", got, svc.Enabled(), err)
			}
			wantCleanup, wantRecovery, wantWait := 1, 0, 0
			if cancelAt == "before startup" {
				wantCleanup = 0
			}
			if cancelAt == "recovery" {
				wantRecovery = 1
			}
			if cancelAt == "wait" {
				wantWait = 1
			}
			if cleanupCalls != wantCleanup || recoveryCalls != wantRecovery || waits != wantWait {
				t.Fatalf("operations after cancellation: cleanup=%d recovery=%d waits=%d", cleanupCalls, recoveryCalls, waits)
			}
		})
	}
}

func TestInitializeInteractiveRuntimeWithoutOrphanCleanup(t *testing.T) {
	factory := &startupRuntimeFactory{}
	svc := service.NewInteractiveServiceWithRuntime(nil, factory)
	calls := 0
	got, err := initializeInteractiveRuntime(context.Background(), svc, factory, nil, func(context.Context) error { calls++; return nil }, func(context.Context, time.Duration) error { t.Fatal("unexpected wait"); return nil })
	if err != nil || got != factory || !svc.Enabled() || calls != 1 {
		t.Fatalf("process runtime startup = %v, %v, %d", got, err, calls)
	}
}

func TestWaitInteractiveStartupHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitInteractiveStartup(ctx, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v", err)
	}
}

func TestInitializeInteractiveRuntimeRetriesWithFreshAttemptContext(t *testing.T) {
	factory := &startupRuntimeFactory{}
	svc := service.NewInteractiveServiceWithRuntime(nil, factory)
	var attempts []context.Context
	got, err := initializeInteractiveRuntime(context.Background(), svc, factory, func(ctx context.Context) error {
		if len(attempts) > 0 && attempts[len(attempts)-1].Err() == nil {
			t.Fatal("previous attempt context was not released")
		}
		attempts = append(attempts, ctx)
		return nil
	}, func(context.Context) error {
		if len(attempts) == 1 {
			return context.DeadlineExceeded
		}
		return nil
	}, func(context.Context, time.Duration) error { return nil })
	if err != nil || got != factory || len(attempts) != 2 || attempts[0] == attempts[1] {
		t.Fatalf("timed-out recovery = %v, %v, %v", got, err, attempts)
	}
	for _, attempt := range attempts {
		if attempt.Err() == nil {
			t.Fatal("attempt timeout context leaked")
		}
	}
}

func TestInitializeInteractiveRuntimeRequiresDurableRecovery(t *testing.T) {
	factory := &startupRuntimeFactory{}
	svc := service.NewInteractiveServiceWithRuntime(nil, factory)
	got, err := initializeInteractiveRuntime(context.Background(), svc, factory, func(context.Context) error { t.Fatal("cleanup before durable storage is configured"); return nil }, nil, func(context.Context, time.Duration) error { t.Fatal("retrying a missing configuration"); return nil })
	if got != nil || svc.Enabled() || err == nil || !strings.Contains(err.Error(), "durable session storage unavailable") {
		t.Fatalf("storage gate = %v, %v, %v", got, svc.Enabled(), err)
	}
}

func TestInitializeInteractiveRuntimeClassifiesExpiredAttemptWithProcessError(t *testing.T) {
	for _, failedPhase := range []string{"cleanup", "recovery"} {
		t.Run(failedPhase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				factory := &startupRuntimeFactory{}
				svc := service.NewInteractiveServiceWithRuntime(nil, factory)
				svc.SetEnabled(true)
				cleanupCalls, recoveryCalls := 0, 0
				started := time.Now()
				expireAttempt := func(ctx context.Context) error {
					if svc.Enabled() {
						t.Fatal("session admission enabled during startup")
					}
					<-ctx.Done()
					return errors.New("docker command: signal: killed private-token")
				}
				got, err := initializeInteractiveRuntime(context.Background(), svc, factory, func(ctx context.Context) error {
					cleanupCalls++
					if failedPhase == "cleanup" {
						return expireAttempt(ctx)
					}
					return nil
				}, func(ctx context.Context) error {
					recoveryCalls++
					return expireAttempt(ctx)
				}, nil)
				if got != nil || svc.Enabled() {
					t.Fatalf("expired startup admitted sessions: factory=%v enabled=%v", got, svc.Enabled())
				}
				if cleanupCalls != 3 || failedPhase == "cleanup" && recoveryCalls != 0 || failedPhase == "recovery" && recoveryCalls != 3 {
					t.Fatalf("attempt counts: cleanup=%d recovery=%d", cleanupCalls, recoveryCalls)
				}
				if elapsed := time.Since(started); elapsed != 100*time.Second {
					t.Fatalf("startup duration = %v, want three 30s attempts and two 5s waits", elapsed)
				}
				want := "interactive startup " + failedPhase + " timed out after 3 attempts"
				if err == nil || err.Error() != want {
					t.Fatalf("unsafe/inaccurate startup reason: %v, want %q", err, want)
				}
			})
		})
	}
}
