package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	serveragent "github.com/royalrick/anbanwriter/server/agent"
)

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
	return r.postJSON(ctx, "/api/v1/agent/complete", map[string]any{
		"task_id": r.cfg.TaskID,
		"result":  result,
	})
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
		return fmt.Errorf("report request failed: HTTP %d", resp.StatusCode)
	}
	return nil
}
