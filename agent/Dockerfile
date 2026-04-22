FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/abwriter-agent ./agent

FROM node:25-slim

RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /out/abwriter-agent /usr/local/bin/abwriter-agent
COPY plugins/claude-code/.claude-plugin/ /anbanai/.claude-plugin/
COPY plugins/claude-code/.mcp.json       /anbanai/.mcp.json
COPY plugins/claude-code/agents/         /anbanai/agents/
COPY plugins/claude-code/skills/         /anbanai/skills/
COPY plugins/claude-code/hooks/          /anbanai/hooks/
COPY plugins/shared/themes/              /anbanai/themes/
COPY plugins/shared/writers/             /anbanai/writers/

USER node
RUN claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anbanwriter@anbanai
USER root

WORKDIR /workspace
CMD ["sleep", "infinity"]
