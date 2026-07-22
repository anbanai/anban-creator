# Anban 智能创作助手

Anban is a Studio-first content creation platform for WeChat articles and Seednote-oriented creation workflows. It combines a Web Studio, MCP tools, Claude Code agent execution, AI image generation, publishing helpers, task tracking, and credits into one repeatable creator workspace.

## Product Surfaces

- **Web Studio** — manage channels, plans, tasks, generated files, credits, model settings, and publishing state.
- **MCP Server** — exposes writing, image, publishing, billing, workspace, and Seednote formatting tools to connected agents.
- **Agent Runtime** — executes `wechatarticle`, `seednote`, and related content agents locally or in Docker.
- **Creation Workflow v1** — turns task output into staged artifacts: topic, outline, draft, final content, visual assets, draft package, and review summary.
- **Plugin Assets** — Claude Code and Codex share one plugin source under `plugins/`, with native manifests and host adapters for each harness.

Managed execution uses separate `creator-agent-article`, `creator-agent-seednote`,
and `creator-agent-montage` images. They share the same plugin tree while keeping
Python/Agent-Reach and OpenMontage/Remotion/ffmpeg out of the standard image.

## Quick Start

```bash
# Configure the required private billing top-up credential.
cp .env.example .env
# Set ANBAN_BILLING_ADMIN_API_KEY in .env before starting Compose.

# Start infra and services with Docker Compose
# (builds the Seednote and Montage profile images before startup)
make docker-up

# Or run server and web separately during development
make server-dev
make web-dev
```

Server build:

```bash
make server-build
./bin/anban-creator-server -config server/config.yaml
```

Frontend build:

```bash
make web-build
```

## Development Commands

```bash
# Go tests
go test ./...

# Frontend tests
cd studio && bun run test

# Frontend production build
cd studio && bun run build

# Format and vet Go code
make fmt
make vet
```

## Core Workflow

1. Create a channel in Studio with platform, account positioning, keywords, style, theme, and optional publishing credentials.
2. Create a manual task or scheduled plan.
3. The agent writes canonical artifacts into `output/`.
4. The server uploads task files and builds workflow metadata from known artifacts.
5. Studio shows stage progress, generated files, review readiness, warnings, and publishing state.
6. Optional auto-publishing creates WeChat article draft entries when enabled.

Canonical Creation Workflow v1 artifacts include:

- `01-topic.json`
- `02-outline.md`
- `03-draft.md`
- `04-final.md`
- `05-article.html`
- `cover.png`
- `images.json`
- `draft.json`
- `review.json`

## Supported Platforms

- **公众号文章** — long-form Markdown/HTML conversion, cover generation, WeChat draft publishing.
- **种草笔记/Seednote** — profile-assisted content creation and export formatting; direct publishing is not part of v1.

## Configuration

Server configuration lives in `server/config.yaml`; use `server/config.example.yaml` as a starting point.

Important sections:

- database and Redis
- Claude Code executor
- storage provider
- image generation providers
- writing model defaults
- WeChat auth and publishing settings
- credits and invitation rules

Docker Compose requires `ANBAN_BILLING_ADMIN_API_KEY` in the root `.env` file. Kubernetes deployments read the same variable from Secret `anban-billing-admin-api-key`, key `api-key`; do not put the credential directly in manifests or config files.

Users can configure per-account platform credentials and per-user model settings from Studio.

## Repository Layout

```text
server/       Go API server, MCP tools, scheduling, task execution, storage, publishing
agent/        Standalone agent runner used by Docker/local execution
app/          Shared Go packages for config, converter, writer, humanizer, image, WeChat draft helpers
studio/       React Web Studio
desktop/      Tauri v2 desktop shell (local-execution client wrapping Studio)
miniapp/      WeChat Mini Program parity client
plugins/ Unified Claude Code and Codex plugin: shared skills plus native agents, manifests, MCP, hooks, and installers
docs/         Design specs and implementation plans
```

## Testing

Run full backend and frontend verification before shipping:

```bash
go test ./...
cd studio && bun run test
cd studio && bun run build
```

## License

MIT
