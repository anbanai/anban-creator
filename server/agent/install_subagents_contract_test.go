package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestInstallSubagentsRefreshesFixedCreatorEndpoint(t *testing.T) {
	tests := []struct {
		name          string
		initialURL    string
		initialBearer string
	}{
		{
			name:          "legacy endpoint and correct bearer",
			initialURL:    "https://api.creator.anbanai.com/mcp",
			initialBearer: `bearer_token_env_var = "ANBAN_API_KEY"`,
		},
		{
			name:          "custom endpoint and wrong bearer",
			initialURL:    "https://custom.example/mcp",
			initialBearer: `bearer_token_env_var = "CUSTOM_API_KEY"`,
		},
		{
			name:       "custom endpoint and missing bearer",
			initialURL: "https://custom.example/mcp",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			codexDir := filepath.Join(home, ".codex")
			if err := os.MkdirAll(codexDir, 0o755); err != nil {
				t.Fatalf("create temporary Codex directory: %v", err)
			}
			configPath := filepath.Join(codexDir, "config.toml")
			decoy := `[mcp_servers.creator] # decoy inside multiline string
url = "https://decoy.example/mcp"`
			config := fmt.Sprintf(`model = "gpt-test"
instructions = """
%s
"""

[features]
multi_agent = false
custom_feature = true

[custom]
value = "preserve-me"
matrix = [
  [1, 2],
  [3, 4],
]

# Example delimiter: """
[mcp_servers.creator] # user comment
url = %q
%s
timeout_sec = 123

[[custom.sources]] # preserve
name = "source-one"
url = "https://unrelated.example/data"

[mcp_servers.other]
url = "https://other.example/mcp"
`, decoy, tt.initialURL, tt.initialBearer)
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatalf("write temporary Codex config: %v", err)
			}

			pluginRoot := filepath.Join(repoRoot(t), "harness")
			scriptPath := filepath.Join(pluginRoot, "install", "install-subagents.sh")
			runInstaller := func() []byte {
				t.Helper()
				cmd := exec.Command("bash", scriptPath)
				cmd.Env = []string{
					"HOME=" + home,
					"ANBAN_PLUGIN_ROOT=" + pluginRoot,
					"PATH=" + os.Getenv("PATH"),
				}
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("run installer: %v\n%s", err, output)
				}
				body, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatalf("read merged Codex config: %v", err)
				}
				return body
			}

			first := runInstaller()
			var parsed struct {
				Model        string `toml:"model"`
				Instructions string `toml:"instructions"`
				Features     struct {
					MultiAgent    bool `toml:"multi_agent"`
					CustomFeature bool `toml:"custom_feature"`
				} `toml:"features"`
				MCPServers map[string]struct {
					URL               string `toml:"url"`
					BearerTokenEnvVar string `toml:"bearer_token_env_var"`
					TimeoutSec        int    `toml:"timeout_sec"`
				} `toml:"mcp_servers"`
				Custom struct {
					Value   string  `toml:"value"`
					Matrix  [][]int `toml:"matrix"`
					Sources []struct {
						Name string `toml:"name"`
						URL  string `toml:"url"`
					} `toml:"sources"`
				} `toml:"custom"`
			}
			if _, err := toml.Decode(string(first), &parsed); err != nil {
				t.Fatalf("parse installed Codex config as TOML: %v\n%s", err, first)
			}
			if len(parsed.MCPServers) != 2 {
				t.Fatalf("parsed MCP server count = %d, want exactly creator and unrelated server", len(parsed.MCPServers))
			}
			creator, ok := parsed.MCPServers["creator"]
			if !ok {
				t.Fatal("parsed config missing exactly one creator table")
			}
			if creator.URL != "https://creator.anbanai.com/mcp" {
				t.Fatalf("creator endpoint after upgrade = %q, want fixed official endpoint", creator.URL)
			}
			if creator.BearerTokenEnvVar != "ANBAN_API_KEY" || creator.TimeoutSec != 123 {
				t.Fatalf("creator table lost unrelated keys: %+v", creator)
			}
			if parsed.MCPServers["other"].URL != "https://other.example/mcp" {
				t.Fatalf("unrelated MCP server changed: %+v", parsed.MCPServers["other"])
			}
			if parsed.Model != "gpt-test" || !parsed.Features.CustomFeature || parsed.Custom.Value != "preserve-me" {
				t.Fatalf("unrelated config changed: model=%q features=%+v custom=%+v", parsed.Model, parsed.Features, parsed.Custom)
			}
			if len(parsed.Custom.Matrix) != 2 || len(parsed.Custom.Matrix[0]) != 2 || len(parsed.Custom.Matrix[1]) != 2 ||
				parsed.Custom.Matrix[0][0] != 1 || parsed.Custom.Matrix[0][1] != 2 ||
				parsed.Custom.Matrix[1][0] != 3 || parsed.Custom.Matrix[1][1] != 4 {
				t.Fatalf("multiline nested array changed: %+v", parsed.Custom.Matrix)
			}
			if len(parsed.Custom.Sources) != 1 || parsed.Custom.Sources[0].Name != "source-one" || parsed.Custom.Sources[0].URL != "https://unrelated.example/data" {
				t.Fatalf("array-table source changed: %+v", parsed.Custom.Sources)
			}
			if parsed.Instructions != decoy+"\n" {
				t.Fatalf("multiline string changed = %q, want %q", parsed.Instructions, decoy+"\n")
			}
			if !strings.Contains(string(first), "[mcp_servers.creator] # user comment") {
				t.Fatal("installer did not preserve the original commented creator header")
			}
			for _, want := range []string{
				`bearer_token_env_var = "ANBAN_API_KEY"`,
				"custom_feature = true",
				"timeout_sec = 123",
				`url = "https://other.example/mcp"`,
				`value = "preserve-me"`,
			} {
				if !strings.Contains(string(first), want) {
					t.Errorf("merged Codex config lost unrelated content %q", want)
				}
			}

			second := runInstaller()
			if !bytes.Equal(first, second) {
				t.Fatal("installer changed config on an idempotent second run")
			}
		})
	}
}

func TestInstallSubagentsCreatesMissingConfigFromRegistration(t *testing.T) {
	home := t.TempDir()
	tmpDir := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		t.Fatalf("create dedicated temporary directory: %v", err)
	}

	pluginRoot := filepath.Join(repoRoot(t), "harness")
	scriptPath := filepath.Join(pluginRoot, "install", "install-subagents.sh")
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = []string{
		"HOME=" + home,
		"ANBAN_PLUGIN_ROOT=" + pluginRoot,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + tmpDir,
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run first-time installer: %v\n%s", err, output)
	}

	configPath := filepath.Join(home, ".codex", "config.toml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read created Codex config: %v", err)
	}
	registration, err := os.ReadFile(filepath.Join(pluginRoot, "install", "agents-registration.toml"))
	if err != nil {
		t.Fatalf("read bundled registration: %v", err)
	}
	if !bytes.Equal(config, registration) {
		t.Fatal("first-time installer did not copy bundled registration exactly")
	}
	assertInstallerTempDirEmpty(t, tmpDir, "")
}

func TestInstallSubagentsRejectsUnsupportedCreatorRepresentations(t *testing.T) {
	const secret = "must-not-appear-in-installer-output"
	tests := []struct {
		name   string
		config string
	}{
		{
			name: "quoted creator table segment",
			config: `[mcp_servers."creator"]
url = "https://custom.example/mcp"
api_key = "` + secret + `"
`,
		},
		{
			name: "quoted url key",
			config: `[mcp_servers.creator]
"url" = "https://custom.example/mcp"
api_key = "` + secret + `"
`,
		},
		{
			name: "quoted bearer key",
			config: `[mcp_servers.creator]
url = "https://custom.example/mcp"
"bearer_token_env_var" = "CUSTOM_API_KEY"
api_key = "` + secret + `"
`,
		},
		{
			name: "top-level dotted creator connection fields",
			config: `mcp_servers.creator.url = "https://custom.example/mcp"
mcp_servers.creator.bearer_token_env_var = "CUSTOM_API_KEY"
secret = "` + secret + `"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			tmpDir := filepath.Join(home, "tmp")
			if err := os.MkdirAll(tmpDir, 0o700); err != nil {
				t.Fatalf("create dedicated temporary directory: %v", err)
			}
			codexDir := filepath.Join(home, ".codex")
			if err := os.MkdirAll(codexDir, 0o755); err != nil {
				t.Fatalf("create temporary Codex directory: %v", err)
			}
			configPath := filepath.Join(codexDir, "config.toml")
			initial := []byte(tt.config)
			if err := os.WriteFile(configPath, initial, 0o640); err != nil {
				t.Fatalf("write temporary Codex config: %v", err)
			}
			if err := os.Chmod(configPath, 0o640); err != nil {
				t.Fatalf("set temporary Codex config mode: %v", err)
			}

			pluginRoot := filepath.Join(repoRoot(t), "harness")
			scriptPath := filepath.Join(pluginRoot, "install", "install-subagents.sh")
			cmd := exec.Command("bash", scriptPath)
			cmd.Env = []string{
				"HOME=" + home,
				"ANBAN_PLUGIN_ROOT=" + pluginRoot,
				"PATH=" + os.Getenv("PATH"),
				"TMPDIR=" + tmpDir,
			}
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("installer accepted unsupported creator representation\n%s", output)
			}
			if !strings.Contains(string(output), "unsupported creator MCP configuration representation") ||
				!strings.Contains(string(output), "[mcp_servers.creator]") ||
				!strings.Contains(string(output), "bare url key") ||
				!strings.Contains(string(output), "bare bearer_token_env_var key") {
				t.Fatalf("installer error lacks canonical representation guidance:\n%s", output)
			}
			if strings.Contains(string(output), secret) {
				t.Fatalf("installer error leaked config credential: %s", output)
			}

			after, readErr := os.ReadFile(configPath)
			if readErr != nil {
				t.Fatalf("read Codex config after rejected install: %v", readErr)
			}
			if !bytes.Equal(after, initial) {
				t.Fatalf("installer changed rejected config\ngot:\n%s\nwant:\n%s", after, initial)
			}
			info, statErr := os.Stat(configPath)
			if statErr != nil {
				t.Fatalf("stat Codex config after rejected install: %v", statErr)
			}
			if got := info.Mode().Perm(); got != 0o640 {
				t.Fatalf("rejected config mode = %o, want 640", got)
			}
			assertInstallerTempDirEmpty(t, tmpDir, secret)
		})
	}
}

func TestInstallSubagentsRejectsIncompleteSemanticRegistration(t *testing.T) {
	const secret = "semantic-scanner-secret-must-not-leak"
	home := t.TempDir()
	tmpDir := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		t.Fatalf("create dedicated temporary directory: %v", err)
	}
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("create temporary Codex directory: %v", err)
	}
	configPath := filepath.Join(codexDir, "config.toml")
	initial := []byte(`instructions = """
Literal: \"""
[features]
multi_agent = false
"""
credential = "` + secret + `"
`)
	if err := os.WriteFile(configPath, initial, 0o640); err != nil {
		t.Fatalf("write temporary Codex config: %v", err)
	}
	if err := os.Chmod(configPath, 0o640); err != nil {
		t.Fatalf("set temporary Codex config mode: %v", err)
	}

	pluginRoot := filepath.Join(repoRoot(t), "harness")
	scriptPath := filepath.Join(pluginRoot, "install", "install-subagents.sh")
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = []string{
		"HOME=" + home,
		"ANBAN_PLUGIN_ROOT=" + pluginRoot,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + tmpDir,
	}
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("installer accepted semantically incomplete registration\n%s", output)
	}
	if !strings.Contains(string(output), "missing required registration key") ||
		!strings.Contains(string(output), "features.multi_agent") {
		t.Fatalf("installer error lacks missing registration key path:\n%s", output)
	}
	if strings.Contains(string(output), secret) {
		t.Fatalf("installer error leaked config credential: %s", output)
	}

	after, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read Codex config after rejected install: %v", readErr)
	}
	if !bytes.Equal(after, initial) {
		t.Fatalf("installer changed rejected config\ngot:\n%s\nwant:\n%s", after, initial)
	}
	info, statErr := os.Stat(configPath)
	if statErr != nil {
		t.Fatalf("stat Codex config after rejected install: %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("rejected config mode = %o, want 640", got)
	}
	assertInstallerTempDirEmpty(t, tmpDir, secret)
}

func assertInstallerTempDirEmpty(t *testing.T, root, secret string) {
	t.Helper()
	var residual []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		residual = append(residual, relative)
		if secret != "" && info.Mode().IsRegular() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(body, []byte(secret)) {
				t.Fatalf("temporary file %s retained config credential", relative)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect dedicated temporary directory: %v", err)
	}
	if len(residual) != 0 {
		t.Fatalf("installer left temporary files after failure: %v", residual)
	}
}
