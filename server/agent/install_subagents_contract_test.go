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
			name:          "Studio endpoint and correct bearer",
			initialURL:    "https://creator.anbanai.com/mcp",
			initialBearer: `bearer_token_env_var = "ANBAN_API_KEY"`,
		},
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
			if creator.URL != "https://api-creator.anbanai.com/mcp" {
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

func TestInstallSubagentsMigratesIPExplainerRegistration(t *testing.T) {
	for _, header := range []string{
		"[agents.talking-head-montage]",
		`[agents."talking-head-montage"]`,
		"[agents . talking-head-montage]",
		"[agents.talking-head-montage]\n[agents.talking-head-montage.options]",
	} {
		t.Run(header, func(t *testing.T) {
			testHome := t.TempDir()
			agentsDir := filepath.Join(testHome, ".codex", "agents")
			if err := os.MkdirAll(agentsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(testHome, ".codex", "config.toml")
			initial := `model = "keep-model"
instructions = """
[agents.talking-head-montage]
This is quoted documentation, not a registration.
"""

` + header + `
description = "old plugin registration"
config_file = "~/.codex/agents/talking-head-montage.toml"

[agents."personal[helper]"]
config_file = "~/bracket-agent.toml"

[agents.'helper#with.dots']
config_file = "~/literal-agent.toml"

[agents.personal-helper]
description = "preserve this unrelated agent"
config_file = "~/personal-helper.toml"
`
			if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
				t.Fatal(err)
			}
			retiredFile := filepath.Join(agentsDir, "talking-head-montage.toml")
			if err := os.WriteFile(retiredFile, []byte("name = \"talking-head-montage\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			pluginRoot := filepath.Join(repoRoot(t), "harness")
			var previous []byte
			for run := 0; run < 2; run++ {
				cmd := exec.Command("bash", filepath.Join(pluginRoot, "install", "install-subagents.sh"))
				cmd.Env = []string{"HOME=" + testHome, "ANBAN_PLUGIN_ROOT=" + pluginRoot, "PATH=" + os.Getenv("PATH")}
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("install run %d: %v\n%s", run, err, output)
				}
				body, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				var parsed map[string]any
				if _, err := toml.Decode(string(body), &parsed); err != nil {
					t.Fatal(err)
				}
				agents := parsed["agents"].(map[string]any)
				if _, exists := agents["talking-head-montage"]; exists {
					t.Fatal("retired workflow is still discoverable")
				}
				current, ok := agents["ip-explainer"].(map[string]any)
				if !ok || current["config_file"] != "~/.codex/agents/ip-explainer.toml" {
					t.Fatalf("new workflow registration is missing or wrong: %v", current)
				}
				instructions, _ := parsed["instructions"].(string)
				if parsed["model"] != "keep-model" || !strings.Contains(instructions, "[agents.talking-head-montage]") {
					t.Fatal("installer changed unrelated top-level settings or quoted documentation")
				}
				for name, want := range map[string]string{"personal[helper]": "~/bracket-agent.toml", "helper#with.dots": "~/literal-agent.toml"} {
					agent, ok := agents[name].(map[string]any)
					if !ok || agent["config_file"] != want {
						t.Fatalf("unrelated quoted agent %s changed: %v", name, agent)
					}
				}
				other := agents["personal-helper"].(map[string]any)
				if other["config_file"] != "~/personal-helper.toml" {
					t.Fatal("installer changed an unrelated agent")
				}
				if _, err := os.Stat(retiredFile); !os.IsNotExist(err) {
					t.Fatalf("retired native Agent file remains: %v", err)
				}
				if _, err := os.Stat(filepath.Join(agentsDir, "ip-explainer.toml")); err != nil {
					t.Fatalf("new native Agent file was not installed: %v", err)
				}
				if run > 0 && !bytes.Equal(body, previous) {
					t.Fatal("reinstall changed an already migrated registration")
				}
				previous = body
			}
		})
	}
}

func TestInstallSubagentsRejectedMigrationPreservesInstalledFiles(t *testing.T) {
	for _, initial := range []string{
		"[agents.talking-head-montage]\nconfig_file = 'old.toml'\n[mcp_servers.\"creator\"]\nurl = 'https://example.com/mcp'\n",
		"[agents]\ntalking-head-montage = {config_file = 'old.toml'}\n",
		"agents.talking-head-montage.config_file = 'old.toml'\n",
		`[agents.talking-head-montage]
description = """
Literal: \"""
"""
[agents.personal-helper]
config_file = "~/personal-helper.toml"
`,
		`[agents.talking-head-montage.'"""']
value = "retired child"
[agents.personal-helper]
config_file = "~/personal-helper.toml"
`,
		`[agents.talking-head-montage]
config_file = "old.toml"
[custom]
instructions = """
Literal: \"""
[mcp_servers.creator]
url = "https://unrelated.example/quoted-documentation"
bearer_token_env_var = "QUOTED_DOCUMENTATION"
"""
[mcp_servers.creator]
url = "https://creator.anbanai.com/mcp"
bearer_token_env_var = "ANBAN_API_KEY"
`,
		"invalid = [\n",
	} {
		t.Run(initial, func(t *testing.T) {
			testHome := t.TempDir()
			codexDir := filepath.Join(testHome, ".codex")
			agentsDir := filepath.Join(codexDir, "agents")
			if err := os.MkdirAll(agentsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(codexDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"talking-head-montage", "article"} {
				if err := os.WriteFile(filepath.Join(agentsDir, name+".toml"), []byte("original "+name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			pluginRoot := filepath.Join(repoRoot(t), "harness")
			cmd := exec.Command("bash", filepath.Join(pluginRoot, "install", "install-subagents.sh"))
			cmd.Env = []string{"HOME=" + testHome, "ANBAN_PLUGIN_ROOT=" + pluginRoot, "PATH=" + os.Getenv("PATH")}
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("accepted invalid/unsupported migration: %s", output)
			}
			body, err := os.ReadFile(configPath)
			if err != nil || string(body) != initial {
				t.Fatalf("config changed: %v", err)
			}
			entries, err := os.ReadDir(agentsDir)
			if err != nil || len(entries) != 2 {
				t.Fatalf("installed file set changed: %v %v", entries, err)
			}
			for _, name := range []string{"talking-head-montage", "article"} {
				body, err := os.ReadFile(filepath.Join(agentsDir, name+".toml"))
				if err != nil || string(body) != "original "+name {
					t.Fatalf("installed %s changed on rejected migration: %v", name, err)
				}
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
