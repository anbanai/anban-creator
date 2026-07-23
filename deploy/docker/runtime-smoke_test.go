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
		if !strings.Contains(string(output), "SKIP") || !strings.Contains(string(output), "Docker CLI") {
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
	const taskID = "11111111-1111-4111-8111-111111111111"
	const projectID = "22222222-2222-4222-8222-222222222222"
	stateFile := filepath.Join(testDir, "queries")
	if err := os.WriteFile(stateFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspectLog := filepath.Join(testDir, "inspect.log")
	dockerPath := filepath.Join(testDir, "docker")
	docker := `#!/bin/sh
printf '%s\n' "$*" >>"$TEST_INSPECT_LOG"
if [ "$1 $2 $3" != "container inspect workload-2" ]; then
  exit 1
fi
printf '%s\n' "[{\"Id\":\"instance-2\",\"Image\":\"sha256:resolved-attempt-2\",\"State\":{\"Running\":true},\"Config\":{\"Labels\":{\"anban.ai/execution-id\":\"execution-2\",\"anban.ai/task-id\":\"$TEST_TASK_ID\",\"anban.ai/project-id\":\"$TEST_PROJECT_ID\"}}}]"
`
	if err := os.WriteFile(dockerPath, []byte(docker), 0o755); err != nil {
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
row=$(poll_execution_identity "$TEST_TASK_ID" 2 "$TEST_TASK_ID" "$TEST_PROJECT_ID")
printf '%s|queries=%s' "$row" "$(cat "$TEST_STATE_FILE")"
`
	output, exitCode := runtimeSmokeShell(t, body,
		"TEST_STATE_FILE="+stateFile,
		"TEST_INSPECT_LOG="+inspectLog,
		"TEST_TASK_ID="+taskID,
		"TEST_PROJECT_ID="+projectID,
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

func TestRuntimeSmokeRejectsMalformedTaskIDBeforeSQL(t *testing.T) {
	sqlCalled := filepath.Join(t.TempDir(), "sql-called")
	body := `
MYSQL_ROOT_PASSWORD=test-password
compose() { : >"$TEST_SQL_CALLED"; }
query_execution "not-a-uuid' OR 1=1 --"
`
	output, exitCode := runtimeSmokeShell(t, body, "TEST_SQL_CALLED="+sqlCalled)
	if exitCode == 0 {
		t.Fatalf("malformed task ID unexpectedly reached SQL: %q", output)
	}
	if _, err := os.Stat(sqlCalled); !os.IsNotExist(err) {
		t.Fatalf("malformed task ID invoked Compose/SQL: %v", err)
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
	dockerPath := filepath.Join(testDir, "docker")
	docker := `#!/bin/sh
if [ "$1 $2" = "container ls" ]; then
  printf '%s\n' 'container-1'
elif [ "$1 $2" = "container inspect" ]; then
  printf '%s\n' 'task-1'
elif [ "$1 $2 $3" = "container rm -f" ]; then
  exit 73
fi
`
	if err := os.WriteFile(dockerPath, []byte(docker), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
CLEANUP_MAX_ATTEMPTS=2
CLEANUP_INTERVAL_SECONDS=0
SMOKE_DIR="$TEST_SMOKE_DIR"
COMPOSE_FILE=
TASK_IDS='task-1'
PROJECT_IDS=
cleanup 0
`
	output, exitCode := runtimeSmokeShell(t, body,
		"TEST_SMOKE_DIR="+smokeDir,
		"PATH="+testDir+":"+os.Getenv("PATH"),
	)
	if exitCode == 0 {
		t.Fatalf("successful run hid labeled resource removal failure: %q", output)
	}
	if _, err := os.Stat(smokeDir); !os.IsNotExist(err) {
		t.Fatalf("resource removal failure prevented temp removal: %v", err)
	}
}

func TestRuntimeSmokeCleanupStopsServerThenRemovesLateProjectAndTaskResources(t *testing.T) {
	testDir := t.TempDir()
	smokeDir := filepath.Join(testDir, "anban-runtime-smoke.test")
	if err := os.MkdirAll(smokeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(smokeDir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(testDir, "cleanup.log")
	stoppedPath := filepath.Join(testDir, "server-stopped")
	containerState := filepath.Join(testDir, "container-state")
	volumeState := filepath.Join(testDir, "volume-state")
	body := `
CLEANUP_MAX_ATTEMPTS=3
CLEANUP_INTERVAL_SECONDS=0
SMOKE_DIR="$TEST_SMOKE_DIR"
COMPOSE_FILE="$TEST_COMPOSE_FILE"
COMPOSE_PROJECT=runtime-smoke-test
PROJECT_IDS="$TEST_PROJECT_ID"
TASK_IDS="$TEST_TASK_ID"
compose() {
  printf 'compose %s\n' "$*" >>"$TEST_CLEANUP_LOG"
  if [[ "$1 $2" == "stop server" ]]; then
    : >"$TEST_SERVER_STOPPED"
    printf '%s\n' late-container >"$TEST_CONTAINER_STATE"
    printf '%s\n' late-volume >"$TEST_VOLUME_STATE"
  elif [[ "$1" == "down" ]]; then
    [[ ! -s "$TEST_CONTAINER_STATE" && ! -s "$TEST_VOLUME_STATE" ]]
  fi
}
docker() {
  printf 'docker %s\n' "$*" >>"$TEST_CLEANUP_LOG"
  [[ -f "$TEST_SERVER_STOPPED" ]] || return 88
  local last="${!#}"
  if [[ "$1 $2" == "container ls" ]]; then
    [[ "$last" == "label=anban.ai/project-id=$TEST_PROJECT_ID" && -s "$TEST_CONTAINER_STATE" ]] && cat "$TEST_CONTAINER_STATE"
  elif [[ "$1 $2" == "container inspect" ]]; then
    printf '%s\n' "$TEST_PROJECT_ID"
  elif [[ "$1 $2 $3" == "container rm -f" ]]; then
    : >"$TEST_CONTAINER_STATE"
  elif [[ "$1 $2" == "volume ls" ]]; then
    [[ "$last" == "label=anban.ai/task-id=$TEST_TASK_ID" && -s "$TEST_VOLUME_STATE" ]] && cat "$TEST_VOLUME_STATE"
  elif [[ "$1 $2" == "volume inspect" ]]; then
    printf '%s\n' "$TEST_TASK_ID"
  elif [[ "$1 $2" == "volume rm" ]]; then
    : >"$TEST_VOLUME_STATE"
  fi
  return 0
}
cleanup 0
`
	const projectID = "22222222-2222-4222-8222-222222222222"
	const taskID = "11111111-1111-4111-8111-111111111111"
	output, exitCode := runtimeSmokeShell(t, body,
		"TEST_SMOKE_DIR="+smokeDir,
		"TEST_COMPOSE_FILE="+composeFile,
		"TEST_CLEANUP_LOG="+logPath,
		"TEST_SERVER_STOPPED="+stoppedPath,
		"TEST_CONTAINER_STATE="+containerState,
		"TEST_VOLUME_STATE="+volumeState,
		"TEST_PROJECT_ID="+projectID,
		"TEST_TASK_ID="+taskID,
	)
	if exitCode != 0 {
		t.Fatalf("quiesce-first cleanup exited %d: %s", exitCode, output)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logData)
	if !strings.HasPrefix(log, "compose stop server\n") {
		t.Fatalf("cleanup did not stop server first:\n%s", log)
	}
	for _, want := range []string{
		"container ls -aq --filter label=anban.ai/project-id=" + projectID,
		"container ls -aq --filter label=anban.ai/task-id=" + taskID,
		"volume ls -q --filter label=anban.ai/project-id=" + projectID,
		"volume ls -q --filter label=anban.ai/task-id=" + taskID,
		"container rm -f late-container",
		"volume rm late-volume",
		"compose down -v --remove-orphans",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("cleanup log missing %q:\n%s", want, log)
		}
	}
	if _, err := os.Stat(smokeDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup left temp dir behind: %v", err)
	}
}

func TestRuntimeSmokeMakeReachesDockerOnlySkipBeforeAnyBuild(t *testing.T) {
	repoRoot := runtimeSmokeRepoRoot(t)
	binDir := t.TempDir()
	if err := os.Symlink("/bin/bash", filepath.Join(binDir, "bash")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "--no-print-directory", "docker-runtime-smoke")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+binDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Docker-only smoke skip failed before script: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "SKIP: Docker CLI is not installed") {
		t.Fatalf("Docker-only smoke output = %q, want script-owned skip", output)
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
	for _, forbidden := range []string{"DOCKER_" + "CLI", "Docker-" + "compatible", "ANBAN_RUNTIME_SMOKE_" + "SKIP_IMAGE_BUILD"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("runtime-smoke.sh contains unsupported portability contract %q", forbidden)
		}
	}
}

func TestRuntimeSmokeMakeTargetDelegatesBuildsToDockerOnlyScript(t *testing.T) {
	repoRoot := runtimeSmokeRepoRoot(t)
	makeData, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	scriptData, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker", "runtime-smoke.sh"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(makeData)
	script := string(scriptData)
	for _, want := range []string{".PHONY: docker-runtime-smoke", "docker-runtime-smoke:\n\t@deploy/docker/runtime-smoke.sh"} {
		if !strings.Contains(makefile, want) {
			t.Errorf("Makefile missing runtime smoke contract %q", want)
		}
	}
	for _, forbidden := range []string{"DOCKER_RUNTIME_SMOKE_" + "DEPS", "DOCKER_" + "CLI", "ANBAN_RUNTIME_SMOKE_" + "SKIP_IMAGE_BUILD"} {
		if strings.Contains(makefile, forbidden) {
			t.Errorf("Makefile contains unsupported runtime smoke indirection %q", forbidden)
		}
	}
	for _, dockerfile := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		if !strings.Contains(script, "docker build") || !strings.Contains(script, dockerfile) {
			t.Errorf("runtime smoke script does not own Docker build for %s", dockerfile)
		}
	}
}
