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
   - Local database files, dumps and volumes (`*.db`, `*.sqlite*`, `data/`) must never be staged.
   - User-uploaded pictures, videos, or attachments (`uploads/`, `media/`) must never be committed.
   - Verify `.gitignore` before every commit.

---

## 🎯 ARCHITECTURE & PHILOSOPHY

Mnema Talk is a **lightweight, single-server Discord alternative** written in **Go**:

### 1. Single-Binary Architecture
- Backend (REST API), WebSocket hub, WebRTC SFU and the built web app (`web/dist`, via `//go:embed`) are compiled into **one standalone Go binary**.
- Data lives in **PostgreSQL 17**; uploads and avatars in **S3-compatible object storage** (SeaweedFS by default).
- Deployment: `docker compose` with three containers (`app`, `postgres`, `seaweedfs`). Keep the app lean (low idle RAM, no extra services).

### 2. Single-Server Model
- There are **no multiple guilds/servers** to discover or federate.
- One community space with categories, text channels and voice/hangout channels (no direct messages).

### 3. Voice & Screenshare
- **Pion WebRTC** (`github.com/pion/webrtc/v4`) as a pure Go **Selective Forwarding Unit (SFU)**.
- **No server-side transcoding**: the SFU forwards RTP packets in memory. Transport is DTLS-SRTP client-to-SFU, which is **not** end-to-end encrypted.
- Source-quality screen sharing (up to 4K at 60 FPS) with hardware encoding on the client.
- **Hangout spots**: clicking a voice channel connects immediately, without ringing.

### 4. Admin & Security Model
- **Single administrator**: username configurable via `ADMIN_USERNAME`, default **`Herzog`**.
- **Invite-only registration**: public self-registration is permanently disabled. Only the admin can create invite codes.
- Security model summary: see `SECURITY.md`. Don't weaken it (cookie flags, CSRF origin check, CSP, upload sniffing, rate limits) without an explicit decision.
- **Media retention must stay default-off**: `MEDIA_RETENTION_DAYS=0` means nothing is deleted automatically. Never change that default.

---

## 📁 REPOSITORY STRUCTURE

```
mnema-talk/
├── cmd/server/              # Main entrypoint (main.go)
├── internal/
│   ├── config/              # Environment configuration loading + validation
│   ├── db/                  # pgx pool, embedded versioned migrations (db/migrations/*.sql)
│   ├── httpx/               # Middleware: security headers, CSRF origin check, rate limits, client IP
│   ├── auth/                # Password hashing, sessions (token_version), invites, admin logic
│   ├── chat/                # Channels, categories, messages, replies, members
│   ├── media/               # Uploads (MIME sniffing), authenticated media serving, retention
│   ├── s3/                  # S3 client
│   ├── events/              # Event types / publishing to the hub
│   ├── ws/                  # Real-time WebSocket hub (events, presence)
│   ├── sfu/                 # Pion WebRTC SFU (audio/video/screenshare forwarding)
│   ├── server/              # Router wiring + integration tests
│   └── testutil/            # Postgres fixture for integration tests (dockertest / TEST_DATABASE_URL)
├── web/                     # Vue 3 + Pinia + Vite + Tailwind SPA; web.go embeds web/dist
├── scripts/git-hooks/       # pre-commit (gitleaks + gofmt); `make install-hooks`
├── .github/workflows/
│   ├── ci.yml               # CI: gitleaks, Go vet/fmt/tests, govulncheck, frontend, docker build
│   └── release.yml          # CD after green CI on main: release-please + GHCR image push
├── release-please-config.json, .release-please-manifest.json, version.txt, CHANGELOG.md
│                            # Managed by release-please; don't edit version/changelog by hand
├── Dockerfile               # Multi-stage build, non-root runtime
├── docker-compose.yml       # app + postgres + seaweedfs (production)
├── docker-compose.dev.yml   # Dev override: publishes postgres/seaweedfs on 127.0.0.1
├── Makefile                 # `make help` lists all targets
├── .env.example             # Every config variable, placeholder values only
├── .gitleaks.toml           # Secret-scan allowlist (placeholders, test-only values)
├── SECURITY.md              # Vulnerability reporting + security model
├── THIRD_PARTY_NOTICES.md   # Open-source software we use and ship, with licenses
├── AGENTS.md                # This file
└── README.md                # Overview, quickstart, deployment
```

---

## 🛠️ DEVELOPMENT WORKFLOW FOR AGENTS

1. **Check `.env` and `.gitignore` first**: before modifying or creating files, make sure no secrets, databases or uploads are staged.
2. **Run `make check` before committing**: gofmt, `go vet`, unit + integration tests, govulncheck, frontend lint/test/build, `npm audit`, and the Docker build. That's the same set CI runs.
3. **Tests**: `go test ./...` (unit), `go test -tags=integration ./...` (integration; needs Docker or `TEST_DATABASE_URL`), `npm run test` in `web/`. New behaviour comes with tests.
4. **Migrations**: add a **new numbered file** in `internal/db/migrations/` (e.g. `0003_<name>.sql`). **Never edit a migration that has already been applied**; they are embedded and run in order at startup.
5. **Configuration**: every new env variable is read in `internal/config`, documented in `.env.example` with a placeholder, and passed through in `docker-compose.yml` when needed.
6. **Pure Go preferred**: keep CGO out of the build (`CGO_ENABLED=0`) so cross-compilation for Linux (Unraid/Docker) stays trivial.
7. **Dependencies**: pin GitHub Actions to full commit SHAs; commit `go.sum` / `package-lock.json`; don't run `go mod tidy` in the Docker build.
   New or removed shipped dependencies (Go modules in the binary, web `dependencies`, vendored code in `web/src/third_party/`) go into `THIRD_PARTY_NOTICES.md` and the in-app list `web/src/lib/thirdParty.js`.
8. **Never rewrite `main`**: no force push, no history rewrite, no moving or deleting release tags (a ruleset blocks it). release-please finds the last release by its tag commit in `main`'s history; once that commit is gone it re-releases old versions. Rebase your own unpushed commits onto `origin/main` instead.
9. **Structured commits**: release-please derives versions and the changelog from them (`feat:` minor, `fix:` patch, `!`/`BREAKING CHANGE:` major). Use descriptive conventional commits:
   - `feat: add webrtc sfu audio track routing`
   - `fix: resolve websocket presence disconnect leak`
   - `docs: update deployment and caddy proxy guide`
