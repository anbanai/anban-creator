package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeednoteFinalTitleOwnership(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	// The agent should own finalize_task_title (D2: moved from hook to agent).
	agentPath := filepath.Join(root, "claudecode", "agents", "seednote.md")
	raw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentPath, err)
	}
	agentBody := string(raw)
	for _, want := range []string{
		"finalize_task_title",
		"重复标题错误",
		"save_template",
		"save_eligible",
	} {
		if !strings.Contains(agentBody, want) {
			t.Fatalf("seednote agent missing %q", want)
		}
	}

	// The hook should NOT contain business logic for title finalization or template saving.
	hooksPath := filepath.Join(root, "claudecode", "hooks", "hooks.json")
	hooksRaw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	hooks := string(hooksRaw)
	for _, want := range []string{
		`"matcher": "seednote"`,
		"submit_agent_feedback",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("hooks missing %q", want)
		}
	}
}
