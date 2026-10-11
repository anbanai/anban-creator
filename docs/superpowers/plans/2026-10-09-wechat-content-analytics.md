# WeChat Content Analytics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with verification checkpoints.

**Goal:** Add trustworthy WeChat account-level daily and traffic-source analytics to the existing content analytics page while preserving the current per-content and Seednote behavior.

**Architecture:** Keep generic cumulative content observations and projections unchanged. Add a WeChat-owned account fact model with one import batch containing account-day metrics and source-day read facts, expose read-only account overview and traffic-source services under `/projects/:id/wechat-analytics`, and extend the existing WeChat analytics page with account overview/trend/source sections while retaining its article table/detail flow. Account facts use the existing project analytics revision and import lifecycle, but are never inferred from article snapshots.

**Tech Stack:** Go, Fiber v3, GORM, SQLite/MySQL migrations, React 19, TypeScript, TanStack Query, Recharts, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-09-wechat-content-analytics-design.md`

## Global Constraints

- Account metrics are WeChat-only and must not change Seednote contracts or UI.
- `null` means unavailable; `0` means an observed zero; incomplete coverage is never filled or interpolated.
- Account totals are sourced only from account-day facts; never sum cumulative content observations to create them.
- Traffic-source shares are `null` unless source totals reconcile with a covered account metric denominator.
- All dates are inclusive Asia/Shanghai calendar dates.
- Import, revoke, idempotency, winner selection, and revision changes are server-owned and transactional.
- Follow repository TDD requirements: write a failing test, run it, then implement the smallest passing change.
- Do not edit Seednote-specific components or add compatibility fields outside the typed contracts below.

### Task 1: Model, migration, and repository contract

**Files:**
- Create: `server/model/wechat_account_analytics.go`
- Create: `server/repository/wechat_account_analytics.go`
- Create: `server/migrations/20261009_wechat_account_analytics.sql`
- Create: `server/migrations/wechat_account_analytics_contract_test.go`
- Modify: `server/model/model.go`
- Modify: `server/repository/repository.go`

**Interfaces:**
- `model.WechatAccountAnalyticsBatch`, `model.WechatAccountAnalyticsDay`, `model.WechatTrafficSourceDay` persist immutable batch facts and nullable metrics.
- Repository methods create/list/revoke batches, insert facts, lock a project, select deterministic winners, and expose the current analytics revision.

- [ ] **Step 1: Write failing persistence contract tests** covering nullable-versus-zero metrics, source enum normalization storage, unique `(project_id, stat_date, batch_id, source)` facts, and migration/AutoMigrate table registration.
- [ ] **Step 2: Run `cd server && go test ./migrations ./repository -run WechatAccountAnalytics -count=1` and verify the new symbols/table checks fail.
- [ ] **Step 3: Add typed models and repository interfaces/implementation.** Store account metrics as nullable `*int64`, source values as normalized enums plus original label, and preserve raw row JSON/provenance on the batch/fact. Add winner ordering by active batch, effective time, received time, then stable fact ID.
- [ ] **Step 4: Add the SQL migration and `AutoMigrate` registrations with project/date indexes and the batch revoke index.
- [ ] **Step 5: Run the focused migration/repository tests and then `go test ./server/...` equivalent from `server` until green.

### Task 2: Official report parser and account import lifecycle

**Files:**
- Create: `server/service/wechat_account_analytics_import.go`
- Create: `server/service/wechat_account_analytics_parser.go`
- Create: `server/service/wechat_account_analytics_import_test.go`
- Create: `server/service/wechat_account_analytics_parser_test.go`
- Modify: `server/service/wechat_analytics_import.go`
- Modify: `server/router/router.go`
- Modify: `server/main.go`

**Interfaces:**
- `ParseWechatAccountAnalyticsWorkbook(data []byte, location *time.Location) (*ParsedWechatAccountAnalyticsWorkbook, error)` recognizes only documented official headers and returns coverage, account rows, source rows, unknown headers, and row errors.
- `WechatAccountAnalyticsImportService.Preview/Import/List/GetBatch/Revoke` accept the existing upload/idempotency request shape and return a revisioned receipt.

- [ ] **Step 1: Write parser tests first** for date recognition, supported account metrics, source normalization (`消息`, `朋友在看`, `朋友圈`, `搜一搜`, `其他`), null versus explicit zero, unsupported layouts, negative values, duplicate dates, and mismatched source totals.
- [ ] **Step 2: Run the parser tests and confirm failure because the parser/service does not exist.
- [ ] **Step 3: Implement strict XLS/XLSX parsing using the existing workbook reader conventions.** Do not guess unknown columns; report them in preview. Reject invalid rows and unsupported layouts before persistence.
- [ ] **Step 4: Add service tests for transactional import, idempotent replay/conflict, deterministic same-date winner, revoke restoration, and revision increments. Run them red before implementing service methods.
- [ ] **Step 5: Implement import/revoke using the existing upload session, repository transaction, `AnalyticsService` revision gate, and raw provenance limits. Account reports do not require article matching.
- [ ] **Step 6: Register project-scoped preview/import/list/detail/revoke routes and service wiring; run focused handler/router tests.

### Task 3: Account overview and traffic-source query services

**Files:**
- Create: `server/service/wechat_account_analytics.go`
- Create: `server/handler/wechat_account_analytics.go`
- Create: `server/service/wechat_account_analytics_test.go`
- Create: `server/handler/wechat_account_analytics_test.go`
- Modify: `server/router/router.go`
- Modify: `server/main.go`

**Interfaces:**
- `GET /projects/:id/wechat-analytics/overview?from&to` returns `revision`, inclusive range, `data_through`, covered dates, daily series, nullable period totals, and qualified day-over-day/week-over-week comparisons.
- `GET /projects/:id/wechat-analytics/traffic-sources?from&to&metric` returns `revision`, metric, total, coverage, data-quality reason, and ranked `{source,label,value,share}` rows.
- `WechatAccountAnalyticsService.Overview` and `.TrafficSources` execute both reads inside one repeatable-read repository snapshot.

- [ ] **Step 1: Add failing service tests** for complete and incomplete ranges, previous-window comparisons, zero denominator, null metrics, source mismatch, and deterministic winner selection.
- [ ] **Step 2: Run the tests and verify expected failures.
- [ ] **Step 3: Implement date-window helpers for yesterday, 7-day, 30-day, and custom equal-length comparisons in Asia/Shanghai. Return exact coverage dates and omit comparisons when either window is incomplete or prior value is zero.
- [ ] **Step 4: Implement source aggregation and share qualification. Sum daily account facts only; expose `data_quality` when source totals do not reconcile.
- [ ] **Step 5: Add Fiber handlers with UUID/auth/query validation and explicit 400/403/409/503 mappings. Add route wiring and handler tests for response shape and revision behavior.

### Task 4: Studio WeChat account analytics contract and UI

**Files:**
- Create: `studio/src/lib/wechat-account-analytics.ts`
- Create: `studio/src/components/analytics/WechatAccountOverview.tsx`
- Create: `studio/src/components/analytics/WechatTrafficSources.tsx`
- Create: `studio/src/components/analytics/WechatAccountAnalytics.test.tsx`
- Modify: `studio/src/pages/ContentAnalyticsPage.tsx`
- Modify: `studio/src/lib/api/index.ts` only if the existing export barrel requires it.

**Interfaces:**
- Typed client methods `wechatAccountAnalyticsApi.overview(projectId, {from,to})` and `.trafficSources(projectId, {from,to,metric})` consume the server contracts without client-side rollups.
- The page renders period tabs, coverage/data-through status, nullable comparison cards, daily trend metric/source controls, ranked source rows, and existing article table/detail for `platform=wechat`; `platform=seednote` remains on its existing branch.

- [ ] **Step 1: Write Vitest tests for period URL state, account query calls, incomplete/empty/error states, zero-versus-unavailable rendering, source filtering, and article table preservation. Run them red.
- [ ] **Step 2: Implement the typed API and minimal components using the existing cards, tabs, buttons, table, Recharts, and query patterns. The client must display server-provided shares/comparisons and must never derive totals.
- [ ] **Step 3: Integrate the WeChat sections above the existing per-content section and preserve content deep links, import/history controls, and Seednote rendering. Run focused Studio tests.

### Task 5: Verification and visual QA

**Files:**
- Modify only files discovered by failing verification; no unrelated refactors.

- [ ] **Step 1: Run `cd server && go test ./...`.
- [ ] **Step 2: Run `cd server && go build -o /tmp/anban-creator-server .`.
- [ ] **Step 3: Run `cd studio && bun run test` and `cd studio && bun run build`.
- [ ] **Step 4: Start the Studio dev server on an available port and verify the WeChat analytics page at desktop and mobile widths with covered, gapped, zero, and source-mismatch fixtures; record any limitations.
- [ ] **Step 5: Review `git diff` to ensure only the WeChat account analytics scope and the plan/spec files changed; leave existing Seednote and unrelated worktree edits intact.
