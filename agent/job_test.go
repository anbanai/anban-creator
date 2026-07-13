package main

import (
	"context"
	"io"
	"reflect"
	"testing"
)

func TestJobCommandBootstrapsBeforeRunAndMapsConfig(t *testing.T) {
	var order []string
	bootstrap := func(_ context.Context, cfg JobConfig) (*BootstrapResponse, error) {
		order = append(order, "bootstrap")
		if cfg.ExecutionID != "execution-1" || cfg.WorkloadTokenFile != "/token" {
			t.Fatalf("job config=%+v", cfg)
		}
		return &BootstrapResponse{ExecutionToken: "jwt", TaskID: "task-1", TaskType: "article", ProjectID: "project-1", Prompt: "write", Model: "sonnet", MaxTurns: 9, AgentFlag: "anban:wechatarticle", AutoMemoryDirectory: ".claude/memory"}, nil
	}
	run := func(_ context.Context, cfg *Config) error {
		order = append(order, "run")
		if cfg.APIKey != "jwt" || cfg.ExecutionID != "execution-1" || cfg.TaskID != "task-1" || cfg.Topic != "write" || cfg.ArtifactUploadMode != ArtifactUploadDirect {
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
