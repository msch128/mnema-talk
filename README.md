# Mnema Talk 🎙️⚡

An ultra-lightweight, private, single-server Discord alternative written in **Go (Golang)** with real-time text chat, instant voice hangouts, and source-quality **4K 60 FPS screen sharing**.

---

## 🌟 Why Mnema Talk?

Existing self-hosted alternatives often fall into one of two extremes:
1. **Bloated Monoliths**: Heavy multi-container stacks (like Stoat/Revolt with 10+ microservices, MongoDB, Redis, RabbitMQ, and MinIO) consuming gigabytes of RAM.
2. **Chat-First Protocols**: Systems like Matrix/Element that lack native, instant-join voice channels and rely on clunky Jitsi iframe widgets or complex setups.

**Mnema Talk** solves this with a **Single-Binary Architecture**:
- 🚀 **Ultra-lightweight**: Target memory usage is **< 60 MB RAM** on idle.
- 🔊 **Discord-Style Hangouts**: Click a voice channel to connect immediately with microphone and live presence. No phone-call ringing.
- 🖥️ **4K 60 FPS Screen Share**: Powered by a pure Go WebRTC SFU (Pion) with zero server-side video transcoding overhead.
- 🔒 **Invite-Only & Private**: Public registration is disabled; only the administrator (`Herzog`) can generate invite links.
- 🗄️ **Zero External Services**: Embedded SQLite with WAL mode and local file storage. No external database or message broker required.

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                 Mnema Talk (Single Go Binary)               │
│                                                             │
│  [ Embedded Frontend (Vite/React via //go:embed)          ] │
│  [ REST API (Auth, Channel Management, Media Uploads)      ] │
│  [ WebSockets (Live Chat, Presence, Speaking Indicators)  ] │
│  [ Pion WebRTC SFU (Voice Routing, 4K 60FPS Screenshare)  ] │
│  [ Embedded SQLite (WAL Mode Engine)                      ] │
│  [ Local File Storage (Media / Uploads)                   ] │
└─────────────────────────────────────────────────────────────┘
                              │
                    Caddy Reverse Proxy
                 (e.g., https://chat.example.com)
```

---

## 🚀 Quickstart

### Prerequisites
- [Go 1.22+](https://golang.org)
- [Node.js 20+](https://nodejs.org) (for building the frontend)
- Docker (optional for containerized deployment)

### 1. Configuration
Copy `.env.example` to `.env` and adjust the settings:
```bash
cp .env.example .env
```

### 2. Development Setup
```bash
# Build frontend
cd web && npm install && npm run build && cd ..

# Run backend
go run cmd/server/main.go
```

---

## 🔒 Security & AI Development Guidelines

For AI coding assistants and contributors:
- Please read [AGENTS.md](AGENTS.md) before making changes.
- **Strict Rule**: Never commit secrets, passwords, production tokens, or private IP addresses.

---

## 📄 License

GNU AGPL-3.0, Copyright (C) 2026 Marius Schröder (msch128). See `LICENSE` for details.
