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

func runtimeSmokeShell(t *testing.T, body string, env ...string) (string, int) {
	t.Helper()
	repoRoot := runtimeSmokeRepoRoot(t)
	script := filepath.Join(repoRoot, "deploy", "docker", "runtime-smoke.sh")
	cmd := exec.Command("/bin/bash", "-c", `source "$1"; eval "$2"`, "runtime-smoke-test", script, body)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(output), exitErr.ExitCode()
	}
	t.Fatalf("run shell harness: %v", err)
	return "", -1
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

func TestRuntimeSmokeProjectCreateUsesActualResponseEnvelope(t *testing.T) {
	valid := `
TOKEN=test-token
PROJECT_IDS=
CREATED_PROJECT_ID=
api_post() { printf '%s\n' '{"code":0,"data":{"project":{"id":"project-1"},"recommended_templates":[]}}'; }
create_project article marker
printf '%s|%s' "$CREATED_PROJECT_ID" "$PROJECT_IDS"
`
	output, exitCode := runtimeSmokeShell(t, valid)
	if exitCode != 0 || strings.TrimSpace(output) != "project-1| project-1" {
		t.Fatalf("actual project response = exit %d output %q", exitCode, output)
	}

	legacy := `
TOKEN=test-token
PROJECT_IDS=
CREATED_PROJECT_ID=
api_post() { printf '%s\n' '{"code":0,"data":{"id":"invented-shape"}}'; }
create_project article marker
`
	output, exitCode = runtimeSmokeShell(t, legacy)
	if exitCode == 0 {
		t.Fatalf("invented project response unexpectedly accepted: %q", output)
	}
}

func TestRuntimeSmokePollWaitsForExpectedAttemptAndInspectsItWhileActive(t *testing.T) {
	testDir := t.TempDir()
	stateFile := filepath.Join(testDir, "queries")
	if err := os.WriteFile(stateFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspectLog := filepath.Join(testDir, "inspect.log")
	podmanPath := filepath.Join(testDir, "podman")
	podman := `#!/bin/sh
printf '%s\n' "$*" >>"$TEST_INSPECT_LOG"
if [ "$1 $2 $3" != "container inspect workload-2" ]; then
  exit 1
fi
printf '%s\n' '[{"Id":"instance-2","Image":"sha256:resolved-attempt-2","State":{"Running":true},"Config":{"Labels":{"anban.ai/execution-id":"execution-2","anban.ai/task-id":"task-1","anban.ai/project-id":"project-1"}}}]'
`
	if err := os.WriteFile(podmanPath, []byte(podman), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
POLL_DEADLINE_SECONDS=3
POLL_INTERVAL_SECONDS=0
query_execution() {
  count=$(cat "$TEST_STATE_FILE")
  count=$((count + 1))
  printf '%s' "$count" >"$TEST_STATE_FILE"
  if [[ "$count" == 1 ]]; then
    printf '%s\n' 'execution-1|1||article|creator-agent-article:latest|docker|workload-1|instance-1|running'
  else
    printf '%s\n' 'execution-2|2|execution-1|article|creator-agent-article:latest|docker|workload-2|instance-2|running'
  fi
}
row=$(poll_execution_identity task-1 2 task-1 project-1)
printf '%s|queries=%s' "$row" "$(cat "$TEST_STATE_FILE")"
`
	output, exitCode := runtimeSmokeShell(t, body,
		"TEST_STATE_FILE="+stateFile,
		"TEST_INSPECT_LOG="+inspectLog,
		"DOCKER_CLI=podman",
		"PATH="+testDir+":"+os.Getenv("PATH"),
	)
	if exitCode != 0 {
		t.Fatalf("attempt-aware poll exited %d: %s", exitCode, output)
	}
	if !strings.Contains(output, "execution-2|2|") || !strings.Contains(output, "sha256:resolved-attempt-2") || !strings.Contains(output, "queries=2") {
		t.Fatalf("attempt-aware poll output = %q", output)
	}
	inspects, err := os.ReadFile(inspectLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(inspects)) != "container inspect workload-2" {
		t.Fatalf("runtime inspect order = %q, want only expected attempt 2", inspects)
	}
}

func TestRuntimeSmokeCleanupPreservesFailureAndAlwaysRemovesTempDir(t *testing.T) {
	smokeDir := filepath.Join(t.TempDir(), "anban-runtime-smoke.test")
	if err := os.MkdirAll(smokeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(smokeDir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := `
SMOKE_DIR="$TEST_SMOKE_DIR"
COMPOSE_FILE="$TEST_COMPOSE_FILE"
TASK_IDS='task-1'
PROJECT_IDS='project-1'
cleanup_labeled_resources() { return 71; }
compose() { return 72; }
trap 'cleanup "$?"' EXIT
exit 42
`
	output, exitCode := runtimeSmokeShell(t, body, "TEST_SMOKE_DIR="+smokeDir, "TEST_COMPOSE_FILE="+composeFile)
	if exitCode != 42 {
		t.Fatalf("cleanup changed original status: exit %d output %q", exitCode, output)
	}
	if _, err := os.Stat(smokeDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup left temp dir behind: %v", err)
	}
}

func TestRuntimeSmokeCleanupFailsSuccessfulRunWhenResourceCleanupFails(t *testing.T) {
	smokeDir := filepath.Join(t.TempDir(), "anban-runtime-smoke.test")
	if err := os.MkdirAll(smokeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(smokeDir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := `
SMOKE_DIR="$TEST_SMOKE_DIR"
COMPOSE_FILE="$TEST_COMPOSE_FILE"
TASK_IDS='task-1'
PROJECT_IDS='project-1'
cleanup_labeled_resources() { return 71; }
compose() { return 72; }
cleanup 0
`
	output, exitCode := runtimeSmokeShell(t, body, "TEST_SMOKE_DIR="+smokeDir, "TEST_COMPOSE_FILE="+composeFile)
	if exitCode == 0 {
		t.Fatalf("successful run hid cleanup failure: %q", output)
	}
	if _, err := os.Stat(smokeDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup failure prevented temp removal: %v", err)
	}
}

func TestRuntimeSmokeLabeledCleanupReportsRemovalFailure(t *testing.T) {
	testDir := t.TempDir()
	smokeDir := filepath.Join(testDir, "anban-runtime-smoke.test")
	if err := os.MkdirAll(smokeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	podmanPath := filepath.Join(testDir, "podman")
	podman := `#!/bin/sh
if [ "$1 $2" = "container ls" ]; then
  printf '%s\n' 'container-1'
elif [ "$1 $2" = "container inspect" ]; then
  printf '%s\n' 'task-1'
elif [ "$1 $2 $3" = "container rm -f" ]; then
  exit 73
fi
`
	if err := os.WriteFile(podmanPath, []byte(podman), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
SMOKE_DIR="$TEST_SMOKE_DIR"
COMPOSE_FILE=
TASK_IDS='task-1'
PROJECT_IDS=
cleanup 0
`
	output, exitCode := runtimeSmokeShell(t, body,
		"TEST_SMOKE_DIR="+smokeDir,
		"DOCKER_CLI=podman",
		"PATH="+testDir+":"+os.Getenv("PATH"),
	)
	if exitCode == 0 {
		t.Fatalf("successful run hid labeled resource removal failure: %q", output)
	}
	if _, err := os.Stat(smokeDir); !os.IsNotExist(err) {
		t.Fatalf("resource removal failure prevented temp removal: %v", err)
	}
}

func TestRuntimeSmokeMakeUsesConfiguredDockerCompatibleCLI(t *testing.T) {
	repoRoot := runtimeSmokeRepoRoot(t)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	writeExecutable := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable("podman", "#!/bin/sh\nprintf 'podman %s\\n' \"$*\" >>\"$FAKE_CALL_LOG\"\n")
	writeExecutable("git", "#!/bin/sh\nprintf 'git %s\\n' \"$*\" >>\"$FAKE_CALL_LOG\"\n")
	env := append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "FAKE_CALL_LOG="+logPath)
	cmd := exec.Command("make", "--no-print-directory", "docker-agent-image", "docker-seednote-agent-image", "docker-montage-agent-image", "DOCKER_CLI=podman")
	cmd.Dir = repoRoot
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("configured CLI image targets failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(data)
	for _, dockerfile := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		if !strings.Contains(calls, "podman build") || !strings.Contains(calls, dockerfile) {
			t.Errorf("configured CLI did not build %s; calls:\n%s", dockerfile, calls)
		}
	}

	dryRun := exec.Command("make", "--no-print-directory", "-n", "docker-runtime-smoke", "DOCKER_CLI=podman")
	dryRun.Dir = repoRoot
	dryRun.Env = env
	output, err := dryRun.CombinedOutput()
	if err != nil {
		t.Fatalf("configured CLI dependency dry-run failed: %v\n%s", err, output)
	}
	for _, dockerfile := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		if !strings.Contains(string(output), dockerfile) {
			t.Errorf("docker-runtime-smoke omitted %s prerequisite with DOCKER_CLI=podman:\n%s", dockerfile, output)
		}
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
