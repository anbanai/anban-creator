FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/anban ./agent

FROM node:25-slim

RUN apt-get update && apt-get install -y --no-install-recommends tini jq && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /out/anban /usr/local/bin/anban
COPY claudecode/   /anbanai/

USER node
RUN claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anban@anbanai
USER root

WORKDIR /workspace

ENTRYPOINT ["tini", "--"]
CMD ["sleep", "infinity"]
