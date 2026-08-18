# DSH Plugin Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `@anban/dsh-plugin@4.1.13` installable, diagnosable, secret-safe, and race-safe in supported DSH Web/Desktop environments while preserving canonical Article/Seednote Skills and official DSH lifecycle boundaries.

**Architecture:** Keep Agent Packs and `plugins/skills/**` as the only business-workflow source. Add focused DSH adapter modules for operational errors, package integrity, portable process invocation, and the global Preset mutation lock; keep status read-only and keep install/remove explicit. Treat npm publication and GitHub Release creation as operator-authorized release actions, while making repository gates prove the exact artifact is ready.

**Tech Stack:** TypeScript 6, Node.js 22/24 ESM, Vitest 4, pnpm 11, Cordis/DeepSeek Harness rc.6, Go contract tests, GitHub Actions.

---

## File Map

- Create `plugins/dsh/src/operational-error.ts`: stable public DSH error codes and one-line/debug-safe formatting.
- Create `plugins/dsh/src/preset-lock.ts`: atomically owned cross-process lock for the shared Preset root.
- Create `plugins/dsh/scripts/package-integrity.mjs`: source-tree and packed-tarball validation.
- Modify `plugins/dsh/src/safe-error.ts`: conservative Authorization redaction and normalized second pass.
- Modify `plugins/dsh/src/cli.ts` and `plugins/dsh/src/preset-manager.ts`: shared operational diagnostics for CLI and DSH command handlers.
- Modify `plugins/dsh/bin/anban-dsh.js`: deterministic `ERR_RUNTIME_MISSING` recovery output.
- Modify `plugins/dsh/src/skills-provider.ts`: startup-failure cleanup and idempotent async disposal.
- Modify `plugins/dsh/src/presets.ts`: typed failures and lock install/remove mutations after acquisition.
- Modify `plugins/dsh/scripts/smoke-profile.mjs`: structured pack results, all-export imports, registry source support, and portable command construction.
- Modify corresponding files in `plugins/dsh/tests/`: regression, lifecycle, package, portability, and two-process coverage.
- Modify `plugins/package.json` and `plugins/pnpm-lock.yaml`: integrity/check scripts and `4.1.13` release metadata.
- Modify `.github/workflows/ci.yml` and `.github/workflows/release.yml`: package/OS/release/registry acceptance gates.
- Modify `plugins/README.md`, `plugins/docs/dsh-installation.md`, and `plugins/CHANGELOG.md`: official install, credentials, support matrix, and global lifecycle guidance.
- Modify both native manifests, the Claude marketplace manifest, and `server/agent/dsh_plugin_contract_test.go`: synchronized `4.1.13` contract.

### Task 1: Authorization Redaction Corpus

**Files:**
- Modify: `plugins/dsh/tests/anban-mcp.test.ts`
- Modify: `plugins/dsh/src/safe-error.ts`

- [ ] **Step 1: Add failing positive and negative table tests**

Add a table that expects all payload-bearing variants to become `Authorization: [REDACTED]`, including whitespace, `.token=`, `headers.authorization=`, brackets, parentheses, quotes, escaped quotes, JSON arrays, tabs, NUL/control interruption, and mixed case. Add negative cases for `xauthorization`, longer identifiers, and prose such as `authorization is required` that must remain non-secret text.

```ts
it.each([
  'Authorization Bearer leaked-secret',
  'authorization.token=leaked-secret',
  'headers.authorization=leaked-secret',
  '[Authorization]=leaked-secret',
  'authorization = leaked-secret',
  'AuThOrIzAtIoN\tBearer leaked-secret',
  'authori\u0000zation: leaked-secret',
])('redacts Authorization payloads from %j', (source) => {
  const rendered = safeErrorLine(new Error(source))
  expect(rendered).toContain('Authorization: [REDACTED]')
  expect(rendered).not.toContain('leaked-secret')
})

it.each(['xauthorization', 'authorizationPolicy', 'authorization is required'])(
  'does not classify ordinary text %j as a credential payload',
  (source) => expect(safeErrorLine(new Error(source))).toContain(source),
)
```

- [ ] **Step 2: Run the focused tests and confirm the leak**

Run: `cd plugins && pnpm vitest run dsh/tests/anban-mcp.test.ts`

Expected: FAIL because the confirmed Authorization variants still contain `leaked-secret`; negative prose remains unchanged.

- [ ] **Step 3: Implement bounded two-pass Authorization scanning**

Update `safe-error.ts` so key recognition is case-insensitive and boundary-aware, accepts the approved separators, replaces only the payload-bearing span, and repeats after control removal plus whitespace normalization. Keep the existing 4,096-byte scan bound, 512-byte output bound, proxy/native-error protections, and explicit-secret replacement.

```ts
function redactAuthorizationPass(line: string): string {
  // Scan a bounded string, require identifier boundaries, then replace the
  // value-bearing span introduced by whitespace, '.', '[', '(', ':', '=', or ','.
}

function redactAuthorization(line: string): string {
  const first = redactAuthorizationPass(line)
  const normalized = first
    .replace(CONTROL_CHARACTERS_PATTERN, '')
    .replace(WHITESPACE_PATTERN, ' ')
  return redactAuthorizationPass(normalized)
}
```

- [ ] **Step 4: Run the focused tests**

Run: `cd plugins && pnpm vitest run dsh/tests/anban-mcp.test.ts`

Expected: PASS with no positive corpus value present in rendered output.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/src/safe-error.ts dsh/tests/anban-mcp.test.ts
git -C plugins commit -m "fix: harden DSH authorization redaction"
```

### Task 2: Typed Operational Diagnostics

**Files:**
- Create: `plugins/dsh/src/operational-error.ts`
- Create: `plugins/dsh/tests/operational-error.test.ts`
- Modify: `plugins/dsh/src/cli.ts`
- Modify: `plugins/dsh/src/preset-manager.ts`
- Modify: `plugins/dsh/tests/cli.test.ts`
- Modify: `plugins/dsh/tests/preset-manager.test.ts`

- [ ] **Step 1: Add failing formatter and adapter tests**

Cover the seven approved codes, safe recovery text, hidden causes, sanitized debug stacks, single-line default output, and identical CLI/GUI results. Assert `ANBAN_DSH_DEBUG=1` never renders cause objects or credential values.

```ts
const error = new OperationalError(
  'ERR_PRESET_OPERATION',
  'Unable to install Anban Presets.',
  { recovery: 'Run anban-dsh status-presets.', cause: new Error('token=secret') },
)
expect(formatOperationalError(error)).toBe(
  'ERR_PRESET_OPERATION: Unable to install Anban Presets. Run anban-dsh status-presets.',
)
expect(formatOperationalError(error)).not.toContain('secret')
```

- [ ] **Step 2: Verify the tests fail**

Run: `cd plugins && pnpm vitest run dsh/tests/operational-error.test.ts dsh/tests/cli.test.ts dsh/tests/preset-manager.test.ts`

Expected: FAIL because `OperationalError` and the shared formatter do not exist and adapters still emit generic text.

- [ ] **Step 3: Implement the stable error contract**

Define `OperationalErrorCode` as the exact union below, own only validated ASCII public fields, retain `cause` privately, and render debug stack lines only through `safeErrorLine`.

```ts
export type OperationalErrorCode =
  | 'ERR_RUNTIME_MISSING'
  | 'ERR_PRESET_UNOWNED'
  | 'ERR_PRESET_MODIFIED'
  | 'ERR_PRESET_LOCKED'
  | 'ERR_PRESET_LOCK_INVALID'
  | 'ERR_PRESET_ROLLBACK'
  | 'ERR_PRESET_OPERATION'

export class OperationalError extends Error {
  readonly code: OperationalErrorCode
  readonly recovery?: string
}

export function formatOperationalError(
  error: unknown,
  options: { debug?: boolean } = {},
): string
```

Update CLI and command handlers to map known errors through this formatter and wrap unknown failures as `ERR_PRESET_OPERATION` without exposing their cause.

- [ ] **Step 4: Run focused tests and typecheck**

Run: `cd plugins && pnpm vitest run dsh/tests/operational-error.test.ts dsh/tests/cli.test.ts dsh/tests/preset-manager.test.ts && pnpm run typecheck`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/src/operational-error.ts dsh/src/cli.ts dsh/src/preset-manager.ts dsh/tests/operational-error.test.ts dsh/tests/cli.test.ts dsh/tests/preset-manager.test.ts
git -C plugins commit -m "feat: add stable DSH operational diagnostics"
```

### Task 3: Missing Runtime Recovery

**Files:**
- Modify: `plugins/dsh/bin/anban-dsh.js`
- Modify: `plugins/dsh/tests/package.test.ts`

- [ ] **Step 1: Add a failing incomplete-install test**

Copy only the bin shim into a temporary package boundary, execute it, and require exit code 1 plus one safe stderr line containing `ERR_RUNTIME_MISSING`, `dsh/lib/cli.js`, and a tarball-based reinstall instruction. Add a second test proving unrelated import failures use `ERR_PRESET_OPERATION` and do not echo secrets.

- [ ] **Step 2: Verify the current generic message fails the contract**

Run: `cd plugins && pnpm vitest run dsh/tests/package.test.ts -t "missing CLI"`

Expected: FAIL because current stderr is only `anban-dsh: command failed`.

- [ ] **Step 3: Make the shim distinguish missing entrypoint from runtime failure**

Resolve `../lib/cli.js`, check it before import, emit the owned recovery message for `ENOENT`, and route all other failures through the same public operational format without printing a cause or stack by default.

- [ ] **Step 4: Run package shim tests**

Run: `cd plugins && pnpm vitest run dsh/tests/package.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/bin/anban-dsh.js dsh/tests/package.test.ts
git -C plugins commit -m "fix: diagnose incomplete DSH package installs"
```

### Task 4: Skills Provider Lifecycle

**Files:**
- Modify: `plugins/dsh/src/skills-provider.ts`
- Modify: `plugins/dsh/tests/skills-provider.test.ts`

- [ ] **Step 1: Add failing lifecycle tests**

Test readiness rejection with exactly one awaited dispose, readiness plus dispose rejection with a controlled public failure, successful readiness, normal cleanup, and two cleanup calls still causing one child disposal.

```ts
await expect(apply(context, config)).rejects.toThrow('startup failed')
expect(child.dispose).toHaveBeenCalledOnce()

const cleanup = await apply(context, config)
await cleanup()
await cleanup()
expect(child.dispose).toHaveBeenCalledOnce()
```

- [ ] **Step 2: Confirm startup cleanup currently fails**

Run: `cd plugins && pnpm vitest run dsh/tests/skills-provider.test.ts`

Expected: FAIL with zero disposal calls after readiness rejection and repeated disposal on duplicate cleanup.

- [ ] **Step 3: Implement exact-child cleanup**

Wrap readiness in `try/catch`, await disposal once on rejection, preserve readiness as primary when disposal succeeds, wrap dual failure in a secret-safe controlled error, and return an idempotent cleanup closure after successful startup.

- [ ] **Step 4: Run focused tests and typecheck**

Run: `cd plugins && pnpm vitest run dsh/tests/skills-provider.test.ts && pnpm run typecheck`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/src/skills-provider.ts dsh/tests/skills-provider.test.ts
git -C plugins commit -m "fix: dispose failed DSH skills children"
```

### Task 5: Cross-Process Preset Lock Primitive

**Files:**
- Create: `plugins/dsh/src/preset-lock.ts`
- Create: `plugins/dsh/tests/preset-lock.test.ts`

- [ ] **Step 1: Add failing lock unit tests**

Use injected filesystem, clock, wait, hostname, random owner id, and PID liveness boundaries. Cover exclusive acquisition, bounded contention timeout, exact-owner release, idempotent release, stale same-host dead-PID quarantine/reclaim, live PID, remote host, malformed owner JSON, permission errors, and ownership replacement before release.

```ts
const lock = await acquirePresetLock(root, dependencies)
expect(await readOwner(root)).toMatchObject({ schemaVersion: 1, ownerId: 'owner-a' })
await lock.release()
expect(await pathExists(join(root, '.anban-dsh.lock'))).toBe(false)
```

- [ ] **Step 2: Verify module absence**

Run: `cd plugins && pnpm vitest run dsh/tests/preset-lock.test.ts`

Expected: FAIL because `preset-lock.ts` does not exist.

- [ ] **Step 3: Implement the lock primitive**

Atomically create `<presetRoot>/.anban-dsh.lock`, write `owner.json` with schema version, PID, hostname, ISO creation time, package version, and cryptographically random owner id. On contention, wait/retry until deadline. Reclaim only a valid current-host dead PID by atomically renaming to a unique quarantine path before recursive removal. Reject malformed/remote/indeterminate locks with `ERR_PRESET_LOCK_INVALID` or `ERR_PRESET_LOCKED`. Release only when the stored owner id still matches.

- [ ] **Step 4: Run lock tests and typecheck**

Run: `cd plugins && pnpm vitest run dsh/tests/preset-lock.test.ts && pnpm run typecheck`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/src/preset-lock.ts dsh/tests/preset-lock.test.ts
git -C plugins commit -m "feat: add cross-process DSH preset lock"
```

### Task 6: Serialize Preset Mutations

**Files:**
- Modify: `plugins/dsh/src/presets.ts`
- Modify: `plugins/dsh/tests/presets.test.ts`
- Modify: `plugins/dsh/tests/preset-manager.test.ts`

- [ ] **Step 1: Add failing integration and two-process tests**

Assert status never acquires the write lock. Assert install and remove acquire before reading mutable state and release in `finally`. Spawn independent Node processes against one temporary `DSH_HOME` for install/install, install/remove, force/install, failed operation, and crash residue; require deterministic serialization and healthy ownership manifests.

- [ ] **Step 2: Reproduce the race under the new test**

Run: `cd plugins && pnpm vitest run dsh/tests/presets.test.ts -t "process|concurrent|lock"`

Expected: FAIL with an unguarded mutation, previously observed as `ENOTEMPTY rename`.

- [ ] **Step 3: Integrate lock and typed ownership errors**

Acquire one global lock around the complete install/remove transaction, then re-read status inside the lock. Map unowned, modified, rollback, and unknown operation failures to their approved codes. Keep status lock-free. Preserve all containment, symlink, atomic rename, backup, and rollback protections.

```ts
export async function installPresets(options: InstallOptions = {}) {
  const context = await publicContext()
  const lock = await acquirePresetLock(context.presetRoot, context.lock)
  try {
    return await installWithContext(context, options)
  } finally {
    await lock.release()
  }
}
```

- [ ] **Step 4: Run Preset suites repeatedly**

Run with Vitest 4.1.8:

```bash
cd plugins
for run in 1 2 3; do
  pnpm vitest run dsh/tests/preset-lock.test.ts dsh/tests/presets.test.ts dsh/tests/preset-manager.test.ts
done
```

Expected: PASS across all repetitions with no leftover `.anban-dsh.lock` except the explicit crash-residue fixture, which the next acquisition safely handles.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/src/presets.ts dsh/tests/presets.test.ts dsh/tests/preset-manager.test.ts
git -C plugins commit -m "fix: serialize DSH preset mutations"
```

### Task 7: Source And Packed-Artifact Integrity

**Files:**
- Create: `plugins/dsh/scripts/package-integrity.mjs`
- Modify: `plugins/dsh/tests/package.test.ts`
- Modify: `plugins/package.json`
- Modify: `plugins/pnpm-lock.yaml`

- [ ] **Step 1: Add failing manifest and tamper tests**

Require `prepack`, `verify:source`, `verify:pack`, and `check` scripts. Exercise source verification with a missing export/bin/patch/Preset manifest/Agent composition/declared Skill. Exercise packed verification with a missing file and an unexpected file. Assert all public exports import and the extracted CLI executes.

- [ ] **Step 2: Verify scripts are absent**

Run: `cd plugins && pnpm vitest run dsh/tests/package.test.ts`

Expected: FAIL because the integrity scripts and verifier do not exist.

- [ ] **Step 3: Implement deterministic integrity verification**

Read `package.json` structurally. Source mode validates every export target, bin, Bundle patch, Article/Seednote manifest, Agent composition, and all declared Skills. Packed mode consumes the `pnpm pack --json` file inventory, extracts to a temporary directory using an available system tar command, compares exact expected inventory, imports `anban-mcp`, `preset-manager`, and `skills-provider`, and invokes the extracted bin. Never parse human-readable pack output.

Set scripts to the equivalent of:

```json
{
  "prepack": "pnpm run build && pnpm run verify:source",
  "verify:source": "node dsh/scripts/package-integrity.mjs source",
  "verify:pack": "node dsh/scripts/package-integrity.mjs pack",
  "check": "pnpm run typecheck && pnpm run test && pnpm run verify:pack && pnpm run smoke:profile"
}
```

- [ ] **Step 4: Run package tests and real pack verification**

Run: `cd plugins && pnpm vitest run dsh/tests/package.test.ts && pnpm run verify:source && pnpm run verify:pack`

Expected: PASS; exactly one temporary tarball is inspected and removed.

- [ ] **Step 5: Commit**

```bash
git -C plugins add package.json pnpm-lock.yaml dsh/scripts/package-integrity.mjs dsh/tests/package.test.ts
git -C plugins commit -m "build: verify DSH package artifacts"
```

### Task 8: Portable Fresh-Profile Smoke

**Files:**
- Modify: `plugins/dsh/scripts/smoke-profile.mjs`
- Modify: `plugins/dsh/tests/profile-smoke.test.ts`

- [ ] **Step 1: Add failing portability and artifact-source tests**

Cover POSIX executable, Windows `.cmd` shim, plain command from PATH, standard Node lifecycle entrypoint, and pinned Desktop public-runtime fixture where `process.execPath` is Electron and `npm_execpath` points at the supported shim. Assert no Desktop-private imports, `desktopRuntime`, `desktopPnpmBootstrap`, or direct `ELECTRON_RUN_AS_NODE` use. Test both a local packed tarball and a registry specifier.

- [ ] **Step 2: Verify Desktop and structured-pack cases fail**

Run: `cd plugins && pnpm vitest run dsh/tests/profile-smoke.test.ts`

Expected: FAIL because current code couples `npm_execpath` to `process.execPath`, rejects Windows command shims, parses the last stdout line, and imports only one export.

- [ ] **Step 3: Implement public command construction and structured pack parsing**

Resolve `pnpm` from standard lifecycle/PATH data, represent Windows `.cmd` explicitly, and never treat Electron as Node. Invoke `pnpm pack --json --pack-destination <root>`, parse the single JSON result, validate its filename and inventory, and import every manifest export from the installed package. Accept an optional registry package spec so release verification can install `@anban/dsh-plugin@<version>` without repacking local source.

- [ ] **Step 4: Run unit and real profile smoke**

Run: `cd plugins && pnpm vitest run dsh/tests/profile-smoke.test.ts && pnpm run smoke:profile`

Expected: PASS and logs confirm Article/Seednote, one Bundle MCP row, one Preset manager row, no Preset-local MCP, and all public exports.

- [ ] **Step 5: Commit**

```bash
git -C plugins add dsh/scripts/smoke-profile.mjs dsh/tests/profile-smoke.test.ts
git -C plugins commit -m "test: make DSH profile smoke portable"
```

### Task 9: CI And Release Gates

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `server/agent/dsh_plugin_contract_test.go`

- [ ] **Step 1: Add failing Go workflow-contract tests**

Require CI OS command-shape coverage, package `check` including real smoke, release tag/version validation for every manifest, one structured/versioned tarball, npm publication authority, anonymous `npm view`, clean registry-profile smoke, SHA-256 generation, and exact verified tarball upload. Require a manual release failure when npm authority is missing; never make publication conditional success.

- [ ] **Step 2: Run the contract test and observe workflow gaps**

Run: `go test ./server/agent -run 'TestDSH.*(CI|Release|Package)' -count=1`

Expected: FAIL on absent publish/registry-smoke gates and incomplete version validation.

- [ ] **Step 3: Update workflows**

Make CI run locked install plus `pnpm run check` and explicit command-construction tests on Linux, macOS, and Windows. In release, use Node 24 and pnpm 11.19, validate tag against package/native/marketplace versions, pack once with JSON output, verify exact tarball, publish that file using repository-configured npm provenance/authority, query it anonymously, run registry smoke in a clean profile, checksum it, then attach that exact artifact. Keep GitHub Release and npm publishing as tag/manual operator actions only.

- [ ] **Step 4: Run workflow contracts**

Run: `go test ./server/agent -run 'TestDSH' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit parent workflow changes**

```bash
git add .github/workflows/ci.yml .github/workflows/release.yml server/agent/dsh_plugin_contract_test.go
git commit -m "ci: gate DSH package and registry releases"
```

### Task 10: Official DSH Documentation And Support Matrix

**Files:**
- Modify: `plugins/README.md`
- Modify: `plugins/docs/dsh-installation.md`
- Modify: `plugins/CHANGELOG.md`
- Modify: `plugins/dsh/tests/package.test.ts`
- Modify: `server/agent/dsh_plugin_contract_test.go`

- [ ] **Step 1: Add failing documentation-contract tests**

Require the four-surface support matrix, Article/Seednote-only DSH scope, npm and checksummed release tarball install, immutable Git source caveat, Web/Desktop discovery, explicit install/status/upgrade/rollback/remove, global `$DSH_HOME/.agent-presets` warning, two-step Bundle/Preset lifecycle, and official credential precedence. Require the exact persistent YAML key and POSIX `0600` guidance. Reject source-directory `file:` recommendations and claims that model onboarding configures arbitrary third-party credentials.

- [ ] **Step 2: Verify documentation contracts fail**

Run: `cd plugins && pnpm vitest run dsh/tests/package.test.ts && cd .. && go test ./server/agent -run TestDSH -count=1`

Expected: FAIL on missing distribution, support-matrix, credentials, and global-lifecycle guidance.

- [ ] **Step 3: Rewrite the DSH install guide and concise README entry**

Document the official precedence exactly:

```text
process environment (read-only) -> $DSH_HOME/.credentials.yaml -> invocation-project .env -> $DSH_HOME/.env
```

Show only the placeholder credential:

```yaml
ANBAN_API_KEY: <value>
```

Explain owner-only directory permissions, file mode `0600`, no automatic shared-Preset mutation during Bundle activation, cross-profile consequences, and recovery commands. State clearly that Skills-only installation reuses Skills but does not provide DSH Bundle/MCP/Presets, while full DSH support is Article/Seednote only.

- [ ] **Step 4: Run documentation contracts**

Run: `cd plugins && pnpm vitest run dsh/tests/package.test.ts && cd .. && go test ./server/agent -run TestDSH -count=1`

Expected: PASS with no secret-like fixture values.

- [ ] **Step 5: Commit documentation**

```bash
git -C plugins add README.md docs/dsh-installation.md CHANGELOG.md dsh/tests/package.test.ts
git -C plugins commit -m "docs: clarify official DSH installation lifecycle"
git add server/agent/dsh_plugin_contract_test.go
git commit -m "test: enforce DSH documentation contract"
```

### Task 11: Version 4.1.13 And Canonical Agent Pack Drift

**Files:**
- Modify: `plugins/package.json`
- Modify: `plugins/pnpm-lock.yaml`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.claude-plugin/marketplace.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `plugins/CHANGELOG.md`
- Modify: `server/agent/dsh_plugin_contract_test.go`
- Regenerate only if needed: `plugins/dsh/presets/article/**`
- Regenerate only if needed: `plugins/dsh/presets/seednote/**`

- [ ] **Step 1: Change the parent version contract first**

Set `dshPluginVersion = "4.1.13"` and require all four plugin version locations plus changelog heading to match.

- [ ] **Step 2: Confirm the contract fails against 4.1.11**

Run: `go test ./server/agent -run TestDSH -count=1`

Expected: FAIL with version mismatch.

- [ ] **Step 3: Bump all plugin metadata together**

Change package, lockfile importer, Claude manifest, Codex manifest, Claude marketplace entry, and changelog to `4.1.13`. Do not change unrelated plugin content.

- [ ] **Step 4: Generate and prove canonical Skill reuse**

Run: `make agent-pack-generate && make agent-pack-check && go test ./server/agent -run TestDSH -count=1`

Expected: PASS. `git -C plugins diff --exit-code -- dsh/presets/article/skills dsh/presets/seednote/skills` must show no unexplained authored divergence after generation.

- [ ] **Step 5: Commit synchronized version changes**

```bash
git -C plugins add package.json pnpm-lock.yaml .claude-plugin/plugin.json .claude-plugin/marketplace.json .codex-plugin/plugin.json CHANGELOG.md dsh/presets
git -C plugins commit -m "chore: release DSH plugin 4.1.13"
git add server/agent/dsh_plugin_contract_test.go
git commit -m "test: require DSH plugin 4.1.13"
```

### Task 12: Full Verification, Reviews, Gitlink, And Local Main Merge

**Files:**
- Modify: parent `plugins` gitlink
- Verify: all files changed by Tasks 1-11

- [ ] **Step 1: Run plugin verification from fresh output**

```bash
cd plugins
pnpm install --frozen-lockfile
pnpm run typecheck
pnpm run test
pnpm run build
pnpm run verify:source
pnpm run verify:pack
pnpm run smoke:profile
cd ..
```

Expected: every command exits 0; no lock residue or temporary tarball remains in the repository.

- [ ] **Step 2: Run repository verification**

```bash
make agent-pack-generate
make agent-pack-check
go test ./server/mcp -count=1
go test ./server/agent -count=1
go test ./...
go build -o /tmp/anban-creator-server ./server
git diff --check
git status --short
```

Expected: all tests/builds pass; status contains only intended commits plus the pre-existing unrelated untracked directories.

- [ ] **Step 3: Perform independent specification review**

Use `superpowers:requesting-code-review` with the approved design and this plan. The reviewer must classify findings as Critical, Important, or Minor and verify official DSH boundaries, security, release truthfulness, and test coverage. Fix every Critical/Important item with a regression test and repeat the relevant verification.

- [ ] **Step 4: Perform independent code-quality review**

Request a second review focused on correctness, concurrency, portability, cleanup, secret handling, and maintainability. Fix every Critical/Important item, rerun all targeted tests, then repeat both reviews until neither reports Critical/Important findings.

- [ ] **Step 5: Commit the plugin and parent gitlink**

```bash
git -C plugins status --short
git -C plugins log -1 --oneline
git add plugins
git commit -m "feat: harden DSH plugin distribution"
```

Expected: plugin submodule is clean on its reviewed commit and the parent commit records only the new gitlink plus intended parent changes.

- [ ] **Step 6: Re-run completion verification**

Use `superpowers:verification-before-completion`. Repeat the plugin `check`/smoke, Agent Pack check, full Go tests, server build, `git diff --check`, and status checks from fresh output. Do not claim npm availability because no publish operation has been authorized or executed.

- [ ] **Step 7: Merge the reviewed branch into local `main`**

Use `superpowers:finishing-a-development-branch`. Confirm local `main` has no conflicting user changes, merge `codex/dsh-plugin-reliability` non-interactively, and rerun a merge-boundary smoke plus the targeted Go contract. Do not push, tag, publish npm, or create a GitHub Release.

```bash
git switch main
git merge --no-ff codex/dsh-plugin-reliability
make dsh-plugin-smoke
go test ./server/agent -run TestDSH -count=1
git status --short --branch
```

Expected: local `main` contains the reviewed repair, verification passes, and only the preserved unrelated untracked directories remain.

---

## Self-Review Checklist

- [ ] Every confirmed defect in the approved design maps to a red test and implementation task.
- [ ] Article/Seednote remain the only DSH Presets; canonical `plugins/skills/**` ownership is unchanged.
- [ ] No task adds automatic Bundle-activation mutation, a second workflow engine, or Server-side orchestration.
- [ ] Release automation is implemented and testable, but publication/tagging remains operator-authorized.
- [ ] Error output is bounded, one-line by default, and never renders untrusted causes.
- [ ] Preset mutation is serialized across processes and status remains read-only.
- [ ] Web/Desktop portability uses public/standard runtime contracts only.
- [ ] Both native manifest versions move with plugin assets.
- [ ] Final integration requires two clean reviews and fresh verification evidence.
