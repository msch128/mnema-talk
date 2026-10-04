# AGENTS.md — Mnema Talk Project Rules & AI Guidelines

Welcome to **Mnema Talk** (`https://github.com/msch128/mnema-talk.git`).
This repository is **PUBLIC**. All AI coding agents (Antigravity, Copilot, Claude, etc.) and human contributors MUST strictly adhere to the rules outlined below.

---

## 🚨 RULE 1: ZERO SECRET EXPOSURE (CRITICAL)

Because this repository is open-source and publicly visible on GitHub:

1. **NEVER COMMIT REAL SECRETS**:
   - No real passwords, JWT secrets, session keys, private keys, or API tokens.
   - No production domain API keys (e.g. OVH DNS keys, Cloudflare tokens).
   - No internal private IP addresses, LAN hostnames, or personal production emails.
2. **ENVIRONMENT VARIABLES ONLY**:
   - All sensitive settings must be read from environment variables (or `.env` in local development).
   - Always provide safe, dummy placeholder values in `.env.example`.
3. **NEVER COMMIT DATA OR UPLOADS**:
   - SQLite database files (`*.db`, `*.sqlite*`, `data/`) must never be staged.
   - User-uploaded pictures, videos, or attachments (`uploads/`, `media/`) must never be committed.
   - Verify `.gitignore` before every commit.

---

## 🎯 ARCHITECTURE & PHILOSOPHY

Mnema Talk is designed as an **ultra-lightweight, single-server Discord alternative** written in **Go (Golang)**:

### 1. Single-Binary Architecture
- Backend, WebSockets, WebRTC SFU, and static frontend assets are compiled into a **single standalone Go binary**.
- Minimal memory footprint: Target idle RAM usage is **< 60 MB** (unlike 12-container monoliths like Stoat/Revolt).
- Single Docker container deployment.

### 2. Single-Server Model
- There are **no multiple guilds/servers** to discover or federate.
- One unified community space with customizable categories:
  - Text channels (`#general`, `#gaming`, `#tech`).
  - Voice / Hangout channels (`🔊 Voice 1`, `🔊 Gaming Squad`).

### 3. Voice & 4K 60 FPS Screenshare
- Built using **Pion WebRTC** (`github.com/pion/webrtc`) as a pure Go **Selective Forwarding Unit (SFU)**.
- **No server-side video transcoding**: The server routes encrypted UDP RTP packets in-memory directly between peers.
- Supports source-quality screen sharing up to **4K at 60 FPS** with hardware-accelerated encoding (NVENC / QuickSync / AV1 / VP9) on the client.
- **Hangout spots**: Clicking a voice channel immediately connects the user without phone-call ringing.

### 4. Admin & Security Model
- **Sole Administrator**: Account `Herzog` is the sole server administrator.
- **Invite-Only Registration**: Public self-registration is permanently disabled. Only `Herzog` can generate registration invite links or codes.
- **Zero bloat**: Embedded SQLite with WAL mode (`modernc.org/sqlite` or `mattn/go-sqlite3`), no external database services.

---

## 📁 REPOSITORY STRUCTURE

```
mnema-talk/
├── cmd/
│   └── server/          # Main entrypoint (main.go)
├── internal/
│   ├── config/          # Environment configuration loading
│   ├── db/              # SQLite database schema, migrations & queries
│   ├── auth/            # Password hashing (bcrypt/argon2), JWT/sessions, Herzog admin logic
│   ├── chat/            # Text channels, message history, file upload handling
│   ├── ws/              # Real-time WebSocket hub (events, live presence, typing)
│   └── sfu/             # Pion WebRTC SFU (audio/video packet routing, 60fps screenshare)
├── web/                 # Frontend SPA (Vite + React or Svelte/Tailwind)
│   └── dist/            # Compiled static assets embedded into Go via //go:embed
├── .env.example         # Example configuration with safe dummy values
├── .gitignore           # Strict ignore file
├── AGENTS.md            # Guidelines for AI development agents
└── README.md            # Project overview and deployment guide
```

---

## 🛠️ DEVELOPMENT WORKFLOW FOR AGENTS

1. **Check `.env` and `.gitignore` first**: Before modifying or creating files, ensure no secrets or local database files are staged.
2. **Pure Go preferred**: Keep CGO dependencies to a minimum so cross-compilation for Linux (Unraid/Docker) from any OS is trivial.
3. **Structured Commits**: Use descriptive conventional commits:
   - `feat: add webrtc sfu audio track routing`
   - `fix: resolve websocket presence disconnect leak`
   - `docs: update deployment and caddy proxy guide`
