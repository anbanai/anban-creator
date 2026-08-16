package agent

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const dshPluginVersion = "4.1.11"

func TestDSHPluginContract(t *testing.T) {
	root := repoRoot(t)
	pluginRoot := filepath.Join(root, "plugins")

	t.Run("distribution versions and bundle patch stay aligned", func(t *testing.T) {
		type packageManifest struct {
			Version string `json:"version"`
			DSH     struct {
				Bundle struct {
					Patch string `json:"patch"`
				} `json:"bundle"`
			} `json:"dsh"`
		}
		var npm packageManifest
		readJSONContractFile(t, filepath.Join(pluginRoot, "package.json"), &npm)
		if npm.Version != dshPluginVersion {
			t.Errorf("npm version = %q, want %s", npm.Version, dshPluginVersion)
		}
		if npm.DSH.Bundle.Patch != "./dsh/cordis.patch.yml" {
			t.Errorf("npm DSH bundle patch = %q, want ./dsh/cordis.patch.yml", npm.DSH.Bundle.Patch)
		}

		for name, path := range map[string]string{
			"Claude": filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"),
			"Codex":  filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"),
		} {
			var manifest struct {
				Version string `json:"version"`
			}
			readJSONContractFile(t, path, &manifest)
			if manifest.Version != dshPluginVersion {
				t.Errorf("%s version = %q, want %s", name, manifest.Version, dshPluginVersion)
			}
		}

		var marketplace struct {
			Plugins []struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"plugins"`
		}
		readJSONContractFile(t, filepath.Join(pluginRoot, ".claude-plugin", "marketplace.json"), &marketplace)
		if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != "anban" {
			t.Fatalf("Claude marketplace plugins = %+v, want only anban", marketplace.Plugins)
		}
		if marketplace.Plugins[0].Version != dshPluginVersion {
			t.Errorf("Claude marketplace version = %q, want %s", marketplace.Plugins[0].Version, dshPluginVersion)
		}

		var patch []struct {
			Insert []struct {
				ID   string `yaml:"id"`
				Name string `yaml:"name"`
			} `yaml:"insert"`
		}
		readYAMLContractFile(t, filepath.Join(pluginRoot, "dsh", "cordis.patch.yml"), &patch)
		if len(patch) != 1 || len(patch[0].Insert) != 2 {
			t.Fatalf("DSH bundle patch = %+v, want exactly two Host insert rows", patch)
		}
		gotRows := []string{
			patch[0].Insert[0].ID + "=" + patch[0].Insert[0].Name,
			patch[0].Insert[1].ID + "=" + patch[0].Insert[1].Name,
		}
		wantRows := []string{
			"anban-mcp=@anban/dsh-plugin/anban-mcp",
			"anban-preset-manager=@anban/dsh-plugin/preset-manager",
		}
		if strings.Join(gotRows, "\n") != strings.Join(wantRows, "\n") {
			t.Errorf("DSH Host rows = %v, want %v", gotRows, wantRows)
		}
	})

	t.Run("only Article and Seednote publish generated DSH Presets", func(t *testing.T) {
		type packManifest struct {
			ID    string `yaml:"id"`
			Agent struct {
				DSHSource string   `yaml:"dsh_source"`
				Skills    []string `yaml:"skills"`
			} `yaml:"agent"`
		}

		packFiles, err := filepath.Glob(filepath.Join(pluginRoot, "packs", "*", "agent-pack.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var dshPacks []string
		for _, packFile := range packFiles {
			var pack packManifest
			readYAMLContractFile(t, packFile, &pack)
			if pack.Agent.DSHSource == "" {
				continue
			}
			dshPacks = append(dshPacks, pack.ID)
			if pack.Agent.DSHSource != "agent.dsh.yml" {
				t.Errorf("Pack %s dsh_source = %q, want agent.dsh.yml", pack.ID, pack.Agent.DSHSource)
			}

			source := readRepoFile(t, filepath.Join(filepath.Dir(packFile), pack.Agent.DSHSource))
			if got := strings.Count(source, "@anban/dsh-plugin/skills-provider"); got != 1 {
				t.Errorf("Pack %s skills provider occurrences = %d, want 1", pack.ID, got)
			}

			presetRoot := filepath.Join(pluginRoot, "dsh", "presets", pack.ID)
			presetAgent := readRepoFile(t, filepath.Join(presetRoot, "agent.cordis.yml"))
			if strings.Count(presetAgent, "@anban/dsh-plugin/skills-provider") != 1 {
				t.Errorf("generated Preset %s must contain the skills provider exactly once", pack.ID)
			}
			assertGeneratedPresetSkills(t, presetRoot, pack.Agent.Skills)
			assertGeneratedPresetHasNoHostAdapters(t, presetRoot)
		}
		sort.Strings(dshPacks)
		if got := strings.Join(dshPacks, ","); got != "article,seednote" {
			t.Errorf("Packs with dsh_source = %q, want article,seednote", got)
		}
	})

	t.Run("published adapter keeps credentials in memory", func(t *testing.T) {
		source := readRepoFile(t, filepath.Join(pluginRoot, "dsh", "src", "anban-mcp.ts"))
		for _, want := range []string{
			"credentialRef('ANBAN_API_KEY')",
			"const MCP_URL = 'https://creator.anbanai.com/mcp'",
			"Authorization: `Bearer ${resolved.value}`",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("DSH MCP adapter missing %q", want)
			}
		}
		for _, forbidden := range []string{"process.env.ANBAN_MCP", "create_task"} {
			if strings.Contains(source, forbidden) {
				t.Errorf("DSH MCP adapter contains forbidden configurable or business operation %q", forbidden)
			}
		}
		if got := strings.Count(source, "credentialRef("); got != 1 {
			t.Errorf("DSH MCP adapter credential references = %d, want only ANBAN_API_KEY", got)
		}
		if got := strings.Count(source, "https://creator.anbanai.com/mcp"); got != 1 {
			t.Errorf("DSH MCP adapter fixed endpoint occurrences = %d, want 1", got)
		}

		literalSecret := regexp.MustCompile(`(?i)(api[_-]?key|token|secret)[[:space:]]*[:=][[:space:]]*["'\x60][^"'\x60]+["'\x60]`)
		publishedRoots := []string{
			filepath.Join(pluginRoot, "package.json"),
			filepath.Join(pluginRoot, "dsh", "cordis.patch.yml"),
			filepath.Join(pluginRoot, "dsh", "bin"),
			filepath.Join(pluginRoot, "dsh", "src"),
			filepath.Join(pluginRoot, "dsh", "presets"),
		}
		for _, root := range publishedRoots {
			walkPublishedDSHFiles(t, root, func(path, body string) {
				lower := strings.ToLower(body)
				if strings.Contains(lower, "create_task") {
					t.Errorf("published DSH asset %s contains forbidden create_task operation", path)
				}
				for _, marker := range []string{"anban_mcp_url", "anban_mcp_endpoint", "mcp_endpoint"} {
					if strings.Contains(lower, marker) {
						t.Errorf("published DSH asset %s contains configurable MCP endpoint marker %q", path, marker)
					}
				}
				for _, marker := range []string{"leaked-secret", "resolved-secret", "fake-secret", "test-secret"} {
					if strings.Contains(lower, marker) {
						t.Errorf("published DSH asset %s contains literal secret marker %q", path, marker)
					}
				}
				for _, match := range literalSecret.FindAllString(body, -1) {
					if !strings.Contains(match, "ANBAN_API_KEY") {
						t.Errorf("published DSH asset %s contains a literal credential assignment: %s", path, match)
					}
				}
				for _, line := range strings.Split(body, "\n") {
					if strings.Contains(strings.ToLower(line), "authorization") && strings.Contains(strings.ToLower(line), "bearer ") && !strings.Contains(line, "Bearer ${resolved.value}") {
						t.Errorf("published DSH asset %s serializes an Authorization header: %s", path, strings.TrimSpace(line))
					}
				}
			})
		}
	})
}

func readJSONContractFile(t *testing.T, path string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(readRepoFile(t, path)), target); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func readYAMLContractFile(t *testing.T, path string, target any) {
	t.Helper()
	if err := yaml.Unmarshal([]byte(readRepoFile(t, path)), target); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func assertGeneratedPresetSkills(t *testing.T, presetRoot string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(presetRoot, "skills"))
	if err != nil {
		t.Fatalf("read generated Preset skills: %v", err)
	}
	var got []string
	for _, entry := range entries {
		if entry.IsDir() {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(got)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("generated Preset %s Skill directories = %v, want %v", filepath.Base(presetRoot), got, want)
	}
}

func assertGeneratedPresetHasNoHostAdapters(t *testing.T, presetRoot string) {
	t.Helper()
	if err := filepath.WalkDir(presetRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		switch entry.Name() {
		case "skills-provider.mjs", ".mcp.json":
			t.Errorf("generated Preset contains forbidden host adapter %s", path)
		}
		if strings.HasSuffix(entry.Name(), ".yml") || strings.HasSuffix(entry.Name(), ".yaml") {
			body := readRepoFile(t, path)
			if strings.Contains(body, "@deepseek-ai/dsh-mcp-client") || strings.Contains(body, "id: mcp-client") {
				t.Errorf("generated Preset contains a duplicate mcp-client row in %s", path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func walkPublishedDSHFiles(t *testing.T, root string, visit func(path, body string)) {
	t.Helper()
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat published DSH asset %s: %v", root, err)
	}
	if !info.IsDir() {
		visit(root, readRepoFile(t, root))
		return
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		visit(path, readRepoFile(t, path))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
