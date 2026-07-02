package agent

import (
	"path/filepath"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestDockerExecutorUsesAnbanRuntimeNames(t *testing.T) {
	e := &DockerExecutor{serverURL: "http://localhost:8080/"}
	task := model.Task{ID: "task-1", Type: model.PlatformArticle, Prompt: "topic"}

	cmd := e.buildAgentCommand(&ExecutionOptions{Task: &task}, "sonnet", 100, "/workspace", "key")
	if got, want := cmd[0], "anban"; got != want {
		t.Fatalf("agent binary = %q, want %q", got, want)
	}
	if got, want := cmd[1], "run"; got != want {
		t.Fatalf("agent subcommand = %q, want %q", got, want)
	}
	if got, want := flagValue(cmd, "--agent-flag"), "anban:wechatarticle"; got != want {
		t.Fatalf("--agent-flag = %q, want %q", got, want)
	}

	if got, want := filepath.Base(filepath.Dir(DefaultWorkspaceDir(task.ID))), "anban-creator"; got != want {
		t.Fatalf("default workspace base = %q, want %q", got, want)
	}
	if got, want := EphemeralContainerName(task.ID), "anban-creator-task-task-1"; got != want {
		t.Fatalf("container name = %q, want %q", got, want)
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
