package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeSeednotePhaseSkillsUseFileBackedContracts(t *testing.T) {
	root := repoRoot(t)
	for _, skill := range []string{
		"seednote-research",
		"seednote-viral-analysis",
		"seednote-writing",
		"seednote-visual-design",
	} {
		path := filepath.Join(root, "harness", "skills", skill, "SKILL.md")
		body := readRepoFile(t, path)
		frontmatter := parseSkillFrontmatter(t, path, body)
		if got := frontmatterStringValue(frontmatter["context"]); got != "" {
			t.Fatalf("%s context = %q, want no forked context", path, got)
		}
		for _, forbidden := range []string{"$ARGUMENTS", "context: fork", "隔离执行契约", "只接收短回执"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains obsolete invocation boilerplate %q", path, forbidden)
			}
		}
	}
}

func TestClaudeSeednoteAgentDeclaresPhaseSkillsWithoutInvocationBoilerplate(t *testing.T) {
	path := filepath.Join(repoRoot(t), "harness", "agents", "seednote.md")
	body := readRepoFile(t, path)
	frontmatter := frontmatterBlock(t, body)
	for _, want := range []string{
		"  - humanizer",
		"  - seednote-research",
		"  - seednote-viral-analysis",
		"  - seednote-writing",
		"  - seednote-visual-design",
	} {
		if !strings.Contains(frontmatter, want) {
			t.Fatalf("%s missing declared phase Skill %q", path, want)
		}
	}
	if strings.Contains(frontmatter, "agent-reach") {
		t.Fatalf("%s frontmatter still declares removed agent-reach Skill", path)
	}
	for _, want := range []string{
		"output/topic-analysis.md",
		"output/source-analysis.md",
		"output/viral-template.json",
		"output/content.md",
		"output/image-plan.md",
		"output/image-review.md",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing file-backed phase contract %q", path, want)
		}
	}
	for _, banned := range []string{"context: fork", "$ARGUMENTS", "只接收短回执", "内置去 AI", "不要再调用 `humanizer` Skill"} {
		if strings.Contains(body, banned) {
			t.Fatalf("%s contains obsolete Seednote invocation contract %q", path, banned)
		}
	}
}
