# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Writer for WeChat** (anbanwriter) is a multi-component content creation platform: a Go CLI tool for transforming Markdown into WeChat-formatted HTML with AI-powered writing and publishing, plus a web application (Go server + React frontend) for managing content plans and task execution.

**Components**:
- **CLI** (`app/`): Go CLI for conversion, writing, humanization, image generation, and direct WeChat publishing
- **Server** (`server/`): Fiber-based HTTP API with MySQL, Redis, Asynq task queue, and MCP endpoint
- **Studio** (`studio/`): React 19 + TypeScript + Vite frontend for content management

- **Language**: Go 1.26.0 (CLI + Server), TypeScript (Web)
- **CLI Framework**: Cobra
- **Logging**: Zap (CLI), Zerolog (Server)
- **WeChat SDK**: silenceper/wechat/v2

## Build & Test Commands

### CLI

```bash
make build                    # Build CLI binary to bin/anbanwriter
make release                  # Build all platform binaries
make test                     # Run all Go tests
make test                     # or: go test -v ./...
go test -v ./app/config       # Run specific package tests
go test -v -run TestConfig_Validate ./app/config  # Run specific test
make coverage                 # Tests with coverage report
make ci                       # Run all CI checks (fmt + vet + test + lint)
make fmt                      # Format code
make vet                      # Static analysis
make lint                     # Lint (requires golangci-lint)
make deps                     # Download and tidy dependencies
make install                  # Install CLI to GOPATH/bin
```

### Server

```bash
make server-build             # Build server binary to bin/anbanwriter-server
make server-dev               # Run server via go run (development)
make server-run               # Build and run server with config
make server-test              # Run server tests
```

### Web Frontend

```bash
make web-install              # Install frontend dependencies
make web-dev                  # Run frontend dev server (proxies API to localhost:8080)
make web-build                # Build frontend for production
```

### Docker Infrastructure

```bash
make docker-up                # Start MySQL 8.0 and Redis 7 containers
make docker-down              # Stop containers
make docker-logs              # Follow container logs
```

## Architecture

### CLI Module Structure (`app/`)

```
app/
├── main.go                 # CLI entry point, command routing
├── {command}.go            # Individual command implementations
├── errors.go               # Hinter interface, AppError type
├── doctor.go               # Diagnostic checks (config, env, network)
├── table.go                # CJK-aware table rendering utilities
│
├── config/                 # Configuration management (JSON)
│   └── config.go           # Single-account config
│
├── converter/              # Markdown → WeChat HTML conversion
│   ├── converter.go        # Core conversion interface & orchestration
│   ├── ai.go               # AI mode (Claude-based)
│   ├── image.go            # Image reference extraction & placeholder handling
│   ├── prompt.go           # AI prompt building with theme support
│   └── theme.go            # Theme management system
│
├── writer/                 # Styled writing assistance
│   ├── assistant.go        # Writing style orchestration
│   ├── generator.go        # Content generation
│   ├── cover_generator.go  # Cover image prompt generation
│   ├── style.go            # Style definition loading (YAML-based)
│   └── types.go            # Data structures
│
├── humanizer/              # AI writing trace removal
│   ├── humanizer.go        # Detection & removal of AI patterns
│   ├── prompt.go           # Humanization prompts
│   └── result.go           # Quality scoring (5 dimensions)
│
├── image/                  # Image processing & generation
│   ├── processor.go        # Unified image handling (upload, compress, generate)
│   ├── compress.go         # Image compression (max 1920px)
│   ├── provider.go         # Provider interface & factory
│   ├── openai.go           # OpenAI DALL-E provider
│   ├── gemini.go           # Google Gemini provider (google.golang.org/genai)
│   ├── openrouter.go       # OpenRouter multi-model gateway
│   └── volcengine.go       # Volcengine Seedream provider (async polling)
│
├── storage/                # Persistent storage (SQLite via GORM)
│   ├── store.go            # Database init, Store struct
│   ├── models.go           # Content, Draft, Image data models
│   ├── content.go          # Content lifecycle CRUD
│   ├── draft.go            # Draft record storage
│   ├── history.go          # Unified history queries
│   └── image.go            # Image record storage
│
├── draft/                  # WeChat draft management
│   └── service.go          # Draft creation & publishing
│
└── wechat/                 # WeChat API wrapper
    ├── service.go          # Material upload, access token management
    └── errors.go           # WechatAPIError with retryable detection
```

### Server Module Structure (`server/`)

Fiber v3 HTTP API with MySQL (GORM), Redis, Asynq task queue, WebSocket, and MCP endpoint.

```
server/
├── main.go                 # Server entry point, wiring, graceful shutdown
├── config.yaml             # Server config (YAML with ANBAN_SERVER_* env overrides)
│
├── agent/                  # Agent execution layer
│   ├── executor.go         # Task execution engine (claude-agent-sdk-go)
│   ├── prompts.go          # Agent prompt templates
│   ├── mcp_server.go       # MCP protocol server
│   ├── mcp_tools.go        # MCP tool definitions
│   └── mcp_draft.go        # MCP draft operations
│
├── auth/                   # Authentication
│   ├── jwt.go              # JWT service (access + refresh tokens)
│   └── wechat.go           # WeChat OAuth login
│
├── config/                 # Server config loading
│   └── config.go           # YAML config with env overrides
│
├── handler/                # HTTP handlers
│   ├── auth.go             # Register, login, refresh, logout, wx-login
│   ├── channel.go          # Channel CRUD (WeChat accounts)
│   ├── plan.go             # Plan CRUD + pause/resume
│   ├── task.go             # Task CRUD + file management + streaming
│   ├── timeline.go         # Unified timeline view
│   └── websocket.go        # WebSocket hub for real-time updates
│
├── middleware/              # HTTP middleware
│   ├── auth.go             # JWT Bearer token validation
│   ├── mcp_auth.go         # MCP API key or JWT auth
│   ├── ratelimit.go        # Redis-based rate limiting
│   └── request_logger.go   # Zerolog request logging
│
├── model/                  # GORM models
│   ├── model.go            # Base model
│   ├── user.go, channel.go, plan.go, task.go, task_file.go, session.go
│   ├── user_config.go      # Legacy (migrating to channel)
│   ├── migration.go        # Auto-migration + UserConfig→Channel migration
│   └── constants.go        # Status constants
│
├── repository/             # Data access layer (MySQL via GORM)
├── service/                # Business logic
│   ├── channel.go, plan.go, task.go
│   ├── task_execution.go   # Task execution with agent SDK
│   └── task_files.go       # Task file management
│
├── router/                 # Fiber router setup
│   └── router.go           # All routes + middleware wiring
│
├── scheduler/              # Background task processing
│   ├── scheduler.go        # Asynq client + processor
│   └── plan_checker.go     # Periodic plan checker
│
├── storage/                # File storage providers
│   ├── factory.go          # Provider factory
│   ├── local.go            # Local filesystem
│   └── oss.go              # Alibaba Cloud OSS
│
├── mcp/                    # MCP endpoint
│   └── mcp.go              # MCP HTTP handler
│
└── integration/
    └── e2e_test.go         # End-to-end tests
```

### Web Frontend (`studio/`)

React 19 + TypeScript + Vite 6 + Tailwind CSS v4. State: Zustand v5. Data fetching: TanStack React Query v5. Routing: React Router DOM v7.

Pages: Login, Register, Dashboard, Channels (WeChat account management), Plans, Tasks, TaskDetail, Timeline, Settings.

Vite dev server proxies `/api` → `localhost:8080` and `/ws` → `ws://localhost:8080`.

### Configuration System (CLI)

**Single Account Support**: One WeChat account via `.anbanwriter/settings.json`.

**Config Search Priority**: CWD → `CLAUDE_PLUGIN_ROOT` → `~/.config/anbanwriter/` → `~/.anbanwriter/` → executable-relative

**Two Loading Modes**:
- `Load()` / `LoadWithDefaults()`: Full validation including WeChat AppID/Secret
- `LoadMinimal()`: Skips WeChat validation, used by commands that don't need WeChat API (write, doctor)

### Conversion Flow

1. **Image Extraction**: Parse Markdown for image references (local/online/AI-generated)
2. **Markdown → HTML**: WeChat-compatible HTML with theme styling
   - **AI Mode**: Claude with theme-specific prompts (autumn-warm, spring-fresh, ocean-calm, custom)
   - All CSS inline, safe HTML tags only
3. **Image Placeholders**: Replace with `<!-- IMG:0 -->` format
4. **Image Processing** (if enabled): compress → upload to WeChat CDN
5. **Placeholder Replacement**: Replace placeholders with CDN URLs
6. **Draft Publishing** (optional): Create draft in WeChat backend

### Image Generation Providers

| Provider | Value | Notes |
|----------|-------|-------|
| OpenAI | `openai` (default) | Synchronous, dall-e-2/dall-e-3 |
| Google Gemini | `gemini` or `google` | Inline image data, `gemini-3-pro-image-preview` |
| OpenRouter | `openrouter` or `or` | Multi-model gateway, base64 images |
| Volcengine/Seedream | `volcengine`, `volc`, `seedream` | Async polling, `doubao-seedream-5-0-250128` |

All providers implement the `Provider` interface (`app/image/provider.go`).

### Writing Styles

Located in `writers/*.yaml`. Each defines core_traits, structure_patterns, language_usage, domain_knowledge.

Built-in: `dan-koe`, `cultural-depth`, `casual-science`

### AI Humanization

Detects and removes 5 categories of AI patterns (content, language, style, filler, collaboration traces).

Intensity levels: `gentle`, `medium`, `aggressive`

Quality scoring (5 dimensions, 10 points each): Directness, Rhythm, Trust, Authenticity, Precision

## Development Patterns

### Adding New CLI Commands

1. Create `app/{command}.go` with cobra command definition
2. Add command to `rootCmd` in `main.go`
3. Use `initConfig()` for lazy config loading (allows --help without config)
4. Return JSON responses via `responseSuccess()` or `responseError()`

### Adding New Image Providers

1. Implement `Provider` interface in `app/image/{provider}.go`
2. Register in `app/image/provider.go` factory
3. Add tests with httptest mocking

### Adding New Themes

1. Create YAML file in `themes/{name}.yaml`
2. Theme system auto-loads with hot-reload support

### Writing Tests

- Table-driven tests for multiple scenarios
- `t.TempDir()` for temp files/dirs
- `httptest.NewServer` for HTTP mocking
- Test both success and error paths

### Error Handling

- **CLI**: `Hinter` interface (`app/errors.go`) for user-friendly hints. Zap structured logging. `responseError()` for JSON output.
- **Server**: Zerolog structured logging. GORM error handling. JWT error responses.
- `WechatAPIError` (`app/wechat/errors.go`): parses WeChat error codes, `IsRetryable()` for transient errors

### WeChat API Integration

- **Access Token**: Automatically cached/refreshed by wechat SDK
- **Material Upload**: Images < 10MB, returns media_id and CDN URL, retry on transient failures
- **Draft Creation**: Content < 20,000 chars or 1MB, HTML safe tags only

## Important Constraints

1. **WeChat HTML**: All CSS inline, no external resources, safe tags only (section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr). No: script, iframe, form, input, style, link.
2. **Image Processing**: Max 1920px width, < 10MB for upload, preserves aspect ratio. Formats: JPG, JPEG, PNG, GIF, BMP, WebP.
3. **Configuration**: CLI config is JSON (`.anbanwriter/settings.json`). Server config is YAML (`server/config.yaml`).
4. **AI Generation**: Prompts in Chinese for better results. Theme prompts define complete styling. Image prompts descriptive but concise.

## CLI Commands Overview

- `anbanwriter account init` - Create config file with guided setup
- `anbanwriter account info [--scope article|xls|rednote]` - Show account profile for AI context
- `anbanwriter account history` - View unified history
- `anbanwriter convert <file>` - Convert Markdown to WeChat HTML
- `anbanwriter write` - Style-based writing assistance
- `anbanwriter humanize <file>` - Remove AI writing traces
- `anbanwriter score <file>` - Evaluate article quality
- `anbanwriter outline` - Generate article outline
- `anbanwriter draft article <json_file>` - Create news article draft from JSON
- `anbanwriter draft xls` - Create Xiaolvshu image posts (max 20 images)
- `anbanwriter image generate <prompt>` - Generate AI images
- `anbanwriter image upload <file>` - Upload image to WeChat CDN
- `anbanwriter image download <url>` - Download image
- `anbanwriter video assemble <images_or_dir>` - Assemble images into video (requires ffmpeg)
- `anbanwriter content track` - Track content lifecycle status
- `anbanwriter content list` - List tracked content
- `anbanwriter workspace prepare <type>` - Archive stale staging and create clean workspace
- `anbanwriter workspace archive <type>` - Archive staging dir to YYYYMMDD-NNN format
- `anbanwriter rednote` - Xiaohongshu content creation
- `anbanwriter topics` - Topic research and generation
- `anbanwriter seo` - SEO analysis and optimization
- `anbanwriter doctor` - Diagnose config and connection issues

## Skills Integration

Skills in `claudecode/skills/`:

- `content-writing` - Article writing workflow
- `topic-research` - Research and scoring
- `seo-optimization` - SEO best practices
- `article-publishing` - Article draft publishing
- `article-visual-design` - Article image generation and management
- `xls-publishing` - Xiaolvshu image post publishing
- `xls-visual-design` - Xiaolvshu visual content (3:4 ratio)
- `rednote-research` - Xiaohongshu topic research
- `rednote-writing` - Xiaohongshu content writing
- `rednote-visual-design` - Xiaohongshu visual content (3:4 ratio)
- `flower-content-design` - Flower photography prompt design
- `flower-visual-design` - Flower image series generation
- `config` - Configuration management

## Plugin & Agent Ecosystem

```
.claude-plugin/
├── plugin.json          # Plugin manifest v2.3.3
└── marketplace.json     # Marketplace listing

hooks/hooks.json         # SessionStart (env setup), SubagentStop, TaskCompleted

agents/
├── wechatarticle.md    # Full article pipeline agent (maxTurns: 50)
├── wechatxls.md        # Image post pipeline agent (maxTurns: 25)
├── rednote.md          # Xiaohongshu creation engine (maxTurns: 20)
└── flower.md           # Flower image generation agent
```

`.mcp.json` configures an MCP server pointing to the server's `/mcp` endpoint.

## Notes for Development

- Go 1.26 features used throughout — ensure compatibility
- CLI uses zap logging, server uses zerolog — don't mix
- JSON responses use `printJSON()` helper in CLI
- Two Cobra patterns coexist: package-level var with `init()` (older) and factory functions returning `*cobra.Command` (preferred)
- Server config overrides via `ANBAN_SERVER_*` env vars
- Docker Compose provides MySQL 8.0 + Redis 7 for local development
