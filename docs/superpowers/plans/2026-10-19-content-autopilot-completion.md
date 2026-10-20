# Content Autopilot Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the Server-owned, policy-driven content asset workflow so eligible assets can be scored, budgeted, produced in recoverable stages, reviewed, scheduled, published through existing Server services, and inspected in Studio.

**Architecture:** Keep the existing `ContentAssetService`, repository, Asynq worker, task lifecycle, billing wallet/catalog, analytics revisions, and publication services as the authoritative boundaries. Add deterministic policy/snapshot/state behavior in Server services and repositories; keep generation and semantic review in the existing Agent Packs; surface the persisted lineage and run state in Studio.

**Tech Stack:** Go, GORM, Asynq, existing billing and WeChat publication services, React 19, TypeScript, TanStack Query, Vitest.

**Spec:** User-approved “选题资产与跨平台全自动生产闭环” plan in the conversation; repository constraints in `AGENTS.md`, `docs/ANBAN-SKILL-SPEC.md`, and `harness/docs/plugin-development.md`.

## Global Constraints

- Server owns deterministic workflow state, identity, billing, scheduling, publication readiness, external publication, idempotency, retries, and reconciliation.
- Agent generates research, briefs, platform content, quality/compliance evidence, and files only; Agent logs never prove publication success.
- MCP remains atomic and stateless; it does not compose the end-to-end workflow.
- Analytics remain the fact source; scoring snapshots record platform-local evidence and revision without duplicating metric facts.
- Autopilot is disabled until a user policy enables it; automatic publication additionally requires explicit policy permission and existing Server readiness.
- Preserve existing task identity contracts and keep legacy topic-pool APIs compatible.
- User-facing terminology is “选题资产”; do not expose “母题”.

## Review Focus

- Concurrent/retried workers cannot create or charge duplicate work: test atomic claim, idempotency, and finalization replay.
- Unverified, stale, missing, or cross-platform metrics cannot yield a high-confidence recommendation: test insufficient evidence and platform-local scoring.
- Budget exhaustion or failed work cannot strand reserved credits: test reserve, settle, and reversal exactly once.
- A completed generation task cannot imply publication success: test readiness blocking, ambiguous publication, and reconciliation through existing durable evidence.
- User-controlled links cannot expose another user's task/publication: test ownership for each supported content kind.

---

### Task 1: Enforce Asset and Lineage Ownership

**Files:** `server/service/content_asset.go`, `server/service/content_asset_test.go`, relevant content repositories/models.

**Interfaces:** Existing `ContentAssetService` methods remain the public service API. Add a server-owned transition method for research/validation outcomes; client `Create`/`Update` may edit descriptive fields but cannot assign validated/ready or arbitrary lifecycle states.

- [ ] Add failing tests for rejected client status/lifecycle escalation and for ownership validation of task and publication links.
- [ ] Run the focused Go tests and confirm failures.
- [ ] Implement a finite asset transition table and content-kind-specific owner lookups; reject unknown kinds and cross-user references.
- [ ] Run `cd server && go test ./service ./repository`.

### Task 2: Persist Evidence-Based Platform Score Snapshots

**Files:** `server/model/content_asset.go`, `server/repository/content_asset.go`, `server/service/content_asset_scoring.go`, related tests and analytics repositories.

**Interfaces:** Add a server method that scores `(asset, target project, platform)` and persists an immutable snapshot containing score, confidence, evidence references, analytics revision/as-of, missing fields, reuse saturation, angle, cost, and risk. Client requests cannot submit this snapshot.

- [ ] Add failing tests for insufficient evidence, stale evidence, revision changes, and platform metric isolation.
- [ ] Run focused tests and confirm failures.
- [ ] Read existing platform analytics contracts and implement normalization only from that platform's own facts; return `data_insufficient` when required evidence is absent.
- [ ] Re-score when the referenced analytics revision changes and preserve prior snapshots.
- [ ] Run targeted Go tests.

### Task 3: Enforce Budget Reservation and Settlement

**Files:** `server/model/content_asset.go`, `server/repository/content_asset.go`, `server/service/content_asset.go`, billing integration tests.

**Interfaces:** Bind `ContentAssetService` to existing `BillingCatalogService` and `BillingWalletService`. A run stores quote/charge identity and reserved/cost credits; a deterministic reservation key makes admission and reversal idempotent.

- [ ] Add failing tests for daily and per-run caps, insufficient wallet credits, duplicate admission, successful settlement, and failure reversal.
- [ ] Run focused tests and confirm failures.
- [ ] Implement policy budget checks and use existing wallet/catalog operations; do not create a parallel balance ledger.
- [ ] Ensure a failed or cancelled pre-publication run releases its reservation once, while completed work settles actual cost.
- [ ] Run billing and content-asset service tests.

### Task 4: Run Policy-Driven Automatic Selection and Recovery

**Files:** `server/service/content_asset.go`, new focused scheduler service/tests, `server/scheduler/scheduler.go`, `server/main.go`, repository interfaces/tests.

**Interfaces:** Add a Server scheduler entrypoint that scans only enabled user policies and validated/ready assets, applies project/platform/score/confidence/budget/window filters, then creates idempotent reuse runs. Keep Asynq execution as the worker transport.

- [ ] Add failing tests for disabled policy, out-of-scope project/platform, low score/confidence, duplicate cron ticks, missing queue, and retry/recovery.
- [ ] Run focused tests and confirm failures.
- [ ] Implement bounded candidate selection, stable idempotency keys, finite retry/backoff, and explicit blocked/failed outcomes when no queue or eligible target exists.
- [ ] Wire scheduler lifecycle into Server startup/shutdown.
- [ ] Run scheduler and service tests.

### Task 5: Implement Cost-Tiered Production and Server-Owned Finalization

**Files:** `server/model/content_asset.go`, `server/service/content_asset.go`, `server/service/task_execution_complete.go`, existing publication service and their tests.

**Interfaces:** Reuse run stages represent L1 research/brief, L2 content/script, L3 assets/editing, L4 rendering, then review/schedule/publish/observe. Server advances only from persisted task artifacts/review/readiness; video rendering starts only after script success and policy budget/confidence gates.

- [ ] Add failing tests for script-before-render, renderer failure without repeat spend, task completion not equaling run completion, readiness denial, and pause/resume from the last durable stage.
- [ ] Run focused tests and confirm failures.
- [ ] Implement deterministic stage transitions and task/run binding with atomic compare-and-set updates.
- [ ] Route draft/publish/reconcile through existing Server publication services; treat ambiguous outcomes only according to durable publication evidence.
- [ ] Run task, publication, scheduler, and content-asset tests.

### Task 6: Complete Studio Recommendations and Lineage Views

**Files:** `studio/src/pages/ContentAssetsPage.tsx`, `studio/src/pages/DashboardPage.tsx`, `studio/src/lib/api/content-assets.ts`, `studio/src/types/content-asset.ts`, associated tests.

**Interfaces:** Consume persisted score snapshots, lineage, and reuse-run status from Server APIs; do not calculate authoritative recommendation scores in the browser.

- [ ] Add failing tests for recommended reuse list, evidence freshness, lineage details, run recovery point/error, and policy save preservation.
- [ ] Run focused Studio tests and confirm failures.
- [ ] Implement dense, responsive asset/run views with visible platform, angle, cost, publication state, evidence date, recommendation reason, and automation state.
- [ ] Run `cd studio && bun run test` and `cd studio && bun run build`.

### Task 7: Contract and Release Verification

**Files:** Any focused contract tests and harness artifacts only if workflow contracts change.

- [ ] Verify Harness outputs still match the content-reuse schema and no Agent publication side effects were added.
- [ ] Run `cd server && go test ./...`, `cd server && go build -o /tmp/anban-creator-server .`, `cd studio && bun run test`, `cd studio && bun run build`, `make agent-pack-check`, and `git diff --check`.
- [ ] Review the complete diff for plan coverage, user-worktree changes, and publication/billing boundary violations before integration.

## Review-driven completion sequence (2026-10-20)

The existing full-suite checkpoint verifies the implemented subset, not the whole specification. Continue in this dependency order; do not merge while any item remains open.

- [x] Correct admission and paid-operation capacity checks, actual wallet totals, cross-day budget attribution, policy pagination and paused-publication recovery. Scoped review passed after retaining capacity for ambiguous post-provider failures and adding durable-evidence reconciliation.
- [ ] Add explicit `content_research`, `content_validation`, and `content_script` task contracts under a managed `content-planning` Pack. Keep final platform task identities and full artifact requirements. Planning tasks use file-backed evidence and cannot generate paid media or publish.
- [ ] Connect enabled-policy discovery and candidate research to real planning executions. Verify external citations against execution-scoped successful capability receipts and internal sources against owned analytics. Apply semantic grouping suggestions through transactional Server validation and source deduplication.
- [ ] Support source-platform-normalized transfer recommendations and external-evidence exploration without claiming target performance. Select the latest valid snapshot per target; invalidate recommendations on contributing evidence/Profile/analytics changes.
- [ ] Persist stage/task/output/charge bindings and transactional audit events. Enforce accepted script before rendering, retain successful stages on failure, and recover from immutable artifacts on supported executors. Preserve frozen target identity and policy across stages.
- [ ] Connect publication observation to later analytics revisions while keeping feedback insights advisory until independently validated and confirmed.
- [ ] Complete persisted run management, filterable/paginated asset views, all content-detail lineage, platform-specific metrics and full automatic-production settings (including multiple windows and per-platform thresholds).
- [ ] Run all release checks and independent whole-feature review, resolve remaining blockers, then integrate into local `main` without changing unrelated worktrees.

A paid provider request with an unknown outcome must remain paused and retain capacity when the provider lacks a result lookup or idempotency contract. Durable successful result and settlement evidence can resolve that hold; elapsed time alone cannot authorize a repeated paid request. Existing manual tasks remain available when the optional planning runtime is not configured.
