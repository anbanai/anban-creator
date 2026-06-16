# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Anban 智能创作助手** (anbanwriter) is a content creation platform with three main components:
- **Agent** (`agent/`): Standalone Go binary for containerized Claude Code task execution
- **Server** (`server/`): Fiber v3 HTTP API with MySQL, Redis, Asynq task queue, WebSocket, and MCP endpoint
- **Studio** (`studio/`): React 19 + TypeScript + Vite 8 frontend for content management

The `app/` directory is a **library** (no `main.go`) providing content creation functionality used by both the server and agent. It handles Markdown-to-WeChat-HTML conversion, AI writing, image generation, humanization, and WeChat publishing.

- **Language**: Go 1.26.0 (Agent + Server + app library), TypeScript (Studio)
- **Logging**: Zerolog (all components — app library, server, agent)
- **WeChat SDK**: silenceper/wechat/v2

## Build & Test Commands

### Server & Agent

```bash
make server-build             # Build server binary to bin/abwriter-server
make server-run               # Build and run server with config
make server-dev               # Run server via go run (development)
make server-test              # Run server tests (go test -v ./server/...)

make docker-agent-image       # Build agent Docker image (Claude Code + plugin)
make docker-server-image      # Build server Docker image (Go binary)
make docker-images            # Build both images
```

### Go Tests

```bash
make test                     # Run all Go tests (go test -v ./...)
go test -v ./app/config       # Run specific package tests
go test -v -run TestConfig_Validate ./app/config  # Run specific test
make coverage                 # Tests with coverage report
make ci                       # Run all CI checks (fmt + vet + test + lint)
make fmt                      # Format code (go fmt)
make vet                      # Static analysis (go vet)
make lint                     # Lint (requires golangci-lint)
```

### Studio Frontend

```bash
cd studio && bun install      # Install dependencies (uses Bun, not npm)
make web-dev                  # Run dev server (proxies /api → localhost:8080, /ws → ws://localhost:8080)
make web-build                # Build for production (tsc -b && vite build)
cd studio && bun run test     # Run vitest tests
cd studio && bun run test:watch  # Watch mode tests
```

### Docker Infrastructure

```bash
make docker-up                # Start all services: MySQL 8.0, Redis 7, agent, server
make docker-down              # Stop all containers
make docker-logs              # Follow container logs
```

## Architecture

### Agent (`agent/`)

Standalone Go binary that executes Claude Code tasks in Docker containers. The server dispatches tasks; the agent runs them in isolation.

- `main.go` — Entry point, orchestrates run lifecycle
- `runner.go` — Creates and manages Claude Code CLI subprocess
- `config.go` — Agent configuration parsing
- `downloader.go` — Downloads workspace files from server
- `reporter.go` — Reports task results back to server
- `bootstrap.sh` / `install.sh` — Claude Code + plugin installation scripts

### Server (`server/`)

Fiber v3 HTTP API. Layered architecture: handler → service → repository → MySQL/GORM.

Key packages:
- `handler/` — HTTP handlers (auth, channel, plan, task, timeline, websocket, credit, agent)
- `service/` — Business logic including `task_execution.go` (agent SDK integration), `task_files.go`, `credit.go`, `publishing.go`, `task_agent.go`, `redis_notifier.go` (Redis pub/sub progress events), `task_events.go` (notifier interface)
- `agent/` — Agent execution layer using claude-agent-sdk-go, includes MCP server and tool definitions
- `scheduler/` — Asynq-based background task processing with `plan_checker.go`
- `model/` — GORM models with auto-migration
- `storage/` — File storage factory (local filesystem or Alibaba Cloud OSS)
- `mcp/` — MCP HTTP endpoint
- `setup.go` — Server bootstrap helpers (config, logger, DB/Redis connections)
- `services.go` — Core service wiring (returns `coreServices` struct)
- `handlers.go` — Handler instantiation
- `workers.go` — Asynq server, periodic cleanup startup

### App Library (`app/`)

Shared library for content creation, used by both server and agent:
- `converter/` — Markdown → WeChat HTML with theme system and AI mode
- `writer/` — AI-powered styled writing with YAML-defined writing styles
- `humanizer/` — AI trace detection and removal with quality scoring
- `image/` — Multi-provider image generation (OpenAI, Gemini, Volcengine), compression, processing
- `storage/` — SQLite via GORM for CLI-side content/draft/image records
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

- `GET /health` — Health check (MySQL + Redis status)
- `GET /ws` — WebSocket for real-time updates
- `POST /api/v1/auth/*` — Register, login, refresh, logout, wx-login (public)
- `GET /api/v1/auth/me` — Current user (authenticated)
- `/api/v1/channels` — Channel CRUD + fetch-profile, archive/restore
- `/api/v1/plans` — Plan CRUD + pause/resume
- `/api/v1/tasks` — Task CRUD + cancel, stream (SSE), preview, files/zip/download
- `/api/v1/timeline` — Unified timeline view
- `/api/v1/credits` — Balance, sign-in, transactions
- `/api/v1/files/*` — Local file serving (local storage mode only)
- `/mcp` — MCP endpoint (API key or JWT auth, configured in `claudecode/.mcp.json` and `openclaw/.mcp.json`)

## Configuration

### Server

YAML config at `server/config.yaml`. All fields overridable via `ANBAN_SERVER_*` env vars.

Graceful degradation: MySQL unreachable → degraded mode (no persistence). Redis unreachable → in-process goroutine task execution, rate limiting skipped.

### App Library (used by CLI commands via plugin skills)

Single WeChat account via `.anbanwriter/settings.json`. Config search priority: CWD → `CLAUDE_PLUGIN_ROOT` → `~/.config/anbanwriter/` → `~/.anbanwriter/` → executable-relative.

Two loading modes: `Load()`/`LoadWithDefaults()` (full validation) vs `LoadMinimal()` (skips WeChat validation).

## Key Data Flows

### Conversion Flow (app/converter)

1. Image extraction from Markdown (local/online/AI-generated references)
2. Markdown → WeChat HTML with inline CSS and theme styling (AI mode uses Claude)
3. Image placeholders (`<!-- IMG:0 -->`) → compress → upload to WeChat CDN → replace with CDN URLs
4. Optional draft creation in WeChat backend

### Task Execution (server → agent)

1. Server creates task, dispatches to agent via Docker container
2. Agent downloads workspace, runs Claude Code CLI subprocess
3. Claude Code executes skill-based workflows using the plugin system
4. Agent reports results back to server
5. Server streams progress to Studio via WebSocket/SSE

## Image Generation Providers

All implement `Provider` interface (`app/image/provider.go`).

| Provider | Value | Notes |
|----------|-------|-------|
| OpenAI | `openai` (default) | Synchronous, dall-e-2/dall-e-3 |
| Google Gemini | `gemini` or `google` | Inline image data |
| Volcengine/Seedream | `volcengine`, `volc`, `seedream` | Async polling |

## Important Constraints

1. **WeChat HTML**: All CSS inline, no external resources. Safe tags only: section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr. No: script, iframe, form, input, style, link.
2. **Image Processing**: Max 1920px width, < 10MB for upload, preserves aspect ratio. Formats: JPG, JPEG, PNG, GIF, BMP, WebP.
3. **AI Generation**: Prompts in Chinese for better results. Theme prompts define complete styling.
4. **Studio**: Uses Bun (not npm). Use `bun install`, `bun run dev`, `bun run test`.
5. **大型重构必须验证字段完整性**：在重构 studio 页面、表单对话框、API handler 或任何用户可见的功能时，必须**逐项检查原有功能**（表单字段、按钮、交互、API endpoint）是否被完整保留。禁止以"代码净化"、"简化"、"cleanup"等名义删除已发布的用户可见功能。若确需移除某个功能，必须在 commit message 中**明确列出被删除的功能并说明原因**。重构前建议：① 列出受影响的所有表单字段/交互/按钮；② `git diff` 后逐项核对；③ 不确定的功能一律保留。
6. **Go 优先使用泛型设计**：遇到类型参数化场景（JSON 列存储、容器类型、类型安全的 helper、仓储/工具函数）时，**优先使用泛型**——例如 `datatypes.JSONSlice[T]` / `datatypes.JSONType[T]` 处理 JSON 列、自定义泛型类型替代 `any` / `interface{}` / 裸 `string` + 手动 `json.Marshal`。优势：① 编译期类型安全；② 零值序列化为合法 JSON（nil slice → `"null"` 字面量，MySQL JSON 列接受；而非空字符串 `""` 触发 `Error 3140`）；③ 减少重复的 marshal/unmarshal 样板代码。仅当泛型会显著增加复杂度、或第三方库不兼容、或性能敏感场景才退回非泛型。

## Development Patterns

### Adding New Image Providers

1. Implement `Provider` interface in `app/image/{provider}.go`
2. Register in `app/image/provider.go` factory
3. Add tests with httptest mocking

### Adding New Themes

1. Create YAML file in `server/resources/themes/{name}.yaml`
2. Themes are embedded at compile time via `go:embed` and served via MCP and REST API

### Server Wire Function Pattern

`server/main.go` delegates to domain-specific setup files:
- `setup.go` — `resolveConfig()`, `initLogger()`, `connectMySQL()`, `connectRedis()`, `setupStorage()`
- `services.go` — `setupCoreServices()` returns `coreServices` struct with all service instances
- `handlers.go` — `setupHandlers()` + `setupMCPHandler()` return handler instances
- `workers.go` — `startWorkers()` for Asynq server, scheduler, periodic cleanup

### Task Progress (Redis Pub/Sub)

Task progress events use `TaskProgressNotifier` interface (`server/service/task_events.go`):
- Redis pub/sub primary (`redisNotifier`), database polling fallback (`pollingNotifier`)
- SSE handler in `server/handler/task.go` subscribes via notifier
- Publish points in `task_execution.go`, `task_agent.go`, `task.go`

### Error Handling

- **App library**: `Hinter` interface (`app/errors.go`) for user-friendly hints, `printJSON()` for JSON output
- **Server**: Zerolog structured logging, GORM error handling, JWT error responses
- **WechatAPIError** (`app/wechat/errors.go`): parses WeChat error codes, `IsRetryable()` for transient errors

### WeChat API Integration

- Access token automatically cached/refreshed by wechat SDK
- Material upload: images < 10MB, returns media_id and CDN URL, retry on transient failures
- Draft creation: content < 20,000 chars or 1MB, HTML safe tags only

## Plugin & Agent Ecosystem

Three client access points share the same server MCP API:

### Claude Code Plugin (`claudecode/`)

Git submodule → `anbanai/anbanwriter-claudecode`. Uses Agent + Skill + MCP architecture.

```
claudecode/                        # git submodule → anbanai/anbanwriter-claudecode
├── .claude-plugin/                # Plugin manifest
├── agents/                        # Agent definitions (markdown)
├── skills/                        # 18 Claude Code skills
├── hooks/                         # Lifecycle hooks
├── themes/                        # Conversion themes (YAML)
└── writers/                       # Writing styles (YAML)
```

### OpenClaw Plugin (`openclaw/`)

Git submodule → `anbanai/anbanwriter-openclaw`. OpenClaw-native plugin using SKILL.md-based skills aligned with claudecode.

```
openclaw/                          # git submodule → anbanai/anbanwriter-openclaw
├── openclaw.plugin.json           # Plugin manifest (MCP config, skills root)
├── .mcp.json                      # MCP server config (bundle compatibility)
├── skills/                        # 18 SKILL.md skills (aligned with claudecode)
├── src/                           # TypeScript hooks (quality verification, delivery summaries)
├── themes/                        # Conversion themes (YAML, shared with claudecode)
└── writers/                       # Writing styles (YAML, shared with claudecode)
```

Both plugins connect to the same `anbanwriter` MCP server and share themes/writers.

### Codex Plugin (`codex/`)

Codex-native port of `claudecode/`. Plugin bundle includes skills + MCP + hooks; the five subagents are installed separately (Codex plugins cannot bundle subagents — see GitHub issue #18988).

```
codex/                             # git submodule → anbanai/anbanwriter-codex
├── .codex-plugin/                 # Codex plugin manifest (camelCase fields)
├── .mcp.json                      # MCP server config (identical to claudecode)
├── skills/                        # 18 SKILL.md skills (aligned with claudecode, init adjusted for ~/.codex/config.toml)
├── agents/                        # 5 subagent TOMLs (wechatarticle, seednote, designer, live-slicer, short-video-studio)
├── install/                       # install-subagents.sh + agents-registration.toml
├── hooks/                         # SubagentStop + Stop (Stop replaces Claude Code's TaskCompleted)
├── CODEX.md                       # Codex-specific developer guide
└── README.md                      # End-user install/usage guide
```

Install flow: `codex plugin marketplace add ./codex && codex plugin install anbanwriter`, then `bash codex/install/install-subagents.sh` to register the subagents in `~/.codex/config.toml`.

All three plugins (`claudecode/`, `openclaw/`, `codex/`) connect to the same `anbanwriter` MCP server and share themes/writers/skill content.

## Notes

- CLI uses zerolog logging — all components use zerolog, never mix with zap
- Two Cobra patterns coexist in app/: package-level var with `init()` (older) and factory functions returning `*cobra.Command` (preferred)
- Docker Compose provides MySQL 8.0 + Redis 7 + agent + server containers
- Server binary is `bin/abwriter-server` (not anbanwriter-server)
- **Never modify base UI components in `studio/src/components/ui/`**. These are managed shadcn/ui primitives. If a base component update breaks business logic, fix the business component only — never patch the primitive.
