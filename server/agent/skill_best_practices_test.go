package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var agentSkillNameRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

func TestDistributedSkillsFollowAgentSkillsBestPractices(t *testing.T) {
	root := repoRoot(t)
	for _, distro := range []string{"claudecode", "codex", "openclaw"} {
		skillsRoot := filepath.Join(root, distro, "skills")
		err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "SKILL.md" {
				return nil
			}
			assertAgentSkillBestPractice(t, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s skills: %v", distro, err)
		}
	}
}

func TestDistributedSkillsDoNotShipAuxiliaryReadmes(t *testing.T) {
	root := repoRoot(t)
	auxiliaryNames := map[string]bool{
		"README.md":             true,
		"CHANGELOG.md":          true,
		"INSTALLATION_GUIDE.md": true,
		"QUICK_REFERENCE.md":    true,
	}
	for _, distro := range []string{"claudecode", "codex", "openclaw"} {
		skillsRoot := filepath.Join(root, distro, "skills")
		err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if auxiliaryNames[filepath.Base(path)] {
				t.Fatalf("%s must not ship auxiliary docs inside a skill; move guidance into SKILL.md or references/*.md", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s skills: %v", distro, err)
		}
	}
}

func TestDistributedWriterSkillReferencesStayMirrored(t *testing.T) {
	root := repoRoot(t)
	canonical := readRepoFile(t, filepath.Join(root, "claudecode", "skills", "writers", "references", "writer-style-schema.md"))
	for _, distro := range []string{"codex", "openclaw"} {
		path := filepath.Join(root, distro, "skills", "writers", "references", "writer-style-schema.md")
		if got := readRepoFile(t, path); got != canonical {
			t.Fatalf("%s must match claudecode writer-style-schema.md", path)
		}
	}
}

func TestSkillUpstreamIndexDocumentsMirroredSourceBoundaries(t *testing.T) {
	root := repoRoot(t)
	claudeReadme := readRepoFile(t, filepath.Join(root, "claudecode", "README.md"))
	for _, want := range []string{
		"docs/claude/",
		"agent-reach",
		"Panniantong/agent-reach",
		"guizang-social-card",
		"op7418/guizang-social-card-skill",
		"AGPL-3.0",
		"moments",
		"Caihui0127/caihui-moments-skill",
		"不默认使用“彩卉”人设",
	} {
		if !strings.Contains(claudeReadme, want) {
			t.Fatalf("claudecode README source index missing %q", want)
		}
	}

	for _, distro := range []string{"codex", "openclaw"} {
		readme := readRepoFile(t, filepath.Join(root, distro, "README.md"))
		for _, want := range []string{
			"../claudecode/README.md",
			"../docs/claude/",
			"claudecode/skills",
			"codex/skills",
			"openclaw/skills",
		} {
			if !strings.Contains(readme, want) {
				t.Fatalf("%s README source-index pointer missing %q", distro, want)
			}
		}
	}
}

func TestClaudeCodePluginAgentsFollowOfficialBestPractices(t *testing.T) {
	root := repoRoot(t)
	agentsRoot := filepath.Join(root, "claudecode", "agents")
	skillsRoot := filepath.Join(root, "claudecode", "skills")
	forbiddenPluginAgentFields := []string{"hooks", "mcpServers", "permissionMode"}

	err := filepath.WalkDir(agentsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		body := readRepoFile(t, path)
		fm := parseSkillFrontmatter(t, path, body)

		name := frontmatterStringValue(fm["name"])
		if name == "" {
			t.Fatalf("%s missing required name frontmatter", path)
		}
		if !agentSkillNameRE.MatchString(name) || strings.Contains(name, "--") {
			t.Fatalf("%s name = %q; Claude Code agent names should be lowercase letters/digits/hyphens", path, name)
		}
		if want := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)); name != want {
			t.Fatalf("%s name = %q, want filename stem %q", path, name, want)
		}

		description := frontmatterStringValue(fm["description"])
		if description == "" {
			t.Fatalf("%s missing required description frontmatter", path)
		}
		if n := len([]rune(description)); n > 1024 {
			t.Fatalf("%s description is %d chars; keep agent trigger descriptions concise", path, n)
		}
		if strings.ContainsAny(description, "<>") {
			t.Fatalf("%s description must not contain XML angle brackets", path)
		}

		for _, field := range forbiddenPluginAgentFields {
			if _, ok := fm[field]; ok {
				t.Fatalf("%s declares %q, but Claude Code plugin agents ignore unsupported security-sensitive fields", path, field)
			}
		}
		for _, skillName := range frontmatterStringValues(fm["skills"]) {
			skillPath := filepath.Join(skillsRoot, skillName, "SKILL.md")
			if _, err := os.Stat(skillPath); err != nil {
				t.Fatalf("%s references missing plugin skill %q at %s", path, skillName, skillPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk claudecode agents: %v", err)
	}
}

func TestCodexAgentSkillConfigsPointToBundledSkills(t *testing.T) {
	root := repoRoot(t)
	agentsRoot := filepath.Join(root, "codex", "agents")
	skillPathRE := regexp.MustCompile(`path\s*=\s*"__PLUGIN_ROOT__/skills/([^/]+)/SKILL\.md"`)

	err := filepath.WalkDir(agentsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".toml" {
			return nil
		}
		body := readRepoFile(t, path)
		for _, match := range skillPathRE.FindAllStringSubmatch(body, -1) {
			skillName := match[1]
			skillPath := filepath.Join(root, "codex", "skills", skillName, "SKILL.md")
			if _, err := os.Stat(skillPath); err != nil {
				t.Fatalf("%s references missing bundled skill %q at %s", path, skillName, skillPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk codex agents: %v", err)
	}
}

func assertAgentSkillBestPractice(t *testing.T, path string) {
	t.Helper()
	body := readRepoFile(t, path)
	if lineCount := strings.Count(body, "\n") + 1; lineCount > 500 {
		t.Fatalf("%s has %d lines; docs/claude and Agent Skills recommend keeping SKILL.md under 500 lines", path, lineCount)
	}

	fm := parseSkillFrontmatter(t, path, body)
	name := frontmatterStringValue(fm["name"])
	if name == "" {
		t.Fatalf("%s missing required name frontmatter", path)
	}
	if !agentSkillNameRE.MatchString(name) || strings.Contains(name, "--") {
		t.Fatalf("%s name = %q; Agent Skills names must be 1-64 chars, lowercase letters/digits/hyphens, no edge or repeated hyphens", path, name)
	}
	if want := filepath.Base(filepath.Dir(path)); name != want {
		t.Fatalf("%s name = %q, want parent directory name %q", path, name, want)
	}

	description := frontmatterStringValue(fm["description"])
	if description == "" {
		t.Fatalf("%s missing required description frontmatter", path)
	}
	if n := len([]rune(description)); n > 1024 {
		t.Fatalf("%s description is %d chars; Agent Skills limit is 1024", path, n)
	}
	if strings.ContainsAny(description, "<>") {
		t.Fatalf("%s description must not contain XML angle brackets", path)
	}

	if compatibility := frontmatterStringValue(fm["compatibility"]); len([]rune(compatibility)) > 500 {
		t.Fatalf("%s compatibility is %d chars; Agent Skills limit is 500", path, len([]rune(compatibility)))
	}

	if tools, ok := fm["allowed-tools"]; ok {
		for _, tool := range frontmatterStringValues(tools) {
			if strings.TrimSpace(tool) == "" {
				t.Fatalf("%s allowed-tools contains an empty tool entry", path)
			}
		}
	}
}

func parseSkillFrontmatter(t *testing.T, path, body string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("%s missing YAML frontmatter", path)
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatalf("%s frontmatter is not closed", path)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		t.Fatalf("%s has invalid YAML frontmatter: %v", path, err)
	}
	return fm
}

func frontmatterStringValue(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func frontmatterStringValues(raw any) []string {
	switch v := raw.(type) {
	case string:
		return strings.Fields(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := frontmatterStringValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
