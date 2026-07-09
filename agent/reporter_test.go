package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

// newTestReporter points a real *Reporter at an in-memory HTTP transport,
// returning the reporter and a counter of requests received (across all paths).
func newTestReporter(t *testing.T, handler http.HandlerFunc) (*Reporter, *int32) {
	t.Helper()
	var hits int32
	rep := NewReporter(&Config{ServerURL: "http://agent.test", APIKey: "k", TaskID: "t1"})
	rep.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&hits, 1)
		rr := httptest.NewRecorder()
		handler(rr, r)
		return rr.Result(), nil
	})}
	return rep, &hits
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// withFastBackoff shrinks the retry backoff for the duration of a test so retry
// behavior is exercised without slowing the suite by seconds.
func withFastBackoff(t *testing.T) {
	t.Helper()
	prev := reportRetryBaseBackoff
	reportRetryBaseBackoff = time.Millisecond
	t.Cleanup(func() { reportRetryBaseBackoff = prev })
}

func TestReportHeartbeatUpdatesProgressWithoutLogMessage(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	rep, hits := newTestReporter(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	if err := rep.ReportHeartbeat(context.Background()); err != nil {
		t.Fatalf("ReportHeartbeat: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected one heartbeat request, got %d", got)
	}
	if gotPath != "/api/v1/agent/progress" {
		t.Fatalf("path = %q, want /api/v1/agent/progress", gotPath)
	}
	if gotBody["task_id"] != "t1" {
		t.Fatalf("task_id = %v, want t1", gotBody["task_id"])
	}
	if _, ok := gotBody["message"]; ok {
		t.Fatalf("heartbeat should not include message: %#v", gotBody)
	}
}

func TestPrepareArtifactUploadPostsRequestAndDecodesEnvelope(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody ArtifactPrepareRequest
	rep, _ := newTestReporter(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{
				"key":                   "uploads/users/u/projects/p/tasks/t1/artifacts/output/article.md",
				"bucket":                "bucket",
				"endpoint":              "oss-cn-hangzhou.aliyuncs.com",
				"headers":               map[string]string{"Content-Type": "text/markdown"},
				"sts_access_key_id":     "sts-ak",
				"sts_access_key_secret": "sts-secret",
				"sts_security_token":    "sts-token",
				"max_size":              512,
			},
		})
	})

	got, err := rep.PrepareArtifactUpload(context.Background(), ArtifactPrepareRequest{
		TaskID:       "t1",
		RelativePath: "output/article.md",
		Filename:     "article.md",
		ContentType:  "text/markdown",
		Size:         9,
		SHA256:       strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("PrepareArtifactUpload: %v", err)
	}
	if gotPath != "/api/v1/agent/artifacts/prepare" || gotAuth != "Bearer k" {
		t.Fatalf("path/auth = %q/%q, want prepare endpoint with bearer auth", gotPath, gotAuth)
	}
	if gotBody.TaskID != "t1" || gotBody.RelativePath != "output/article.md" {
		t.Fatalf("request body = %+v, want task and relative path", gotBody)
	}
	if got.Key == "" || got.STSAccessKeyID != "sts-ak" || got.STSSecurityToken != "sts-token" {
		t.Fatalf("decoded prepare response = %+v, want key and sts credentials", got)
	}
}

func TestReportArtifactManifestPostsEndpoint(t *testing.T) {
	var gotPath string
	var gotBody ArtifactManifestRequest
	rep, _ := newTestReporter(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	err := rep.ReportArtifactManifest(context.Background(), ArtifactManifestRequest{
		TaskID: "t1",
		Files: []ArtifactManifestFile{{
			RelativePath: "output/article.md",
			ObjectKey:    "uploads/users/u/projects/p/tasks/t1/artifacts/output/article.md",
			ContentType:  "text/markdown",
			Size:         9,
			SHA256:       strings.Repeat("a", 64),
		}},
	})
	if err != nil {
		t.Fatalf("ReportArtifactManifest: %v", err)
	}
	if gotPath != "/api/v1/agent/artifacts/manifest" || gotBody.TaskID != "t1" || len(gotBody.Files) != 1 {
		t.Fatalf("manifest request = path %q body %+v, want one file on manifest endpoint", gotPath, gotBody)
	}
}

// TestReportComplete_RetriesTransientThenSucceeds: a transient 5xx must be
// retried and eventually succeed once the server recovers.
func TestReportComplete_RetriesTransientThenSucceeds(t *testing.T) {
	withFastBackoff(t)
	var n int32
	rep, hits := newTestReporter(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway) // 502 → retryable
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := rep.ReportComplete(context.Background(), &serveragent.ExecutionResult{Success: true}); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 3 {
		t.Fatalf("expected 3 attempts (2 retries), got %d", got)
	}
}

// TestReportComplete_NoRetryOnClientError: a non-retryable 4xx (410 — task
// already force-finalized) must fail on the FIRST attempt. Retrying it would
// stall the agent up to ~6s for a guaranteed-identical failure.
func TestReportComplete_NoRetryOnClientError(t *testing.T) {
	withFastBackoff(t)
	rep, hits := newTestReporter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	})
	err := rep.ReportComplete(context.Background(), &serveragent.ExecutionResult{Success: true})
	if err == nil {
		t.Fatal("expected error on HTTP 410")
	}
	var he *httpStatusError
	if !errors.As(err, &he) || he.code != http.StatusGone {
		t.Fatalf("expected httpStatusError{410}, got %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("non-retryable 4xx must NOT be retried; got %d attempts", got)
	}
}

// TestReportComplete_GivesUpAfterMaxAttempts: a persistent 5xx must exhaust the
// retry budget (3 attempts) and then surface the last error.
func TestReportComplete_GivesUpAfterMaxAttempts(t *testing.T) {
	withFastBackoff(t)
	rep, hits := newTestReporter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // 503 → retryable, never recovers
	})
	err := rep.ReportComplete(context.Background(), &serveragent.ExecutionResult{Success: true})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(hits); got != 3 {
		t.Fatalf("expected exactly 3 attempts then give up, got %d", got)
	}
}
