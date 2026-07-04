FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/anban ./agent

FROM ghcr.io/openhands/agent-server:1.23.0-python

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

USER node
RUN git config --global http.version HTTP/1.1 && \
    claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anban@anbanai
USER root

WORKDIR /workspace

ENTRYPOINT ["tini", "--"]
CMD ["sleep", "infinity"]
