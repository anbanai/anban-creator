package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSeednoteFinalizationOwnership(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	t.Run("agent owns finalization calls", func(t *testing.T) {
		agentPath := filepath.Join(root, "claudecode", "agents", "seednote.md")
		raw, err := os.ReadFile(agentPath)
		if err != nil {
			t.Fatalf("read %s: %v", agentPath, err)
		}
		agentBody := string(raw)
		saveTemplatePattern := regexp.MustCompile(`save_eligible\s*=\s*true[^\n]*条件满足时调用[^\n]*save_template`)
		if matches := saveTemplatePattern.FindAllStringIndex(agentBody, -1); len(matches) != 1 {
			t.Errorf("seednote agent has %d conditional save_template invocation contracts, want exactly one", len(matches))
		}
		for _, tool := range []string{"finalize_task_title", "submit_agent_feedback"} {
			callPattern := regexp.MustCompile(regexp.QuoteMeta(tool) + `\s*\(`)
			if calls := callPattern.FindAllStringIndex(agentBody, -1); len(calls) != 1 {
				t.Errorf("seednote agent has %d %s call expressions, want exactly one", len(calls), tool)
			}
		}
	})

	t.Run("hooks contain only mechanical gates", func(t *testing.T) {
		hooksPath := filepath.Join(root, "claudecode", "hooks", "hooks.json")
		hooksRaw, err := os.ReadFile(hooksPath)
		if err != nil {
			t.Fatalf("read %s: %v", hooksPath, err)
		}
		hooksText := string(hooksRaw)
		forbiddenTools := []string{"finalize_task_title", "submit_agent_feedback"}
		for _, forbidden := range forbiddenTools {
			if strings.Contains(hooksText, forbidden) {
				t.Errorf("Claude hooks JSON must not contain %q in any field", forbidden)
			}
		}
		var cfg struct {
			Hooks map[string][]struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Prompt  string `json:"prompt"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(hooksRaw, &cfg); err != nil {
			t.Fatalf("decode %s: %v", hooksPath, err)
		}
		for event, groups := range cfg.Hooks {
			for _, group := range groups {
				for _, hook := range group.Hooks {
					if hook.Type == "prompt" {
						t.Errorf("Claude hook %s/%s must not use prompt hooks", event, group.Matcher)
					}
					for _, forbidden := range forbiddenTools {
						if strings.Contains(hook.Prompt, forbidden) || strings.Contains(hook.Command, forbidden) {
							t.Errorf("Claude hook %s/%s must not contain %q in prompt or command text", event, group.Matcher, forbidden)
						}
					}
				}
			}
		}
	})

	t.Run("skill assigns finalization to the agent", func(t *testing.T) {
		skillPath := filepath.Join(root, "claudecode", "skills", "seednote", "SKILL.md")
		skillRaw, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("read %s: %v", skillPath, err)
		}
		skillBody := string(skillRaw)
		if strings.Contains(skillBody, "hook 统一负责") {
			t.Error("seednote skill must not assign finalization ownership to hooks")
		}
		const ownershipStatement = "最终标题排重与入库由 seednote Agent 的归档阶段负责；本专业流程不另建 Hook 副本。"
		if !strings.Contains(skillBody, ownershipStatement) {
			t.Errorf("seednote skill missing canonical Agent ownership statement %q", ownershipStatement)
		}
	})
}
