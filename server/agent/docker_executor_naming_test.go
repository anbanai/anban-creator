package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/client"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestDockerExecutorUsesCreatorAgentRuntimeNames(t *testing.T) {
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}
	task := model.Task{ID: "task-1", Type: model.PlatformArticle, Prompt: "topic"}

	cmd, err := e.buildAgentCommand(&ExecutionOptions{Task: &task}, "sonnet", 100, "/workspace", "key")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmd[0], "anban"; got != want {
		t.Fatalf("agent binary = %q, want %q", got, want)
	}
	if got, want := cmd[1], "run"; got != want {
		t.Fatalf("agent subcommand = %q, want %q", got, want)
	}
	if got, want := flagValue(cmd, "--agent-flag"), "anban:article"; got != want {
		t.Fatalf("--agent-flag = %q, want %q", got, want)
	}

	if got, want := filepath.Base(filepath.Dir(DefaultWorkspaceDir(task.ID))), "anban-creator"; got != want {
		t.Fatalf("default workspace base = %q, want %q", got, want)
	}
	if got, want := EphemeralContainerName(task.ID), "creator-agent-task-task-1"; got != want {
		t.Fatalf("container name = %q, want %q", got, want)
	}
}

func TestDockerResultWorkDirUsesTaskRuntimeRoot(t *testing.T) {
	workspace := t.TempDir()
	if got, want := dockerResultWorkDir(workspace, model.PlatformMontage), filepath.Join(workspace, MontageRuntimeDirName); got != want {
		t.Fatalf("Montage result WorkDir = %q, want %q", got, want)
	}
	if got := dockerResultWorkDir(workspace, model.PlatformArticle); got != workspace {
		t.Fatalf("Article result WorkDir = %q, want %q", got, workspace)
	}

	parsedFailure := &ExecutionResult{Success: false, WorkDir: "/workspace/montage"}
	normalizeDockerResultWorkDir(parsedFailure, workspace, model.PlatformMontage)
	if want := filepath.Join(workspace, MontageRuntimeDirName); parsedFailure.WorkDir != want {
		t.Fatalf("parsed failure WorkDir = %q, want host path %q", parsedFailure.WorkDir, want)
	}
}

func TestDockerAgentCommandTransportsExactModelUsageAliasesInStableOrder(t *testing.T) {
	e := &DockerExecutor{modelUsageAliases: map[string]ModelUsageIdentity{
		"z-raw": {Provider: "volcengine_ark", Model: "z-model"},
		"a-raw": {Provider: "volcengine_ark", Model: "a-model"},
	}}
	task := model.Task{ID: "task-1", Type: model.PlatformArticle}
	cmd, err := e.buildAgentCommand(&ExecutionOptions{Task: &task}, "default", 10, "/workspace", "key")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--model-usage-alias", "a-raw=volcengine_ark/a-model",
		"--model-usage-alias", "z-raw=volcengine_ark/z-model",
	}
	if !strings.Contains(strings.Join(cmd, " "), strings.Join(want, " ")) {
		t.Fatalf("command = %#v, want stable aliases %#v", cmd, want)
	}
}

func TestDockerAgentCommandRejectsInvalidModelUsageAliases(t *testing.T) {
	e := &DockerExecutor{modelUsageAliases: map[string]ModelUsageIdentity{
		"raw": {Provider: "", Model: "canonical"},
	}}
	task := model.Task{ID: "task-1", Type: model.PlatformArticle}
	if _, err := e.buildAgentCommand(&ExecutionOptions{Task: &task}, "default", 10, "/workspace", "key"); err == nil {
		t.Fatal("invalid model usage alias was silently omitted")
	}
}

func TestCleanupOrphanedContainersUsesCurrentNameFilter(t *testing.T) {
	var gotFilters map[string]map[string]bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filters")), &gotFilters); err != nil {
			t.Errorf("decode Docker filters: %v", err)
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
		client.WithVersion("1.44"),
	)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer cli.Close()

	e := &DockerExecutor{dockerCLI: cli}
	e.CleanupOrphanedContainers()

	if want := "^/creator-agent-task-"; !gotFilters["name"][want] {
		t.Errorf("Docker name filters = %#v, want %q", gotFilters["name"], want)
	}
	if len(gotFilters["name"]) != 1 {
		t.Errorf("Docker name filters = %#v, want current runtime only", gotFilters["name"])
	}
}

func TestCleanupOrphanedContainersRemovesOnlySafeStates(t *testing.T) {
	containers := []map[string]string{
		{"Id": "created-id", "State": "created", "Status": "Up 2 hours"},
		{"Id": "exited-id", "State": "exited", "Status": "Up 2 hours"},
		{"Id": "dead-id", "State": "dead", "Status": "Up 2 hours"},
		{"Id": "running-id", "State": "running", "Status": "Exited (1)"},
		{"Id": "restarting-id", "State": "restarting", "Status": "Exited (1)"},
		{"Id": "paused-id", "State": "paused", "Status": "Exited (1)"},
		{"Id": "removing-id", "State": "removing", "Status": "Exited (1)"},
		{"Id": "empty-id", "State": "", "Status": "Exited (1)"},
		{"Id": "unknown-id", "State": "unknown", "Status": "Exited (1)"},
	}
	var deletedIDs []string
	forceByID := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if err := json.NewEncoder(w).Encode(containers); err != nil {
				t.Errorf("encode Docker containers: %v", err)
			}
		case http.MethodDelete:
			id := path.Base(r.URL.Path)
			deletedIDs = append(deletedIDs, id)
			forceByID[id] = r.URL.Query().Get("force")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
		client.WithVersion("1.44"),
	)
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	defer cli.Close()

	logger := zerolog.Nop()
	e := &DockerExecutor{dockerCLI: cli, logger: &logger}
	e.CleanupOrphanedContainers()

	wantDeleted := []string{"created-id", "exited-id", "dead-id"}
	if !slices.Equal(deletedIDs, wantDeleted) {
		t.Fatalf("deleted container IDs = %#v, want %#v", deletedIDs, wantDeleted)
	}
	for _, id := range wantDeleted {
		if forceByID[id] != "1" {
			t.Errorf("DELETE force for %q = %q, want 1", id, forceByID[id])
		}
	}
}

func TestDockerExecutorPassesContainerAutoMemoryDirectory(t *testing.T) {
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}
	task := model.Task{ID: "task-1", Type: model.PlatformArticle, Prompt: "topic"}
	opts := &ExecutionOptions{
		Task:                &task,
		AutoMemoryDirectory: "/workspace/task-1/.claude/memory",
	}

	cmd, err := e.buildAgentCommand(opts, "sonnet", 100, "/workspace/task-1", "key")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := flagValue(cmd, "--auto-memory-directory"), "/workspace/task-1/.claude/memory"; got != want {
		t.Fatalf("--auto-memory-directory = %q, want %q", got, want)
	}
}

func TestDockerExecutorExposesMontageRuntimePath(t *testing.T) {
	e := &DockerExecutor{
		serverURL: "http://localhost:8080/",
		claudeEnv: map[string]string{
			"HOME":                 "/configured-home",
			"PATH":                 "/configured-bin",
			"ANTHROPIC_AUTH_TOKEN": "runtime-token",
		},
	}

	env := e.buildAgentEnv(&ExecutionOptions{
		Task:    &model.Task{ID: "task-1", Type: model.PlatformMontage},
		Project: &model.Project{ID: "project-1"},
		MontageEnv: map[string]string{
			"NEW_PROVIDER_TOKEN":    "future-secret",
			"HOME":                  "/montage-home",
			"PATH":                  "/montage-bin",
			"ANBAN_API_URL":         "https://montage.invalid",
			"ANBAN_DEFAULT_PROJECT": "montage-project",
			MontageSubmoduleEnvName: "/montage/source",
			"ANTHROPIC_AUTH_TOKEN":  "montage-token",
		},
	}, dockerRuntimeHome("/workspace/task-1"))

	if !slices.Contains(env, "ANBAN_MONTAGE_TEMPLATE_PATH=/opt/montage-template") {
		t.Fatalf("env = %#v, want Montage template path", env)
	}
	if countEnvKey(env, MontageSubmoduleEnvName) != 0 {
		t.Fatalf("env = %#v, Montage workspace path must be assigned by the Agent after materialization", env)
	}
	if !slices.Contains(env, "NEW_PROVIDER_TOKEN=future-secret") {
		t.Fatalf("env = %#v, want unrestricted Montage env", env)
	}
	if countEnvKey(env, "ANTHROPIC_AUTH_TOKEN") != 1 || !slices.Contains(env, "ANTHROPIC_AUTH_TOKEN=runtime-token") {
		t.Fatalf("env = %#v, want managed Claude runtime token to take precedence", env)
	}
	if !slices.Contains(env, "HOME=/workspace/task-1/.anban-runtime-home") || slices.Contains(env, "HOME=/home/node") {
		t.Fatalf("env = %#v, want task-scoped Docker HOME", env)
	}
	if countEnvKey(env, "HOME") != 1 || countEnvKey(env, "PATH") != 1 {
		t.Fatalf("env = %#v, want exactly one managed HOME and PATH", env)
	}
	for key, want := range map[string]string{
		"ANBAN_API_URL":         "http://localhost:8080/",
		"ANBAN_DEFAULT_PROJECT": "project-1",
		MontageTemplateEnvName:  ContainerMontageTemplatePath,
	} {
		if countEnvKey(env, key) != 1 || !slices.Contains(env, key+"="+want) {
			t.Fatalf("env = %#v, want one managed %s=%s", env, key, want)
		}
	}
	if !slices.Contains(env, "PATH="+ContainerMontageRuntimePath) {
		t.Fatalf("env = %#v, want OpenMontage venv on managed PATH", env)
	}

}

func TestDockerExecutorDoesNotExposeMontageEnvToOtherTasks(t *testing.T) {
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}

	env := e.buildAgentEnv(&ExecutionOptions{
		Task:       &model.Task{ID: "task-1", Type: model.PlatformArticle},
		MontageEnv: map[string]string{"NEW_PROVIDER_TOKEN": "future-secret"},
	}, dockerRuntimeHome("/workspace"))

	if slices.Contains(env, "NEW_PROVIDER_TOKEN=future-secret") {
		t.Fatalf("env = %#v, non-Montage task must not receive Montage env", env)
	}
	if countEnvKey(env, MontageTemplateEnvName) != 0 || countEnvKey(env, MontageSubmoduleEnvName) != 0 {
		t.Fatalf("env = %#v, Article task must not receive managed Montage paths", env)
	}
	if !slices.Contains(env, "PATH="+ContainerContentRuntimePath) {
		t.Fatalf("env = %#v, want minimal content PATH", env)
	}

	seednoteEnv := e.buildAgentEnv(&ExecutionOptions{
		Task: &model.Task{ID: "task-2", Type: model.PlatformSeednote},
	}, dockerRuntimeHome("/workspace/task-2"))
	if !slices.Contains(seednoteEnv, "PATH="+ContainerSeednoteRuntimePath) {
		t.Fatalf("env = %#v, want Agent-Reach PATH", seednoteEnv)
	}
	if countEnvKey(seednoteEnv, MontageTemplateEnvName) != 0 || countEnvKey(seednoteEnv, MontageSubmoduleEnvName) != 0 {
		t.Fatalf("env = %#v, Seednote task must not receive managed Montage paths", seednoteEnv)
	}
}

func TestDockerAgentHostConfigUsesSchedulerSettings(t *testing.T) {
	cfg := dockerAgentHostConfig("/host/workspace/task-1", "", config.DockerConfig{
		Network:   "anban-runtime",
		CPUCores:  3,
		MemoryMB:  5120,
		PidsLimit: 256,
	})
	if cfg.NanoCPUs != 3e9 || cfg.Memory != 5120*1024*1024 {
		t.Fatalf("resources = cpu %d memory %d", cfg.NanoCPUs, cfg.Memory)
	}
	if cfg.PidsLimit == nil || *cfg.PidsLimit != 256 {
		t.Fatalf("pids limit = %v, want 256", cfg.PidsLimit)
	}
	if string(cfg.NetworkMode) != "anban-runtime" {
		t.Fatalf("network mode = %q, want anban-runtime", cfg.NetworkMode)
	}
}

func TestStandaloneDockerContainerBindsTaskWorkspace(t *testing.T) {
	cfg := dockerAgentHostConfig("/host/workspace/task-1", "", config.DockerConfig{})
	if len(cfg.VolumesFrom) != 0 || len(cfg.Mounts) != 1 {
		t.Fatalf("host config = %#v", cfg)
	}
	if cfg.Mounts[0].Source != "/host/workspace/task-1" || cfg.Mounts[0].Target != "/workspace" {
		t.Fatalf("workspace mount = %#v", cfg.Mounts[0])
	}
}

func flagValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func countEnvKey(env []string, key string) int {
	prefix := key + "="
	count := 0
	for _, value := range env {
		if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
			count++
		}
	}
	return count
}
