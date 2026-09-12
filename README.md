# Anban 智能创作助手

Anban is a Studio-first content creation platform for WeChat articles and Seednote-oriented creation workflows. It combines a Web Studio, MCP tools, Claude Code agent execution, AI image generation, publishing helpers, task tracking, and credits into one repeatable creator workspace.

## Product Surfaces

- **Web Studio** — manage channels, plans, tasks, generated files, credits, model settings, and publishing state.
- **MCP Server** — exposes atomic writing, image, publishing, billing, and Seednote capabilities to connected agents.
- **Agent Runtime** — executes Desktop-claimed tasks or dispatches `article`, `seednote`, and related managed work to one-shot containers.
- **Creation Workflow v1** — turns task output into staged artifacts: topic, outline, draft, final content, visual assets, draft package, and review summary.
- **Plugin Assets** — Claude Code and Codex share one plugin source under `harness/`, with native manifests and host adapters for each harness.

Managed execution uses separate `creator-agent-article`, `creator-agent-seednote`,
and `creator-agent-montage` images. They share the same plugin tree while keeping
the Seednote workflow image independent from the Montage OpenMontage/Remotion/ffmpeg
profile. Seednote Xiaohongshu research flows through authenticated Anban Server MCP
tools backed by the separately deployed `sidecar-seednote`.
Their independent build definitions, together with the Server, Studio, and
sidecar iLink definitions, live in `deploy/docker/` and use the repository root as the
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
- `ANBAN_AGENT_IMAGE_SEEDNOTE`: the independent Seednote workflow image; its Xiaohongshu research uses authenticated Anban Server MCP tools.
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

# TypeScript Agent runtime
cd agent-ts && npm ci && bun run test && bun run typecheck && bun run build

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

Server configuration lives in the local, gitignored `server/config.yaml`. Create it from the tracked example before first run:

```bash
cp server/config.example.yaml server/config.yaml
```

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
`ANBAN_AGENT_EXECUTION_TOKEN_SECRET`, `ANBAN_JWT_SECRET_KEY`, `ANBAN_OSS_ENDPOINT`,
`ANBAN_OSS_ACCESS_KEY_ID`, and `ANBAN_OSS_ACCESS_KEY_SECRET`, plus
`MOONSHOT_API_KEY` for the configured writing
and understanding routes. The checked-in image routes also require
`VOLCENGINE_ARK_API_KEY` and `WANGCAI_OPENAI_API_KEY`. Add any other provider and
integration variables referenced by `server/config.yaml` directly to `.env`;
the relevant optional feature remains unavailable until its credentials are
configured.

The application recognizes exactly three Agent product IDs: `effective`,
`balanced`, and `quality`. Their provider, model, and Claude Code settings live
only under each `claude.execution_profiles.<id>.envs` map. The checked-in
configuration uses DeepSeek, Zhipu, and Moonshot, so set
`ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL` plus `ANBAN_DEEPSEEK_API_KEY`,
`ANBAN_ZHIPU_ANTHROPIC_BASE_URL` plus `ANBAN_ZHIPU_API_KEY`, and
`ANBAN_MOONSHOT_ANTHROPIC_BASE_URL` plus `ANBAN_MOONSHOT_API_KEY` before the
Server starts. A configured profile must include its Base URL, Token, default
model, and the Opus/Fable/Sonnet/Haiku model variables. The remaining Claude
control variables are optional and may be removed to use provider defaults.
Invalid or incomplete configured profiles fail closed; execution never falls
back to another profile or model.

Deployments may select different Anthropic-compatible models by changing the
profile `envs` values and the corresponding `model_usage_aliases` plus provider
cost catalog entries. Do not rename the three product IDs, derive a profile
from a SKU string, or put provider credentials in tasks, snapshots, or billing
records.

Kubernetes reads the billing and execution-token values from Secret
`anban-billing-admin-api-key`, key `api-key`, and Secret
`anban-agent-execution-token`, key `token-secret`, respectively. Configure the
Agent profile variables through Secret `anban-agent-profile-providers`
using keys `deepseek-anthropic-base-url`, `deepseek-api-key`, `moonshot-api-key`,
and `zhipu-api-key`. The Deployment pins the official Moonshot and Zhipu
Anthropic-compatible Base URLs as non-secret values. Configure the
remaining `server/config.yaml` environment references through the deployment's
Secret/config injection. Do not put credentials directly in manifests or
config files.

### Agent profile migration runbook

This release is an incompatible maintenance-window migration. First back up the
database, record task/plan/execution/profile/Quote counts, pause task creation,
plan scheduling, retries, and Agent consumers, then drain or explicitly cancel
every `starting` or `running` execution. Rehearse the complete sequence and
restore on a production-sized database copy before touching production.

Deploy the new Server image with traffic still disabled and run, in order:

```bash
mysql --defaults-extra-file=/secure/mysql.cnf anban_creator \
  < server/migrations/20260730_agent_profile_envs_expand.sql

go run ./server/cmd/agent-profile-envs-backfill \
  --config server/config.yaml --batch-size 500 \
  --legacy-provider-base-url 'volcengine_ark=https://ark.cn-beijing.volces.com/api/compatible' \
  --dry-run
go run ./server/cmd/agent-profile-envs-backfill \
  --config server/config.yaml --batch-size 500 \
  --legacy-provider-base-url 'volcengine_ark=https://ark.cn-beijing.volces.com/api/compatible'
go run ./server/cmd/agent-profile-envs-backfill \
  --config server/config.yaml --batch-size 500 --verify-only

mysql --defaults-extra-file=/secure/mysql.cnf anban_creator \
  < server/migrations/20260730_agent_profile_envs_expire_quotes.sql
mysql --defaults-extra-file=/secure/mysql.cnf anban_creator \
  < server/migrations/20260730_agent_profile_envs_contract.sql
```

Repeat `--legacy-provider-base-url` for every historical snapshot Provider that
differs from the current Provider assigned to the same product profile. The
20260728 migration created `balanced` snapshots with `volcengine_ark`, so that
mapping is required when the current `balanced` example uses Zhipu. Use the
actual historical production endpoint if it differed from the example above.
The mapping restores only the non-secret frozen endpoint; it does not restore a
legacy Token or make a historical task executable after its product profile
changes Provider.

Do not run Contract DDL before `--verify-only` succeeds. The Contract phase
drops legacy execution columns and cannot be rolled back by enabling old IDs;
rollback means restoring the maintenance-window backup. Afterward, publish
catalog `retail-2026-07-30-v7`, mount the new profile config and Secrets, start
the Server, and verify `/api/v1/agent/execution-profiles` exposes only the three
new IDs with the expected availability. Then deploy Studio, Miniapp, and the
TypeScript Agent images, verify Free/Pro/Enterprise creation, retry,
billing detail, and total charge flows, and only then resume schedulers,
consumers, and user traffic.

Users can configure per-account platform credentials and per-user model settings from Studio.

## Repository Layout

```text
server/       Go API server, MCP tools, scheduling, task execution, storage, publishing
agent-ts/     TypeScript Agent runtime used by Docker/Kubernetes and Desktop local execution
app/          Shared Go packages for config, converter, writer, humanizer, image, WeChat draft helpers
studio/       React Web Studio
desktop/      Tauri v2 desktop shell (local-execution client wrapping Studio)
miniapp/      WeChat Mini Program parity client
harness/ Unified Claude Code and Codex plugin: shared skills plus native agents, manifests, MCP, hooks, and installers
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
