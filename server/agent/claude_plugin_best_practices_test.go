package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	claudeCodePluginNameRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	claudeCodeUserConfigKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	claudeCodeSemverRE        = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

func TestClaudeCodePluginManifestMatchesOfficialBestPracticeFields(t *testing.T) {
	var manifest struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Version     string `json:"version"`
		UserConfig  map[string]struct {
			Type        string `json:"type"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Required    bool   `json:"required"`
			Sensitive   bool   `json:"sensitive"`
			Default     string `json:"default"`
		} `json:"userConfig"`
	}
	raw := readRepoFile(t, "../../claudecode/.claude-plugin/plugin.json")
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("plugin.json must be valid JSON: %v", err)
	}
	if !claudeCodePluginNameRE.MatchString(manifest.Name) {
		t.Fatalf("plugin name %q must be kebab-case for Claude Code namespacing", manifest.Name)
	}
	if manifest.DisplayName == "" {
		t.Fatal("plugin.json must set displayName for Claude Code plugin UI surfaces")
	}
	if manifest.Description == "" {
		t.Fatal("plugin.json must set description")
	}
	if !claudeCodeSemverRE.MatchString(manifest.Version) {
		t.Fatalf("plugin version %q must be semantic version x.y.z", manifest.Version)
	}

	for key, opt := range manifest.UserConfig {
		if !claudeCodeUserConfigKeyRE.MatchString(key) {
			t.Fatalf("userConfig key %q must be a valid identifier for ${user_config.%s} substitution", key, key)
		}
		if opt.Type == "" || opt.Title == "" || opt.Description == "" {
			t.Fatalf("userConfig.%s must set type, title, and description", key)
		}
	}
	apiKey := manifest.UserConfig["api_key"]
	if apiKey.Type != "string" || !apiKey.Required || !apiKey.Sensitive {
		t.Fatalf("api_key userConfig must be a required sensitive string, got %+v", apiKey)
	}
	apiURL := manifest.UserConfig["api_url"]
	if apiURL.Type != "string" || apiURL.Default != "https://api.creator.anbanai.com" {
		t.Fatalf("api_url userConfig must be a string with official hosted default, got %+v", apiURL)
	}

	var marketplace struct {
		Plugins []struct {
			Name        string `json:"name"`
			Source      string `json:"source"`
			DisplayName string `json:"displayName"`
			Version     string `json:"version"`
		} `json:"plugins"`
	}
	raw = readRepoFile(t, "../../claudecode/.claude-plugin/marketplace.json")
	if err := json.Unmarshal([]byte(raw), &marketplace); err != nil {
		t.Fatalf("marketplace.json must be valid JSON: %v", err)
	}
	for _, plugin := range marketplace.Plugins {
		if plugin.Name != manifest.Name {
			continue
		}
		if plugin.Source != "./" {
			t.Fatalf("marketplace entry source = %q, want ./ for repo-root plugin", plugin.Source)
		}
		if plugin.DisplayName != manifest.DisplayName {
			t.Fatalf("marketplace displayName = %q, want %q", plugin.DisplayName, manifest.DisplayName)
		}
		if plugin.Version != manifest.Version {
			t.Fatalf("marketplace version = %q, want %q", plugin.Version, manifest.Version)
		}
		return
	}
	t.Fatalf("marketplace.json missing plugin entry %q", manifest.Name)
}

func TestClaudeCodePluginComponentsUseRootDefaultLocations(t *testing.T) {
	root := filepath.Join(repoRoot(t), "claudecode")
	for _, rel := range []string{
		"skills",
		"agents",
		"hooks/hooks.json",
		".mcp.json",
		".claude-plugin/plugin.json",
		".claude-plugin/marketplace.json",
	} {
		path := filepath.Join(root, rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Claude Code plugin component %s must exist: %v", rel, err)
		}
	}

	for _, rel := range []string{
		"skills",
		"agents",
		"hooks",
		"commands",
		"output-styles",
		"themes",
		"monitors",
		".mcp.json",
	} {
		path := filepath.Join(root, ".claude-plugin", rel)
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("%s must live at plugin root, not under .claude-plugin/", rel)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
}

func TestClaudeCodePluginAgentsUseOnlySupportedFrontmatterFields(t *testing.T) {
	allowed := map[string]bool{
		"name": true, "description": true, "model": true, "effort": true,
		"maxTurns": true, "tools": true, "disallowedTools": true,
		"skills": true, "memory": true, "background": true, "isolation": true,
		"color": true,
	}
	ignoredForPluginAgents := map[string]bool{
		"hooks": true, "mcpServers": true, "permissionMode": true,
	}

	root := filepath.Join(repoRoot(t), "claudecode", "agents")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read claudecode agents: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		fm := readYAMLFrontmatter(t, path)
		for key := range fm {
			if ignoredForPluginAgents[key] {
				t.Fatalf("%s uses %q, which Claude Code ignores for plugin-shipped agents", path, key)
			}
			if !allowed[key] {
				t.Fatalf("%s uses unsupported plugin-agent frontmatter field %q", path, key)
			}
		}
		name := frontmatterString(fm["name"])
		if name == "" || !claudeCodePluginNameRE.MatchString(name) {
			t.Fatalf("%s has invalid Claude Code agent name %q", path, name)
		}
		if want := strings.TrimSuffix(entry.Name(), ".md"); name != want {
			t.Fatalf("%s name = %q, want filename-derived %q for predictable plugin-scoped id", path, name, want)
		}
		if frontmatterString(fm["description"]) == "" {
			t.Fatalf("%s must set description so Claude Code can delegate appropriately", path)
		}
		if isolation := frontmatterString(fm["isolation"]); isolation != "" && isolation != "worktree" {
			t.Fatalf("%s isolation = %q, the only plugin-supported value is worktree", path, isolation)
		}
	}
}

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

func TestClaudeCodePluginChangelogMentionsManifestVersion(t *testing.T) {
	var manifest struct {
		Version string `json:"version"`
	}
	raw := readRepoFile(t, "../../claudecode/.claude-plugin/plugin.json")
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("plugin.json must be valid JSON: %v", err)
	}
	if manifest.Version == "" {
		t.Fatal("plugin.json must set version")
	}
	changelog := readRepoFile(t, "../../claudecode/CHANGELOG.md")
	if !strings.Contains(changelog, "## ["+manifest.Version+"]") {
		t.Fatalf("CHANGELOG.md must include an entry for plugin version %s", manifest.Version)
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
		if isUpstreamHumanizerSkillPath(path) {
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

func TestClaudeCodeSkillsUseOfficialInvocationContract(t *testing.T) {
	root := filepath.Join(repoRoot(t), "claudecode", "skills")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "SKILL.md" {
			return nil
		}
		if isUpstreamHumanizerSkillPath(path) {
			return nil
		}
		fm := readYAMLFrontmatter(t, path)
		description := frontmatterString(fm["description"])
		if description == "" {
			t.Fatalf("%s must set description so Claude Code can decide when to invoke the skill", path)
		}
		whenToUse := frontmatterString(fm["when_to_use"])
		if n := len([]rune(description + whenToUse)); n > 1536 {
			t.Fatalf("%s description plus when_to_use is %d chars; Claude Code truncates skill listings at 1536", path, n)
		}
		if name := frontmatterString(fm["name"]); name != "" && !claudeCodePluginNameRE.MatchString(name) {
			t.Fatalf("%s name = %q, want lowercase plugin-safe skill display name", path, name)
		}
		for _, key := range []string{"user-invocable", "disable-model-invocation"} {
			if raw, ok := fm[key]; ok {
				if _, ok := raw.(bool); !ok {
					t.Fatalf("%s %s must be boolean when present", path, key)
				}
			}
		}
		if tools, ok := fm["allowed-tools"]; ok {
			for _, tool := range frontmatterStringList(tools) {
				if tool == "AskUserQuestion" {
					t.Fatalf("%s must not preapprove AskUserQuestion; Anban plugin agents are zero-interaction pipelines", path)
				}
			}
		}
		if context := frontmatterString(fm["context"]); context != "" && context != "fork" {
			t.Fatalf("%s context = %q, the Claude Code skill contract only supports fork", path, context)
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
		if skill == "humanizer" {
			continue
		}
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

func readYAMLFrontmatter(t *testing.T, path string) map[string]any {
	t.Helper()
	body := readRepoFile(t, path)
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

func frontmatterString(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func frontmatterStringList(raw any) []string {
	switch v := raw.(type) {
	case string:
		return strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		})
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := frontmatterString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
