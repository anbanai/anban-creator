# Topic Radar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver the selected-project topic radar end to end: durable subscriptions and digests, Agent artifact finalization, automatic TopicPool adoption, independent short-video script jobs, webhook notifications, APIs, and Studio workflows.

**Architecture:** The Server owns identity, frozen snapshots, persistence, state transitions, scheduling, validation, idempotency, retries, adoption, scripts, and notifications. The managed `topic-radar` Agent receives frozen context and writes file-backed artifacts; it never mutates authoritative state or calls providers directly. Studio consumes typed Server read models and exposes results, provenance, automatic adoption state, scripts, and webhook configuration.

**Tech Stack:** Go Fiber, GORM, Asynq, standard `net/http`/`encoding/json`; React 19, TypeScript, Vite, TanStack Query; canonical Harness Packs and Skills.

**Spec:** `docs/superpowers/specs/2026-10-31-topic-radar.md`

## Global Constraints

- Agent identity/channel: `topic-radar` / `topic-radar`.
- Digest task kind/type: `topic_digest` / `topic-digest`; script task kind/type: `topic_script` / `topic-script`.
- Scheduled occurrence key is `sched:<subscription_id>:<planned RFC3339 UTC time>`; manual runs use UUID or client idempotency key.
- One queued/running digest per subscription; manual runs have no daily or billing cap; explicit retry creates a new run.
- Generated suggestions automatically create durable adoption outbox rows and enter TopicPool; Studio has no adoption checkbox/action.
- MVP notifications are webhook-only Feishu, WeCom, and DingTalk; secrets never leave the Server.
- No external enqueue, runtime dispatch, or webhook call occurs inside a DB transaction.

## Review Focus

- Concurrent manual/scheduled creation must yield one active digest and preserve durable idempotency.
- Missing or unresolvable source references must fail finalization without persisting unusable suggestions.
- Duplicate TopicPool titles must become terminal `duplicate` adoptions linked to the canonical row.
- Script and notification failures must remain independently retryable after digest success.
- Invalid webhook configuration must be terminal while network, 429, and 5xx failures retry with bounded recovery.

### Task 1: Server Subscription And Digest Lifecycle

**Files:** Create `server/service/topic_radar.go`, `server/service/topic_radar_test.go`; modify `server/model/topic_radar.go`, repository interfaces as needed, scheduler constants, and `server/main.go` wiring.

**Interfaces:** `TopicRadarService.CreateSubscription`, `ListSubscriptions`, `UpdateSubscription`, `SetSubscriptionEnabled`, `Run`, `Retry`, `GetDigest`, and `ListDigests`; all methods accept `context.Context` and enforce user/project ownership. `Run` creates a durable queued digest and returns the existing active digest on the concurrent guard, while a duplicate occurrence/request key is idempotent.

- [x] Write RED tests for subscription defaults/validation, manual idempotency, active-run guard, scheduled UTC occurrence normalization, and explicit retry creating a new request key.
- [x] Implement the minimal service and repository operations, including frozen input snapshot/hash and transaction-scoped durable creation followed by transport enqueue.
- [x] Add scheduler type/handler registration for topic digest dispatch and recovery hooks.
- [x] Run focused service/repository tests and the relevant scheduler tests.

### Task 2: Digest Artifact Finalization And Adoption Worker

**Files:** Create `server/service/topic_radar_finalizer.go`, `server/service/topic_radar_adoption.go`, tests; modify task execution finalization hooks and topic repositories.

**Interfaces:** `FinalizeDigest(ctx, digestID, artifactBytes) error`, `ProcessAdoption(ctx, adoptionID) error`, `ReconcileAdoptions(ctx, now, limit) error`.

- [x] Write RED tests for schema/source validation, rank persistence, adoption outbox creation, lease/CAS, duplicate TopicPool adoption, and retry after transient failure.
- [x] Validate source refs against the frozen input, reject candidates without resolvable provenance, persist suggestions and durable adoptions, and transition digest independently of adoption outcome.
- [x] Implement lease-based `AddWithSource` adoption with terminal `adopted`/`duplicate` states and recoverable failures.
- [x] Run focused tests plus existing TopicPool regressions.

### Task 3: Independent Short-Video Script Jobs

**Files:** Create `server/service/topic_script.go`, tests; modify scheduler/task identity and API read models.

**Interfaces:** `RequestScript`, `RetryScript`, `GetScriptJob`, `ProcessScriptArtifact`.

- [x] Write RED tests for request fingerprint idempotency, target variants, dedicated task identity, and digest success surviving script failure.
- [x] Implement durable script job creation after suggestion ownership validation, enqueue only after commit, and artifact/status finalization with independent retry state.
- [x] Run focused tests and scheduler contract tests.

### Task 4: Webhook Notification Outbox

**Files:** Create `server/notification/*.go`, `server/service/topic_notification.go`, tests; modify config, scheduler, and main wiring.

**Interfaces:** provider validation/rendering for Feishu, WeCom, DingTalk; `CreateChannel`, `UpdateChannel`, `ListChannels`, `DeleteChannel`, `TestChannel`, `EnqueueDigest`, `ProcessDelivery`, `ReconcileDeliveries`.

- [x] Write RED tests for immutable payload rendering/source attribution, WeCom 4096 UTF-8 byte limit, invalid configuration, provider retry classification, delivery uniqueness, lease recovery, and secret-safe read models.
- [x] Implement provider adapters with standard HTTP/JSON, durable payload snapshots, claim/send/complete/retry flow, and bounded recovery.
- [x] Add configuration validation and scheduler reconciliation after durable rows exist.
- [x] Run notification and scheduler tests.

### Task 5: HTTP API And Router Integration

**Files:** Create `server/handler/topic_radar.go`, `server/handler/notification.go`; modify `server/router/router.go`, `server/main.go`; add handler tests.

**Interfaces:** `/api/v1/projects/:project_id/topic-radar/subscriptions`, digest list/detail/run/retry, suggestion/script read models, and `/api/v1/projects/:project_id/topic-radar/notification-channels` CRUD/test.

- [x] Write RED Fiber tests for ownership, validation, no-secret responses, automatic-adoption read models, and route status codes.
- [x] Implement thin handlers delegating all policy to services and register routes under authenticated API v1.
- [x] Run handler/router tests.

### Task 6: Harness Pack And Agent Artifacts

**Files:** Create `harness/packs/topic-radar/agent-pack.yaml`, `harness/agents/topic-radar.md`, `harness/agents/topic-radar.toml`, `harness/skills/topic-digest/SKILL.md`, `harness/skills/short-video-script/SKILL.md`; modify both plugin manifests.

**Interfaces:** stable JSON artifacts `output/topic-digest.json`, optional evidence, and `output/topic-script.json`, with schema version, source refs, data timestamps, missing fields, quality status, and no Server-side mutation instructions.

- [x] Read `docs/ANBAN-SKILL-SPEC.md` and `harness/docs/plugin-development.md`; write contract tests/fixtures RED first.
- [x] Implement canonical Pack/Agent/Skills, update both manifest patch versions, and add MCP contract coverage.
- [x] Run targeted `server/mcp` tests and pack checks.

### Task 7: Studio Topic Radar And Settings

**Files:** Create `studio/src/pages/TopicRadarPage.tsx`, topic-radar components/API modules; modify routes, navigation, Dashboard, TopicPool, Settings, query keys; add tests.

**Interfaces:** typed query/mutation clients for subscription/digest/script/channel read models; no adoption selection control; provenance and source explanation visible in Studio and notification previews.

- [x] Write RED tests for route rendering, automatic adoption status, source display, script request/retry, and secret-free channel editing.
- [x] Implement dense project-scoped radar page, timeline/results/script drawer, TopicPool provenance, Dashboard summary, and Settings webhook management.
- [x] Run Studio tests and build.

### Task 8: Full Verification And Review

- [x] Run `cd server && go test ./...`.
- [x] Run `cd server && go build -o /tmp/anban-creator-server .`.
- [x] Run `cd studio && bun run test` and `bun run build`.
- [x] Run relevant MCP/Pack contract tests and inspect all diffs for scope, secret handling, transaction boundaries, and lifecycle independence.
- [x] Request a fresh whole-branch code review, fix material findings, and repeat verification.

## Authorized Pre-merge Review Follow-up

- [x] Preserve populated legacy TopicPool tables during dedupe migration, including duplicate rows, provenance, timestamps, and interrupted upgrades; demonstrate the original failure and passing migration regressions.
- [x] Resolve normalized TopicPool duplicates before creating content assets, preserve canonical provenance, and cover same-request, existing manual, and existing adopted topics with RED-to-GREEN regressions.
- [x] Complete subscription deletion across authenticated API and Studio, stop new admission/scheduling, retain existing history, and verify confirmation, errors, and selected-state refresh.
- [x] Integrate the latest local-main Harness contracts, preserve both native versions at 4.2.116, and pass DSH tests, packed-artifact checks, profile smoke, and native plugin validation.
- [x] Complete independent re-review of the final fixes, integrate the latest root main, and pass full final-tree Go/Studio/Pack verification before the authorized local-main merge.

Final automated verification on 2026-10-31 (Asia/Shanghai): full Go tests, server build, Go vet, MCP boundary test, Pack generation/check, full Studio tests (175 files / 1,581 tests), and Studio production build pass. Harness DSH tests (476 passed / 1 skipped), packed-artifact integrity, installed-profile smoke, and native Claude plugin validation pass. Independent re-review reports no remaining actionable findings. Native plugin validation retains the existing optional icon/privacy-policy directory warnings.

Live provider calls, actual Docker/Kubernetes execution, and a live MySQL migration are outside the completed automated verification. No remote push is authorized.
