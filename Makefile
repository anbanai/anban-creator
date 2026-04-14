# Writer CLI Makefile
# WeChat writing tool unified build

.PHONY: all build clean test install help lint fmt vet release sync ci coverage \
        server-build server-run server-dev server-test \
        web-install web-dev web-build \
        docker-up docker-down docker-logs

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

# Default target
all: build

# ---------------------------------------------------------------------------
# CLI targets
# ---------------------------------------------------------------------------

# Build for all platforms (release)
release:
	@mkdir -p bin
	@echo "Building for Linux amd64..."
	@GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/abwriter-linux-amd64 ./app
	@echo "Building for Linux arm64..."
	@GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/abwriter-linux-arm64 ./app
	@echo "Building for macOS amd64 (Intel)..."
	@GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/abwriter-darwin-amd64 ./app
	@echo "Building for macOS arm64 (Apple Silicon)..."
	@GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/abwriter-darwin-arm64 ./app
	@echo "Building for Windows amd64..."
	@GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/abwriter-windows-amd64.exe ./app
	@chmod +x bin/*-linux* bin/*-darwin* 2>/dev/null || true
	@echo "Release builds complete in bin/"

# Build for current platform
build:
	@mkdir -p bin
	@go build -ldflags="$(LDFLAGS)" -o bin/abwriter ./app
	@echo "Build complete: bin/abwriter"

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

# Install to GOPATH/bin
install:
	@go install ./app

# Download and tidy dependencies
deps:
	@go mod download
	@go mod tidy

# Sync Skill directories
sync:
	@bash scripts/sync.sh

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
	@./bin/abwriter-server -config server/config.yaml

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
	@cd studio && npm install

# Run frontend dev server
web-dev:
	@cd studio && npm run dev

# Build frontend for production
web-build:
	@cd studio && npm run build

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

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

help:
	@echo "Writer CLI + Online Service Makefile"
	@echo ""
	@echo "CLI targets:"
	@echo "  make build         - Build current platform CLI binary"
	@echo "  make release       - Build all platform binaries to bin/"
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
	@echo "  make web-install   - Install frontend dependencies"
	@echo "  make web-dev       - Run frontend dev server"
	@echo "  make web-build     - Build frontend for production"
	@echo ""
	@echo "Docker targets:"
	@echo "  make docker-up     - Start MySQL and Redis containers"
	@echo "  make docker-down   - Stop containers"
	@echo "  make docker-logs   - Follow container logs"
	@echo ""
	@echo "Quick install:"
	@echo "  go install github.com/royalrick/anbanwriter/app/cmd/writer@latest"
