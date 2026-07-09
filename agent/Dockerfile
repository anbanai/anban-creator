FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/anban ./agent

FROM ghcr.io/openhands/agent-server:1.23.0-python

USER root

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

COPY --from=builder /out/anban /usr/local/bin/anban
COPY claudecode/   /anbanai/
COPY third_party/OpenMontage/ /app/third_party/OpenMontage/

ENV CLAUDE_PLUGIN_ROOT=/anbanai
ENV ANBAN_MONTAGE_SUBMODULE_PATH=/app/third_party/OpenMontage

USER node
RUN git config --global http.version HTTP/1.1

RUN npx -y skills@latest add heygen-com/hyperframes \
        --agent claude-code \
        --skill music-to-video \
        --skill slideshow \
        --copy \
        -g \
        -y && \
    npx -y skills@latest add remotion-dev/skills \
        --agent claude-code \
        --skill remotion-best-practices \
        --copy \
        -g \
        -y

RUN claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anban@anbanai
USER root

WORKDIR /workspace

ENTRYPOINT ["tini", "--"]
CMD ["sleep", "infinity"]
