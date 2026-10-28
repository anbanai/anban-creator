# Article Share Links Implementation Plan

**Goal:** 用户可为公众号文章和种草笔记创建有期限、始终读取最新交付的匿名阅读链接。

**Architecture:** Server owns share lifecycle and delivery access. Studio provides a management dialog and an isolated public reader. Reuse existing frozen delivery contracts and atomic delivery promotion.

**Spec:** `docs/superpowers/specs/2026-10-28-article-share-links-design.md`

**Global constraints:** Work only in the isolated `codex/article-share-links` worktree based on main `ad854ca1`. Preserve unrelated files. No Harness changes, new framework, downloads, analytics, passwords or publishing side effects. TDD for behavior. Use Bun in Studio. No subagents inside implementer/reviewer tasks.

### Task 1: Server sharing lifecycle and public content

Read the design spec for the exact behavior and HTTP contract. Implement only Server files for this task.

- Add `TaskShare` model, a task-share repository wired through regular and transaction repositories, AutoMigrate registration and additive SQL migration. Unique task and token indexes; no cleanup cron. Store a cryptographically random URL-safe token with enough entropy; never serialize raw model to API/logs.
- Add focused sharing service files: lifecycle/ownership and public delivery assembly. Follow existing service/repository patterns. Use task AgentID/Channel/TaskKind identity, not new compatibility fields. Scope to wechat-article and seednote content_generation.
- Active share edits retain token; expired/revoked share creation rotates token. Validate days and compute expiry using service time; allow a test clock using normal private service dependency pattern. Serialize concurrent management operations through existing task row lock/transaction patterns. Missing share status is none; deletion is idempotent.
- Read only current delivered files matching frozen public delivery contract. Require the correct main body, include only image/cover/tail delivery roles of the same execution, preserve sorted seednote images. Do not call GetVisibleFiles because it includes failed retained artifacts. Use bounded storage reads and streaming images through existing storage abstractions, covering both Local and OSS behavior. Avoid new outbound HTTP fetching.
- Resolve relative image references and known TaskFile platform/WeChat/storage URLs to the share image endpoint; parse HTML with existing x/net/html and Markdown with existing tooling as needed. Unknown image sources must not become publicly fetched URLs. Public payload excludes private task fields, raw storage URLs, prompts and internal files. Missing content/storage is distinguished from expired links.
- Add focused handler and wire owner/public routes through main/router. JSON and statuses exactly as spec. Public endpoints bypass auth, management endpoints require it. Every image request revalidates expiry/revocation/current-delivery ownership. Mark all share responses no-store/nosniff; public routes noindex/no-referrer. Redact token segments in the existing request logger without changing unrelated request logging.
- TDD: first add tests and run red, then implement. Tests cover expiry integer/range/edge, owner access, stable token on update, rotate on reenable, revoke, latest complete manifest, failed/pending/internal exclusion, deleted task, relative/platform image rewrite, foreign/retained image refusal, byte streaming and safe response contract, HTTP anonymous/owner routes and log redaction. Use real SQLite repositories and storage fakes/httptest where appropriate.
- Verify focused Go tests during development, then `go test ./...` and `go build -o /tmp/anban-share-server .` in server. Record pre-existing baseline failures separately; do not change unrelated code. Commit only Task 1 files and write report with RED/GREEN evidence and actual API shapes.

Interfaces produced are the five HTTP endpoints and JSON payloads in the spec. Frontend Task 2 consumes them. Test fixtures must include real frozen execution delivery contracts and use current repository identities.

### Task 2: Studio management and public reader

Read the design spec for exact behavior and HTTP payloads, and the Task 1 report for actual integration details. Implement only Studio files for this task.

- Add typed share API and types. Owner API uses current authenticated client; public reader uses anonymous fetch/client with the same configured API base and no refresh/login interceptors. Handle 404/410 as invalid link, 409 as unavailable content, 429/retry and network/500 as retryable errors.
- Add sharing dialog in the TaskDeliverablesPanel header near download. Only supported task kinds with delivered main body get initial creation; keep management reachable if an existing delivered version remains after failure. 1/7/custom days, default 7, integer 1–365 validation, create/copy/update/close actions, clear exact expiry, busy and retry states. Build full share URL from server share_path and Studio origin. A missing share is a valid none state, never auto-create just by opening dialog.
- Add lazy public `/share/:token` route outside ProtectedRoute, PublicRoute login redirects and AppLayout. Show title and complete content on a narrow mobile-friendly reading page. Seednote Markdown via existing react-markdown+remark-gfm without raw HTML; gallery order from response and avoid duplicating body images. Do not truncate using export helpers.
- Render article HTML with safe element/attribute/style cleanup and sandboxed no-script iframe or equivalent isolated reader; preserve useful inline article layout. Allow only URLs supplied by the current response for images; remove active content, dangerous styles, base/meta navigation and arbitrary external image sources. Prevent raw HTML response becoming page script. Show image loading errors with a reload action instead of silently dropping content. No download buttons.
- Add noindex/no-referrer metadata for share page and restore document metadata on navigation where relevant. Public request caching must not keep old/expired contents; evaluate expiry on load, refetch on revisit, and don't expose private workspace chrome when logged in.
- Update Nginx logging for `/share/` and `/api/v1/public/shares/` so raw tokens never enter access logs. Preserve SPA fallback and API proxy behavior. Avoid unrelated deployment edits.
- TDD: cover dialog create/copy/edit/revoke/reopen/validation/error, public anonymous route with valid and expired login state, both content types, full text, images/order/duplicates, expired/missing/failed requests and malicious content. Use MSW or existing test patterns at actual HTTP boundaries, not mock-only assertions.
- Run focused tests while developing, then `bun run test` and `bun run build`. Commit Task 2 files and write report with RED/GREEN evidence and any integration concerns.

Interfaces consumed: Task 1 endpoints. Do not change their wire contract unilaterally; notify the controller of mismatches. Use the task's current public type representation for display; backend remains authoritative for eligibility.

### Task 3: Integrated validation and completion

- Review the whole branch against the design and fix confirmed issues in scope with focused regression tests.
- Exercise public UI in a real browser without credentials, with a local fixture server for API responses if production credentials are unavailable. Cover valid article and seednote rendering, invalid link, phone viewport, and management flow. Fixtures and screenshots stay outside tracked source.
- Run final fresh Go/Studio tests and builds after relevant changes. Inspect diff/status, preserve unrelated main checkout files, and leave the implementation on the feature branch for review. Do not merge, push or deploy without user request.
- Report completed behavior, verified checks, actual limitations and migration requirement.

## Validation record

- Server feature tests pass across handler, service, repository, middleware and migration packages; Server binary builds successfully.
- Full Server test run matches the clean main baseline: 12 existing WeChat publication/reconciliation service failures, with no additional failures. The feature does not change those workflows.
- Studio final verification after review follow-up: 167 test files / 1502 tests pass and production build passes. Existing Vite native-config and chunk-size warnings remain.
- Browser smoke uses the real Studio implementation with local sample API responses: create a custom 30-day link, copy it, change to 7 days without changing the URL, revoke it, and verify anonymous invalid-link state. Both readers render complete text and images; mobile article scrolling reaches the final paragraph. Screenshots were saved outside tracked source for user confirmation.
- Real Nginx container regressions pass all 11 tests: configuration syntax, SPA fallback, public API proxy, privacy headers and token-free access logging, including canonical, mixed-case, encoded-character and encoded-slash share paths. Live expiry and BFCache restoration also have passing reader regressions.
- Final review fixes: owner sharing works with a separate API origin without expanding CORS permissions; application and Nginx logs redact equivalent encoded sharing paths. HTTP-boundary and actual-router regressions pass, and scoped re-review found no remaining issues.
- Release order: apply `server/migrations/20261028_task_shares.sql`, then deploy Server, then Studio. No push, merge or deployment was performed; the local feature branch remains available for review.

## UI follow-up

- Replaced the sharing dialog with an expandable card inside delivery results. The page stays interactive, with a full-width link, grouped expiry choices, sharing status, and responsive spacing. Existing create/copy/update/revoke behavior is retained.
- Public reader and error-page branding now uses `Anban AI`.
- Validation: 167 Studio test files / 1503 tests pass; production build passes. Browser checks cover expansion/collapse, creation, copying, custom expiry update, revocation, desktop layout and 390px mobile layout without horizontal overflow. Screenshots use local sample API data and are saved outside the repository for user confirmation.
