package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestSidecarMonitorRunReturnsImmediatelyAndBecomesReadyAfterRetry(t *testing.T) {
	var attempts atomic.Int32
	logger := zerolog.Nop()
	monitor := NewSidecarMonitor(SidecarMonitorConfig{
		Name:            "seednote",
		InitialBackoff:  5 * time.Millisecond,
		MaxBackoff:      5 * time.Millisecond,
		HealthyInterval: time.Hour,
		CheckTimeout:    time.Second,
		HealthCheck: func(context.Context) error {
			if attempts.Add(1) == 1 {
				return errors.New("not ready")
			}
			return nil
		},
	}, &logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		monitor.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Run returned before context cancellation")
	case <-time.After(20 * time.Millisecond):
	}

	eventually(t, time.Second, func() bool {
		return monitor.Ready()
	})

	snap := monitor.Snapshot()
	if !snap.Ready {
		t.Fatalf("snapshot ready = false, want true")
	}
	if snap.LastError != "" {
		t.Fatalf("snapshot last error = %q, want empty", snap.LastError)
	}
	if snap.LastCheckedAt.IsZero() || snap.LastReadyAt.IsZero() {
		t.Fatalf("snapshot timestamps not populated: %#v", snap)
	}
}

func TestSidecarMonitorMarksUnavailableAfterHealthyCheckFails(t *testing.T) {
	var fail atomic.Bool
	logger := zerolog.Nop()
	monitor := NewSidecarMonitor(SidecarMonitorConfig{
		Name:            "ilink",
		InitialBackoff:  5 * time.Millisecond,
		MaxBackoff:      5 * time.Millisecond,
		HealthyInterval: 5 * time.Millisecond,
		CheckTimeout:    time.Second,
		HealthCheck: func(context.Context) error {
			if fail.Load() {
				return errors.New("connection refused")
			}
			return nil
		},
	}, &logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Run(ctx)

	eventually(t, time.Second, monitor.Ready)
	fail.Store(true)
	eventually(t, time.Second, func() bool {
		snap := monitor.Snapshot()
		return !snap.Ready && strings.Contains(snap.LastError, "connection refused")
	})
}

func eventually(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
