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

func TestDSHWindowsCmdProbeUsesTheRealPlatformGate(t *testing.T) {
	body := readRepoFile(t, filepath.Join(repoRoot(t), "plugins", "dsh", "tests", "profile-smoke.test.ts"))
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

type workflowContract struct {
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Required bool `yaml:"required"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	RunsOn   string `yaml:"runs-on"`
	Defaults struct {
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
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	If   any               `yaml:"if"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
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
	if !ok || check.RunsOn != "ubuntu-latest" || check.Defaults.Run.WorkingDirectory != "plugins" {
		return fmt.Errorf("dsh-plugin must be an Ubuntu job with plugins as its working directory")
	}
	if err := requireActionInput(check, "Set up pnpm", "pnpm/action-setup@v4", "version", "11.19.0"); err != nil {
		return err
	}
	if err := requireActionInput(check, "Set up Node", "actions/setup-node@v4", "node-version", "24"); err != nil {
		return err
	}
	if _, err := requireEnabledRunStep(check, "Install DSH plugin dependencies", "pnpm install --frozen-lockfile"); err != nil {
		return err
	}
	if _, err := requireEnabledRunStep(check, "Check DSH plugin", "pnpm run check"); err != nil {
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
	if !ok || portable.RunsOn != "${{ matrix.os }}" || portable.Defaults.Run.WorkingDirectory != "plugins" {
		return fmt.Errorf("dsh-plugin-portability must run its plugins commands on matrix.os")
	}
	gotOS := append([]string(nil), portable.Strategy.Matrix["os"]...)
	sort.Strings(gotOS)
	if strings.Join(gotOS, ",") != "macos-latest,windows-latest" {
		return fmt.Errorf("DSH portability OS matrix = %v", gotOS)
	}
	if err := requireActionInput(portable, "Set up pnpm", "pnpm/action-setup@v4", "version", "11.19.0"); err != nil {
		return err
	}
	if err := requireActionInput(portable, "Set up Node", "actions/setup-node@v4", "node-version", "24"); err != nil {
		return err
	}
	if _, err := requireEnabledRunStep(portable, "Install locked DSH plugin dependencies", "pnpm install --frozen-lockfile"); err != nil {
		return err
	}
	commandShape, err := requireEnabledStep(portable, "Run portable profile command tests")
	if err != nil || !runHasCode(commandShape.Run, "dsh/tests/profile-smoke.test.ts") || !runHasCode(commandShape.Run, "portable profile-smoke commands") {
		return fmt.Errorf("portable job must run the focused command-shape tests")
	}
	windowsProbe, err := requireEnabledStep(portable, "Execute the Windows cmd shim test")
	if err != nil || windowsProbe.If != nil || !runHasCode(windowsProbe.Run, "executes a cmd shim with spaces and metacharacters through cross-spawn") {
		return fmt.Errorf("portable job must execute the Windows-only it.runIf cmd shim test")
	}
	desktop, err := requireEnabledStep(portable, "Run the official public Desktop runtime fixture")
	if err != nil || desktop.If != nil || !runHasCode(desktop.Run, "honors the pinned DSH Desktop public-runtime contract") {
		return fmt.Errorf("portable job must run the public Desktop runtime fixture on macOS and Windows")
	}
	build, err := requireEnabledRunStep(portable, "Build DSH plugin for real profile smoke", "pnpm run build")
	if err != nil || build.If != nil {
		return fmt.Errorf("portable job must build the package on macOS and Windows before smoke")
	}
	smoke, err := requireEnabledRunStep(portable, "Smoke-test real packaged fresh profile", "pnpm run smoke:profile")
	if err != nil || smoke.If != nil {
		return fmt.Errorf("portable job must run the real profile smoke on macOS and Windows")
	}
	return requireNamedStepOrder(portable, []string{
		"Install locked DSH plugin dependencies",
		"Run portable profile command tests",
		"Execute the Windows cmd shim test",
		"Run the official public Desktop runtime fixture",
		"Build DSH plugin for real profile smoke",
		"Smoke-test real packaged fresh profile",
	})
}

func validateDSHReleaseWorkflow(workflow workflowContract) error {
	if workflow.Permissions["contents"] != "write" || workflow.Permissions["id-token"] != "write" {
		return fmt.Errorf("release workflow must grant contents and id-token write")
	}
	input, ok := workflow.On.WorkflowDispatch.Inputs["version"]
	if !ok || !input.Required {
		return fmt.Errorf("workflow_dispatch version input must be required")
	}
	release, ok := workflow.Jobs["release"]
	if !ok || release.RunsOn != "ubuntu-latest" {
		return fmt.Errorf("release must be one Ubuntu job")
	}
	if err := requireActionInput(release, "Set up pnpm", "pnpm/action-setup@v4", "version", "11.19.0"); err != nil {
		return err
	}
	if err := requireActionInput(release, "Set up Node", "actions/setup-node@v4", "node-version", "24"); err != nil {
		return err
	}
	version, err := requireEnabledStep(release, "Validate release version contract")
	if err != nil || !runHasCode(version.Run, "GITHUB_REF_TYPE") || !runHasCode(version.Run, "GITHUB_REF_NAME") || !runHasCode(version.Run, `/^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/`) {
		return fmt.Errorf("release version step must validate a stable X.Y.Z tag context")
	}
	versions, err := requireEnabledStep(release, "Validate every DSH plugin version location and changelog")
	for _, path := range []string{
		"plugins/package.json",
		"plugins/.claude-plugin/plugin.json",
		"plugins/.codex-plugin/plugin.json",
		"plugins/.claude-plugin/marketplace.json",
		"plugins/CHANGELOG.md",
	} {
		if err != nil || !runHasCode(versions.Run, path) {
			return fmt.Errorf("release version locations step missing executable validation for %s", path)
		}
	}
	if _, err := requireEnabledRunStep(release, "Install DSH plugin dependencies", "pnpm install --frozen-lockfile"); err != nil {
		return err
	}
	if _, err := requireEnabledRunStep(release, "Verify DSH plugin source", "pnpm run verify:source"); err != nil {
		return err
	}
	pack, err := requireEnabledStep(release, "Pack DSH plugin exactly once")
	if err != nil || pack.ID != "pack" || !runHasCode(pack.Run, "pnpm pack --json --pack-destination") || !runHasCode(pack.Run, "parsePackResult") {
		return fmt.Errorf("release must structurally capture one controlled pack result")
	}
	packCount := 0
	for _, step := range release.Steps {
		if stepEnabled(step) && runHasCode(step.Run, "pnpm pack --json --pack-destination") {
			packCount++
		}
	}
	if packCount != 1 {
		return fmt.Errorf("release controlled pack steps = %d, want 1", packCount)
	}
	verify, err := requireEnabledStep(release, "Verify exact DSH package tarball")
	if err != nil || verify.Env["TARBALL"] != "${{ steps.pack.outputs.tarball }}" || verify.Env["PACK_METADATA"] != "${{ steps.pack.outputs.metadata }}" || !runHasCode(verify.Run, "inspectAndExtractArchive") {
		return fmt.Errorf("exact pack verification must consume the pack step outputs")
	}
	localSmoke, err := requireEnabledStep(release, "Smoke-test exact local DSH package")
	if err != nil || localSmoke.Env["TARBALL"] != "${{ steps.pack.outputs.tarball }}" || localSmoke.Env["PACK_METADATA"] != "${{ steps.pack.outputs.metadata }}" || !runHasCode(localSmoke.Run, "smokeProfile") {
		return fmt.Errorf("local profile smoke must consume the exact pack step outputs")
	}
	asset, err := requireEnabledStep(release, "Stage exact DSH package asset")
	if err != nil || asset.ID != "asset" || asset.Env["SOURCE_TARBALL"] != "${{ steps.pack.outputs.tarball }}" || !runHasCode(asset.Run, "copyFile") || !runHasCode(asset.Run, "createHash('sha256')") || !runHasCode(asset.Run, "GITHUB_OUTPUT") {
		return fmt.Errorf("release asset staging must copy and byte-verify the exact tarball into bin")
	}
	preflight, err := requireEnabledStep(release, "Inspect exact npm registry version")
	if err != nil || preflight.ID != "registry-preflight" || preflight.If != nil || preflight.Env["TARBALL"] != "${{ steps.asset.outputs.tarball }}" || !runHasCode(preflight.Run, "NPM_CONFIG_USERCONFIG") || !runHasCode(preflight.Run, "dist.integrity") || !runHasCode(preflight.Run, "createHash('sha512')") || !runHasCode(preflight.Run, "E404") || !runHasCode(preflight.Run, "publish_required=") {
		return fmt.Errorf("registry preflight must distinguish E404 and require exact existing SRI")
	}
	publish, err := requireEnabledRunStep(release, "Publish exact DSH package to npm", `npm publish "$TARBALL" --access public --provenance`)
	if err != nil || workflowScalar(publish.If) != "steps.registry-preflight.outputs.publish_required == 'true'" || publish.Env["TARBALL"] != "${{ steps.asset.outputs.tarball }}" {
		return fmt.Errorf("npm publication must run only after an anonymous E404 and consume the exact asset")
	}
	view, err := requireEnabledStep(release, "Verify anonymous npm availability")
	if err != nil || view.If != nil || view.Env["VERSION"] != "${{ steps.version.outputs.VERSION }}" || view.Env["TARBALL"] != "${{ steps.asset.outputs.tarball }}" || !runHasCode(view.Run, `npm view "@anban/dsh-plugin@${VERSION}"`) || !runHasCode(view.Run, "dist.integrity") || !runHasCode(view.Run, "createHash('sha512')") || !runHasCode(view.Run, "NPM_CONFIG_USERCONFIG") || !runHasCode(view.Run, "for attempt in") || !runHasCode(view.Run, "sleep ") {
		return fmt.Errorf("anonymous npm view must retry and verify the exact published version bytes")
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
	if err != nil || githubRelease.If != nil || githubRelease.Uses != "" || githubRelease.Env["GH_TOKEN"] != "${{ secrets.GITHUB_TOKEN }}" || !runHasCode(githubRelease.Run, "gh release view") || !runHasCode(githubRelease.Run, "gh release create") || !runHasCode(githubRelease.Run, "gh release upload") || !runHasCode(githubRelease.Run, "--clobber") || !runHasCode(githubRelease.Run, "bin/*") {
		return fmt.Errorf("GitHub Release must idempotently create and upload only flat bin assets with gh")
	}
	for _, step := range release.Steps {
		if step.Uses == "oven-sh/setup-bun@v2" || strings.HasPrefix(step.Uses, "softprops/action-gh-release@") {
			return fmt.Errorf("release workflow contains an unused or mutable privileged action %q", step.Uses)
		}
	}
	return requireNamedStepOrder(release, []string{
		"Validate release version contract",
		"Validate every DSH plugin version location and changelog",
		"Install DSH plugin dependencies",
		"Verify DSH plugin source",
		"Pack DSH plugin exactly once",
		"Verify exact DSH package tarball",
		"Smoke-test exact local DSH package",
		"Build server binaries and Agent package",
		"Stage exact DSH package asset",
		"Inspect exact npm registry version",
		"Publish exact DSH package to npm",
		"Verify anonymous npm availability",
		"Smoke-test exact registry DSH package",
		"Generate checksums",
		"Create Release",
	})
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

func workflowScalar(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
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
