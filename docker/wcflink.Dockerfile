# docker/wcflink.Dockerfile
#
# Multi-stage build for the wcfLink WeChat-bot sidecar (github.com/lich0821/wcfLink).
# The source is fetched from GitHub at build time and pinned by WCFLINK_REF.
# wcfLink uses modernc.org/sqlite (pure Go), so CGO is disabled and the final
# image is a static binary on alpine.
# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder
ARG WCFLINK_REPO=https://github.com/lich0821/wcfLink.git
ARG WCFLINK_REF=refs/tags/v0.1.0
ARG WCFLINK_COMMIT=fb0999b81043c91e8fddb780eb2ecf03f1f8588f
WORKDIR /src

RUN apk add --no-cache ca-certificates git
RUN git init . \
    && git remote add origin "$WCFLINK_REPO" \
    && git fetch --depth 1 origin "$WCFLINK_REF" \
    && git checkout --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "$WCFLINK_COMMIT"

RUN go mod download

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
