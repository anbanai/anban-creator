package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
)

type sidecarLogEntry struct {
	Level              string `json:"level"`
	Message            string `json:"message"`
	Error              string `json:"error"`
	Sidecar            string `json:"sidecar"`
	SuppressedFailures uint64 `json:"suppressed_failures"`
}

type sidecarLogBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (buf *sidecarLogBuffer) Write(p []byte) (int, error) {
	buf.mu.Lock()
	defer buf.mu.Unlock()
	return buf.Buffer.Write(p)
}

func sidecarLogs(t *testing.T, buf *sidecarLogBuffer) []sidecarLogEntry {
	t.Helper()
	buf.mu.Lock()
	defer buf.mu.Unlock()
	var entries []sidecarLogEntry
	decoder := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for decoder.More() {
		var entry sidecarLogEntry
		if err := decoder.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestSidecarMonitorLogsFailureTransitionsAndSummaries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf sidecarLogBuffer
		logger := zerolog.New(&buf)
		var healthErr error
		var healthMu sync.Mutex
		monitor := NewSidecarMonitor(SidecarMonitorConfig{
			Name: "seednote", InitialBackoff: 30 * time.Second, MaxBackoff: 30 * time.Second,
			HealthyInterval: 30 * time.Second,
			HealthCheck: func(context.Context) error {
				healthMu.Lock()
				defer healthMu.Unlock()
				return healthErr
			},
		}, &logger)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go monitor.Run(ctx)
		synctest.Wait()
		if entries := sidecarLogs(t, &buf); len(entries) != 0 {
			t.Fatalf("healthy startup logged: %#v", entries)
		}

		steps := []struct {
			name       string
			err        error
			advance    time.Duration
			logs       int
			level      string
			suppressed uint64
		}{
			{"first failure", errors.New("connection refused"), 30 * time.Second, 1, "warn", 0},
			{"same failure suppressed", errors.New("connection refused"), 30 * time.Second, 1, "warn", 0},
			{"different failure immediate", errors.New("HTTP 503"), 30 * time.Second, 2, "warn", 1},
			{"before five minute summary", errors.New("HTTP 503"), 270 * time.Second, 2, "warn", 1},
			{"five minute summary", errors.New("HTTP 503"), 30 * time.Second, 3, "warn", 9},
			{"next failure suppressed", errors.New("HTTP 503"), 30 * time.Second, 3, "warn", 9},
			{"recovered", nil, 30 * time.Second, 4, "info", 1},
			{"healthy stays quiet", nil, 30 * time.Second, 4, "info", 1},
			{"same error after recovery is immediate", errors.New("HTTP 503"), 30 * time.Second, 5, "warn", 0},
		}
		for _, step := range steps {
			healthMu.Lock()
			healthErr = step.err
			healthMu.Unlock()
			time.Sleep(step.advance)
			synctest.Wait()
			entries := sidecarLogs(t, &buf)
			if len(entries) != step.logs {
				t.Fatalf("%s: got %d logs, want %d: %#v", step.name, len(entries), step.logs, entries)
			}
			entry := entries[len(entries)-1]
			if entry.Level != step.level || entry.Sidecar != "seednote" || entry.SuppressedFailures != step.suppressed {
				t.Fatalf("%s: unexpected event: %#v", step.name, entry)
			}
			if step.err != nil && entry.Error != step.err.Error() {
				t.Fatalf("%s: error = %q, want %q", step.name, entry.Error, step.err.Error())
			}
			if step.name == "recovered" && entry.Message != "sidecar recovered" {
				t.Fatalf("recovery message = %q", entry.Message)
			}
			if monitor.Ready() != (step.err == nil) {
				t.Fatalf("%s: suppressed logging changed readiness", step.name)
			}
		}
	})
}

func TestSidecarMonitorDoesNotReportShutdownAsOutage(t *testing.T) {
	for _, beforeCheck := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel_before_check_%t", beforeCheck), func(t *testing.T) {
			var buf sidecarLogBuffer
			logger := zerolog.New(&buf)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var stop bool
			var attempts int
			monitor := NewSidecarMonitor(SidecarMonitorConfig{Name: "ilink", HealthCheck: func(ctx context.Context) error {
				attempts++
				if stop {
					cancel()
					return ctx.Err()
				}
				return nil
			}}, &logger)
			if err := monitor.check(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := monitor.Snapshot()
			stop = true
			if beforeCheck {
				cancel()
			}
			monitor.Run(ctx)
			if got := monitor.Snapshot(); got != before {
				t.Fatalf("shutdown changed readiness: before=%#v after=%#v", before, got)
			}
			if buf.Len() != 0 {
				t.Fatalf("shutdown produced outage log: %s", buf.String())
			}
			if beforeCheck && attempts != 1 {
				t.Fatalf("cancelled monitor still checked health: %d attempts", attempts)
			}
		})
	}
}

func TestSidecarMonitorConfiguredFailureLogInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf sidecarLogBuffer
		logger := zerolog.New(&buf)
		monitor := NewSidecarMonitor(SidecarMonitorConfig{
			Name: "seednote", InitialBackoff: 30 * time.Second, MaxBackoff: 30 * time.Second,
			FailureLogInterval: 90 * time.Second,
			HealthCheck:        func(context.Context) error { return errors.New("offline") },
		}, &logger)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go monitor.Run(ctx)
		synctest.Wait()
		if entries := sidecarLogs(t, &buf); len(entries) != 1 {
			t.Fatalf("startup failure must be immediate: %#v", entries)
		}
		time.Sleep(60 * time.Second)
		synctest.Wait()
		if entries := sidecarLogs(t, &buf); len(entries) != 1 {
			t.Fatalf("repeated errors before the interval: %#v", entries)
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if entries := sidecarLogs(t, &buf); len(entries) != 2 || entries[1].SuppressedFailures != 2 {
			t.Fatalf("configured summary interval ignored: %#v", entries)
		}
	})
}

func TestSidecarMonitorCheckTimeoutStillReportsOutage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf sidecarLogBuffer
		logger := zerolog.New(&buf)
		monitor := NewSidecarMonitor(SidecarMonitorConfig{
			Name: "ilink", CheckTimeout: 3 * time.Second, InitialBackoff: time.Minute,
			HealthCheck: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		}, &logger)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go monitor.Run(ctx)
		time.Sleep(3 * time.Second)
		synctest.Wait()
		entries := sidecarLogs(t, &buf)
		if len(entries) != 1 || entries[0].Error != context.DeadlineExceeded.Error() {
			t.Fatalf("health timeout not reported: %#v", entries)
		}
		if snapshot := monitor.Snapshot(); snapshot.Ready || snapshot.LastError != context.DeadlineExceeded.Error() {
			t.Fatalf("timeout snapshot = %#v", snapshot)
		}
	})
}

func TestSidecarMonitorLoggingDoesNotChangeCheckCadence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var checkedAt []time.Duration
		monitor := NewSidecarMonitor(SidecarMonitorConfig{
			InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, HealthyInterval: 10 * time.Second,
			HealthCheck: func(context.Context) error {
				checkedAt = append(checkedAt, time.Since(start))
				if len(checkedAt) <= 3 {
					return errors.New("offline")
				}
				return nil
			},
		}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go monitor.Run(ctx)
		time.Sleep(17 * time.Second)
		synctest.Wait()
		want := []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second, 17 * time.Second}
		if !slices.Equal(checkedAt, want) {
			t.Fatalf("check times = %v, want %v", checkedAt, want)
		}
		if !monitor.Ready() {
			t.Fatal("monitor without logger did not recover")
		}
	})
}

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
