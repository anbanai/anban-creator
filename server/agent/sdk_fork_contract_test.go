package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeAgentSDKIsRemoved(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "third_party", "claude-agent-sdk-go")); !os.IsNotExist(err) {
		t.Fatalf("removed Claude Agent SDK directory still exists: %v", err)
	}

	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if strings.Contains(string(goMod), "github.com/severity1/claude-agent-sdk-go") {
		t.Fatal("go.mod still declares the removed Claude Agent SDK")
	}

	dockerfile, err := os.ReadFile(filepath.Join(root, "deploy", "docker", "Dockerfile.server"))
	if err != nil {
		t.Fatalf("read server Dockerfile: %v", err)
	}
	if strings.Contains(string(dockerfile), "third_party/claude-agent-sdk-go") {
		t.Fatal("server Dockerfile still references the removed Claude Agent SDK")
	}
}
