package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestJobCommandDoesNotRunAfterInvalidBootstrapResponse(t *testing.T) {
	t.Setenv(jobFinalizationTimeoutEnv, "60ms")
	previousReserve := jobCompletionReserve
	jobCompletionReserve = 20 * time.Millisecond
	t.Cleanup(func() { jobCompletionReserve = previousReserve })
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("workload"), 0o600); err != nil {
		t.Fatal(err)
	}
	completed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/complete" {
			completed = true
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v1/agent/progress" {
			time.Sleep(50 * time.Millisecond)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"execution_token": testExecutionToken(t, "execution-1", "task-1", "project-1"),
			"task_id":         "task-1", "task_type": "", "project_id": "project-1", "prompt": "write",
			"max_turns": 40, "agent_flag": "", "auto_memory_directory": ".claude/memory",
		}})
	}))
	defer server.Close()
	ran := false
	cmd := newJobCommand(testBootstrapJob, func(context.Context, *Config) error { ran = true; return nil })
	err := cmd.Run(context.Background(), []string{"job", "--server-url", server.URL, "--execution-id", "execution-1", "--workspace", t.TempDir(), "--workload-token-file", tokenFile})
	if err == nil || ran || !completed {
		t.Fatalf("err=%v ran=%v completed=%v", err, ran, completed)
	}
}

func TestJobCommandBootstrapsBeforeRunAndMapsConfig(t *testing.T) {
	var order []string
	bootstrap := func(_ context.Context, cfg JobConfig) (*BootstrapResponse, error) {
		order = append(order, "bootstrap")
		if cfg.ExecutionID != "execution-1" || cfg.WorkloadTokenFile != "/token" {
			t.Fatalf("job config=%+v", cfg)
		}
		return &BootstrapResponse{ExecutionToken: "jwt", TaskID: "task-1", TaskType: "article", ProjectID: "project-1", Prompt: "write", Model: "sonnet", MaxTurns: 9, AgentFlag: "anban:wechatarticle", AutoMemoryDirectory: ".claude/memory", RuntimeEnv: map[string]string{
			"ANTHROPIC_AUTH_TOKEN": "runtime-token",
			"ANBAN_API_KEY":        "must-not-override",
		}}, nil
	}
	run := func(_ context.Context, cfg *Config) error {
		order = append(order, "run")
		if cfg.APIKey != "jwt" || cfg.ExecutionID != "execution-1" || cfg.TaskID != "task-1" || cfg.Topic != "write" || cfg.ArtifactUploadMode != ArtifactUploadDirect || cfg.RuntimeEnv["ANTHROPIC_AUTH_TOKEN"] != "runtime-token" || len(cfg.RuntimeEnv) != 1 {
			t.Fatalf("runtime config=%+v", cfg)
		}
		return nil
	}
	cmd := newJobCommand(bootstrap, run)
	if err := cmd.Run(context.Background(), []string{"job", "--server-url", "http://server", "--execution-id", "execution-1", "--workspace", "/workspace", "--workload-token-file", "/token"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"bootstrap", "run"}) {
		t.Fatalf("order=%v", order)
	}
}

func TestJobCommandAcceptsOnlyBootstrapFlags(t *testing.T) {
	cmd := newJobCommand(func(context.Context, JobConfig) (*BootstrapResponse, error) { return &BootstrapResponse{}, nil }, func(context.Context, *Config) error { return nil })
	names := make(map[string]bool)
	for _, name := range cmd.FlagNames() {
		names[name] = true
	}
	for _, forbidden := range []string{"api-key", "task-id", "task-type", "model", "max-turns", "artifact-upload-mode"} {
		if names[forbidden] {
			t.Fatalf("job exposes forbidden flag %q", forbidden)
		}
	}
	w := io.Discard
	cmd.Writer, cmd.ErrWriter = w, w
}

func TestFinalizationContextIsBoundedForJob(t *testing.T) {
	t.Setenv(jobFinalizationTimeoutEnv, "40ms")
	previousReserve := jobCompletionReserve
	jobCompletionReserve = 15 * time.Millisecond
	t.Cleanup(func() { jobCompletionReserve = previousReserve })
	window := newFinalizationWindow(&Config{ExecutionID: "execution-1"})
	workCtx, cancelWork := window.workContext()
	defer cancelWork()
	if _, ok := workCtx.Deadline(); !ok {
		t.Fatal("job finalization context has no deadline")
	}
	select {
	case <-workCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("job pre-completion context did not time out")
	}
	completeCtx, cancelComplete := window.completionContext()
	defer cancelComplete()
	if completeCtx.Err() != nil {
		t.Fatalf("completion reserve was already expired: %v", completeCtx.Err())
	}
}

func TestFinalizationContextCanBeCancelled(t *testing.T) {
	ctx, cancel := newFinalizationWindow(&Config{ExecutionID: "execution-1"}).workContext()
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("cancel did not stop finalization context")
	}
}
