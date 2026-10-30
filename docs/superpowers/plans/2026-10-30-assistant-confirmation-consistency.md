# Assistant confirmation consistency implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development for scoped implementation and review.

**Goal:** A creation draft has the same eligibility and recovery behavior in Agent staging, Server snapshots/commit, and Studio.
**Architecture:** Server validates and owns state transitions; Studio consumes authoritative draft views and commits the viewed version. Existing durable task/receipt protections remain authoritative.
**Tech Stack:** Go/Fiber/GORM, React/TypeScript/Vitest, harness Markdown and manifests.
**Spec:** docs/superpowers/specs/2026-10-30-assistant-confirmation-consistency-design.md

## Global Constraints

- Work only in the attached worktree on `codex/assistant-confirmation-consistency`; do not push or deploy.
- Surgical changes, no unrelated refactors. Preserve profile/review behavior and existing external side-effect ownership.
- Write meaningful failing tests before behavior changes; record RED/GREEN evidence.
- Use the shared contract in the spec verbatim; report a required adjustment before changing it.
- No implementation subagent may dispatch other agents. Commit only task-owned files, never another worker's files.

### Task 1: Server-owned validation and durable confirmation transitions

**Files:** server/service/assistant_interaction_{contract,stage,commit}.go, server/service/assistant_interaction.go, server/service/assistant_commit.go, server/repository/assistant_interaction.go, server/handler/assistant_interaction.go, route registration, and corresponding tests. One focused new service validation file is appropriate. Add repository methods/tests only as needed for atomic transitions. MCP only encodes one service result.

**Interfaces:** implement the spec's blocking issue fields, errors, authoritative stage draft, snapshot actions, and POST continue endpoint. Keep existing public result fields. Creation-only content rules must not unintentionally validate profile/review payloads as creation.

- [ ] Run focused baseline interaction/repository tests.
- [ ] Add failing service tests for missing-input ready submissions, incomplete/malformed payload distinction, and identical readiness across stage/SSE/snapshot/commit.
- [ ] Add failing tests for exact latest-version confirmation, durable continue across refresh/runtime loss, and database-fenced competing continue/commit/stage operations.
- [ ] Add failing tests for legacy conflict receipt reclassification, corrected revisions recovering workflow, and a valid creation producing/replaying exactly one task with approved placeholders and final options preserved.
- [ ] Implement the smallest shared validation and state transitions. Snapshot reads may derive corrected readiness for historical data; do not rewrite Agent content or automatically create tasks.
- [ ] Retain precise business errors and durable uncertain outcome handling. Successful receipt replay remains authoritative even after draft expiration. Ensure a known failed preflight does not become a pending commit that blocks correction.
- [ ] Run focused tests, full `go test ./...`, build `go build -o /tmp/anban-confirmation-server .`, and targeted MCP boundary tests. Record any baseline environmental failure separately.
- [ ] Self-review and commit Server changes; write task report including exact DTOs and observed checks.

### Task 2: Studio consumes authoritative state and confirms the displayed draft

**Files:** studio/src/lib/api/assistant.ts and tests, studio/src/lib/assistant-interaction-state.ts and tests, studio/src/pages/AssistantWorkspace.tsx and tests, studio/src/components/assistant/AssistantRecoveryBanner.tsx and tests as needed.

**Interfaces:** consume Task 1 contract and continue endpoint. `blocking_issues` carries code/field/message. Missing inputs/invalid errors allow editing; stale version requests reload; uncertain outcomes preserve reconciliation. Server-supplied available_actions and effective ready drive UI. Existing profile/review flows must retain behavior.

- [ ] Run focused baseline tests.
- [ ] Add failing tests: incomplete draft shows issues and allows discussion; refresh/SSE cannot revive invalid or superseded confirmation; true ready draft offers creation.
- [ ] Add failing API/UI tests: submit the viewed draft_id/revision directly, never substitute a later snapshot; idempotency tracks final options and is retained for network uncertainty.
- [ ] Add failing tests: continue calls Server and applies returned snapshot; refresh stays in editing; failure preserves draft/composer/answers and does not locally claim success.
- [ ] Implement narrow changes. Distinguish saving failures, missing inputs, true version conflicts, and reconciliation in controls and messages. Avoid locking composer simply because a draft object exists.
- [ ] Run full `bun run test` and `bun run build`. Self-review and commit Studio changes; write task report.

### Task 3: Planner guidance and distribution

**Files:** harness/skills/creation-planning/SKILL.md, owning pack source only if needed, generated distribution outputs, synchronized version metadata/CHANGELOG required by harness guide, and root harness gitlink.

**Interfaces:** missing_inputs means blockers only; effective readiness returned by Server is authoritative. Agent preserves approved optional inputs in existing constraints, handles staged non-ready results and issues, and only announces ready after a ready service result.

- [ ] Read docs/ANBAN-SKILL-SPEC.md and harness/docs/plugin-development.md; inspect harness-local instructions and version scripts.
- [ ] Update canonical planner guidance with a clear user-approved-placeholder example and blocking-input example. Avoid new fields or a second workflow/state machine.
- [ ] Use existing generation/version tooling and update both native manifest versions and other required metadata consistently. Keep upstream humanizer untouched.
- [ ] Run appropriate pack/generation/DSH checks and affected MCP contract tests. Record unavailable external validation accurately.
- [ ] Commit harness changes on a new task branch, then commit the root gitlink. Do not push either repository.

### Final verification

- [ ] Whole-branch review against the spec; resolve actionable findings and verify affected changes.
- [ ] Confirm required full Go and Studio tests/builds ran on the final affected code, and inspect `git diff --check`, status, branches, and submodule state.
- [ ] Report local branch/worktree, checks, and deployment limitation. No production success claim without deployment and browser verification.
