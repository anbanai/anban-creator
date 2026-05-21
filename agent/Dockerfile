FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/abwriter-agent ./agent

FROM node:25-slim

RUN apt-get update && apt-get install -y --no-install-recommends tini && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /out/abwriter-agent /usr/local/bin/abwriter-agent
COPY claudecode/   /anbanai/

USER node
RUN claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anbanwriter@anbanai
USER root

WORKDIR /workspace

ENTRYPOINT ["tini", "--"]
CMD ["sleep", "infinity"]
