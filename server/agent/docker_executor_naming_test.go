package agent

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestDockerExecutorUsesAnbanRuntimeNames(t *testing.T) {
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
	if got, want := EphemeralContainerName(task.ID), "anban-creator-task-task-1"; got != want {
		t.Fatalf("container name = %q, want %q", got, want)
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
			"HOME": "/configured-home",
			"PATH": "/configured-bin",
		},
	}

	env := e.buildAgentEnv(&ExecutionOptions{
		Task:               &model.Task{ID: "task-1", Type: model.PlatformMontage},
		Project:            &model.Project{ID: "project-1"},
		MontageProviderEnv: map[string]string{"FAL_KEY": "fal-secret"},
	}, dockerRuntimeHome("/workspace/task-1"))

	if !slices.Contains(env, "ANBAN_MONTAGE_SUBMODULE_PATH=/app/third_party/OpenMontage") {
		t.Fatalf("env = %#v, want Montage runtime path", env)
	}
	if !slices.Contains(env, "FAL_KEY=fal-secret") {
		t.Fatalf("env = %#v, want Montage provider env", env)
	}
	if !slices.Contains(env, "HOME=/workspace/task-1/.anban-runtime-home") || slices.Contains(env, "HOME=/home/node") {
		t.Fatalf("env = %#v, want task-scoped Docker HOME", env)
	}
	if countEnvKey(env, "HOME") != 1 || countEnvKey(env, "PATH") != 1 {
		t.Fatalf("env = %#v, want exactly one managed HOME and PATH", env)
	}

}

func TestDockerExecutorDoesNotExposeMontageProviderEnvToOtherTasks(t *testing.T) {
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}

	env := e.buildAgentEnv(&ExecutionOptions{
		Task:               &model.Task{ID: "task-1", Type: model.PlatformArticle},
		MontageProviderEnv: map[string]string{"FAL_KEY": "fal-secret"},
	}, dockerRuntimeHome("/workspace"))

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
