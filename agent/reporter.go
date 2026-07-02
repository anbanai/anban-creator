package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

// httpStatusError carries the HTTP status of a failed report so the retry layer
// can decide retryability without re-parsing the message string.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("report request failed: HTTP %d", e.code)
}

// isRetryableStatus reports whether an HTTP status warrants a retry. 5xx and the
// two transient 4xx codes (408 request-timeout, 429 rate-limit) may succeed on a
// second attempt; everything else — 400 bad payload, 401 bad key, 404/410 task
// gone, 409 not claimable — is terminal, so retrying only burns ~6s before the
// same failure.
func isRetryableStatus(code int) bool {
	return code >= 500 || code == http.StatusRequestTimeout || code == http.StatusTooManyRequests
}

// reportRetryBaseBackoff is the initial backoff for terminal-report retries.
// Overridden in tests to keep them fast.
var reportRetryBaseBackoff = 2 * time.Second

type Reporter struct {
	cfg    *Config
	client *http.Client
}

func NewReporter(cfg *Config) *Reporter {
	return &Reporter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (r *Reporter) ReportProgress(ctx context.Context, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	return r.postJSON(ctx, "/api/v1/agent/progress", map[string]any{
		"task_id": r.cfg.TaskID,
		"message": message,
	})
}

func (r *Reporter) ReportResult(ctx context.Context, result *serveragent.ExecutionResult) error {
	if result == nil {
		return nil
	}
	return r.postJSON(ctx, "/api/v1/agent/progress", map[string]any{
		"task_id": r.cfg.TaskID,
		"result":  result,
	})
}

// ReportComplete signals terminal completion to the server. The server finalizes
// the task ONLY when it is a local_claimed task still in the running state
// (guarded CAS) — so calling this from the cloud Docker path is a safe no-op
// (cloud's authoritative finalization is server-side HandleExecution). For
// local-execution tasks this is the terminal half of the path: without it the
// task could never reach completed/failed and would be force-failed by the
// stuck-task reaper. Idempotent on the server, so a retry is harmless.
func (r *Reporter) ReportComplete(ctx context.Context, result *serveragent.ExecutionResult) error {
	if result == nil {
		return nil
	}
	// Terminal report for local-execution tasks: the server finalizes the task
	// only on this call (idempotent CAS). A single transient failure (network
	// blip, 5xx) would otherwise leave the task "running" until the stuck-task
	// reaper force-fails + refunds it ~5 min later — the user sees a failed
	// task despite a successful run. Retry with backoff; idempotent, so safe.
	return r.postJSONWithRetry(ctx, "/api/v1/agent/complete", map[string]any{
		"task_id": r.cfg.TaskID,
		"result":  result,
	})
}

// postJSONWithRetry retries a terminal report a bounded number of times with
// exponential backoff. Used only for the idempotent /complete call — progress
// and result reports stay single-shot (a missed line is harmless). Terminal HTTP
// failures (non-retryable 4xx) bail immediately; network errors and 5xx back off
// with ±25% jitter so concurrently-finishing agents don't retry in lockstep.
func (r *Reporter) postJSONWithRetry(ctx context.Context, path string, payload any) error {
	const maxAttempts = 3
	var lastErr error
	backoff := reportRetryBaseBackoff
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = r.postJSON(ctx, path, payload)
		if lastErr == nil {
			return nil
		}
		// A terminal 4xx (not 408/429) won't recover — fail fast instead of
		// backing off. Network errors and 5xx fall through to retry.
		var he *httpStatusError
		if errors.As(lastErr, &he) && !isRetryableStatus(he.code) {
			return lastErr
		}
		if attempt == maxAttempts {
			break
		}
		jitter := time.Duration(rand.Int63n(int64(backoff)/2 + 1))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff/2 + jitter):
		}
		backoff *= 2
	}
	return lastErr
}

func (r *Reporter) postJSON(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal report payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.ServerURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create report request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send report request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return &httpStatusError{code: resp.StatusCode}
	}
	return nil
}
