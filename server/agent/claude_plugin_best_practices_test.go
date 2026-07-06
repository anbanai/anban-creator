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

func TestClaudeCodeSkillsHaveProgressiveExamples(t *testing.T) {
	root := repoRoot(t)
	claudeSkillsRoot := filepath.Join(root, "claudecode", "skills")
	entries, err := os.ReadDir(claudeSkillsRoot)
	if err != nil {
		t.Fatalf("read claudecode skills: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skill := entry.Name()
		skillPath := filepath.Join(root, "claudecode", "skills", skill, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Fatalf("%s missing SKILL.md: %v", skill, err)
		}
		skillBody := readRepoFile(t, skillPath)
		if !strings.Contains(skillBody, "references/examples.md") {
			t.Fatalf("%s must link to references/examples.md for progressive disclosure of cases", skillPath)
		}

		examplesPath := filepath.Join(root, "claudecode", "skills", skill, "references", "examples.md")
		examplesBody := readRepoFile(t, examplesPath)
		if count := strings.Count(examplesBody, "\n### Case "); count < 3 {
			t.Fatalf("%s must include at least 3 concrete cases, got %d", examplesPath, count)
		}
		for _, want := range []string{
			"## Source Patterns",
			"Anthropic official",
			"GitHub high-star",
			"## How To Use These Cases",
		} {
			if !strings.Contains(examplesBody, want) {
				t.Fatalf("%s missing %q", examplesPath, want)
			}
		}

		for _, mirror := range []string{"openclaw", "codex"} {
			mirrorPath := filepath.Join(root, mirror, "skills", skill, "SKILL.md")
			if _, err := os.Stat(mirrorPath); err == nil {
				mirrorSkillBody := readRepoFile(t, mirrorPath)
				if !strings.Contains(mirrorSkillBody, "references/examples.md") {
					t.Fatalf("%s must link to references/examples.md to stay in sync with claudecode", mirrorPath)
				}
				mirrorExamplesPath := filepath.Join(root, mirror, "skills", skill, "references", "examples.md")
				mirrorExamplesBody := readRepoFile(t, mirrorExamplesPath)
				if mirrorExamplesBody != examplesBody {
					t.Fatalf("%s must match %s unless a test documents a distribution-specific difference", mirrorExamplesPath, examplesPath)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat %s: %v", mirrorPath, err)
			}
		}
	}
}
