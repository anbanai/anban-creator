package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeCodePluginHasGitHubHealthFilesAndChangelog(t *testing.T) {
	for _, rel := range []string{
		"claudecode/CHANGELOG.md",
		"claudecode/SECURITY.md",
		"claudecode/CONTRIBUTING.md",
	} {
		path := filepath.Join(repoRoot(t), rel)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			t.Fatalf("%s must exist as a repository health/release-practice file", rel)
		}
	}
}

func TestClaudeCodeHooksUseExecFormForPluginPathCommands(t *testing.T) {
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Async   bool     `json:"async"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	raw := readRepoFile(t, "../../claudecode/hooks/hooks.json")
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("hooks.json must be valid JSON: %v", err)
	}

	for event, groups := range cfg.Hooks {
		for _, group := range groups {
			for _, hook := range group.Hooks {
				if hook.Type != "command" || !strings.Contains(hook.Command, "${CLAUDE_PLUGIN_ROOT}") {
					continue
				}
				if hook.Args == nil {
					t.Fatalf("%s/%s command %q references a plugin path and must set args for Claude Code exec form", event, group.Matcher, hook.Command)
				}
				if strings.ContainsAny(hook.Command, " \t><|&;") {
					t.Fatalf("%s/%s command %q must use exec-form command plus args, not shell-form quoting/redirection", event, group.Matcher, hook.Command)
				}
				if strings.Contains(hook.Command, "bootstrap.sh") && !hook.Async {
					t.Fatalf("%s/%s bootstrap hook must run async so SessionStart is not blocked", event, group.Matcher)
				}
			}
		}
	}
}

func TestClaudeCodeSubagentHooksUsePluginScopedMatchers(t *testing.T) {
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	raw := readRepoFile(t, "../../claudecode/hooks/hooks.json")
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("hooks.json must be valid JSON: %v", err)
	}

	for _, group := range cfg.Hooks["SubagentStop"] {
		if !strings.HasPrefix(group.Matcher, "anban:") {
			t.Fatalf("SubagentStop matcher %q must use the Claude Code plugin-scoped agent name, e.g. anban:seednote", group.Matcher)
		}
	}
}

func TestClaudeCodeDocsDoNotTellAgentsToPrintSecrets(t *testing.T) {
	root := filepath.Join(repoRoot(t), "claudecode")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		text := readRepoFile(t, path)
		if strings.Contains(text, "echo $ANBAN_API_KEY") || strings.Contains(text, "print $ANBAN_API_KEY") {
			t.Fatalf("%s must not instruct agents to print ANBAN_API_KEY; test for presence without logging the value", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk claudecode docs: %v", err)
	}
}

func TestClaudeCodeSkillsStayWithinOfficialSizeGuideline(t *testing.T) {
	root := filepath.Join(repoRoot(t), "claudecode", "skills")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "SKILL.md" {
			return nil
		}
		lineCount := strings.Count(readRepoFile(t, path), "\n") + 1
		if lineCount > 500 {
			t.Fatalf("%s has %d lines; keep SKILL.md under 500 lines and move detail to references/", path, lineCount)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk claudecode skills: %v", err)
	}
}
