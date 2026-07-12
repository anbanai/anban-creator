package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/docker/docker/client"

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

func TestCleanupOrphanedContainersIncludesCurrentAndLegacyNameFilters(t *testing.T) {
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

	for _, want := range []string{"^/creator-agent-task-", "^/anban-creator-task-"} {
		if !gotFilters["name"][want] {
			t.Errorf("Docker name filters = %#v, want %q", gotFilters["name"], want)
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
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}

	env := e.buildAgentEnv(&ExecutionOptions{
		Task:               &model.Task{ID: "task-1", Type: model.PlatformMontage},
		Project:            &model.Project{ID: "project-1"},
		MontageProviderEnv: map[string]string{"FAL_KEY": "fal-secret"},
	})

	if !slices.Contains(env, "ANBAN_MONTAGE_SUBMODULE_PATH=/app/third_party/OpenMontage") {
		t.Fatalf("env = %#v, want Montage runtime path", env)
	}
	if !slices.Contains(env, "FAL_KEY=fal-secret") {
		t.Fatalf("env = %#v, want Montage provider env", env)
	}
}

func TestDockerExecutorDoesNotExposeMontageProviderEnvToOtherTasks(t *testing.T) {
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}

	env := e.buildAgentEnv(&ExecutionOptions{
		Task:               &model.Task{ID: "task-1", Type: model.PlatformArticle},
		MontageProviderEnv: map[string]string{"FAL_KEY": "fal-secret"},
	})

	if slices.Contains(env, "FAL_KEY=fal-secret") {
		t.Fatalf("env = %#v, non-Montage task must not receive Montage provider env", env)
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
