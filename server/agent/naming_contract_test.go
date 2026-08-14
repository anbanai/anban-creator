package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAnbanCreatorNamingContract(t *testing.T) {
	root := repoRoot(t)

	assertJSONField(t, filepath.Join(root, "plugins", ".claude-plugin", "plugin.json"), "name", "anban")
	assertClaudePluginUserConfig(t, filepath.Join(root, "plugins", ".claude-plugin", "plugin.json"))
	assertJSONField(t, filepath.Join(root, "plugins", ".codex-plugin", "plugin.json"), "name", "anban")
	assertClaudeMarketplacePlugin(t, filepath.Join(root, "plugins", ".claude-plugin", "marketplace.json"))

	assertOnlyMCPServerKey(t, filepath.Join(root, "plugins", ".mcp.json"), "creator")

	for _, path := range []string{
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "plugins", "README.md"),
		filepath.Join(root, "deploy/docker/Dockerfile.agent-article"),
		filepath.Join(root, "deploy/docker/Dockerfile.server"),
		filepath.Join(root, "studio", "src", "pages", "PluginsPage.tsx"),
		filepath.Join(root, "studio", "public", "claude", "index.html"),
		filepath.Join(root, "studio", "public", "codex", "index.html"),
		filepath.Join(root, "miniapp", "src", "pages", "connect", "claude-code.vue"),
		filepath.Join(root, "miniapp", "src", "pages", "connect", "codex.vue"),
		filepath.Join(root, "plugins", "README.md"),
		filepath.Join(root, "plugins", "install", "install-subagents.sh"),
	} {
		assertFileNotContains(t, path, "anbancreator"+"@anbanai")
		assertFileNotContains(t, path, "creator"+"@anbanai")
		assertFileNotContains(t, path, "plugin install "+"anban-creator")
		assertFileNotContains(t, path, "anban-creator"+"@anbanai")
		assertFileNotContains(t, path, "ab"+"c:")
		assertFileNotContains(t, path, "案"+"板")
	}
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "plugin@marketplace")
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "anban@anbanai")
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "The MCP server key is `creator`")
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "anban:<agent>")
	assertFileContains(t, filepath.Join(root, "plugins", "README.md"), "claude plugin install --scope user anban@anbanai")
	assertFileContains(t, filepath.Join(root, "plugins", "README.md"), "/anban:anban-setup")
	assertFileContains(t, filepath.Join(root, "plugins", "README.md"), "/anban:article")
	assertFileContains(t, filepath.Join(root, "plugins", "README.md"), "--agent anban:article")
	assertFileNotExists(t, filepath.Join(root, "plugins", "agents", "wechat"+"article.md"))
	assertFileNotExists(t, filepath.Join(root, "plugins", "agents", "wechat"+"article.toml"))
	assertFileNotContains(t, filepath.Join(root, "plugins", "README.md"), "--dangerously-skip-permissions")
	assertFileContains(t, filepath.Join(root, "plugins", "README.md"), "插件内的 MCP server key 固定为 `creator`")
	assertFileNotExists(t, filepath.Join(root, "plugins", "CLAUDE.md"))
	assertFileContains(t, filepath.Join(root, "plugins", "docs", "plugin-development.md"), "Plugin developer notes")
	assertJSONMCPServerURL(t, filepath.Join(root, "plugins", ".mcp.json"), "creator", "https://creator.anbanai.com/mcp")
	assertFileNotContains(t, filepath.Join(root, "plugins", ".mcp.json"), "user_config.api_url")
	assertFileContains(t, filepath.Join(root, "plugins", ".mcp.json"), "Bearer ${user_config.api_key}")
	assertTrackedFilesDoNotContainLegacyNames(t, root)
	assertBusinessLayerFilesDoNotContainHostMCPPrefixes(t, root)
}

func TestAnbanCreatorNamingContractUsesFixedMCPEndpoint(t *testing.T) {
	root := repoRoot(t)
	const endpoint = "https://creator.anbanai.com/mcp"

	cases := []struct {
		name      string
		path      string
		assertURL func(*testing.T, string, string)
	}{
		{
			name: "claude plugin",
			path: filepath.Join(root, "plugins", ".mcp.json"),
			assertURL: func(t *testing.T, path, want string) {
				assertJSONMCPServerURL(t, path, "creator", want)
			},
		},
		{
			name: "codex registration",
			path: filepath.Join(root, "plugins", "install", "agents-registration.toml"),
			assertURL: func(t *testing.T, path, want string) {
				assertTOMLMCPServerURL(t, path, "creator", want)
			},
		},
	}

	agentPaths, err := filepath.Glob(filepath.Join(root, "plugins", "agents", "*.toml"))
	if err != nil {
		t.Fatalf("glob Codex agent configurations: %v", err)
	}
	if len(agentPaths) == 0 {
		t.Fatal("no plugins/agents/*.toml files found")
	}
	for _, path := range agentPaths {
		cases = append(cases, struct {
			name      string
			path      string
			assertURL func(*testing.T, string, string)
		}{
			name: "Codex agent " + strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			path: path,
			assertURL: func(t *testing.T, path, want string) {
				assertTOMLMCPServerURL(t, path, "creator", want)
			},
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assertURL(t, tc.path, endpoint)

			// Source scans intentionally remain separate from semantic URL lookup.
			body := readTextFile(t, tc.path)
			for _, obsolete := range []string{
				"ANBAN_API_URL",
				"user_config.api_url",
				"https://api.creator.anbanai.com/mcp",
			} {
				if strings.Contains(body, obsolete) {
					t.Errorf("%s still contains obsolete MCP connection term %q", tc.path, obsolete)
				}
			}
		})
	}
}

func TestAnbanSetupExamplesUseFixedHostedEndpoint(t *testing.T) {
	path := filepath.Join(repoRoot(t), "plugins", "skills", "anban-setup", "references", "examples.md")
	body := readTextFile(t, path)

	for _, required := range []string{
		"https://creator.anbanai.com/mcp",
		"list_projects",
		"网络可达性",
		"Claude Code：检查插件 `api_key`",
		"Codex：用 `test -n \"$ANBAN_API_KEY\"`",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("%s missing fixed-endpoint setup guidance %q", path, required)
		}
	}
	for _, finding := range configurableSetupEndpointGuidance(body) {
		t.Errorf("%s contains configurable endpoint guidance %q", path, finding)
	}
}

func TestConfigurableSetupEndpointGuidanceScanner(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "legitimate negative wording", body: "固定端点不是 API URL 配置项。", want: false},
		{name: "obsolete manifest field", body: "修改 api_url 后重启。", want: true},
		{name: "obsolete environment variable", body: "设置 ANBAN_API_URL 后重启。", want: true},
		{name: "obsolete hosted endpoint", body: "连接 https://api.creator.anbanai.com/mcp。", want: true},
		{name: "custom endpoint guidance", body: "使用自定义 API 地址。", want: true},
		{name: "format diagnostic", body: "检查 API URL 格式。", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(configurableSetupEndpointGuidance(tc.body)) > 0; got != tc.want {
				t.Fatalf("configurable endpoint guidance = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPluginAgentCredentialDiagnosticsStayHostSpecific(t *testing.T) {
	root := repoRoot(t)
	for _, relPath := range []string{
		"plugins/agents/ecommerce.md",
	} {
		body := readTextFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
		for _, required := range []string{"`api_key`", "原始认证错误", "MCP"} {
			if !strings.Contains(body, required) {
				t.Errorf("%s missing Claude credential diagnostic %q", relPath, required)
			}
		}
		if strings.Contains(body, "ANBAN_API_KEY") {
			t.Errorf("%s must not inspect the Codex bearer-token environment variable", relPath)
		}
	}

	for _, relPath := range []string{
		"plugins/agents/ecommerce.toml",
	} {
		body := readTextFile(t, filepath.Join(root, filepath.FromSlash(relPath)))
		for _, required := range []string{
			`test -n "$ANBAN_API_KEY"`,
			`bearer_token_env_var = "ANBAN_API_KEY"`,
		} {
			if !strings.Contains(body, required) {
				t.Errorf("%s missing Codex credential diagnostic %q", relPath, required)
			}
		}
	}
}

func configurableSetupEndpointGuidance(body string) []string {
	var findings []string
	for _, forbidden := range []string{
		"api_url",
		"ANBAN_API_URL",
		"https://api.creator.anbanai.com/mcp",
		"自定义 API 地址",
		"检查 API URL 格式",
	} {
		if strings.Contains(body, forbidden) {
			findings = append(findings, forbidden)
		}
	}
	return findings
}

func TestAnbanCreatorNamingContractTOMLEndpointLookupIsSectionAware(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	body := `developer_instructions = """
[mcp_servers.creator]
url = "https://creator.anbanai.com/mcp"
"""

[unrelated]
url = "https://creator.anbanai.com/mcp"

[mcp_servers.creator]
url = "https://wrong.example/mcp"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write TOML fixture: %v", err)
	}

	if got := tomlStringInTable(t, path, "mcp_servers.creator", "url"); got != "https://wrong.example/mcp" {
		t.Fatalf("section-aware TOML lookup = %q, want actual mcp_servers.creator URL", got)
	}
}

func assertClaudeMarketplacePlugin(t *testing.T, path string) {
	t.Helper()
	var object struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Owner       named  `json:"owner"`
		Plugins     []struct {
			Name       string `json:"name"`
			Homepage   string `json:"homepage"`
			Repository string `json:"repository"`
			Author     named  `json:"author"`
		} `json:"plugins"`
	}
	readJSONFile(t, path, &object)
	if object.Name != "anbanai" {
		t.Fatalf("%s marketplace name = %q, want %q", path, object.Name, "anbanai")
	}
	if object.Owner.Name != "anbanai" {
		t.Fatalf("%s owner name = %q, want %q", path, object.Owner.Name, "anbanai")
	}
	if strings.TrimSpace(object.Description) == "" {
		t.Fatalf("%s marketplace description is required", path)
	}
	if len(object.Plugins) != 1 {
		t.Fatalf("%s has %d plugins, want exactly one", path, len(object.Plugins))
	}
	plugin := object.Plugins[0]
	if plugin.Name != "anban" {
		t.Fatalf("%s plugin name = %q, want %q", path, plugin.Name, "anban")
	}
	if plugin.Author.Name != "anbanai" {
		t.Fatalf("%s author name = %q, want %q", path, plugin.Author.Name, "anbanai")
	}
	for _, got := range []string{plugin.Homepage, plugin.Repository} {
		if !strings.Contains(got, "github.com/anbanai/creator-skills") {
			t.Fatalf("%s plugin URL = %q, want canonical plugin repository", path, got)
		}
	}
}

type named struct {
	Name string `json:"name"`
}

func assertTrackedFilesDoNotContainLegacyNames(t *testing.T, root string) {
	t.Helper()
	for _, path := range trackedFiles(t, root) {
		fullPath := filepath.Join(root, path)
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.IsDir() {
			continue
		}
		body := readTextFile(t, fullPath)
		for _, banned := range []string{
			"anbancreator" + "@anbanai",
			"creator" + "@anbanai",
			"anban-creator" + "@anbanai",
			"ab" + "c:",
			"案" + "板",
			"royal" + "rick",
			"ab" + "writer",
		} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(banned)) {
				t.Fatalf("%s still contains banned term %q", path, banned)
			}
		}
	}
}

func assertBusinessLayerFilesDoNotContainHostMCPPrefixes(t *testing.T, root string) {
	t.Helper()
	doublePrefixedMCPTool := regexp.MustCompile(`mcp__[A-Za-z0-9_-]+__mcp__[A-Za-z0-9_-]+__`)
	hostPrefixedMCPTool := regexp.MustCompile(`mcp__[A-Za-z0-9_-]+__`)
	invalidAnbanPrefix := "mcp__" + "anban__"
	invalidPluginPrefix := "mcp__" + "plugin_anban"
	invalidUnderscorePrefix := "mcp_" + "anban"
	invalidPluginName := "plugin_" + "anban"
	for _, fullPath := range businessLayerMCPDocs(t, root) {
		path, err := filepath.Rel(root, fullPath)
		if err != nil {
			t.Fatalf("rel %s: %v", fullPath, err)
		}
		body := readTextFile(t, fullPath)
		if match := hostPrefixedMCPTool.FindString(body); match != "" {
			t.Fatalf("%s contains host-prefixed MCP tool name %q; business docs must use bare MCP tool names", path, match)
		}
		if strings.Contains(body, invalidAnbanPrefix) {
			t.Fatalf("%s contains invalid host MCP prefix %q", path, invalidAnbanPrefix)
		}
		if strings.Contains(body, invalidPluginPrefix) {
			t.Fatalf("%s contains invalid host MCP prefix %q", path, invalidPluginPrefix)
		}
		if strings.Contains(body, invalidUnderscorePrefix) {
			t.Fatalf("%s contains invalid host MCP prefix %q", path, invalidUnderscorePrefix)
		}
		if strings.Contains(body, invalidPluginName) {
			t.Fatalf("%s contains invalid host MCP plugin prefix %q", path, invalidPluginName)
		}
		if match := doublePrefixedMCPTool.FindString(body); match != "" {
			t.Fatalf("%s contains double-prefixed MCP tool name %q", path, match)
		}
	}
}

func businessLayerMCPDocs(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	addFile := func(rel string) {
		path := filepath.Join(root, rel)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			t.Fatalf("stat %s: %v", rel, err)
		}
		if !info.IsDir() {
			out = append(out, path)
		}
	}
	addTree := func(rel string) {
		base := filepath.Join(root, rel)
		if _, err := os.Stat(base); err != nil {
			if os.IsNotExist(err) {
				return
			}
			t.Fatalf("stat %s: %v", rel, err)
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "dist", "build":
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".md", ".tsx", ".vue":
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", rel, err)
		}
	}

	addFile("CLAUDE.md")
	addFile(filepath.Join("plugins", "README.md"))
	addFile(filepath.Join("plugins", "docs", "plugin-development.md"))
	addTree(filepath.Join("plugins", "agents"))
	addTree(filepath.Join("plugins", "skills"))
	addTree(filepath.Join("plugins", "skills"))
	addTree(filepath.Join("studio", "src", "components", "connect"))
	addTree(filepath.Join("miniapp", "src", "pages", "connect"))
	return out
}

func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(output), "\x00"), "\x00")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" || line == "go.sum" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(line))); os.IsNotExist(err) {
			continue
		}
		out = append(out, filepath.FromSlash(line))
	}
	return out
}

func assertJSONField(t *testing.T, path, field, want string) {
	t.Helper()
	var object map[string]any
	readJSONFile(t, path, &object)
	if got, _ := object[field].(string); got != want {
		t.Fatalf("%s field %q = %q, want %q", path, field, got, want)
	}
}

func assertClaudePluginUserConfig(t *testing.T, path string) {
	t.Helper()
	var object struct {
		UserConfig map[string]struct {
			Type        string `json:"type"`
			Required    bool   `json:"required"`
			Sensitive   bool   `json:"sensitive"`
			Default     string `json:"default"`
			Description string `json:"description"`
		} `json:"userConfig"`
	}
	readJSONFile(t, path, &object)
	if _, ok := object.UserConfig["api_url"]; ok {
		t.Fatalf("%s must not expose userConfig.api_url", path)
	}
	if len(object.UserConfig) != 1 {
		t.Fatalf("%s has %d userConfig entries, want exactly api_key", path, len(object.UserConfig))
	}
	apiKey, ok := object.UserConfig["api_key"]
	if !ok {
		t.Fatalf("%s missing userConfig.api_key", path)
	}
	if apiKey.Type != "string" || !apiKey.Required || !apiKey.Sensitive {
		t.Fatalf("%s userConfig.api_key = %+v, want required sensitive string", path, apiKey)
	}
}

func assertOnlyMCPServerKey(t *testing.T, path, want string) {
	t.Helper()
	var object struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	readJSONFile(t, path, &object)
	if len(object.MCPServers) != 1 {
		t.Fatalf("%s has %d MCP server keys, want exactly one %q", path, len(object.MCPServers), want)
	}
	if _, ok := object.MCPServers[want]; !ok {
		t.Fatalf("%s MCP server keys = %v, want only %q", path, keys(object.MCPServers), want)
	}
}

func assertJSONMCPServerURL(t *testing.T, path, serverName, want string) {
	t.Helper()
	var object struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	readJSONFile(t, path, &object)
	server, ok := object.MCPServers[serverName]
	if !ok {
		t.Fatalf("%s missing mcpServers.%s", path, serverName)
	}
	if server.URL != want {
		t.Errorf("%s mcpServers.%s.url = %q, want %q", path, serverName, server.URL, want)
	}
}

func assertTOMLMCPServerURL(t *testing.T, path, serverName, want string) {
	t.Helper()
	got := tomlStringInTable(t, path, "mcp_servers."+serverName, "url")
	if got != want {
		t.Errorf("%s [mcp_servers.%s].url = %q, want %q", path, serverName, got, want)
	}
}

func tomlStringInTable(t *testing.T, path, wantTable, wantKey string) string {
	t.Helper()
	lines := strings.Split(readTextFile(t, path), "\n")
	currentTable := ""
	multilineDelimiter := ""
	for lineNumber, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if multilineDelimiter != "" {
			if strings.Count(line, multilineDelimiter)%2 == 1 {
				multilineDelimiter = ""
			}
			continue
		}
		if strings.Count(line, `"""`)%2 == 1 {
			multilineDelimiter = `"""`
			continue
		}
		if strings.Count(line, `'''`)%2 == 1 {
			multilineDelimiter = `'''`
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if strings.HasPrefix(line, "[[") {
				currentTable = ""
				continue
			}
			closeAt := strings.IndexByte(line, ']')
			if closeAt < 0 {
				t.Fatalf("%s:%d has unterminated TOML table header", path, lineNumber+1)
			}
			currentTable = strings.TrimSpace(line[1:closeAt])
			continue
		}
		if currentTable != wantTable {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != wantKey {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
			t.Fatalf("%s:%d %s.%s must be a quoted TOML string", path, lineNumber+1, wantTable, wantKey)
		}
		return value[1 : len(value)-1]
	}
	t.Fatalf("%s missing [%s].%s", path, wantTable, wantKey)
	return ""
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	body := readTextFile(t, path)
	if !strings.Contains(body, want) {
		t.Fatalf("%s missing %q", path, want)
	}
}

func assertFileNotContains(t *testing.T, path, banned string) {
	t.Helper()
	body := readTextFile(t, path)
	if strings.Contains(body, banned) {
		t.Fatalf("%s still contains banned term %q", path, banned)
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s should not exist", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func readJSONFile(t *testing.T, path string, dst any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
