# syntax=docker/dockerfile:1.7
# ==========================================================
# Mnema Talk - multi-stage build
#   1. web:     build the Vue 3 SPA (npm ci, reproducible from package-lock.json)
#   2. builder: compile the static Go binary with the SPA embedded (go:embed)
#   3. runtime: minimal Alpine, non-root, no shell tools beyond busybox
# Database migrations are embedded in the binary (internal/db/migrations).
# ==========================================================

# --- Stage 1: frontend ---
FROM node:26-alpine AS web
WORKDIR /web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY web/ ./
RUN npm run build

# --- Stage 2: Go binary ---
FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/web.go ./web/web.go
COPY --from=web /web/dist ./web/dist

# Release version baked into the binary (cmd/server: var version = "dev").
# The release workflow passes the tag, e.g. --build-arg VERSION=1.2.3.
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-w -s -X main.version=${VERSION}" \
      -o /out/mnema-talk ./cmd/server

# --- Stage 3: runtime ---
FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata

# Least-privilege runtime user. The binary writes nothing to disk except
# multipart upload temp files in /tmp; media lives in S3, data in PostgreSQL.
RUN adduser -D -H -u 10001 app

COPY --from=builder /out/mnema-talk /usr/local/bin/mnema-talk

ENV PORT=8080 \
    BIND_ADDR=0.0.0.0 \
    APP_ENV=production

# HTTP / WebSocket, and the WebRTC media port range (keep in sync with
# WEBRTC_UDP_PORT_MIN/MAX).
EXPOSE 8080
EXPOSE 50000-50050/udp

USER 10001
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/mnema-talk"]
