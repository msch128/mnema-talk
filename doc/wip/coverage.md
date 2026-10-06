# WIP: coverage >= 80 % (Go and web), enforced in CI

Branch: `claude/0.4-coverage`. Goal from `doc/roadmap-0.4.0.md`: Go and web
coverage each at least 80 %, measured like CI, then enforced in CI.

Status: done. Both are above 80 % and CI fails below 80 % (last commit on the
branch). Remaining items below are optional margin work.

## How to measure (same as CI)

- Go: `make coverage-go` (unit + integration, `-coverpkg` over `./cmd/...
  ./internal/... ./web` minus testutil/tools), which ends with
  `node scripts/coverage-summary.mjs go coverage.out --min $(COVERAGE_MIN)` (statements).
  Needs Docker or `TEST_DATABASE_URL`. Without Docker, a local Postgres works;
  isolate in your own schema, and keep `public` on the path for `pg_trgm`:
  `TEST_DATABASE_URL='postgres://mnema:mnema@localhost:5432/mnema_test?sslmode=disable&search_path=<schema>,public'`.
  In this sandbox the ICE-based `internal/sfu` tests time out under `-race`;
  run without `-race` locally (coverage is the same, CI runs them with race).
- Web: `make coverage-web` (or `cd web && npm ci && npm run test:coverage`, then
  `node ../scripts/coverage-summary.mjs web coverage/coverage-summary.json --min 80`) (lines).
- Threshold: `COVERAGE_MIN ?= 80` in the Makefile, `--min 80` in
  `.github/workflows/ci.yml` (both jobs). The script exits 1 below the minimum,
  comparing the rounded figure it prints. On failure CI still writes the job
  summary and uploads the report.

## Numbers

| Date | Go (statements) | Web (lines) | Note |
|---|---:|---:|---|
| 2026-10-06 | 82.4 % (4301/5222) | 82.4 % (5878/7132) | baseline, main @ eaba62a |
| 2026-10-06 | 84.7 % (4425/5222) | 85.0 % (6062/7132) | after the tests on this branch |

## Done

- Go (integration, `internal/server`): admin invite management (validation
  table, list order, delete, expired invites); admin media dashboard (stats,
  paging, delete, idempotent delete, 410 after delete), prune keeps avatars,
  configured retention for manual prune; API docs page and OpenAPI route
  (session redirect, turned off).
- Go (unit): `internal/s3` client against a fake path-style S3 endpoint
  (bucket creation, upload, ranged get, delete, chunked batch delete with
  partial failures, 403 errors); `internal/media` retention scheduler.
- Web: `AdminInvitesTab`, `AdminMediaTab`, `LoginModal`, `CreateChannelModal`,
  `PresenceMenu`, `useMessageMenu`; more `ThreadSidebar` cases (send, reply
  target, failures, jump to replied message, loading).

## Next targets (optional, to widen the margin)

Go: `cmd/server/main.go` 0 % (needs a seam to be testable; not worth it),
`internal/sfu/announce.go` 51 %, `internal/auth/admin_seed.go` 75 %,
`internal/chat/channels.go` 76 %, `internal/server/metrics.go` 78 %.

Web: `TalkParticipants.vue` 40 % (Talk polish pending, wait),
`App.vue` 59 %, `AccountMenu.vue` 60 %, `SearchModal.vue` 62 %,
`Sidebar.vue` 68 %, `UserProfileModal.vue` 69 %, `useNavMenus.js` 71 %,
`AdminLayoutTab.vue` 73 %.

Avoid tests against `VoiceStage.vue` / `useWebRTC.js` internals until the
stream quality and Talk polish branches have landed.
