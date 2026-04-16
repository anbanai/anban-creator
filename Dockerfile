# Stage 1: Build Go server binary
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Cache dependency downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /abwriter-server ./server/

# Stage 2: Runtime image with Claude Code
FROM node:25-slim

# Install Claude Code CLI.
RUN npm install -g @anthropic-ai/claude-code

# Install the abwriter server binary.
COPY --from=builder /abwriter-server /app/abwriter-server

# Install the abwriter plugin (agents, skills, hooks, manifest, themes, writers).
COPY plugin/.claude-plugin/ /anbanai/.claude-plugin/
COPY plugin/.mcp.json       /anbanai/.mcp.json
COPY plugin/agents/         /anbanai/agents/
COPY plugin/skills/         /anbanai/skills/
COPY plugin/hooks/          /anbanai/hooks/
COPY plugin/themes/         /anbanai/themes/
COPY plugin/writers/        /anbanai/writers/

# Install plugin as node user so registration lands in /home/node/.claude/
USER node
RUN claude plugin marketplace add /anbanai && \
    claude plugin install --scope user anbanwriter@anbanai
USER root

WORKDIR /app
CMD ["/app/abwriter-server"]
