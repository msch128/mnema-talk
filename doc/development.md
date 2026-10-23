# Development

How to build, test and release Mnema Talk. Read [AGENTS.md](../AGENTS.md)
first: the repository is public and the rules there (no secrets, conventional
commits, migrations, dependencies) apply to everyone.

## Setup

Requirements: Go (see `go.mod`), Node 26 + npm, Docker, GNU make (on Windows use
Git Bash or WSL). Run `make help` for all targets.

```sh
make install-hooks   # pre-commit: gitleaks + gofmt
make dev             # postgres + seaweedfs in Docker (docker-compose.dev.yml publishes
                     # them on 127.0.0.1), server on :8080 via go run (reads .env)
cd web && npm run dev   # Vite dev server on http://localhost:3000, proxies /api to :8080
```

For the Vite dev server, allow its origin in `.env`:

```sh
CORS_ALLOWED_ORIGINS=http://localhost:8080,http://localhost:3000
DATABASE_URL=postgres://mnema:<password>@localhost:5432/mnema_talk?sslmode=disable
```

`make build` produces `bin/mnema-talk` with the web app embedded (`make web`
builds only `web/dist`).

`main` is protected: changes land through a pull request (no approvals
required) once the `secret-scan`, `backend`, `frontend`, `govulncheck`,
`docker`, `e2e` and CodeQL `analyze` checks are green; squash merge only.

Frontend source uses strict TypeScript, including Vue scripts, media workers,
browser tests and build tools. See [the TypeScript contract](typescript.md)
for compiler scopes, runtime decoding and the generated-vendor exception.

## Testing

```sh
make test              # Go unit tests (-race)
make test-integration  # Go integration tests: starts postgres:18-alpine via Docker,
                       # or uses TEST_DATABASE_URL if set
make test-web          # vitest
make coverage          # Go (unit + integration) and web coverage: coverage.out,
                       # web/coverage/lcov.info; CI shows the totals in the job summary
                       # gates for Go and web: at least 90 % in total and 80 % in every file
                       # (Go: COVERAGE_MIN / COVERAGE_FILE_MIN in the Makefile;
                       # web: web/scripts/check-coverage.ts, all four metrics)
make lint              # gofmt, go vet, eslint
make typecheck-web     # complete frontend source and strict compiler checks
make contracts-check   # generated frontend REST contract drift check
make check             # everything CI runs: lint, tests, govulncheck, builds,
                       # npm audit, docker build
make e2e               # Playwright (Chromium) against the real binary, see below
make load-test         # voice/screen-share load test, see loadtest/README.md
make upgrade-drill     # 0.8.16 and 0.9.0 images -> this build with seeded data (FROM_VERSIONS=0.9.0 for one)
make backup-drill      # backup -> change -> restore on a throwaway stack (FROM_VERSION=0.9.0: back up on that release, restore into this build, check the downgrade refusal)
make postgres-upgrade-drill # the one-time PostgreSQL 17 -> 18 move
```

### Browser tests (e2e)

`make e2e` (= `e2e/run.sh`) builds the web app and the binary, starts a
throwaway PostgreSQL and SeaweedFS with Docker on loopback ports, runs the
Playwright specs in `e2e/tests/` with fake camera/microphone devices and tears
everything down again. `SKIP_BUILD=1` reuses the last build,
`E2E_INSTALL_BROWSER=1` installs the browsers named in `E2E_BROWSERS` first
(default `chromium`; also `webkit` and `firefox`) and `E2E_ONLY` is handed to
`playwright test`, so `E2E_ONLY="smoke.spec.ts"` runs one file and
`E2E_ONLY="--project=webkit"` another engine. Without it only the `chromium`
project runs.

The suite has three Playwright projects. **Chromium** is the required check and
runs everything. **WebKit** (what Safari, iOS and installed iOS web apps use)
runs every spec that does not join a call in CI as `e2e (webkit)` (Playwright's
Linux WebKit has no `RTCRtpScriptTransform`, so a sealed call cannot happen
there, see [Browsers](product-status.md#browsers); the call specs run in the separate
`e2e (webkit-macos-calls)` job on a macOS runner, which starts PostgreSQL and
SeaweedFS as local processes with `E2E_NO_DOCKER=1`; both jobs are not required
checks; Playwright stays at 1.63.0 because the WebKit 27.2 of 1.64.x reports a `style-src-attr` violation whenever a `<select>` is added to a page under `style-src-attr 'none'`, which the CSP guard in the specs rightly fails on); **Firefox** runs a smoke subset
(sign-in, chat, search, mentions, sealed Talk, key rotation, screen share) as
`e2e (firefox)`. Neither is a required check yet. Engine differences the specs
account for: Firefox has no context-level camera/microphone grants (its fake
devices are set by preferences in `playwright.config.ts`); every WebKit context
gets its own browser process, because WebKit mutes the microphone of all but one
page of a process; Chrome DevTools Protocol specs (crashing a tab, synthetic
touch) skip on the other engines.

Two more projects emulate a phone: `mobile-chromium` (Pixel 7) and
`mobile-webkit` (iPhone 15) run only `tests/mobile.spec.ts`, and the other
projects skip that file. The required `e2e` check runs `chromium` and both phone
projects.

Runs do not collide, so several can go at once (other worktrees, agents):
every run uses its own Docker Compose project (`mnema-e2e-<pid>`) and free
ports for PostgreSQL, S3, the app and WebRTC media, picked at start. Pin them
for debugging with `E2E_PROJECT`, `E2E_PG_PORT`, `E2E_S3_PORT`, `E2E_APP_PORT`,
`E2E_UDP_PORT` (first of 51 UDP ports) and `E2E_LOG`.

**Without Docker** (e.g. in a sandbox): start PostgreSQL 18 and a SeaweedFS
S3 gateway any other way, run the binary with the environment `e2e/run.sh`
uses (`APP_ENV=development`, a fresh `JWT_SECRET`, `ADMIN_INITIAL_PASSWORD`,
`DATABASE_URL`, `S3_*`, `UPDATE_CHECK_ENABLED=false`,
`WEBRTC_NAT_1TO1_IP=127.0.0.1`, ...), then point Playwright at it:

```sh
cd e2e && npm ci
E2E_BASE_URL=http://127.0.0.1:58080 E2E_ADMIN_USER=Herzog \
  E2E_ADMIN_PASSWORD=<the admin password you set> npx playwright test
```

The screen share specs need a Chromium with an H.264 encoder (CI's Chrome
has one; a locally installed open-source Chromium may not).

### Screenshots

The pictures in the README (`doc/images/`) show one invented community, Lantern
Fox Studio, a small game studio with eight members, four roles, custom emoji, a
poll, pins, a thread and a build-bot webhook. `scripts/screenshots/seed.mjs`
creates all of it (avatars, emoji and the game picture are drawn in the
browser; nothing is real) and spreads the messages over one working day;
`scripts/screenshots/run.sh` builds the app, starts a throwaway stack, seeds it,
starts a Talk with a fake screen share and captures the PNGs with Playwright
(1440x900, English, the phone shot at 390 px). `SHOTS_ONLY="chat roles"` limits
it; `SKIP_BUILD=1` reuses the last build. Never capture a real instance.

### CI pipeline

On every pull request and every push to `main`, `.github/workflows/ci.yml` runs
these jobs in parallel (rough wall times with warm caches):

| Job | What | Time |
|-----|------|------|
| `secret-scan` | gitleaks (pinned, checksum verified) over the tree and the history | < 1 min |
| `backend` | gofmt, go vet, OpenAPI check, unit tests, unit + integration tests with coverage (Postgres service), build | 4–6 min |
| `govulncheck` | Go vulnerability scan | 1–2 min |
| `frontend` | eslint, strict TypeScript, contract drift, vitest with coverage, build, `npm audit` | 2–3 min |
| `docker` | image build for linux/amd64 + linux/arm64, not pushed | 2–4 min |
| `e2e` | Playwright smoke test against the real binary | 4–6 min |

`codeql.yml` (actions, Go incl. tests and the integration tag, JS) runs on the
same events plus weekly. Caches: Go modules and build cache per job (keyed on
`go.sum` and the Go version), npm (`setup-node`), Playwright browsers (keyed on
the Playwright version) and Docker layers (GitHub Actions cache, scope `image`,
shared with the release build). A new push to a pull request cancels its
running CI; runs for pushes to `main` always finish, because the release
workflow starts from their result. Every job has a `timeout-minutes`; tests are
never retried.

## Web UI building blocks

The look of the web client comes from tokens and a few shared classes, not from per-view tweaks:

- `web/src/style.css` `@theme`: colours, radius scale (`rounded-sm` to `rounded-2xl`), elevation scale (`shadow-xs` to `shadow-2xl`), motion (`--ease-mnema`, 150 ms default, `animate-*` entry animations).
- `web/src/styles/buttons.css`: `.btn` with `-primary`, `-secondary`, `-ghost`, `-danger`, `-danger-solid`, sizes `-sm`/`-lg`, `.btn-icon`, `.btn-link`, `.chip` (a choice in a row), `.choice-card`, and the keyboard focus ring. A loading button sets `aria-busy="true"`.
- `web/src/styles/controls.css`: checkbox, radio, slider, select and text fields are styled on the elements themselves (no class needed); `.switch` is the toggle (`role="switch"` and `aria-checked`). Slider fill is painted by `lib/rangeFill.ts`.
- `web/src/styles/surfaces.css`: dialog scrim and panel (`BaseDialog`), `.pop-in` for menus and popovers, toast motion, the `shadow-popover` utility, and the single `prefers-reduced-motion` rule.

All of it respects the CSP (`style-src 'self'; style-src-attr 'none'`): no inline styles, only classes, tokens and CSSOM. `e2e/tests/a11y.spec.ts` runs axe (contrast and accessible names) over the main surfaces.

## Releases

Releases are automated with [release-please](https://github.com/googleapis/release-please)
(`.github/workflows/release.yml`) and driven by conventional commits
(`feat:` = minor, `fix:` = patch, `feat!:` / `BREAKING CHANGE:` = major). From
1.0.0 on, the public API listed in [api-stability.md](api-stability.md) only
breaks in a major release: `make openapi-diff` fails a change that breaks
`api/openapi.json` unless a commit carries a `BREAKING CHANGE:` footer.

1. After CI passes on a push to `main`, release-please opens or updates a
   **release PR** that bumps `version.txt` and `CHANGELOG.md`. Only the CI run
   for the commit that is still `main`'s tip releases; if `main` moved on, the
   run for the newer commit decides.
2. The release workflow merges that PR right away and publishes the GitHub
   Release and tag `vX.Y.Z`, so every green push to
   `main` that contains a `feat:` or `fix:` becomes a release. A PR opened
   with `GITHUB_TOKEN` starts no workflows, so the workflow reports the tested
   CI run's result as commit statuses on the release commit (only after
   checking that it touches nothing but `version.txt`, `CHANGELOG.md` and the
   manifest and sits directly on the tested commit); that satisfies the
   required checks on `main`.
3. The same workflow then builds the image and pushes it to GHCR:
   `ghcr.io/msch128/mnema-talk:X.Y.Z`, `:X.Y` and `:latest` (linux/amd64 and
   linux/arm64), with OCI labels,
   provenance and SBOM, and signs every tag keylessly with cosign (verify with
   [operations.md](operations.md#verifying-a-release-image)). The version and commit are baked into the binary
   (`internal/version`, via the `VERSION`/`REVISION` build args) and the same
   version into the web app, so the app can tell browsers when a newer
   version is running.
4. Windows Desktop DEV builds are published from
   [mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop).
   Releases through v0.7.3 still carry
   `Mnema-Desktop-DEV-vX.Y.Z-windows-x64.zip` on this repository.
   Extract that ZIP and run `Mnema Desktop DEV.exe`, then enter your HTTPS
   instance address. These are unsigned DEV packages with no automatic updater.
   See [desktop client status](https://github.com/msch128/mnema-talk-desktop/blob/main/experimental/README.md)
   for verification limits. The server ships through Docker; new releases do not
   publish standalone server binaries for Windows or macOS.
