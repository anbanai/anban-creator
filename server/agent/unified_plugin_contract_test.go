package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestUnifiedPluginLayout(t *testing.T) {
	root := repoRoot(t)
	pluginRoot := filepath.Join(root, "plugins")

	for _, legacy := range []string{"claudecode", "codex"} {
		if _, err := os.Stat(filepath.Join(root, legacy)); !os.IsNotExist(err) {
			t.Fatalf("legacy plugin root %q must not exist: %v", legacy, err)
		}
	}
	gitmodules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
	for _, legacy := range []string{"claudecode", "codex"} {
		if strings.Contains(gitmodules, `submodule "`+legacy+`"`) || strings.Contains(gitmodules, "path = "+legacy) {
			t.Fatalf(".gitmodules still declares legacy plugin %q", legacy)
		}
	}

	skillRoot := filepath.Join(pluginRoot, "skills")
	if info, err := os.Stat(skillRoot); err != nil || !info.IsDir() {
		t.Fatalf("canonical plugin Skill root missing at %s: %v", skillRoot, err)
	}
	nestedSkillRoots, err := filepath.Glob(filepath.Join(pluginRoot, "*", "skills"))
	if err != nil {
		t.Fatalf("glob nested plugin Skill roots: %v", err)
	}
	if len(nestedSkillRoots) != 0 {
		t.Fatalf("nested plugin Skill roots = %v, want none below flattened root %s", nestedSkillRoots, pluginRoot)
	}

	type manifest struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Skills    string `json:"skills"`
		Interface any    `json:"interface"`
	}
	readManifest := func(path string) manifest {
		t.Helper()
		var got manifest
		raw := readRepoFile(t, path)
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		return got
	}
	claudeManifest := readManifest(filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"))
	codexManifest := readManifest(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"))
	if claudeManifest.Name != "anban" || codexManifest.Name != "anban" {
		t.Fatalf("native manifest names = %q/%q, want anban", claudeManifest.Name, codexManifest.Name)
	}
	if claudeManifest.Version == "" || claudeManifest.Version != codexManifest.Version {
		t.Fatalf("native manifest versions = %q/%q, want one aligned version", claudeManifest.Version, codexManifest.Version)
	}
	if codexManifest.Skills != "./skills/" || codexManifest.Interface == nil {
		t.Fatalf("Codex manifest must reference shared Skills and declare interface metadata")
	}

	for _, path := range []string{
		filepath.Join(pluginRoot, ".mcp.json"),
		filepath.Join(pluginRoot, "hooks", "hooks.json"),
		filepath.Join(pluginRoot, "install", "agents-registration.toml"),
	} {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			t.Fatalf("required native adapter missing at %s: %v", path, err)
		}
	}
	registration := readRepoFile(t, filepath.Join(pluginRoot, "install", "agents-registration.toml"))
	for _, want := range []string{"[mcp_servers.creator]", `bearer_token_env_var = "ANBAN_API_KEY"`} {
		if !strings.Contains(registration, want) {
			t.Fatalf("Codex registration missing %q", want)
		}
	}

	markdownAgents := pluginAgentNames(t, filepath.Join(pluginRoot, "agents"), ".md")
	tomlAgents := pluginAgentNames(t, filepath.Join(pluginRoot, "agents"), ".toml")
	if len(markdownAgents) != 7 || strings.Join(markdownAgents, "\n") != strings.Join(tomlAgents, "\n") {
		t.Fatalf("native Agent sets differ: Claude=%v Codex=%v", markdownAgents, tomlAgents)
	}
}

func pluginAgentNames(t *testing.T, dir, extension string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read Agent directory %s: %v", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), extension))
	}
	sort.Strings(names)
	return names
}
