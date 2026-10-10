# Project Creator 交互式项目创建 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Studio、Go Server、TypeScript Bridge 和 DSH SDK 之间实现可复用的临时交互 Agent 工作区，并将 Project Creator 作为首个交互 Agent。

**Architecture:** Studio 通过稳定的 REST/SSE 协议与 Server 交互；Server 为每个创建页面启动一个带 lease 的独立 Runtime，并以 opaque runtime token 绑定会话身份；TypeScript Bridge 将 DSH Runtime 的控制协议和原始事件转换为 Server 事件。Runtime 只能 stage 临时草稿，最终项目创建、profile revision 和幂等提交全部由 Server 事务完成。

**Tech Stack:** Go Fiber v3、GORM、SSE、TypeScript、DSH SDK、React 19、React Router、TanStack Query、Vitest、Go test、Agent Pack catalog generator。

**Spec:** `docs/superpowers/specs/2026-10-10-project-creator-interactive-design.md`

## Global Constraints

- `profile-analysis` 保持现有零交互托管行为；新增 Agent 的内部 id 为 `project-creator`，展示名为 `Project Creator`。
- 不使用 `/init`，不显示“新会话”按钮；选择 Agent 后自动创建临时 Session 并触发 `session/start`。
- 未提交 Session、草稿和 Runtime 状态不持久化；Runtime 退出、刷新、关闭、Server 重启或超时后不可恢复。
- 浏览器不得提交任意 profile、插件路径、MCP 配置或用户身份；Server 从能力注册表、鉴权上下文和 Session 推导全部权限。
- `AskUserQuestion` 只对 manifest 声明 `interaction.user_questions: true` 的 root Agent 开放；子 Agent 和现有托管 Agent 继续禁止。
- MCP 只能执行原子 capability；Runtime 不得修改正式 profile、项目 memory、活动上下文、`AGENTS.md` 或 `CLAUDE.md`。
- 修改 `harness/` 后必须运行 `make agent-pack-generate`、`make agent-pack-check`，并同步 Claude/Codex plugin 版本。

### Task 1: DSH interactive protocol

**Files:**
- Modify: `/Users/medivh/WORKSPACE/deepseek-harness/packages/core/agent-loop/src/agent.ts`
- Modify: `/Users/medivh/WORKSPACE/deepseek-harness/packages/core/agent/src/runtime-types.ts`
- Modify: `/Users/medivh/WORKSPACE/deepseek-harness/packages/sdk/protocol/src/types.ts`
- Modify: `/Users/medivh/WORKSPACE/deepseek-harness/packages/sdk/server/src/server.ts`
- Modify: `/Users/medivh/WORKSPACE/deepseek-harness/packages/sdk/client/src/client.ts`
- Test: DSH SDK and agent-loop Vitest suites

**Interfaces:** `session/start` binds the preset and triggers one non-user first turn; `session/answer` validates the live root Agent and delegates to `userQuestions.answer()`.

- [x] Add `session/start`, `session/prompt`, `session/answer`, and `shutdown` protocol types and handlers.
- [x] Make startup idempotent, prevent duplicate `session.ready`, and allow retry after a missing `start()` capability.
- [x] Add tests for startup order, no synthetic user message, duplicate startup, and answer validation.
- [x] Run `pnpm run typecheck` and the targeted Vitest suites in the DSH repository.

### Task 2: Project Creator Pack and capability registry

**Files:**
- Create: `harness/packs/project-creator/agent-pack.yaml`
- Create: `harness/packs/project-creator/agent.claude.md`
- Create: `harness/packs/project-creator/agent.codex.toml`
- Create: `harness/packs/project-creator/agent.dsh.yml`
- Create/modify: `harness/dsh/presets/project-creator/`
- Modify: `server/agentpack/catalog.go`, generated catalogs, and plugin manifests

**Interfaces:** the Pack declares `interaction.user_questions: true`, profile `project-creator`, no task/channel routing, and only Project Creator capabilities.

- [x] Add the Pack, DSH preset, Claude Agent, Codex Agent, and structured draft contract.
- [x] Keep `profile-analysis` and all existing managed Packs non-interactive.
- [x] Validate catalog rules reject interactive task/channel/billing bindings.
- [x] Run `make agent-pack-generate` and `make agent-pack-check`.

### Task 3: Server session, lease, SSE, and commit service

**Files:**
- Create: `server/model/interactive.go`
- Create: `server/repository/interactive.go`
- Create: `server/service/interactive.go`
- Create: `server/handler/interactive.go`
- Modify: `server/router/router.go`, `server/main.go`, `server/config/config.go`, `server/config.example.yaml`
- Tests: `server/service/interactive_test.go`, handler/API tests

**Interfaces:** `InteractiveService` exposes `Agents`, `CreateSession`, `Events`, `Message`, `Answer`, `StageDraftForSession`, `Commit`, and `Close`; `InteractiveHandler` maps them to the REST/SSE routes in the spec.

- [x] Add authenticated Session ownership, lease expiry, in-memory event history, watcher cleanup, and terminal events.
- [x] Start one Bridge/Runtime process per Session and bind an opaque runtime token to the Session identity.
- [x] Add strict question batch validation and pending-answer serialization.
- [x] Validate six dimensions and draft revisions on the Server; keep profile status `draft`.
- [x] Implement transaction-backed commit and user/idempotency receipt replay.
- [x] Add tests for ownership, expiry, answer retry, draft validation, commit idempotency, and failure cleanup.

### Task 4: MCP and TypeScript Bridge boundary

**Files:**
- Create: `server/mcp/interactive_tools.go`
- Modify: `server/mcp/mcp.go`
- Create: `runtime-dsh/src/interactive-bridge.ts`
- Test: `server/mcp/interactive_tools_test.go`, `runtime-dsh/test/interactive-bridge.test.ts`

**Interfaces:** authenticated interactive runtime calls `stage_project_creator_draft` without browser-provided `session_id` or token; Bridge supports `session/start`, `session/prompt`, `session/answer`, and `shutdown`.

- [x] Resolve runtime bearer credentials only in Server MCP authentication and inject Session identity into context.
- [x] Deny execution JWTs and unauthenticated calls to the interactive staging capability.
- [x] Convert DSH events to stable Anban events and close the Runtime with the Session.
- [x] Run `bun test test/interactive-bridge.test.ts`, `bun run typecheck`, and targeted Go tests in `runtime-dsh`.

### Task 5: Studio chat workspace and responsive UX

**Files:**
- Create: `studio/src/pages/ChatPage.tsx`
- Create: `studio/src/lib/api/interactive.ts`
- Create: `studio/src/lib/interactive-events.ts`
- Modify: `studio/src/App.tsx`, `studio/src/pages/ProjectsPage.tsx`
- Tests: Studio API, SSE, and ChatPage interaction tests

**Interfaces:** `/chat` and `/projects/create` render the same workspace; `streamInteractiveSession` reconnects with `Last-Event-ID` up to three times with exponential backoff and stops on terminal events.

- [x] Add automatic Session creation after Agent selection and remove the “new session” step.
- [x] Add DSH-style mode selector, message stream, Markdown rendering, question cards, answer locking, draft confirmation, and six-dimension progress.
- [x] Add mobile horizontal progress, fixed composer, leave/refresh warning, retry state, and form fallback.
- [x] Preserve messages and local draft on terminal errors; only explicit restart or Agent switch clears the temporary conversation.
- [x] Filter unavailable registry entries and handle a URL that requests an unavailable Agent.
- [x] Run `bun run test` and `bun run build`, recording unrelated pre-existing build failures separately.

### Task 6: End-to-end verification and rollout

**Files:**
- Modify: feature configuration and rollout documentation as needed
- Test: DSH, Agent TS, Server, MCP, Agent Pack, and Studio suites

**Interfaces:** feature remains disabled unless interactive runtime configuration is present; enabling it exposes only the registered Project Creator capability.

- [x] Verify catalog generation and Pack consistency.
- [x] Verify DSH typecheck and targeted protocol tests.
- [x] Verify Agent TS tests, typecheck, and build.
- [x] Verify Go package tests and server build.
- [x] Verify Studio tests and build; report baseline failures without masking feature results.
- [ ] Enable the configuration flag only after deployment has the Bridge command, runtime image, MCP URL, and sticky-session routing.

## Self-review

The design covers the requested UX, temporary Session semantics, DSH protocol extension, Agent capability policy, MCP boundary, draft schema, final transaction, SSE reconnect, failure handling, and acceptance criteria. No task relies on an unspecified profile, endpoint, or placeholder implementation. The only rollout step intentionally remains unchecked because production configuration and deployment routing are environment operations rather than a code change.
