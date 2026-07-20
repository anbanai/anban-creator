package main

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartHeartbeatReportsFailureAndRecovery(t *testing.T) {
	previous := agentHeartbeatInterval
	agentHeartbeatInterval = 5 * time.Millisecond
	t.Cleanup(func() { agentHeartbeatInterval = previous })

	var responses int32
	reporter, hits := newTestReporter(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&responses, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := startHeartbeat(ctx, reporter, &stderr)
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(hits) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	output := stderr.String()
	if !strings.Contains(output, "agent heartbeat failed (1 consecutive)") || !strings.Contains(output, "agent heartbeat restored after 1 failure(s)") {
		t.Fatalf("heartbeat diagnostics = %q", output)
	}
}
