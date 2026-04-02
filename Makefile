# Writer CLI Makefile
# 微信公众号写作工具统一构建

.PHONY: all build clean test install help lint fmt vet release sync server-build server-run server-dev

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

# 默认目标
all: build

# 构建所有平台的二进制文件（发布到 bin/ 目录）
release:
	@echo "🔨 构建 anbanwriter 所有平台版本..."
	@echo ""
	@mkdir -p bin
	@echo "📦 Building for Linux amd64..."
	@GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/anbanwriter-linux-amd64 ./app
	@echo "✓ Linux amd64"
	@echo "📦 Building for Linux arm64..."
	@GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/anbanwriter-linux-arm64 ./app
	@echo "✓ Linux arm64"
	@echo "📦 Building for macOS amd64 (Intel)..."
	@GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/anbanwriter-darwin-amd64 ./app
	@echo "✓ macOS amd64"
	@echo "📦 Building for macOS arm64 (Apple Silicon)..."
	@GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/anbanwriter-darwin-arm64 ./app
	@echo "✓ macOS arm64"
	@echo "📦 Building for Windows amd64..."
	@GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/anbanwriter-windows-amd64.exe ./app
	@echo "✓ Windows amd64"
	@echo ""
	@chmod +x bin/*-linux* bin/*-darwin* 2>/dev/null || true
	@echo "✅ 构建完成！二进制文件在 bin/ 目录"
	@echo ""
	@ls -lh bin/

# 构建当前平台
build:
	@echo "🔨 构建当前平台..."
	@mkdir -p bin
	@go build -ldflags="$(LDFLAGS)" -o bin/anbanwriter ./app
	@echo "✅ 构建完成: bin/anbanwriter"

# 快速构建（仅当前平台，用于开发）
fast:
	@echo "🔨 快速构建当前平台..."
	@mkdir -p bin
	@go build -ldflags="$(LDFLAGS)" -o bin/anbanwriter ./app
	@echo "✅ 构建完成: bin/anbanwriter"

# 清理
clean:
	@echo "🧹 清理..."
	@rm -rf bin/
	@rm -rf dist/ release/
	@rm -f *.log

# 运行测试
test:
	@echo "🧪 运行测试..."
	@go test -v ./...

# 代码检查
lint:
	@echo "🔍 代码检查..."
	@golangci-lint run ./... 2>/dev/null || echo "  (需要安装 golangci-lint)"

# 格式化代码
fmt:
	@echo "🎨 格式化代码..."
	@go fmt ./...
	@gofmt -w .

# 静态分析
vet:
	@echo "🔬 静态分析..."
	@go vet ./...

# 安装到 GOPATH/bin
install:
	@echo "📦 安装到 $(GOPATH)/bin..."
	@go install ./app

# 下载依赖
deps:
	@echo "📥 下载依赖..."
	@go mod download
	@go mod tidy

# 同步 Skill 目录
sync:
	@echo "🔄 同步 Skill 目录..."
	@bash scripts/sync.sh

# 帮助
help:
	@echo "Writer CLI Makefile 命令:"
	@echo ""
	@echo "开发命令:"
	@echo "  make build       - 构建当前平台二进制"
	@echo "  make fast        - 快速构建（开发用）"
	@echo "  make release     - 构建所有平台二进制到 bin/"
	@echo "  make clean       - 清理构建文件"
	@echo ""
	@echo "代码质量:"
	@echo "  make fmt         - 格式化代码"
	@echo "  make vet         - 静态分析"
	@echo "  make test        - 运行测试"
	@echo ""
	@echo "依赖管理:"
	@echo "  make deps        - 下载依赖"
	@echo "  make install     - 安装到 GOPATH/bin"
	@echo ""
	@echo "文档同步:"
	@echo "  make sync        - 同步 Skill 目录到插件目录"
	@echo ""
	@echo "  make server-build - 构建 server 二进制到 bin/anbanwriter-server"
	@echo "  make server-run   - 构建并运行 server"
	@echo "  make server-dev   - go run 运行 server（开发用）"
	@echo ""
	@echo "用户快速安装:"
	@echo "  go install github.com/royalrick/anbanwriter/app/cmd/writer@latest"

# Server 构建
server-build:
	@echo "🔨 构建 server..."
	@mkdir -p bin
	@go build -o bin/anbanwriter-server ./server
	@echo "✅ 构建完成: bin/anbanwriter-server"

server-run: server-build
	@./bin/anbanwriter-server

server-dev:
	@cd server && go run .
