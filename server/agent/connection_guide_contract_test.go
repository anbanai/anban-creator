package agent

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var connectionGuideVariablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bexport[ \t]+([A-Z][A-Z0-9_]*)[ \t]*=`),
	regexp.MustCompile(`(?m)^[ \t]*["']([A-Z][A-Z0-9_]*)["'][ \t]*:`),
}

var claudePluginAPIKeyPattern = regexp.MustCompile(`(?i)\bapi_key\b`)
var pluginCredentialPattern = regexp.MustCompile(`(?i)\b(?:api_key|credential|token)\b`)

type connectionGuideCredentialMode string

const (
	claudePluginUserConfig connectionGuideCredentialMode = "Claude plugin userConfig"
	codexEnvironment       connectionGuideCredentialMode = "Codex environment"
)

func TestConnectionGuidesUseFixedPluginEndpoint(t *testing.T) {
	root := repoRoot(t)
	for _, guide := range []struct {
		relPath string
		mode    connectionGuideCredentialMode
	}{
		{relPath: "studio/public/claude/index.html", mode: claudePluginUserConfig},
		{relPath: "studio/public/codex/index.html", mode: codexEnvironment},
	} {
		body := readTextFile(t, filepath.Join(root, filepath.FromSlash(guide.relPath)))
		for _, problem := range connectionGuideContractProblems(body, guide.mode) {
			t.Errorf("%s: %s", guide.relPath, problem)
		}
	}
}

func TestConnectionGuideContractScanner(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		mode        connectionGuideCredentialMode
		wantProblem bool
	}{
		{
			name: "Claude plugin api_key user config",
			body: "插件使用固定服务地址，在插件配置的 api_key 字段填写密钥。",
			mode: claudePluginUserConfig,
		},
		{
			name: "Claude negative environment clarification",
			body: "插件使用固定服务地址，在插件配置的 api_key 字段填写密钥，无需修改环境变量或 settings.json。",
			mode: claudePluginUserConfig,
		},
		{
			name: "Claude environment key guidance",
			body: `插件使用固定服务地址，在插件配置的 api_key 字段填写密钥。把凭据写入 settings.json：
  "ANBAN_API_KEY": "key"`,
			mode:        claudePluginUserConfig,
			wantProblem: true,
		},
		{
			name:        "Claude narrative environment key guidance",
			body:        "插件使用固定服务地址，在插件配置的 api_key 字段填写密钥，同时在环境变量或 settings.json 中设置 ANBAN_API_KEY。",
			mode:        claudePluginUserConfig,
			wantProblem: true,
		},
		{
			name:        "Claude mixed-polarity environment guidance",
			body:        "插件使用固定服务地址，在插件配置的 api_key 字段填写密钥。Do not rely only on environment; write ANBAN_API_KEY to settings.json.",
			mode:        claudePluginUserConfig,
			wantProblem: true,
		},
		{
			name: "Codex API key environment config",
			body: `The plugin uses a built-in endpoint; configure only the API key.
export ANBAN_API_KEY="key"
const GUIDE_STEP_ID = 4`,
			mode: codexEnvironment,
		},
		{
			name: "Codex mixed environment and plugin config",
			body: `The plugin uses a built-in endpoint.
export ANBAN_API_KEY="key"
Also fill the plugin userConfig api_key field.`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "Codex negative plugin config clarification",
			body: `The plugin uses a built-in endpoint. Do not fill the plugin userConfig api_key field.
export ANBAN_API_KEY="key"`,
			mode: codexEnvironment,
		},
		{
			name: "Codex adjacent negative credential clarification",
			body: `The plugin uses a built-in endpoint.
export ANBAN_API_KEY="key"
Configure plugin config display options. Do not fill api_key.`,
			mode: codexEnvironment,
		},
		{
			name: "Codex cross-line plugin config guidance",
			body: `插件使用固定服务地址。
export ANBAN_API_KEY="key"
打开插件配置。
在那里填写 api_key 密钥。`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "Codex mixed-polarity plugin config guidance",
			body: `The plugin uses a built-in endpoint.
export ANBAN_API_KEY="key"
Do not rely only on plugin config; fill the plugin userConfig api_key field.`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "Codex affirmative plugin config credential guidance",
			body: `The plugin uses a built-in endpoint. Fill the plugin configuration credential field.
export ANBAN_API_KEY="key"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "Codex extra environment variable",
			body: `官方服务地址已内置在插件中，连接时只需配置 API Key。
export ANBAN_API_KEY="key"
export TOKEN="second connection value"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "case-insensitive configurable endpoint",
			body: `官方服务地址已内置在插件中，连接时只需配置 API Key。CUSTOM ENDPOINT is supported.
export ANBAN_API_KEY="key"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "configurable base URL",
			body: `The plugin uses a fixed endpoint and optionally supports a configurable BASE URL.
export ANBAN_API_KEY="key"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "self-host and local service guidance",
			body: `The plugin uses a fixed endpoint. SELF-HOST users can select a local service.
export ANBAN_API_KEY="key"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "Chinese own service address",
			body: `官方服务地址已内置在插件中，也可以填写自己的服务地址。
export ANBAN_API_KEY="key"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "Chinese self-host and local service guidance",
			body: `官方插件使用固定服务地址，自建版本可以连接本地服务。
export ANBAN_API_KEY="key"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
		{
			name: "legacy MCP host",
			body: `The plugin uses a fixed endpoint. Connect to https://api.creator.anbanai.com/mcp.
export ANBAN_API_KEY="key"`,
			mode:        codexEnvironment,
			wantProblem: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotProblem := len(connectionGuideContractProblems(tc.body, tc.mode)) > 0
			if gotProblem != tc.wantProblem {
				t.Fatalf("connectionGuideContractProblems() problem = %v, want %v; findings: %v", gotProblem, tc.wantProblem, connectionGuideContractProblems(tc.body, tc.mode))
			}
		})
	}
}

func connectionGuideContractProblems(body string, mode connectionGuideCredentialMode) []string {
	var problems []string
	connectionVariables := make(map[string]struct{})
	for _, pattern := range connectionGuideVariablePatterns {
		for _, match := range pattern.FindAllStringSubmatch(body, -1) {
			connectionVariables[match[1]] = struct{}{}
		}
	}
	lowerBody := strings.ToLower(body)
	switch mode {
	case claudePluginUserConfig:
		if !claudePluginAPIKeyPattern.MatchString(body) {
			problems = append(problems, "missing Claude plugin api_key configuration")
		}
		if !containsConnectionGuideConcept(lowerBody, "插件配置", "plugin config", "userconfig", "user config") {
			problems = append(problems, "missing Claude plugin userConfig guidance")
		}
		if len(connectionVariables) != 0 || containsAffirmativeEnvironmentCredentialGuidance(lowerBody) {
			problems = append(problems, "Claude guide must not configure environment credentials")
		}
	case codexEnvironment:
		if _, ok := connectionVariables["ANBAN_API_KEY"]; !ok {
			problems = append(problems, "missing ANBAN_API_KEY environment configuration")
		}
		if len(connectionVariables) != 1 {
			problems = append(problems, "Codex connection configuration must contain only ANBAN_API_KEY")
		}
		if containsAffirmativePluginCredentialGuidance(lowerBody) {
			problems = append(problems, "Codex guide must not include Claude plugin credential configuration")
		}
	default:
		problems = append(problems, "unknown connection guide credential mode")
	}

	hasPlugin := containsConnectionGuideConcept(lowerBody, "插件", "plugin")
	hasFixedBehavior := containsConnectionGuideConcept(lowerBody, "内置", "固定", "自动", "built-in", "built in", "fixed", "automatic")
	hasEndpointConcept := containsConnectionGuideConcept(lowerBody, "服务地址", "端点", "endpoint", "service address", "service url")
	if !hasPlugin || !hasFixedBehavior || !hasEndpointConcept {
		problems = append(problems, "missing fixed or built-in plugin endpoint guidance")
	}

	for _, forbidden := range []string{
		"api_url",
		"api.creator.anbanai.com",
		"base url",
		"api url",
		"configurable endpoint",
		"configurable service",
		"configurable url",
		"custom endpoint",
		"custom service",
		"custom url",
		"endpoint override",
		"override endpoint",
		"self-host",
		"self host",
		"localhost",
		"local endpoint",
		"local service",
		"own service address",
		"自建",
		"本地服务",
		"本地端点",
		"自己的服务地址",
		"自定义服务",
		"自定义地址",
		"自定义端点",
		"可配置端点",
		"可配置服务地址",
		"服务地址可配置",
	} {
		if strings.Contains(lowerBody, forbidden) {
			problems = append(problems, "contains forbidden endpoint override guidance "+forbidden)
		}
	}
	return problems
}

func containsConnectionGuideConcept(body string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(body, candidate) {
			return true
		}
	}
	return false
}

func containsAffirmativePluginCredentialGuidance(body string) bool {
	for _, clauses := range connectionGuideInstructionWindows(body) {
		nonnegativeClauses := connectionGuideNonnegativeClauses(clauses)
		affirmativeWindow := strings.Join(nonnegativeClauses, " ")
		hasTarget := containsConnectionGuideConcept(affirmativeWindow, "插件配置", "plugin config", "userconfig", "user config")
		hasCredential := pluginCredentialPattern.MatchString(affirmativeWindow) || containsConnectionGuideConcept(affirmativeWindow, "api key", "密钥", "凭据")
		if !hasTarget || !hasCredential {
			continue
		}
		hasNonnegativeTargetClause := false
		for _, clause := range clauses {
			if !connectionGuideClauseIsNegative(clause) && containsConnectionGuideConcept(clause,
				"插件配置", "plugin config", "userconfig", "user config") {
				hasNonnegativeTargetClause = true
				break
			}
		}
		for _, clause := range clauses {
			if connectionGuideClauseIsNegative(clause) {
				continue
			}
			actionClause := strings.NewReplacer(
				"插件配置", "",
				"plugin config", "",
				"userconfig", "",
				"user config", "",
			).Replace(clause)
			hasAction := containsConnectionGuideConcept(actionClause,
				"设置", "写入", "配置", "填写", "填入", "set", "write", "configure", "fill")
			hasClauseTarget := containsConnectionGuideConcept(clause,
				"插件配置", "plugin config", "userconfig", "user config")
			hasClauseCredential := pluginCredentialPattern.MatchString(clause) || containsConnectionGuideConcept(clause, "api key", "密钥", "凭据")
			if hasAction && (hasClauseTarget || hasClauseCredential && hasNonnegativeTargetClause) {
				return true
			}
		}
	}
	return false
}

func containsAffirmativeEnvironmentCredentialGuidance(body string) bool {
	for _, clauses := range connectionGuideInstructionWindows(body) {
		nonnegativeClauses := connectionGuideNonnegativeClauses(clauses)
		affirmativeWindow := strings.Join(nonnegativeClauses, " ")
		hasTarget := containsConnectionGuideConcept(affirmativeWindow,
			"环境变量", "environment", "settings_json", "settings json")
		hasCredential := containsConnectionGuideConcept(affirmativeWindow,
			"anban_api_key", "密钥", "凭据", "key", "credential", "token")
		if !hasTarget || !hasCredential {
			continue
		}
		hasNonnegativeTargetClause := false
		for _, clause := range clauses {
			if !connectionGuideClauseIsNegative(clause) && containsConnectionGuideConcept(clause,
				"环境变量", "environment", "settings_json", "settings json") {
				hasNonnegativeTargetClause = true
				break
			}
		}
		for _, clause := range clauses {
			if connectionGuideClauseIsNegative(clause) {
				continue
			}
			hasAction := containsConnectionGuideConcept(clause,
				"设置", "写入", "配置", "填写", "填入", "set", "write", "configure", "fill")
			hasClauseTarget := containsConnectionGuideConcept(clause,
				"环境变量", "environment", "settings_json", "settings json")
			hasClauseCredential := containsConnectionGuideConcept(clause,
				"anban_api_key", "密钥", "凭据", "key", "credential", "token")
			if hasAction && (hasClauseTarget || hasClauseCredential && hasNonnegativeTargetClause) {
				return true
			}
		}
	}
	return false
}

func connectionGuideInstructionWindows(body string) [][]string {
	normalized := strings.ReplaceAll(body, "settings.json", "settings_json")
	rawClauses := strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case '\n', '\r', '.', '。', '!', '！', '?', '？', ';', '；', ',', '，':
			return true
		default:
			return false
		}
	})
	clauses := make([]string, 0, len(rawClauses))
	for _, clause := range rawClauses {
		if clause = strings.Join(strings.Fields(clause), " "); clause != "" {
			clauses = append(clauses, clause)
		}
	}
	windows := make([][]string, 0, len(clauses)*2)
	for i, clause := range clauses {
		windows = append(windows, []string{clause})
		if i+1 < len(clauses) {
			windows = append(windows, []string{clause, clauses[i+1]})
		}
	}
	return windows
}

func connectionGuideClauseIsNegative(clause string) bool {
	return containsConnectionGuideConcept(clause,
		"无需", "不需要", "不要", "切勿", "不得",
		"do not", "don't", "without", "need not", "must not")
}

func connectionGuideNonnegativeClauses(clauses []string) []string {
	nonnegative := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		if !connectionGuideClauseIsNegative(clause) {
			nonnegative = append(nonnegative, clause)
		}
	}
	return nonnegative
}
