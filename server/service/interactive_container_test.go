package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

func TestContainerRuntimeFactoryUsesHardenedSessionSpec(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := filepath.Join(dir, "docker-fake.sh")
	body := "#!/bin/sh\nif [ \"$1\" = image ] && [ \"$2\" = inspect ]; then printf '%s\\n' 'sha256:" + strings.Repeat("b", 64) + "'; exit 0; fi\nprintf '%s\\n' \"$*\" > " + argsFile + "\nwhile IFS= read -r line; do case \"$line\" in *\\\"id\\\":1*) printf '%s\\n' '{\"id\":1,\"result\":{}}' ;; *shutdown*) exit 0 ;; esac; done\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	factory := ContainerRuntimeFactory{Config: srvconfig.DshConfig{
		DockerBinary: script, Image: "creator-dsh-runtime@sha256:" + strings.Repeat("a", 64), Network: "creator-runtime-network",
		ProjectMemoryVolume: "creator-project-memory", ProjectMemoryMount: "/workspace/project", ServerURL: "http://server:8080",
		MemoryMB: 128, CPUCores: 1, PidsLimit: 64,
	}, ServerInstanceID: "server-a", Provider: "deepseek-official", API: "deepseek-messages", BaseURL: "https://example.invalid", Model: "model"}
	rt, err := factory.StartSession(context.Background(), InteractiveRuntimeSpec{SessionID: "session-1", ProjectID: "project-1", AgentPackID: "project-creator", RuntimeToken: "secret", InitialContext: `{"project_id":"project-1"}`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Command(context.Background(), "session/start", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--cap-drop ALL", "--security-opt no-new-privileges:true", "--read-only", "--pids-limit 64", "--label com.anban.runtime=dsh-interactive", "--label com.anban.preset_id=project-creator", "ANBAN_DSH_BRIDGE_PRESET_ID=project-creator", "ANBAN_DSH_BRIDGE_PATCH=/app/harness/dsh/project-creator.patch.yml", "ANBAN_DSH_BRIDGE_INITIAL_CONTEXT_FILE=/tmp/anban-dsh-initial-context", "destination=/tmp/anban-dsh-initial-context,readonly", "creator-dsh-runtime@sha256:"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("docker command missing %q: %s", want, data)
		}
	}
}

func TestInteractivePresetPatchPathAllowsOnlyAssistantPresets(t *testing.T) {
	for presetID, want := range map[string]string{
		"project-creator":   "/app/harness/dsh/project-creator.patch.yml",
		"creation-planner":  "/app/harness/dsh/creation-planner.patch.yml",
		"postmortem-review": "/app/harness/dsh/postmortem-review.patch.yml",
	} {
		got, err := interactivePresetPatchPath(presetID)
		if err != nil || got != want {
			t.Errorf("interactivePresetPatchPath(%q) = %q, %v; want %q", presetID, got, err, want)
		}
	}
	if _, err := interactivePresetPatchPath("unknown-preset"); err == nil {
		t.Fatal("unknown preset was accepted")
	}
}

func TestContainerRuntimeFactoryRejectsUnknownPresetBeforeLaunching(t *testing.T) {
	factory := ContainerRuntimeFactory{}
	if _, err := factory.StartSession(context.Background(), InteractiveRuntimeSpec{
		SessionID: "session-1", AgentPackID: "unknown-preset", RuntimeToken: "token",
	}, nil); err == nil || !strings.Contains(err.Error(), "unsupported interactive DSH preset") {
		t.Fatalf("StartSession() error = %v, want unsupported preset", err)
	}
}

func TestContainerRuntimeFactoryUsesEphemeralMemoryForCreateProject(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := filepath.Join(dir, "docker-fake.sh")
	body := "#!/bin/sh\nif [ \"$1\" = image ] && [ \"$2\" = inspect ]; then printf '%s\\n' 'sha256:" + strings.Repeat("b", 64) + "'; exit 0; fi\nprintf '%s\\n' \"$*\" > " + argsFile + "\nwhile IFS= read -r line; do case \"$line\" in *\\\"id\\\":1*) printf '%s\\n' '{\"id\":1,\"result\":{}}' ;; *shutdown*) exit 0 ;; esac; done\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	factory := ContainerRuntimeFactory{Config: srvconfig.DshConfig{
		DockerBinary: script, Image: "creator-dsh-runtime@sha256:" + strings.Repeat("a", 64), Network: "creator-runtime-network",
		ProjectMemoryVolume: "creator-project-memory", ProjectMemoryMount: "/workspace/project", ServerURL: "http://server:8080",
		MemoryMB: 128, CPUCores: 1, PidsLimit: 64,
	}, ServerInstanceID: "server-a", Provider: "deepseek-official", API: "deepseek-messages", BaseURL: "https://example.invalid", Model: "model"}
	rt, err := factory.StartSession(context.Background(), InteractiveRuntimeSpec{SessionID: "session-create", AgentPackID: "project-creator", RuntimeToken: "secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Command(context.Background(), "session/start", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "volume-subpath=projects/") {
		t.Fatalf("create-project session unexpectedly mounted a project subpath: %s", data)
	}
}
