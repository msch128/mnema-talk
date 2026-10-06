# WIP: coverage >= 80 % (Go and web), enforced in CI

Branch: `claude/0.4-coverage`. Goal from `doc/roadmap-0.4.0.md`: Go and web
coverage each at least 80 %, measured like CI, then enforced in CI.

## How to measure (same as CI)

- Go: `make coverage-go` (unit + integration, `-coverpkg` over `./cmd/...
  ./internal/... ./web` minus testutil/tools), then
  `node scripts/coverage-summary.mjs go coverage.out` (statements).
  Needs Docker or `TEST_DATABASE_URL`. Without Docker, a local Postgres works;
  isolate in your own schema, and keep `public` on the path for `pg_trgm`:
  `TEST_DATABASE_URL='postgres://mnema:mnema@localhost:5432/mnema_test?sslmode=disable&search_path=<schema>,public'`.
  In this sandbox the ICE-based `internal/sfu` tests time out under `-race`;
  run without `-race` locally (coverage is the same, CI runs them with race).
- Web: `cd web && npm ci && npm run test:coverage`, then
  `node ../scripts/coverage-summary.mjs web coverage/coverage-summary.json` (lines).

## Numbers

| Date | Go (statements) | Web (lines) | Note |
|---|---:|---:|---|
| 2026-10-06 | 82.4 % (4301/5222) | 82.4 % (5878/7132) | baseline, main @ eaba62a |

Both were already above 80 % at the start, but with a thin margin; the other
0.4 branches add code, so the work here widens the margin on the weakest,
highest-value files before switching enforcement on.

## Least covered at baseline

Go: `cmd/server/main.go` 0 %, `internal/auth/invites.go` 37 %,
`internal/s3/s3.go` 41 %, `internal/sfu/announce.go` 51 %,
`internal/media/admin.go` 55 %, `internal/server/public.go` 62 %,
`internal/media/handler.go` 67 %.

Web: `TalkParticipants.vue` 40 %, `AdminInvitesTab.vue` 43 %,
`useMessageMenu.js` 47 %, `LoginModal.vue` 49 %, `ThreadSidebar.vue` 49 %,
`CreateChannelModal.vue` 53 %, `PresenceMenu.vue` 58 %, `App.vue` 59 %.

Avoid tests against `VoiceStage.vue` / `useWebRTC.js` internals for now (other
branches are reshaping them).

## Done

- Go: integration tests for admin invite management (validation, list order,
  delete, expired invites).

## Left / next targets

- Go: `internal/media/admin.go`, `internal/server/public.go`, `internal/s3/s3.go`.
- Web: `AdminInvitesTab.vue`, `LoginModal.vue`, `CreateChannelModal.vue`,
  `useMessageMenu.js`, `ThreadSidebar.vue`.
- Enforcement: `scripts/coverage-summary.mjs --min 80`, used in
  `.github/workflows/ci.yml` and the Makefile coverage targets.
