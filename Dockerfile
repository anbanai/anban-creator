# Stage 1: Build Go binaries
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /abwriter-server ./server/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /abwriter ./app/

# Stage 2: Runtime image with Claude Code
FROM node:25-slim

# Install Claude Code CLI.
RUN npm install -g @anthropic-ai/claude-code

# Install the abwriter server binary.
COPY --from=builder /abwriter-server /app/abwriter-server

# Install the abwriter CLI binary (used by agents via Bash tool).
COPY --from=builder /abwriter /app/abwriter

# Install the abwriter plugin (agents, skills, hooks, manifest, themes, writers).
# NOTE: plugin/.mcp.json is intentionally excluded — Docker containers receive
# their MCP config from the executor via .claude/.mcp.json in the workspace.
COPY plugin/.claude-plugin/ /app/.claude-plugin/
COPY plugin/agents/         /app/agents/
COPY plugin/skills/         /app/skills/
COPY plugin/hooks/          /app/hooks/
COPY plugin/themes/         /app/themes/
COPY plugin/writers/        /app/writers/

# Set environment so Claude Code discovers the plugin and binary.
ENV CLAUDE_PLUGIN_ROOT=/app
ENV PATH="/app:${PATH}"

WORKDIR /app
CMD ["/app/abwriter-server"]
