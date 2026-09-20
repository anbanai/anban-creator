# AGENTS.md

This file provides guidance to AI coding assistants when working in this repository.

## Project Overview

**Anban 自媒体智能创作助手** is a Studio-first content creation platform for WeChat articles, Seednote-style notes, live slicing, AI image work, and agent-assisted publishing workflows.

The current repository is not a standalone Cobra CLI. It has three main surfaces:

- `server/`: Go Fiber v3 API server, MCP endpoint, scheduler, storage, publishing, business services, and Server-owned reusable Go packages under `server/app/`.
- `agent-ts/`: TypeScript runner used for one-shot managed agent execution.
- `studio/`: React 19 + TypeScript + Vite 8 Web Studio.

Plugin assets have one canonical source at `harness/`:

- `.claude-plugin/` and `.codex-plugin/` are native host manifests.
- `skills/`, templates, writers, scripts, and binaries are shared once.
- `agents/*.md` are Claude Code Agents; `agents/*.toml` are Codex subagents.
- `.mcp.json`/`hooks/hooks.json` are Claude adapters; Codex MCP and subagents are registered through `install/`, with completion checks embedded in TOML instructions.

## Server / Harness / MCP Boundary

- Harness owns non-deterministic content/image generation, semantic review, and file artifacts only. Managed Article Agents must hand off `output/draft.json` and must not trigger automatic WeChat draft publication.
- Server owns identity, frozen snapshots, publication readiness policy, external WeChat calls, state machines, idempotency, retries, billing, persistence, reconciliation, and public outcomes. Only the Server finalizer may automatically create or formally publish a WeChat article.
- MCP is an atomic, stateless transport layer. It may authenticate, validate, invoke one capability, and encode its result, but it must not compose workflow steps or decide the final publication state. Explicit interactive draft creation remains an independent MCP capability.
- Never treat Agent logs or legacy result files as evidence that WeChat received a request. `ambiguous` is valid only when durable publication evidence records a `DraftAddAttemptedAt`.

## Build And Test Commands

### Go

```bash
# Run all Go tests
cd server && go test ./...

# Run package-specific tests
cd server && go test ./mcp
cd server && go test ./app/image

# Build the Server binary
cd server && go build -o /tmp/anban-creator-server .

# Repository Make targets
make test
make server-build
make vet
make fmt
```

The Go module root is `server/`; run direct Go commands there or use the repository Make targets.

### Agent Runtime

```bash
cd agent-ts && npm ci
cd agent-ts && bun run test
cd agent-ts && bun run typecheck
cd agent-ts && bun run build
```

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
make docker-seednote-agent-image
make docker-montage-agent-image
make docker-server-image
```

All repository-owned Docker build definitions live under `deploy/docker/`; keep
the repository root as the build context. Agent profiles use independent
`Dockerfile.agent-article`, `Dockerfile.agent-seednote`, and
`Dockerfile.agent-montage` files rather than stages inherited from one business
image.

The managed runtime is split into three images: `creator-agent-article` for the
minimal Article runtime, `creator-agent-seednote` for the independent Seednote
workflow, and `creator-agent-montage` for OpenMontage/Remotion/ffmpeg. Seednote
Xiaohongshu research flows through authenticated Anban Server MCP tools backed by
the separately deployed `sidecar-seednote`. Keep the canonical
plugin tree intact in every image; image selection controls system dependencies,
not which Skills are distributed.

Managed execution is server-scheduled and one-shot. Every execution receives a
fresh container (or Kubernetes Job), and the runtime owns its workspace and
output. Do not add a persistent Agent service, a shared host workspace mount, or
Server-side execution of Agent workflow steps. Montage always runs from the
runtime-provided `/workspace/openmontage` project root.

Configure dispatch with `ANBAN_AGENT_EXECUTOR`,
`ANBAN_AGENT_IMAGE_ARTICLE`, `ANBAN_AGENT_IMAGE_SEEDNOTE`,
`ANBAN_AGENT_IMAGE_MONTAGE`, and `ANBAN_AGENT_EXECUTION_TOKEN_SECRET`. The
Server never builds a missing runtime image; build or publish all selected
images before dispatch and prefer immutable digests outside local development.
The Compose Docker executor requires the Docker daemon socket mounted at
`/var/run/docker.sock` and membership in its host group. Kubernetes retains
namespace, task/project PVC storage class, runtime ServiceAccount, image pull
secret, and execution-token Secret settings in `server/Deployment.yaml`.

## Architecture

### Server

The server follows a layered structure:

- `server/handler`: HTTP handlers and request/response glue.
- `server/service`: business logic for tasks, channels, credits, publishing, image capabilities, live slicing, and writing.
- `server/repository`: GORM persistence layer.
- `server/model`: database models.
- `server/mcp`: MCP tools exposed to connected agents.
- `server/agent`: Claude Code execution support and config builders.
- `server/resources`: embedded themes, layouts, and writer configs.
- `server/scheduler`: Asynq/cron-style plan checking.
- `server/storage`: local and OSS storage implementations.

Keep handlers thin. Put behavior in services, persistence in repositories, and cross-agent tool contracts in `server/mcp`.

### Agent Runner

`agent-ts/` is the single Agent runtime. It supports both the managed `job`
entrypoint used by Docker/Kubernetes.

Important files:

- `agent-ts/src/main.ts`: managed job lifecycle entry point.
- `agent-ts/src/runner.ts`: Claude Agent SDK execution and stream handling.
- `agent-ts/src/bootstrap.ts`: managed Bootstrap validation.
- `agent-ts/src/downloads.ts`: generated image materialization.
- `agent-ts/src/reporter.ts`: progress, artifact, and completion reporting.

### Server App Packages

`server/app/` contains reusable content functionality owned exclusively by the Server.

Key packages:

- `server/app/converter`: Markdown to WeChat-safe inline HTML.
- `server/app/image`: image compression and OpenAI/Gemini/Volcengine generation providers.
- `server/app/writer`: style-based writing helpers.
- `server/app/draft` and `server/app/wechat`: WeChat draft/material helpers.
- `server/app/config`: JSON configuration helpers used by Server integrations.

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

The project ships all agent-facing workflows from `harness/agents` and `harness/skills`.

Current major agents:

- `article`: end-to-end WeChat article creation.
- `seednote`: Seednote-style note creation, clone/rewrite, visual generation, and archival.
- `live-slicer`: live video transcription, segmentation, ffmpeg export, and optional CapCut draft generation.

Development rules:

- Agent Packs under `harness/packs/<id>/` are the canonical execution and distribution units. They never replace strong business identity: first-class scenarios keep explicit `Project.Platform` and `Task.Type`; do not add compatibility fields such as `platform_or_type`.
- Use `make agent-pack-new`, then `make agent-pack-generate` and `make agent-pack-check`. Managed scaffolds start with plugin-only surfaces; add `project`, `task`, or `plan` only after the typed Go/Studio business fields, Schema validation, billing operation/SKUs, and service/UI surface are implemented.
- Runtime profiles express dependency images; runtime adapters express exceptional workspace layouts. Keep ordinary workflow sequencing in Agents/Skills, not adapters or MCP handlers.
- Agents must use MCP tools directly, not ad hoc HTTP clients.
- Local media work in live-slicer uses `ffmpeg` and `ffprobe`.
- Do not reintroduce legacy Python helper scripts for live slicing.
- Keep generated task artifacts explicit and file-backed, especially JSON returned by MCP tools.
- Skills must stay host-neutral. Put unavoidable host syntax in the native manifest, MCP, Hook, Agent, or install adapter rather than duplicating a Skill tree.
- When changing plugin assets under `harness/` (agents, skills/`SKILL.md`, hooks, themes, writers, manifests, install scripts, or runtime-affecting docs), update both native manifest versions in the same change: `harness/.claude-plugin/plugin.json` and `harness/.codex-plugin/plugin.json`. Default to a patch bump unless the release scope warrants minor/major.

## Testing Patterns

Use table-driven Go tests for behavior with multiple cases. Prefer `httptest` for external API providers.

Useful targeted commands:

```bash
cd server && go test ./mcp -run TestLiveSliceSkillFiles -count=1
cd server && go test ./service -run TestBuildLive -count=1
cd studio && bun run test -- src/lib/vite-config.test.ts
```

Before claiming completion, run fresh verification that matches the changed surface:

- Go changes: `cd server && go test ./...` and relevant `cd server && go build -o /tmp/... .`.
- Studio changes: `cd studio && bun run test` and `cd studio && bun run build`.
- Skill/agent contract changes: targeted `server/mcp` tests plus full Go tests.

## Implementation Guidance

- Prefer current project patterns over introducing new frameworks.
- For Claude Code-facing features and system-level workflows, design agentic-first: build on official Claude Code capabilities and conventions such as Agents, Skills, Hooks, MCP, configuration, permissions, context management, tool calls, observable progress, and recoverable workflows. Let complex work live in agent workflows instead of duplicating scheduling, plugin discovery, context injection, tool execution, or closed form-wizard frameworks inside the repository. Add custom infrastructure only when official capabilities cannot meet the product need, and document the reason.
- MCP is a stateless capability transport. Agents and Skills own orchestration only within non-deterministic generation workflows, including creative sequencing, provider retries, semantic quality gates, and stop/continue decisions. Deterministic business state, external side effects, idempotency, reconciliation, and recovery retries belong to the Server. An MCP handler may authenticate, validate protocol and security constraints, invoke one application capability, and encode its result. It must not compose multiple domain services or conditionally run another capability. Atomic persistence and settlement belong in the application service.
- Keep behavior changes covered by tests.
- Do not preserve obsolete compatibility paths in this new project unless a current product path depends on them.
- Do not revert user or submodule changes you did not make.
- Avoid writing secrets or API keys into logs, fixtures, docs, or generated artifacts.
- Use structured errors and user-facing hints where existing code already does.
- For WeChat HTML, keep CSS inline and avoid unsafe tags such as `script`, `iframe`, `form`, `input`, `style`, and `link`.
