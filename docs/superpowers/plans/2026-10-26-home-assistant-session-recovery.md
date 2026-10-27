# 首页助手会话可靠性优化实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将首页助手的可恢复边界收敛为 Server-owned interaction，使消息、计划暂存和确认创建在 runtime/session 失效后仍能恢复、重试且幂等完成。

**Architecture:** `assistant_interaction` 是用户可恢复事实的唯一主键；DSH logical session、MCP transport 和 runtime token 只是带 generation 的可替换 binding。Server 持久化 interaction snapshot、draft receipt、event/attempt 记录，并负责 generation 校验、串行 command、runtime replacement、commit receipt 和最终创建；MCP 只传输一个原子 stage 能力，Studio 只从 Server snapshot 恢复。

**Tech Stack:** Go Fiber v3、GORM/MySQL、现有 InteractiveService/runtime factory、MCP tools、TypeScript DSH bridge、React 19、TanStack Query、Vitest、Bun。

**Spec:** `docs/superpowers/specs/2026-10-26-home-assistant-session-recovery-design.md`

## Global Constraints

- 同一用户、项目和当前 mode 只保留一个可恢复 interaction；多个标签页连接同一个 interaction。
- 首页 mode 切换共享一个 interaction；结构化 draft 按 mode 分开。
- runtime 失效由 Server 自动异步替换 binding；Studio 展示 `reconnecting`/`restart_required`，不要求用户重新打开页面。
- 结构化 draft 和待确认创建收据保留 30 天；消息事件和输入收据保留 24 小时。
- 旧 in-flight session 不迁移；发布后旧 session 直接不可恢复，用户只能恢复 durable draft 或重新开始。
- 同一 interaction 的消息、回答和 stage 按 Server command sequence 串行化；generation 变化时只自动重放幂等且无副作用请求。
- Server 是身份、状态、幂等、创建和外部副作用的唯一权威；MCP 不编排工作流，不直接创建 Task 或发布内容。
- 新项目直接采用新 interaction API，不保留旧首页 session/workspace API 的兼容分支。
- 不记录 runtime token、MCP bearer、用户正文、完整草稿和素材 URL；诊断只通过 attempt id 关联事件链。

## Review Focus

- Server 在 draft ready 和用户确认之间重启时，用户仍能看到计划并完成一次创建；由 Task 5 的 runtime-loss commit test 覆盖。
- runtime replacement 后迟到的旧 generation stage/event 不能覆盖新 draft 或状态；由 Task 3/4 的 stale-generation test 覆盖。
- 多标签页同时发送、回答或 stage 时，Server 只接受一个 command sequence，重复 attempt 返回原结果；由 Task 3/4 的 ordering/idempotency test 覆盖。
- MCP transport 过期或 DSH 断开时，bridge 能刷新 interaction capability，用户不会看到裸 `session not found`；由 Task 6 的 bridge contract test 和 Task 7 的 UI test 覆盖。
- SSE cursor gap、页面刷新和 404/410 恢复时，消息、输入和 draft 不被清空；由 Task 7 的 snapshot-recovery tests 覆盖。

### Task 1: 定义 interaction 合同、错误码和诊断字段

**Files:**
- Create: `server/model/assistant_interaction.go`
- Create: `server/service/assistant_interaction_contract.go`
- Modify: `server/handler/interactive.go`
- Modify: `server/mcp/interactive_tools.go`
- Test: `server/service/assistant_interaction_contract_test.go`
- Test: `server/handler/interactive_test.go`

**Interfaces:**
- Produces `model.AssistantInteraction`, `model.AssistantInteractionRuntime`, `model.AssistantInteractionDraft`, `model.AssistantInteractionEvent`, `model.AssistantInteractionAttempt` and lifecycle/status constants.
- Produces `service.InteractionSnapshot`, `service.InteractionStageInput`, `service.InteractionStageResult`, `service.InteractionCommitRequest` and `service.InteractionError` with stable error codes.
- Produces `service.NewInteractionAttemptID()` and `service.InteractionOperation` values for `message`, `answer`, `stage`, `events`, and `commit`.

- [ ] **Step 1: Write failing contract tests** for the exact lifecycle values (`active`, `reconnecting`, `needs_confirmation`, `completed`, `abandoned`), runtime values (`starting`, `running`, `closed`, `failed`), draft confirmation values (`pending`, `confirmed`, `consumed`, `superseded`), and error-code-to-recovery-action mapping.
- [ ] **Step 2: Run the focused tests** with `cd server && go test ./service ./handler -run 'TestInteractionContract|TestInteractiveError' -count=1`; verify they fail because the new types/codes do not exist.
- [ ] **Step 3: Implement the contract types** in `server/model/assistant_interaction.go` and `server/service/assistant_interaction_contract.go`. Keep JSON fields user-safe; put internal correlation fields behind `json:"-"`.
- [ ] **Step 4: Update error encoding** in `server/handler/interactive.go` so `assistant_interaction_generation_changed`, `assistant_interaction_reconnecting`, `assistant_interaction_restart_required`, `assistant_message_not_accepted`, `assistant_draft_saved_retryable`, `assistant_draft_revision_conflict`, `assistant_commit_already_applied`, and `assistant_commit_reconciliation_required` return structured `error_code`, `available_actions`, and snapshot metadata.
- [ ] **Step 5: Add contract tests for redaction** proving attempt IDs contain no token/body/material URL and raw `interactive_session_not_found` is never serialized as user-facing copy.
- [ ] **Step 6: Run the focused tests again** and confirm PASS before moving to persistence.

### Task 2: Add durable interaction, runtime binding, draft, event and attempt persistence

**Files:**
- Modify: `server/model/model.go`
- Modify: `server/repository/repository.go`
- Create: `server/repository/assistant_interaction.go`
- Create: `server/migrations/20261026_assistant_interactions.sql`
- Test: `server/repository/assistant_interaction_test.go`
- Test: `server/migrations/assistant_interaction_contract_test.go`

**Interfaces:**
- Produces `repository.AssistantInteractions()` with methods `Create`, `FindByIDForUser`, `FindByScopeForUpdate`, `UpdateLifecycle`, `CompareAndSwapGeneration`, `CreateRuntimeBinding`, `FindCurrentRuntime`, `CloseRuntime`, `CreateDraftIfAttemptNew`, `FindDraftForUser`, `FindLatestDraft`, `CreateEvent`, `ListEventsAfter`, `CreateAttempt`, `FindAttempt`, `ExpireAttempts`, and `ExpireEvents`.
- Repository methods must accept `context.Context` and use row locks/CAS for scope and generation mutations.
- Retention values are constants: drafts/commit receipts 30 days; events/attempts 24 hours.

- [ ] **Step 1: Write repository tests** covering unique active interaction per `(user_id, project_id, mode scope)`, generation CAS, current runtime lookup, stage attempt deduplication, event sequence uniqueness, draft revision increments, and retention deletion.
- [ ] **Step 2: Run `cd server && go test ./repository ./migrations -run 'TestAssistantInteraction' -count=1`** and verify the tests fail against the missing tables/repository.
- [ ] **Step 3: Implement the five model tables** with explicit indexes and constraints: interaction scope/lifecycle, runtime generation, draft `(interaction_id, mode_id, revision)`, event `(interaction_id, sequence)`, and attempt `(interaction_id, attempt_id)`.
- [ ] **Step 4: Implement repository transactions** in `server/repository/assistant_interaction.go`; `CompareAndSwapGeneration` must update only when the expected generation matches, and stage/commit attempt lookup must return the original result for an identical payload digest and a conflict for a different digest.
- [ ] **Step 5: Register the models** in `model.AutoMigrate`, the root repository and transaction repository, add the additive production migration, and wire a scheduled cleanup for 30-day drafts/receipts and 24-hour events/attempts. Do not backfill old logical sessions; the migration must mark them outside the new API's recoverable set.
- [ ] **Step 6: Run the repository and migration tests** and confirm PASS, including a rollback-safe transaction test for concurrent scope creation.

### Task 3: Implement Server interaction lifecycle, generation CAS and runtime replacement

**Files:**
- Create: `server/service/assistant_interaction.go`
- Modify: `server/service/interactive.go`
- Modify: `server/main.go`
- Modify: `server/service/interactive_runtime_test.go`
- Create: `server/service/assistant_interaction_test.go`

**Interfaces:**
- `NewAssistantInteractionService(repo repository.Repository, runtime RuntimeFactory, clock func() time.Time) *AssistantInteractionService`.
- `CreateOrResume(ctx context.Context, userID string, in AssistantInteractionInput, idempotencyKey string) (InteractionSnapshot, error)`.
- `Snapshot(ctx context.Context, userID, interactionID string) (InteractionSnapshot, error)`.
- `EnsureRuntime(ctx context.Context, userID, interactionID string, expectedGeneration int64) (RuntimeBindingView, error)`.
- `RunCommand(ctx context.Context, userID, interactionID string, expectedGeneration int64, op InteractionOperation, attemptID string, fn func(RuntimeBindingView) error) error`.
- `ReplaceRuntime(ctx context.Context, userID, interactionID string, failedGeneration int64, cause string) (RuntimeBindingView, error)`.

- [ ] **Step 1: Write failing service tests** for create/resume idempotency, same-scope interaction reuse, generation CAS rejection, serialized commands, runtime replacement, and stale runtime events being ignored.
- [ ] **Step 2: Run `cd server && go test ./service -run 'TestAssistantInteraction|TestRuntimeGeneration' -count=1`** and verify failure.
- [ ] **Step 3: Implement `AssistantInteractionService`** using repository row locks and one command sequence per interaction. Runtime IDs and tokens stay internal; only interaction ID and generation appear in the service snapshot.
- [ ] **Step 4: Integrate runtime replacement with the existing `InteractiveService` runtime factory**. On closed/failed binding, create the next generation under the same interaction, persist `reconnecting`, and return `restart_required` only after replacement fails.
- [ ] **Step 5: Gate every message/answer/stage/event callback by generation**; discard old-generation callbacks before they mutate draft, event or lifecycle state.
- [ ] **Step 6: Add server-instance/runtime correlation logging and metrics** with attempt ID, operation, generation, status and latency, explicitly excluding secrets and content. Emit `assistant_interaction_recovery_total{cause,outcome}`, `assistant_draft_stage_total{outcome}`, `assistant_draft_stage_retry_after_session_loss_total`, `assistant_commit_duplicate_prevented_total`, `assistant_runtime_generation_replaced_total`, and draft-durable p50/p95 latency.
- [ ] **Step 7: Run the focused service tests and the existing interactive runtime tests**; all must pass before changing MCP or Studio.

### Task 4: Move MCP draft staging to interaction scope and make it durable/idempotent

**Files:**
- Create: `server/service/assistant_interaction_stage.go`
- Modify: `server/service/interactive.go`
- Modify: `server/mcp/interactive_tools.go`
- Modify: `server/mcp/mcp.go`
- Modify: `server/ASSISTANT-RUNTIME.md`
- Test: `server/mcp/interactive_tools_test.go`
- Test: `server/mcp/boundary_contract_test.go`
- Test: `server/service/assistant_interaction_stage_test.go`

**Interfaces:**
- `StageDraftForInteraction(ctx context.Context, userID string, in service.InteractionStageInput) (service.InteractionStageResult, error)`.
- MCP stage arguments contain `interaction_id`, `generation`, `attempt_id`, `mode_id`, `payload`, `payload_digest`, and `ready`; they do not contain a durable `session_id`.
- The tool returns `draft_id`, `revision`, `confirmation_required`, `available_actions`, and `durable: true`.

- [ ] **Step 1: Write failing MCP/service tests** for a successful stage after runtime replacement, duplicate same-attempt replay, different-payload conflict, old-generation rejection, and runtime loss returning `assistant_draft_saved_retryable` rather than a raw session error.
- [ ] **Step 2: Run `cd server && go test ./mcp ./service -run 'TestStage.*Interaction|TestMCP.*Generation|TestDraft.*Idempotent' -count=1`** and verify failure.
- [ ] **Step 3: Implement `StageDraftForInteraction`** as one repository transaction: authenticate the current interaction capability, validate generation, write/update the durable draft, append the event, and record the attempt result.
- [ ] **Step 4: Change MCP identity resolution** so the runtime token resolves to interaction ID + generation + capability hash; a missing logical session can trigger `EnsureRuntime` but cannot erase or replace the interaction draft.
- [ ] **Step 5: Update both `stage_assistant_draft` and `stage_project_creator_draft` schemas and runtime documentation** to use the new atomic contract and preserve the existing agent allowlists.
- [ ] **Step 6: Run targeted MCP boundary tests plus `cd server && go test ./mcp ./service -run 'TestStage|TestMCP' -count=1`** and confirm no handler composes a second capability.

### Task 5: Make commit depend only on durable draft receipts and add reconciliation outcomes

**Files:**
- Modify: `server/model/interactive.go`
- Modify: `server/service/assistant_commit.go`
- Modify: `server/handler/assistant.go`
- Modify: `server/repository/interactive.go`
- Create: `server/service/assistant_interaction_commit.go`
- Test: `server/service/assistant_interaction_commit_test.go`
- Test: `server/handler/assistant_test.go`

**Interfaces:**
- `CommitInteraction(ctx context.Context, userID, interactionID string, req InteractionCommitRequest) (InteractionCommitResult, error)`.
- `InteractionCommitRequest` contains `DraftID`, `Revision`, `IdempotencyKey`, and mode-specific commit options; it never requires runtime/session ID or confirmation token from the old runtime.
- `InteractionCommitResult.Status` is one of `created`, `already_created`, `needs_reconciliation`, or a retryable structured error.

- [ ] **Step 1: Write failing tests** for commit after runtime termination, duplicate idempotency key, same key with a different draft, stale draft revision, external create ambiguity, and replay of a stored result.
- [ ] **Step 2: Run `cd server && go test ./service ./handler -run 'TestCommitInteraction|TestAssistantCommit' -count=1`** and verify failure.
- [ ] **Step 3: Implement `CommitInteraction`** to lock the durable draft, validate `ready` + revision + user scope, create or reuse a commit receipt, then call the existing task/profile/review commit functions inside the Server-owned transaction boundary.
- [ ] **Step 4: Persist `created`/`already_created` results and `DraftAddAttemptedAt`-backed `needs_reconciliation` evidence**; never infer external success from runtime logs or legacy result files.
- [ ] **Step 5: Replace handler commit routes with interaction/draft IDs and map errors to executable actions; keep user copy free of session/MCP terminology.**
- [ ] **Step 6: Run the focused commit/handler tests and the existing publication/task idempotency tests**; verify double-click produces one task.

### Task 6: Refresh DSH interaction capabilities without treating transport sessions as business sessions

**Files:**
- Modify: `runtime-dsh/src/dsh-bridge.ts`
- Modify: `runtime-dsh/test/dsh-bridge.test.ts`
- Modify: `runtime-dsh/test/sdk-contract.test.ts`
- Modify: `server/agentpack/dsh_contract.go`
- Modify: `server/agentpack/dsh_generate.go`

**Interfaces:**
- Bridge commands accept `interaction_id`, `generation`, and a short-lived capability envelope for `session/start`, `session/prompt`, and `session/answer`.
- Add `interaction/capability-refresh` handling that returns the new generation/binding without changing interaction ID.
- `mapHarnessNotification` must include generation and drop tool/session events from an older generation.

- [ ] **Step 1: Write failing bridge tests** for transport expiry followed by capability refresh, old-generation tool result suppression, shared runtime with multiple modes, and secret redaction in error events.
- [ ] **Step 2: Run `cd runtime-dsh && bun run test -- test/dsh-bridge.test.ts test/sdk-contract.test.ts`** and verify failure.
- [ ] **Step 3: Implement the capability envelope and refresh command**; keep the DSH transport session disposable and never expose it as the Studio interaction identity.
- [ ] **Step 4: Add generation checks to notification mapping and prompt/answer serialization** so stale callbacks cannot mutate the current interaction.
- [ ] **Step 5: Update agent pack contract generation and fixtures** so both assistant and project creator presets pass interaction capability fields to MCP stage tools.
- [ ] **Step 6: Run all runtime-dsh tests and the generated contract checks** before wiring the Studio UI.

### Task 7: Replace Studio session recovery with interaction snapshot recovery and explicit user states

**Files:**
- Create: `studio/src/lib/assistant-interaction-state.ts`
- Create: `studio/src/components/assistant/AssistantRecoveryBanner.tsx`
- Modify: `studio/src/lib/api/assistant.ts`
- Modify: `studio/src/lib/api/interactive.ts`
- Modify: `studio/src/lib/interactive-events.ts`
- Modify: `studio/src/pages/AssistantWorkspace.tsx`
- Modify: `studio/src/components/assistant/AssistantDraftSummary.tsx`
- Test: `studio/src/lib/assistant-interaction-state.test.ts`
- Test: `studio/src/pages/AssistantWorkspace.test.tsx`
- Test: `studio/src/lib/interactive-events.test.ts`

**Interfaces:**
- API client methods: `createInteraction`, `getInteractionSnapshot`, `sendInteractionMessage`, `answerInteractionQuestion`, `streamInteraction`, and `commitInteraction`.
- `AssistantInteractionState` exposes `connecting`, `reconnecting`, `needs_confirmation`, `input_retryable`, `draft_retryable`, `restart_required`, and `completed`, plus `availableActions` and the preserved composer/draft.
- `streamInteraction(interactionID, generation, token, signal, options)` resets the cursor from the server snapshot when generation changes or an event gap occurs.

- [ ] **Step 1: Write failing state/API tests** for refresh recovery, 404/410 handling, generation change, draft stage retry, duplicate commit result, and `restart_required` rendering.
- [ ] **Step 2: Run `cd studio && bun run test -- src/lib/assistant-interaction-state.test.ts src/pages/AssistantWorkspace.test.tsx src/lib/interactive-events.test.ts`** and verify failure.
- [ ] **Step 3: Implement the new typed API clients** in `studio/src/lib/api/assistant.ts` and `studio/src/lib/api/interactive.ts`; remove session ID and Last-Event-ID from persisted authority, retaining only composer/attachment/display cache in sessionStorage.
- [ ] **Step 4: Implement the state reducer and recovery banner**. Map server errors to user actions: “恢复并继续”, “重试发送”, “重试暂存”, “恢复计划”, “重新开始”, and “查看任务”. Do not show `session not found`, token, MCP, or runtime wording.
- [ ] **Step 5: Refactor `AssistantWorkspace.tsx`** to bootstrap from `getInteractionSnapshot`, preserve messages/questions/draft on reconnect, invalidate old cursors on generation change, and submit commit by `draft_id + revision + idempotency_key`.
- [ ] **Step 6: Add UI tests** proving page refresh and SSE gap retain composer/messages/draft, stage failure retains confirmation entry, and repeated confirmation displays the existing task.
- [ ] **Step 7: Run targeted Studio tests and `cd studio && bun run build`**; inspect the built route for type errors and missing recovery actions.

### Task 8: Cut over routes, remove the old dual session/workspace path, and add end-to-end verification

**Files:**
- Modify: `server/handler/assistant.go`
- Modify: `server/handler/interactive.go`
- Modify: `server/service/assistant.go`
- Modify: `server/service/interactive.go`
- Modify: `server/ASSISTANT-RUNTIME.md`
- Modify: `server/migrations/20261026_assistant_interactions.sql`
- Modify: `studio/src/pages/AssistantWorkspace.tsx`
- Test: `server/handler/assistant_test.go`
- Test: `server/service/assistant_test.go`
- Test: `studio/src/pages/AssistantWorkspace.test.tsx`

**Interfaces:**
- The public home flow exposes only `/assistant/interactions` resources and their message/events/commit actions.
- Old `AssistantWorkspaceSession`, `AssistantSessionID`, `X-Assistant-Session-ID`, and runtime-session recovery branches are deleted or made unreachable from the new route graph.
- Existing unrelated interactive endpoints remain unchanged unless they are part of the home assistant route.

- [ ] **Step 1: Write cutover tests** asserting old session IDs are rejected by new routes, old rows cannot be resumed, and new interaction snapshots contain all fields needed for refresh without a session identifier.
- [ ] **Step 2: Run the focused server/Studio cutover tests** and verify failure while both paths still exist.
- [ ] **Step 3: Switch router registration and service wiring** to `AssistantInteractionService`; make startup recovery replace/mark old active bindings without copying in-flight transcript state.
- [ ] **Step 4: Remove old workspace/session persistence and client branches** after the new route tests pass; preserve only the migration marker and documented abandoned-state behavior.
- [ ] **Step 5: Add an end-to-end test** covering: create interaction → send message → runtime replacement → durable stage → page refresh → commit → duplicate commit replay.
- [ ] **Step 6: Run final verification:**
  - `cd server && go test ./...`
  - `cd server && go build -o /tmp/anban-creator-server .`
  - `cd runtime-dsh && bun run test`
  - `cd runtime-dsh && bun run typecheck`
  - `cd studio && bun run test`
  - `cd studio && bun run build`
- [ ] **Step 7: Run the verification-before-completion checklist** and record the exact command output, migration status, and any known operational limits before claiming the issue is fixed.

## Execution Notes

- Start implementation in an isolated Git worktree using `superpowers:using-git-worktrees`; the current checkout contains unrelated untracked documentation and must not be reset or cleaned.
- Execute tasks in order. Tasks 1–2 establish the contract and durable boundary; Tasks 3–5 depend on those interfaces; Task 6 can proceed after the capability contract exists; Task 7 follows the HTTP/API contract; Task 8 is the final cutover.
- Use TDD for each task: write the smallest failing test, run it, implement the minimum behavior, rerun focused tests, then commit a reviewable unit.
- Do not “fix” this incident by extending TTLs or adding silent client retries before durable draft persistence and generation checks are in place.
