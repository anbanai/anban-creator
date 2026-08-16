package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const dshPluginVersion = "4.1.11"

type dshPackageManifest struct {
	Version string   `json:"version"`
	Files   []string `json:"files"`
	DSH     struct {
		Bundle struct {
			Patch string `json:"patch"`
		} `json:"bundle"`
	} `json:"dsh"`
}

type dshPublishedFile struct {
	Path string
	Body string
}

var dshLiteralSecret = regexp.MustCompile(`(?i)(api[_-]?key|token|secret)[[:space:]]*[:=][[:space:]]*["'\x60][^"'\x60]+["'\x60]`)
var dshConfigurableEndpoint = regexp.MustCompile(`(?i)mcp[_-]?(?:url|endpoint)[[:space:]]*[:=][^\n]*(?:process\.env|config|options|credentialref)`)

func TestDSHPluginContract(t *testing.T) {
	root := repoRoot(t)
	pluginRoot := filepath.Join(root, "plugins")

	t.Run("distribution versions and bundle patch stay aligned", func(t *testing.T) {
		var npm dshPackageManifest
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
			if got := yamlPluginRowCount(t, source, "@anban/dsh-plugin/skills-provider"); got != 1 {
				t.Errorf("Pack %s skills provider occurrences = %d, want 1", pack.ID, got)
			}

			presetRoot := filepath.Join(pluginRoot, "dsh", "presets", pack.ID)
			presetAgent := readRepoFile(t, filepath.Join(presetRoot, "agent.cordis.yml"))
			if yamlPluginRowCount(t, presetAgent, "@anban/dsh-plugin/skills-provider") != 1 {
				t.Errorf("generated Preset %s must contain the skills provider exactly once", pack.ID)
			}
			assertGeneratedPresetSkills(t, presetRoot, pack.Agent.Skills)
			assertGeneratedPresetHasNoHostAdapters(t, presetRoot)
		}
		sort.Strings(dshPacks)
		if got := strings.Join(dshPacks, ","); got != "article,seednote" {
			t.Errorf("Packs with dsh_source = %q, want article,seednote", got)
		}

		presetEntries, err := os.ReadDir(filepath.Join(pluginRoot, "dsh", "presets"))
		if err != nil {
			t.Fatal(err)
		}
		var presetDirectories []string
		for _, entry := range presetEntries {
			if entry.IsDir() {
				presetDirectories = append(presetDirectories, entry.Name())
			}
		}
		sort.Strings(presetDirectories)
		if got := strings.Join(presetDirectories, ","); got != "article,seednote" {
			t.Errorf("generated Preset directories = %q, want article,seednote", got)
		}
	})

	t.Run("installation commands use official profile forwarding", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		for _, want := range []string{
			"dsh plugin --profile web exec anban-dsh install-presets",
			"dsh plugin --profile web exec anban-dsh status",
			"dsh plugin --profile web exec anban-dsh remove-presets",
			"dsh plugin --profile web approve-builds",
			`ACTIVE_PROFILE="replace-with-desktop-profile-name"`,
			`dsh plugin --profile "$ACTIVE_PROFILE" add @anban/dsh-plugin`,
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh install-presets`,
			`dsh plugin --profile "$ACTIVE_PROFILE" remove @anban/dsh-plugin`,
			`dsh plugin --profile "$ACTIVE_PROFILE" approve-builds`,
			"--profile <active-profile>",
			"anban-dsh install-presets",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("DSH installation guide missing %q", want)
			}
		}
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "dsh plugin ") && !strings.Contains(line, " --profile ") {
				t.Errorf("DSH installation guide contains profile-implicit plugin command %q", line)
			}
		}
		if !strings.Contains(body, "only when") || !strings.Contains(body, "node_modules/.bin") {
			t.Error("bare anban-dsh shorthand must be explicitly conditional on the active profile bin directory being on PATH")
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

		var manifest dshPackageManifest
		readJSONContractFile(t, filepath.Join(pluginRoot, "package.json"), &manifest)
		for _, published := range publishedDSHPayloadFiles(t, pluginRoot, manifest.Files) {
			for _, finding := range dshPayloadFindings(published.Body) {
				t.Errorf("published DSH asset %s %s", published.Path, finding)
			}
		}
	})
}

func TestDSHReleaseWorkflowValidatesTagAndTarballVersion(t *testing.T) {
	body := readRepoFile(t, filepath.Join(repoRoot(t), ".github", "workflows", "release.yml"))
	for _, want := range []string{
		`TAG_VERSION: ${{ steps.version.outputs.VERSION }}`,
		`package_version=$(node -p "require('./plugins/package.json').version")`,
		`test "$TAG_VERSION" = "$package_version"`,
		`expected_tarball="bin/anban-dsh-plugin-${TAG_VERSION}.tgz"`,
		`test -f "$expected_tarball"`,
		`tarball_count=$(find bin -maxdepth 1 -type f -name 'anban-dsh-plugin-*.tgz' | wc -l | tr -d ' ')`,
		`test "$tarball_count" = "1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("release workflow missing DSH version safeguard %q", want)
		}
	}
	for _, forbidden := range []string{"npm publish", "NPM_TOKEN", "NODE_AUTH_TOKEN"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("release workflow contains forbidden npm publication surface %q", forbidden)
		}
	}
}

func TestDSHYAMLPluginRowsAreStructural(t *testing.T) {
	const provider = "@anban/dsh-plugin/skills-provider"
	body := `
# name: '@anban/dsh-plugin/skills-provider'
- {id: first, name: "@anban/dsh-plugin/skills-provider"}
- id: second
  name: '@anban/dsh-plugin/skills-provider'
- description: "name: @anban/dsh-plugin/skills-provider"
`
	if got := yamlPluginRowCount(t, body, provider); got != 2 {
		t.Fatalf("structural skills-provider rows = %d, want 2", got)
	}

	const mcpClient = "@deepseek-ai/dsh-mcp-client"
	flowDuplicates := `[{name: "@deepseek-ai/dsh-mcp-client"}, {name: '@deepseek-ai/dsh-mcp-client'}]`
	if got := yamlPluginRowCount(t, flowDuplicates, mcpClient); got != 2 {
		t.Fatalf("structural flow-style mcp-client rows = %d, want 2", got)
	}
}

func TestDSHPublishedPayloadFilesFollowPackageAllowlist(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"runtime", "docs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		"package.json":   `{"files":["runtime/**","docs/guide.md"]}`,
		"runtime/new.js": "export const operation = \"create_task\"\nexport const MCP_URL = process.env.CREATOR_MCP_URL\n",
		"docs/guide.md":  "The legacy create_task operation is not shipped.\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := publishedDSHPayloadFiles(t, root, []string{"runtime/**", "docs/guide.md"})
	byRelativePath := make(map[string]dshPublishedFile, len(files))
	for _, file := range files {
		relative, err := filepath.Rel(root, file.Path)
		if err != nil {
			t.Fatal(err)
		}
		byRelativePath[filepath.ToSlash(relative)] = file
	}
	unsafe, ok := byRelativePath["runtime/new.js"]
	if !ok {
		t.Fatal("new package-allowlisted runtime file escaped published payload scanning")
	}
	if _, ok := byRelativePath["docs/guide.md"]; ok {
		t.Fatal("explanatory package documentation must not be treated as executable payload")
	}
	if got := strings.Join(dshPayloadFindings(unsafe.Body), "\n"); !strings.Contains(got, "create_task") {
		t.Fatalf("unsafe publishable file findings = %q, want create_task", got)
	}
	if got := strings.Join(dshPayloadFindings(unsafe.Body), "\n"); !strings.Contains(got, "configurable MCP endpoint") {
		t.Fatalf("unsafe publishable file findings = %q, want configurable MCP endpoint", got)
	}
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
			if yamlPluginRowCount(t, body, "@deepseek-ai/dsh-mcp-client") != 0 {
				t.Errorf("generated Preset contains a duplicate mcp-client row in %s", path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func yamlPluginRowCount(t *testing.T, body, pluginName string) int {
	t.Helper()
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("parse DSH composition YAML: %v", err)
	}
	var count int
	var visit func(*yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for index := 0; index+1 < len(node.Content); index += 2 {
				key := node.Content[index]
				value := node.Content[index+1]
				if key.Kind == yaml.ScalarNode && key.Value == "name" && value.Kind == yaml.ScalarNode && value.Value == pluginName {
					count++
				}
				visit(value)
			}
			return
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(&document)
	return count
}

func publishedDSHPayloadFiles(t *testing.T, pluginRoot string, patterns []string) []dshPublishedFile {
	t.Helper()
	selected := map[string]struct{}{
		filepath.Join(pluginRoot, "package.json"): {},
	}
	libPatternMatched := false
	for _, pattern := range patterns {
		pattern = pathpkg.Clean(filepath.ToSlash(pattern))
		if explanatoryPackagePattern(pattern) {
			continue
		}
		matched := false
		if err := filepath.WalkDir(pluginRoot, func(filePath string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if filePath != pluginRoot && (entry.Name() == ".git" || entry.Name() == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			relative, err := filepath.Rel(pluginRoot, filePath)
			if err != nil {
				return err
			}
			matches, err := matchPackageFilePattern(pattern, filepath.ToSlash(relative))
			if err != nil {
				return err
			}
			if matches {
				selected[filePath] = struct{}{}
				matched = true
			}
			return nil
		}); err != nil {
			t.Fatalf("expand package files pattern %q: %v", pattern, err)
		}
		if strings.HasPrefix(pattern, "dsh/lib/") && matched {
			libPatternMatched = true
		}
	}

	if !libPatternMatched {
		sourceRoot := filepath.Join(pluginRoot, "dsh", "src")
		entries, err := os.ReadDir(sourceRoot)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read DSH source mappings: %v", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".ts") {
				selected[filepath.Join(sourceRoot, entry.Name())] = struct{}{}
			}
		}
	}

	paths := make([]string, 0, len(selected))
	for filePath := range selected {
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			paths = append(paths, filePath)
		}
	}
	sort.Strings(paths)
	published := make([]dshPublishedFile, 0, len(paths))
	for _, filePath := range paths {
		published = append(published, dshPublishedFile{Path: filePath, Body: readRepoFile(t, filePath)})
	}
	return published
}

func explanatoryPackagePattern(pattern string) bool {
	return strings.HasPrefix(pattern, "docs/") || pattern == "README.md" || pattern == "CHANGELOG.md" || pattern == "LICENSE"
}

func matchPackageFilePattern(pattern, fileName string) (bool, error) {
	patternSegments := strings.Split(pathpkg.Clean(pattern), "/")
	fileSegments := strings.Split(pathpkg.Clean(fileName), "/")
	var match func(int, int) (bool, error)
	match = func(patternIndex, fileIndex int) (bool, error) {
		if patternIndex == len(patternSegments) {
			return fileIndex == len(fileSegments), nil
		}
		if patternSegments[patternIndex] == "**" {
			for next := fileIndex; next <= len(fileSegments); next++ {
				matched, err := match(patternIndex+1, next)
				if err != nil || matched {
					return matched, err
				}
			}
			return false, nil
		}
		if fileIndex == len(fileSegments) {
			return false, nil
		}
		matched, err := pathpkg.Match(patternSegments[patternIndex], fileSegments[fileIndex])
		if err != nil || !matched {
			return false, err
		}
		return match(patternIndex+1, fileIndex+1)
	}
	return match(0, 0)
}

func dshPayloadFindings(body string) []string {
	var findings []string
	lower := strings.ToLower(body)
	if strings.Contains(lower, "create_task") {
		findings = append(findings, "contains forbidden create_task operation")
	}
	if dshConfigurableEndpoint.MatchString(body) {
		findings = append(findings, "contains configurable MCP endpoint")
	}
	for _, marker := range []string{"anban_mcp_url", "anban_mcp_endpoint", "mcp_endpoint"} {
		if strings.Contains(lower, marker) {
			findings = append(findings, fmt.Sprintf("contains configurable MCP endpoint marker %q", marker))
		}
	}
	for _, marker := range []string{"leaked-secret", "resolved-secret", "fake-secret", "test-secret"} {
		if strings.Contains(lower, marker) {
			findings = append(findings, fmt.Sprintf("contains literal secret marker %q", marker))
		}
	}
	for _, match := range dshLiteralSecret.FindAllString(body, -1) {
		if !strings.Contains(match, "ANBAN_API_KEY") {
			findings = append(findings, "contains a literal credential assignment: "+match)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), "authorization") && strings.Contains(strings.ToLower(line), "bearer ") && !strings.Contains(line, "Bearer ${resolved.value}") {
			findings = append(findings, "serializes an Authorization header: "+strings.TrimSpace(line))
		}
	}
	return findings
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
