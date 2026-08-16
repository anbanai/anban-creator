# DeepSeek Harness Article/Seednote Plugin Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `article` and `seednote` as installable DeepSeek Harness Agent Presets backed by one `@anban/dsh-plugin` Bundle, the existing canonical Skills, and the fixed authenticated Anban MCP endpoint.

**Architecture:** Agent Packs remain the source of truth. The Go generator copies each Pack's explicit `agent.dsh.yml`, emits official Preset metadata, and copies only declared Skill trees into generated Presets. The npm Bundle contributes Host-plane MCP and Preset-management plugins plus an Agent-plane Skill provider export that resolves installed Preset Skills through the official DSH home helper and delegates to official DSH plugins.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3`, TypeScript 6, Node.js 22.19+/24+, pnpm 11.19, Vitest 4, DeepSeek Harness `0.1.0-rc.6`, Cordis `4.0.1`, DSH Desktop `2.0.0` at `4f68147091e585aaa1d815f99d30a657b3842d7c`.

---

## Working Tree And Commit Boundaries

`plugins/` is a Git submodule. Keep these histories separate:

1. Commit DSH Bundle, Preset sources, generated Presets, native manifest bumps, and plugin documentation inside `plugins/`.
2. Commit Go generator/tests, root Make/CI changes, this plan, and the final `plugins` gitlink in the parent repository.

Do not stage, remove, or modify the existing user-owned untracked directories:

```text
claudecode/
codex/
openclaw/
third_party/Agent-Reach/
third_party/Humanizer/
```

The current worktree's `claudecode/` and `codex/` directories make
`TestUnifiedPluginLayout` fail before it reaches version assertions. Run the
full test in a clean checkout for release evidence; in this worktree, report
that unrelated failure without deleting the directories.

## File Map

### Parent repository

- `server/agentpack/types.go`: optional `agent.dsh_source` field.
- `server/agentpack/catalog.go`: secure DSH source validation and Pack digest inclusion.
- `server/agentpack/dsh_contract.go`: DSH composition YAML shape validation.
- `server/agentpack/generate.go`: invoke DSH generation/checking without changing native Agent copying.
- `server/agentpack/dsh_generate.go`: deterministic Preset derivation, Skill copy, stale cleanup, and drift checking.
- `server/agentpack/catalog_test.go`: TDD coverage for optional DSH sources and byte-identical native outputs.
- `server/agent/dsh_plugin_contract_test.go`: repository-level Bundle/Preset/version contract.
- `server/agent/unified_plugin_contract_test.go`: expected native manifest version `4.1.11`.
- `Makefile`: DSH plugin install/check targets.
- `.github/workflows/ci.yml`: recursive submodule checkout and DSH package job.
- `.github/workflows/release.yml`: recursive submodule checkout and built DSH Bundle tarball.
- `docs/superpowers/specs/2026-08-16-dsh-plugin-integration-design.md`: approved design; do not change unless implementation uncovers a contradiction.

### `plugins/` submodule

- `package.json`, `pnpm-lock.yaml`: `@anban/dsh-plugin` package, exact DSH/Cordis peers, scripts, exports, Bundle manifest.
- `tsconfig.json`, `tsconfig.test.json`, `vitest.config.ts`: build and test configuration.
- `.gitignore`: Node modules, generated `dsh/lib`, and local tarballs.
- `dsh/cordis.patch.yml`: Host-plane Bundle rows only.
- `dsh/src/anban-mcp.ts`: credential-driven singleton official MCP child lifecycle.
- `dsh/src/skills-provider.ts`: Bundle-owned Agent-plane wrapper around `dsh-skill-filesystem`.
- `dsh/src/presets.ts`: ownership, digest, atomic install, status, and removal library.
- `dsh/src/preset-manager.ts`: `/anban-presets-*` command registration.
- `dsh/src/cli.ts`, `dsh/bin/anban-dsh.js`: CLI argument parsing and executable entry.
- `dsh/src/safe-error.ts`: secret-safe error rendering shared by Host adapters.
- `dsh/scripts/clean.mjs`: cross-platform generated-build cleanup.
- `dsh/scripts/smoke-profile.mjs`: disposable official DSH profile packaging/config/Preset smoke.
- `dsh/tests/*.test.ts`: unit and package integration tests.
- `packs/article/agent-pack.yaml`, `packs/seednote/agent-pack.yaml`: opt into DSH with `dsh_source`.
- `packs/article/agent.dsh.yml`, `packs/seednote/agent.dsh.yml`: explicit native DSH compositions.
- `dsh/presets/article/**`, `dsh/presets/seednote/**`: generated, never hand-edited.
- `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, `.codex-plugin/plugin.json`: aligned `4.1.11` release metadata.
- `README.md`, `docs/dsh-installation.md`, `docs/plugin-development.md`, `CHANGELOG.md`: installation, limits, development, and release notes.
- `Makefile`: submodule-local DSH install/check/smoke targets.

### Explicitly unchanged

- `server/mcp/**`
- `server/service/**`
- `agent-ts/**`
- `studio/**`
- `plugins/.mcp.json`
- `plugins/install/**`
- existing Claude Markdown and Codex TOML source contents
- managed runtime images and adapters

## Task 1: Add The Optional DSH Agent Pack Contract

**Files:**
- Modify: `server/agentpack/types.go:35-41`
- Modify: `server/agentpack/catalog.go:102-136,258-270`
- Create: `server/agentpack/dsh_contract.go`
- Modify: `server/agentpack/catalog_test.go:72-196,522-585`

- [ ] **Step 1: Write failing tests for optional and invalid `dsh_source`**

Add table-driven cases that prove an omitted field remains valid and that an
absolute path, traversal path, missing file, mapping root, row without `id`,
row without `name`, and duplicate row id are rejected. Use this valid fixture:

```go
const validDSHComposition = `
- id: persona
  name: '@deepseek-ai/dsh-persona'
  config:
    text: Demo
- id: tool-bash
  name: '@deepseek-ai/dsh-tool-bash'
  disabled: !!js process.platform === 'win32'
`
```

Add `agent.dsh.yml` to `writePackFixture` only when the manifest contains
`dsh_source: agent.dsh.yml`, so existing no-DSH fixtures keep testing the
optional path.

- [ ] **Step 2: Run the focused tests and verify red state**

```bash
go test ./server/agentpack -run 'TestLoadCatalog.*DSH|TestLoadCatalogRejectsInvalidDSH' -count=1
```

Expected: FAIL because `AgentSpec` rejects the unknown `dsh_source` field.

- [ ] **Step 3: Add the field and DSH node-shape validator**

Add to `AgentSpec`:

```go
DSHSource string `yaml:"dsh_source" json:"-"`
```

Create `dsh_contract.go` with this public package boundary:

```go
func validateDSHAgentSource(path string, body []byte) error
```

Decode into `yaml.Node`, require one document whose root is a sequence, require
every sequence item to be a mapping, extract scalar string `id` and `name`
keys, reject blank values and duplicate ids, and deliberately leave nested
plugin config uninterpreted so official tags such as `!!js` survive.

- [ ] **Step 4: Validate and hash the optional source**

In `validateManifest`, keep Claude/Codex required and add a separate optional
branch:

```go
if manifest.Agent.DSHSource != "" {
	path, err := securePackFile(manifest.dir, manifest.Agent.DSHSource)
	if err != nil {
		return fmt.Errorf("invalid DSH Agent source %q: %w", manifest.Agent.DSHSource, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read DSH Agent source %q: %w", manifest.Agent.DSHSource, err)
	}
	if err := validateDSHAgentSource(path, body); err != nil {
		return fmt.Errorf("DSH Agent source %q: %w", manifest.Agent.DSHSource, err)
	}
}
```

Append `manifest.Agent.DSHSource` to the ordered source list in
`digestManifest`, skipping it when empty.

- [ ] **Step 5: Run tests and verify green state**

```bash
go test ./server/agentpack -run 'TestLoadCatalog.*DSH|TestLoadCatalogRejectsInvalidDSH|TestLoadCatalogValidatesAndResolvesManagedPack' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the parent contract change**

```bash
git add server/agentpack/types.go server/agentpack/catalog.go server/agentpack/dsh_contract.go server/agentpack/catalog_test.go
git commit -m "feat(agentpack): accept DSH agent sources"
```

## Task 2: Generate And Check DSH Presets Deterministically

**Files:**
- Create: `server/agentpack/dsh_generate.go`
- Modify: `server/agentpack/generate.go:14-127`
- Modify: `server/agentpack/catalog_test.go:171-343,554-596`

- [ ] **Step 1: Write failing generation tests**

Add tests that expect:

```text
generated/dsh/presets/demo/agent.cordis.yml
generated/dsh/presets/demo/preset.yml
generated/dsh/presets/demo/skills/demo-skill/SKILL.md
generated/dsh/presets/demo/skills/demo-skill/scripts/run.sh
```

Assert exact metadata:

```yaml
name: Demo Pack
description: Demo managed workflow
```

Also assert executable source files stay executable, `.git` metadata is not
copied, a second generation reports `Changed == false`, Packs without
`dsh_source` create no Preset, stale Preset directories/files are removed, and
`CheckRepository` reports DSH drift after changing composition, metadata, a
Skill file, or adding an unexpected file.

- [ ] **Step 2: Run the focused generation tests and verify red state**

```bash
go test ./server/agentpack -run 'TestGenerate.*DSH|TestCheckRepositoryDetectsDSH|TestGenerateProducesDeterministicNativeAgentsAndCatalog' -count=1
```

Expected: FAIL because no DSH output tree exists.

- [ ] **Step 3: Implement expected-tree derivation**

Create these focused helpers in `dsh_generate.go`:

```go
type generatedPresetFile struct {
	body []byte
	mode fs.FileMode
}

func expectedDSHPresetFiles(pluginRoot string, catalog *Catalog) (map[string]generatedPresetFile, error)
func generateDSHPresets(pluginRoot, outputRoot string, catalog *Catalog) (bool, error)
func checkDSHPresets(pluginRoot, outputRoot string, catalog *Catalog) error
```

Use slash-normalized relative paths as map keys. Copy `agent.dsh.yml` bytes
without rewriting, render `preset.yml` with `yaml.Marshal` over only `name` and
`description`, walk each declared Skill in manifest order, skip every `.git`
entry, reject symlinks/non-regular files, and normalize output modes to `0644`
or `0755` based on the source executable bits.

- [ ] **Step 4: Implement generated-tree synchronization**

Write expected files with a mode-aware `writeGeneratedFileIfChanged`, then
walk only the generated `dsh/presets` root and remove paths absent from the
expected map. Remove files before empty directories and never follow symlinks.
An empty expected map removes only the generated Preset root, not `dsh/src`,
`dsh/bin`, or `dsh/cordis.patch.yml`.

- [ ] **Step 5: Wire generation and checking without changing native copy logic**

Change `generateTo` to receive `dshPresetsDir`, call `LoadCatalog` once, run the
existing Claude/Codex loop unchanged, then call `generateDSHPresets`. Use:

```go
filepath.Join(outputRoot, "dsh", "presets")
```

for `Generate`, and:

```go
filepath.Join(pluginRoot, "dsh", "presets")
```

for repository generation/checking.

- [ ] **Step 6: Run focused and full Agent Pack tests**

```bash
go test ./server/agentpack -count=1
```

Expected: PASS, including the existing exact Claude Markdown and Codex TOML assertions.

- [ ] **Step 7: Commit the generator**

```bash
git add server/agentpack/generate.go server/agentpack/dsh_generate.go server/agentpack/catalog_test.go
git commit -m "feat(agentpack): generate DSH presets"
```

## Task 3: Add Article And Seednote DSH Compositions

**Files:**
- Modify: `plugins/packs/article/agent-pack.yaml:6-12`
- Create: `plugins/packs/article/agent.dsh.yml`
- Modify: `plugins/packs/seednote/agent-pack.yaml:6-12`
- Create: `plugins/packs/seednote/agent.dsh.yml`
- Generate: `plugins/dsh/presets/article/**`
- Generate: `plugins/dsh/presets/seednote/**`
- Generate: `plugins/agents/*`, `plugins/agent-pack-catalog.json`, `server/agentpack/catalog.generated.json` only through `make agent-pack-generate`

- [ ] **Step 1: Record native Agent checksums before editing**

```bash
find plugins/agents -maxdepth 1 -type f \( -name '*.md' -o -name '*.toml' \) -print0 | sort -z | xargs -0 shasum -a 256 > /tmp/anban-native-agents.before
```

Expected: 12 checksum lines.

- [ ] **Step 2: Opt only Article and Seednote into DSH**

Add this key after `codex_source` in each selected Pack:

```yaml
dsh_source: agent.dsh.yml
```

Do not add it to Montage, ecommerce, Moments, or live-slicer, and do not change
Pack business bindings, runtime profiles, versions, surfaces, billing, or artifacts.

- [ ] **Step 3: Create the official minimal capability composition in both sources**

Each file must use this row order, changing only the persona text, `presetId`,
and `providerName`:

```yaml
- id: persona
  name: '@deepseek-ai/dsh-persona'
  config:
    text: |-
      You are running as an Anban workflow inside DeepSeek Harness.
      Load the named Skills before following their procedures.
      Use DSH filesystem and shell tools for local work.
      Anban MCP tools are registered globally under the mcp__creator__ namespace.
      Never replace a missing MCP tool with HTTP code.
      TASK_ID and managed-runtime files exist only when the initiating context supplied them.

- id: agent-instructions
  name: '@deepseek-ai/dsh-agent-instructions'
  config:
    maxBytes: 65536

- id: tool-bash
  name: '@deepseek-ai/dsh-tool-bash'
  disabled: !!js process.platform === 'win32'

- id: tool-pwsh
  name: '@deepseek-ai/dsh-tool-pwsh'
  disabled: !!js process.platform !== 'win32'

- id: tool-fs
  name: '@deepseek-ai/dsh-tool-fs'

- id: tool-fs-search
  name: '@deepseek-ai/dsh-tool-fs-search'
  config:
    sampleOverCapGlobResults: false

- id: anban-skills
  name: '@anban/dsh-plugin/skills-provider'
  config:
    presetId: article
    providerName: anban-article

- id: tool-skill
  name: '@deepseek-ai/dsh-tool-skill'

- id: tool-todo
  name: '@deepseek-ai/dsh-tool-todo'
  config:
    allowParallelInProgress: false
```

For Seednote, use `presetId: seednote` and `providerName: anban-seednote`.
Do not mount `@deepseek-ai/dsh-skill-filesystem` directly and do not create a
second MCP row in either Preset.

- [ ] **Step 4: Build each full persona from its canonical workflow**

Copy the complete Markdown body after Claude frontmatter from the matching
`agent.claude.md` into the YAML literal, preserving business sequencing and
all output contracts. Apply only these host adaptations:

```text
Claude Code built-in MCP wording -> DSH global mcp__creator__* tools
bare MCP example list_projects -> resolve as mcp__creator__list_projects
TaskCreate/TaskUpdate bookkeeping -> todo_write bookkeeping
Read/Write/Edit references -> DSH filesystem tools
Bash references -> DSH Bash/Pwsh tools
AskUserQuestion references -> ask_user wording only where interaction is allowed
```

Prepend this exact shared adapter contract to both personas:

```text
You are running as an Anban workflow inside DeepSeek Harness.
Load the named Skills before following their procedures.
Use DSH filesystem and shell tools for local work.
Anban MCP tools are registered globally as mcp__creator__TOOL_NAME; never replace a missing MCP tool with HTTP code.
TASK_ID, managed attachment indexes, and managed output assumptions exist only when the initiating context supplied them.
When a required task or execution context is absent, preserve the MCP error, write only locally valid diagnostics, and do not invent ids or claim remote completion.
```

Do not fix current task-id-dependent business behavior in these files.

- [ ] **Step 5: Generate Presets and verify native Agents stayed byte-identical**

```bash
make agent-pack-generate
make agent-pack-check
find plugins/agents -maxdepth 1 -type f \( -name '*.md' -o -name '*.toml' \) -print0 | sort -z | xargs -0 shasum -a 256 > /tmp/anban-native-agents.after
diff -u /tmp/anban-native-agents.before /tmp/anban-native-agents.after
```

Expected: generation/check PASS; checksum diff has no output; generated Article
contains exactly its eight declared Skill directories and Seednote exactly its four.

- [ ] **Step 6: Commit the Pack and generated assets in the submodule**

```bash
git -C plugins add packs/article/agent-pack.yaml packs/article/agent.dsh.yml packs/seednote/agent-pack.yaml packs/seednote/agent.dsh.yml dsh/presets agent-pack-catalog.json
git add server/agentpack/catalog.generated.json
git -C plugins commit -m "feat: add Article and Seednote DSH presets"
git commit -m "build(agentpack): record DSH preset catalog"
```

## Task 4: Scaffold The DSH npm Bundle And Build Contract

**Files:**
- Modify: `plugins/.gitignore`
- Create: `plugins/package.json`
- Create: `plugins/pnpm-lock.yaml`
- Create: `plugins/tsconfig.json`
- Create: `plugins/tsconfig.test.json`
- Create: `plugins/vitest.config.ts`
- Create: `plugins/dsh/scripts/clean.mjs`
- Create: `plugins/dsh/cordis.patch.yml`
- Create: `plugins/dsh/bin/anban-dsh.js`
- Create: `plugins/dsh/tests/package.test.ts`

- [ ] **Step 1: Write the package manifest test first**

Require package name/version/type, Node engine, exact DSH/Cordis peer versions,
the Bundle patch path, CLI, strict files allowlist, and these exports:

```ts
expect(pkg.exports).toEqual({
  './anban-mcp': {
    types: './dsh/lib/anban-mcp.d.ts',
    default: './dsh/lib/anban-mcp.js',
  },
  './preset-manager': {
    types: './dsh/lib/preset-manager.d.ts',
    default: './dsh/lib/preset-manager.js',
  },
  './skills-provider': {
    types: './dsh/lib/skills-provider.d.ts',
    default: './dsh/lib/skills-provider.js',
  },
  './package.json': './package.json',
})
```

- [ ] **Step 2: Run the package test and verify red state**

```bash
cd plugins && pnpm dlx vitest@4.1.8 run dsh/tests/package.test.ts
```

Expected: FAIL because no package manifest/test toolchain exists.

- [ ] **Step 3: Create `package.json` with exact host contracts**

Use version `4.1.11`, `packageManager: pnpm@11.19.0`, and:

```json
"dsh": {"bundle": {"patch": "./dsh/cordis.patch.yml"}},
"bin": {"anban-dsh": "./dsh/bin/anban-dsh.js"},
"scripts": {
  "clean": "node dsh/scripts/clean.mjs",
  "build": "pnpm run clean && tsc -p tsconfig.json",
  "typecheck": "tsc -p tsconfig.json --noEmit && tsc -p tsconfig.test.json --noEmit",
  "test": "vitest run",
  "smoke:profile": "pnpm run build && node dsh/scripts/smoke-profile.mjs",
  "check": "pnpm run typecheck && pnpm run test && pnpm run build && pnpm pack --dry-run"
}
```

Use this exact package allowlist:

```json
"files": [
  "dsh/cordis.patch.yml",
  "dsh/lib/**/*.js",
  "dsh/lib/**/*.d.ts",
  "dsh/bin/anban-dsh.js",
  "dsh/presets/**",
  "docs/dsh-installation.md",
  "README.md",
  "CHANGELOG.md",
  "LICENSE"
]
```

Declare exact peer and dev versions for `@deepseek-ai/cordis` `4.0.1` and the
five directly consumed DSH packages (`dsh-commands`, `dsh-credentials`,
`dsh-home-paths`, `dsh-mcp-client`, `dsh-skill-filesystem`) at `0.1.0-rc.6`.
Add `@deepseek-ai/dsh` `0.1.0-rc.6`, `@types/node` `22.20.0`, TypeScript
`6.0.3`, and Vitest `4.1.8` as dev dependencies.

- [ ] **Step 4: Create build configuration and patch**

Compile `dsh/src/**/*.ts` as strict NodeNext ESM into `dsh/lib` with declarations.
The Bundle patch must contain only:

```yaml
- insert:
    - id: anban-mcp
      name: '@anban/dsh-plugin/anban-mcp'
    - id: anban-preset-manager
      name: '@anban/dsh-plugin/preset-manager'
```

The CLI file is a checked-in JavaScript shim with a Node shebang that imports
`runCLI` from `../lib/cli.js`, catches errors, writes one line to stderr, and
sets `process.exitCode = 1`.

- [ ] **Step 5: Install dependencies and verify package contract**

```bash
cd plugins && pnpm install
pnpm exec vitest run dsh/tests/package.test.ts
```

Expected: package test PASS. Build/typecheck entry points are added and verified
as the source modules land in Tasks 5-9.

- [ ] **Step 6: Commit the Bundle scaffold in the submodule**

```bash
git -C plugins add .gitignore package.json pnpm-lock.yaml tsconfig.json tsconfig.test.json vitest.config.ts dsh/cordis.patch.yml dsh/bin/anban-dsh.js dsh/scripts/clean.mjs dsh/tests/package.test.ts
git -C plugins commit -m "build: scaffold DSH bundle package"
```

## Task 5: Implement Safe Preset Ownership And Atomic Installation

**Files:**
- Create: `plugins/dsh/src/presets.ts`
- Create: `plugins/dsh/tests/presets.test.ts`

- [ ] **Step 1: Write failing ownership/install/status/remove tests**

Cover fresh install, identical no-op, changed source upgrade, unowned conflict,
modified owned conflict, `force` replacement, interrupted copy cleanup, safe
removal, refusal to remove unowned directories, symlink rejection, and invalid
Preset ids. Use a temporary `DSH_HOME` and a fixture source root; never touch
the real home.

The ownership file is exactly:

```text
.anban-dsh-preset.json
```

with this shape:

```ts
interface PresetOwnership {
  schemaVersion: 1
  packageName: '@anban/dsh-plugin'
  packageVersion: string
  presetId: string
  sourceDigest: string
}
```

- [ ] **Step 2: Run the tests and verify red state**

```bash
cd plugins && pnpm exec vitest run dsh/tests/presets.test.ts
```

Expected: FAIL because `dsh/src/presets.ts` does not exist.

- [ ] **Step 3: Implement the public management API**

Export these exact functions and types:

```ts
export const PRESET_IDS = ['article', 'seednote'] as const
export type PresetId = (typeof PRESET_IDS)[number]

export interface InstallOptions {
  dshHome?: string
  force?: boolean
}

export interface PresetStatus {
  id: PresetId
  state: 'absent' | 'current' | 'outdated' | 'modified' | 'unowned'
  sourceDigest: string
  installedDigest?: string
  installedVersion?: string
}

export async function installPresets(options?: InstallOptions): Promise<PresetStatus[]>
export async function statusPresets(options?: Pick<InstallOptions, 'dshHome'>): Promise<PresetStatus[]>
export async function removePresets(options?: Pick<InstallOptions, 'dshHome'>): Promise<PresetId[]>
```

Resolve the default home with `resolveDshHome`, address destinations under
`.agent-presets`, derive sources from `new URL('../presets/', import.meta.url)`,
and read the package version from `../../package.json` with JSON import attributes
or a small filesystem reader.

- [ ] **Step 4: Implement deterministic digests and containment**

Hash sorted slash-normalized relative path, mode class (`file` or `executable`),
NUL separators, and file bytes. Exclude only the ownership file from installed
digests. Reject symlinks and non-regular files in both source and destination
inspection. Validate ids against:

```ts
/^[a-z0-9][a-z0-9-]{0,63}$/
```

- [ ] **Step 5: Implement recoverable atomic replacement**

Copy to a sibling temporary directory, write ownership last, then rename. For
forced replacement, rename the current destination to a sibling backup, rename
the completed temporary directory into place, remove the backup, and restore
the backup if the second rename fails. Always remove abandoned temp/backup paths
owned by the current operation in `finally` blocks.

- [ ] **Step 6: Run tests and commit**

```bash
cd plugins && pnpm exec vitest run dsh/tests/presets.test.ts
```

Expected: PASS.

```bash
git -C plugins add dsh/src/presets.ts dsh/tests/presets.test.ts
git -C plugins commit -m "feat: manage installed DSH presets"
```

## Task 6: Add CLI And Official DSH Commands

**Files:**
- Create: `plugins/dsh/src/cli.ts`
- Create: `plugins/dsh/src/preset-manager.ts`
- Create: `plugins/dsh/tests/cli.test.ts`
- Create: `plugins/dsh/tests/preset-manager.test.ts`

- [ ] **Step 1: Write failing CLI parsing tests**

Require only these forms:

```text
anban-dsh install-presets
anban-dsh install-presets --force
anban-dsh status
anban-dsh remove-presets
```

Unknown commands/options return exit code `2`; operational failures return `1`;
success returns `0`. Output must never contain credentials.

- [ ] **Step 2: Write failing command-registration tests**

Using a fake `ctx.commands`, assert exact global commands:

```text
anban-presets-install   input hint: [force]
anban-presets-status    no input
anban-presets-remove    input hint: confirm
```

Install accepts only blank input or `force`; removal succeeds only for
`confirm`; every handler returns `{kind, text}` directly and does not call an
Agent/model API.

- [ ] **Step 3: Run tests and verify red state**

```bash
cd plugins && pnpm exec vitest run dsh/tests/cli.test.ts dsh/tests/preset-manager.test.ts
```

Expected: FAIL because the entry points do not exist.

- [ ] **Step 4: Implement CLI and command adapters over the shared library**

Export:

```ts
export async function runCLI(
  argv: readonly string[],
  io: Pick<Console, 'log' | 'error'> = console,
): Promise<number>
```

From `preset-manager.ts`, export Cordis plugin metadata:

```ts
export const name = 'anban-preset-manager'
export const inject = ['commands']
export function apply(ctx: Context): void
```

Register each returned disposer through `ctx.effect`. Format status as one
stable line per Preset: id, state, source digest prefix, installed digest prefix,
and installed version when present.

- [ ] **Step 5: Run tests and commit**

```bash
cd plugins && pnpm exec vitest run dsh/tests/cli.test.ts dsh/tests/preset-manager.test.ts
```

Expected: PASS.

```bash
git -C plugins add dsh/src/cli.ts dsh/src/preset-manager.ts dsh/tests/cli.test.ts dsh/tests/preset-manager.test.ts
git -C plugins commit -m "feat: expose DSH preset commands"
```

## Task 7: Add The Bundle-Owned Skill Provider Export

**Files:**
- Create: `plugins/dsh/src/skills-provider.ts`
- Create: `plugins/dsh/tests/skills-provider.test.ts`

- [ ] **Step 1: Write failing provider tests**

Mock `@deepseek-ai/dsh-skill-filesystem` and assert that valid Article config
mounts one official child with:

```ts
{
  providerName: 'anban-article',
  includeDefaultRoots: false,
  customSkillDirs: [dshHomePath('.agent-presets', 'article', 'skills')],
}
```

Assert invalid ids, path separators, blank provider names, and unknown config
keys fail before mounting. Assert disposal awaits the child fiber's `dispose()`.

- [ ] **Step 2: Run the test and verify red state**

```bash
cd plugins && pnpm exec vitest run dsh/tests/skills-provider.test.ts
```

Expected: FAIL because the exported plugin does not exist.

- [ ] **Step 3: Implement the narrow wrapper**

Use this interface and lifecycle:

```ts
export interface Config {
  presetId: string
  providerName: string
}

export const name = 'anban-skills-provider'

export async function apply(ctx: Context, config: Config): Promise<() => Promise<void>> {
  const resolved = validateConfig(config)
  const child = ctx.plugin(skillFilesystem, {
    providerName: resolved.providerName,
    includeDefaultRoots: false,
    customSkillDirs: [dshHomePath('.agent-presets', resolved.presetId, 'skills')],
  })
  await child
  return () => child.dispose()
}
```

Do not inspect, parse, watch, or register Skills directly.

- [ ] **Step 4: Run tests, build, and verify package export resolution**

```bash
cd plugins && pnpm exec vitest run dsh/tests/skills-provider.test.ts
pnpm run build
node -e "import('@anban/dsh-plugin/skills-provider').then(m => console.log(m.name))"
```

Expected: tests PASS; import prints `anban-skills-provider` when run through the
package self-reference.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/src/skills-provider.ts dsh/tests/skills-provider.test.ts
git -C plugins commit -m "feat: load Preset Skills through DSH"
```

## Task 8: Add Credential-Safe Singleton MCP Lifecycle

**Files:**
- Create: `plugins/dsh/src/safe-error.ts`
- Create: `plugins/dsh/src/anban-mcp.ts`
- Create: `plugins/dsh/tests/anban-mcp.test.ts`

- [ ] **Step 1: Write failing lifecycle tests**

Use a fake Context and mocked `@deepseek-ai/dsh-mcp-client`. Cover:

- absent `ANBAN_API_KEY`: zero children, one setup warning, plugin remains active;
- present key: exactly one child with fixed endpoint and bearer header;
- exact config values `streamable-http`, `creator`, `900000`, and `false`;
- unrelated `credentials/updated` events do nothing;
- matching updates dispose before remount and serialize rapid updates;
- unset credential removes the child and tools;
- plugin disposal removes the listener, waits queued refreshes, then disposes the child;
- fake secrets are absent from logs and rendered thrown errors;
- duplicate `creator` child startup failure is rethrown without renaming the namespace.

- [ ] **Step 2: Run the test and verify red state**

```bash
cd plugins && pnpm exec vitest run dsh/tests/anban-mcp.test.ts
```

Expected: FAIL because the Host adapter does not exist.

- [ ] **Step 3: Implement fixed constants and safe rendering**

Use only:

```ts
const API_KEY_REF = credentialRef('ANBAN_API_KEY')
const MCP_URL = 'https://creator.anbanai.com/mcp'
const SERVER_NAME = 'creator'
const TOOL_CALL_TIMEOUT_MS = 900_000
```

`safe-error.ts` must convert unknown errors to one line, replace the resolved
secret and full `Authorization` value with `[REDACTED]`, and never stringify
the child config object.

- [ ] **Step 4: Implement serialized reconciliation**

Export:

```ts
export const name = 'anban-mcp'
export const inject = ['credentials']
export async function apply(ctx: Context): Promise<() => Promise<void>>
```

Resolve through `ctx.credentials.resolve(API_KEY_REF)`. When present, mount:

```ts
const child = ctx.plugin(mcpClient, {
  transport: 'streamable-http',
  serverName: SERVER_NAME,
  url: MCP_URL,
  headers: {Authorization: `Bearer ${resolved.value}`},
  toolCallTimeoutMs: TOOL_CALL_TIMEOUT_MS,
  failOnStartupError: false,
})
await child
```

Maintain one Promise chain for refreshes. Always await old-child disposal
before resolving/mounting the next value. Reset the one-warning guard only
after a configured credential successfully mounts.

- [ ] **Step 5: Run tests and commit**

```bash
cd plugins && pnpm exec vitest run dsh/tests/anban-mcp.test.ts
```

Expected: PASS.

```bash
git -C plugins add dsh/src/safe-error.ts dsh/src/anban-mcp.ts dsh/tests/anban-mcp.test.ts
git -C plugins commit -m "feat: connect DSH to Anban MCP"
```

## Task 9: Add Official DSH Profile And Preset Smoke Coverage

**Files:**
- Create: `plugins/dsh/scripts/smoke-profile.mjs`
- Create: `plugins/dsh/tests/profile-smoke.test.ts`
- Modify: `plugins/package.json`

- [ ] **Step 1: Write the smoke harness contract test**

Test that the script uses a temporary `DSH_HOME`, packages the current Bundle,
installs it through `dsh plugin --profile web add packTarball`, invokes the built
CLI to install Presets, runs `dsh --profile web --dump-config`, and cleans its
temporary directory. The test may mock subprocesses; it must assert exact argv,
not shell strings.

- [ ] **Step 2: Implement the disposable profile smoke**

The real script must:

1. require a completed `dsh/lib` build;
2. create one temporary root named `smokeRoot` with `fs.mkdtemp`;
3. run `pnpm pack --pack-destination smokeRoot` and capture `packTarball`;
4. run the locally installed DSH binary at `node_modules/.bin/dsh` with
   `plugin --profile web add packTarball` and
   `DSH_HOME=path.join(smokeRoot, 'home')`;
5. run the tarball-installed `anban-dsh install-presets` binary from the profile;
6. call official `discoverPresets` on
   `path.join(smokeRoot, 'home', '.agent-presets')` and require healthy
   `article` and `seednote` rows;
7. run `dsh --profile web --dump-config` and require exactly one `anban-mcp` row,
   one `anban-preset-manager` row, and no Preset-local MCP row;
8. dynamically import `@anban/dsh-plugin/skills-provider` from the installed
   profile to prove the package subpath and its peer imports resolve;
9. remove the temporary root in `finally`.

At this point add the package lifecycle scripts, now that every exported source
entry exists:

```json
"prepare": "pnpm run build"
```

`prepare` is sufficient for both Git-source installation and normal packing;
do not add a duplicate `prepack` build.

- [ ] **Step 3: Run the official profile smoke**

```bash
cd plugins && pnpm run smoke:profile
```

Expected: PASS with summary lines for Bundle rows, healthy Presets, and export resolution.

- [ ] **Step 4: Commit**

```bash
git -C plugins add package.json pnpm-lock.yaml dsh/scripts/smoke-profile.mjs dsh/tests/profile-smoke.test.ts
git -C plugins commit -m "test: smoke DSH profile installation"
```

## Task 10: Add Repository Contracts, Commands, CI, And Release Metadata

**Files:**
- Create: `server/agent/dsh_plugin_contract_test.go`
- Modify: `server/agent/unified_plugin_contract_test.go:62-67`
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `plugins/Makefile`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.claude-plugin/marketplace.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `plugins/README.md`
- Create: `plugins/docs/dsh-installation.md`
- Modify: `plugins/docs/plugin-development.md`
- Modify: `plugins/CHANGELOG.md`

- [ ] **Step 1: Write failing parent repository contracts**

Require:

- npm, Claude, Claude marketplace, and Codex versions all equal `4.1.11`;
- Bundle manifest patch is `./dsh/cordis.patch.yml`;
- patch has exactly the two Host rows;
- only Article and Seednote declare `dsh_source`;
- their sources use `@anban/dsh-plugin/skills-provider` exactly once;
- no generated Preset contains `skills-provider.mjs`, `.mcp.json`, or another
  `mcp-client` row;
- generated Skill directory names exactly match each Pack's `agent.skills`;
- package and Presets contain no literal secret value, serialized Authorization
  header, `create_task`, or configurable MCP endpoint; the literal credential
  reference `ANBAN_API_KEY` and the in-memory `Bearer ${resolved.value}`
  construction remain required.

- [ ] **Step 2: Run the contracts and verify red state**

```bash
go test ./server/agent -run 'TestDSHPluginContract|TestUnifiedPluginLayout' -count=1
```

Expected: DSH contract FAIL until metadata/docs/targets are complete. In this
worktree, `TestUnifiedPluginLayout` also reports the unrelated untracked legacy directories.

- [ ] **Step 3: Add Make targets**

In `plugins/Makefile` add:

```make
.PHONY: dsh-install dsh-check dsh-smoke

dsh-install:
	pnpm install --frozen-lockfile

dsh-check:
	pnpm run check

dsh-smoke:
	pnpm run smoke:profile
```

In the root `Makefile`, delegate as `dsh-plugin-install`, `dsh-plugin-check`,
and `dsh-plugin-smoke`, and list them in `help`.

- [ ] **Step 4: Add the CI job and recursive submodules**

Set `submodules: recursive` on every checkout that runs repository tests. Add a
`dsh-plugin` job using Node 24 and `pnpm/action-setup@v4` with version `11.19.0`,
then run:

```bash
make dsh-plugin-install
make dsh-plugin-check
make dsh-plugin-smoke
```

Do not put a production Anban credential in CI and do not make live MCP calls there.

In `release.yml`, use the same recursive checkout and pnpm setup, run
`pnpm install --frozen-lockfile && pnpm pack --pack-destination ../bin` from
`plugins/`, and add `bin/anban-dsh-plugin-*.tgz` to the GitHub Release files.
Do not add npm publication or an npm token in this milestone.

- [ ] **Step 5: Publish aligned release metadata and documentation**

Set all three native/marketplace versions to `4.1.11`. Add a changelog entry
stating that DSH Web/Desktop now support generated Article and Seednote Presets,
official credential/MCP/Skill adapters, and no Server business changes.

Document exact installation:

```bash
dsh plugin --profile web add @anban/dsh-plugin
anban-dsh install-presets
```

For Desktop, use the active-profile terminal:

```bash
dsh plugin add @anban/dsh-plugin
anban-dsh install-presets
```

Then restart Desktop. Document `ANBAN_API_KEY` as the only credential reference,
the fixed endpoint, `status`/removal commands, the `pnpm approve-builds` step for
Git-source installs, and the current limitation that task-context-dependent MCP
operations may fail until a valid task/execution context is supplied.

- [ ] **Step 6: Run contracts and commit both histories**

```bash
go test ./server/agent -run 'TestDSHPluginContract' -count=1
git -C plugins add Makefile .claude-plugin/plugin.json .claude-plugin/marketplace.json .codex-plugin/plugin.json README.md docs/dsh-installation.md docs/plugin-development.md CHANGELOG.md
git -C plugins commit -m "docs: publish DSH plugin integration"
git add server/agent/dsh_plugin_contract_test.go server/agent/unified_plugin_contract_test.go Makefile .github/workflows/ci.yml .github/workflows/release.yml
git commit -m "test: enforce DSH plugin distribution"
```

Expected: DSH contract PASS.

## Task 11: Verify Web, Desktop, And Live MCP Behavior

**Files:**
- No product code changes expected.
- Record commands/results in the implementation handoff; do not commit credentials or screenshots containing secrets.

- [ ] **Step 1: Run full local package and generator verification**

```bash
make agent-pack-generate
make agent-pack-check
make dsh-plugin-check
make dsh-plugin-smoke
go test ./server/agentpack ./server/mcp -count=1
go test ./...
go build -o /tmp/anban-creator-server ./server
```

Expected: changed-surface commands PASS. Full Go tests PASS in a clean checkout;
the current worktree may retain the documented unrelated legacy-directory failure.

- [ ] **Step 2: Pack and install into a disposable Web profile**

```bash
cd plugins
pnpm pack --pack-destination /tmp
export DSH_HOME="$(mktemp -d /tmp/anban-dsh-web.XXXXXX)"
dsh plugin --profile web add /tmp/anban-dsh-plugin-4.1.11.tgz
anban-dsh install-presets
dsh --profile web --dump-config
```

Expected: dump contains one Host `anban-mcp` and one `anban-preset-manager` row;
official Preset discovery shows healthy `article` and `seednote`.

- [ ] **Step 3: Perform the live MCP smoke with a user-supplied development key**

Set `ANBAN_API_KEY` only in the shell environment used to start DSH. Start Web,
select either Anban Preset, and verify the tool catalog contains
`mcp__creator__list_projects` and `mcp__creator__get_project_profile`. Call both
with a development account. Repeat startup with the key missing and invalid.

Expected: valid key calls succeed; missing/invalid keys do not prevent DSH boot;
logs contain no key/header. Do not test `create_task`, image generation, billing,
publishing, or task mutation in this milestone.

- [ ] **Step 4: Verify pinned Desktop compatibility**

```bash
git clone https://github.com/anywhere-labs/deepseek-harness-desktop.git /tmp/deepseek-harness-desktop
git -C /tmp/deepseek-harness-desktop checkout 4f68147091e585aaa1d815f99d30a657b3842d7c
cd /tmp/deepseek-harness-desktop
corepack yarn install --immutable
corepack yarn package:dir
```

Launch the packaged Desktop, open its active-profile terminal, install the same
tarball with `dsh plugin add`, run `anban-dsh install-presets`, restart Desktop,
and select both Presets. In the Desktop command surface run
`/anban-presets-status`, then verify their Skill catalogs are isolated to the
copied Pack Skills and the same singleton `mcp__creator__*` tools appear. Do not
import Desktop-private services.

- [ ] **Step 5: Inspect final diffs and submodule state**

```bash
git -C plugins status --short
git status --short
git diff --submodule=log --check
```

Expected: `plugins/` clean; parent shows only intentional root changes plus the
updated `plugins` gitlink and pre-existing untracked directories.

## Task 12: Record The Final Plugin Gitlink And Integration Result

**Files:**
- Modify: `plugins` gitlink in the parent repository.

- [ ] **Step 1: Confirm the child history contains all DSH commits**

```bash
git -C plugins log --oneline --decorate -8
git -C plugins status --short
```

Expected: the submodule is clean and its latest commits cover Presets, Bundle,
management, Skills, MCP, smoke tests, and documentation.

- [ ] **Step 2: Commit the parent gitlink**

```bash
git add plugins
git commit -m "feat: integrate DSH Article and Seednote plugins"
```

- [ ] **Step 3: Run final non-secret evidence commands**

```bash
git status --short
git log --oneline -8
git -C plugins rev-parse HEAD
make agent-pack-check
```

Expected: only the pre-existing untracked directories remain; Pack check PASS;
the final handoff reports parent and submodule commit ids, package tarball name,
Web/Desktop smoke outcome, live MCP outcome, and any unrelated full-test failure.

## Acceptance Checklist

- [ ] `@anban/dsh-plugin@4.1.11` packs with built Host adapters and `./skills-provider`.
- [ ] Official DSH Web profile composition contains one Anban MCP adapter and one Preset manager.
- [ ] Article and Seednote install into `$DSH_HOME/.agent-presets` and are healthy/selectable.
- [ ] Each Preset exposes exactly its Pack-declared Skills through the Bundle-owned provider.
- [ ] `ANBAN_API_KEY` is resolved only through `ctx.credentials`.
- [ ] One official `creator` MCP child produces `mcp__creator__*` tools.
- [ ] Missing, invalid, unset, and updated credentials follow the approved lifecycle without leaking secrets.
- [ ] Claude Markdown and Codex TOML generated Agents are byte-identical to their Pack sources.
- [ ] No Server business behavior, `agent-ts`, managed runtime, `create_task`, or `generate_image/taskId` behavior changed.
- [ ] DSH Desktop `2.0.0` pinned commit works with the same Bundle and Presets.
- [ ] Child submodule commits and parent gitlink commit are both recorded.
