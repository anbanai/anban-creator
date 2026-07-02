package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBuildsAgentCLIAssets(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	body := string(raw)

	for _, want := range []string{
		"-o bin/anban-linux-amd64 ./agent",
		"-o bin/anban-linux-arm64 ./agent",
		"-o bin/anban-darwin-amd64 ./agent",
		"-o bin/anban-darwin-arm64 ./agent",
		"-o bin/anban-windows-amd64.exe ./agent",
		"bin/anban-linux-amd64",
		"bin/anban-linux-arm64",
		"bin/anban-darwin-amd64",
		"bin/anban-darwin-arm64",
		"bin/anban-windows-amd64.exe",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}
}

func TestPluginBootstrapInstallsAnbanBinary(t *testing.T) {
	root := repositoryRoot(t)
	scripts := []string{
		filepath.Join(root, "scripts", "bootstrap.sh"),
		filepath.Join(root, "claudecode", "scripts", "bootstrap.sh"),
		filepath.Join(root, "codex", "scripts", "bootstrap.sh"),
		filepath.Join(root, "openclaw", "scripts", "bootstrap.sh"),
	}

	var first string
	for _, path := range scripts {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("bootstrap script missing at %s: %v", path, err)
		}
		body := string(raw)
		for _, want := range []string{
			"anban-${OS}-${ARCH}",
			"anban-creator-server-${OS}-${ARCH}",
			"install_asset \"$AGENT_ASSET\" \"$AGENT_DEST\"",
			"install_asset \"$SERVER_ASSET\" \"$SERVER_DEST\"",
			"ANBAN_PLUGIN_ROOT",
			"CLAUDE_PLUGIN_ROOT",
			"PLUGIN_ROOT",
			"$BIN_DIR/anban",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
		if first == "" {
			first = body
		} else if body != first {
			t.Fatalf("bootstrap scripts must stay identical; %s differs", path)
		}
	}
}

func TestPluginBootstrapPreservesBundledAnbanBinary(t *testing.T) {
	root := repositoryRoot(t)
	pluginRoot := t.TempDir()
	binDir := filepath.Join(pluginRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(binDir, "anban")
	if err := os.WriteFile(agentPath, []byte("bundled-anban\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/usr/bin/env bash
case "$1" in
  -s) echo Darwin ;;
  -m) echo arm64 ;;
  *) /usr/bin/uname "$@" ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "curl"), `#!/usr/bin/env bash
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
if printf '%s\n' "$*" | grep -q 'api.github.com'; then
  printf '{"tag_name":"v9.9.9"}\n'
  exit 0
fi
if [ -z "$out" ]; then
  echo "missing -o" >&2
  exit 2
fi
printf 'downloaded asset\n' > "$out"
`)

	cmd := exec.Command("bash", filepath.Join(root, "scripts", "bootstrap.sh"))
	cmd.Env = append(os.Environ(),
		"ANBAN_PLUGIN_ROOT="+pluginRoot,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap failed: %v\n%s", err, output)
	}

	raw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "bundled-anban\n" {
		t.Fatalf("bootstrap overwrote bundled bin/anban: got %q", got)
	}
	if _, err := os.Stat(filepath.Join(binDir, "anban-creator-server")); err != nil {
		t.Fatalf("bootstrap should still install missing server binary: %v", err)
	}
}

func TestPluginBinaryPackagingBuildsFixedAnbanFiles(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "build-plugin-binaries.sh"))
	if err != nil {
		t.Fatalf("read plugin binary packaging script: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"go build",
		"./agent",
		"for plugin in claudecode codex openclaw",
		"/bin/anban",
		"GOOS",
		"GOARCH",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plugin binary packaging script missing %q", want)
		}
	}

	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makeBody := string(makefile)
	for _, want := range []string{
		"plugin-binaries:",
		"PLUGIN_GOOS",
		"PLUGIN_GOARCH",
		"scripts/build-plugin-binaries.sh",
	} {
		if !strings.Contains(makeBody, want) {
			t.Fatalf("Makefile missing %q", want)
		}
	}
}

func TestPluginsWireAnbanBootstrap(t *testing.T) {
	root := repositoryRoot(t)
	for _, tc := range []struct {
		name         string
		path         string
		wantVersion  string
		wantSnippets []string
	}{
		{
			name:        "claudecode",
			path:        filepath.Join(root, "claudecode", "hooks", "hooks.json"),
			wantVersion: "2.10.10",
			wantSnippets: []string{
				"SessionStart",
				"${CLAUDE_PLUGIN_ROOT}/scripts/bootstrap.sh",
			},
		},
		{
			name:        "codex",
			path:        filepath.Join(root, "codex", "install", "install-subagents.sh"),
			wantVersion: "2.10.8",
			wantSnippets: []string{
				"ANBAN_PLUGIN_ROOT=\"$PLUGIN_ROOT\"",
				"scripts/bootstrap.sh",
			},
		},
		{
			name:        "openclaw",
			path:        filepath.Join(root, "openclaw", "src", "index.ts"),
			wantVersion: "2.7.8",
			wantSnippets: []string{
				"bootstrapAnbanBinary(api)",
				"scripts/bootstrap.sh",
				"ANBAN_PLUGIN_ROOT",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}
			body := string(raw)
			for _, want := range tc.wantSnippets {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing %q", tc.path, want)
				}
			}
			assertPluginVersion(t, root, tc.name, tc.wantVersion)
		})
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func assertPluginVersion(t *testing.T, root, plugin, want string) {
	t.Helper()
	paths := map[string]string{
		"claudecode": filepath.Join(root, "claudecode", ".claude-plugin", "plugin.json"),
		"codex":      filepath.Join(root, "codex", ".codex-plugin", "plugin.json"),
		"openclaw":   filepath.Join(root, "openclaw", "openclaw.plugin.json"),
	}
	raw, err := os.ReadFile(paths[plugin])
	if err != nil {
		t.Fatalf("read %s manifest: %v", plugin, err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse %s manifest: %v", plugin, err)
	}
	if manifest.Version != want {
		t.Fatalf("%s version = %q, want %q", plugin, manifest.Version, want)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
