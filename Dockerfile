# ==========================================================
# Mnema Talk - Multi-Stage Dockerfile
# Stage 1: Build Vue 3 Frontend
# Stage 2: Build Pure Go SFU + API Server
# Stage 3: Minimal Production Alpine Runtime
# ==========================================================

# --- Stage 1: Frontend Build ---
FROM node:22-alpine AS frontend-builder
WORKDIR /build

COPY web/package.json web/package-lock.json* ./
RUN npm install

COPY web/ ./
RUN npm run build

# --- Stage 2: Go Backend Build ---
FROM golang:1.23-alpine AS backend-builder
WORKDIR /build

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
# Copy compiled frontend assets into web/dist for embed.FS
COPY --from=frontend-builder /build/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /mnema-talk ./cmd/server

# --- Stage 3: Runtime ---
FROM alpine:3.21 AS runner

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

COPY --from=backend-builder /mnema-talk /usr/local/bin/mnema-talk
COPY --from=backend-builder /build/migrations ./migrations

# Expose HTTP / WS / WebRTC ports
EXPOSE 8080
EXPOSE 50000-50050/udp

ENV PORT=8080 \
    APP_ENV=production \
    BIND_ADDR=0.0.0.0

ENTRYPOINT ["/usr/local/bin/mnema-talk"]
