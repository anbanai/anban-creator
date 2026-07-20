package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClaudeAgentsOwnFinalFeedback(t *testing.T) {
	agents := []string{
		"designer",
		"ecommerce",
		"live-slicer",
		"moments",
		"montage",
		"seednote",
		"videocreator",
		"videoeditor",
		"wechatarticle",
	}

	for _, name := range agents {
		t.Run(name, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join("../../claudecode/agents", name+".md"))
			calls, ownedCalls := claudeAgentFeedbackCallCounts(body, name)
			if calls != 1 {
				t.Errorf("%s agent has %d submit_agent_feedback call expressions, want exactly one", name, calls)
			}
			if ownedCalls != 1 {
				t.Errorf("%s agent has %d submit_agent_feedback calls with the expected agent_name, want exactly one", name, ownedCalls)
			}
		})
	}
}

func TestClaudeAgentFeedbackCallsMatchMCPSchema(t *testing.T) {
	agents := []string{
		"designer", "ecommerce", "live-slicer", "moments", "montage",
		"seednote", "videocreator", "videoeditor", "wechatarticle",
	}
	allowedArgs := map[string]bool{
		"task_id": true, "agent_name": true, "scores": true,
		"errors": true, "optimizations": true, "summary": true,
	}

	for _, name := range agents {
		t.Run(name, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join("../../claudecode/agents", name+".md"))
			calls, err := documentedToolCalls(body, "submit_agent_feedback")
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 {
				t.Fatalf("%s agent has %d feedback calls, want exactly one", name, len(calls))
			}
			call := calls[0]
			if strings.Contains(call, "...") {
				t.Fatal("feedback call must not contain literal ellipsis")
			}
			args, err := documentedCallArgs(call)
			if err != nil {
				t.Fatal(err)
			}
			for arg := range args {
				if !allowedArgs[arg] {
					t.Errorf("feedback call contains unsupported argument %q", arg)
				}
			}
			taskID := strings.TrimSpace(args["task_id"])
			if taskID != "$TASK_ID" {
				quotedTaskID, ok := documentedStringValue(taskID)
				if !ok || strings.TrimSpace(quotedTaskID) == "" {
					t.Errorf("task_id = %q, want $TASK_ID or a non-empty quoted string", args["task_id"])
				}
			}
			agentName, ok := documentedStringValue(args["agent_name"])
			if !ok || agentName != name {
				t.Errorf("agent_name = %q, want quoted exact value %q", args["agent_name"], name)
			}
			scores, ok := documentedStringValue(args["scores"])
			if !ok || !json.Valid([]byte(scores)) {
				t.Errorf("scores = %q, want a quoted valid JSON object", args["scores"])
			} else {
				var dimensions map[string]float64
				if err := json.Unmarshal([]byte(scores), &dimensions); err != nil {
					t.Errorf("scores must decode as a numeric JSON object: %v", err)
				}
				for _, dimension := range []string{"quality", "completeness", "efficiency"} {
					if _, exists := dimensions[dimension]; !exists {
						t.Errorf("scores missing %q", dimension)
					}
				}
			}
			for _, field := range []string{"errors", "optimizations", "summary"} {
				if _, ok := documentedStringValue(args[field]); !ok {
					t.Errorf("%s = %q, want a string value", field, args[field])
				}
			}
		})
	}

	schemaSource := readRepoFile(t, "../../server/mcp/agent_feedback_tools.go")
	for _, name := range agents {
		if !strings.Contains(schemaSource, name) {
			t.Errorf("submit_agent_feedback agent_name description missing %q", name)
		}
	}
}

func TestDocumentedToolCallParserHandlesNestedValues(t *testing.T) {
	body := "tool-list: submit_agent_feedback\n" +
		"submit_agent_feedback \n(\n" +
		"  task_id=$TASK_ID,\n" +
		"  agent_name=\"designer\",\n" +
		"  scores='{\"quality\":8,\"completeness\":9,\"efficiency\":7}',\n" +
		"  summary=\"checked (including commas, parentheses)\"\n" +
		")"
	calls, err := documentedToolCalls(body, "submit_agent_feedback")
	if err != nil || len(calls) != 1 {
		t.Fatalf("calls = %#v, err = %v", calls, err)
	}
	args, err := documentedCallArgs(calls[0])
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := documentedStringValue(args["summary"]); got != "checked (including commas, parentheses)" {
		t.Fatalf("summary = %q", got)
	}
}

func documentedToolCalls(body, tool string) ([]string, error) {
	var calls []string
	for searchAt := 0; searchAt < len(body); {
		relativeAt := strings.Index(body[searchAt:], tool)
		if relativeAt < 0 {
			break
		}
		start := searchAt + relativeAt
		afterName := start + len(tool)
		if (start > 0 && isDocumentedIdentifierByte(body[start-1])) || (afterName < len(body) && isDocumentedIdentifierByte(body[afterName])) {
			searchAt = afterName
			continue
		}
		openAt := afterName
		for openAt < len(body) && (body[openAt] == ' ' || body[openAt] == '\t' || body[openAt] == '\r' || body[openAt] == '\n') {
			openAt++
		}
		if openAt >= len(body) || body[openAt] != '(' {
			searchAt = afterName
			continue
		}
		end, err := balancedCallEnd(body, openAt)
		if err != nil {
			return nil, fmt.Errorf("parse %s call at byte %d: %w", tool, start, err)
		}
		calls = append(calls, body[start:end])
		searchAt = end
	}
	return calls, nil
}

func isDocumentedIdentifierByte(char byte) bool {
	return char == '_' || char == '-' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
}

func balancedCallEnd(body string, openAt int) (int, error) {
	depth := 0
	var quote byte
	escaped := false
	for i := openAt; i < len(body); i++ {
		char := body[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated call")
}

func documentedCallArgs(call string) (map[string]string, error) {
	openAt := strings.Index(call, "(")
	if openAt < 0 || !strings.HasSuffix(strings.TrimSpace(call), ")") {
		return nil, fmt.Errorf("invalid documented call %q", call)
	}
	body := strings.TrimSpace(call)[openAt+1 : len(strings.TrimSpace(call))-1]
	parts, err := splitDocumentedTopLevel(body, ',')
	if err != nil {
		return nil, err
	}
	args := make(map[string]string, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		assignment, err := splitDocumentedTopLevel(part, '=')
		if err != nil || len(assignment) != 2 {
			return nil, fmt.Errorf("invalid documented argument %q", part)
		}
		name := strings.TrimSpace(assignment[0])
		if _, duplicate := args[name]; duplicate {
			return nil, fmt.Errorf("duplicate documented argument %q", name)
		}
		args[name] = strings.TrimSpace(assignment[1])
	}
	return args, nil
}

func splitDocumentedTopLevel(value string, separator byte) ([]string, error) {
	var parts []string
	start := 0
	depth := 0
	var quote byte
	escaped := false
	for i := 0; i < len(value); i++ {
		char := value[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		switch char {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case separator:
			if depth == 0 {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
		if depth < 0 {
			return nil, fmt.Errorf("unbalanced documented value %q", value)
		}
	}
	if quote != 0 || depth != 0 {
		return nil, fmt.Errorf("unbalanced documented value %q", value)
	}
	return append(parts, value[start:]), nil
}

func documentedStringValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != value[len(value)-1] || (value[0] != '\'' && value[0] != '"') {
		return "", false
	}
	if value[0] == '\'' {
		return value[1 : len(value)-1], true
	}
	decoded, err := strconv.Unquote(value)
	return decoded, err == nil
}

func TestClaudeAgentFinalFeedbackMatching(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantCalls int
		wantOwned int
	}{
		{
			name:      "exact multiline owner",
			body:      "submit_agent_feedback(\n  task_id=$TASK_ID,\n  agent_name = \"designer\",\n  summary=\"done\"\n)",
			wantCalls: 1,
			wantOwned: 1,
		},
		{
			name:      "owner suffix is not exact",
			body:      `submit_agent_feedback(task_id=$TASK_ID, agent_name="designer-extra", summary="done")`,
			wantCalls: 1,
			wantOwned: 0,
		},
		{
			name:      "extra mismatched call is still counted",
			body:      "submit_agent_feedback(agent_name=\"designer\")\nsubmit_agent_feedback(agent_name=\"other\")",
			wantCalls: 2,
			wantOwned: 1,
		},
		{
			name:      "tool list mention is ignored",
			body:      "tools: submit_agent_feedback\nsubmit_agent_feedback(agent_name='designer')",
			wantCalls: 1,
			wantOwned: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, ownedCalls := claudeAgentFeedbackCallCounts(tt.body, "designer")
			if calls != tt.wantCalls || ownedCalls != tt.wantOwned {
				t.Fatalf("call counts = (%d, %d), want (%d, %d)", calls, ownedCalls, tt.wantCalls, tt.wantOwned)
			}
		})
	}
}

func TestClaudeAgentFeedbackFollowsDeliveryReport(t *testing.T) {
	tests := []struct {
		name         string
		anchor       string
		summaryTerms []string
	}{
		{
			name:         "wechatarticle",
			anchor:       "**产出**：`$DIR/draft.json`",
			summaryTerms: []string{"所选模板", "草稿状态", "Vision 校验通过率"},
		},
		{
			name:         "designer",
			anchor:       "进度报告格式：",
			summaryTerms: []string{"图片总数", "一致性状态", "人工复核数量"},
		},
		{
			name:         "live-slicer",
			anchor:       "若流程中断，报告要包含：",
			summaryTerms: []string{"成功/失败切片数", "输出目录", "可恢复 warning"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join("../../claudecode/agents", tt.name+".md"))
			if err := validateClaudeAgentFeedbackContract(body, tt.name, tt.anchor, tt.summaryTerms); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClaudeAgentFeedbackContractRejectsMutations(t *testing.T) {
	const valid = "FINAL REPORT\nsubmit_agent_feedback(agent_name=\"designer\", summary=\"image count, consistency, manual review\")"
	tests := []struct {
		name string
		body string
	}{
		{
			name: "feedback before report",
			body: "submit_agent_feedback(agent_name=\"designer\", summary=\"image count, consistency, manual review\")\nFINAL REPORT",
		},
		{
			name: "missing summary field",
			body: strings.Replace(valid, "manual review", "review", 1),
		},
	}

	if err := validateClaudeAgentFeedbackContract(valid, "designer", "FINAL REPORT", []string{"image count", "consistency", "manual review"}); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateClaudeAgentFeedbackContract(tt.body, "designer", "FINAL REPORT", []string{"image count", "consistency", "manual review"}); err == nil {
				t.Fatal("mutated feedback contract unexpectedly passed")
			}
		})
	}
}

func claudeAgentFeedbackCallCounts(body, name string) (int, int) {
	callPattern := regexp.MustCompile(`submit_agent_feedback\s*\(`)
	quotedName := regexp.QuoteMeta(name)
	ownedCallPattern := regexp.MustCompile(fmt.Sprintf(
		`(?s)submit_agent_feedback\s*\([^)]*?\bagent_name\s*=\s*(?:"%s"|'%s')\s*(?:,|\))`,
		quotedName,
		quotedName,
	))
	return len(callPattern.FindAllStringIndex(body, -1)), len(ownedCallPattern.FindAllStringIndex(body, -1))
}

func validateClaudeAgentFeedbackContract(body, name, anchor string, summaryTerms []string) error {
	calls, ownedCalls := claudeAgentFeedbackCallCounts(body, name)
	if calls != 1 || ownedCalls != 1 {
		return fmt.Errorf("%s feedback call counts = (%d total, %d owned), want (1, 1)", name, calls, ownedCalls)
	}
	anchorAt := strings.Index(body, anchor)
	if anchorAt < 0 {
		return fmt.Errorf("%s agent missing delivery anchor %q", name, anchor)
	}
	callAt := regexp.MustCompile(`submit_agent_feedback\s*\(`).FindStringIndex(body)
	if callAt == nil || callAt[0] <= anchorAt {
		return fmt.Errorf("%s feedback must occur after delivery anchor %q", name, anchor)
	}
	callEnd := strings.Index(body[callAt[0]:], ")")
	if callEnd < 0 {
		return fmt.Errorf("%s feedback call is not terminated", name)
	}
	call := body[callAt[0] : callAt[0]+callEnd+1]
	for _, term := range summaryTerms {
		if !strings.Contains(call, term) {
			return fmt.Errorf("%s feedback summary missing %q", name, term)
		}
	}
	return nil
}

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
		if !strings.HasPrefix(group.Matcher, "^anban:") || !strings.HasSuffix(group.Matcher, "$") {
			t.Fatalf("SubagentStop matcher %q must be an anchored Claude Code plugin-scoped agent name, e.g. ^anban:seednote$", group.Matcher)
		}
	}
}

func TestClaudeCodeCompletionHooksUseSupportedRoles(t *testing.T) {
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	raw := readRepoFile(t, "../../claudecode/hooks/hooks.json")
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("hooks.json must be valid JSON: %v", err)
	}

	if _, exists := cfg.Hooks["TaskCompleted"]; exists {
		t.Fatal("hooks.json must not register TaskCompleted hooks")
	}

	expected := map[string]string{
		"^anban:seednote$":     "${CLAUDE_PLUGIN_ROOT}/hooks/seednote-quality-gate.sh",
		"^anban:videocreator$": "${CLAUDE_PLUGIN_ROOT}/hooks/videocreator-quality-gate.sh",
		"^anban:videoeditor$":  "${CLAUDE_PLUGIN_ROOT}/hooks/videoeditor-quality-gate.sh",
	}
	groups := cfg.Hooks["SubagentStop"]
	if len(groups) != len(expected) {
		t.Fatalf("SubagentStop has %d groups, want exactly %d", len(groups), len(expected))
	}
	seen := make(map[string]bool, len(groups))
	for _, group := range groups {
		command, ok := expected[group.Matcher]
		if !ok {
			t.Fatalf("SubagentStop has unsupported matcher %q", group.Matcher)
		}
		if seen[group.Matcher] {
			t.Fatalf("SubagentStop matcher %q is registered more than once", group.Matcher)
		}
		seen[group.Matcher] = true
		if len(group.Hooks) != 1 {
			t.Fatalf("SubagentStop matcher %q has %d hooks, want exactly one command hook", group.Matcher, len(group.Hooks))
		}
		hook := group.Hooks[0]
		if hook.Type != "command" {
			t.Fatalf("SubagentStop matcher %q hook type = %q, want command", group.Matcher, hook.Type)
		}
		if hook.Command != command {
			t.Fatalf("SubagentStop matcher %q command = %q, want %q", group.Matcher, hook.Command, command)
		}
	}
	for matcher := range expected {
		if !seen[matcher] {
			t.Fatalf("SubagentStop missing matcher %q", matcher)
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
