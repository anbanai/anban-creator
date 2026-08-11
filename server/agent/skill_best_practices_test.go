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
	for _, distro := range []string{"plugins"} {
		skillsRoot := filepath.Join(root, distro, "skills")
		err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "SKILL.md" {
				return nil
			}
			if isUpstreamHumanizerSkillPath(path) {
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

func isUpstreamHumanizerSkillPath(path string) bool {
	return filepath.Base(path) == "SKILL.md" && isUpstreamHumanizerPath(path)
}

func isUpstreamHumanizerPath(path string) bool {
	return strings.Contains(filepath.ToSlash(path), "/plugins/skills/humanizer/")
}

func TestDistributedSkillsDoNotShipAuxiliaryReadmes(t *testing.T) {
	root := repoRoot(t)
	auxiliaryNames := map[string]bool{
		"README.md":             true,
		"CHANGELOG.md":          true,
		"INSTALLATION_GUIDE.md": true,
		"QUICK_REFERENCE.md":    true,
	}
	for _, distro := range []string{"plugins"} {
		skillsRoot := filepath.Join(root, distro, "skills")
		err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if isUpstreamHumanizerPath(path) {
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
	canonical := readRepoFile(t, filepath.Join(root, "plugins", "skills", "writers", "references", "writer-style-schema.md"))
	for _, distro := range []string{"plugins"} {
		path := filepath.Join(root, distro, "skills", "writers", "references", "writer-style-schema.md")
		if got := readRepoFile(t, path); got != canonical {
			t.Fatalf("%s must match claudecode writer-style-schema.md", path)
		}
	}
}

func TestSkillUpstreamIndexDocumentsMirroredSourceBoundaries(t *testing.T) {
	root := repoRoot(t)
	claudeReadme := readRepoFile(t, filepath.Join(root, "plugins", "README.md"))
	for _, want := range []string{
		"docs/claude/",
		"moments",
		"Caihui0127/caihui-moments-skill",
		"不默认使用“彩卉”人设",
	} {
		if !strings.Contains(claudeReadme, want) {
			t.Fatalf("claudecode README source index missing %q", want)
		}
	}

	for _, want := range []string{"skills/", "docs/claude/", "宿主差异"} {
		if !strings.Contains(claudeReadme, want) {
			t.Fatalf("unified plugin README source-index contract missing %q", want)
		}
	}
}

func TestClaudeCodePluginAgentsFollowOfficialBestPractices(t *testing.T) {
	root := repoRoot(t)
	agentsRoot := filepath.Join(root, "plugins", "agents")
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
				t.Fatalf("%s declares %q; Claude Code ignores this field for plugin agents", path, field)
			}
		}
		for _, skill := range frontmatterStringValues(fm["skills"]) {
			skillPath := filepath.Join(root, "plugins", "skills", skill, "SKILL.md")
			if _, err := os.Stat(skillPath); err != nil {
				t.Fatalf("%s preloads missing Skill %q: %v", path, skill, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk claudecode agents: %v", err)
	}
}

func TestClaudeCodePluginAgentsDeclareOwnedSkills(t *testing.T) {
	root := repoRoot(t)
	expected := map[string][]string{
		"designer":    {"line-art-coloring"},
		"ecommerce":   {"ecommerce-product-analysis", "ecommerce-copywriting", "humanizer", "ecommerce-visual-design", "ecommerce-platform-specs"},
		"live-slicer": {"live-slice", "capcut-draft"},
		"moments":     {"moments", "humanizer"},
		"montage":     {"montage"},
		"seednote":    {"seednote-research", "seednote-viral-analysis", "seednote-writing", "seednote-visual-design"},
		"article":     {"content-writing", "humanizer", "article-visual-design", "article-cover-design", "topic-research", "seo-optimization", "article-publishing", "article-viral-strategy"},
	}

	for agentName, want := range expected {
		path := filepath.Join(root, "plugins", "agents", agentName+".md")
		body := readRepoFile(t, path)
		fm := parseSkillFrontmatter(t, path, body)
		got := frontmatterStringValues(fm["skills"])
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("%s skills = %q, want %q", path, got, want)
		}
	}
}

func TestClaudeCodeSkillsHaveRuntimeOwner(t *testing.T) {
	root := repoRoot(t)
	agentsRoot := filepath.Join(root, "plugins", "agents")
	skillsRoot := filepath.Join(root, "plugins", "skills")
	referenced := map[string]bool{}
	agents, err := os.ReadDir(agentsRoot)
	if err != nil {
		t.Fatalf("read Claude agents: %v", err)
	}
	for _, entry := range agents {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(agentsRoot, entry.Name())
		body := readRepoFile(t, path)
		fm := parseSkillFrontmatter(t, path, body)
		for _, skill := range frontmatterStringValues(fm["skills"]) {
			referenced[skill] = true
		}
	}

	userEntrypoints := map[string]bool{
		"anban-setup":            true,
		"article":                true,
		"ecommerce":              true,
		"portrait-pose-variants": true,
		"short-video-cover":      true,
		"writers":                true,
	}
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		t.Fatalf("read Claude skills: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, err := os.Stat(filepath.Join(skillsRoot, name, "SKILL.md")); os.IsNotExist(err) {
			continue
		}
		if !referenced[name] && !userEntrypoints[name] {
			t.Fatalf("Claude Skill %q has no Agent reference or declared user entrypoint; remove it or assign a runtime owner", name)
		}
	}
}

func TestCodexAgentSkillConfigsPointToBundledSkills(t *testing.T) {
	root := repoRoot(t)
	agentsRoot := filepath.Join(root, "plugins", "agents")
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
			skillPath := filepath.Join(root, "plugins", "skills", skillName, "SKILL.md")
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

func TestCodexAgentsDoNotPreloadDuplicateUmbrellaSkills(t *testing.T) {
	root := repoRoot(t)
	for agentName, umbrellaSkill := range map[string]string{
		"ecommerce": "ecommerce",
		"seednote":  "seednote",
		"article":   "article",
	} {
		path := filepath.Join(root, "plugins", "agents", agentName+".toml")
		body := readRepoFile(t, path)
		config := `path = "__PLUGIN_ROOT__/skills/` + umbrellaSkill + `/SKILL.md"`
		if strings.Contains(body, config) {
			t.Fatalf("%s preloads duplicate umbrella Skill %q", path, umbrellaSkill)
		}
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
	descriptionLower := strings.ToLower(description)
	if !strings.HasPrefix(descriptionLower, "use when ") && !strings.HasPrefix(descriptionLower, "this skill should be used when ") {
		t.Fatalf("%s description must start with activation wording (%q or %q), got %q", path, "Use when", "This skill should be used when", description)
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

func TestLongSkillReferencesUseDirectProgressiveDisclosure(t *testing.T) {
	root := repoRoot(t)
	for _, distro := range []string{"plugins"} {
		skillsRoot := filepath.Join(root, distro, "skills")
		err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "SKILL.md" {
				return nil
			}
			skillBody := readRepoFile(t, path)
			skillDir := filepath.Dir(path)
			refsDir := filepath.Join(skillDir, "references")
			if _, err := os.Stat(refsDir); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}

			return filepath.WalkDir(refsDir, func(refPath string, refEntry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if refEntry.IsDir() || filepath.Ext(refPath) != ".md" {
					return nil
				}
				refBody := readRepoFile(t, refPath)
				if strings.Count(refBody, "\n")+1 <= 100 {
					return nil
				}
				rel, err := filepath.Rel(skillDir, refPath)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)
				if filepath.Dir(rel) == "references" && !strings.Contains(skillBody, rel) {
					t.Fatalf("%s is a long reference and must be linked directly from %s", refPath, path)
				}
				if !hasReferenceContents(refBody) {
					t.Fatalf("%s has more than 100 lines and must start with a Contents/Table of contents/目录 section", refPath)
				}
				return nil
			})
		})
		if err != nil {
			t.Fatalf("walk %s skills: %v", distro, err)
		}
	}
}

func hasReferenceContents(body string) bool {
	head := body
	if len(head) > 2500 {
		head = head[:2500]
	}
	for _, line := range strings.Split(head, "\n") {
		normalized := strings.ToLower(strings.TrimSpace(line))
		normalized = strings.TrimLeft(normalized, "# ")
		switch normalized {
		case "contents", "table of contents", "目录":
			return true
		}
	}
	return false
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
