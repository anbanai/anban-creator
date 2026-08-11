# Anban Creator Makefile
# Content creation platform: MCP Server + Web Studio

.DELETE_ON_ERROR:

BINARY      := anban-creator-server
BINDIR      := bin
AGENT_IMAGE := creator-agent-article:latest
SEEDNOTE_AGENT_IMAGE ?= creator-agent-seednote:latest
MONTAGE_AGENT_IMAGE ?= creator-agent-montage:latest
TS_AGENT_IMAGE ?= creator-agent-article-ts:latest
TS_SEEDNOTE_AGENT_IMAGE ?= creator-agent-seednote-ts:latest
TS_MONTAGE_AGENT_IMAGE ?= creator-agent-montage-ts:latest
SERVER_IMAGE := anban-creator-server:latest
WCFLINK_IMAGE := anban-creator-wcflink:latest
STUDIO_IMAGE := anban-creator-studio:latest
SERVER_CONFIG := server/config.yaml
DOCKER_SOCKET_GID := $(shell stat -L -c '%g' /var/run/docker.sock 2>/dev/null || stat -L -f '%g' /var/run/docker.sock 2>/dev/null || echo 0)

.PHONY: all clean distclean test help lint fmt vet deps ci coverage \
        server-build server-run server-dev server-test git-sync-setup humanizer-update \
        agent-pack-new agent-pack-generate agent-pack-check \
        agent-build-native \
        web-install web-dev web-build \
        docker-up docker-down docker-logs docker-image \
        docker-agent-image docker-seednote-agent-image docker-montage-agent-image docker-agent-ts-image docker-seednote-agent-ts-image docker-montage-agent-ts-image docker-server-image docker-wcflink-image docker-studio-image docker-images

.PHONY: docker-runtime-smoke

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
	@go test -v ./...

# Code linting (requires golangci-lint)
lint:
	@golangci-lint run ./...

# Format code
fmt:
	@go fmt ./...

# Static analysis
vet:
	@go vet ./...

# Download and tidy dependencies
deps:
	@go mod download
	@go mod tidy

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
	@go run ./server/cmd/agent-pack new $(ARGS)

agent-pack-generate:
	@go run ./server/cmd/agent-pack generate

agent-pack-check:
	@go run ./server/cmd/agent-pack check

# Run all CI checks (format, vet, test, lint)
ci: agent-pack-check fmt vet test lint

# Run tests with coverage report
coverage:
	@go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out
	@echo ""
	@echo "Full report: go tool cover -html=coverage.out"

# ---------------------------------------------------------------------------
# Server targets
# ---------------------------------------------------------------------------

# Build the server binary
server-build:
	@mkdir -p $(BINDIR)
	@go build -o $(BINDIR)/$(BINARY) ./server
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
	@go test -v ./server/...

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
	@git submodule update --init --recursive third_party/claude-agent-sdk-go
	@echo "Building $(AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-article -t $(AGENT_IMAGE) . && \
	echo "Image build complete: $(AGENT_IMAGE)"

# Build the independent Seednote workflow image.
docker-seednote-agent-image:
	@git submodule update --init --recursive third_party/claude-agent-sdk-go
	@echo "Building $(SEEDNOTE_AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-seednote -t $(SEEDNOTE_AGENT_IMAGE) . && \
	echo "Image build complete: $(SEEDNOTE_AGENT_IMAGE)"

# Build the dedicated Montage Agent image with its OpenMontage workspace template.
docker-montage-agent-image:
	@git submodule update --init --recursive third_party/claude-agent-sdk-go third_party/OpenMontage
	@echo "Building $(MONTAGE_AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-montage -t $(MONTAGE_AGENT_IMAGE) . && \
	echo "Image build complete: $(MONTAGE_AGENT_IMAGE)"

# Build TypeScript runtime candidates. They are published under distinct tags;
# Server runtime image mappings remain on the Go images until operational cutover.
docker-agent-ts-image:
	@echo "Building $(TS_AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-article-ts -t $(TS_AGENT_IMAGE) . && \
	echo "Image build complete: $(TS_AGENT_IMAGE)"

docker-seednote-agent-ts-image:
	@echo "Building $(TS_SEEDNOTE_AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-seednote-ts -t $(TS_SEEDNOTE_AGENT_IMAGE) . && \
	echo "Image build complete: $(TS_SEEDNOTE_AGENT_IMAGE)"

docker-montage-agent-ts-image:
	@git submodule update --init --recursive third_party/OpenMontage
	@echo "Building $(TS_MONTAGE_AGENT_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.agent-montage-ts -t $(TS_MONTAGE_AGENT_IMAGE) . && \
	echo "Image build complete: $(TS_MONTAGE_AGENT_IMAGE)"

# The script owns its Docker availability check and all isolated smoke builds.
docker-runtime-smoke:
	@deploy/docker/runtime-smoke.sh

# Build the anban-creator-server Docker image
docker-server-image:
	@git submodule update --init --recursive
	@echo "Building $(SERVER_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.server -t $(SERVER_IMAGE) . && \
	echo "Image build complete: $(SERVER_IMAGE)"

# Build the wcfLink sidecar Docker image from its pinned upstream source.
docker-wcflink-image:
	@echo "Building $(WCFLINK_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.wcflink -t $(WCFLINK_IMAGE) . && \
	echo "Image build complete: $(WCFLINK_IMAGE)"

# Build the Studio Docker image from the repository-root build context.
docker-studio-image:
	@echo "Building $(STUDIO_IMAGE)..." && \
	docker build -f deploy/docker/Dockerfile.studio -t $(STUDIO_IMAGE) . && \
	echo "Image build complete: $(STUDIO_IMAGE)"

# Build all supported images.
docker-images: docker-agent-image docker-seednote-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image

# Backward-compatible alias (builds agent image)
docker-image: docker-agent-image

# ---------------------------------------------------------------------------
# Desktop (Tauri) targets
# ---------------------------------------------------------------------------

# Build the anban binary natively (no Docker) for the host platform.
# The desktop app bundles this as a sidecar to claim & run tasks on the user's
# machine via claude-agent-sdk-go (which spawns the local `claude` CLI).
# Cross-compile the host-native variant; use ARCHES= to override, e.g.
#   make agent-build-native ARCHES="darwin/arm64 darwin/amd64"
ARCHES ?= $(shell go env GOOS)/$(shell go env GOARCH)
PLUGIN_GOOS ?= $(shell go env GOOS)
PLUGIN_GOARCH ?= $(shell go env GOARCH)

agent-build-native:
	@mkdir -p $(BINDIR)
	@set -e; for arch in $(ARCHES); do \
		os=$${arch%/*}; goarch=$${arch#*/}; \
		out="$(BINDIR)/anban-$${os}-$${goarch}"; \
		echo "Building anban for $${os}/$${goarch}..."; \
		GOOS=$${os} GOARCH=$${goarch} CGO_ENABLED=0 go build -trimpath -o $${out} ./agent; \
	done
	@echo "Agent native build complete: $(ARCHES)"

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
	@echo "  make docker-agent-ts-image - Build TypeScript Article runtime candidate"
	@echo "  make docker-seednote-agent-ts-image - Build TypeScript Seednote runtime candidate"
	@echo "  make docker-montage-agent-ts-image - Build TypeScript Montage runtime candidate"
	@echo "  make docker-runtime-smoke - Run isolated Docker dispatch smoke coverage"
	@echo "  make docker-server-image - Build server image (Go binary)"
	@echo "  make docker-wcflink-image - Build wcfLink sidecar image"
	@echo "  make docker-studio-image - Build Studio image (Bun + nginx)"
	@echo "  make docker-images      - Build all supported images"
	@echo "  make docker-image       - Build agent image (alias)"
	@echo ""
	@echo "Desktop (Tauri) targets:"
	@echo "  make agent-build-native - Build anban natively (desktop sidecar)"
