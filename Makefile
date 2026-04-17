# AnbanWriter Makefile
# Content creation platform: MCP Server + Web Studio

.PHONY: all clean test help lint fmt vet \
        server-build server-run server-dev server-test \
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
	@rm -rf bin/ studio/dist/ studio/node_modules/
	@rm -rf dist/ release/
	@rm -f *.log

# Run all tests
test:
	@go test -v ./...

# Code linting (requires golangci-lint)
lint:
	@golangci-lint run ./...

# Format code
fmt:
	@go fmt ./...
	@gofmt -w .

# Static analysis
vet:
	@go vet ./...

# Download and tidy dependencies
deps:
	@go mod download
	@go mod tidy

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
	@mkdir -p bin
	@go build -o bin/abwriter-server ./server
	@echo "Server build complete: bin/abwriter-server"

# Build and run the server
server-run: server-build
	./bin/abwriter-server -config server/config.yaml

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

# Start infrastructure services (MySQL, Redis)
docker-up:
	@docker-compose up -d

# Stop infrastructure services
docker-down:
	@docker-compose down

# Follow infrastructure logs
docker-logs:
	@docker-compose logs -f

# Build the abwriter-agent Docker image (required for executor: docker)
docker-agent-image:
	@echo "Building abwriter-agent:latest..."
	@docker build -f agent/Dockerfile -t abwriter-agent:latest .
	@echo "Image build complete: abwriter-agent:latest"

# Build the abwriter-server Docker image
docker-server-image:
	@echo "Building abwriter-server:latest..."
	@docker build -f server/Dockerfile -t abwriter-server:latest .
	@echo "Image build complete: abwriter-server:latest"

# Build both images
docker-images: docker-agent-image docker-server-image

# Backward-compatible alias (builds agent image)
docker-image: docker-agent-image

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

help:
	@echo "AnbanWriter - Content Creation Platform"
	@echo ""
	@echo "Shared targets:"
	@echo "  make test          - Run all tests"
	@echo "  make coverage      - Run tests with coverage report"
	@echo "  make ci            - Run all CI checks (fmt + vet + test + lint)"
	@echo "  make vet           - Static analysis"
	@echo "  make fmt           - Format code"
	@echo "  make lint          - Lint code (requires golangci-lint)"
	@echo "  make deps          - Download and tidy dependencies"
	@echo "  make clean         - Remove all build artifacts"
	@echo ""
	@echo "Server targets:"
	@echo "  make server-build  - Build server binary to bin/abwriter-server"
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
	@echo "  make docker-up          - Start MySQL, Redis and agent containers"
	@echo "  make docker-down        - Stop containers"
	@echo "  make docker-logs        - Follow container logs"
	@echo "  make docker-agent-image - Build agent image (Claude Code + plugin)"
	@echo "  make docker-server-image - Build server image (Go binary)"
	@echo "  make docker-images      - Build both images"
	@echo "  make docker-image       - Build agent image (alias)"
