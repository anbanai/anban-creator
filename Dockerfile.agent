FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/abwriter-agent ./agent

FROM node:25-slim

RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /out/abwriter-agent /usr/local/bin/abwriter-agent
COPY plugin/.claude-plugin/ /anbanai/.claude-plugin/
COPY plugin/.mcp.json       /anbanai/.mcp.json
COPY plugin/agents/         /anbanai/agents/
COPY plugin/skills/         /anbanai/skills/
COPY plugin/hooks/          /anbanai/hooks/
COPY plugin/themes/         /anbanai/themes/
COPY plugin/writers/        /anbanai/writers/

USER node
RUN claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anbanwriter@anbanai
USER root

WORKDIR /workspace
CMD ["sleep", "infinity"]
