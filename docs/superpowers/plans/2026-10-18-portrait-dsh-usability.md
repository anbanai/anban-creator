# DSH Portrait Usability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the new DSH project-portrait flow recoverable, understandable, and stable for ordinary users while keeping `wechat-article` as the only article Pack identity.

**Architecture:** Fix the Pack renderer at its source and regenerate derived catalogs. Keep long-running DSH turns alive through the SDK contract, normalize profile data at the Studio boundary, and carry structured recommendation metadata through Server, bridge, and question UI. Preserve saved conversation state and ready drafts when a runtime session disappears, while leaving authoritative commit receipts on the Server.

**Tech Stack:** Go/Fiber services, TypeScript DSH bridge, React 19/Vite Studio, Vitest, Go tests, generated Agent Pack catalogs.

**Spec:** DSH portrait chat transcript and screenshots supplied by the user; repository contracts in `AGENTS.md`, `docs/ANBAN-SKILL-SPEC.md`, and `harness/docs/plugin-development.md`.

## Global Constraints

- `wechat-article` is the sole article Pack ID and renderer key; do not add an `article` alias or compatibility Pack.
- Harness remains responsible for non-deterministic generation; Server remains responsible for profile persistence, commit idempotency, and durable outcomes.
- Runtime executions must not modify project profiles, prompts, or active generation context outside the Server-owned commit path.
- Any Pack asset change updates both native plugin manifest patch versions and regenerated Pack/catalog outputs.
- Recommendations are advisory metadata; users retain custom and skip paths and no recommendation is treated as a fact.
- Existing uncommitted user changes remain intact.

## Review Focus

- A stale or lost runtime session must not erase the locally saved conversation or ready draft.
- A profile payload containing `null` arrays or missing dimensions must render as an incomplete profile instead of throwing.
- A long answer turn must not fail because of a short SDK request timeout.
- An option recommendation must survive transport parsing and remain visibly optional.
- Generated assets must agree on `wechat-article` and pass catalog drift checks.

### Task 1: Correct the WeChat Article renderer identity

**Files:**
- Modify: `harness/packs/wechat-article/agent-pack.yaml`
- Modify: `harness/.claude-plugin/plugin.json`
- Modify: `harness/.codex-plugin/plugin.json`
- Generate: `server/agentpack/catalog.generated.json`, native Agent/DSH generated assets as required by the Make target
- Test: `server/agentpack` generation/check commands

- [ ] Change `ui.renderer` from `custom:article` to `custom:wechat-article`, bump both plugin manifests by one patch version, and regenerate derived assets.
- [ ] Run `make agent-pack-generate` and `make agent-pack-check`; confirm no `custom:article` remains in generated Pack metadata.

### Task 2: Reject DSH runtime deadlines shorter than the idle lease

**Files:**
- Modify: `server/config/config.go`
- Test: `server/config/project_memory_config_test.go`

- [ ] Add a failing configuration test that rejects an interactive runtime maximum lifetime equal to or shorter than the configured idle timeout.
- [ ] Run the focused config test and observe the missing cross-field validation.
- [ ] Validate the container/Kubernetes DSH runtime maximum lifetime is strictly greater than its idle timeout; keep the existing 24-hour default.
- [ ] Run the focused config tests and `go test ./config`.

### Task 3: Normalize incomplete project profiles at the Studio boundary

**Files:**
- Modify: `studio/src/types/project.ts` or add a focused profile normalization module under `studio/src/lib`
- Modify: `studio/src/components/projects/ProfileDimensionCard.tsx`
- Modify: `studio/src/components/projects/ProjectMemoryDialog.tsx`
- Modify: `studio/src/pages/ProfileLabPage.tsx`
- Test: `studio/src/components/projects/ProfileDimensionCard.test.tsx`, `studio/src/components/projects/ProjectMemoryDialog.test.tsx`, relevant ProfileLab tests

- [ ] Add failing tests with `null` `sources`, `evidence`, `missing_fields`, `follow_up_questions`, empty content, and missing dimensions; assert the UI renders placeholders and no exception.
- [ ] Run the focused tests and verify the null dereference failure.
- [ ] Implement one shared `normalizeProjectProfile`/dimension boundary that supplies `{}` content, empty arrays, and all six named dimensions, then use it for API/query data before rendering.
- [ ] Keep editable JSON and revision behavior unchanged for valid profiles.
- [ ] Run focused Studio tests and the full Studio test suite.

### Task 4: Carry structured recommendation metadata through question flow

**Files:**
- Modify: `server/service/interactive.go`
- Modify: `server/mcp/interactive_tools.go` if schema exposure is required
- Modify: `runtime-dsh/src/dsh-bridge.ts`
- Modify: `studio/src/lib/api/interactive.ts`
- Modify: `studio/src/components/dsh-user-questions/DshQuestionComposer.tsx`
- Modify: `studio/src/pages/ChatPage.tsx`
- Modify: `studio/src/components/dsh-user-questions/DshQuestionComposer.test.tsx`, `server/service/interactive_test.go`, relevant ChatPage tests
- Modify: `harness/packs/project-creator/agent.dsh.yml`, `agent.claude.md`, `agent.codex.toml`, and discovery Skill if needed

- [ ] Add failing tests for `recommended: true` on one option, preservation through event/API parsing, default selection of the structured recommendation, and continued support for label suffix parsing.
- [ ] Run focused tests and verify the new metadata is dropped or absent.
- [ ] Add optional `Recommended bool json:"recommended,omitempty"` to the shared option contract and preserve it in the bridge and Studio parsers.
- [ ] Render a concise recommendation marker and rationale from `description`; preserve “其他/自定义” and “稍后补充/跳过” behavior.
- [ ] Update Project Creator instructions to recommend one context-specific option only when the user asks or is stuck, explain why, and avoid fixed business templates.
- [ ] Regenerate/check Pack assets and run focused Server, runtime, and Studio tests.

### Task 5: Make lost-session recovery explicit and draft-safe

**Files:**
- Modify: `studio/src/pages/ChatPage.tsx`
- Modify: `studio/src/lib/api/interactive.ts` only if a typed error/status helper is needed
- Test: `studio/src/pages/ChatPage.test.tsx`

- [ ] Add failing tests for a persisted conversation with a ready draft followed by a `session not found` response; assert the saved transcript/draft remains visible and the user receives separate recovery actions.
- [ ] Run the focused ChatPage tests and verify the current “重新开始” dead end.
- [ ] Handle an authoritative session-not-found response by clearing only the unusable runtime session ID, retaining messages, answers, and ready draft, and showing “恢复已保存草稿” versus “重新开始” actions.
- [ ] Ensure a new session uses a fresh idempotency key and does not claim to restore missing runtime context; commit remains guarded by the existing Server confirmation token and idempotency receipt.
- [ ] Run focused and full Studio tests.

### Task 6: Verify projection and end-to-end contracts

**Files:**
- Test/modify only where needed: `server/service/interactive_test.go`, `server/handler/interactive_test.go`, `server/mcp/interactive_tools_test.go`

- [ ] Add or strengthen tests proving a successful new/update commit writes all six profile Markdown dimensions plus `AGENTS.md`, and a projection failure remains retryable through the durable receipt.
- [ ] Run `cd server && go test ./...` and `go build -o /tmp/anban-creator-server .`.
- [ ] Run `cd runtime-dsh && npm run typecheck && npm test && npm run build`.
- [ ] Run `cd studio && bun run test && bun run build`.
- [ ] Run final `make agent-pack-check` and review `git diff` for unrelated changes or obsolete `article` references.
