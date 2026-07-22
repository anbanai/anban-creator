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
		filepath.Join(root, "studio", "src", "components", "connect", "ClaudeGuide.tsx"),
		filepath.Join(root, "studio", "src", "components", "connect", "CodexGuide.tsx"),
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
	assertFileContains(t, filepath.Join(root, "plugins", ".mcp.json"), "${user_config.api_url}/mcp")
	assertFileContains(t, filepath.Join(root, "plugins", ".mcp.json"), "Bearer ${user_config.api_key}")
	assertTrackedFilesDoNotContainLegacyNames(t, root)
	assertBusinessLayerFilesDoNotContainHostMCPPrefixes(t, root)
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
		if !strings.Contains(got, "github.com/royalmorty/anbanwriter") {
			t.Fatalf("%s plugin URL = %q, want unified monorepo", path, got)
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
	apiKey, ok := object.UserConfig["api_key"]
	if !ok {
		t.Fatalf("%s missing userConfig.api_key", path)
	}
	if apiKey.Type != "string" || !apiKey.Required || !apiKey.Sensitive {
		t.Fatalf("%s userConfig.api_key = %+v, want required sensitive string", path, apiKey)
	}
	apiURL, ok := object.UserConfig["api_url"]
	if !ok {
		t.Fatalf("%s missing userConfig.api_url", path)
	}
	if apiURL.Type != "string" || apiURL.Default != "https://api.creator.anbanai.com" {
		t.Fatalf("%s userConfig.api_url = %+v, want string default official API URL", path, apiURL)
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
