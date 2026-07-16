package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"github.com/docker/docker/client"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
)

func TestDockerExecutorUsesCreatorAgentRuntimeNames(t *testing.T) {
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}
	task := model.Task{ID: "task-1", Type: model.PlatformVideoEditor, Prompt: "topic"}

	cmd := e.buildAgentCommand(&ExecutionOptions{Task: &task}, "sonnet", 100, "/workspace", "key")
	if got, want := cmd[0], "anban"; got != want {
		t.Fatalf("agent binary = %q, want %q", got, want)
	}
	if got, want := cmd[1], "run"; got != want {
		t.Fatalf("agent subcommand = %q, want %q", got, want)
	}
	if got, want := flagValue(cmd, "--agent-flag"), "anban:videoeditor"; got != want {
		t.Fatalf("--agent-flag = %q, want %q", got, want)
	}

	if got, want := filepath.Base(filepath.Dir(DefaultWorkspaceDir(task.ID))), "anban-creator"; got != want {
		t.Fatalf("default workspace base = %q, want %q", got, want)
	}
	if got, want := EphemeralContainerName(task.ID), "creator-agent-task-task-1"; got != want {
		t.Fatalf("container name = %q, want %q", got, want)
	}
	if got, want := DockerAgentImageDefault, "creator-agent:latest"; got != want {
		t.Fatalf("Docker Agent image default = %q, want %q", got, want)
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

	cmd := e.buildAgentCommand(opts, "sonnet", 100, "/workspace/task-1", "key")
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

	if !slices.Contains(env, "ANBAN_MONTAGE_SUBMODULE_PATH=/app/third_party/OpenMontage") {
		t.Fatalf("env = %#v, want Montage runtime path", env)
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
		MontageSubmoduleEnvName: ContainerMontageSubmodulePath,
	} {
		if countEnvKey(env, key) != 1 || !slices.Contains(env, key+"="+want) {
			t.Fatalf("env = %#v, want one managed %s=%s", env, key, want)
		}
	}
	if !slices.Contains(env, "PATH="+ContainerRuntimePath) {
		t.Fatalf("env = %#v, want Agent-Reach venv on managed PATH", env)
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
