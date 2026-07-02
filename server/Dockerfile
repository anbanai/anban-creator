# Stage 1: Build Go server binary
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /abwriter-server ./server/

# Stage 2: Runtime with Node.js + Claude CLI + Plugin
FROM node:25-slim

# Install CA certificates for Go binary TLS verification (slim images lack these)
# jq is required by claudecode/hooks/seednote-quality-gate.sh — under the default
# `executor: local` config the Claude Code CLI subprocess runs in this container,
# so the SubagentStop hook fires here and needs jq to parse stdin JSON.
# git is required by `skills add` to install GitHub-hosted video overlay skills.
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates jq git && \
    rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /abwriter-server /app/abwriter-server

# COPY server/config.yaml /app/conf/config.yaml

# Copy plugin files (claudecode/ contains agents/, skills/, hooks/, writers/)
COPY claudecode/ /anbanai/

ENV CLAUDE_PLUGIN_ROOT=/anbanai

# Install plugin as non-root user (Claude CLI requires this for --scope user)
USER node
RUN git config --global http.version HTTP/1.1

RUN npx -y skills@1.5.14 add heygen-com/hyperframes \
        --agent claude-code \
        --skill music-to-video \
        --skill slideshow \
        --copy \
        -g \
        -y && \
    npx -y skills@1.5.14 add remotion-dev/skills \
        --agent claude-code \
        --skill remotion-best-practices \
        --copy \
        -g \
        -y

RUN claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anban@anbanai

# Ensure data directories are writable by node user.
# Switch back to root only to create/chown directories, then drop to node.
USER root
RUN mkdir -p /app/data/files /app/data/workspace /app/data/logs/tasks && \
    chown -R node:node /app/data

USER node

WORKDIR /app
CMD ["/app/abwriter-server", "-config", "/app/conf/config.yaml"]
