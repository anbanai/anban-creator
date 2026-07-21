# Anban Creator Makefile
# Content creation platform: MCP Server + Web Studio

.DELETE_ON_ERROR:

BINARY      := anban-creator-server
BINDIR      := bin
AGENT_IMAGE := creator-agent:latest
MONTAGE_AGENT_IMAGE ?= creator-agent-montage:latest
SERVER_IMAGE := anban-creator-server:latest
WCFLINK_IMAGE := anban-creator-wcflink:latest
STUDIO_IMAGE := anban-creator-studio:latest
SERVER_CONFIG := server/config.yaml
DOCKER_SOCKET_GID := $(shell stat -L -c '%g' /var/run/docker.sock 2>/dev/null || stat -L -f '%g' /var/run/docker.sock 2>/dev/null || echo 0)

.PHONY: all clean distclean test help lint fmt vet deps ci coverage \
        server-build server-run server-dev server-test git-sync-setup agent-reach-update humanizer-update \
        agent-build-native plugin-binaries \
        web-install web-dev web-build \
        docker-up docker-down docker-logs docker-image \
        docker-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image docker-images

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

# Fast-forward the pinned third-party Agent-Reach source to upstream main.
# Rebuild the Agent image after committing the updated submodule gitlink.
agent-reach-update:
	@scripts/update-agent-reach.sh

# Fast-forward the pinned upstream Humanizer source and mirror its SKILL.md.
# Review the upstream diff and bump plugin versions before release.
humanizer-update:
	@scripts/update-humanizer.sh

# Run all CI checks (format, vet, test, lint)
ci: fmt vet test lint

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
	./$(BINDIR)/$(BINARY) -config $(SERVER_CONFIG)

# Run the server via go run (development)
server-dev:
	@cd server && go run . -config config.yaml

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

# Start all Compose services (MySQL, Redis, wcfLink, server, Studio)
docker-up:
	@DOCKER_GID="$(DOCKER_SOCKET_GID)" docker compose up -d

# Stop infrastructure services
docker-down:
	@docker compose down

# Follow infrastructure logs
docker-logs:
	@docker compose logs -f

# Build the Anban agent Docker image (required for executor: docker)
docker-agent-image:
	@git submodule update --init --recursive
	@echo "Building $(AGENT_IMAGE)..." && \
	docker build -f Dockerfile.agent -t $(AGENT_IMAGE) . && \
	echo "Image build complete: $(AGENT_IMAGE)"

# Build the dedicated Montage Agent image with an immutable OpenMontage template.
docker-montage-agent-image:
	@git submodule update --init --recursive third_party/OpenMontage claudecode
	@echo "Building $(MONTAGE_AGENT_IMAGE)..." && \
	docker build -f Dockerfile.agent-montage \
	  --build-arg OPENMONTAGE_REVISION=$$(git -C third_party/OpenMontage rev-parse HEAD) \
	  -t $(MONTAGE_AGENT_IMAGE) . && \
	echo "Image build complete: $(MONTAGE_AGENT_IMAGE)"

# Build the anban-creator-server Docker image
docker-server-image:
	@git submodule update --init --recursive
	@echo "Building $(SERVER_IMAGE)..." && \
	docker build -f Dockerfile.server -t $(SERVER_IMAGE) . && \
	echo "Image build complete: $(SERVER_IMAGE)"

# Build the wcfLink sidecar Docker image from its pinned upstream source.
docker-wcflink-image:
	@echo "Building $(WCFLINK_IMAGE)..." && \
	docker build -f Dockerfile.wcflink -t $(WCFLINK_IMAGE) . && \
	echo "Image build complete: $(WCFLINK_IMAGE)"

# Build the Studio Docker image from the repository-root build context.
docker-studio-image:
	@echo "Building $(STUDIO_IMAGE)..." && \
	docker build -f Dockerfile.studio -t $(STUDIO_IMAGE) . && \
	echo "Image build complete: $(STUDIO_IMAGE)"

# Build all supported images.
docker-images: docker-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image

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

# Build a fixed plugin-local anban binary into each plugin distribution.
# Override PLUGIN_GOOS/PLUGIN_GOARCH when preparing a platform-specific plugin
# package, e.g. `make plugin-binaries PLUGIN_GOOS=darwin PLUGIN_GOARCH=arm64`.
plugin-binaries:
	@PLUGIN_GOOS="$(PLUGIN_GOOS)" PLUGIN_GOARCH="$(PLUGIN_GOARCH)" scripts/build-plugin-binaries.sh

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
	@echo "  make agent-reach-update - Pull Agent-Reach main and update its gitlink"
	@echo "  make humanizer-update - Pull upstream Humanizer and sync all plugin copies"
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
	@echo "  make docker-montage-agent-image - Build Montage agent image with OpenMontage"
	@echo "  make docker-server-image - Build server image (Go binary)"
	@echo "  make docker-wcflink-image - Build wcfLink sidecar image"
	@echo "  make docker-studio-image - Build Studio image (Bun + nginx)"
	@echo "  make docker-images      - Build all supported images"
	@echo "  make docker-image       - Build agent image (alias)"
	@echo ""
	@echo "Desktop (Tauri) targets:"
	@echo "  make agent-build-native - Build anban natively (desktop sidecar)"
	@echo "  make plugin-binaries    - Bundle anban into claudecode/codex bin/"
