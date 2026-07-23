# Anban 智能创作助手

Anban is a Studio-first content creation platform for WeChat articles and Seednote-oriented creation workflows. It combines a Web Studio, MCP tools, Claude Code agent execution, AI image generation, publishing helpers, task tracking, and credits into one repeatable creator workspace.

## Product Surfaces

- **Web Studio** — manage channels, plans, tasks, generated files, credits, model settings, and publishing state.
- **MCP Server** — exposes writing, image, publishing, billing, workspace, and Seednote formatting tools to connected agents.
- **Agent Runtime** — executes `article`, `seednote`, and related content agents locally or in Docker.
- **Creation Workflow v1** — turns task output into staged artifacts: topic, outline, draft, final content, visual assets, draft package, and review summary.
- **Plugin Assets** — Claude Code and Codex share one plugin source under `plugins/`, with native manifests and host adapters for each harness.

Managed execution uses separate `creator-agent-article`, `creator-agent-seednote`,
and `creator-agent-montage` images. They share the same plugin tree while keeping
Python/Agent-Reach and OpenMontage/Remotion/ffmpeg out of the standard image.
Their independent build definitions, together with the Server, Studio, and
wcfLink definitions, live in `deploy/docker/` and use the repository root as the
Docker build context.

## Managed Agent Runtime

The Server scheduler dispatches every managed execution to a fresh container:
Docker locally or a Kubernetes Job in production. There is no persistent Agent
service and no Server-owned execution directory. The runtime owns the task
workspace and output, retains resume state in the task workspace volume, and
uploads registered task artifacts back through the Server API.

The shared scheduler configuration is:

- `ANBAN_AGENT_EXECUTOR`: `docker` or `kubernetes`.
- `ANBAN_AGENT_IMAGE_ARTICLE`: the minimal Article runtime image.
- `ANBAN_AGENT_IMAGE_SEEDNOTE`: the Python and Agent-Reach runtime image.
- `ANBAN_AGENT_IMAGE_MONTAGE`: the OpenMontage, Remotion, and ffmpeg runtime image.
- `ANBAN_AGENT_EXECUTION_TOKEN_SECRET`: a private value of at least 32 bytes used
  to mint short-lived workload tokens.

The Server never builds missing runtime images. `make docker-up` builds the three
local runtime images before starting Compose; direct `docker compose up` users
must build or pull them first. Production deployments should publish and select
immutable image digests so a persisted execution can always resume with the
same runtime identity.

The Docker executor requires access to a Docker daemon. Compose mounts
`/var/run/docker.sock` into the Server and uses the host socket group through
`DOCKER_GID`; anyone deploying the Server another way must provide equivalent
daemon access. Kubernetes instead uses the namespace, task/project PVC storage
classes, and ServiceAccount settings in `server/Deployment.yaml`, while reading
the execution token secret from a Kubernetes Secret.

## Quick Start

```bash
# Create the local Server environment file.
cp .env.example .env
# Fill every required value in .env before starting Compose.

# Start infra and services with Docker Compose
# (builds all three managed runtime images before startup)
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

Docker Compose loads the root `.env` into the Server container and rejects a
startup with any required value missing. Populate all required entries from
`.env.example`: `ANBAN_BILLING_ADMIN_API_KEY`, a 32-byte or longer
`ANBAN_AGENT_EXECUTION_TOKEN_SECRET`, `ANBAN_JWT_SECRET_KEY`,
`CLAUDE_CODE_AUTH_TOKEN`, `ANBAN_OSS_ENDPOINT`, `ANBAN_OSS_ACCESS_KEY_ID`, and
`ANBAN_OSS_ACCESS_KEY_SECRET`, plus `MOONSHOT_API_KEY` for the configured writing
and understanding routes. Additional provider and integration variables used by
`server/config.yaml` can also be added to `.env`; the relevant optional feature
will remain unavailable until its credentials are configured.

Kubernetes reads the billing and execution-token values from Secret
`anban-billing-admin-api-key`, key `api-key`, and Secret
`anban-agent-execution-token`, key `token-secret`, respectively. Configure the
remaining `server/config.yaml` environment references through the deployment's
Secret/config injection. Do not put credentials directly in manifests or config
files.

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
