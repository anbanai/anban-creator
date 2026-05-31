package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeednoteFinalTitleIsHookOwned(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	for _, path := range []string{
		filepath.Join(root, "claudecode", "agents", "seednote.md"),
		filepath.Join(root, "claudecode", "skills", "seednote", "SKILL.md"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(raw)
		if strings.Contains(body, "finalize_task_title") {
			t.Fatalf("%s should not require the seednote agent to call finalize_task_title", path)
		}
	}

	hooksPath := filepath.Join(root, "claudecode", "hooks", "hooks.json")
	hooksRaw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	hooks := string(hooksRaw)
	for _, want := range []string{
		`"matcher": "seednote"`,
		"hook 负责",
		"finalize_task_title",
		"content.md",
		"重复标题错误",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("hooks missing %q", want)
		}
	}
}
