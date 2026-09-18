# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Anban 智能创作助手** (anban-creator) is a content creation platform. Core components:
- **Agent** (`agent-ts/`): TypeScript runtime for containerized Claude Code task execution
- **Server** (`server/`): Fiber v3 HTTP API with MySQL, Redis, Asynq task queue, WebSocket, and MCP endpoint
- **Studio** (`studio/`): React 19 + TypeScript + Vite 8 frontend for content management

Companion client surfaces wrapping the same server API:
- **Miniapp** ([private companion repository](https://github.com/anbanai/creator-miniapp)): WeChat Mini Program client kept at feature parity with Studio (real-time updates via SSE, not WebSocket)

Anban provides the Web Studio, cloud Agent runtime, and installable plugin assets.

The `server/app/` directory contains Server-owned reusable Go packages for Markdown-to-WeChat-HTML conversion, styled writing, image generation, and WeChat publishing. The TypeScript Agent runtime does not import them.

Claude Code and Codex share one repository-owned plugin source at `harness/`. Third-party dependencies remain submodules and should be initialized before Docker builds. The `sidecar-ilink` image used by the iLink WeChat assistant channel is fetched from `lich0821/wcfLink` by `deploy/docker/Dockerfile.sidecar-ilink` at build time.

## Server / Harness / MCP Boundary

- Harness (the managed Agent and its Skills) owns non-deterministic content and image generation, semantic quality judgments, and file-backed artifacts. It never decides or triggers automatic WeChat draft publication.
- Server owns identity and frozen project facts, publication policy, state transitions, credentials, external side effects, idempotency, retries, billing, persistence, reconciliation, and user-visible outcomes. Automatic article publication is initiated only by the Server finalizer.
- MCP is a stateless atomic capability transport. An MCP handler authenticates and validates one capability, then returns its result; it does not orchestrate a workflow or own the terminal publication state. The interactive `create_draft` capability remains available only for explicit interactive calls.
- `output/draft.json` is a versioned handoff package. Agent-provided author, cover media IDs, publication results, logs, or ambiguous states are not authoritative; Server derives author from the frozen project snapshot and cover media from current execution files.

MCP server config uses the `creator` server key. Business-facing agent, skill, and setup docs must reference bare MCP tool names such as `generate_image`; host-specific tool-name prefixes are a runtime concern and belong only in system-level config or tests.

- **Language**: Go 1.27.0 (Server), TypeScript (Agent + Studio)
- **Logging**: Zerolog for Go components; never mix with zap
- **WeChat SDK**: silenceper/wechat/v2

## Build & Test Commands

### Server & Agent

```bash
make server-build             # Build server binary to bin/anban-creator-server
make server-run               # Build and run server with config
make server-dev               # Run server via go run (development)
make server-test              # Run server tests (go -C server test -v ./...)

make docker-agent-image       # Build minimal Article Agent image
make docker-seednote-agent-image # Build independent Seednote workflow image
make docker-montage-agent-image  # Build Montage image (OpenMontage + Remotion + ffmpeg)
make docker-server-image      # Build server Docker image (Go binary)
make docker-sidecar-ilink-image    # Build the latest iLink sidecar Docker image
make docker-sidecar-seednote-image # Build the latest Seednote sidecar Docker image
make docker-studio-image      # Build Studio Docker image (Bun + nginx)
make docker-images            # Build all supported Docker images
```

All repository-owned Dockerfiles are centralized in `deploy/docker/` and build
from the repository-root context. Article, Seednote, and Montage use independent
Agent Dockerfiles so their system dependencies and release lifecycles stay
separate.

The Go module root is `server/`. Use `make server-build` or `cd server && go build -o /tmp/anban-creator-server .`.

### Go Tests

```bash
make test                     # Run all Go tests (go -C server test -v ./...)
cd server && go test -v ./app/config       # Run specific package tests
cd server && go test -v -run TestConfig_Validate ./app/config  # Run specific test
make coverage                 # Tests with coverage report
make ci                       # Run all CI checks (fmt + vet + test + lint)
make fmt                      # Format code (go fmt)
make vet                      # Static analysis (go vet)
make lint                     # Lint (requires golangci-lint)
```

> ⚠️ Older golangci-lint builds that bundle go-critic v0.6.2 may panic during initialization. If that happens, upgrade golangci-lint. See `.golangci.yml`.

### Studio Frontend

```bash
cd studio && bun install      # Install dependencies (uses Bun, not npm)
make web-dev                  # Run dev server (proxies /api → localhost:8080, /ws → ws://localhost:8080)
make web-build                # Build for production (tsc -b && vite build)
cd studio && bun run test     # Run vitest tests
cd studio && bun run test:watch  # Watch mode tests
```

### TypeScript Agent

```bash
cd agent-ts && npm ci
make agent-test
make agent-build
```

### Docker Infrastructure

```bash
make docker-up                # Build all Agent profiles, then start Compose services
make docker-down              # Stop all containers
make docker-logs              # Follow container logs
```

## Architecture

### Agent (`agent-ts/`)

TypeScript runtime that executes Claude Code tasks in Docker/Kubernetes. The server dispatches managed tasks to one of three dependency profiles: `creator-agent-article`, `creator-agent-seednote`, or `creator-agent-montage`. All profiles contain the same canonical plugin; only their system runtimes differ.

- `src/main.ts` — Managed `job` lifecycle and terminal finalization
- `src/runner.ts` — Claude Agent SDK query and stream handling
- `src/bootstrap.ts` / `src/workspace.ts` — Bootstrap validation and workspace materialization
- `src/downloads.ts` / `src/artifacts.ts` — Generated image and final artifact transport
- `src/reporter.ts` — Progress, heartbeat, artifact, and completion reporting

### Server (`server/`)

Fiber v3 HTTP API. Layered architecture: handler → service → repository → MySQL/GORM.

Key packages:
- `handler/` — HTTP handlers (auth, project, plan, task, timeline, websocket, credit, agent, ilink)
- `router/` — `router.go` registers all `/api/v1` route groups, middleware, and the WS hub (the single source of truth for routes)
- `service/` — Business logic including `task_execution.go` (agent SDK integration), `task_files.go`, `credit.go`, `publishing.go`, `task_agent.go`, `redis_notifier.go` (Redis pub/sub progress events), `task_events.go` (notifier interface), `ilink_*.go` (WeChat assistant binding, conversation, polling, and terminal notification outbox)
- `wcf/` — HTTP client for the iLink sidecar transport used by the iLink channel; do not put product-level WeChat assistant behavior here
- `agent/` — Server-side execution policy, dispatch, and result contracts; this is distinct from the `agent-ts/` runtime
- `scheduler/` — Asynq-based background task processing with `plan_checker.go`
- `model/` — GORM models with auto-migration
- `storage/` — File storage factory (local filesystem or Alibaba Cloud OSS)
- `mcp/` — MCP HTTP endpoint
- `setup.go` — Server bootstrap helpers (config, logger, DB/Redis connections)
- `services.go` — Core service wiring (returns `coreServices` struct)
- `handlers.go` — Handler instantiation
- `workers.go` — Asynq server, periodic cleanup startup

### Server App Packages (`server/app/`)

Server-owned reusable Go packages for content creation and publishing capabilities:
- `converter/` — Markdown → WeChat HTML with theme system and AI mode
- `writer/` — AI-powered styled writing with YAML-defined writing styles
- `image/` — Multi-provider image generation (OpenAI, Gemini, Volcengine), compression, processing
- `draft/` — WeChat draft creation and publishing
- `wechat/` — WeChat API wrapper with retry logic
- `config/` — Configuration management (JSON)

### Studio Frontend (`studio/`)

React 19 + TypeScript + Vite 8 + Tailwind CSS v4. State: Zustand v5. Data fetching: TanStack React Query v5. Routing: React Router DOM v7. UI: shadcn/ui + Base UI. Forms: React Hook Form + Zod validation. Package manager: **Bun**.

```
studio/src/
├── pages/          # Route pages
├── components/
│   ├── ui/         # shadcn/ui primitives
│   ├── auth/       # LoginDialog, UserAccountPopover
│   └── layout/     # AppLayout, Sidebar, PageHeader
├── lib/
│   ├── api.ts      # Axios API client
│   ├── sse.ts      # Server-Sent Events for task streaming
│   ├── schemas.ts  # Zod validation schemas
│   └── labels.ts   # Label constants
├── hooks/          # useWebSocket, useKeyboardShortcuts
├── stores/         # Zustand stores
└── test/           # Test setup (vitest)
```

Vite dev server proxies `/api` → `localhost:8080` and `/ws` → `ws://localhost:8080`.

### Server API Routes

All routes are registered in `server/router/router.go`. Everything under `/api/v1` except `auth/*` (public) and `agent/*` (API-key auth) requires JWT auth.

- `GET /health` — Health check (MySQL + Redis status)
- `GET /ws`, `GET /ws/login` — WebSocket for real-time updates / QR login
- `POST /api/v1/auth/*` — Public: register, login, code-login (SMS), send-code, refresh, logout, wx-login, qrcode/scanned/qr-callback (QR login)
- `GET /api/v1/auth/me`, `PUT /api/v1/auth/password` — Authenticated user
- `POST /api/v1/agent/*` — Agent↔server protocol (execution-token auth): bootstrap, upload, progress, complete
- `/api/v1/projects` — Project CRUD + fetch-profile, analyze-image, archive/restore, stats (formerly `/channels`)
- `/api/v1/projects/:project_id/topics` — Topic pool (create/list/delete/reset)
- `GET /api/v1/image-capabilities` — Tier-filtered public image capability catalog
- `/api/v1/plans` — Plan CRUD + pause/resume
- `/api/v1/tasks` — Task CRUD + cancel/retry, bulk-cancel/retry/delete, stream (SSE), preview, files/zip/download, WeChat publication lifecycle, Seednote analytics, and WeChat article analytics
- `/api/v1/ilink/*` — ilink 微信助手 binding + commands + task terminal notifications
- `/api/v1/timeline`, `/api/v1/usage/stats` — Timeline view, usage stats
- `/api/v1/billing/*` — Wallet, immutable SKU catalog, quotes, transactions, and referral status
- `/api/admin/billing/*` — Manual API top-ups and internal cost/margin/reconciliation reports
- `/api/v1/api-keys`, `/api/v1/feedback` — API key management, feedback
- `/api/v1/files/*` — Local file serving (local storage mode) / OSS redirect
- `/mcp` — MCP endpoint (API key or JWT auth, configured in plugin `.mcp.json` files)

## Configuration

### Server

YAML config at `server/config.yaml` — the single source of truth. Environment variables affect config **only** where the YAML explicitly writes a `${VAR}` or `${VAR:-default}` placeholder (expanded before parsing); there is no hidden `ANBAN_*` override layer. Per-environment differences (Docker hostnames/addresses) are expressed as visible `${...}` placeholders in the file (e.g. `redis.addr: "${ANBAN_REDIS_ADDR:-localhost:6379}"`); `docker-compose.yml` supplies those envs. To env-control any other field, add a `${...}` placeholder in the YAML. Expansion is `${VAR}` only (braced) — literal `$` is never touched, so passwords are safe. Env values must be valid YAML scalars for their field (e.g. a bool field needs `true`/`false`, not `1`/`yes` — there is no truthy coercion).

Graceful degradation: MySQL unreachable → degraded mode (no persistence). Redis unreachable → in-process goroutine task execution, rate limiting skipped.

### Server App Configuration

Single WeChat account via `.anban-creator/settings.json`. Config search priority: CWD → `CLAUDE_PLUGIN_ROOT` → `~/.config/anban-creator/` → `~/.anban-creator/` → executable-relative.

Two loading modes: `Load()`/`LoadWithDefaults()` (full validation) vs `LoadMinimal()` (skips WeChat validation).

## Key Data Flows

### Conversion Flow (`server/app/converter`)

1. Image extraction from Markdown (local/online/AI-generated references)
2. Markdown → WeChat HTML with inline CSS and theme styling (AI mode uses Claude)
3. Image placeholders (`<!-- IMG:0 -->`) → compress → upload to WeChat CDN → replace with CDN URLs
4. Optional draft creation in WeChat backend

### Task Execution (server → agent)

1. Server creates a task and selects the content, Seednote, or Montage runtime image
2. TypeScript Agent validates Bootstrap inputs and starts a Claude Agent SDK session
3. Claude Code executes skill-based workflows using the plugin system
4. Agent reports results back to server
5. Server streams progress to Studio via WebSocket/SSE


## Image Generation Providers

All implement the `Provider` interface in `server/app/image/provider.go`.

| Provider | Value | Notes |
|----------|-------|-------|
| OpenAI-compatible | `openai`, `wangcai_openai` | Synchronous image generation |
| Google Gemini | `gemini` or `google` | Inline image data |
| Volcengine/Seedream | `volcengine`, `volcengine_ark`, `vol`, `seedream` | Async polling |

## Important Constraints

1. **WeChat HTML**: All CSS inline, no external resources. Safe tags only: section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr. No: script, iframe, form, input, style, link.
2. **Image Processing**: Max 1920px width, < 10MB for upload, preserves aspect ratio. Formats: JPG, JPEG, PNG, GIF, BMP, WebP.
3. **AI Generation**: Prompts in Chinese for better results. Theme prompts define complete styling.
4. **Studio**: Uses Bun (not npm). Use `bun install`, `bun run dev`, `bun run test`.
5. **Studio 首页 / AI 入口 UX 必须克制**：首页是 Codex-style AI 输入中心，不是数据看板。默认首屏只保留创作输入、项目选择、附件、发送等必要控件；不要常驻接入状态、下一步建议、统计卡、空状态面板或解释性文案。只有在存在阻塞、错误、补配置需求或用户主动进入管理页时，才展示对应提示和入口。新增首页内容前先问：它是否直接帮助用户现在输入并创建任务？如果不是，默认不要放在首页。
6. **大型重构必须验证字段完整性**：在重构 studio 页面、表单对话框、API handler 或任何用户可见的功能时，必须**逐项检查原有功能**（表单字段、按钮、交互、API endpoint）是否被完整保留。禁止以"代码净化"、"简化"、"cleanup"等名义删除已发布的用户可见功能。若确需移除某个功能，必须在 commit message 中**明确列出被删除的功能并说明原因**。重构前建议：① 列出受影响的所有表单字段/交互/按钮；② `git diff` 后逐项核对；③ 不确定的功能一律保留。
7. **Golang 能用泛型时优先用泛型**：遇到类型参数化场景（JSON 列存储、容器类型、类型安全的 helper、仓储/工具函数）时，**优先使用泛型**——例如 `datatypes.JSONSlice[T]` / `datatypes.JSONType[T]` 处理 JSON 列、自定义泛型类型替代 `any` / `interface{}` / 裸 `string` + 手动 `json.Marshal`。优势：① 编译期类型安全；② 零值序列化为合法 JSON（nil slice → `"null"` 字面量，MySQL JSON 列接受；而非空字符串 `""` 触发 `Error 3140`）；③ 减少重复的 marshal/unmarshal 样板代码。仅当泛型会显著增加复杂度、或第三方库不兼容、或性能敏感场景才退回非泛型。
8. **修改插件/Skill 必须更新版本号**：每次修改 `harness/` 下的运行资产（agents、skills/`SKILL.md`、hooks、themes、writers、manifest、安装脚本或会影响插件运行/发布的文档）时，必须在同一次变更中同步更新 `.claude-plugin/plugin.json` 与 `.codex-plugin/plugin.json` 的 `version`。默认 bump patch，除非变更范围需要 minor/major。
9. **优先基于 Claude Code / Agentic-first 思想设计**：所有面向 Claude Code 的功能、插件、Agent、Skill、Hook、MCP 接入和执行流程，都必须优先站在 Claude Code 官方能力与约定之上进行拓展，充分利用 Claude Code 已提供的 Agent、Skill、Hook、MCP、配置、权限、上下文管理、工具调用和工作流能力。系统级功能（server、agent runner、scheduler、task execution、Studio workflow、人机协作入口等）也遵循同一原则：优先参考 AI 时代 Claude Code 体现的 agentic 前卫思想来设计，把复杂工作交给具备上下文、工具调用、计划执行、可观察进度和可恢复性的 Agent 工作流承载，而不是退回传统的硬编码流程或封闭式表单向导。不要在仓库内自造一套重复的调度、插件、工具发现、上下文注入或执行框架；只有在官方能力无法满足当前产品需求、且已有方案经过明确评估后，才允许实现必要的最小补充，并在代码或文档中说明原因。
10. **网络失败时使用本地代理重试**：执行依赖下载、Git、`curl` 或其他联网命令时，如果直连网络不可用，使用 `HTTP_PROXY=http://127.0.0.1:7897 HTTPS_PROXY=http://127.0.0.1:7897` 为该命令临时设置代理后重试；不要因此永久修改仓库或系统的全局代理配置。
11. **MCP 只承载原子能力**：MCP is a stateless capability transport. Agents and Skills 只拥有非确定性创作流程的编排，包括创作步骤、供应商重试、语义质量闸门和停止/继续决策；确定性业务状态、外部副作用、幂等、对账和恢复重试属于 Server。MCP handler 只允许完成身份解析、协议与安全校验、调用 one application capability，并编码结果；禁止组合多个领域 Service 或按结果继续调用另一能力。需要保持一致性的持久化与结算必须封装在应用 Service 的原子操作中。

## Development Patterns

### Adding New Image Providers

1. Implement `Provider` interface in `server/app/image/{provider}.go`
2. Register in `server/app/image/provider.go` factory
3. Add tests with httptest mocking

### Adding New Themes

1. Create YAML file in `server/resources/themes/{name}.yaml`
2. Themes are embedded at compile time via `go:embed` and served via MCP and REST API

### Server Wire Function Pattern

`server/main.go` delegates to domain-specific setup files:
- `setup.go` — `resolveConfig()`, `initLogger()`, `connectMySQL()`, `connectRedis()`, `setupStorage()`
- `services.go` — `setupCoreServices()` returns `coreServices` struct with all service instances
- `handlers.go` — `setupHandlers()` + `setupMCPHandler()` return handler instances
- `router/router.go` — `RegisterRoutes()` wires all `/api/v1` groups, middleware, and the WS hub
- `workers.go` — `startWorkers()` for Asynq server, scheduler, periodic cleanup

### Task Progress (Redis Pub/Sub)

Task progress events use `TaskProgressNotifier` interface (`server/service/task_events.go`):
- Redis pub/sub primary (`redisNotifier`), database polling fallback (`pollingNotifier`)
- SSE handler in `server/handler/task.go` subscribes via notifier
- Publish points in `task_execution.go`, `task_agent.go`, `task.go`

### WeChat API Integration

- Access token automatically cached/refreshed by wechat SDK
- Material upload: images < 10MB, returns media_id and CDN URL, retry on transient failures
- Draft creation: content < 20,000 chars or 1MB, HTML safe tags only

### ilink WeChat Assistant

`sidecar-ilink` is the iLink sidecar that bridges platform-managed WeChat assistant accounts to the server. Its image is fetched from `lich0821/wcfLink` by `deploy/docker/Dockerfile.sidecar-ilink` during builds instead of being checked out as a local submodule. Business-facing code, config, routes, and data models use the platform channel name **ilink**. Server-side integration lives in `server/model/ilink.go`, `server/repository/ilink.go`, `server/service/ilink_*.go`, and `server/handler/ilink.go`. The poller consumes iLink sidecar events and routes them by `platform_account_id + external_user_id`; task success/failure/cancel terminal notifications are persisted through `ilink_notification_outbox` before delivery. Studio binds users to the platform WeChat assistant via `/api/v1/ilink/*`.

## Plugin & Agent Ecosystem

Three client access points share the same server MCP API:

### Naming Contract

The public naming model is **brand + capability**:

- Claude Code install uses `plugin@marketplace`: install `anban@anbanai`, where `anban` is the plugin ID and `anbanai` is the marketplace/publisher.
- The MCP server key is `creator`, because Claude Code treats the server name in `.mcp.json` as the label for that server's tools and for commands such as MCP removal/status; it is not the plugin ID.
- Runtime hosts may decorate MCP tools internally; do not put host-specific tool-name prefixes in agent, skill, or user-facing setup docs.
- Agent namespaces use the plugin/brand ID, for example `anban:<agent>`.
- Do not use compact aliases or alternate install names based on `creator`, the unhyphenated product name, or the full product slug.

### Unified Claude Code and Codex Plugin (`harness/`)

The layout follows the same one-repository pattern used by ECC: shared capability content lives once, while each harness keeps its native adapter.

```
harness/
├── .claude-plugin/                # Claude Code manifest and marketplace metadata
├── .codex-plugin/                 # Codex manifest
├── .mcp.json                      # Claude Code userConfig MCP adapter
├── agents/                        # Claude Markdown Agents + Codex TOML subagents
├── skills/                        # One canonical Skill tree
├── hooks/                         # Claude Code lifecycle adapter
├── install/                       # Codex MCP and subagent registration
├── templates/                     # Shared structure templates
└── writers/                       # Shared writing styles
```

Codex install flow: `codex plugin marketplace add ./harness && codex plugin add anban@anbanai`, then `bash harness/install/install-subagents.sh`.

Both adapters use the `creator` MCP server key. Skills must remain host-neutral; unavoidable host syntax belongs in a manifest, Agent, MCP, Hook, or install adapter. Agents must call MCP tools directly, with no ad-hoc HTTP clients.

### Adding a New Business Scenario with an Agent Pack

Agent Packs are the canonical execution, distribution, and extension units for Agents. They do not replace business identity: keep or add an explicit `Project.Platform` and `Task.Type` for every first-class scenario. Never introduce ambiguous compatibility fields such as `platform_or_type`, and do not move established typed business fields into `agent_config` or `agent_input`.

Choose the smallest integration level that satisfies the product requirement:

1. **Skill-only**: add a host-neutral `harness/skills/<id>/SKILL.md`; no Pack is required unless the Skill needs its own invokable Agent.
2. **Plugin Agent**: scaffold a `kind: plugin` Pack. It is distributed to Claude Code and Codex but has no managed task binding or runtime.
3. **Managed Agent**: scaffold a `kind: managed` Pack and bind an execution route to a reusable runtime profile. The scaffold starts plugin-only; expose product surfaces only after business validation and billing are implemented.
4. **First-class business scenario**: in addition to the managed Pack, add explicit Server model/service validation and Studio project/task/plan behavior. Only expose a surface that the corresponding service actually supports.

Scaffold from the repository root:

```bash
make agent-pack-new ARGS="-id <kebab-id> -kind plugin"
make agent-pack-new ARGS="-id <kebab-id> -kind managed -task-type <task-type> -runtime-profile article"
```

The command creates `harness/packs/<id>/agent-pack.yaml`, `agent.claude.md`, and `agent.codex.toml`. Treat these as canonical sources; never edit generated `harness/agents/<name>.md` or `.toml` directly. Put reusable, host-neutral workflow knowledge under `harness/skills/` and reference Skill IDs from `agent.skills`.

#### Manifest contract

Keep `agent-pack.yaml` declarative and narrow:

- `id`, `version`, `kind`, `display_name`, `description`: stable Pack identity and presentation.
- `agent`: native source files, Skill dependencies, Agent name, and native-host `max_turns`.
- `bindings.project_platforms` / `bindings.task_types`: explicit links to existing business identifiers. A plugin-only Pack has no managed bindings.
- `runtime.profile`: the dependency image class. Reuse `article`, `seednote`, or `montage` unless the scenario has genuinely different system dependencies.
- `runtime.adapter`: use `standard` by default. Use `openmontage` only when execution requires the OpenMontage workspace contract; do not create an adapter for ordinary workflow differences.
- `runtime.max_turns`: the managed Server execution default. Keep it separate from `agent.max_turns`, because native interactive Agents and one-shot managed jobs have different operational budgets. `claude.max_turns` remains an optional operator override keyed by task type or Pack ID.
- `surfaces`: any subset of `plugin`, `project`, `task`, and `plan`. The list must match implemented Server and Studio capabilities; for example, do not advertise `plan` when plan creation rejects that task type.
- `billing_operations`: map every managed task type exposed through `project`, `task`, or `plan` to its billing operation. A runtime-only Pack may omit billing only while it remains plugin-only. Never infer billing from Pack ID or runtime profile.
- `progress`, `artifacts`, and `features`: observable delivery contracts, not workflow orchestration.
- `schemas.project_config` / `schemas.task_input`: optional relative JSON Schema files for scenario-specific JSON extensions. When a Schema is absent, the Server rejects non-empty `agent_config` or `agent_input`.
- `schemas.ui` / `schemas.output`: optional declarative UI metadata and output validation contracts when the scenario needs them.
- `ui.renderer`: omit it for the generic Schema form. Use `custom:<key>` only to declare that an existing typed business form owns the interaction; register that form key explicitly in `studio/src/lib/agent-pack-renderers.ts`. This registry is a guard, not a dynamic React component loader.

The generic Studio form intentionally supports a limited Schema subset: a root object with `additionalProperties: false` whose properties are strings (including `format: textarea`), enums, booleans, numbers, or integers. Every object, including objects owned by a custom renderer, must declare `additionalProperties: false`. Every node must declare a supported `type`; unknown Schema keywords fail Catalog loading. Declared defaults are initialized before submit, and required booleans default to `false` when no explicit default exists. Nested objects and arrays require a registered existing typed form. Prefer the generic form for ordinary scenario extensions. Preserve existing typed forms for current first-class scenarios; `agent_config` and `agent_input` only carry new scenario-specific extension data.

Agents read frozen scenario extensions through existing MCP contracts: call `get_task` for `agent_input`, and call task-aware `get_project_profile` for `agent_config`. Project configuration is copied into `ProjectSnapshot` when a task is created, so later project edits cannot change an in-flight or historical task.

#### When a new runtime is justified

Do not add a Docker image for a new Agent by default. Add a runtime profile/image only when it requires system packages, language runtimes, large pinned sources, or isolation/release behavior that existing profiles cannot provide. When necessary:

1. Add an independent `deploy/docker/Dockerfile.agent-<profile>` using the repository root as build context.
2. Add explicit Server configuration, image validation, Compose/Kubernetes selection, and build/release targets.
3. Build or publish the image before dispatch; production uses immutable digests.
4. Add a runtime adapter only for an unavoidable execution-layout contract, not for business workflow sequencing.

#### Generate and verify

After editing a Pack, its Agent sources, referenced Skills, or Schemas:

```bash
make agent-pack-generate
make agent-pack-check
```

Generation copies native Claude/Codex Agent files and refreshes both `server/agentpack/catalog.generated.json` and the image-consumed `harness/agent-pack-catalog.json`. Commit all generated output in the same change. Pack digests include canonical Agent sources, Schemas, and every file under each referenced Skill directory (scripts/assets included), excluding checkout-specific `.git` metadata, so regenerate after any of them changes.

Before completing a new scenario, verify every applicable item:

- [ ] Product scope is classified as Skill-only, plugin Agent, managed Agent, or first-class scenario.
- [ ] `Project.Platform` and `Task.Type` remain explicit and strongly typed; no compatibility naming was added.
- [ ] A first-class scenario updates the Go business constants/validation plus Studio `ProjectPlatform`, `TaskType`, `PlanType`, labels, and Zod enums explicitly. Catalog data must not bypass these typed authorities.
- [ ] Manifest bindings, surfaces, billing operations, progress, artifacts, runtime profile, and adapter match real behavior.
- [ ] Claude and Codex canonical Agent sources stay semantically aligned and use MCP tools directly.
- [ ] Referenced Skills exist and remain host-neutral.
- [ ] Scenario-only JSON fields have restricted Schemas; invalid, omitted, empty, update, clone, and plan-to-task behavior are tested.
- [ ] Generic Schema UI is used unless a registered existing typed form (`custom:<key>`) is necessary; existing form fields and interactions are preserved.
- [ ] Server Catalog routing, task execution Pack identity/version/digest, bootstrap, runner validation, and runtime workspace behavior are tested.
- [ ] Every managed task type exposed through `project`, `task`, or `plan` has an explicit billing mapping and real SKU coverage. Runtime-only Packs remain plugin-only. Every advertised surface has valid service support.
- [ ] A new runtime image/profile is added only when dependency isolation requires it, with Docker/Kubernetes configuration and smoke validation.
- [ ] `make agent-pack-generate` and `make agent-pack-check` pass without unexpected generated drift.
- [ ] Targeted tests, `cd server && go test ./...`, Server and Agent builds, Studio tests, and Studio build pass.
- [ ] Both `harness/.claude-plugin/plugin.json` and `harness/.codex-plugin/plugin.json` receive the same semantic-version bump, and `harness/CHANGELOG.md` documents the release.

## Notes

- CLI uses zerolog logging — all components use zerolog, never mix with zap
- Docker Compose provides MySQL 8.0 + Redis 7 + agent + server containers
- Server binary is `bin/anban-creator-server`
- **Never modify base UI components in `studio/src/components/ui/`**. These are managed shadcn/ui primitives. If a base component update breaks business logic, fix the business component only — never patch the primitive.
- `AGENTS.md` mirrors this guidance for non-Claude assistants; keep it roughly in sync when adding cross-cutting rules.
