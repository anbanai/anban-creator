package mcp

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeNotifier counts NotifyProgress calls. Implements progressNotifier.
type fakeNotifier struct {
	calls atomic.Int32
}

func (f *fakeNotifier) NotifyProgress(_ context.Context, _ *mcp.ProgressNotificationParams) error {
	f.calls.Add(1)
	return nil
}

// TestStartProgressHeartbeat_NoToken verifies the helper becomes a no-op when
// the client did not send a progress token. This is the graceful-degradation
// path: if the client doesn't support progress notifications, the heartbeat
// must not spawn a goroutine that would block on nil.
func TestStartProgressHeartbeat_NoToken(t *testing.T) {
	fake := &fakeNotifier{}
	before := runtime.NumGoroutine()

	stop := startProgressHeartbeat(context.Background(), fake, nil, "write_article", 10*time.Millisecond)
	if stop == nil {
		t.Fatal("expected non-nil stop function even for no-op")
	}
	stop()

	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("no-op path leaked goroutines: before=%d after=%d", before, after)
	}
	if got := fake.calls.Load(); got != 0 {
		t.Errorf("expected 0 NotifyProgress calls for no-op, got %d", got)
	}
}

// TestStartProgressHeartbeat_NilSession verifies the helper is also a no-op
// when the session is nil (e.g., request arrived without a bound session).
func TestStartProgressHeartbeat_NilSession(t *testing.T) {
	before := runtime.NumGoroutine()

	stop := startProgressHeartbeat(context.Background(), nil, "tok-1", "write_article", 10*time.Millisecond)
	stop()

	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("nil-session path leaked goroutines: before=%d after=%d", before, after)
	}
}

// TestStartProgressHeartbeat_NonPositiveInterval is a no-op to avoid a
// runaway ticker.
func TestStartProgressHeartbeat_NonPositiveInterval(t *testing.T) {
	fake := &fakeNotifier{}
	stop := startProgressHeartbeat(context.Background(), fake, "tok-1", "write_article", 0)
	stop()
	if got := fake.calls.Load(); got != 0 {
		t.Errorf("expected 0 calls for zero interval, got %d", got)
	}
}

// TestStartProgressHeartbeat_FiresOnTick verifies the goroutine actually
// pushes notifications at each tick and stops cleanly when the returned stop
// function is called.
func TestStartProgressHeartbeat_FiresOnTick(t *testing.T) {
	fake := &fakeNotifier{}
	interval := 20 * time.Millisecond

	before := runtime.NumGoroutine()
	stop := startProgressHeartbeat(context.Background(), fake, "tok-1", "write_article", interval)

	// Wait long enough for ~3 ticks. Generous slack so CI flake stays low.
	time.Sleep(75 * time.Millisecond)
	stop()

	got := fake.calls.Load()
	if got < 2 {
		t.Errorf("expected >=2 NotifyProgress calls in 75ms with 20ms interval, got %d", got)
	}

	// Goroutine should exit promptly after stop.
	deadline := time.Now().Add(500 * time.Millisecond)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutine leaked after stop: before=%d after=%d", before, after)
	}
}

// TestStartProgressHeartbeat_StopsOnContextCancel verifies the goroutine also
// exits when the request context is cancelled (e.g., client disconnects).
func TestStartProgressHeartbeat_StopsOnContextCancel(t *testing.T) {
	fake := &fakeNotifier{}
	ctx, cancel := context.WithCancel(context.Background())

	before := runtime.NumGoroutine()
	stop := startProgressHeartbeat(ctx, fake, "tok-1", "write_article", 20*time.Millisecond)
	defer stop() // Defensive: stop() should also be safe to call after ctx cancel.

	time.Sleep(30 * time.Millisecond) // at least one tick
	cancel()

	deadline := time.Now().Add(500 * time.Millisecond)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutine leaked after ctx cancel: before=%d after=%d", before, after)
	}
}

// TestStartProgressHeartbeat_StopIsIdempotent verifies the returned stop
// function can be called multiple times without panicking. Important because
// callers may legitimately double-defer (e.g., explicit stop + defer stop).
// Regression guard for the close-on-already-closed-channel panic.
func TestStartProgressHeartbeat_StopIsIdempotent(t *testing.T) {
	fake := &fakeNotifier{}
	stop := startProgressHeartbeat(context.Background(), fake, "tok-1", "write_article", time.Second)

	stop()
	stop()
	stop()
}
