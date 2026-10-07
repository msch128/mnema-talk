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
- Data lives in **PostgreSQL 18**; uploads and avatars in **S3-compatible object storage** (SeaweedFS by default).
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
│   ├── update/              # GitHub release check + client for the optional updater sidecar
│   ├── version/             # Build version/revision (ldflags), shared with the web build
│   ├── server/              # Router wiring + integration tests
│   └── testutil/            # Postgres fixture for integration tests (dockertest / TEST_DATABASE_URL)
├── web/                     # Vue 3 + Pinia + Vite + Tailwind SPA; web.go embeds web/dist
├── scripts/git-hooks/       # pre-commit (gitleaks + gofmt); `make install-hooks`
├── scripts/smoke-image.sh   # Image smoke test on the production compose file (`make smoke`)
├── scripts/backup-drill.sh  # Backup -> restore drill on a throwaway stack (`make backup-drill`)
├── scripts/upgrade-postgres.sh # One-time PostgreSQL 17 -> 18 move (0.4 -> 0.5)
├── scripts/postgres-upgrade-drill.sh # Drill for it (`make postgres-upgrade-drill`)
├── .github/workflows/
│   ├── ci.yml               # CI: gitleaks, Go vet/fmt/tests, govulncheck, frontend, e2e,
│   │                        # docker build + smoke test, backup/restore drill, trivy scan
│   ├── image-scan.yml       # Weekly trivy scan of the released :latest image
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

<!-- REPOWISE_AGENTS:START — Do not edit below this line. Auto-generated by Repowise. -->
## Codebase Intelligence for main (Repowise)

Indexed by [Repowise](https://repowise.dev). Last indexed: 2026-10-05 (commit aa34060). Confidence: 98%.Scope: standard index · model content · full Git · 85 eligible file pages omitted. Missing or unavailable evidence is not a negative finding. Machine-readable scope: `{"analysis": {"skipped": [], "unavailable": ["Test run: limiting to 10 files"]}, "content_provenance": "model", "dropped_files": {"generated": {"count": 0, "paths": [], "truncated": false}}, "file_pages": {"configured_cap": null, "effective_cap": null, "eligible": 95, "generated": 10, "omitted": 85}, "git_commit_cap": 1000, "git_history_coverage": {"complete_through_depth": 0, "deep_commits": 0, "deep_files": 0, "eligible_files": 143, "fallback_files": 143, "files_with_history": 143, "global_commits": 0, "per_file_limit": 1000, "recent_files": 0, "retained_commits": 412, "unavailable_files": 0, "workers": 8}, "git_tier": "full", "provider": {"embedder": "mock", "model": "claude_cli/claude-haiku-4-5", "model_cost_possible": true, "name": "claude_cli", "reused": false}, "run_mode": "standard", "search": {"full_text": "available", "next_command": "repowise reindex", "semantic": "unavailable"}, "upgrade": {"completed_stages": [], "next_stage": null, "retryable": false, "status": "not_applicable"}, "version": 1}`

### How to work in this repo

- **Trust the index.** `verified: true` and `_meta.complete` mean the bytes were checked against the live tree, so never re-read them. Re-read only what `bounds: "approximate"` or `_meta.stale_warning` names. `confidence` rates the prose, not the evidence: on `low` read the `fallback_targets` or `best_guesses` the reply names, and run `repowise update` and ask again if `_meta.hint` says the index is behind HEAD. `index_behind: true` alone is informational.
- **A zero carries its basis.** An empty `callers`/`callees`/`used_by` comes with a `*_basis` saying how much of that language's calls the graph resolved, so read it before concluding nothing calls a symbol. `_meta.scope_hint` names the areas the answer did not touch.
- **Pre-edit, not instead-of-edit.** These tools decide *which* files to read and edit. Reading a file before you edit it is correct and expected.
- **Noisy commands** (tests, builds, `git log`/`diff`, searches, listings): prefer `repowise distill <cmd>`, the same command with its exit code preserved and errors-first output. A `[repowise#<ref>: N lines omitted]` marker is recoverable via `repowise expand <ref>` (add `-q <regex>` to filter); never re-run the command to see omitted output.
- **Recording a decision** you had to reason out: `repowise decision add --title T --decision D` records it without prompting and prints the id (`--format json` to parse it back). It lands `proposed`, for a person to confirm.

### Tools

| Tool | When and why |
|------|--------------|
| `get_answer(question)` | First call for any how/where/why question. Cite `confidence: "high"` or `grounding: "extracted"` directly; `degraded` means judge by `retrieval_quality`. `symbol_bodies` has live bodies. |
| `get_context(targets=[...])` | Triage card for files/modules/symbols: docs, signatures, hotspot, fix history. No source bytes — `include=["skeleton"]` for the whole file verified, `["callers"|"decisions"]` for depth. Batch targets. |
| `get_symbol(id, depth?)` | **Follow-up, not an entry point** — one verified body for an id a prior response named (`path.py::Name`, `path.py:140-180`, `repowise#<hex>`). Never walk a file symbol by symbol; Read it. |
| `search_codebase(query)` | Hybrid search, auto-routed by query shape; force with `mode=symbol|path|concept|hybrid`. A hit whose `sources` are `[fts]` only has no semantic agreement, so verify it. |
| `get_why(query, targets?)` | Why the code is shaped this way: decision records, git archaeology, rationale comments. Call before a refactor or a pattern divergence. |
| `get_risk(targets, changed_files?, include?)` | File history and structural reach. PR mode leads with `directive`; its 0-10 structural heuristic is uncalibrated, not a probability. Read typed test recommendations and coverage state first. |
| `get_change_risk(revspec?, extensions?, exclude_patterns?)` | Deterministic live-diff review signal for a commit or range. Lead with benchmarked percentile/classification; the 0-10 diff-shape score is supporting, not a probability. `get_risk` scores paths. |
| `get_health(targets?, include?)` | Defect / maintainability / performance scores and findings, plus documentation the code no longer supports. Self-check the files you touched before finishing. |
| `get_dead_code(tier?, min_confidence?, safe_only?)` | Confidence-tiered unreachable files / unused exports / zombie packages. For cleanup sweeps, not targeted fixes. |
| `get_overview()` | Architecture map. Call once, first, in an unfamiliar repo; skip it after that. |

### Architecture
Mnema Talk: Discord alternative server -> consumes real-time text messages, voice connections, screen shares via WebSocket & WebRTC -> routes through centralized hub, forwards RTC streams (no transcode) -> outputs instant-join voice hangouts, threaded chat, high-quality screen shares served as single Go binary. Backend layer: Go server, auth (sessions, invite codes), WebSocket hub (message routing, presence), Pion SFU (RTC forwarding), Postgres (state), S3 store (blobs). Frontend layer: Vue SPA, state stores (chat, voice, auth, presence), API client, i18n, WebRTC client. Both layers ship in one Docker compose stack.

### Key modules
- `root` — internal/config/config.go::Config aggregates all runtime settings into a single struct: server binding parameters (port, address), database…
- `web` — Language persists on account (auth store applies post-login); browser decides first visit only
- `internal` — internal/config Load(): parses DATABASE_URL, JWT_SECRET, S3, WebRTC STUN/TURN, session TTL, admin credentials
- `web/src/lib` — Implements client API transport, chat domain rules (mentions, notifications, threading), message pagination & display, markdown rendering…
- `web/src/components` — BaseDialog.vue → foundation shell for all modals
ChatArea.vue → **hotspot: 17 commits/90d, 22 bug fixes**
State persists edits, reactions…
- `internal/httpx` — RequestID middleware -> injects UUID or validates X-Request-Id header, stores in context
- `internal/server` — internal/ws/hub.go — active WebSocket connections & broadcasted state
- `internal/auth` — Validates user identity via JWT tokens & cookie sessions, authorizes channel access by type, enforces admin privileges, manages invitation…
- `web/src/stores` — Maintenance: Architecturally central (high PageRank)
- `internal/config` — internal/config/config.go hotspot: 9 commits/90 days, 7 bug-fix commits

### Entry points
- `web/src/main.js`
- `web/src/App.vue`
- `internal/server/server.go`
- `cmd/server/main.go`

### Files that need care (bug-fix history first, then churn — check `get_risk` before editing)
- `web/src/composables/useWebRTC.js` — 8 bug fixes, last fix today (bug magnet); 19 commits/90d
- `web/src/stores/chat.js` — 5 bug fixes, last fix today (bug magnet); 15 commits/90d
- `internal/server/server.go` — 4 bug fixes, last fix today (bug magnet); 9 commits/90d
- `internal/config/config.go` — 4 bug fixes, last fix today (bug magnet); 9 commits/90d
- `internal/chat/messages.go` — 4 bug fixes, last fix today (bug magnet); 9 commits/90d

### Code health
Three co-equal signals: code health 7.52/10 avg (Good), hotspot health 5.86/10 (declining), worst `web/src/components/ChatArea.vue` at 1.75/10 · maintainability 8.15/10 · performance risk 17 open static performance findings. Detail: `get_health()`.

Critical files:
- `web/src/components/ChatArea.vue` — complex conditional (handleMessageKeydown) — impact −2.1
- `web/src/components/MemberList.vue` — churn risk — impact −1.7
- `web/src/App.vue` — complex method (applyRoute) — impact −1.3
- `web/src/components/LegalModal.vue` — churn risk — impact −1.3
- `web/src/stores/voice.js` — large method (defineStore callback) — impact −1.1

### Commands
- Build: `make build`
- Test: `make test`
- Lint: `make lint`
- Dev: `make dev`
- Format: `make fmt`

<!-- REPOWISE_AGENTS:END -->
