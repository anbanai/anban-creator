# Admin Archestra Agent 测试页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Studio 提供一个仅管理员可见的 Archestra A2A 对话测试页，并由 Anban Server 安全代理 Agent 列表、对话、任务查询、审批和取消操作。

**Architecture:** Server 读取固定的 `archestra.a2a_base_url` 与 `archestra.a2a_token`，通过一个无状态 A2A HTTP 客户端调用 Archestra；Admin handler 每次从认证用户 ID 重新读取数据库并确认 `is_admin`。Studio 只保存当前页面的 `contextId`、`taskId` 和消息，所有请求都走现有 `/api/v1` 客户端，不新增持久化模型。

**Tech Stack:** Go/Fiber、GORM repository、`net/http` JSON-RPC client、React 19、React Router、TanStack Query、shadcn/Base UI、Vitest。

**Spec:** 用户提供的《Admin Archestra Agent 测试页》实现计划（本会话消息）。

## Global Constraints

- 浏览器只能调用 Anban Server，不能传入或看到 Archestra URL、Bearer Token。
- 每个代理接口都必须重新从 repository 读取用户并确认 `is_admin`；普通用户返回 403。
- 缺少 Archestra Token 时 Server 正常启动，代理接口返回服务不可用，Studio 显示未配置状态。
- 不记录 Token、提示词、模型回复或完整上游响应；结构化日志只记录管理员 ID、Agent ID、操作和结果。
- 对话、`contextId`、`taskId` 只保存在当前 Studio 页面内存中，刷新后新建会话。
- A2A 使用 `GET /v2/a2a/agents`、`SendMessage`、`GetTask`、审批用 `taskOps.approvalDecisions`、`CancelTask`。

## Review Focus

- 未配置 Token、无效 URL、超时和非 2xx 上游响应都返回稳定的服务不可用错误且不泄露上游正文。
- Agent ID 只能作为固定配置 URL 下的路径段，不能让浏览器改变目标主机。
- 审批只接受当前任务返回的 approval ID，页面在 `input-required` 时明确展示批准/拒绝操作。
- 认证上下文缺失、普通用户、数据库中管理员标记已撤销都不能调用代理。
- 刷新页面不得恢复旧会话或向 Server 创建会话记录。

### Task 1: Server configuration and A2A client

**Files:**
- Create: `server/service/archestra.go`
- Create: `server/service/archestra_test.go`
- Modify: `server/config/config.go`
- Modify: `server/config/config_phase2_test.go`
- Modify: `server/config.example.yaml`

**Interfaces:**
- `config.ArchestraConfig{A2ABaseURL, A2AToken, Timeout}` with default base URL `http://archestra-platform.anbanai-prod.svc.cluster.local:9000` and non-blocking validation.
- `service.NewArchestraClient(config.ArchestraConfig, *http.Client) *ArchestraClient`.
- `ArchestraClient.ListAgents`, `SendMessage`, `GetTask`, `CancelTask` return redacted typed data and never expose token or raw upstream body.

- [ ] Write failing config and `httptest` client tests for URL/header/body construction, task parsing, approval metadata, timeout, and upstream error redaction.
- [ ] Run targeted Go tests and observe failure because the config/client do not exist.
- [ ] Implement the config section, safe URL joining, JSON-RPC requests, response normalization, and typed errors.
- [ ] Run targeted Go tests and confirm pass.

### Task 2: Admin Server API and wiring

**Files:**
- Create: `server/handler/archestra_admin.go`
- Create: `server/handler/archestra_admin_test.go`
- Modify: `server/router/router.go`
- Modify: `server/main.go`

**Interfaces:**
- `GET /api/v1/admin/archestra/agents`
- `POST /api/v1/admin/archestra/agents/:agentId/messages`
- `GET /api/v1/admin/archestra/tasks/:taskId`
- `POST /api/v1/admin/archestra/tasks/:taskId/approval`
- `POST /api/v1/admin/archestra/tasks/:taskId/cancel`

- [ ] Write failing Fiber/GORM tests for unauthenticated, ordinary, revoked-admin, configured-admin, unconfigured, upstream error and all proxy operations.
- [ ] Run the handler tests and observe failure.
- [ ] Implement repository-backed admin checks, stable response envelopes, redacted audit logs, and router/main wiring.
- [ ] Run handler and router tests and confirm pass.

### Task 3: Studio API, navigation, route and chat page

**Files:**
- Create: `studio/src/lib/api/archestra.ts`
- Create: `studio/src/pages/AdminArchestraPage.tsx`
- Create: `studio/src/pages/AdminArchestraPage.test.tsx`
- Modify: `studio/src/types/index.ts`
- Modify: `studio/src/lib/navigation.ts`
- Modify: `studio/src/lib/navigation.test.ts`
- Modify: `studio/src/App.tsx`
- Modify: `studio/src/App.admin-routes.test.ts`

**Interfaces:**
- Typed Studio client for the five Server routes; no direct Archestra URL or token.
- Page states: loading/unconfigured/error, agent selection, empty/new chat, working task polling, completion, failure, input-required approval, approve/reject, cancel, and clear conversation.

- [ ] Write failing navigation, route and component tests for admin visibility, message continuation, polling, approval and cancel states.
- [ ] Run targeted Studio tests and observe failure.
- [ ] Implement the typed API client and native Studio chat surface with in-memory context/task/message state.
- [ ] Run targeted Studio tests and confirm pass.

### Task 4: Full verification and deployment configuration

**Files:**
- Modify: `server/Deployment.yaml`
- Modify: `docs/deployment.md` or `docs/archestra-integration.md` if needed.

- [ ] Add Kubernetes Secret-backed placeholders for `archestra.a2a_base_url` and `archestra.a2a_token` without logging or exposing the token.
- [ ] Run `cd server && go test ./...` and `go build -o /tmp/anban-creator-server .`.
- [ ] Run `cd studio && bun install`, `bun run test`, and `bun run build`.
- [ ] Review the final diff for authorization, URL pinning, token leakage, persistence, and unrelated changes.
