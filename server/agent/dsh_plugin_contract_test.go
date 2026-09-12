package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	dshPluginVersion     = "4.1.23"
	dshPluginReleaseDate = "2026-09-12"
)

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
	pluginRoot := filepath.Join(root, "harness")

	t.Run("distribution versions and bundle patch stay aligned", func(t *testing.T) {
		var npm dshPackageManifest
		readJSONContractFile(t, filepath.Join(pluginRoot, "package.json"), &npm)
		if npm.Version != dshPluginVersion {
			t.Errorf("npm version = %q, want %s", npm.Version, dshPluginVersion)
		}
		if npm.DSH.Bundle.Patch != "./dsh/cordis.patch.yml" {
			t.Errorf("npm DSH bundle patch = %q, want ./dsh/cordis.patch.yml", npm.DSH.Bundle.Patch)
		}

		var lockfile struct {
			Importers map[string]struct {
				Version string `yaml:"version"`
			} `yaml:"importers"`
		}
		readYAMLContractFile(t, filepath.Join(pluginRoot, "pnpm-lock.yaml"), &lockfile)
		rootImporter, ok := lockfile.Importers["."]
		if !ok || rootImporter.Version != dshPluginVersion {
			t.Errorf("pnpm root importer version = %q (present=%t), want %s", rootImporter.Version, ok, dshPluginVersion)
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

		changelog := readRepoFile(t, filepath.Join(pluginRoot, "CHANGELOG.md"))
		wantReleaseHeading := fmt.Sprintf("## [%s] - %s", dshPluginVersion, dshPluginReleaseDate)
		if !strings.Contains(changelog, wantReleaseHeading) {
			t.Errorf("changelog missing release heading %q", wantReleaseHeading)
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
			assertGeneratedPresetSkills(t, pluginRoot, presetRoot, pack.Agent.Skills)
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
			`ACTIVE_PROFILE="replace-with-web-or-desktop-profile-name"`,
			`dsh plugin --profile "$ACTIVE_PROFILE" add "@anban/dsh-plugin@${PUBLISHED_VERSION}"`,
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh install-presets`,
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh status`,
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh remove-presets`,
			`dsh plugin --profile "$ACTIVE_PROFILE" remove @anban/dsh-plugin`,
			`dsh plugin --profile "$ACTIVE_PROFILE" approve-builds`,
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

	t.Run("documentation defines exact supported surfaces and canonical ownership", func(t *testing.T) {
		guide := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		readme := readRepoFile(t, filepath.Join(pluginRoot, "README.md"))
		for _, body := range []string{guide, readme} {
			for _, row := range []string{
				"| Skills-only installer | Yes | No | No | No |",
				"| Claude Code plugin | Yes | Claude Agent | Claude MCP adapter | No |",
				"| Codex plugin | Yes | Codex subagent | Codex MCP adapter | No |",
				"| Full DSH plugin | Article/Seednote generated copies | DSH composition | Official Bundle/MCP adapters | Article/Seednote only |",
			} {
				if !strings.Contains(body, row) {
					t.Errorf("DSH documentation missing support row %q", row)
				}
			}
		}
		for _, want := range []string{
			"DSH is not a separate Anban business workflow or Skill tree.",
			"harness/skills/**",
			"Agent Pack generator copies the exact declared Skills",
			"DSH-only code",
			"only Article and Seednote",
		} {
			if !strings.Contains(guide, want) {
				t.Errorf("DSH installation guide missing ownership boundary %q", want)
			}
		}
		if regexp.MustCompile(`(?i)(?:ecommerce|live-slicer|moments|montage)[^.|\n]*(?:DSH Preset|Preset support)`).MatchString(guide) {
			t.Error("DSH documentation claims an unsupported scenario or Preset")
		}
	})

	t.Run("documentation limits installs to supported immutable artifacts", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		for _, want := range []string{
			"public npm package (primary)",
			`PUBLISHED_VERSION="replace-with-published-version"`,
			`npm view "@anban/dsh-plugin@${PUBLISHED_VERSION}" version`,
			"checksummed GitHub Release",
			"anban-dsh-plugin-<published-version>.tgz.sha256",
			"immutable Git tag or full commit",
			"prepare build",
			"pnpm pack --json",
			"exact tarball path reported in the `filename` field",
			`dsh plugin --profile "$ACTIVE_PROFILE" add "$PACKED_TARBALL"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("DSH installation guide missing supported artifact guidance %q", want)
			}
		}
		for _, finding := range dshDocumentationPluginAddFindings(body) {
			t.Error(finding)
		}
	})

	t.Run("documentation exposes both executable Preset management surfaces", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		normalized := strings.Join(strings.Fields(body), " ")
		for _, want := range []string{
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh install-presets`,
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh status`,
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh install-presets --force`,
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh remove-presets`,
			"/anban-presets-install",
			"/anban-presets-status",
			"/anban-presets-install force",
			"/anban-presets-remove confirm",
			"same Preset manager",
		} {
			if !strings.Contains(normalized, want) {
				t.Errorf("DSH installation guide missing executable host surface %q", want)
			}
		}
		if got := strings.Count(body, "\nACTIVE_PROFILE="); got != 1 {
			t.Errorf("ACTIVE_PROFILE definitions = %d, want 1", got)
		}
		if regexp.MustCompile(`(?m)^dsh plugin --profile web (?:add|exec|remove)\b`).MatchString(body) {
			t.Error("DSH lifecycle hard-codes web after active profile discovery")
		}
	})

	t.Run("documentation normalizes DSH home before filesystem use", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		sectionStart := strings.Index(body, "## Resolve DSH home")
		sectionEndOffset := strings.Index(body[sectionStart+1:], "\n## ")
		if sectionStart < 0 || sectionEndOffset < 0 {
			t.Fatal("DSH installation guide missing bounded home normalization section")
		}
		section := body[sectionStart : sectionStart+1+sectionEndOffset]
		scriptMatch := regexp.MustCompile(`(?s)node <<'NODE'\n(.*?)\nNODE`).FindStringSubmatch(section)
		if scriptMatch == nil {
			t.Fatal("DSH home normalization section missing quoted Node heredoc")
		}
		script := scriptMatch[1]
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		absoluteFixture := filepath.Join(t.TempDir(), "absolute-dsh-home")
		for _, test := range []struct {
			name     string
			input    *string
			expected string
		}{
			{name: "unset", expected: filepath.Join(home, ".dsh")},
			{name: "whitespace", input: dshStringPointer("  \t "), expected: filepath.Join(home, ".dsh")},
			{name: "significant whitespace", input: dshStringPointer("  relative/dsh-home  "), expected: filepath.Join(pluginRoot, "  relative/dsh-home  ")},
			{name: "tilde", input: dshStringPointer("~"), expected: home},
			{name: "tilde child", input: dshStringPointer("~/custom"), expected: filepath.Join(home, "custom")},
			{name: "tilde backslash child", input: dshStringPointer(`~\custom`), expected: filepath.Join(home, "custom")},
			{name: "relative", input: dshStringPointer("relative/dsh-home"), expected: filepath.Join(pluginRoot, "relative/dsh-home")},
			{name: "absolute", input: &absoluteFixture, expected: absoluteFixture},
		} {
			t.Run(test.name, func(t *testing.T) {
				cmd := exec.Command("node", "-e", script)
				cmd.Dir = pluginRoot
				cmd.Env = dshEnvironmentWithHome(os.Environ(), test.input)
				output, err := cmd.Output()
				if err != nil {
					t.Fatalf("home normalizer failed: %v", err)
				}
				if got := string(output); got != test.expected {
					t.Errorf("normalized home = %q, want %q", got, test.expected)
				}
			})
		}
		for _, want := range []string{`const configured = process.env.DSH_HOME ?? ''`, `configured.trim() === ''`} {
			if !strings.Contains(script, want) {
				t.Errorf("DSH home normalizer missing official configured-value semantics %q", want)
			}
		}
		if strings.Contains(script, `(process.env.DSH_HOME ?? '').trim()`) {
			t.Error("DSH home normalizer trims significant whitespace from a configured nonblank value")
		}
		root := string(filepath.Separator)
		rootCommand := exec.Command("node", "-e", script)
		rootCommand.Dir = pluginRoot
		rootCommand.Env = dshEnvironmentWithHome(os.Environ(), &root)
		rootOutput, rootErr := rootCommand.CombinedOutput()
		if rootErr == nil || !strings.Contains(strings.ToLower(string(rootOutput)), "filesystem root") {
			t.Errorf("filesystem root was not rejected: err=%v output=%q", rootErr, rootOutput)
		}

		captureIndex := strings.Index(section, `NORMALIZED_DSH_HOME="$(`)
		emptyGuardIndex := strings.Index(section, `[ -n "$NORMALIZED_DSH_HOME" ]`)
		exportIndex := strings.Index(section, `export DSH_HOME="$NORMALIZED_DSH_HOME"`)
		if captureIndex < 0 || emptyGuardIndex <= captureIndex || exportIndex <= emptyGuardIndex {
			t.Errorf("unsafe DSH home normalization order: capture=%d guard=%d export=%d", captureIndex, emptyGuardIndex, exportIndex)
		}
		if strings.Contains(body[:sectionStart], "$DSH_HOME") {
			t.Error("DSH installation guide uses DSH_HOME before resolving the effective home")
		}
		for _, want := range []string{
			`install -d -m 700 "$DSH_HOME"`,
			`chmod 700 "$DSH_HOME"`,
			`chmod 600 "$DSH_HOME/.credentials.yaml"`,
			`mv -- "$DSH_HOME/.agent-presets/.anban-dsh.lock" "$LOCK_QUARANTINE"`,
		} {
			index := strings.Index(body, want)
			if index < 0 {
				t.Errorf("DSH installation guide missing quoted home filesystem command %q", want)
			} else if index <= sectionStart+exportIndex {
				t.Errorf("DSH home filesystem command precedes effective home definition %q", want)
			}
		}
	})

	t.Run("documentation protects the global two-step lifecycle", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		normalized := strings.Join(strings.Fields(body), " ")
		for _, want := range []string{
			"Bundle is profile-local",
			"Presets are global",
			"shared by every profile using the same `DSH_HOME`",
			"Bundle activation never installs, upgrades, or removes Presets",
			"cross-profile",
			`dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh install-presets --force`,
			"ERR_RUNTIME_MISSING",
			"ERR_PRESET_LOCK_INVALID",
			"lock residue",
		} {
			if !strings.Contains(normalized, want) {
				t.Errorf("DSH installation guide missing global lifecycle guidance %q", want)
			}
		}
		status := strings.Index(body, `dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh status`)
		force := strings.Index(body, `dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh install-presets --force`)
		removePresets := strings.Index(body, `dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh remove-presets`)
		removeBundle := strings.Index(body, `dsh plugin --profile "$ACTIVE_PROFILE" remove @anban/dsh-plugin`)
		if status < 0 || force < 0 || removePresets < 0 || removeBundle < 0 || status >= force || status >= removePresets || removePresets >= removeBundle {
			t.Errorf("DSH lifecycle order is unsafe: status=%d force=%d presets=%d bundle=%d", status, force, removePresets, removeBundle)
		}
		if regexp.MustCompile(`rm\s+(?:-[^\s]*r[^\s]*\s+)?[^\n]*\.anban-dsh\.lock`).MatchString(body) {
			t.Error("DSH lock recovery must not recommend deleting lock residue")
		}
	})

	t.Run("documentation uses official credential precedence and path", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		normalized := strings.Join(strings.Fields(body), " ")
		precedence := "inherited process environment (read-only, highest priority) -> `$DSH_HOME/.credentials.yaml` (managed, writable) -> invocation-project `.env` -> `$DSH_HOME/.env`"
		if !strings.Contains(normalized, precedence) {
			t.Errorf("DSH credential precedence must be exactly %q", precedence)
		}
		if got := strings.Count(body, "ANBAN_API_KEY: <value>"); got != 1 {
			t.Errorf("DSH persistent credential placeholder count = %d, want 1", got)
		}
		for _, want := range []string{"temporary or CI override", "owner-only", "0700", "0600", "model onboarding", "does not configure arbitrary third-party credentials"} {
			if !strings.Contains(normalized, want) {
				t.Errorf("DSH credential guidance missing %q", want)
			}
		}
		for _, forbidden := range []string{"~/.dsh/.credentials.yaml", "<harness-home>/.credentials.yaml", "<dsh-home>/.credentials.yaml"} {
			if strings.Contains(strings.ToLower(body), forbidden) {
				t.Errorf("DSH credential guidance contains generic path %q", forbidden)
			}
		}
	})

	t.Run("release notes keep package publication under operator control", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "CHANGELOG.md"))
		releaseHeading := fmt.Sprintf("## [%s] - %s", dshPluginVersion, dshPluginReleaseDate)
		start := strings.Index(body, releaseHeading)
		if start < 0 {
			t.Fatalf("DSH changelog missing release section %q", releaseHeading)
		}
		endOffset := strings.Index(body[start+1:], "\n## [")
		end := len(body)
		if endOffset >= 0 {
			end = start + 1 + endOffset
		}
		release := body[start:end]
		for _, want := range []string{"Prepared", "does not claim npm publication", "release workflow", "release operator", "must"} {
			if !strings.Contains(release, want) {
				t.Errorf("DSH %s release section missing publication boundary %q", dshPluginVersion, want)
			}
		}
		if regexp.MustCompile(`(?i)(?:package|version) (?:is|is now|has been) (?:published|available) (?:on|from) npm`).MatchString(release) {
			t.Errorf("DSH %s release section falsely claims current npm availability", dshPluginVersion)
		}
	})

	t.Run("current release operators perform a real low privilege MCP check", func(t *testing.T) {
		body := readRepoFile(t, filepath.Join(pluginRoot, "docs", "dsh-installation.md"))
		normalized := strings.Join(strings.Fields(body), " ")
		for _, want := range []string{
			dshPluginVersion + " release-operator checklist",
			"after automated code gates and before public announcement",
			"dedicated low-privilege",
			"outside Git",
			"list_projects",
			"get_project_profile",
			"account, environment, time, and result",
			"without recording the key",
			"rotate or revoke",
			"missing valid key is an explicit manual release gate",
			"automated missing-key and invalid-key tests remain required",
		} {
			if !strings.Contains(normalized, want) {
				t.Errorf("DSH release-operator checklist missing %q", want)
			}
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

func TestDSHDocumentationPluginAddClassifierRejectsSourceDirectories(t *testing.T) {
	fixture := func(specifier string) string {
		return "```bash\ndsh plugin --profile \"$ACTIVE_PROFILE\" add " + specifier + "\n```"
	}
	for _, allowed := range []string{
		`"@anban/dsh-plugin@4.1.14"`,
		`"/tmp/anban-dsh-plugin-4.1.14.tgz"`,
		`"file:/tmp/anban-dsh-plugin-4.1.14.tgz"`,
		`"https://github.com/anbanai/anbancreator/releases/download/v4.1.14/anban-dsh-plugin-4.1.14.tgz"`,
		`"git+https://github.com/anbanai/harness.git#v4.1.14"`,
		`"git+https://github.com/anbanai/harness.git#0123456789abcdef0123456789abcdef01234567"`,
	} {
		if findings := dshDocumentationPluginAddFindings(fixture(allowed)); len(findings) != 0 {
			t.Errorf("approved add %q findings = %v", allowed, findings)
		}
	}
	for _, forbidden := range []string{
		".",
		"..",
		"../legacy-plugin",
		"./plugins",
		"/tmp/legacy-plugin",
		"file:../legacy-plugin",
		"file:/tmp/legacy-plugin",
		"file:/tmp/anban-dsh-plugin.tgz",
		`"@anban/dsh-plugin"`,
		`"@anban/dsh-plugin@latest"`,
		`"@anban/dsh-plugin@^4.1.14"`,
		`"@anban/dsh-plugin@01.2.3"`,
		`"/tmp/arbitrary-plugin-4.1.12.tgz"`,
		`"https://example.com/anban-dsh-plugin-4.1.12.tgz"`,
		`"https://github.com/anbanai/anbancreator/releases/download/v4.1.14/anban-dsh-plugin-4.1.15.tgz"`,
		`"git+https://github.com/anbanai/harness.git#main"`,
		`"git+https://github.com/anbanai/harness.git#HEAD"`,
		`"git+https://github.com/anbanai/harness.git#v01.2.3"`,
		`"git+https://github.com/anbanai/harness.git"`,
	} {
		if findings := dshDocumentationPluginAddFindings(fixture(forbidden)); len(findings) == 0 {
			t.Errorf("source-directory add %q was accepted", forbidden)
		}
	}
	for _, source := range []string{
		"```bash\n$ dsh plugin --profile \"$ACTIVE_PROFILE\" add \"@anban/dsh-plugin\"\n```",
		"```bash\nCHECK_ONLY=1 dsh plugin --profile \"$ACTIVE_PROFILE\" add \"/tmp/arbitrary-plugin-4.1.12.tgz\"\n```",
		"```bash\ncommand dsh plugin --profile \"$ACTIVE_PROFILE\" add \"git+https://github.com/anbanai/harness.git#main\"\n```",
		"```bash\ndsh plugin --profile \"$ACTIVE_PROFILE\" add \\\n  \"file:/tmp/legacy-plugin\"\n```",
		"```bash\nenv -- dsh plugin --profile \"$ACTIVE_PROFILE\" add \"@anban/dsh-plugin\"\n```",
		"```bash\ncommand -- dsh plugin --profile \"$ACTIVE_PROFILE\" add \"/tmp/arbitrary-plugin-4.1.12.tgz\"\n```",
		"```bash\nenv -u DSH_HOME dsh plugin --profile \"$ACTIVE_PROFILE\" add \"git+https://github.com/anbanai/harness.git#main\"\n```",
		"```bash\nONE=1 TWO=2 wrapper -- dsh plugin --profile \"$ACTIVE_PROFILE\" add \"file:/tmp/legacy-plugin\"\n```",
		"```bash\nLABEL=\"two words\" dsh plugin --profile \"$ACTIVE_PROFILE\" add \"/tmp/with spaces/arbitrary-plugin-4.1.12.tgz\"\n```",
		"```bash\ndsh plugin --profile \"$ACTIVE_PROFILE\" add\n```",
		"```bash\ndsh plugin --profile \"$ACTIVE_PROFILE\" add \"unterminated\n```",
	} {
		if findings := dshDocumentationPluginAddFindings(source); len(findings) == 0 {
			t.Errorf("prefixed or continued forbidden add was not classified:\n%s", source)
		}
	}
	for _, source := range []string{
		"```bash\n$ dsh plugin --profile \"$ACTIVE_PROFILE\" add \"@anban/dsh-plugin@4.1.14\"\n```",
		"```bash\nCHECK_ONLY=1 dsh plugin --profile \"$ACTIVE_PROFILE\" add \"file:/tmp/anban-dsh-plugin-4.1.14.tgz\"\n```",
		"```bash\ncommand dsh plugin --profile \"$ACTIVE_PROFILE\" add \"git+https://github.com/anbanai/harness.git#0123456789abcdef0123456789abcdef01234567\"\n```",
		"```bash\ndsh plugin --profile \"$ACTIVE_PROFILE\" add \\\n  \"https://github.com/anbanai/anbancreator/releases/download/v4.1.14/anban-dsh-plugin-4.1.14.tgz\"\n```",
		"```bash\nenv -- dsh plugin --profile \"$ACTIVE_PROFILE\" add \"@anban/dsh-plugin@4.1.14\"\n```",
		"```bash\ncommand -- dsh plugin --profile \"$ACTIVE_PROFILE\" add \"file:/tmp/anban-dsh-plugin-4.1.14.tgz\"\n```",
		"```bash\nenv -u DSH_HOME dsh plugin --profile \"$ACTIVE_PROFILE\" add \"git+https://github.com/anbanai/harness.git#0123456789abcdef0123456789abcdef01234567\"\n```",
		"```bash\nONE=1 TWO=2 LABEL=\"two words\" wrapper -- dsh plugin --profile \"$ACTIVE_PROFILE\" add \"/tmp/with spaces/anban-dsh-plugin-4.1.14.tgz\"\n```",
	} {
		if findings := dshDocumentationPluginAddFindings(source); len(findings) != 0 {
			t.Errorf("approved prefixed or continued add findings = %v:\n%s", findings, source)
		}
	}
	if findings := dshDocumentationPluginAddFindings(`Run dsh plugin --profile web add "@anban/dsh-plugin" in a shell.`); len(findings) != 0 {
		t.Errorf("prose outside shell fences was classified: %v", findings)
	}
}

var dshShellFencePattern = regexp.MustCompile("(?s)```(?:bash|sh|shell)\\n(.*?)```")
var dshShellAssignmentPattern = regexp.MustCompile(`^([A-Z_][A-Z0-9_]*)=["']([^"']*)["']$`)
var dshNpmAddPattern = regexp.MustCompile(`^@anban/dsh-plugin@(?:replace-with-published-version|(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))$`)
var dshLocalTarballAddPattern = regexp.MustCompile(`(^|[/\\])anban-dsh-plugin-(?:replace-with-published-version|(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))\.tgz$`)
var dshReleaseTarballAddPattern = regexp.MustCompile(`^https://github\.com/anbanai/anbancreator/releases/download/v((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))/anban-dsh-plugin-((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))\.tgz$`)
var dshGitAddPattern = regexp.MustCompile(`^git\+https://github\.com/anbanai/harness\.git#(.+)$`)
var dshGitTagPattern = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`)
var dshGitCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func dshDocumentationPluginAddFindings(source string) []string {
	variables := make(map[string]string)
	var findings []string
	for _, block := range dshShellFencePattern.FindAllStringSubmatch(source, -1) {
		for _, line := range dshShellLogicalLines(block[1]) {
			if assignment := dshShellAssignmentPattern.FindStringSubmatch(line); assignment != nil {
				variables[assignment[1]] = assignment[2]
				continue
			}
			commandLine := strings.TrimPrefix(line, "$ ")
			words, ambiguous := dshShellWords(commandLine)
			sawAddSequence := false
			for commandIndex := 0; commandIndex+1 < len(words); commandIndex++ {
				if words[commandIndex] != "dsh" || words[commandIndex+1] != "plugin" {
					continue
				}
				commandEnd := len(words)
				for i := commandIndex + 2; i < len(words); i++ {
					if dshShellOperator(words[i]) {
						commandEnd = i
						break
					}
				}
				addIndex := -1
				for i := commandIndex + 2; i < commandEnd; i++ {
					if words[i] == "add" {
						addIndex = i
						break
					}
				}
				if addIndex < 0 {
					continue
				}
				sawAddSequence = true
				if ambiguous {
					findings = append(findings, line+": ambiguous shell tokenization")
					break
				}
				if addIndex+1 >= commandEnd {
					findings = append(findings, line+": missing add specifier")
					continue
				}
				specifier := os.Expand(words[addIndex+1], func(key string) string {
					if value, ok := variables[key]; ok {
						return value
					}
					return "${" + key + "}"
				})
				npmPackage := dshNpmAddPattern.MatchString(specifier)
				tarball := false
				if strings.HasPrefix(specifier, "file:") {
					tarball = dshLocalTarballAddPattern.MatchString(strings.TrimPrefix(specifier, "file:"))
				} else if release := dshReleaseTarballAddPattern.FindStringSubmatch(specifier); release != nil {
					tarball = release[1] == release[2]
				} else if !strings.Contains(specifier, "://") {
					tarball = dshLocalTarballAddPattern.MatchString(specifier)
				}
				immutableGit := false
				if git := dshGitAddPattern.FindStringSubmatch(specifier); git != nil {
					ref := git[1]
					immutableGit = ref == "replace-with-immutable-tag-or-full-40-character-commit" || dshGitTagPattern.MatchString(ref) || dshGitCommitPattern.MatchString(ref)
				}
				if !npmPackage && !tarball && !immutableGit {
					findings = append(findings, fmt.Sprintf("%s: unsupported add specifier %s", line, specifier))
				}
			}
			if ambiguous && !sawAddSequence && regexp.MustCompile(`\bdsh\s+plugin\b.*\badd\b`).MatchString(commandLine) {
				findings = append(findings, line+": ambiguous shell tokenization")
			}
		}
	}
	return findings
}

func dshShellLogicalLines(block string) []string {
	var logicalLines []string
	current := ""
	for _, rawLine := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		continued := strings.HasSuffix(trimmed, `\`)
		fragment := strings.TrimSpace(strings.TrimSuffix(trimmed, `\`))
		if current == "" {
			current = fragment
		} else if fragment != "" {
			current += " " + fragment
		}
		if !continued && current != "" {
			logicalLines = append(logicalLines, current)
			current = ""
		}
	}
	if current != "" {
		logicalLines = append(logicalLines, current)
	}
	return logicalLines
}

func dshShellWords(line string) ([]string, bool) {
	var words []string
	var word strings.Builder
	wordStarted := false
	var quote byte
	ambiguous := false
	flush := func() {
		if wordStarted {
			words = append(words, word.String())
		}
		word.Reset()
		wordStarted = false
	}
	for index := 0; index < len(line); index++ {
		character := line[index]
		if quote != 0 {
			if character == quote {
				quote = 0
			} else if quote == '"' && character == '\\' {
				if index+1 >= len(line) {
					ambiguous = true
				} else {
					index++
					word.WriteByte(line[index])
				}
			} else {
				word.WriteByte(character)
			}
			wordStarted = true
			continue
		}
		if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			flush()
			continue
		}
		if character == '"' || character == '\'' {
			quote = character
			wordStarted = true
			continue
		}
		if character == '\\' {
			if index+1 >= len(line) {
				ambiguous = true
			} else {
				index++
				word.WriteByte(line[index])
				wordStarted = true
			}
			continue
		}
		if character == ';' || character == '&' || character == '|' {
			flush()
			operator := string(character)
			if character != ';' && index+1 < len(line) && line[index+1] == character {
				operator += string(character)
				index++
			}
			words = append(words, operator)
			continue
		}
		word.WriteByte(character)
		wordStarted = true
	}
	if quote != 0 {
		ambiguous = true
	}
	flush()
	return words, ambiguous
}

func dshShellOperator(word string) bool {
	return word == ";" || word == "&" || word == "&&" || word == "|" || word == "||"
}

func dshStringPointer(value string) *string {
	return &value
}

func dshEnvironmentWithHome(environment []string, home *string) []string {
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "DSH_HOME=") {
			filtered = append(filtered, entry)
		}
	}
	if home != nil {
		filtered = append(filtered, "DSH_HOME="+*home)
	}
	return filtered
}

func TestDSHCIWorkflowRunsLockedChecksAndPortableRuntimeTests(t *testing.T) {
	workflow := readWorkflowContract(t, ".github/workflows/ci.yml")
	if err := validateDSHCIWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
}

func TestDSHReleaseWorkflowGatesExactRegistryRelease(t *testing.T) {
	workflow := readWorkflowContract(t, ".github/workflows/release.yml")
	if err := validateDSHReleaseWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
}

func TestDSHReleaseWorkflowRejectsArtifactMutationGaps(t *testing.T) {
	findStep := func(t *testing.T, job workflowJob, name string) int {
		t.Helper()
		for index, step := range job.Steps {
			if step.Name == name {
				return index
			}
		}
		t.Fatalf("missing workflow step %q", name)
		return -1
	}

	t.Run("upload before digest", func(t *testing.T) {
		workflow := readWorkflowContract(t, ".github/workflows/release.yml")
		job := workflow.Jobs["dsh-package"]
		digest := findStep(t, job, "Create exact DSH artifact digest")
		upload := findStep(t, job, "Upload exact DSH release artifact")
		job.Steps[digest], job.Steps[upload] = job.Steps[upload], job.Steps[digest]
		workflow.Jobs["dsh-package"] = job
		if err := validateDSHReleaseWorkflow(workflow); err == nil {
			t.Fatal("upload-before-digest workflow satisfied the release contract")
		}
	})

	for _, test := range []struct {
		job      string
		consumer string
	}{
		{job: "dsh-desktop-acceptance", consumer: "Install exact plugin through public DSH"},
		{job: "release", consumer: "Stage exact DSH package asset"},
	} {
		t.Run("mutation before "+test.consumer, func(t *testing.T) {
			workflow := readWorkflowContract(t, ".github/workflows/release.yml")
			job := workflow.Jobs[test.job]
			consumer := findStep(t, job, test.consumer)
			mutation := workflowStep{
				Name: "Replace verified artifact",
				Run:  "node -e \"require('node:fs').writeFileSync('release/dsh/package.tgz', 'changed')\"",
			}
			job.Steps = append(job.Steps[:consumer], append([]workflowStep{mutation}, job.Steps[consumer:]...)...)
			workflow.Jobs[test.job] = job
			if err := validateDSHReleaseWorkflow(workflow); err == nil {
				t.Fatalf("artifact mutation before %q satisfied the release contract", test.consumer)
			}
		})
	}
}

func TestDSHReleaseArtifactDigestRejectsChangedTarballBytes(t *testing.T) {
	root := t.TempDir()
	metadataPath := filepath.Join(root, "pack.json")
	tarballName := "anban-dsh-plugin-" + dshPluginVersion + ".tgz"
	tarballPath := filepath.Join(root, tarballName)
	digestPath := filepath.Join(root, "artifact.sha256.json")
	metadata := fmt.Sprintf(`{
  "name": "@anban/dsh-plugin",
  "version": %q,
  "filename": %q,
  "files": [{"path": "package.json"}]
}`, dshPluginVersion, tarballName)
	if err := os.WriteFile(metadataPath, []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tarballPath, []byte("trusted artifact bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(repoRoot(t), "scripts", "dsh-artifact-integrity.mjs")
	run := func(action string, expectedDigest ...string) ([]byte, error) {
		args := []string{script, action, metadataPath, dshPluginVersion, digestPath}
		args = append(args, expectedDigest...)
		command := exec.Command("node", args...)
		return command.CombinedOutput()
	}
	if output, err := run("create"); err != nil {
		t.Fatalf("create release artifact digest: %v\n%s", err, output)
	}
	var digestManifest struct {
		TarballSHA256 string `json:"tarballSha256"`
	}
	readJSONContractFile(t, digestPath, &digestManifest)
	if output, err := run("verify", digestManifest.TarballSHA256); err != nil {
		t.Fatalf("verify unchanged release artifact: %v\n%s", err, output)
	}
	if output, err := run("verify", strings.Repeat("0", 64)); err == nil {
		t.Fatalf("release artifact passed an unrelated trusted-job digest:\n%s", output)
	}
	if err := os.WriteFile(tarballPath, []byte("tampered artifact byte"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := run("verify", digestManifest.TarballSHA256); err == nil {
		t.Fatalf("changed release artifact passed digest verification:\n%s", output)
	}
	if err := os.WriteFile(tarballPath, []byte("trusted artifact bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	tamperedMetadata := strings.Replace(metadata, `"package.json"`, `"tampered.js"`, 1)
	if err := os.WriteFile(metadataPath, []byte(tamperedMetadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := run("verify", digestManifest.TarballSHA256); err == nil {
		t.Fatalf("changed pack metadata passed digest verification:\n%s", output)
	}
}

func TestDSHDesktopProcessSupervisor(t *testing.T) {
	command := exec.Command(
		"node",
		"--test",
		filepath.Join(repoRoot(t), "scripts", "dsh-process-supervisor.test.mjs"),
		filepath.Join(repoRoot(t), "scripts", "dsh-desktop-acceptance.test.mjs"),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Desktop process supervisor tests: %v\n%s", err, output)
	}
	body := readRepoFile(t, filepath.Join(repoRoot(t), "scripts", "dsh-desktop-acceptance.mjs"))
	for _, required := range []string{"runBoundedCommand", "spawnProcessTree", "terminateProcessTree"} {
		if !strings.Contains(body, required) {
			t.Errorf("Desktop acceptance does not use shared process supervisor %q", required)
		}
	}
	for _, forbidden := range []string{"child.kill()", "child.kill('SIGKILL')"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("Desktop acceptance still uses direct child termination %q", forbidden)
		}
	}

	supervisor := readRepoFile(t, filepath.Join(repoRoot(t), "scripts", "dsh-process-supervisor.mjs"))
	jobAdapter := readRepoFile(t, filepath.Join(repoRoot(t), "scripts", "dsh-windows-job.ps1"))
	for _, required := range []string{
		"JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE",
		"CREATE_SUSPENDED",
		"EXTENDED_STARTUPINFO_PRESENT",
		"PROC_THREAD_ATTRIBUTE_JOB_LIST",
		"STARTUPINFOEX",
		"ResumeThread",
	} {
		if !strings.Contains(jobAdapter, required) {
			t.Errorf("Windows Job Object adapter missing %q", required)
		}
	}
	if !strings.Contains(supervisor, "dsh-windows-job.ps1") {
		t.Error("Desktop process supervisor does not launch the Windows Job Object adapter")
	}
	for _, forbidden := range []string{"AssignProcessToJobObject", "Get-CimInstance", "taskkill"} {
		if strings.Contains(supervisor, forbidden) || strings.Contains(jobAdapter, forbidden) {
			t.Errorf("Windows process supervision still uses PID-based cleanup %q", forbidden)
		}
	}
}

func TestDSHReliabilityPlanUsesExecutableVitestRepetition(t *testing.T) {
	body := readRepoFile(t, filepath.Join(repoRoot(t), "docs", "superpowers", "plans", "2026-08-17-dsh-plugin-reliability.md"))
	if strings.Contains(body, "--repeat=3") {
		t.Fatal("Vitest 4.1.8 plan command uses unsupported --repeat=3")
	}
	for _, want := range []string{
		"Vitest 4.1.8",
		"for run in 1 2 3; do",
		"pnpm vitest run dsh/tests/preset-lock.test.ts dsh/tests/presets.test.ts dsh/tests/preset-manager.test.ts",
		"done",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("DSH reliability plan missing executable repetition fragment %q", want)
		}
	}
}

func TestDSHWindowsCmdProbeUsesTheRealPlatformGate(t *testing.T) {
	body := readRepoFile(t, filepath.Join(repoRoot(t), "harness", "dsh", "tests", "profile-smoke.test.ts"))
	gate := "it.runIf(process.platform === 'win32')"
	probe := "executes a cmd shim with spaces and metacharacters through cross-spawn"
	if !strings.Contains(body, gate) || !strings.Contains(body, probe) {
		t.Fatalf("Windows cmd probe must use %q for %q", gate, probe)
	}
}

func TestWorkflowContractStructureRejectsCommentsOtherJobsAndDisabledSteps(t *testing.T) {
	workflow := parseWorkflowContract(t, `
name: adversarial
# pnpm run smoke:profile
jobs:
  unrelated:
    runs-on: macos-latest
    steps:
      - name: Real profile smoke
        run: pnpm run smoke:profile
  target:
    runs-on: windows-latest
    steps:
      - name: Real profile smoke
        if: false
        run: pnpm run smoke:profile
`)
	target := workflow.Jobs["target"]
	if _, err := requireEnabledRunStep(target, "Real profile smoke", "pnpm run smoke:profile"); err == nil {
		t.Fatal("disabled target step or unrelated job satisfied the structural command contract")
	}
}

func TestReleasePermissionContractRejectsInheritedPrivilegedAcceptance(t *testing.T) {
	workflow := parseWorkflowContract(t, `
name: adversarial
permissions:
  contents: write
  id-token: write
jobs:
  dsh-desktop-acceptance:
    runs-on: macos-latest
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      id-token: write
`)
	if err := validateReleasePermissionContract(workflow); err == nil {
		t.Fatal("acceptance job inherited privileged workflow permissions")
	}
}

func TestReleaseDesktopArtifactContractSupportsFutureVersionsAndRejectsLiterals(t *testing.T) {
	dynamicVersion := "${{ steps.version.outputs.VERSION }}"
	step := workflowStep{
		Run: `node ../scripts/dsh-verify-pack.mjs ../release/dsh/pack.json "${{ steps.version.outputs.VERSION }}"`,
	}
	if err := validateReleaseCrossPlatformDSHPackStep(step); err != nil {
		t.Fatalf("runtime-derived verifier rejected: %v", err)
	}

	tarball := "${{ github.workspace }}/release/dsh/anban-dsh-plugin-${{ steps.version.outputs.VERSION }}.tgz"
	resolved := strings.ReplaceAll(tarball, dynamicVersion, "4.1.14")
	if resolved != "${{ github.workspace }}/release/dsh/anban-dsh-plugin-4.1.14.tgz" {
		t.Fatalf("future release tarball = %q", resolved)
	}

	step.Run = `node ../scripts/dsh-verify-pack.mjs ../release/dsh/pack.json 4.1.14`
	if err := validateReleaseCrossPlatformDSHPackStep(step); err == nil {
		t.Fatal("hardcoded release version satisfied the Desktop pack contract")
	}
}

func TestWorkflowContractStructureRejectsWrongStepOrder(t *testing.T) {
	workflow := parseWorkflowContract(t, `
name: adversarial order
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - name: Verify registry
        run: npm view package version
      - name: Publish package
        run: npm publish package.tgz
`)
	if err := requireNamedStepOrder(
		workflow.Jobs["release"],
		[]string{"Publish package", "Verify registry"},
	); err == nil {
		t.Fatal("reversed publish and registry verification steps satisfied the order contract")
	}
}

func TestDSHReleaseConcurrencyContractRejectsNonSerialReleaseKeys(t *testing.T) {
	tests := []struct {
		name        string
		concurrency workflowConcurrency
	}{
		{name: "missing concurrency"},
		{
			name: "static group",
			concurrency: workflowConcurrency{
				Group:            "dsh-release",
				CancelInProgress: dshBoolPointer(false),
			},
		},
		{
			name: "tag and manual runs use different keys",
			concurrency: workflowConcurrency{
				Group:            "dsh-release-${{ github.ref }}-${{ inputs.version }}",
				CancelInProgress: dshBoolPointer(false),
			},
		},
		{
			name: "cancels active release",
			concurrency: workflowConcurrency{
				Group:            dshReleaseConcurrencyGroup,
				CancelInProgress: dshBoolPointer(true),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDSHReleaseConcurrency(test.concurrency); err == nil {
				t.Fatal("non-serial release concurrency satisfied the workflow contract")
			}
		})
	}
}

func TestDSHReleaseProvenanceContractRejectsMentionsWithoutEnforcement(t *testing.T) {
	tests := []struct {
		name string
		run  string
	}{
		{
			name: "query without validation",
			run:  `npm view package version dist.integrity dist.attestations.provenance --json`,
		},
		{
			name: "validation without registry query",
			run: `

const publishedProvenance = published['dist.attestations.provenance']
if (typeof publishedProvenance !== 'object' || Array.isArray(publishedProvenance)) process.exit(1)
`,
		},
		{
			name: "commented validation",
			run: `
npm view package version dist.integrity dist.attestations.provenance --json
// const publishedProvenance = published['dist.attestations.provenance']
// if (typeof publishedProvenance !== 'object' || Array.isArray(publishedProvenance)) process.exit(1)
`,
		},
		{
			name: "object presence without SLSA provenance predicate",
			run: `
npm view package version dist.integrity dist.attestations.provenance --json
const publishedProvenance = published['dist.attestations.provenance']
if (typeof publishedProvenance !== 'object' || Array.isArray(publishedProvenance)) process.exit(1)
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateNpmProvenanceAcceptance(test.run); err == nil {
				t.Fatal("non-enforcing provenance snippet satisfied the registry acceptance contract")
			}
		})
	}
}

func TestWorkflowContractRejectsMutatingOrEmptyGitHubReleaseUpdates(t *testing.T) {
	tests := []struct {
		name string
		run  string
	}{
		{
			name: "clobber existing asset",
			run:  `gh release upload "$RELEASE_TAG" bin/* --clobber`,
		},
		{
			name: "create empty release before upload",
			run: `
gh release create "$RELEASE_TAG" --verify-tag --generate-notes
gh release upload "$RELEASE_TAG" bin/*
`,
		},
		{
			name: "upload missing assets before comparison",
			run: `
trap cleanup EXIT
isDraft=false
isPrerelease=false
targetCommitish=main
gh release create "$RELEASE_TAG" "${local_assets[@]}" --verify-tag
missing_assets=(bin/plugin.tgz)
gh release upload "$RELEASE_TAG" "${missing_assets[@]}"
gh release download "$RELEASE_TAG"
createHash('sha256')
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			step := workflowStep{Name: "Create Release", Run: test.run}
			if err := validateGitHubReleaseAssetStep(step); err == nil {
				t.Fatal("unsafe GitHub Release mutation satisfied the asset contract")
			}
		})
	}
}

func TestDSHReleaseVersionContractRejectsNonStableVersions(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{version: "4.1.12", valid: true},
		{version: "0.0.0", valid: true},
		{version: "4.1.12-rc.1", valid: false},
		{version: "4.1.12+build.7", valid: false},
		{version: "04.1.12", valid: false},
		{version: "4.1", valid: false},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			if got := dshStableReleaseVersion.MatchString(test.version); got != test.valid {
				t.Fatalf("stable release version acceptance = %t, want %t", got, test.valid)
			}
		})
	}
}

var dshStableReleaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

const (
	actionsCheckoutV4SHA         = "11d5960a326750d5838078e36cf38b85af677262"
	actionsDownloadArtifactV4SHA = "d3f86a106a0bac45b974a628896c90dbdf5c8093"
	actionsSetupGoV5SHA          = "40f1582b2485089dde7abd97c1529aa768e1baff"
	actionsSetupNodeV4SHA        = "49933ea5288caeca8642d1e84afbd3f7d6820020"
	actionsUploadArtifactV4SHA   = "ea165f8d65b6e75b540449e92b4886f43607fa02"
	dshDesktopCommit             = "4f68147091e585aaa1d815f99d30a657b3842d7c"
	pnpmActionSetupV4SHA         = "b906affcce14559ad1aafd4ab0e942779e9f58b1"
)

type workflowContract struct {
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Required bool `yaml:"required"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	Concurrency workflowConcurrency    `yaml:"concurrency"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowConcurrency struct {
	Group            string `yaml:"group"`
	CancelInProgress *bool  `yaml:"cancel-in-progress"`
}

type workflowJob struct {
	Needs       any               `yaml:"needs"`
	Outputs     map[string]string `yaml:"outputs"`
	RunsOn      string            `yaml:"runs-on"`
	Permissions map[string]string `yaml:"permissions"`
	Defaults    struct {
		Run struct {
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"run"`
	} `yaml:"defaults"`
	Strategy struct {
		Matrix map[string][]string `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name             string            `yaml:"name"`
	ID               string            `yaml:"id"`
	Uses             string            `yaml:"uses"`
	Run              string            `yaml:"run"`
	WorkingDirectory string            `yaml:"working-directory"`
	If               any               `yaml:"if"`
	With             map[string]any    `yaml:"with"`
	Env              map[string]string `yaml:"env"`
}

func readWorkflowContract(t *testing.T, relativePath string) workflowContract {
	t.Helper()
	path := filepath.Join(repoRoot(t), filepath.FromSlash(relativePath))
	return parseWorkflowContract(t, readRepoFile(t, path))
}

func parseWorkflowContract(t *testing.T, body string) workflowContract {
	t.Helper()
	var workflow workflowContract
	if err := yaml.Unmarshal([]byte(body), &workflow); err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	return workflow
}

func validateDSHCIWorkflow(workflow workflowContract) error {
	check, ok := workflow.Jobs["dsh-plugin"]
	if !ok || check.RunsOn != "ubuntu-latest" || check.Defaults.Run.WorkingDirectory != "harness" {
		return fmt.Errorf("dsh-plugin must be an Ubuntu job with harness as its working directory")
	}
	if err := requireActionInput(check, "Set up pnpm", "pnpm/action-setup@"+pnpmActionSetupV4SHA, "version", "11.19.0"); err != nil {
		return err
	}
	if err := requireActionInput(check, "Set up Node", "actions/setup-node@"+actionsSetupNodeV4SHA, "node-version", "24"); err != nil {
		return err
	}
	if _, err := requireEnabledRunStep(check, "Install DSH plugin dependencies", "pnpm install --frozen-lockfile"); err != nil {
		return err
	}
	if _, err := requireEnabledRunStep(check, "Check DSH plugin", "pnpm run check"); err != nil {
		return err
	}
	pack, err := requireEnabledStep(check, "Pack exact DSH Desktop acceptance artifact")
	if err != nil {
		return err
	}
	if err := validateCrossPlatformDSHPackStep(pack); err != nil {
		return err
	}
	if err := requireActionInput(check, "Upload exact DSH Desktop acceptance artifact", "actions/upload-artifact@"+actionsUploadArtifactV4SHA, "name", "anban-dsh-plugin"); err != nil {
		return err
	}
	fullChecks := 0
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if stepEnabled(step) && runHasCommand(step.Run, "pnpm run check") {
				fullChecks++
			}
		}
	}
	if fullChecks != 1 {
		return fmt.Errorf("enabled full DSH checks = %d, want exactly one", fullChecks)
	}

	portable, ok := workflow.Jobs["dsh-plugin-portability"]
	if !ok || portable.RunsOn != "${{ matrix.os }}" {
		return fmt.Errorf("dsh-plugin-portability must run its harness commands on matrix.os")
	}
	gotOS := append([]string(nil), portable.Strategy.Matrix["os"]...)
	sort.Strings(gotOS)
	if strings.Join(gotOS, ",") != "macos-latest,windows-latest" {
		return fmt.Errorf("DSH portability OS matrix = %v", gotOS)
	}
	if !workflowJobNeeds(portable, "dsh-plugin") {
		return fmt.Errorf("packaged Desktop acceptance must wait for the exact plugin artifact")
	}
	if err := requireActionInput(portable, "Set up pnpm", "pnpm/action-setup@"+pnpmActionSetupV4SHA, "version", "11.19.0"); err != nil {
		return err
	}
	if err := requireActionInput(portable, "Set up Node", "actions/setup-node@"+actionsSetupNodeV4SHA, "node-version", "24"); err != nil {
		return err
	}
	supervisor, err := requireEnabledStep(portable, "Run Desktop supervisor acceptance tests")
	if err != nil || !runHasCode(supervisor.Run, "dsh-process-supervisor.test.mjs") || !runHasCode(supervisor.Run, "dsh-desktop-acceptance.test.mjs") {
		return fmt.Errorf("Desktop portability matrix must run process-tree and bounded-loopback acceptance tests")
	}
	pluginInstall, err := requireEnabledRunStep(portable, "Install locked DSH plugin dependencies", "pnpm install --frozen-lockfile")
	if err != nil {
		return err
	}
	if pluginInstall.WorkingDirectory != "harness" {
		return fmt.Errorf("portable DSH dependency install must run from harness")
	}
	commandShape, err := requireEnabledStep(portable, "Run portable profile command tests")
	if err != nil || !runHasCode(commandShape.Run, "dsh/tests/profile-smoke.test.ts") || !runHasCode(commandShape.Run, "portable profile-smoke commands") {
		return fmt.Errorf("portable job must run the focused command-shape tests")
	}
	windowsProbe, err := requireEnabledStep(portable, "Execute the Windows cmd shim test")
	if err != nil || windowsProbe.If != nil || !runHasCode(windowsProbe.Run, "executes a cmd shim with spaces and metacharacters through cross-spawn") {
		return fmt.Errorf("portable job must execute the Windows-only it.runIf cmd shim test")
	}
	if err := validatePackagedDSHDesktopJob(portable); err != nil {
		return err
	}
	return requireNamedStepOrder(portable, []string{
		"Checkout pinned DSH Desktop",
		"Verify pinned DSH Desktop commit",
		"Download exact DSH Desktop acceptance artifact",
		"Run Desktop supervisor acceptance tests",
		"Install locked DSH plugin dependencies",
		"Install pinned DSH Desktop dependencies",
		"Build unsigned packaged DSH Desktop",
		"Install exact plugin through public DSH",
		"Launch packaged DSH Desktop application",
		"Validate packaged Desktop profile exports and Skill catalogs",
		"Run portable profile command tests",
		"Execute the Windows cmd shim test",
	})
}

func validatePackagedDSHDesktopJob(job workflowJob) error {
	checkout, err := requireEnabledStep(job, "Checkout pinned DSH Desktop")
	if err != nil || checkout.Uses != "actions/checkout@"+actionsCheckoutV4SHA || workflowScalar(checkout.With["repository"]) != "anywhere-labs/deepseek-harness-desktop" || workflowScalar(checkout.With["ref"]) != dshDesktopCommit || workflowScalar(checkout.With["path"]) != "_dsh-desktop" {
		return fmt.Errorf("Desktop acceptance must checkout the exact public Desktop commit")
	}
	verify, err := requireEnabledStep(job, "Verify pinned DSH Desktop commit")
	if err != nil || !runHasCode(verify.Run, "rev-parse") || !runHasCode(verify.Run, "HEAD") || !runHasCode(verify.Run, dshDesktopCommit) {
		return fmt.Errorf("Desktop acceptance must verify the exact checked out commit")
	}
	if err := requireActionInput(job, "Download exact DSH Desktop acceptance artifact", "actions/download-artifact@"+actionsDownloadArtifactV4SHA, "name", "anban-dsh-plugin"); err != nil {
		return err
	}
	install, err := requireEnabledRunStep(job, "Install pinned DSH Desktop dependencies", "corepack yarn install --immutable")
	if err != nil || install.WorkingDirectory != "_dsh-desktop" {
		return fmt.Errorf("Desktop acceptance must use the pinned immutable Yarn install")
	}
	build, err := requireEnabledRunStep(job, "Build unsigned packaged DSH Desktop", "corepack yarn package:dir")
	if err != nil {
		return err
	}
	if build.WorkingDirectory != "_dsh-desktop" {
		return fmt.Errorf("Desktop package build must run from the pinned checkout")
	}
	publicInstall, err := requireEnabledStep(job, "Install exact plugin through public DSH")
	if err != nil || !runHasCode(publicInstall.Run, "dsh-desktop-acceptance.mjs install") || !runHasCode(publicInstall.Run, "anban-dsh-plugin") {
		return fmt.Errorf("Desktop acceptance must install the handed-off tarball through public DSH plugin commands")
	}
	launch, err := requireEnabledStep(job, "Launch packaged DSH Desktop application")
	if err != nil || !runHasCode(launch.Run, "dsh-desktop-acceptance.mjs launch") {
		return fmt.Errorf("Desktop acceptance must launch the packaged application")
	}
	profile, err := requireEnabledStep(job, "Validate packaged Desktop profile exports and Skill catalogs")
	if err != nil || !runHasCode(profile.Run, "smoke-profile.mjs --existing-profile desktop "+dshPluginVersion) {
		return fmt.Errorf("Desktop acceptance must validate the installed Desktop profile and mounted catalogs")
	}
	for _, step := range job.Steps {
		for _, forbidden := range []string{"ELECTRON_RUN_AS_NODE", "desktopRuntime", "desktopPnpmBootstrap", "public Desktop runtime fixture"} {
			if runHasCode(step.Run, forbidden) {
				return fmt.Errorf("packaged Desktop acceptance uses forbidden fixture/private path %q", forbidden)
			}
		}
	}
	return nil
}

func validateDSHReleaseWorkflow(workflow workflowContract) error {
	if err := validateDSHReleaseConcurrency(workflow.Concurrency); err != nil {
		return err
	}
	if err := validateReleasePermissionContract(workflow); err != nil {
		return err
	}
	input, ok := workflow.On.WorkflowDispatch.Inputs["version"]
	if !ok || !input.Required {
		return fmt.Errorf("workflow_dispatch version input must be required")
	}
	packageJob, ok := workflow.Jobs["dsh-package"]
	if !ok || packageJob.RunsOn != "ubuntu-latest" {
		return fmt.Errorf("release workflow must build one DSH package artifact in an unprivileged Ubuntu job")
	}
	if packageJob.Permissions["contents"] != "read" || packageJob.Permissions["id-token"] != "" {
		return fmt.Errorf("DSH package job must use contents: read without id-token")
	}
	pack, err := requireEnabledStep(packageJob, "Pack DSH plugin exactly once")
	if err != nil || pack.ID != "pack" || !runHasCode(pack.Run, "pnpm pack --json --pack-destination") || !runHasCode(pack.Run, "parsePackResult") {
		return fmt.Errorf("unprivileged DSH package job must structurally capture one controlled pack result")
	}
	digest, err := requireEnabledStep(packageJob, "Create exact DSH artifact digest")
	if err != nil || digest.ID != "artifact_digest" || !runHasCode(digest.Run, "node scripts/dsh-artifact-integrity.mjs create") || !runHasCode(digest.Run, "tarballSha256") || !runHasCode(digest.Run, "GITHUB_OUTPUT") {
		return fmt.Errorf("unprivileged package job must create and export the exact artifact digest")
	}
	if packageJob.Outputs["tarball_sha256"] != "${{ steps.artifact_digest.outputs.tarball_sha256 }}" {
		return fmt.Errorf("unprivileged package job must expose an independent tarball SHA-256 output")
	}
	if err := requireActionInput(packageJob, "Upload exact DSH release artifact", "actions/upload-artifact@"+actionsUploadArtifactV4SHA, "name", "anban-dsh-plugin-release"); err != nil {
		return err
	}
	upload, err := requireEnabledStep(packageJob, "Upload exact DSH release artifact")
	if err != nil {
		return err
	}
	uploaded := make([]string, 0, 3)
	for _, line := range strings.Split(workflowScalar(upload.With["path"]), "\n") {
		if path := strings.TrimSpace(line); path != "" {
			uploaded = append(uploaded, path)
		}
	}
	sort.Strings(uploaded)
	wantUploaded := []string{
		"release/dsh/anban-dsh-plugin-${{ steps.version.outputs.VERSION }}.tgz",
		"release/dsh/artifact.sha256.json",
		"release/dsh/pack.json",
	}
	sort.Strings(wantUploaded)
	if strings.Join(uploaded, "\n") != strings.Join(wantUploaded, "\n") {
		return fmt.Errorf("unprivileged package upload files = %v, want only %v", uploaded, wantUploaded)
	}
	if err := requireConsecutiveEnabledSteps(packageJob, []string{
		"Build DSH plugin",
		"Verify DSH plugin source",
		"Pack DSH plugin exactly once",
		"Verify exact DSH package tarball",
		"Smoke-test exact local DSH package",
		"Create exact DSH artifact digest",
		"Upload exact DSH release artifact",
	}); err != nil {
		return err
	}
	packCount := 0
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if stepEnabled(step) && runHasCode(step.Run, "pnpm pack --json --pack-destination") {
				packCount++
			}
		}
	}
	if packCount != 1 {
		return fmt.Errorf("release workflow pack steps = %d, want exactly one across all jobs", packCount)
	}
	desktop, ok := workflow.Jobs["dsh-desktop-acceptance"]
	if !ok || desktop.RunsOn != "${{ matrix.os }}" {
		return fmt.Errorf("release workflow must gate on packaged Desktop acceptance")
	}
	gotOS := append([]string(nil), desktop.Strategy.Matrix["os"]...)
	sort.Strings(gotOS)
	if strings.Join(gotOS, ",") != "macos-latest,windows-latest" {
		return fmt.Errorf("release packaged Desktop OS matrix = %v", gotOS)
	}
	if !workflowJobNeeds(desktop, "dsh-package") {
		return fmt.Errorf("release Desktop acceptance must consume the unprivileged package artifact")
	}
	if err := validateReleasePackagedDSHDesktopJob(desktop); err != nil {
		return err
	}
	for _, step := range desktop.Steps {
		if step.Uses != "" && !regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`).MatchString(step.Uses) {
			return fmt.Errorf("release Desktop action %q is not pinned to an immutable commit", step.Uses)
		}
	}
	release, ok := workflow.Jobs["release"]
	if !ok || release.RunsOn != "ubuntu-latest" {
		return fmt.Errorf("release must be one Ubuntu job")
	}
	if !workflowJobNeeds(release, "dsh-package") || !workflowJobNeeds(release, "dsh-desktop-acceptance") {
		return fmt.Errorf("npm and GitHub release must wait for the exact package and both Desktop matrix jobs")
	}
	if err := requireActionReference(release, "Checkout code", "actions/checkout@"+actionsCheckoutV4SHA); err != nil {
		return err
	}
	if err := requireActionInput(release, "Set up Go", "actions/setup-go@"+actionsSetupGoV5SHA, "go-version", "1.26"); err != nil {
		return err
	}
	if err := requireActionInput(release, "Set up pnpm", "pnpm/action-setup@"+pnpmActionSetupV4SHA, "version", "11.19.0"); err != nil {
		return err
	}
	if err := requireActionInput(release, "Set up Node", "actions/setup-node@"+actionsSetupNodeV4SHA, "node-version", "24"); err != nil {
		return err
	}
	for _, step := range release.Steps {
		if step.Uses != "" && !regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`).MatchString(step.Uses) {
			return fmt.Errorf("release action %q is not pinned to an immutable commit", step.Uses)
		}
	}
	version, err := requireEnabledStep(release, "Validate release version contract")
	if err != nil || !runHasCode(version.Run, "GITHUB_REF_TYPE") || !runHasCode(version.Run, "GITHUB_REF_NAME") || !runHasCode(version.Run, `/^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/`) {
		return fmt.Errorf("release version step must validate a stable X.Y.Z tag context")
	}
	versions, err := requireEnabledStep(release, "Validate every DSH plugin version location and changelog")
	for _, path := range []string{
		"harness/package.json",
		"harness/.claude-plugin/plugin.json",
		"harness/.codex-plugin/plugin.json",
		"harness/.claude-plugin/marketplace.json",
		"harness/CHANGELOG.md",
	} {
		if err != nil || !runHasCode(versions.Run, path) {
			return fmt.Errorf("release version locations step missing executable validation for %s", path)
		}
	}
	if err := requireActionInput(release, "Download exact DSH release artifact", "actions/download-artifact@"+actionsDownloadArtifactV4SHA, "name", "anban-dsh-plugin-release"); err != nil {
		return err
	}
	verify, err := requireEnabledStep(release, "Verify exact DSH release artifact")
	if err != nil || validateReleaseArtifactVerificationStep(verify) != nil {
		return fmt.Errorf("privileged release must verify metadata, digest file, and independent package-job digest at point of use")
	}
	if err := requireConsecutiveEnabledSteps(release, []string{
		"Download exact DSH release artifact",
		"Verify exact DSH release artifact",
		"Stage exact DSH package asset",
	}); err != nil {
		return fmt.Errorf("release artifact download, verification, and staging must be consecutive: %w", err)
	}
	asset, err := requireEnabledStep(release, "Stage exact DSH package asset")
	if err != nil || asset.ID != "asset" || asset.Env["SOURCE_TARBALL"] != "${{ github.workspace }}/release/dsh/anban-dsh-plugin-${{ steps.version.outputs.VERSION }}.tgz" || !runHasCode(asset.Run, "copyFile") || !runHasCode(asset.Run, "createHash('sha256')") || !runHasCode(asset.Run, "GITHUB_OUTPUT") {
		return fmt.Errorf("release asset staging must copy and byte-verify the exact tarball into bin")
	}
	preflight, err := requireEnabledStep(release, "Inspect exact npm registry version")
	if err != nil || preflight.ID != "registry-preflight" || preflight.If != nil || preflight.Env["TARBALL"] != "${{ steps.asset.outputs.tarball }}" || !runHasCode(preflight.Run, "NPM_CONFIG_USERCONFIG") || !runHasCode(preflight.Run, "dist.integrity") || !runHasCode(preflight.Run, "createHash('sha512')") || !runHasCode(preflight.Run, "E404") || !runHasCode(preflight.Run, "publish_required=") || !runHasCode(preflight.Run, "for (let attempt") || validateNpmProvenanceAcceptance(preflight.Run) != nil {
		return fmt.Errorf("registry preflight must briefly retry E404 and require exact existing SRI with provenance")
	}
	publish, err := requireEnabledRunStep(release, "Publish exact DSH package to npm", `npm publish "$TARBALL" --access public --provenance`)
	if err != nil || workflowScalar(publish.If) != "steps.registry-preflight.outputs.publish_required == 'true'" || publish.Env["TARBALL"] != "${{ steps.asset.outputs.tarball }}" || !runHasCode(publish.Run, "publish_status=") || !runHasCode(publish.Run, "for attempt in") || !runHasCode(publish.Run, "dist.integrity") || !runHasCode(publish.Run, "createHash('sha512')") || !runHasCode(publish.Run, `exit "$publish_status"`) || validateNpmProvenanceAcceptance(publish.Run) != nil {
		return fmt.Errorf("npm publication failure must recover only after exact registry SRI with provenance appears")
	}
	view, err := requireEnabledStep(release, "Verify anonymous npm availability")
	if err != nil || view.If != nil || view.Env["VERSION"] != "${{ steps.version.outputs.VERSION }}" || view.Env["TARBALL"] != "${{ steps.asset.outputs.tarball }}" || !runHasCode(view.Run, `npm view "@anban/dsh-plugin@${VERSION}"`) || !runHasCode(view.Run, "dist.integrity") || !runHasCode(view.Run, "createHash('sha512')") || !runHasCode(view.Run, "NPM_CONFIG_USERCONFIG") || !runHasCode(view.Run, "for attempt in") || !runHasCode(view.Run, "sleep ") || validateNpmProvenanceAcceptance(view.Run) != nil {
		return fmt.Errorf("anonymous npm view must retry and verify exact published bytes with provenance")
	}
	registry, err := requireEnabledStep(release, "Smoke-test exact registry DSH package")
	if err != nil || registry.If != nil || registry.Env["VERSION"] != "${{ steps.version.outputs.VERSION }}" || !runHasCode(registry.Run, `smoke-profile.mjs "@anban/dsh-plugin@${VERSION}"`) || !runHasCode(registry.Run, "NPM_CONFIG_USERCONFIG") || !runHasCode(registry.Run, "for attempt in") || !runHasCode(registry.Run, "sleep ") {
		return fmt.Errorf("registry smoke must retry clean installs of the exact anonymous registry spec")
	}
	checksum, err := requireEnabledStep(release, "Generate checksums")
	if err != nil || checksum.If != nil || checksum.Env["TARBALL"] != "${{ steps.asset.outputs.tarball }}" || !runHasCode(checksum.Run, "process.chdir('bin')") || !runHasCode(checksum.Run, "createHash('sha256')") || !runHasCode(checksum.Run, "checksums.txt") || runHasCode(checksum.Run, "relative(") {
		return fmt.Errorf("checksum step must emit flat basename hashes from bin including the exact tarball")
	}
	githubRelease, err := requireEnabledStep(release, "Create Release")
	if err != nil || githubRelease.If != nil || githubRelease.Uses != "" || githubRelease.Env["GH_TOKEN"] != "${{ secrets.GITHUB_TOKEN }}" {
		return fmt.Errorf("GitHub Release step must be an unconditional built-in gh command")
	}
	if err := validateGitHubReleaseAssetStep(githubRelease); err != nil {
		return err
	}
	for _, step := range release.Steps {
		if step.Uses == "oven-sh/setup-bun@v2" || strings.HasPrefix(step.Uses, "softprops/action-gh-release@") {
			return fmt.Errorf("release workflow contains an unused or mutable privileged action %q", step.Uses)
		}
	}
	return requireNamedStepOrder(release, []string{
		"Validate release version contract",
		"Validate every DSH plugin version location and changelog",
		"Build server binaries and Agent package",
		"Download exact DSH release artifact",
		"Verify exact DSH release artifact",
		"Stage exact DSH package asset",
		"Inspect exact npm registry version",
		"Publish exact DSH package to npm",
		"Verify anonymous npm availability",
		"Smoke-test exact registry DSH package",
		"Generate checksums",
		"Create Release",
	})
}

func validateReleasePackagedDSHDesktopJob(job workflowJob) error {
	version, err := requireEnabledStep(job, "Validate release version contract")
	if err != nil || version.ID != "version" || !runHasCode(version.Run, "GITHUB_REF_NAME") || !runHasCode(version.Run, `/^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/`) {
		return fmt.Errorf("release Desktop acceptance must derive and validate a stable release version")
	}
	checkout, err := requireEnabledStep(job, "Checkout pinned DSH Desktop")
	if err != nil || checkout.Uses != "actions/checkout@"+actionsCheckoutV4SHA || workflowScalar(checkout.With["repository"]) != "anywhere-labs/deepseek-harness-desktop" || workflowScalar(checkout.With["ref"]) != dshDesktopCommit || workflowScalar(checkout.With["path"]) != "_dsh-desktop" {
		return fmt.Errorf("release Desktop acceptance must checkout the exact public Desktop commit")
	}
	verify, err := requireEnabledStep(job, "Verify pinned DSH Desktop commit")
	if err != nil || !runHasCode(verify.Run, "rev-parse") || !runHasCode(verify.Run, "HEAD") || !runHasCode(verify.Run, dshDesktopCommit) {
		return fmt.Errorf("release Desktop acceptance must verify the exact checked out commit")
	}
	desktopInstall, err := requireEnabledRunStep(job, "Install pinned DSH Desktop dependencies", "corepack yarn install --immutable")
	if err != nil {
		return err
	}
	if desktopInstall.WorkingDirectory != "_dsh-desktop" {
		return fmt.Errorf("release Desktop dependency install must run from the pinned checkout")
	}
	desktopBuild, err := requireEnabledRunStep(job, "Build unsigned packaged DSH Desktop", "corepack yarn package:dir")
	if err != nil {
		return err
	}
	if desktopBuild.WorkingDirectory != "_dsh-desktop" {
		return fmt.Errorf("release Desktop package build must run from the pinned checkout")
	}
	supervisor, err := requireEnabledStep(job, "Run Desktop supervisor acceptance tests")
	if err != nil || !runHasCode(supervisor.Run, "dsh-process-supervisor.test.mjs") || !runHasCode(supervisor.Run, "dsh-desktop-acceptance.test.mjs") {
		return fmt.Errorf("release Desktop matrix must run real platform process-tree and bounded-loopback acceptance tests")
	}
	if err := requireActionInput(job, "Download exact DSH release artifact", "actions/download-artifact@"+actionsDownloadArtifactV4SHA, "name", "anban-dsh-plugin-release"); err != nil {
		return err
	}
	verifyArtifact, err := requireEnabledStep(job, "Verify exact DSH release artifact")
	if err != nil || validateReleaseArtifactVerificationStep(verifyArtifact) != nil {
		return fmt.Errorf("release Desktop acceptance must verify metadata, digest file, and independent package-job digest at point of use")
	}
	if err := requireConsecutiveEnabledSteps(job, []string{
		"Download exact DSH release artifact",
		"Verify exact DSH release artifact",
		"Install exact plugin through public DSH",
	}); err != nil {
		return fmt.Errorf("Desktop artifact download, verification, and installation must be consecutive: %w", err)
	}
	publicInstall, err := requireEnabledStep(job, "Install exact plugin through public DSH")
	if err != nil || !runHasCode(publicInstall.Run, "dsh-desktop-acceptance.mjs install") || !runHasCode(publicInstall.Run, "anban-dsh-plugin") {
		return fmt.Errorf("release Desktop acceptance must install the exact tarball through public DSH")
	}
	if publicInstall.Env["DSH_PLUGIN_TARBALL"] != "${{ github.workspace }}/release/dsh/anban-dsh-plugin-${{ steps.version.outputs.VERSION }}.tgz" {
		return fmt.Errorf("release Desktop acceptance must install the versioned pack output")
	}
	launch, err := requireEnabledStep(job, "Launch packaged DSH Desktop application")
	if err != nil || !runHasCode(launch.Run, "dsh-desktop-acceptance.mjs launch") {
		return fmt.Errorf("release Desktop acceptance must launch the packaged application")
	}
	profile, err := requireEnabledStep(job, "Validate packaged Desktop profile exports and Skill catalogs")
	if err != nil || !runHasCode(profile.Run, `smoke-profile.mjs --existing-profile desktop "${{ steps.version.outputs.VERSION }}"`) {
		return fmt.Errorf("release Desktop acceptance must validate exports and mounted Skill catalogs")
	}
	for _, step := range job.Steps {
		if strings.Contains(step.Run+step.Env["DSH_PLUGIN_TARBALL"], dshPluginVersion) {
			return fmt.Errorf("release Desktop acceptance must not hardcode the current plugin version")
		}
		for _, forbidden := range []string{"ELECTRON_RUN_AS_NODE", "desktopRuntime", "desktopPnpmBootstrap", "public Desktop runtime fixture"} {
			if runHasCode(step.Run, forbidden) {
				return fmt.Errorf("release packaged Desktop acceptance uses forbidden path %q", forbidden)
			}
		}
	}
	return requireNamedStepOrder(job, []string{
		"Checkout pinned DSH Desktop",
		"Verify pinned DSH Desktop commit",
		"Validate release version contract",
		"Run Desktop supervisor acceptance tests",
		"Install pinned DSH Desktop dependencies",
		"Build unsigned packaged DSH Desktop",
		"Download exact DSH release artifact",
		"Verify exact DSH release artifact",
		"Install exact plugin through public DSH",
		"Launch packaged DSH Desktop application",
		"Validate packaged Desktop profile exports and Skill catalogs",
	})
}

func validateReleaseArtifactVerificationStep(step workflowStep) error {
	if step.Env["TRUSTED_TARBALL_SHA256"] != "${{ needs.dsh-package.outputs.tarball_sha256 }}" {
		return fmt.Errorf("artifact verification must consume the independent package-job digest")
	}
	for _, required := range []string{
		`node scripts/dsh-verify-pack.mjs release/dsh/pack.json "${{ steps.version.outputs.VERSION }}"`,
		`node scripts/dsh-artifact-integrity.mjs verify release/dsh/pack.json "${{ steps.version.outputs.VERSION }}" release/dsh/artifact.sha256.json "$TRUSTED_TARBALL_SHA256"`,
	} {
		if !runHasCode(step.Run, required) {
			return fmt.Errorf("artifact verification is missing %q", required)
		}
	}
	return nil
}

func validateReleasePermissionContract(workflow workflowContract) error {
	if workflow.Permissions["contents"] == "write" || workflow.Permissions["id-token"] == "write" {
		return fmt.Errorf("release workflow must not grant privileged permissions globally")
	}
	desktop, ok := workflow.Jobs["dsh-desktop-acceptance"]
	if !ok || desktop.Permissions["contents"] != "read" || desktop.Permissions["id-token"] != "" {
		return fmt.Errorf("Desktop acceptance must explicitly use contents: read without id-token")
	}
	packageJob, ok := workflow.Jobs["dsh-package"]
	if !ok || packageJob.Permissions["contents"] != "read" || packageJob.Permissions["id-token"] != "" {
		return fmt.Errorf("DSH package job must explicitly use contents: read without id-token")
	}
	release, ok := workflow.Jobs["release"]
	if !ok || release.Permissions["contents"] != "write" || release.Permissions["id-token"] != "write" {
		return fmt.Errorf("release job must own contents and id-token write permissions")
	}
	return nil
}

func validateCrossPlatformDSHPackStep(step workflowStep) error {
	for _, forbidden := range []string{"<<'NODE'", "<<\"NODE\"", "parsePackResult"} {
		if runHasCode(step.Run, forbidden) {
			return fmt.Errorf("Desktop pack step contains shell-specific inline verifier %q", forbidden)
		}
	}
	if !runHasCode(step.Run, "node ../scripts/dsh-verify-pack.mjs ../release/dsh/pack.json "+dshPluginVersion) {
		return fmt.Errorf("Desktop pack step must use the cross-platform exact-pack verifier")
	}
	return nil
}

func validateReleaseCrossPlatformDSHPackStep(step workflowStep) error {
	for _, forbidden := range []string{"<<'NODE'", "<<\"NODE\"", "parsePackResult"} {
		if runHasCode(step.Run, forbidden) {
			return fmt.Errorf("Desktop pack step contains shell-specific inline verifier %q", forbidden)
		}
	}
	if !runHasCode(step.Run, `node ../scripts/dsh-verify-pack.mjs ../release/dsh/pack.json "${{ steps.version.outputs.VERSION }}"`) {
		return fmt.Errorf("Desktop pack step must verify the runtime-derived release version")
	}
	return nil
}

func requireActionInput(job workflowJob, name, uses, key, value string) error {
	step, err := requireEnabledStep(job, name)
	if err != nil {
		return err
	}
	if step.Uses != uses || workflowScalar(step.With[key]) != value {
		return fmt.Errorf("step %q must use %s with %s=%s", name, uses, key, value)
	}
	return nil
}

func requireActionReference(job workflowJob, name, uses string) error {
	step, err := requireEnabledStep(job, name)
	if err != nil {
		return err
	}
	if step.Uses != uses {
		return fmt.Errorf("step %q must use immutable action %s", name, uses)
	}
	return nil
}

func validateGitHubReleaseAssetStep(step workflowStep) error {
	for _, forbidden := range []string{"--clobber", `gh release create "$RELEASE_TAG" --verify-tag`} {
		if runHasCode(step.Run, forbidden) {
			return fmt.Errorf("GitHub Release step contains unsafe mutation %q", forbidden)
		}
	}
	for _, required := range []string{
		"isDraft",
		"isPrerelease",
		"targetCommitish",
		`gh release create "$RELEASE_TAG" "${local_assets[@]}"`,
		"gh release download",
		"createHash('sha256')",
		"missing_assets",
		`gh release upload "$RELEASE_TAG" "${missing_assets[@]}"`,
		"trap cleanup EXIT",
	} {
		if !runHasCode(step.Run, required) {
			return fmt.Errorf("GitHub Release asset step missing %q", required)
		}
	}
	download := strings.Index(step.Run, "gh release download")
	compare := strings.Index(step.Run, "createHash('sha256')")
	upload := strings.Index(step.Run, `gh release upload "$RELEASE_TAG" "${missing_assets[@]}"`)
	if !(download < compare && compare < upload) {
		return fmt.Errorf("GitHub Release assets must be downloaded and compared before missing uploads")
	}
	return nil
}

func requireEnabledRunStep(job workflowJob, name, command string) (workflowStep, error) {
	step, err := requireEnabledStep(job, name)
	if err != nil {
		return workflowStep{}, err
	}
	if !runHasCommand(step.Run, command) {
		return workflowStep{}, fmt.Errorf("enabled step %q must execute %q", name, command)
	}
	return step, nil
}

func requireEnabledStep(job workflowJob, name string) (workflowStep, error) {
	for _, step := range job.Steps {
		if step.Name == name && stepEnabled(step) {
			return step, nil
		}
	}
	return workflowStep{}, fmt.Errorf("enabled workflow step %q not found", name)
}

func requireNamedStepOrder(job workflowJob, names []string) error {
	previous := -1
	for _, name := range names {
		index := -1
		for candidate, step := range job.Steps {
			if step.Name == name && stepEnabled(step) {
				index = candidate
				break
			}
		}
		if index == -1 {
			return fmt.Errorf("enabled workflow step %q not found", name)
		}
		if index <= previous {
			return fmt.Errorf("workflow step %q is out of order", name)
		}
		previous = index
	}
	return nil
}

func requireConsecutiveEnabledSteps(job workflowJob, names []string) error {
	enabledNames := make([]string, 0, len(job.Steps))
	for _, step := range job.Steps {
		if stepEnabled(step) {
			enabledNames = append(enabledNames, step.Name)
		}
	}
	for start := 0; start+len(names) <= len(enabledNames); start++ {
		if slices.Equal(enabledNames[start:start+len(names)], names) {
			return nil
		}
	}
	return fmt.Errorf("enabled workflow steps do not contain consecutive sequence %v", names)
}

func stepEnabled(step workflowStep) bool {
	switch condition := step.If.(type) {
	case nil:
		return true
	case bool:
		return condition
	case string:
		normalized := strings.ToLower(strings.TrimSpace(condition))
		return normalized != "false" && normalized != "${{ false }}"
	default:
		return true
	}
}

func runHasCommand(run, command string) bool {
	for _, line := range strings.Split(run, "\n") {
		if strings.TrimSpace(line) == command {
			return true
		}
	}
	return false
}

func runHasCode(run, fragment string) bool {
	for _, line := range strings.Split(run, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.Contains(line, fragment) {
			return true
		}
	}
	return false
}

const dshReleaseConcurrencyGroup = "dsh-release-${{ github.event_name == 'workflow_dispatch' && format('v{0}', inputs.version) || github.ref_name }}"

func validateDSHReleaseConcurrency(concurrency workflowConcurrency) error {
	if concurrency.Group != dshReleaseConcurrencyGroup {
		return fmt.Errorf("release concurrency must key tag and manual runs to the same release version")
	}
	if concurrency.CancelInProgress == nil || *concurrency.CancelInProgress {
		return fmt.Errorf("release concurrency must explicitly serialize without cancellation")
	}
	return nil
}

func validateNpmProvenanceAcceptance(run string) error {
	if !runHasAllCode(run, "view", "dist.attestations.provenance") {
		return fmt.Errorf("registry acceptance must query npm provenance")
	}
	for _, fragment := range []string{
		"publishedProvenance",
		"published['dist.attestations.provenance']",
		"typeof publishedProvenance !== 'object'",
		"Array.isArray(publishedProvenance)",
		"publishedProvenance.predicateType !== 'https://slsa.dev/provenance/v1'",
	} {
		if !runHasCode(run, fragment) {
			return fmt.Errorf("registry acceptance must fail closed on missing npm provenance")
		}
	}
	return nil
}

func runHasAllCode(run string, fragments ...string) bool {
	for _, line := range strings.Split(run, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func dshBoolPointer(value bool) *bool {
	return &value
}

func workflowScalar(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func workflowJobNeeds(job workflowJob, name string) bool {
	switch needs := job.Needs.(type) {
	case string:
		return needs == name
	case []any:
		for _, candidate := range needs {
			if workflowScalar(candidate) == name {
				return true
			}
		}
	}
	return false
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

func assertGeneratedPresetSkills(t *testing.T, pluginRoot, presetRoot string, want []string) {
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
	for _, skill := range want {
		assertDirectoryBytesEqual(
			t,
			filepath.Join(pluginRoot, "skills", skill),
			filepath.Join(presetRoot, "skills", skill),
		)
	}
}

func assertDirectoryBytesEqual(t *testing.T, canonicalRoot, generatedRoot string) {
	t.Helper()
	canonicalFiles := directoryFilePaths(t, canonicalRoot)
	generatedFiles := directoryFilePaths(t, generatedRoot)
	if strings.Join(canonicalFiles, "\n") != strings.Join(generatedFiles, "\n") {
		t.Errorf("generated Skill %s files = %v, want canonical files %v", filepath.Base(canonicalRoot), generatedFiles, canonicalFiles)
		return
	}
	for _, relativePath := range canonicalFiles {
		canonical, err := os.ReadFile(filepath.Join(canonicalRoot, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read canonical Skill file %s: %v", relativePath, err)
		}
		generated, err := os.ReadFile(filepath.Join(generatedRoot, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read generated Skill file %s: %v", relativePath, err)
		}
		if !bytes.Equal(generated, canonical) {
			t.Errorf("generated Skill file %s/%s differs from its canonical source", filepath.Base(canonicalRoot), relativePath)
		}
	}
}

func directoryFilePaths(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		t.Fatalf("walk directory %s: %v", root, err)
	}
	sort.Strings(files)
	return files
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
