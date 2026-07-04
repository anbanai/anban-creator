# Stage 1: Build Go server binary
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /anban-creator-server ./server/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /anban ./agent

# Stage 2: API server runtime plus local agent sandbox.
FROM ghcr.io/openhands/agent-server:1.23.0-python

USER root

# Install Anban agent runtime requirements. The OpenHands base may already
# include ffmpeg/ffprobe; install ffmpeg only when either binary is missing.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl git jq fontconfig fonts-noto-cjk python3 tini; \
    if ! command -v npm >/dev/null 2>&1; then \
      apt-get install -y --no-install-recommends nodejs npm; \
    fi; \
    if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then \
      apt-get install -y --no-install-recommends ffmpeg; \
    fi; \
    if ! id -u node >/dev/null 2>&1; then \
      useradd -m -s /bin/bash node; \
    fi; \
    rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /anban-creator-server /app/anban-creator-server
COPY --from=builder /anban /usr/local/bin/anban

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
ENTRYPOINT []
CMD ["/app/anban-creator-server", "-config", "/app/conf/config.yaml"]
