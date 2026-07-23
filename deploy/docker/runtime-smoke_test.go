package docker

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	serverconfig "github.com/anbanai/anban-creator/server/config"
)

func runtimeSmokeRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestRuntimeSmokeDockerAbsenceIsTheOnlySuccessfulSkip(t *testing.T) {
	repoRoot := runtimeSmokeRepoRoot(t)
	script := filepath.Join(repoRoot, "deploy", "docker", "runtime-smoke.sh")

	t.Run("missing Docker CLI skips", func(t *testing.T) {
		cmd := exec.Command("/bin/bash", script)
		cmd.Dir = repoRoot
		cmd.Env = []string{"PATH="}
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("missing Docker CLI returned %v: %s", err, output)
		}
		if !strings.Contains(string(output), "SKIP") || !strings.Contains(string(output), "Docker-compatible CLI") {
			t.Fatalf("missing Docker CLI output = %q, want explicit skip", output)
		}
	})

	t.Run("missing runtime config fails", func(t *testing.T) {
		binDir := t.TempDir()
		dockerPath := filepath.Join(binDir, "docker")
		if err := os.WriteFile(dockerPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("/bin/bash", script)
		cmd.Dir = repoRoot
		env := make([]string, 0, len(os.Environ())+2)
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "PATH=") && !strings.HasPrefix(item, "CLAUDE_CODE_AUTH_TOKEN=") {
				env = append(env, item)
			}
		}
		cmd.Env = append(env, "PATH="+binDir+":"+os.Getenv("PATH"), "CLAUDE_CODE_AUTH_TOKEN=")
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("missing runtime config unexpectedly succeeded: %s", output)
		}
		if strings.Contains(string(output), "SKIP") || !strings.Contains(string(output), "CLAUDE_CODE_AUTH_TOKEN") {
			t.Fatalf("missing runtime config output = %q, want explicit non-skip failure", output)
		}
	})
}

func TestRuntimeSmokeGeneratedServerConfigIsValid(t *testing.T) {
	repoRoot := runtimeSmokeRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker", "runtime-smoke.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const startMarker = "cat >\"$CONFIG_FILE\" <<'YAML'\n"
	start := strings.Index(string(data), startMarker)
	if start < 0 {
		t.Fatal("runtime smoke server config heredoc is missing")
	}
	configBody := string(data)[start+len(startMarker):]
	end := strings.Index(configBody, "\nYAML\n")
	if end < 0 {
		t.Fatal("runtime smoke server config heredoc is unterminated")
	}
	configBody = configBody[:end]
	configBody = strings.Replace(configBody, "/app/conf/billing", filepath.Join(repoRoot, "server", "billing"), 1)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_AUTH_TOKEN", "runtime-smoke-provider-token")
	t.Setenv("ANBAN_RUNTIME_SMOKE_NETWORK", "runtime-smoke-network")
	if _, err := serverconfig.NewConfig(configPath); err != nil {
		t.Fatalf("generated runtime smoke server config is invalid: %v", err)
	}
}

func TestRuntimeSmokeScriptCoversManagedDockerLifecycle(t *testing.T) {
	repoRoot := runtimeSmokeRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker", "runtime-smoke.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	required := []string{
		"set -euo pipefail",
		"docker compose", "up -d", "creator-agent-article:latest",
		"creator-agent-seednote:latest", "creator-agent-montage:latest",
		"/api/v1/auth/register", "/api/admin/billing/topups", "/api/v1/projects", "/api/v1/tasks",
		"/resume", "runtime_workload", "runtime_image", "output/",
		"docker volume inspect", "docker container inspect",
		"anban.ai/task-id", "anban.ai/project-id", "anban.ai/execution-id",
		"poll_task", "POLL_DEADLINE_SECONDS", "cleanup",
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Errorf("runtime-smoke.sh missing %q", want)
		}
	}
	if strings.Contains(script, "while true") {
		t.Error("runtime-smoke.sh contains unbounded polling")
	}
	if strings.Count(script, "create_task ") < 3 {
		t.Error("runtime-smoke.sh must submit one task for each managed profile")
	}
}

func TestRuntimeSmokeMakeTargetBuildsAllManagedImages(t *testing.T) {
	repoRoot := runtimeSmokeRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(data)
	for _, want := range []string{".PHONY: docker-runtime-smoke", "docker-runtime-smoke: $(DOCKER_RUNTIME_SMOKE_DEPS)", "deploy/docker/runtime-smoke.sh"} {
		if !strings.Contains(makefile, want) {
			t.Errorf("Makefile missing runtime smoke contract %q", want)
		}
	}
	depsStart := strings.Index(makefile, "DOCKER_RUNTIME_SMOKE_DEPS :=")
	if depsStart < 0 {
		t.Fatal("Makefile runtime smoke dependency declaration is missing")
	}
	depsEnd := strings.Index(makefile[depsStart:], "\n")
	if depsEnd < 0 {
		t.Fatal("Makefile runtime smoke dependency declaration is missing")
	}
	deps := makefile[depsStart : depsStart+depsEnd]
	for _, want := range []string{"docker-agent-image", "docker-seednote-agent-image", "docker-montage-agent-image"} {
		if !strings.Contains(deps, want) {
			t.Errorf("runtime smoke dependencies missing %q", want)
		}
	}
}
