# DSH Plugin Reliability Design

**Date:** 2026-08-17

**Status:** Approved

## Goal

Make the Article and Seednote DeepSeek Harness integration installable,
diagnosable, and safe under real Web and Desktop execution while preserving the
official DSH architecture and the repository's canonical Agent Pack and Skill
ownership model.

## Scope

This change repairs the confirmed distribution, packaging, credential
redaction, diagnostics, child-lifecycle, Preset concurrency, and documentation
problems in `@anban/dsh-plugin`.

It does not add DSH support for ecommerce, live-slicer, moments, or montage. It
does not move Presets out of the official shared DSH home, automatically mutate
the Preset root during Bundle activation, introduce a second workflow engine,
or change Anban Server business operations.

## Confirmed Findings

The external audit mixes confirmed defects, release-state problems, and
intentional product boundaries. The implementation will use the following
classification.

| Finding | Classification | Decision |
| --- | --- | --- |
| `@anban/dsh-plugin@4.1.11` returns npm 404 and no `v4.1.11` release asset exists | Confirmed release blocker | Publish a prebuilt npm package and checksummed release tarball; verify registry availability after publishing. |
| A `file:` source install can return success without `dsh/lib/cli.js` | Confirmed half-install | Stop recommending source-directory installs; add prepack and packaged-artifact integrity gates; make missing runtime output fail with a specific diagnostic. |
| `Authorization Bearer ...` and `authorization.token=...` survive redaction | Confirmed security defect | Expand conservative Authorization recognition and its positive/negative corpus. |
| CLI and command handlers replace actionable failures with generic text | Confirmed diagnostics defect | Introduce stable public error codes and secret-safe diagnostics. |
| Ordinary Node profile smoke passes, but its package-manager invocation assumes `process.execPath` is Node | Confirmed Desktop portability defect | Consume structured pack output and standard lifecycle runtime information without depending on Desktop-private Electron APIs. |
| CI runs a real Web smoke, but package `check` and the release workflow do not both enforce it | Confirmed gate gap | Make the real smoke part of package, CI, and release acceptance. |
| Skills provider readiness rejection leaves its child undisposed | Confirmed resource leak | Dispose on startup rejection while preserving the startup failure as primary. |
| Two independent Preset installers race | Confirmed concurrency defect | Serialize every Preset mutation with a cross-process lock and re-read state after acquisition. |
| Profile-local Bundle and global Presets have different lifecycles | Intentional DSH architecture with documentation risk | Keep the official global Preset root and document its cross-profile consequences prominently. |
| Bundle installation and Preset materialization are two operations | Intentional safety boundary | Keep the explicit operation and provide status/recovery instructions; do not mutate shared user state on Bundle activation. |
| Only Article and Seednote expose DSH Presets | Approved product scope | Add an explicit support matrix; do not generate unsupported Presets. |
| Credential refresh disposes the old MCP child before mounting the replacement | Approved singleton and secret-lifetime tradeoff | Preserve the current serialized singleton lifecycle. |
| Desktop documentation only shows a shell export | Confirmed onboarding gap | Document the official DSH credential file and its permission rules, with process environment as a temporary or CI override. |

## Canonical Skill Reuse

DSH does not own a separate business Skill tree. `plugins/skills/**` remains the
only authored Skill source. Each `packs/<id>/agent-pack.yaml` declares the
Skills owned by its Agent. The Go Agent Pack generator copies those exact files
into `plugins/dsh/presets/<id>/skills/**`, and the DSH composition mounts that
directory through `@anban/dsh-plugin/skills-provider` and the official
`@deepseek-ai/dsh-skill-filesystem` provider.

Generated DSH Presets remain checked output. `agent-pack-check` must fail if a
generated Skill differs from its canonical source. Host-specific behavior stays
in the Cordis composition, Bundle adapters, credential/MCP integration, and
installer rather than in duplicated Skills.

## Architecture

The existing ownership boundaries remain:

1. Agent Packs own workflow identity, Agent sources, and Skill declarations.
2. `plugins/skills/**` owns domain instructions and references.
3. The Agent Pack generator owns `plugins/dsh/presets/**`.
4. The DSH Bundle owns Host credential resolution, the singleton Creator MCP
   connection, Preset management commands, and Skill-provider mounting.
5. DSH owns profile composition, the global Agent Preset root, credential
   storage, the Skill registry, and tool execution.
6. Anban Server remains the owner of business validation, persistence, billing,
   and side effects exposed through atomic MCP tools.

The integration remains limited to Article and Seednote. The Bundle is added to
each Web or Desktop profile that should expose Creator MCP tools. The generated
Presets are installed once into `$DSH_HOME/.agent-presets` and are shared by all
profiles using the same DSH home.

## Distribution And Installation

### Versioning

The repair is released as `4.1.13`. Because distributed plugin assets and
documentation change, the following versions move together:

- `plugins/package.json`
- `plugins/.claude-plugin/plugin.json`
- `plugins/.codex-plugin/plugin.json`
- `plugins/.claude-plugin/marketplace.json`
- `plugins/CHANGELOG.md`
- repository-side version contracts

### Prebuilt Artifacts

`prepack` builds the TypeScript output and runs a source-side integrity verifier
before an archive is emitted. That verifier checks every `package.json` export,
the `anban-dsh` bin, the Bundle patch, both Preset manifests, both Agent
compositions, and their declared Skill trees. A separate post-pack verifier
extracts the emitted tarball, checks its exact file inventory, imports the three
public exports, and executes the packaged CLI from the extracted package
boundary.

The supported user artifacts are:

1. The public npm package, as the primary path.
2. The checksummed `.tgz` attached to the matching GitHub Release.
3. An immutable Git tag or commit for advanced source installs that explicitly
   approve the package's `prepare` build.

Local development uses `pnpm pack` followed by installation of the resulting
tarball. Documentation does not recommend `file:` source-directory installs.
If a profile nevertheless contains an incomplete package snapshot, invoking the
package bin must return `ERR_RUNTIME_MISSING` with the missing entrypoint and a
tarball-based recovery command rather than a generic failure.

### Release Workflow

The release workflow:

1. validates the tag against every plugin version;
2. installs locked dependencies;
3. runs type checking, unit tests, package integrity, and fresh-profile smoke;
4. creates exactly one versioned tarball;
5. publishes that tarball to npm using repository-configured npm publishing
   authority;
6. verifies anonymous `npm view @anban/dsh-plugin@<version>` resolution;
7. installs the registry copy into a second clean profile and repeats the smoke;
8. generates SHA-256 checksums; and
9. attaches the exact verified tarball and checksums to the GitHub Release.

Missing npm publishing authority is a release failure, not a condition under
which documentation may claim the package is available. Repository code can
implement and test the workflow, but the release operator still owns npm
trusted-publisher or token configuration and the final version tag.

## Smoke Runtime Portability

The smoke test continues to exercise public DSH behavior:

```text
pack prebuilt artifact
  -> initialize a clean profile
  -> add the tarball through dsh plugin
  -> boot/dump the profile
  -> run the installed CLI
  -> discover Article and Seednote
  -> validate singleton Bundle rows
  -> import every installed package export
  -> validate mounted Skill catalogs
  -> remove disposable state
```

`pnpm pack --json` provides the tarball filename. No human-readable stdout
ordering is parsed.

The package-manager child is resolved through standard package-manager
lifecycle information. The runner must handle a normal Node executable and the
Desktop-provided Node shim without assuming that `process.execPath` itself is a
Node binary. It must not import `desktopRuntime`, `desktopPnpmBootstrap`, use
`ELECTRON_RUN_AS_NODE` directly, or depend on another Desktop-private helper.
Windows command shims are handled explicitly rather than passed to a
shell-disabled spawn as if they were native executables.

Tests cover Linux, macOS, and Windows command construction plus a pinned Desktop
public-runtime-contract fixture. Release acceptance retains a real packaged
Desktop smoke on a target Mac and Windows runner; the contract fixture is not a
substitute for the final packaged application check.

## Secret-Safe Diagnostics

### Authorization Redaction

The error renderer recognizes `authorization` as a standalone case-insensitive
key even when its letters are interrupted by accepted quoting, escaping,
control, or whitespace forms. After the key it conservatively redacts payloads
introduced by whitespace, `.`, `[`, `(`, `:`, `=`, or `,`. It performs a second
scan after control removal and whitespace normalization.

Positive tests include:

- `Authorization Bearer leaked-secret`
- `authorization.token=leaked-secret`
- `headers.authorization=leaked-secret`
- `[Authorization]=leaked-secret`
- `authorization = leaked-secret`
- mixed case, escaped quotes, arrays, JSON, and control characters

Negative tests include `xauthorization`, longer identifier keys, and ordinary
prose that mentions authorization without carrying a value. The implementation
prefers omitting uncertain diagnostic text to leaking a credential.

### Operational Errors

Preset and packaging code throws controlled operational errors containing:

- a stable non-secret code;
- one safe public message;
- an optional owned recovery path; and
- an internal cause that is never rendered by default.

Initial codes include:

- `ERR_RUNTIME_MISSING`
- `ERR_PRESET_UNOWNED`
- `ERR_PRESET_MODIFIED`
- `ERR_PRESET_LOCKED`
- `ERR_PRESET_LOCK_INVALID`
- `ERR_PRESET_ROLLBACK`
- `ERR_PRESET_OPERATION`

The CLI and GUI command adapter use the same formatter. Default output is one
line containing the code, public message, and necessary recovery path. It never
prints a stack or nested cause. `ANBAN_DSH_DEBUG=1` may add a sanitized stack,
processed line by line through the same bounded redaction layer. A debug path
must not serialize arbitrary objects, plugin configuration, or causes.

## Skills Provider Lifecycle

After creating the official Skill filesystem child, the adapter awaits child
readiness. If readiness rejects, it disposes that exact child once before
rethrowing. The readiness failure remains primary. If disposal also rejects,
the adapter returns a controlled aggregate failure whose public diagnostic does
not expose either untrusted payload.

Successful startup returns an idempotent cleanup function that awaits disposal.
Tests cover readiness rejection, disposal rejection, successful startup,
normal cleanup, and repeated cleanup.

## Cross-Process Preset Lock

Every operation that mutates `$DSH_HOME/.agent-presets` acquires one global
Anban lock before reading mutable Preset state. Status remains read-only and does
not acquire the write lock.

The lock is an atomically created directory named
`.anban-dsh.lock`. Its owner document contains schema version, PID, hostname,
creation time, package version, and a random owner id. Acquisition validates
the Preset root and lock path using the existing containment and symlink rules.

When a lock already exists, a contender waits for a bounded interval and
retries. A lock may be reclaimed automatically only when its owner document is
valid, it names the current host, and the recorded PID is proven not to exist.
Reclamation first atomically renames the exact lock directory to a unique
quarantine name, so two reclaimers cannot delete a newly acquired lock. A
malformed document, remote hostname, permission failure, or indeterminate
process state returns `ERR_PRESET_LOCK_INVALID` or `ERR_PRESET_LOCKED`; it is not
deleted automatically.

After acquiring the lock, install and remove re-read all Preset state. Concurrent
install/install therefore becomes an idempotent success. Install/remove and
force/install serialize in acquisition order. The exact owner releases the lock
in `finally`; release validates the owner id before removing anything.

Tests use injected time, waiting, hostname, PID-liveness, and fault boundaries,
plus real independent Node processes. They cover contention, timeout,
idempotence, every operation pair, startup failure, cleanup failure, stale-lock
reclamation, corrupt ownership, and a process-crash residue.

## Documentation

The root README and DSH installation guide distinguish:

| Surface | Skills | Native Agent | DSH Bundle/MCP | DSH Preset |
| --- | --- | --- | --- | --- |
| Skills-only installer | Yes | No | No | No |
| Claude Code plugin | Yes | Claude Agent | Claude MCP adapter | No |
| Codex plugin | Yes | Codex subagent | Codex MCP adapter | No |
| Full DSH plugin | Article/Seednote generated copies | DSH composition | Official DSH adapters | Article/Seednote |

The DSH guide provides npm, release-tarball, and immutable Git instructions;
Web and Desktop profile discovery; installation, status, upgrade, rollback, and
removal; and explicit warnings about the global Preset lifecycle.

Credential setup follows official DSH precedence:

1. inherited process environment, read-only and highest priority;
2. `$DSH_HOME/.credentials.yaml`, writable managed storage;
3. invocation-project `.env`; and
4. `$DSH_HOME/.env`.

The persistent Desktop-friendly path writes only:

```yaml
ANBAN_API_KEY: <value>
```

On POSIX the file must be mode `0600` and its directory owner-only. The guide
does not claim that DSH's model-specific onboarding UI can configure an
arbitrary third-party credential reference. No command line, screenshot,
fixture, or generated artifact contains a real key.

## Verification

Implementation follows test-driven development. Required verification includes:

- targeted Vitest tests for each defect before its implementation;
- all DSH package tests, type checking, build, pack integrity, and smoke;
- real two-process Preset mutation tests;
- package-manager command tests for POSIX, Windows, and Desktop wrapper shapes;
- `make agent-pack-generate` and `make agent-pack-check`;
- targeted Go DSH contracts and `go test ./...`;
- `go build -o /tmp/anban-creator-server ./server`;
- clean-tree and generated-output drift checks;
- an independent specification review and code-quality review; and
- final verification from fresh command output immediately before completion.

An actual valid-key Creator MCP call is not automated with a repository-owned
secret. The release checklist requires a release operator to use a dedicated,
low-privilege test account outside Git for `list_projects` and
`get_project_profile`. Missing-key and invalid-key startup remain automated.

## Completion And Integration

The implementation is complete only when all confirmed defects above have a
failing regression test that passes after the fix, every required verification
command succeeds, reviews contain no Critical or Important findings, and the
plugin repository plus parent gitlink are committed on the repair branch.

The reviewed branch is then merged into local `main`. Publishing to npm and
creating the signed/tagged GitHub Release remain explicit release operations;
they require configured external publishing authority and are not silently
performed by a source-code merge.
