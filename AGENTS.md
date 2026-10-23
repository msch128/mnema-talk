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

Mnema Talk is a **lightweight, self-hosted chat and voice app for one community**, written in **Go**. Use the product vocabulary in [`doc/glossary.md`](doc/glossary.md) (community, member, Talk, Owner, timeout, ...) in UI strings, docs and error messages; German always uses "du":

### 1. Single-Binary Architecture
- Backend (REST API), WebSocket hub, WebRTC SFU and the built web app (`web/dist`, via `//go:embed`) are compiled into **one standalone Go binary**.
- Data lives in **PostgreSQL 18**; uploads and avatars in **S3-compatible object storage** (SeaweedFS by default).
- Deployment: `docker compose` with three always-on containers (`app`, `postgres`, `seaweedfs`) plus two opt-in profiles: `coturn` (TURN relay, profile `turn`) and the `updater` self-update sidecar (profile `autoupdate`). Keep the app lean (low idle RAM, no extra services).

### 2. Single-Server Model
- There are **no multiple guilds/servers** to discover or federate.
- One community space with categories, text channels and Talks (voice rooms; direct messages are separate conversations, see `doc/architecture.md`).

### 3. Voice & Screenshare
- **Pion WebRTC** (`github.com/pion/webrtc/v4`) as a pure Go **Selective Forwarding Unit (SFU)**.
- **No server-side transcoding**: the SFU forwards RTP packets in memory. Transport is DTLS-SRTP client-to-SFU. Microphone, camera, screen video and screen-audio payloads are encrypted in the browser with MLS-derived keys, so an honest SFU cannot read them; an active or compromised server (it serves the code and asserts membership) and metadata are out of scope. Text messages and attachments stay readable by the server.
- Source-quality screen sharing (up to 4K at 60 FPS) with hardware encoding on the client.
- **Hangout spots**: clicking a Talk connects immediately, without ringing.

### 4. Admin & Security Model
- **One Owner**: the account named by `ADMIN_USERNAME` (default **`Herzog`**; `users.role = 'admin'` in the database and API, "Owner" in the UI). The Owner has every permission without exception and **nobody** can edit, restrict, demote, disable, kick, rename or overwrite the Owner, or change their roles, not even a role with *Administrator*. Ownership is not transferable through the UI. System, storage and other accounts' passwords are Owner-only.
- **Roles below the Owner**: custom roles (name, colour, position, permission bits) plus an implicit `@everyone`, with per-category and per-channel overwrites, Discord-style. The model, resolution order and hierarchy rules are in `doc/roles-and-permissions.md`; the code is `internal/perm`. Enforcement is server-side everywhere (REST, WebSocket hub, SFU, media serving); the UI only hides what a member cannot do. `@everyone`'s defaults must keep matching what members could always do; a new permission needs enforcement, a test, i18n (en + de) and a decision on its default.
- **Invite-only registration**: public self-registration is permanently disabled. Only the Owner, or members holding *Manage invites*, can create invite codes.
- Security model summary: see `SECURITY.md`. Don't weaken it (cookie flags, CSRF origin check, CSP, upload sniffing, rate limits) without an explicit decision.
- **Media retention must stay default-off**: `MEDIA_RETENTION_DAYS=0` means nothing is deleted automatically. Never change that default.

---

## 📁 REPOSITORY STRUCTURE

```
mnema-talk/
├── cmd/server/              # Main entrypoint (main.go)
├── cmd/webbundle/           # Signs and verifies the web bundle for the trusted desktop client
├── cmd/updater/             # Self-update sidecar: verifies a new image's signature and provenance first
├── internal/
│   ├── config/              # Environment configuration loading + validation
│   ├── db/                  # pgx pool, embedded versioned migrations (db/migrations/*.sql)
│   ├── httpx/               # Middleware: security headers, CSRF origin check, rate limits, client IP
│   ├── auth/                # Password hashing, sessions (token_version), invites, admin logic, permission guards
│   ├── perm/                # Roles and permissions: bits, resolver (Snapshot), cache, hierarchy rules, role/overwrite store
│   ├── roles/               # HTTP API for roles, member roles and channel overwrites; GET /api/permissions
│   ├── webhook/             # Incoming (Discord execute format) and outgoing (signed, durable queue) webhooks
│   ├── ssrf/                # Guard for outbound requests to member-chosen addresses (outgoing webhooks)
│   ├── metrics/             # Prometheus counters (no client library), feed /api/metrics and the live operator view
│   ├── webbundle/           # Build, sign and verify the desktop client's web bundle (Ed25519)
│   ├── chat/                # Channels, categories, messages, replies, pins, edit history, members; direct messages, blocks, reports, saved messages, forwarding, thread follow, search
│   ├── audit/               # Append-only audit log with per-entry visibility classes
│   ├── emoji/               # Custom emoji: storage, serving, admin API
│   ├── emojitext/           # How a custom emoji is written in message text
│   ├── linkpreview/         # Link cards (title, description, image) fetched through the SSRF guard
│   ├── mailer/              # Optional SMTP delivery of the account e-mails (texts live in internal/auth/locales)
│   ├── mdplain/             # Markdown to plain text for push and previews
│   ├── onboarding/          # Community name, rules and the acceptance gate for new members
│   ├── opwarn/              # Operator warnings (configuration and deployment hints) for the System tab
│   ├── push/                # Web Push (VAPID keys, subscriptions, delivery; texts in push/locales)
│   ├── repo/                # Repository-wide checks (file and function size limits)
│   ├── media/               # Uploads (MIME sniffing), authenticated media serving, retention
│   ├── s3/                  # S3 client
│   ├── events/              # Event types / publishing to the hub
│   ├── ws/                  # Real-time WebSocket hub (events, presence)
│   ├── bot/                 # Bot accounts, tokens, bot API authentication, slash commands (doc/bots.md)
│   ├── slash/               # Built-in slash commands: the registry and the Builtin interface (doc/bots.md)
│   ├── slashcmd/            # The built-ins besides /poll: /me, /shrug, /tableflip, /unflip, /spoiler, /nick, /thread, /msg, /timeout, /ban, /kick
│   ├── poll/                # Polls and the /poll command
│   ├── gateway/             # Bot gateway (Discord-style opcodes, intents, resume)
│   ├── sfu/                 # Pion WebRTC SFU (audio/video/screenshare forwarding)
│   ├── update/              # GitHub release check + client for the optional updater sidecar
│   ├── updaterd/            # The updater sidecar (cmd/updater): verify, pull by digest, tag, restart
│   ├── imageverify/         # cosign signature + SLSA provenance check of a release image
│   ├── version/             # Build version/revision (ldflags), shared with the web build
│   ├── nativepreview/       # Isolated transport preview router and ciphertext relay research fixtures
│   ├── tools/openapifix/    # Post-processes the generated OpenAPI document (`make openapi`)
│   ├── server/              # Router wiring + integration tests
│   └── testutil/            # Postgres fixture for integration tests (dockertest / TEST_DATABASE_URL)
├── api/                     # Generated OpenAPI document (`make openapi`)
├── doc/                     # Architecture, operations, upgrade, glossary, roles, webhooks, desktop docs; doc/images has the README screenshots
├── desktop-trust/           # Public trust anchor and signed trust statement for the desktop client
├── e2e/                     # Playwright browser tests (`make e2e`)
├── loadtest/                # Voice/screen-share load test (`make load-test`), see loadtest/README.md
├── web/                     # Vue 3 + Pinia + Vite + Tailwind CSS 4 SPA (shared UI classes in web/src/styles); web.go embeds web/dist
│   └── src/lib/             # Shared client modules: native/ (desktop IPC) and dfn/ (DeepFilterNet3)
├── scripts/git-hooks/       # pre-commit (gitleaks + gofmt); `make install-hooks`
├── scripts/smoke-image.sh   # Image smoke test on the production compose file (`make smoke`)
├── scripts/backup-drill.sh  # Backup -> restore drill on a throwaway stack (`make backup-drill`)
├── scripts/upgrade-drill.sh # Each supported release (GHCR) -> this build with seeded data (`make upgrade-drill`); shares scripts/drill-common.sh, upgrade-drill-data.sh
├── scripts/upgrade-postgres.sh # One-time PostgreSQL 17 -> 18 move (0.4 -> 0.5)
├── scripts/postgres-upgrade-drill.sh # Drill for it (`make postgres-upgrade-drill`)
├── scripts/fresh-install-check.sh # doc/deployment.md "Fresh install" in an empty directory with the built image (`make fresh-install`)
├── scripts/check-doc-links.mjs # Relative links and anchors in README, SECURITY and doc/*.md (`make docs-check`)
├── scripts/check-conflict-markers.mjs # Fails on merge-conflict markers in tracked text files (`make docs-check`, CI)
├── scripts/check-english-only.mjs # Fails on German outside the locale resources; exceptions in scripts/english-only-allowlist.txt (`make english-check`, CI)
├── deploy/monitoring/mnema-alerts.yml # Example Prometheus alert rules; promtool in CI, metric names checked against /api/metrics
├── scripts/backup.sh, restore.sh # Backup and restore of database, media and configuration
├── scripts/web-bundle-check.sh # Signs web/dist with a throwaway key and verifies it (`make web-bundle-check`)
├── scripts/security-floor.sh # Release step: raises the bundle security floor only on a `Security-Floor: raise` trailer
├── .github/workflows/
│   ├── ci.yml               # CI: gitleaks, Go vet/fmt/tests, govulncheck, frontend, e2e (Chromium, WebKit, Firefox, macOS WebKit calls),
│   │                        # docker build + smoke test, upgrade/restore drills, English-only check, trivy scan
│   ├── image-scan.yml       # Weekly trivy scan of the released :latest image
│   ├── trust-expiry.yml     # Weekly: fails when the desktop trust statement has under 60 days left
│   └── release.yml          # CD after green CI on main: release-please + GHCR image push
├── release-please-config.json, .release-please-manifest.json, version.txt, CHANGELOG.md
│                            # Managed by release-please; don't edit version/changelog by hand
├── Dockerfile               # Multi-stage build, non-root runtime
├── docker-compose.yml       # app + postgres + seaweedfs (production); optional coturn, updater
├── docker-compose.dev.yml   # Dev override: publishes postgres/seaweedfs on 127.0.0.1
├── Makefile                 # `make help` lists all targets
├── .env.example             # Every config variable, placeholder values only
├── .gitleaks.toml           # Secret-scan allowlist (placeholders, test-only values)
├── SECURITY.md              # Vulnerability reporting + security model
├── THIRD_PARTY_NOTICES.md   # Open-source software we use and ship, with licenses
├── CONTRIBUTING.md, CODE_OF_CONDUCT.md, SUPPORT.md # Community files; .github/ISSUE_TEMPLATE and pull_request_template.md
├── AGENTS.md                # This file
└── README.md                # Overview, quickstart, deployment
```

---

## 🛠️ DEVELOPMENT WORKFLOW FOR AGENTS

1. **Check `.env` and `.gitignore` first**: before modifying or creating files, make sure no secrets, databases or uploads are staged.
2. **Run `make check` before committing**: gofmt, `go vet`, unit + integration tests, govulncheck, frontend lint/test/build, `npm audit`, and the Docker build. That's the same set CI runs.
3. **Tests**: `go test ./...` (unit), `go test -tags=integration ./...` (integration; needs Docker or `TEST_DATABASE_URL`), `npm run test` in `web/`. New behaviour comes with tests.
   **Coverage gates** (same for Go and web): at least **90 % in total** and **80 % in every single file**. Go: `COVERAGE_MIN` / `COVERAGE_FILE_MIN` in the `Makefile` and `scripts/coverage-summary.mjs` (files under 5 statements are exempt from the floor); web: `web/scripts/check-coverage.ts` (all four metrics). Don't lower them without an explicit decision.
   **Tests and clean code**: tests describe behaviour, are grouped by feature and are named after what the caller observes. No files or tests named after coverage (`*Coverage*`, `*coverage*_test.go`, `coverage100`). Never add a test, branch, export or indirection only to satisfy a coverage number: if code is hard to cover, simplify the code or accept the gap. Delete tests that only pin implementation details. Production code follows clean-code basics: small focused functions and modules, clear names, no dead code, no speculative abstraction. Production source files in `web/src` stay under 400 lines (ESLint `max-lines`; generated files and tests are exempt): split by responsibility, centralise duplicates.

   **Size and complexity limits (Go)**: production `.go` files max 400 lines, `_test.go` files max 800, functions max 80 lines and cognitive complexity 15 (gocognit). Generated files (`Code generated ... DO NOT EDIT`) are exempt. File and function length are enforced by `internal/repo/limits_test.go`, complexity by `make complexity-check` (part of `make lint` and CI). Both keep a shrinking allowlist of old offenders: never add entries or raise a limit, refactor instead.

   **Size and complexity limits (TypeScript)**: the same numbers. Production `.ts`/`.vue` files in `web/src` max 400 lines, `*.test.ts`, `*.fixture.ts` and e2e specs max 800, functions max 80 lines (raw span, blanks and comments count, like the Go check) and cognitive complexity 15 (`eslint-plugin-sonarjs`). All enforced by ESLint (`web/eslint.config.ts`, part of `npm run lint` and CI; Vue SFC script blocks included; generated `types/rest.ts` and `third_party/` are exempt). There is no allowlist: never add an exemption or raise a limit, refactor instead.
   README screenshots (`doc/images/`) come from invented demo data only; regenerate them with `scripts/screenshots/run.sh` when the UI changes visibly, never from a real instance.
4. **Migrations**: add a **new numbered file** in `internal/db/migrations/` (e.g. `0003_<name>.sql`). **Never edit a migration that has already been applied**; they are embedded and run in order at startup.
5. **Configuration**: every new env variable is read in `internal/config`, documented in `.env.example` with a placeholder, and passed through in `docker-compose.yml` when needed.
6. **Pure Go preferred**: keep CGO out of the build (`CGO_ENABLED=0`) so cross-compilation for Linux (Unraid/Docker) stays trivial.
7. **Dependencies**: pin GitHub Actions to full commit SHAs; commit `go.sum` / `package-lock.json`; don't run `go mod tidy` in the Docker build.
   New or removed shipped dependencies (Go modules in the binary, web `dependencies`, vendored code in `web/src/third_party/`) go into `THIRD_PARTY_NOTICES.md` and the in-app list `web/src/lib/thirdParty.ts`.
8. **Never rewrite `main`**: no force push, no history rewrite, no moving or deleting release tags (a ruleset blocks it). release-please finds the last release by its tag commit in `main`'s history; once that commit is gone it re-releases old versions. Rebase your own unpushed commits onto `origin/main` instead.
9. **Pull requests, CI and merging**: `main` is protected by a ruleset. Changes land only through a pull request (no approvals required) with the checks `secret-scan`, `backend`, `frontend`, `govulncheck`, `docker`, `upgrade-drill (<version>)`, `e2e` and CodeQL `analyze (go)`, `analyze (javascript-typescript)`, `analyze (actions)` green; squash merge only. Agents may merge their own PRs once green. Release PRs are opened with `GITHUB_TOKEN` and start no workflows, so `release.yml` mirrors the conclusion of every check that main's ruleset requires (read from the ruleset; `scripts/release-checks.py`) from the tested commit as commit statuses on the release commit, and refuses to merge when one did not succeed (after checking that it only touches `version.txt`, `CHANGELOG.md` and the manifest and sits directly on the tested commit) and then merges it.
10. **Structured commits**: release-please derives versions and the changelog from them (`feat:` minor, `fix:` patch, `!`/`BREAKING CHANGE:` major; from 1.0.0 on there is no pre-major shift). The public API in `doc/api-stability.md` (REST per `api/openapi.json`, WebSocket events in `internal/events`, bot and webhook payloads, the desktop bridge, env vars, metric names) changes incompatibly only in a major release: deprecate in a minor first. `make openapi-diff` (CI `backend` job) fails a breaking OpenAPI change unless a commit carries a `BREAKING CHANGE:` footer. Event names are constants in `internal/events/types.go`, never string literals; adding one means updating `internal/events/testdata/events.golden.json` and `web/src/types/events.ts`. Use descriptive conventional commits:
   - `feat: add webrtc sfu audio track routing`
   - `fix: resolve websocket presence disconnect leak`
   - `docs: update deployment and caddy proxy guide`

    **English only.** Code, comments, identifiers, log and error messages, test names, fixtures, seed data, scripts, CI files and docs are English. German exists only in the locale resources (web `i18n/de.json`, the mail and push texts under `locales/`, the default names in `internal/chat/seed_layout.json`) and in tests that assert that German output. `make english-check` enforces it; an exception goes in `scripts/english-only-allowlist.txt` with a reason.

11. **Release checklist for security fixes** (the desktop client refuses bundles below `security_floor`, see `doc/desktop-trusted-client.md`):
   - A fix for a vulnerability in anything the web bundle ships (markdown, media, auth, message rendering, crypto, `web/src/lib`) is committed as `fix(security): ...` **and** carries a `Security-Floor: raise` trailer as the last paragraph of the PR body/commit message (a real git trailer; the subject alone does nothing). `release.yml` (`scripts/security-floor.sh`) then raises the bundle's `security_floor` to that release's version when it signs. Without the trailer the floor stays where it was; a `security`-scoped subject without it only logs a warning.
   - `web/desktop-compat.json` holds the baseline floor (never above the version being released) and `min_server_version`; raise the floor by hand there only for a withdrawn release that is not covered by a marked commit.
   - Bumping `web_client_api` or `min_server_version` locks older servers out of desktop clients that have seen the new bundle (high-water mark): do it only when the web app really needs it.

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
