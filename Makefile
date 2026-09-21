# Anban Creator Makefile
# Content creation platform: MCP Server + Web Studio

.DELETE_ON_ERROR:

BINARY      := anban-creator-server
BINDIR      := bin
AGENT_IMAGE := creator-agent-article:latest
SEEDNOTE_AGENT_IMAGE ?= creator-agent-seednote:latest
MONTAGE_AGENT_IMAGE ?= creator-agent-montage:latest
HYPIT_AGENT_IMAGE ?= creator-agent-hypit:latest
HYPIT_SOURCE_REPO ?= https://github.com/hypit-ai/hypit.git
HYPIT_SOURCE_REF ?= 5d257c5a50291398d2bca34afb93c22f1ab5c295
OPENMONTAGE_SOURCE_REPO ?= https://github.com/calesthio/OpenMontage.git
OPENMONTAGE_SOURCE_REF ?= 08e2151fa02de28a5d6a312b3d575692bf147ad7
SERVER_IMAGE := anban-creator-server:latest
SIDECAR_ILINK_IMAGE ?= anban-creator-sidecar-ilink:latest
SIDECAR_SEEDNOTE_IMAGE ?= anban-creator-sidecar-seednote:latest
SIDECAR_ILINK_REPO ?= https://github.com/lich0821/wcfLink.git
SIDECAR_ILINK_REF ?= master
SIDECAR_SEEDNOTE_REPO ?= https://github.com/xpzouying/xiaohongshu-mcp.git
SIDECAR_SEEDNOTE_REF ?= main
STUDIO_IMAGE := anban-creator-studio:latest
SERVER_CONFIG := server/config.yaml
DOCKER_SOCKET_GID := $(shell stat -L -c '%g' /var/run/docker.sock 2>/dev/null || stat -L -f '%g' /var/run/docker.sock 2>/dev/null || echo 0)

.PHONY: all clean distclean test help lint fmt vet deps ci coverage \
        server-build server-run server-dev server-test git-sync-setup humanizer-update \
        agent-pack-new agent-pack-generate agent-pack-check \
        agent-install agent-test agent-build \
        web-install web-dev web-build \
        docker-up docker-down docker-logs docker-image \
        docker-agent-image docker-seednote-agent-image docker-montage-agent-image docker-hypit-agent-image docker-server-image docker-sidecar-ilink-image docker-sidecar-seednote-image docker-studio-image docker-images

.PHONY: docker-runtime-smoke docker-hypit-smoke

# Default target
all: server-build

# ---------------------------------------------------------------------------
# Shared targets
# ---------------------------------------------------------------------------

# Clean all build artifacts
clean:
	@rm -rf $(BINDIR)/ studio/dist/ dist/ release/
	@rm -f *.log

distclean: clean
	@rm -rf studio/node_modules/

# Run all tests
test:
	@go -C server test -v ./...
	@cd agent-ts && bun run test

# Code linting (requires golangci-lint)
lint:
	@cd server && golangci-lint run ./...

# Format code
fmt:
	@go -C server fmt ./...

# Static analysis
vet:
	@go -C server vet ./...

# Download and tidy dependencies
deps:
	@go -C server mod download
	@go -C server mod tidy

# Configure repository-local pull/push behavior for managed submodules.
git-sync-setup:
	@scripts/setup-git-sync.sh

# Delegate the pinned Humanizer gitlink update to Creator Skills.
# The child target validates the upstream checkout and plugin manifests.
humanizer-update:
	@scripts/update-humanizer.sh

# Scaffold, generate, and verify the canonical Agent Pack catalog. Pass
# scaffold flags through ARGS, for example:
#   make agent-pack-new ARGS="-id my-agent -kind managed -task-type my-agent -runtime-profile article"
agent-pack-new:
	@go -C server run ./cmd/agent-pack new -plugin-root ../harness $(ARGS)

agent-pack-generate:
	@go -C server run ./cmd/agent-pack generate -plugin-root ../harness -catalog agentpack/catalog.generated.json

agent-pack-check:
	@go -C server run ./cmd/agent-pack check -plugin-root ../harness -catalog agentpack/catalog.generated.json

# Run all CI checks (format, vet, test, lint)
ci: agent-pack-check fmt vet test lint

# Run tests with coverage report
coverage:
	@go -C server test -coverprofile=../coverage.out ./...
	@go -C server tool cover -func=../coverage.out
	@echo ""
	@echo "Full report: go tool cover -html=coverage.out"

# ---------------------------------------------------------------------------
# Server targets
# ---------------------------------------------------------------------------

# Build the server binary
server-build:
	@mkdir -p $(BINDIR)
	@go -C server build -o ../$(BINDIR)/$(BINARY) .
	@echo "Server build complete: $(BINDIR)/$(BINARY)"

# Build and run the server
server-run: server-build
	@set -a; [ ! -f .env ] || . ./.env; set +a; \
		./$(BINDIR)/$(BINARY) -config $(SERVER_CONFIG)

# Run the server via go run (development)
server-dev:
	@set -a; [ ! -f .env ] || . ./.env; set +a; \
		cd server && go run . -config config.yaml

# Run server tests
server-test:
	@go -C server test -v ./...

# ---------------------------------------------------------------------------
# Agent targets
# ---------------------------------------------------------------------------

agent-install:
	@cd agent-ts && bun install --frozen-lockfile

agent-test:
	@cd agent-ts && bun run test
	@cd agent-ts && bun run typecheck

agent-build:
	@cd agent-ts && bun run build

# ---------------------------------------------------------------------------
# Frontend targets
# ---------------------------------------------------------------------------

# Install frontend dependencies
web-install:
	@cd studio && bun install

# Run frontend dev server
web-dev:
	@cd studio && bun run dev

# Build frontend for production
web-build:
	@cd studio && bun run build

# ---------------------------------------------------------------------------
# Docker targets
# ---------------------------------------------------------------------------

# Build all one-shot task runtimes, then start the Compose services. Runtime
# containers are launched on demand by the server and are not Compose services.
docker-up: docker-agent-image docker-seednote-agent-image docker-montage-agent-image
	@DOCKER_GID="$(DOCKER_SOCKET_GID)" docker compose up -d

# Stop infrastructure services
docker-down:
	@docker compose down

# Follow infrastructure logs
docker-logs:
	@docker compose logs -f

# Build the minimal Article Agent image.
docker-agent-image:
	@echo "Building $(AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-article -t $(AGENT_IMAGE) . && \
	echo "Image build complete: $(AGENT_IMAGE)"

# Build the independent Seednote workflow image.
docker-seednote-agent-image:
	@echo "Building $(SEEDNOTE_AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-seednote -t $(SEEDNOTE_AGENT_IMAGE) . && \
	echo "Image build complete: $(SEEDNOTE_AGENT_IMAGE)"

# Build the dedicated Montage Agent with its pinned OpenMontage runtime.
docker-montage-agent-image:
	@echo "Building $(MONTAGE_AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-montage \
		--build-arg OPENMONTAGE_REPO="$(OPENMONTAGE_SOURCE_REPO)" \
		--build-arg OPENMONTAGE_REF="$(OPENMONTAGE_SOURCE_REF)" \
		-t $(MONTAGE_AGENT_IMAGE) . && \
	echo "Image build complete: $(MONTAGE_AGENT_IMAGE)"

# Build official source and prepare its tools without modifying upstream code.
docker-hypit-agent-image:
	docker build --platform linux/amd64 -f deploy/docker/Dockerfile.agent-hypit \
		--build-arg HYPIT_REPO="$(HYPIT_SOURCE_REPO)" \
		--build-arg HYPIT_REF="$(HYPIT_SOURCE_REF)" \
		-t $(HYPIT_AGENT_IMAGE) .

docker-hypit-smoke:
	@deploy/docker/hypit-smoke.sh "$(HYPIT_AGENT_IMAGE)" "$(HYPIT_SOURCE_REF)"

# The script owns its Docker availability check and all isolated smoke builds.
docker-runtime-smoke:
	@deploy/docker/runtime-smoke.sh

# Build the anban-creator-server Docker image
docker-server-image:
	@git submodule update --init --recursive
	@echo "Building $(SERVER_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.server -t $(SERVER_IMAGE) . && \
	echo "Image build complete: $(SERVER_IMAGE)"

docker-sidecar-ilink-image:
	@echo "Building $(SIDECAR_ILINK_IMAGE)..." && \
	docker build --pull --no-cache -f deploy/docker/Dockerfile.sidecar-ilink \
		--build-arg ILINK_REPO="$(SIDECAR_ILINK_REPO)" \
		--build-arg ILINK_REF="$(SIDECAR_ILINK_REF)" \
		-t $(SIDECAR_ILINK_IMAGE) . && \
	echo "Image build complete: $(SIDECAR_ILINK_IMAGE)"

docker-sidecar-seednote-image:
	@echo "Building $(SIDECAR_SEEDNOTE_IMAGE)..." && \
	docker build --pull --no-cache -f deploy/docker/Dockerfile.sidecar-seednote \
		--build-arg SEEDNOTE_REPO="$(SIDECAR_SEEDNOTE_REPO)" \
		--build-arg SEEDNOTE_REF="$(SIDECAR_SEEDNOTE_REF)" \
		-t $(SIDECAR_SEEDNOTE_IMAGE) . && \
	echo "Image build complete: $(SIDECAR_SEEDNOTE_IMAGE)"

# Build the Studio Docker image from the repository-root build context.
docker-studio-image:
	@echo "Building $(STUDIO_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.studio -t $(STUDIO_IMAGE) . && \
	echo "Image build complete: $(STUDIO_IMAGE)"

# Build all supported images.
docker-images: docker-agent-image docker-seednote-agent-image docker-montage-agent-image docker-hypit-agent-image docker-server-image docker-sidecar-ilink-image docker-sidecar-seednote-image docker-studio-image

# Backward-compatible alias (builds agent image)
docker-image: docker-agent-image

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

help:
	@echo "Anban Creator - Content Creation Platform"
	@echo ""
	@echo "Shared targets:"
	@echo "  make test          - Run all tests"
	@echo "  make coverage      - Run tests with coverage report"
	@echo "  make ci            - Run all CI checks (fmt + vet + test + lint)"
	@echo "  make vet           - Static analysis"
	@echo "  make fmt           - Format code"
	@echo "  make lint          - Lint code (requires golangci-lint)"
	@echo "  make deps          - Download and tidy dependencies"
	@echo "  make humanizer-update - Pull upstream Humanizer and sync all plugin copies"
	@echo "  make agent-pack-new ARGS=... - Scaffold a canonical Agent Pack"
	@echo "  make agent-pack-generate - Generate native Agents and Server Catalog"
	@echo "  make agent-pack-check - Fail when generated Agent Pack assets drift"
	@echo "  make agent-install  - Install TypeScript Agent dependencies"
	@echo "  make agent-test     - Test and type-check the TypeScript Agent"
	@echo "  make agent-build    - Compile the TypeScript Agent"
	@echo "  make clean         - Remove build artifacts"
	@echo "  make distclean     - Remove build artifacts + dependencies"
	@echo ""
	@echo "Server targets:"
	@echo "  make server-build  - Build server binary to bin/anban-creator-server"
	@echo "  make server-run    - Build and run server with config"
	@echo "  make server-dev    - go run server (development)"
	@echo "  make server-test   - Run server tests"
	@echo ""
	@echo "Frontend targets:"
	@echo "  make web-install   - Install frontend dependencies (bun)"
	@echo "  make web-dev       - Run frontend dev server (bun)"
	@echo "  make web-build     - Build frontend for production (bun)"
	@echo ""
	@echo "Docker targets:"
	@echo "  make docker-up          - Start all Compose services"
	@echo "  make docker-down        - Stop containers"
	@echo "  make docker-logs        - Follow container logs"
	@echo "  make docker-agent-image - Build agent image (Claude Code + plugin)"
	@echo "  make docker-seednote-agent-image - Build independent Seednote workflow image"
	@echo "  make docker-montage-agent-image - Build Montage agent image with OpenMontage"
	@echo "  make docker-hypit-agent-image - Build video replication runtime from official source"
	@echo "  make docker-hypit-smoke - Verify official runtime and local render without paid generation"
	@echo "  make docker-runtime-smoke - Run isolated Docker dispatch smoke coverage"
	@echo "  make docker-server-image - Build server image (Go binary)"
	@echo "  make docker-sidecar-ilink-image - Build latest upstream iLink sidecar image"
	@echo "  make docker-sidecar-seednote-image - Build latest upstream Seednote sidecar image"
	@echo "  make docker-studio-image - Build Studio image (Bun + nginx)"
	@echo "  make docker-images      - Build all supported images"
	@echo "  make docker-image       - Build agent image (alias)"
