# Execution Identity Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every managed and local Agent execution carry one verifiable task/execution identity from dispatch through Bootstrap, MCP image settlement, Hook metadata, artifact finalization, and terminal completion, while presenting a recoverable Chinese error instead of an opaque `completion_report_failed`.

**Architecture:** The Server remains the authority for `task_id`, `project_id`, and `execution_id`. Bootstrap returns the canonical execution identity and a short-lived execution JWT; the TypeScript runtime validates that identity, exposes only non-secret IDs to the Claude process and Hook, and uses the JWT for MCP and Agent HTTP calls. Server-side MCP image and completion paths fail closed when no execution context is present, while a runtime contract header rejects stale Agent images before work starts.

**Tech Stack:** Go Fiber v3, GORM repositories, MCP Go SDK, TypeScript/Node 22 Agent runtime, Docker/Kubernetes dispatchers, React/Vite Studio, Vitest/Bun, Go table-driven tests.

**Spec:** `docs/superpowers/specs/2026-09-12-execution-identity-recovery.md`

## Global Constraints

- Never put execution JWTs, provider keys, or API keys in environment diagnostics, workspace files, or user-facing messages.
- `task_id`, `project_id`, and `execution_id` are non-secret identity fields and may be exposed to the managed Claude process and Hook.
- Execution-scoped MCP calls must use the JWT minted by Bootstrap or local claim; API keys and static admin keys must not settle fixed-SKU operations.
- Existing artifacts must remain recoverable; an identity failure resumes from the recorded stage and must not delete prior output.
- Changes under `harness/agents`, `harness/skills`, hooks, or manifests require the same patch version bump in `harness/.claude-plugin/plugin.json` and `harness/.codex-plugin/plugin.json`.
- Use TDD for behavior changes and run fresh verification before claiming completion.

---

### Task 1: Make Bootstrap Identity Explicit and Immutable

**Files:**
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/handler/agent.go`
- Modify: `agent-ts/src/bootstrap.ts`
- Modify: `agent-ts/src/config.ts`
- Modify: `agent-ts/src/main.ts`
- Modify: `agent-ts/src/runner.ts`
- Modify: `agent-ts/src/reporter.ts`
- Test: `server/service/agent_bootstrap_test.go`
- Test: `server/handler/agent_test.go`
- Test: `agent-ts/test/bootstrap.test.ts`
- Test: `agent-ts/test/runner.test.ts`
- Test: `agent-ts/test/main.test.ts`

**Interfaces:**
- Add `execution_id` to `AgentBootstrapResponse` / `BootstrapResponse` and to the validated `BOOTSTRAP_RESPONSE_KEYS` set.
- Add `AGENT_RUNTIME_CONTRACT_VERSION` as a shared numeric protocol constant in the runtime and an `X-Anban-Agent-Contract-Version` Bootstrap request header.
- `validateBootstrapResponse(requestedExecutionID, payload)` must reject a missing or mismatched `execution_id`.
- `runClaude`, `Reporter`, completion Hook creation, and artifact finalization must consume the validated response identity, not independently fall back to an empty process environment value.

- [ ] **Step 1: Write failing tests** that reject a Bootstrap response with missing or mismatched `execution_id`, assert the request sends the contract header, and assert `buildExecutionEnvironment` includes `ANBAN_TASK_ID`, `ANBAN_EXECUTION_ID`, and `ANBAN_TASK_TYPE` while still excluding `ANBAN_API_KEY`.
- [ ] **Step 2: Run targeted tests**: `cd agent-ts && bun test test/bootstrap.test.ts test/runner.test.ts test/main.test.ts`; `go test ./server/service ./server/handler`.
- [ ] **Step 3: Implement the response field and validation**. The Server response must set `execution_id: execution.ID`; the runtime must compare it to `--execution-id` before materializing files or starting Claude.
- [ ] **Step 4: Thread one identity object through runtime code**. Remove the default-to-empty execution ID behavior from managed execution after Bootstrap succeeds. Keep process-environment fallback only for explicitly supported local compatibility tests.
- [ ] **Step 5: Add non-secret identity environment variables** in `buildExecutionEnvironment`; do not add tokens or provider credentials. Pass the same values to the completion Hook.
- [ ] **Step 6: Run the targeted tests again**, then run `cd agent-ts && bun run typecheck`.
- [ ] **Step 7: Commit** with `fix: make managed execution identity explicit`.

### Task 2: Fail Closed at MCP Image and Completion Boundaries

**Files:**
- Modify: `server/mcp/image_tools.go`
- Modify: `server/mcp/content_metadata_tools.go`
- Modify: `server/mcp/mcp.go`
- Modify: `server/service/task_image.go`
- Modify: `server/service/task_image_operations.go`
- Modify: `server/service/task_agent.go`
- Test: `server/mcp/image_tools_test.go`
- Test: `server/mcp/mcp_test.go`
- Test: `server/service/task_image_test.go`
- Test: `server/service/task_managed_completion_test.go`

**Interfaces:**
- Add a small shared guard, for example `requireMCPExecutionIdentity(ctx, operation) error`, that checks the execution identity installed by `executionScopeMiddleware` before invoking fixed-SKU image, upload, analysis, or completion metadata services.
- Preserve the existing service-level checks as defense in depth; the MCP guard must never infer identity from `task_id`, `project_id`, or an API key.
- Add stable machine-readable error codes such as `execution_identity_required` and `execution_identity_mismatch`; keep detailed provider/auth text in server diagnostics.

- [ ] **Step 1: Write failing tests** proving that an API-key/static-key MCP request to `generate_image`, `upload_image`, `analyze_image`, and `submit_completion_metadata` returns an execution-identity error without touching billing or provider services.
- [ ] **Step 2: Write a test** proving a valid execution JWT with a stale/current-task mismatch is rejected before image settlement, while a valid current execution reaches the service.
- [ ] **Step 3: Run** `go test ./server/mcp ./server/service -run 'Execution|Image|Completion' -count=1` and confirm the new tests fail before implementation.
- [ ] **Step 4: Implement the guard and error classification**. Do not change image retry behavior for provider failures; only identity/auth failures become deterministic and non-retryable.
- [ ] **Step 5: Add structured log fields** (`operation`, `task_id`, `execution_id_present`, `execution_scope`, `error_code`) without logging credentials or raw JWTs.
- [ ] **Step 6: Run** `go test ./server/mcp ./server/service` and `go test ./...`.
- [ ] **Step 7: Commit** with `fix: fail closed when MCP execution identity is missing`.

### Task 3: Add Runtime Preflight and Preserve the First Root Cause

**Files:**
- Modify: `agent-ts/src/main.ts`
- Modify: `agent-ts/src/errors.ts`
- Modify: `agent-ts/src/reporter.ts`
- Modify: `agent-ts/src/completion-evaluator.ts`
- Modify: `server/agent/runtime_reconciler.go`
- Test: `agent-ts/test/main.test.ts`
- Test: `agent-ts/test/reporter.test.ts`
- Test: `server/agent/runtime_reconciler_test.go`

**Interfaces:**
- Add `assertManagedIdentity(data, config)` before workspace preparation and Claude startup.
- Add deterministic runtime error code `execution_identity_unavailable` with `resume_from` set to the last active stage.
- Completion reporting must retain both `root_error_code` and `completion_report_failed`; the latter is a delivery failure, not a replacement for the original image/authentication failure.

- [ ] **Step 1: Write failing tests** for empty/mismatched identity preflight, for completion report failures preserving the original result, and for retry classification that does not retry a deterministic identity error five times.
- [ ] **Step 2: Run** `cd agent-ts && bun test test/main.test.ts test/reporter.test.ts`; `go test ./server/agent -run TestRuntimeReconciler -count=1`.
- [ ] **Step 3: Implement preflight** immediately after Bootstrap validation. It must fail before image generation and write a minimal structured failure result through the normal completion path.
- [ ] **Step 4: Update finalization** so metadata upload failure is recorded as a warning when terminal completion is acknowledged, while an unacknowledged `/agent/complete` remains `completion_report_failed` and retains the original result in diagnostics.
- [ ] **Step 5: Ensure `failure-state.json` is generated with** `error_code: execution_identity_unavailable` for identity failures, `stage`, `resume_from`, and a short Chinese-safe message; do not include tokens or full environment dumps.
- [ ] **Step 6: Run** `cd agent-ts && bun run test && bun run typecheck`; `go test ./server/agent ./server/service`.
- [ ] **Step 7: Commit** with `fix: preserve root cause during runtime finalization`.

### Task 4: Reject Stale Runtime Images Before Work Starts

**Files:**
- Modify: `server/handler/agent.go`
- Modify: `agent-ts/src/bootstrap.ts`
- Modify: `agent-ts/src/config.ts`
- Modify: `server/agent/docker_runtime.go`
- Modify: `server/agent/kubernetes_job.go`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `README.md`
- Test: `server/handler/agent_test.go`
- Test: `server/agent/docker_runtime_test.go`
- Test: `server/agent/kubernetes_executor_test.go`
- Test: `agent-ts/test/bootstrap.test.ts`

**Interfaces:**
- Bootstrap accepts the runtime contract header and returns HTTP 426 with `agent_runtime_upgrade_required` when it is unsupported.
- Docker and Kubernetes use the same runtime contract constant and command shape; release jobs build each selected image from the exact checked-out commit and publish immutable digests.

- [ ] **Step 1: Write failing tests** for missing, lower, and higher contract headers; assert that Docker/Kubernetes commands still pass the execution ID and that release configuration does not select an unpinned stale image in production.
- [ ] **Step 2: Run** targeted handler/dispatcher tests and confirm failure.
- [ ] **Step 3: Implement contract negotiation**. Keep the supported range explicit and fail closed; do not silently downgrade to API-key MCP authentication.
- [ ] **Step 4: Add CI/release checks** that build `agent-ts` inside each runtime Dockerfile, attach the Git revision and contract version as OCI labels, and verify the image digest before deployment.
- [ ] **Step 5: Document the rollout order**: deploy Server accepting both current and one previous contract, publish new runtime images, switch dispatch image digests, then remove the previous contract after all executions drain.
- [ ] **Step 6: Run** `go test ./server/handler ./server/agent`, `make server-build`, and the Docker contract test suite.
- [ ] **Step 7: Commit** with `fix: gate agent runtime compatibility at bootstrap`.

### Task 5: Make the Article Workflow Stop on Identity Errors and Resume Cleanly

**Files:**
- Modify: `harness/agents/article.md`
- Modify: `harness/agents/article.toml`
- Modify: `harness/skills/article/SKILL.md`
- Modify: `harness/skills/article-visual-design/SKILL.md`
- Modify: `harness/.claude-plugin/plugin.json`
- Modify: `harness/.codex-plugin/plugin.json`
- Modify: `harness/agent-pack-catalog.json`
- Test: `server/agent/article_skill_contract_test.go`
- Test: `server/agent/image_generation_parameter_contract_test.go`
- Test: `server/agentpack/catalog_test.go`

**Interfaces:**
- Identity/authentication errors from `generate_image`, `analyze_image`, `upload_image`, or `submit_completion_metadata` are classified as runtime failures, not creative quality failures.
- The agent writes `output/failure-state.json` once, preserves all completed artifacts, and resumes from `image_generation` after the runtime identity is repaired.
- Provider/quality failures retain their existing bounded retry rules; identity failures are not retried with alternate prompts.

- [ ] **Step 1: Add failing contract assertions** for the identity-error branch, the no-prompt-retry rule, the preserved-artifact rule, and the explicit resume stage.
- [ ] **Step 2: Run** `go test ./server/agent -run 'Article|ImageGeneration|AgentPack' -count=1` and confirm failure.
- [ ] **Step 3: Update both Markdown and TOML Agent definitions** with the same behavior and no direct HTTP fallback.
- [ ] **Step 4: Update both native manifests and the generated catalog** with a patch version bump and regenerated digest.
- [ ] **Step 5: Run** the targeted contract tests and verify no stale plugin source remains under `plugins/`.
- [ ] **Step 6: Commit** with `fix: stop article image retries on execution identity failures`.

### Task 6: Present Recoverable Chinese UX Without Hiding Diagnostics

**Files:**
- Modify: `studio/src/lib/labels.ts`
- Modify: `studio/src/lib/http-client.ts`
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Test: `studio/src/pages/TaskDetailPage.test.tsx`
- Test: `studio/src/lib/http-client.test.ts`

**Interfaces:**
- Map `completion_report_failed` to `结果提交失败（可恢复）`.
- Map `execution_identity_unavailable` / `execution_identity_required` to `执行环境未建立，暂时无法生成或结算图片`.
- Display a short recovery instruction: `已保留已有产物，修复执行环境后可从“图片生成”阶段继续。` Keep raw error code and detailed diagnostics behind the task detail/technical view.

- [ ] **Step 1: Write failing UI tests** for the recoverable heading, root-cause message, resume stage, and separation between user copy and raw diagnostic code.
- [ ] **Step 2: Run** `cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx src/lib/http-client.test.ts`.
- [ ] **Step 3: Implement centralized error-code mapping**; do not duplicate translations in individual pages or toast callers.
- [ ] **Step 4: Ensure `completion_report_failed` is not shown as the primary root cause** when `failure-state.json` contains `execution_identity_unavailable`.
- [ ] **Step 5: Run** `cd studio && bun run test && bun run build`.
- [ ] **Step 6: Commit** with `feat: localize recoverable execution failures`.

### Task 7: End-to-End Verification and Controlled Rollout

**Files:**
- Test: `server/integration/e2e_test.go`
- Test: `deploy/docker/runtime-smoke_test.go`
- Modify: `docs/superpowers/specs/2026-08-03-runtime-finalization-deadline-isolation-design.md` only if the finalized error precedence differs from the documented contract
- Modify: `README.md` with operator diagnostics and rollback steps

- [ ] **Step 1: Add an integration scenario** that creates a task, creates a current execution, boots a runtime, calls `generate_image`, uploads an artifact, submits completion metadata, and completes the task with the same execution identity.
- [ ] **Step 2: Add a negative scenario** where the runtime sends an API key or a mismatched execution token; assert zero provider/billing calls, `execution_identity_required`, preserved artifacts, and `resume_from=image_generation`.
- [ ] **Step 3: Add a response-loss scenario** where `/agent/complete` returns retryable failures; assert terminal `completion_report_failed` while the original root error remains queryable.
- [ ] **Step 4: Run the complete verification set**:
  - `go test ./...`
  - `go build -o /tmp/anban-creator-server ./server`
  - `cd agent-ts && bun run test && bun run typecheck && bun run build`
  - `cd studio && bun run test && bun run build`
  - `go test ./deploy/docker -run TestRuntimeSmoke -count=1` when Docker smoke prerequisites are available
- [ ] **Step 5: Roll out in order**: Server contract compatibility, runtime images, dispatcher image digests, then plugin/UI. Monitor Bootstrap 4xx, execution-identity errors, image settlement denials, completion acknowledgement latency, and `completion_report_failed` count.
- [ ] **Step 6: Roll back by dispatch image digest** if identity errors increase; retain the Server’s compatibility window until all in-flight executions finish.
- [ ] **Step 7: Commit** with `test: verify execution identity recovery end to end`.

## Acceptance Criteria

- A managed runtime cannot start Claude or call image MCP tools without a validated current execution identity.
- The same `execution_id` is present in Bootstrap response validation, MCP authorization, Hook metadata, artifact uploads, and `/agent/complete`.
- An old/incompatible runtime receives an actionable Bootstrap upgrade error before generating content.
- Identity failures make at most one deterministic attempt, preserve existing artifacts, and resume from `image_generation`.
- The Studio shows a Chinese recoverable message while retaining the machine code and detailed diagnostics for operators.
- Full Go, Agent runtime, Studio, and relevant Docker contract tests pass.
