# AnbanWriter 智能创作平台

AnbanWriter is a Studio-first content creation platform for WeChat articles, Xiaolvshu image posts, and Rednote-oriented creation workflows. It combines a Web Studio, MCP tools, Claude/OpenClaw agent execution, AI image generation, publishing helpers, task tracking, and credits into one repeatable creator workspace.

## Product Surfaces

- **Web Studio** — manage channels, plans, tasks, generated files, credits, model settings, and publishing state.
- **MCP Server** — exposes writing, image, publishing, billing, workspace, and Rednote formatting tools to connected agents.
- **Agent Runtime** — executes `wechatarticle`, `wechatxls`, and related content agents locally or in Docker.
- **Creation Workflow v1** — turns task output into staged artifacts: topic, outline, draft, final content, visual assets, draft package, and review summary.
- **Plugin Assets** — Claude/OpenClaw skills, agents, themes, and writer styles live under `claudecode/` and `openclaw/`.

## Quick Start

```bash
# Start infra and services with Docker Compose
make docker-up

# Or run server and web separately during development
make server-dev
make web-dev
```

Server build:

```bash
make server-build
./bin/abwriter-server -config server/config.yaml
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
cd studio && npm test -- --run

# Frontend production build
cd studio && npm run build

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
6. Optional auto-publishing creates WeChat article or Xiaolvshu draft entries when enabled.

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
- **小绿书/XLS** — image post generation, image upload, WeChat newspic draft publishing.
- **小红书/Rednote** — profile-assisted content creation and export formatting; direct publishing is not part of v1.

## Configuration

Server configuration lives in `server/config.yaml`; use `server/config.example.yaml` as a starting point.

Important sections:

- database and Redis
- Claude/OpenClaw executor
- storage provider
- image generation providers
- writing model defaults
- WeChat auth and publishing settings
- credits and invitation rules

Users can configure per-account platform credentials and per-user model settings from Studio.

## Repository Layout

```text
server/       Go API server, MCP tools, scheduling, task execution, storage, publishing
agent/        Standalone agent runner used by Docker/local execution
app/          Shared Go packages for config, converter, writer, humanizer, image, WeChat draft helpers
studio/       React Web Studio
claudecode/   Claude Code plugin assets: agents, skills, themes, writer styles
openclaw/     OpenClaw plugin distribution assets
docs/         Design specs and implementation plans
```

## Testing

Run full backend and frontend verification before shipping:

```bash
go test ./...
cd studio && npm test -- --run
cd studio && npm run build
```

## License

MIT
