# Project Creator Plugin Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans (recommended). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Project Creator DSH session load its two local policy plugins reliably with the published DSH runtime and preserve their scope to the `project-creator` preset.

**Architecture:** Keep local plugin files in the canonical `harness/dsh/plugins/` directory, but declare them in a top-level Cordis group so the current DSH patch loader anchors their relative paths beside the patch file. The plugins remain globally mounted as lightweight lifecycle observers and apply their restrictions or context injection only when the created Agent is bound to the `project-creator` preset.

**Tech Stack:** DSH Cordis patch YAML, JavaScript lifecycle plugins, Go Docker contract tests, Bun/Node runtime smoke tests.

**Spec:** `docs/superpowers/specs/2026-10-10-project-creator-interactive-design.md`

## Global Constraints

- Project Creator may use only `AskUserQuestion` and `mcp__creator__stage_project_creator_draft`.
- Child Agents never receive user-facing question or draft capabilities.
- Existing managed and interactive presets must not be restricted or receive Project Creator context.
- Relative local plugin paths must resolve from the shipped patch directory in both local and `/app` deployments.
- Do not revert unrelated existing changes in the `harness` submodule or generated catalog files.

### Task 1: Lock the failure and scope contracts

**Files:**
- Modify: `server/agent/docker_runtime_contract_test.go`
- Test: `runtime-dsh/test/dsh-bridge.test.ts`

**Interfaces:**
- The Docker contract test verifies the patch exposes one top-level custom-plugin group and still ships both plugin files.
- The Bridge test keeps patch resolution independent from the process working directory.

- [ ] **Step 1: Add a failing contract assertion** that rejects custom plugin rows nested directly under the `preset-project-creator` config and requires a top-level `project-creator-plugins` group with both plugin ids.
- [ ] **Step 2: Run the focused Go contract test** with `cd server && go test ./agent -run 'Test.*Docker|Test.*ProjectCreator' -count=1`; confirm it fails against the current nested patch.
- [ ] **Step 3: Add a Bridge test fixture assertion** that the configured patch and sibling plugin directory are present as the runtime package contract expects.
- [ ] **Step 4: Run the focused Bun test** with `cd runtime-dsh && bun test test/dsh-bridge.test.ts`; record the baseline result.

### Task 2: Move plugin loading to an anchored top-level group

**Files:**
- Modify: `harness/dsh/project-creator.patch.yml`

**Interfaces:**
- `preset-project-creator.config.plugins` contains only preset-owned DSH packages.
- A top-level `project-creator-plugins` group contains `project-creator-tool-restrictions` and `project-creator-context` with `./plugins/...` names, which the current DSH patch parser anchors beside the patch file.

- [ ] **Step 1: Remove the two local plugin rows from the nested preset plugin list.**
- [ ] **Step 2: Add the `project-creator-plugins` group after the preset declaration with the two local rows.**
- [ ] **Step 3: Run the focused Docker contract test and confirm the patch shape passes.**

### Task 3: Preserve preset isolation in the globally mounted observers

**Files:**
- Modify: `harness/dsh/plugins/project-creator-restrictions.js`
- Modify: `harness/dsh/plugins/project-creator-context.js`

**Interfaces:**
- Both plugins identify a live Agent's preset through `agent.ctx.get('agentPresets')?.composedPreset(agent.ctx)`.
- Only `project-creator` root Agents are affected; child Agents are never given the root capabilities or runtime snapshot.

- [ ] **Step 1: Add a failing unit case** showing a non-Project-Creator Agent is ignored by both observers.
- [ ] **Step 2: Implement the preset guard before installing restrictions or injecting context.**
- [ ] **Step 3: Keep the existing root/child checks and allowlist unchanged for Project Creator Agents.**
- [ ] **Step 4: Run the plugin unit tests in the harness submodule and verify the new isolation case passes.**

### Task 4: Prove the real startup path and package integrity

**Files:**
- Modify: `server/agent/docker_runtime_contract_test.go` only if the final package contract needs an additional assertion.
- No production code changes outside the patch and its two plugins unless the startup smoke test identifies a separate defect.

**Interfaces:**
- A DSH SDK `session/start` smoke path must report no `never started` diagnostics for the two local plugins when using the shipped patch.
- The image contract must continue copying `harness/dsh/project-creator.patch.yml` and both plugin files to matching `/app` paths.

- [ ] **Step 1: Run a local SDK startup smoke with a deterministic unavailable MCP endpoint and inspect that only the expected MCP connection failure remains.**
- [ ] **Step 2: Run `cd runtime-dsh && bun run typecheck` and `bun test test/dsh-bridge.test.ts`.**
- [ ] **Step 3: Run `cd server && go test ./agent ./service ./handler ./mcp` and `go build -o /tmp/anban-creator-server .`.**
- [ ] **Step 4: Run `git diff --check` and inspect the parent/submodule status without touching unrelated changes.**

## Review Gates

- The first gate is the patch-shape contract: no code behavior is changed until the test demonstrates the original unsupported nesting.
- The second gate is plugin isolation: moving the rows globally must not widen capabilities to other presets.
- The final gate is a real `session/start` smoke and package-path verification; an MCP failure is acceptable in the offline smoke only when both `never started` diagnostics are absent.
