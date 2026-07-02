package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnbanCreatorNamingContract(t *testing.T) {
	root := repoRoot(t)

	assertJSONField(t, filepath.Join(root, "claudecode", ".claude-plugin", "plugin.json"), "name", "anban")
	assertJSONField(t, filepath.Join(root, "codex", ".codex-plugin", "plugin.json"), "name", "anban")
	assertJSONField(t, filepath.Join(root, "openclaw", "openclaw.plugin.json"), "id", "anban")
	assertJSONField(t, filepath.Join(root, "openclaw", "openclaw.plugin.json"), "name", "Anban 智能创作助手")
	assertClaudeMarketplacePlugin(t, filepath.Join(root, "claudecode", ".claude-plugin", "marketplace.json"))

	for _, path := range []string{
		filepath.Join(root, "claudecode", ".mcp.json"),
		filepath.Join(root, "codex", ".mcp.json"),
		filepath.Join(root, "openclaw", ".mcp.json"),
	} {
		assertOnlyMCPServerKey(t, path, "creator")
	}

	assertFileContains(t, filepath.Join(root, "openclaw", "src", "index.ts"), `id: "anban"`)

	for _, path := range []string{
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "claudecode", "README.md"),
		filepath.Join(root, "agent", "Dockerfile"),
		filepath.Join(root, "server", "Dockerfile"),
		filepath.Join(root, "studio", "src", "components", "connect", "ClaudeGuide.tsx"),
		filepath.Join(root, "studio", "src", "components", "connect", "CodexGuide.tsx"),
		filepath.Join(root, "miniapp", "src", "pages", "connect", "claude-code.vue"),
		filepath.Join(root, "miniapp", "src", "pages", "connect", "codex.vue"),
		filepath.Join(root, "codex", "README.md"),
		filepath.Join(root, "codex", "install", "install-subagents.sh"),
	} {
		assertFileNotContains(t, path, "anbancreator"+"@anbanai")
		assertFileNotContains(t, path, "creator"+"@anbanai")
		assertFileNotContains(t, path, "plugin install "+"anban-creator")
		assertFileNotContains(t, path, "anban-creator"+"@anbanai")
		assertFileNotContains(t, path, "ab"+"c:")
		assertFileNotContains(t, path, "anban"+"writer")
		assertFileNotContains(t, path, "Anban"+"Writer")
		assertFileNotContains(t, path, "案"+"板")
	}
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "plugin@marketplace")
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "anban@anbanai")
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "mcp__plugin_anban_creator__")
	assertFileContains(t, filepath.Join(root, "CLAUDE.md"), "anban:<agent>")
	assertFileContains(t, filepath.Join(root, "claudecode", "README.md"), "claude plugin install --scope user anban@anbanai")
	assertFileContains(t, filepath.Join(root, "claudecode", "README.md"), "--agent anban:article")
	assertFileContains(t, filepath.Join(root, "claudecode", "README.md"), "mcp__plugin_anban_creator__")
	assertTrackedFilesDoNotContainLegacyNames(t, root)
}

func assertClaudeMarketplacePlugin(t *testing.T, path string) {
	t.Helper()
	var object struct {
		Name    string `json:"name"`
		Owner   named  `json:"owner"`
		Plugins []struct {
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
		if !strings.Contains(got, "github.com/anbanai/anban-creator-claudecode") {
			t.Fatalf("%s plugin URL = %q, want anban-creator-claudecode", path, got)
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
			"anban" + "writer",
			"Anban" + "Writer",
			"案" + "板",
			"royal" + "rick",
			"royal" + "morty/anban" + "writer",
		} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(banned)) {
				t.Fatalf("%s still contains banned term %q", path, banned)
			}
		}
	}
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
