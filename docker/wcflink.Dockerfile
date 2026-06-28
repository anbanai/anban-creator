# docker/wcflink.Dockerfile
#
# Multi-stage build for the wcfLink WeChat-bot sidecar (github.com/lich0821/wcfLink).
# Built from the wcflink/ git submodule. wcfLink uses modernc.org/sqlite (pure Go),
# so CGO is disabled and the final image is a static binary on alpine.
#
# Build context is the repo root (context: .) so the submodule source is in scope
# — this mirrors how server/Dockerfile is built. See docker-compose.yml -> wcflink.
# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache module deps first for faster rebuilds.
COPY wcflink/go.mod wcflink/go.sum ./
RUN go mod download

# Copy the submodule source and compile the server binary.
COPY wcflink/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wcfLink ./cmd/wcfLink

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget tzdata
WORKDIR /app
COPY --from=builder /out/wcfLink /app/wcfLink

# Defaults; overridable via compose env. State dir is a bind-mounted volume so
# WeChat sessions survive container restarts.
ENV WCFLINK_LISTEN_ADDR=:18070 \
    WCFLINK_STATE_DIR=/app/state \
    WCFLINK_DB_PATH=/app/state/wcf.db \
    WCFLINK_LOG_LEVEL=info

EXPOSE 18070
VOLUME ["/app/state"]
ENTRYPOINT ["/app/wcfLink"]
