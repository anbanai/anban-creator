package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeAgentSDKForkContract(t *testing.T) {
	root := repositoryRoot(t)
	forkRoot := filepath.Join(root, "third_party", "claude-agent-sdk-go")

	if _, err := os.Stat(filepath.Join(forkRoot, ".claude")); err == nil {
		t.Fatal("vendored Claude Agent SDK must not contain .claude workspace config")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat vendored SDK .claude directory: %v", err)
	}

	requiredFiles := []string{
		"LICENSE",
		"go.mod",
		filepath.Join("internal", "shared", "message.go"),
		filepath.Join("internal", "parser", "json.go"),
	}
	for _, name := range requiredFiles {
		info, err := os.Stat(filepath.Join(forkRoot, name))
		if err != nil {
			t.Fatalf("vendored SDK missing %s: %v", name, err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("vendored SDK path %s must be a regular file", name)
		}
	}

	mainGoMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read main go.mod: %v", err)
	}
	if !strings.Contains(string(mainGoMod), "replace github.com/severity1/claude-agent-sdk-go => ./third_party/claude-agent-sdk-go") {
		t.Fatal("main go.mod must replace Claude Agent SDK with the repository-local fork")
	}

	messageSource, err := os.ReadFile(filepath.Join(forkRoot, "internal", "shared", "message.go"))
	if err != nil {
		t.Fatalf("read vendored SDK message types: %v", err)
	}
	if !strings.Contains(string(messageSource), "type ModelUsage struct") ||
		!strings.Contains(string(messageSource), `json:"modelUsage,omitempty"`) {
		t.Fatal("vendored SDK must expose typed terminal ModelUsage")
	}

	parserSource, err := os.ReadFile(filepath.Join(forkRoot, "internal", "parser", "json.go"))
	if err != nil {
		t.Fatalf("read vendored SDK result parser: %v", err)
	}
	if !strings.Contains(string(parserSource), `data["modelUsage"]`) ||
		!strings.Contains(string(parserSource), "map[string]shared.ModelUsage") {
		t.Fatal("vendored SDK result parser must decode typed terminal ModelUsage")
	}
}
