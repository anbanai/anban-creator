# syntax=docker/dockerfile:1.7
#
# Multi-stage build for the Studio frontend.
#
# dist/ is gitignored, so the production bundle must be built inside the image
# (no dependency on a host-built dist). Stage 1 compiles with Bun (tsc typecheck
# + vite build -> dist/); stage 2 serves the static output with nginx and
# reverse-proxies /api + /ws to the backend compose service.

# Stage 1: build the production bundle.
FROM oven/bun:1.3 AS build
WORKDIR /app

# Cache dependency install. --frozen-lockfile makes the build reproducible and
# fails fast if bun.lock drifts from package.json, rather than silently resolving
# a different tree. Keep bun.lock committed and in sync with package.json.
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# Copy the rest of the source and build.
COPY . .
RUN bun run build

# Stage 2: serve the static bundle with nginx.
FROM nginx:1.24-alpine

# Docker/local defaults preserve the existing HTTP backend contract. ACK
# overrides all three values to reach the TLS-enabled Server Service.
ENV BACKEND_HOST=server
ENV BACKEND_SCHEME=http
ENV BACKEND_PORT=8080

# nginx config reverse-proxies /api (incl. SSE) and /ws to the backend.
COPY default.conf.template /etc/nginx/templates/default.conf.template

COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
