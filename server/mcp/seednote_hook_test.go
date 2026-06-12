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

	// The agent should own save_template (conditional business logic).
	// The agent should NOT own finalize_task_title (that belongs to the hook).
	agentPath := filepath.Join(root, "claudecode", "agents", "seednote.md")
	raw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentPath, err)
	}
	agentBody := string(raw)
	for _, want := range []string{
		"save_template",
		"save_eligible",
	} {
		if !strings.Contains(agentBody, want) {
			t.Fatalf("seednote agent missing %q", want)
		}
	}
	// finalize_task_title should NOT be in the agent
	if strings.Contains(agentBody, "finalize_task_title") {
		t.Fatalf("seednote agent should NOT contain finalize_task_title (belongs to hook)")
	}

	// The hook should own finalize_task_title (post-condition guarantee).
	hooksPath := filepath.Join(root, "claudecode", "hooks", "hooks.json")
	hooksRaw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	hooks := string(hooksRaw)
	for _, want := range []string{
		`"matcher": "seednote"`,
		"finalize_task_title",
		"submit_agent_feedback",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("hooks missing %q", want)
		}
	}
}
