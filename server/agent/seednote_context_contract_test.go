package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeSeednotePhaseSkillsRunInForkedContexts(t *testing.T) {
	root := repoRoot(t)
	for _, skill := range []string{
		"seednote-research",
		"seednote-viral-analysis",
		"seednote-writing",
		"seednote-visual-design",
	} {
		path := filepath.Join(root, "claudecode", "skills", skill, "SKILL.md")
		body := readRepoFile(t, path)
		frontmatter := parseSkillFrontmatter(t, path, body)
		if got := frontmatterStringValue(frontmatter["context"]); got != "fork" {
			t.Fatalf("%s context = %q, want fork", path, got)
		}
		for _, want := range []string{"$ARGUMENTS", "task_id", "project_id", "work_dir", "不超过"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing isolated execution contract %q", path, want)
			}
		}
	}
}

func TestClaudeSeednoteAgentPassesExplicitForkInputs(t *testing.T) {
	path := filepath.Join(repoRoot(t), "claudecode", "agents", "seednote.md")
	body := readRepoFile(t, path)
	for _, want := range []string{
		"context: fork",
		"task_id=$TASK_ID project_id=$PROJECT_ID work_dir=$DIR",
		"action=write_and_humanize",
		"action=compliance_check",
		"content_file=$DIR/content.md",
		"attachments_index=.anban-creator/input-attachments/index.json",
		"outputs=$DIR/image-plan.md,$DIR/image-prompts.md,$DIR/image-review.md",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing explicit fork invocation contract %q", path, want)
		}
	}
	for _, banned := range []string{"anban:humanizer", "using the `humanizer` skill"} {
		if strings.Contains(body, banned) {
			t.Fatalf("%s still loads unused Seednote Skill %q", path, banned)
		}
	}
}
