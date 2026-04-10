# Stage 1: Build Go binaries
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /anbanwriter-server ./server/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /anbanwriter ./app/

# Stage 2: Runtime image with Claude Code
FROM node:22-slim

# Install Claude Code CLI.
RUN npm install -g @anthropic-ai/claude-code

# Install the anbanwriter server binary.
COPY --from=builder /anbanwriter-server /app/anbanwriter-server

# Install the anbanwriter CLI binary (used by agents via Bash tool).
COPY --from=builder /anbanwriter /app/anbanwriter

# Install the anbanwriter plugin (agents, skills, hooks).
COPY claudecode/ /app/claudecode/
COPY themes/ /app/themes/
COPY writers/ /app/writers/

# Set environment so Claude Code discovers the plugin and binary.
ENV CLAUDE_PLUGIN_ROOT=/app
ENV PATH="/app:${PATH}"

WORKDIR /app
ENTRYPOINT ["/app/anbanwriter-server"]
