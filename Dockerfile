# syntax=docker/dockerfile:1.7
# ==========================================================
# Mnema Talk - multi-stage build
#   1. web:     build the Vue 3 SPA (npm ci, reproducible from package-lock.json)
#   2. builder: compile the static Go binary with the SPA embedded (go:embed)
#   3. rootfs:  CA bundle, tzdata and the app user, prepared on the build machine
#   4. runtime: minimal Alpine, non-root, no shell tools beyond busybox
# Database migrations are embedded in the binary (internal/db/migrations).
# Base images are pinned by tag and digest; Dependabot refreshes both weekly.
#
# Multi-arch (docker buildx --platform linux/amd64,linux/arm64): every build
# stage runs on the build machine ($BUILDPLATFORM). The web app is built once,
# the Go binary is cross-compiled per target (pure Go), and the runtime stage
# has no RUN step, so no target-platform emulation (QEMU) is needed.
# ==========================================================

# --- Stage 1: frontend ---
FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:0b36e8c136b94cd4fcf02188228e76c31ad5872eef3fec8cbd2eee500cfd9e80 AS web
WORKDIR /web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY web/ ./
# The web app embeds the same version as the binary (vite define
# __APP_VERSION__): VERSION, else version.txt. The browser compares it with
# the server's to offer a reload after an update.
ARG VERSION=
COPY version.txt /version.txt
RUN MNEMA_VERSION="${VERSION}" npm run build

# --- Stage 2: Go binary ---
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY api/ ./api/
COPY web/web.go ./web/web.go
COPY --from=web /web/dist ./web/dist

# Release version and source revision baked into the binary
# (internal/version). The release workflow passes the release, e.g.
# --build-arg VERSION=1.2.3 --build-arg REVISION=<commit>; without VERSION
# the version from version.txt is used, the same one the web stage embedded.
ARG VERSION=
ARG REVISION=
# Set by buildx per target platform, e.g. linux/arm64 or linux/arm/v7 (GOARM=7).
ARG TARGETOS TARGETARCH TARGETVARIANT
COPY version.txt ./version.txt
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    v="${VERSION:-$(cat version.txt)}" && \
    CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}" GOARM="${TARGETVARIANT#v}" \
    go build -trimpath \
      -ldflags="-w -s -X github.com/msch128/mnema-talk/internal/version.Version=${v} -X github.com/msch128/mnema-talk/internal/version.Revision=${REVISION}" \
      -o /out/mnema-talk ./cmd/server

# --- Stage 3: runtime files, prepared on the build machine ---
# CA bundle, time zone data and /etc/passwd are the same on every
# architecture, so they are assembled here and copied into the target image.
FROM --platform=$BUILDPLATFORM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS rootfs
RUN apk add --no-cache ca-certificates tzdata
# Least-privilege runtime user. The binary writes nothing to disk except
# multipart upload temp files in /tmp; media lives in S3, data in PostgreSQL.
RUN adduser -D -H -u 10001 app

# --- Stage 4: runtime ---
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
COPY --from=rootfs /etc/passwd /etc/group /etc/shadow /etc/
COPY --from=rootfs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=rootfs /usr/share/zoneinfo /usr/share/zoneinfo

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
