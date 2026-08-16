# DeepSeek Harness Article/Seednote Plugin Integration Design

Date: 2026-08-16
Status: Revised design, pending written-spec approval

## 1. Context

Anban currently distributes one canonical plugin tree through native Claude Code and Codex surfaces. Agent Packs under `plugins/packs/<id>/` own the business identity and distribution contract, `plugins/skills/` is the single Skill source, and `server/agentpack/` generates the native Agent files and Server catalog.

This change adds DeepSeek Harness (DSH) as a third host surface for the existing `article` and `seednote` Packs. The first milestone is deliberately limited to DSH packaging and runtime integration. It does not change Anban task creation, task execution ownership, billing, image generation, artifact settlement, or any existing Claude/Codex/managed-runner behavior.

DSH integration follows the official architecture:

- an npm Bundle contributes a `cordis.patch.yml` configuration layer;
- Agent Presets are directories containing `agent.cordis.yml` and optional `preset.yml` metadata;
- Skills are loaded through `@deepseek-ai/dsh-skill-filesystem`;
- credentials are resolved through `ctx.credentials`;
- MCP transport and tool registration are delegated to `@deepseek-ai/dsh-mcp-client`.

Custom code is limited to narrow adapters where current DSH releases do not expose a complete declarative seam.

## 2. Goals

1. Produce one installable `@anban/dsh-plugin` Bundle that works with DSH Web and DSH Desktop profiles.
2. Add selectable `article` and `seednote` Agent Presets.
3. Generate the DSH Presets from the existing canonical Agent Packs.
4. Load only the Skills declared by each Pack, without creating a second hand-maintained Skill tree.
5. Connect once to the fixed Anban MCP endpoint through the official DSH MCP client.
6. Resolve `ANBAN_API_KEY` through the official DSH credentials service without placing the secret in Bundle configuration, Presets, logs, tests, or artifacts.
7. Preserve existing Claude and Codex generated Agent files byte-for-byte.
8. Provide deterministic generation, drift checking, installation, removal, and smoke verification.

## 3. Non-goals

- Do not add or restore an Anban MCP `create_task` tool.
- Do not change `generate_image`, Task, TaskExecution, billing, artifact, or execution-token contracts.
- Do not claim Studio local tasks or turn DSH into an Anban managed executor.
- Do not promise full image-backed execution without an already valid Anban task/execution context.
- Do not change `agent-ts`, Claude Code Agents, Codex subagents, Claude Hooks, Codex installers, or managed runtime images.
- Do not add Desktop-only APIs, UI panels, custom DSH workflow engines, or a second business-orchestration layer.
- Do not publish Montage in this milestone.
- Do not make the Anban MCP endpoint user-configurable. It remains `https://creator.anbanai.com/mcp`.

## 4. Core Architecture

```text
plugins/packs/article/agent-pack.yaml
plugins/packs/seednote/agent-pack.yaml
             |
             | make agent-pack-generate
             v
plugins/dsh/presets/article/
plugins/dsh/presets/seednote/
             |
             | packaged by plugins/package.json
             v
       @anban/dsh-plugin
        |              |
        |              +--> host-plane Anban MCP adapter
        |              |          |
        |              |          +--> ctx.credentials
        |              |          +--> official dsh-mcp-client
        |              |
        |              +--> host-plane Preset manager
        |              |          |
        |              |          +--> official ctx.commands
        |              |
        |              +--> agent-plane Skills provider export
        |                         |
        |                         +--> official dsh-skill-filesystem
        |
        +--> anban-dsh CLI ------> shared Preset-management library
                                            |
                                            +--> $DSH_HOME/.agent-presets/article
                                            +--> $DSH_HOME/.agent-presets/seednote
```

Agent Packs remain the cross-host source of truth. DSH Presets are generated host artifacts, like the current Claude Markdown and Codex TOML outputs. DSH does not know about or load Agent Packs directly.

## 5. Repository Layout

```text
plugins/
├── package.json                         # @anban/dsh-plugin Bundle manifest
├── dsh/
│   ├── cordis.patch.yml                # Bundle patch
│   ├── src/
│   │   ├── anban-mcp.ts                # Credentials-to-official-MCP adapter
│   │   ├── preset-manager.ts           # Explicit DSH command registration
│   │   └── skills-provider.ts          # Bundle-owned official Skill adapter
│   ├── lib/                             # Built JavaScript shipped in the Bundle
│   ├── bin/
│   │   └── anban-dsh.js                 # Explicit install/remove/status commands
│   └── presets/                         # Generated, never edited directly
│       ├── article/
│       │   ├── agent.cordis.yml
│       │   ├── preset.yml
│       │   └── skills/<declared skills>/
│       └── seednote/
│           ├── agent.cordis.yml
│           ├── preset.yml
│           └── skills/<declared skills>/
├── packs/
│   ├── article/
│   │   ├── agent-pack.yaml
│   │   ├── agent.claude.md
│   │   ├── agent.codex.toml
│   │   └── agent.dsh.yml
│   └── seednote/
│       ├── agent-pack.yaml
│       ├── agent.claude.md
│       ├── agent.codex.toml
│       └── agent.dsh.yml
└── skills/                              # Canonical Skill sources
```

The DSH package is rooted at `plugins/` so the existing plugin tree remains one distribution unit. Its npm `files` allowlist ships only the DSH patch, built runtime modules, CLI, generated Presets, required metadata, and license/readme material; Claude/Codex-only files do not need to be included in the npm artifact.

## 6. Agent Pack Contract

`AgentSpec` gains one optional field:

```yaml
agent:
  name: article
  claude_source: agent.claude.md
  codex_source: agent.codex.toml
  dsh_source: agent.dsh.yml
  skills:
    - content-writing
    - humanizer
```

Rules:

- `dsh_source` is optional so Packs not included in this milestone remain valid.
- When present, it must be a secure relative path inside the Pack directory.
- The source must parse as the DSH loader's top-level list of named plugin rows.
- The source is copied without semantic rewriting to the generated `agent.cordis.yml`.
- `display_name` and `description` generate `preset.yml`; those values are not duplicated in `agent.dsh.yml`.
- `agent.skills` selects the exact canonical Skill directories copied into that Preset.
- Claude and Codex source validation and copy behavior remain unchanged.

No DSH configuration is inferred from Claude frontmatter or Codex TOML. Each host retains an explicit native source because tool names, lifecycle assumptions, and prompt composition differ.

## 7. Generator Behavior

`make agent-pack-generate` extends the existing generator with a DSH output phase:

1. Load and validate the catalog as today.
2. Generate Claude and Codex outputs exactly as today.
3. For each Pack with `dsh_source`:
   - create `plugins/dsh/presets/<agent.name>/`;
   - copy the DSH source to `agent.cordis.yml`;
   - generate `preset.yml` from `display_name` and `description`;
   - recursively copy only the Skill directories named by `agent.skills`.
4. Remove stale generated DSH Preset entries that are no longer declared.
5. Keep output ordering and file modes deterministic.

`make agent-pack-check` performs the same derivation without writing and fails on:

- missing or changed generated DSH files;
- unexpected generated Preset directories or files;
- a missing declared Skill;
- a malformed DSH source composition;
- a generated Skill copy that differs from `plugins/skills/<name>/`;
- any drift in the existing Claude/Codex outputs or generated catalogs.

The generator must not depend on Node installation or a DSH runtime. YAML shape validation stays in Go; actual DSH loading is covered by package smoke tests.

## 8. Preset Composition

Each `agent.dsh.yml` is a native agent-plane composition. It uses official DSH plugins for:

- Persona and agent instructions;
- Bash/Pwsh where supported by the host platform;
- filesystem read, write, edit, and search tools;
- Skill catalog and Skill loading;
- ask-user and todo tools where the workflow needs them.

Each composition also mounts the Bundle-owned Skill adapter by exported package
subpath rather than by a relative Preset module:

```yaml
- id: anban-skills
  name: '@anban/dsh-plugin/skills-provider'
  config:
    presetId: article
    providerName: anban-article
```

`presetId` must equal the Pack agent name, and `providerName` must be unique for
the mounted Preset. The Bundle must therefore be installed in every profile that
uses an Anban Preset, which is already required for the Anban MCP tools.

The Preset does not create another MCP connection. The Anban MCP client is a host-plane singleton contributed by the Bundle, so Article and Seednote sessions can coexist without violating the official MCP client's unique `serverName` requirement.

The DSH persona is host-specific and includes only adapter guidance that cannot live in host-neutral Skills:

- use DSH filesystem tools for Skill references to Read/Write/Edit;
- use DSH Bash/Pwsh for Skill references to shell execution;
- use the DSH todo tool for Claude/Codex `TaskCreate` and `TaskUpdate` workflow bookkeeping;
- resolve a bare Anban MCP name such as `list_projects` to `mcp__creator__list_projects`;
- never replace an unavailable MCP call with a hand-written HTTP client;
- treat `$TASK_ID` and managed-runtime attachment files as available only when the initiating context actually supplied them.

Business sequencing, retries, quality gates, artifact names, and stop/continue decisions remain in Agents and Skills. The Preset only composes capabilities and explains host syntax.

## 9. Skill Loading

DSH `customSkillDirs` values are resolved by the official provider against the process working directory, not the Preset directory. A published Preset therefore cannot safely use `./skills` directly.

The Bundle exports `@anban/dsh-plugin/skills-provider`, implemented by
`dsh/src/skills-provider.ts`. Keeping this adapter inside the npm package is
required for reliable ESM dependency resolution: a module copied under
`$DSH_HOME/.agent-presets/<id>/` would not find DSH's fallback dependencies in
`$DSH_HOME/profiles/node_modules` through Node's normal parent-directory walk.
The DSH loader's bare-module resolution guarantee applies to the composition
row, so the Preset mounts the Bundle export by package subpath.

The adapter:

1. validates `presetId` and `providerName` as bounded identifiers;
2. resolves the absolute Skill root with
   `dshHomePath('.agent-presets', presetId, 'skills')`;
3. delegates registration to `@deepseek-ai/dsh-skill-filesystem`;
4. sets `includeDefaultRoots: false`;
5. passes the unique provider name, `anban-article` or `anban-seednote`;
6. enables the official watcher using the provider defaults.

The adapter does not parse Skills, implement discovery, or implement Skill tools. It only converts an installed Preset id into the absolute path required by the official provider. The official provider remains responsible for discovery, parsing, loading, watching, and disposal.

Generated Skill copies preserve the complete directory, including `SKILL.md`, `references/`, `scripts/`, and assets. This keeps a copied DSH Preset self-contained while `plugins/skills/` remains the only editable source.

## 10. Bundle Manifest And Profile Installation

`plugins/package.json` declares:

- package name `@anban/dsh-plugin`;
- ESM package type;
- exact, pinned DSH/cordis peer dependency versions validated by this repository;
- the built Host plugin entry points and the `./skills-provider` package export;
- a `dsh.bundle.patch` pointing to `./dsh/cordis.patch.yml`;
- an `anban-dsh` executable;
- a strict `files` allowlist.

The Bundle patch inserts two host-plane rows: the Anban MCP adapter and a Preset manager that registers explicit user commands through the official `ctx.commands` registry. The Skills provider is not inserted at the Host layer; each generated Preset mounts the exported package subpath with its own Preset id and provider name. The Bundle does not replace DSH's `agent-presets` service, default Preset, credentials provider, tool registry, model adapters, filesystem service, or Web/Desktop surface rows.

Current DSH Bundle manifests do not expose a field for shipping additional Agent Preset roots, and the CLI launcher injects its own shipped root after profile/user patches. The integration therefore uses an explicit installer rather than replacing the official roster service or writing user files during DSH startup.

Installation flow:

```bash
dsh plugin --profile <profile> add @anban/dsh-plugin
anban-dsh install-presets
```

Interactive Web/Desktop users may instead invoke the global DSH command:

```text
/anban-presets-install
```

The command and CLI call the same Preset-management library and produce the same result. The command is registered through the official DSH command service, executes entirely on the Host, returns a direct command result that does not enter model history, and does not use Desktop-specific services. This makes Preset installation available inside packaged DSH Desktop without requiring a system Node.js installation.

Behavior of `anban-dsh install-presets`:

- resolve the official DSH home using DSH's home-path helper;
- copy `article` and `seednote` into `$DSH_HOME/.agent-presets/`;
- use an atomic temporary-directory then rename operation;
- write a small non-secret ownership manifest inside each installed Preset containing package name, package version, Preset id, and source digest;
- no-op when the destination has the same source digest;
- refuse to overwrite an unknown or locally modified destination;
- require an explicit `--force` to replace a conflicting directory;
- never run automatically from `postinstall`, `prepare`, or DSH startup.

`anban-dsh remove-presets` removes only directories carrying a matching Anban ownership manifest. It refuses to remove an unowned directory. `anban-dsh status` reports Bundle version, source digest, installed digest, and drift without reading or printing credentials.

The equivalent interactive commands are `/anban-presets-remove` and `/anban-presets-status`. Removal requires an explicit `confirm` argument, and force replacement requires `/anban-presets-install force`; commands do not open forms or start model turns.

Presets live in the shared DSH home and therefore need installation only once. The Bundle itself must be added to every Web/Desktop profile that should expose the Anban MCP tools.

For Git installs, development documentation follows DSH's official pnpm build-allowance guidance. Published npm packages and release tarballs include built `dsh/lib/` output and require no install-time build permission.

The initial compatibility baseline is:

| Component | Pinned baseline |
| --- | --- |
| `@deepseek-ai/dsh-credentials`, `@deepseek-ai/dsh-home-paths`, `@deepseek-ai/dsh-mcp-client`, `@deepseek-ai/dsh-skill-filesystem`, and their Host-provided DSH contracts | `0.1.0-rc.6` |
| `@deepseek-ai/cordis` | `4.0.1` |
| DSH Desktop | `2.0.0`, repository commit `4f68147091e585aaa1d815f99d30a657b3842d7c` |

DSH packages and Cordis are exact `peerDependencies`, with the same exact versions in `devDependencies` for build and test. The Bundle must share the Host's Cordis and DSH service definitions; it must not install a second runtime copy. The profile's DSH installation fallback supplies the peer packages at runtime.

## 11. Credentials And MCP Lifecycle

The Bundle contributes one Host plugin, `anban-mcp`, with these fixed values:

```text
credential reference: ANBAN_API_KEY
server name:          creator
transport:            streamable-http
URL:                  https://creator.anbanai.com/mcp
Authorization:        Bearer <resolved credential>
```

The plugin injects `credentials` and uses `credentialRef('ANBAN_API_KEY')`. It never reads `$DSH_HOME/.credentials.yaml` directly.

Activation behavior:

1. Resolve the credential through `ctx.credentials`.
2. If absent, remain active without mounting an MCP child and log one actionable warning that identifies only the missing reference.
3. If present, mount the official `@deepseek-ai/dsh-mcp-client` as a child plugin with the fixed HTTP configuration.
4. Set `serverName: creator`, the official reconnect defaults, the existing Anban long tool-call timeout of 900000 ms, and `failOnStartupError: false` so DSH can still boot when the remote service is unavailable.
5. Let the official MCP client own initialization, `tools/list`, tool registration, list-change synchronization, call cancellation, timeout, reconnection, and cleanup.

Credential updates:

- listen for the official `credentials/updated` event;
- ignore unrelated references;
- dispose the current MCP child before resolving the new value;
- mount a new child only after the old child is fully disposed;
- serialize refreshes so rapid edits cannot create two live `creator` instances;
- an empty/unset credential removes the MCP tools without stopping DSH;
- environment-sourced credential changes require a DSH restart, matching official credentials-local semantics.

The resolved key and complete Authorization header may exist only in the in-memory child configuration. Errors must redact header values and must not serialize plugin configuration containing the resolved secret.

## 12. Runtime Data Flow

Preset installation is a Host-side management path that runs before agent
selection. The `anban-dsh` CLI and the registered `/anban-presets-*` commands
call the same library; only that library writes the owned Preset directories in
the official DSH user Preset root. It does not start an agent or invoke a model.

```text
User selects Article or Seednote Preset
        |
        v
DSH mounts the generated agent.cordis.yml
        |
        +--> @anban/dsh-plugin/skills-provider
        |        |
        |        +--> dshHomePath(.agent-presets/<id>/skills)
        |        |
        |        +--> official skill-filesystem --> generated skills/
        |
        +--> official filesystem/shell/todo/ask-user tools
        |
        +--> host ctx.tools global layer
                 |
                 +--> mcp__creator__* tools from the singleton MCP client
```

The Agent follows its Pack workflow using the capabilities that are actually present. Calls requiring a valid Anban Task or execution context keep their current server behavior and may fail when that context was not supplied. This milestone reports that limitation honestly and does not add a fallback business path.

## 13. Error Handling

| Condition | Required behavior |
| --- | --- |
| Missing `ANBAN_API_KEY` | DSH boots; Anban MCP tools are absent; log a single setup hint without secret material. |
| Invalid/expired key | Official MCP startup fails without crashing DSH; tools remain absent; diagnostics identify authentication failure but not the header. |
| Network/server unavailable | Official MCP reconnect policy applies; DSH remains usable. |
| Credential updated | Dispose and recreate only the Anban MCP child. Existing DSH sessions observe the resulting tool-generation change through the official registry. |
| Duplicate `creator` server name | Treat as Bundle misconfiguration and fail the Anban child loudly; never rename the server namespace automatically. |
| Broken generated Preset | `agent-pack-check` and DSH smoke loading fail before release. DSH discovery may list it as broken according to official behavior. |
| Existing unowned Preset directory | Installer refuses to overwrite it. |
| Locally modified owned Preset | Installer reports drift and requires explicit `--force`. |
| Missing Task/execution context | Preserve the existing MCP error. The Agent must not invent ids, create an HTTP fallback, or claim success. |

## 14. Security

- No API key or bearer header in Git, npm metadata, Bundle patches, Presets, Pack manifests, generated catalogs, fixtures, snapshots, logs, screenshots, or artifacts.
- Tests use fixed fake credentials and assert redaction; they never contact production with a repository-owned secret.
- The Bundle stores only the `ANBAN_API_KEY` reference.
- The endpoint is fixed HTTPS and is not accepted from Agent input or Bundle user configuration.
- Preset installation validates ids, rejects path traversal, copies regular files/directories only, and does not preserve symbolic links that escape the source tree.
- Removal is limited to exact owned Preset directories under the resolved DSH user Preset root.
- The Host MCP adapter performs no business API calls. It only resolves a credential and delegates to the official MCP client.
- Agents and Skills continue to use MCP tools rather than direct HTTP calls to the Anban MCP endpoint.

## 15. Compatibility And Versioning

- The same Bundle package is used by DSH Web and Desktop.
- No Desktop-only imports or client modules are permitted in the first milestone.
- DSH dependencies are pinned to `0.1.0-rc.6` and Cordis to `4.0.1` because the upstream project is still in developer-preview/RC evolution.
- Desktop compatibility is tested against DSH Desktop `2.0.0` at commit `4f68147091e585aaa1d815f99d30a657b3842d7c`.
- A dependency upgrade requires rerunning Web and Desktop installation/loading smoke tests before changing the pin.
- `agent.dsh.yml` is optional, so existing Packs and Server catalog consumers remain compatible.
- Existing Claude Markdown and Codex TOML outputs must remain byte-identical after generation.
- Because files under `plugins/` change, both native plugin manifests receive the same patch version bump, even though their runtime behavior does not change.

## 16. Testing Strategy

### 16.1 Go generator tests

- parse optional `dsh_source` with strict YAML known-field handling;
- reject absolute paths, traversal, missing files, and malformed DSH list shape;
- generate `agent.cordis.yml`, `preset.yml`, and exact Skill trees;
- skip DSH output for Packs without `dsh_source`;
- remove stale generated DSH files;
- detect all DSH drift categories in `CheckRepository`;
- prove existing Claude/Codex outputs are unchanged;
- preserve deterministic catalog digests and output ordering.

### 16.2 Node package tests

- Bundle manifest and `files` allowlist are valid;
- Preset commands register globally, return direct command results, and never enqueue a model turn;
- credentials adapter mounts no child when the key is absent;
- it mounts exactly one official MCP child when present;
- unrelated credential updates do nothing;
- matching updates serialize disposal and remount;
- logs and thrown errors do not contain fake secret values;
- the `./skills-provider` package export validates ids, resolves the installed Skill root independently of process cwd through `dshHomePath`, and delegates exactly once to the official provider;
- a disposable profile resolves `@anban/dsh-plugin/skills-provider` from an installed Preset while the provider's own DSH peer imports resolve from the Bundle/profile installation;
- installer handles fresh install, identical no-op, drift refusal, force replacement, safe removal, path validation, and interrupted atomic copy;
- CLI and command entry points produce identical install/status/remove behavior.

### 16.3 DSH integration tests

Against the pinned DSH release:

- install the local Bundle into a disposable Web profile;
- run `dsh --profile <profile> --dump-config` and confirm the Anban Bundle layer and one Host MCP row;
- install Presets into a temporary `DSH_HOME` and confirm official roster discovery lists `article` and `seednote` as healthy;
- mount each Preset through the installed Bundle export and confirm only its declared Anban Skills are discoverable through its provider;
- confirm both Presets can coexist across sessions while only one `creator` MCP client exists;
- check out DSH Desktop commit `4f68147091e585aaa1d815f99d30a657b3842d7c`, install the Bundle through its active-profile plugin mechanism, run `/anban-presets-install`, and repeat the configuration and Preset mount smoke without using Desktop-private services or a system Node.js installation.

### 16.4 MCP smoke tests

- use a user-supplied development credential outside Git;
- confirm `mcp__creator__list_projects` and `mcp__creator__get_project_profile` are discovered and callable;
- confirm missing and invalid credentials do not prevent DSH startup;
- do not require `create_task`, `generate_image`, task mutation, billing, or publishing for the first integration acceptance gate.

### 16.5 Repository verification

At minimum:

```bash
make agent-pack-generate
make agent-pack-check
go test ./server/agentpack ./server/mcp
go test ./...
```

Run the DSH package test/build commands defined in `plugins/package.json`, followed by Web/Desktop smoke commands documented with the pinned dependency version.

## 17. Acceptance Criteria

The milestone is complete when all of the following are true:

1. `@anban/dsh-plugin` installs as an official DSH Bundle into a clean profile.
2. `article` and `seednote` install into the official user Preset root and appear as healthy selectable Presets.
3. Each Preset loads its generated Persona, official tools, and exactly the Skills declared by its Pack through the installed Bundle's `./skills-provider` export.
4. A configured `ANBAN_API_KEY` produces one live `creator` MCP connection and `mcp__creator__*` tools.
5. Live smoke calls to `list_projects` and `get_project_profile` succeed with a user-supplied development key.
6. Missing, invalid, and updated credentials follow the specified lifecycle without exposing secret material or preventing DSH boot.
7. Web and Desktop use the same Bundle and Preset artifacts.
8. `make agent-pack-check` detects DSH drift.
9. Existing Claude/Codex generated Agents remain byte-identical; the only native-host metadata change is the required matching patch-version bump in both plugin manifests.
10. No Server business behavior, task execution path, billing behavior, or existing runner behavior changes.

## 18. Deferred Work

The following work requires a separate design and implementation cycle:

- a safe idempotent interactive task creation contract;
- DSH ownership of Anban TaskExecution records or local-task claims;
- full image generation without a pre-existing execution context;
- task progress/finalization and DSH workspace artifact upload;
- DSH-specific UI for Anban projects, credentials, tasks, or deliverables;
- Montage Preset and its OpenMontage workspace/runtime requirements;
- upstream replacement of the explicit Preset installer if DSH adds a Bundle Preset contribution manifest.

These are intentionally excluded so the first milestone proves the DSH host surface without changing Anban's business invariants.

## 19. Reference Baseline

Implementation decisions in this spec are grounded in the following upstream
documentation and pinned source baseline:

- [DSH basic plugin development](https://deepseek-harness.github.io/deepseek-harness/develop/basic/)
- [DSH Bundle publishing](https://deepseek-harness.github.io/deepseek-harness/develop/basic/publish)
- [Official Agent Presets README](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/agent-presets/README.zh.md)
- [Official credentials README](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/credentials/credentials/README.zh.md)
- [Official MCP client README](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/mcp-client/README.zh.md)
- [DSH Desktop plugin development guide](https://github.com/anywhere-labs/deepseek-harness-desktop/blob/master/docs/plugin-development.md), validated against commit `4f68147091e585aaa1d815f99d30a657b3842d7c`
