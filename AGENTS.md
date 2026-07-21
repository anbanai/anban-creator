# AGENTS.md

This file provides guidance to AI coding assistants when working in this repository.

## Project Overview

**Anban 智能创作助手** is a Studio-first content creation platform for WeChat articles, Seednote-style notes, live slicing, AI image work, and agent-assisted publishing workflows.

The current repository is not a standalone Cobra CLI. It has four main surfaces:

- `server/`: Go Fiber v3 API server, MCP endpoint, scheduler, storage, publishing, and business services.
- `agent/`: Go runner used for local or Docker-based agent execution.
- `app/`: Shared Go library for content conversion, image generation, humanization, WeChat draft helpers, and writer styles.
- `studio/`: React 19 + TypeScript + Vite 8 Web Studio.

Plugin assets have one canonical source at `plugins/anban/`:

- `.claude-plugin/` and `.codex-plugin/` are native host manifests.
- `skills/`, templates, writers, scripts, and binaries are shared once.
- `agents/*.md` are Claude Code Agents; `agents/*.toml` are Codex subagents.
- `.mcp.json`/`hooks/hooks.json` are Claude adapters; Codex MCP and subagents are registered through `install/`, with completion checks embedded in TOML instructions.

## Build And Test Commands

### Go

```bash
# Run all Go tests
go test ./...

# Run package-specific tests
go test ./server/mcp
go test ./app/image

# Build binaries without colliding with existing directories
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent

# Repository Make targets
make test
make server-build
make vet
make fmt
```

Avoid `go build ./server` or `go build ./agent` from the repository root because Go will try to write `server` or `agent` binaries where same-named directories already exist.

### Studio

Use Bun for the Studio app.

```bash
cd studio && bun install
cd studio && bun run test
cd studio && bun run build
```

The Studio build runs `tsc -b && vite build`.

### Docker

```bash
make docker-up
make docker-down
make docker-agent-image
make docker-server-image
```

## Architecture

### Server

The server follows a layered structure:

- `server/handler`: HTTP handlers and request/response glue.
- `server/service`: business logic for tasks, channels, credits, publishing, model config, live slicing, and writing.
- `server/repository`: GORM persistence layer.
- `server/model`: database models.
- `server/mcp`: MCP tools exposed to connected agents.
- `server/agent`: Claude Code execution support and config builders.
- `server/resources`: embedded themes, layouts, writer configs, and image presets.
- `server/scheduler`: Asynq/cron-style plan checking.
- `server/storage`: local and OSS storage implementations.

Keep handlers thin. Put behavior in services, persistence in repositories, and cross-agent tool contracts in `server/mcp`.

### Agent Runner

`agent/` is a standalone Go binary that downloads workspaces, runs the agent CLI process, and reports results back to the server.

Important files:

- `agent/main.go`: run lifecycle entry point.
- `agent/runner.go`: subprocess execution.
- `agent/downloader.go`: workspace downloads.
- `agent/reporter.go`: result reporting.
- `agent/config.go`: environment/config parsing.

### App Library

`app/` contains reusable content functionality. It is a library, not the main product entry point.

Key packages:

- `app/converter`: Markdown to WeChat-safe inline HTML.
- `app/image`: image compression and OpenAI/Gemini/Volcengine generation providers.
- `app/writer`: style-based writing helpers.
- `app/humanizer`: AI writing trace detection/removal.
- `app/draft` and `app/wechat`: WeChat draft/material helpers.
- `app/config`: JSON config loading for plugin/local workflows.

### Studio

`studio/` is a React 19 application using TypeScript, Vite 8, Tailwind CSS v4, TanStack Query, React Router, and shadcn/Base UI components.

Common locations:

- `studio/src/pages`: route pages.
- `studio/src/components`: shared UI and business components.
- `studio/src/lib/api`: typed API clients.
- `studio/src/lib/schemas.ts`: Zod validation.
- `studio/src/hooks`: reusable hooks.
- `studio/src/test`: Vitest setup and mocks.

Prefer route-level lazy loading and explicit vendor chunking for heavy dependencies. Keep operational pages dense, scannable, and task-oriented.

## Skills And Agents

The project ships all agent-facing workflows from `plugins/anban/agents` and `plugins/anban/skills`.

Current major agents:

- `wechatarticle`: end-to-end WeChat article creation.
- `seednote`: Seednote-style note creation, clone/rewrite, visual generation, and archival.
- `designer`: line-art coloring and visual consistency workflows.
- `live-slicer`: live video transcription, segmentation, ffmpeg export, and optional CapCut draft generation.

Development rules:

- Agents must use MCP tools directly, not ad hoc HTTP clients.
- Local media work in live-slicer uses `ffmpeg` and `ffprobe`.
- Do not reintroduce legacy Python helper scripts for live slicing.
- Keep generated task artifacts explicit and file-backed, especially JSON returned by MCP tools.
- Skills must stay host-neutral. Put unavoidable host syntax in the native manifest, MCP, Hook, Agent, or install adapter rather than duplicating a Skill tree.
- When changing plugin assets under `plugins/anban/` (agents, skills/`SKILL.md`, hooks, themes, writers, manifests, install scripts, or runtime-affecting docs), update both native manifest versions in the same change: `plugins/anban/.claude-plugin/plugin.json` and `plugins/anban/.codex-plugin/plugin.json`. Default to a patch bump unless the release scope warrants minor/major.

## Testing Patterns

Use table-driven Go tests for behavior with multiple cases. Prefer `httptest` for external API providers.

Useful targeted commands:

```bash
go test ./server/mcp -run TestLiveSliceSkillFiles -count=1
go test ./server/service -run TestBuildLive -count=1
cd studio && bun run test -- src/lib/vite-config.test.ts
```

Before claiming completion, run fresh verification that matches the changed surface:

- Go changes: `go test ./...` and relevant `go build -o /tmp/...`.
- Studio changes: `cd studio && bun run test` and `cd studio && bun run build`.
- Skill/agent contract changes: targeted `server/mcp` tests plus full Go tests.

## Implementation Guidance

- Prefer current project patterns over introducing new frameworks.
- For Claude Code-facing features and system-level workflows, design agentic-first: build on official Claude Code capabilities and conventions such as Agents, Skills, Hooks, MCP, configuration, permissions, context management, tool calls, observable progress, and recoverable workflows. Let complex work live in agent workflows instead of duplicating scheduling, plugin discovery, context injection, tool execution, or closed form-wizard frameworks inside the repository. Add custom infrastructure only when official capabilities cannot meet the product need, and document the reason.
- Keep behavior changes covered by tests.
- Do not preserve obsolete compatibility paths in this new project unless a current product path depends on them.
- Do not revert user or submodule changes you did not make.
- Avoid writing secrets or API keys into logs, fixtures, docs, or generated artifacts.
- Use structured errors and user-facing hints where existing code already does.
- For WeChat HTML, keep CSS inline and avoid unsafe tags such as `script`, `iframe`, `form`, `input`, `style`, and `link`.
