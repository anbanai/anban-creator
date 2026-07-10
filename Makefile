# Anban Creator Makefile
# Content creation platform: MCP Server + Web Studio

.DELETE_ON_ERROR:

BINARY      := anban-creator-server
BINDIR      := bin
AGENT_IMAGE := anban-creator-agent:latest
SERVER_IMAGE := anban-creator-server:latest
SERVER_CONFIG := server/config.yaml

.PHONY: all clean distclean test help lint fmt vet deps ci coverage \
        server-build server-run server-dev server-test git-sync-setup \
        agent-build-native plugin-binaries \
        web-install web-dev web-build \
        docker-up docker-down docker-logs docker-image \
        docker-agent-image docker-server-image docker-images

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

# Start all services (MySQL, Redis, agent, server)
docker-up:
	@docker compose up -d

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

# Build the anban-creator-server Docker image
docker-server-image:
	@git submodule update --init --recursive
	@echo "Building $(SERVER_IMAGE)..." && \
	docker build -f Dockerfile.server -t $(SERVER_IMAGE) . && \
	echo "Image build complete: $(SERVER_IMAGE)"

# Build both images
docker-images: docker-agent-image docker-server-image

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
	@echo "  make docker-up          - Start all services (MySQL, Redis, agent, server)"
	@echo "  make docker-down        - Stop containers"
	@echo "  make docker-logs        - Follow container logs"
	@echo "  make docker-agent-image - Build agent image (Claude Code + plugin)"
	@echo "  make docker-server-image - Build server image (Go binary)"
	@echo "  make docker-images      - Build both images"
	@echo "  make docker-image       - Build agent image (alias)"
	@echo ""
	@echo "Desktop (Tauri) targets:"
	@echo "  make agent-build-native - Build anban natively (desktop sidecar)"
	@echo "  make plugin-binaries    - Bundle anban into claudecode/codex/openclaw bin/"
