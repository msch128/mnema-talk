# Review: bot platform part 1 (origin/main..6f46c8b33)

Reviewer: independent, read-only (did not write the change).

**Heads reviewed:**
- Round 1: 010fa72fd. Five commits: 9d4a7eb1c, ebcd4a175, 1b722bcba, 972fbd5c5, 010fa72fd.
- Round 2: **6f46c8b33** ("feat: the Go SDK for bots", on origin/msch128/feat-bot-platform). It changes only `sdk/go/**` (29 files).

The round-2 results are in the section "Round 2: the Go SDK" at the end. F1 is resolved at 6f46c8b33. F2 to F7 are unchanged there; that commit touches nothing they concern. Each worktree was removed at the end.

## Verdict: MERGE AFTER FIXES. It cannot merge today.

The commit's own required check is red. `backend` now needs `sdk`, and the `sdk`, `backend-checks` and test-scripts paths each fail on their own, for six independent reasons (F1 to F6). Every failure was reproduced locally. All of them are small fixes. One forward-compatibility trap (F7) must be fixed before the generated web guard ships. The release job itself is sound: the rehearsal is reproducible byte for byte and the tarball is clean.

## Findings, ranked

### F1. BLOCKER (CI): the `sdk/go` module does not exist, so every Go-SDK gate fails
`sdk/go` holds only `README.md` ("Work in progress"). There is no `go.mod` and no package. Yet these were all wired up:
- `.github/workflows/ci.yml` sdk job, step "Go SDK": `go vet ./...` in `sdk/go` exits 1 with `go: warning: "./..." matched no packages / no packages to vet`. The same happens for `go test -race ./...` and `go vet -tags=contract ./...`. Transcript: `vet exit 1`, `test exit 1`, `vet contract 1` (Go 1.27.4, as in go.mod).
- `internal/server/sdk_contract_integration_test.go:40` `TestSDKContract/go` fails with `no packages to test`.
- `scripts/staticcheck.sh:32-33` (`run sdk/go`, `run sdk/go -tags=contract`): `make staticcheck` exits 2 with `staticcheck did not run cleanly: sdk/go/warning: "./..." matched no packages`.
- `.github/dependabot.yml` (gomod, `/sdk/go`): there is no go.mod, so Dependabot errors every week.
- `AGENTS.md:107,167`, `doc/bots.md` (SDKs) and `doc/bot-platform.md:135` describe `sdk/go` as an existing "own module" with examples and contract tests.

Smallest fix: wire the Go SDK in the commit that adds the module. Remove the `sdk/go` lines from ci.yml, staticcheck.sh, complexity-check.sh, dependabot.yml and the `go` subtest, and mark Go as "planned" in the docs. The other option is to land the module now.

### F2. BLOCKER (CI): `make test-scripts` fails because complexity-check.sh hard-requires `sdk/go`
`scripts/complexity-check.sh:18` passes `sdk/go` to gocognit. `scripts/tests/test_gate_tool_failure.py` runs it on a fixture tree without that directory, so 2 tests fail (`test_complexity_check_passes_a_clean_tree`, `..._still_reports_a_function_over_the_limit`) with `gocognit: open sdk/go: no such file or directory`. `make test-scripts` exits 2.

Fix: drop `sdk/go` until it exists (see F1), or skip directories that are absent, and add it to the fixture.

### F3. BLOCKER (CI): `api/openapi.json` is stale
`Message.components` and `core.Component` were added (`internal/chat/core/message_types.go:86`, `component_types.go`) without `make openapi`. `make openapi-check` exits 2 with "api/openapi.json is stale". The regenerated diff is +144 lines: `evidence/openapi-regenerated.diff`.

Fix: regenerate `api/openapi.json` and `web/src/types/rest.ts`, then apply F7 first.

### F4. BLOCKER (CI): the recursive `Component` type crashes the event registry test
`go test ./internal/events/registry` dies with `fatal error: stack overflow` in `describer.byKind` -> `fields` -> `node` (`internal/events/registry/schema.go:147,153,156,172,188`). `message_create` carries `core.Message`, which now carries `Component.Components []Component`. The describer has no cycle guard. `registry_test.go:148` calls `Schema(func(string) bool { return false })`, so nothing ever becomes a `ref`. It still overflows after `make openapi` (checked).

Fix: add a visited-set or depth guard in `describer.node` that emits a `ref` (or `any`) for a type it is already describing, plus a test with a self-referential struct. The other option is a non-recursive wire type (`ActionRow{Components []Component}` where `Component` has no children). That also matches Discord, whose rows do not nest. After that, regenerate `api/events.json` and `eventShapes.ts`.

### F5. BLOCKER (CI): migration 0106 is not in `doc/upgrade.md`
`go test ./internal/db` fails `TestUpgradeDocListsEveryMigrationFrom1_2` (`internal/db/checksum_test.go:82`) with "doc/upgrade.md does not document migration 0106_bot_platform.sql". It fails in both the unit and the integration run.

Fix: add the 0106 row after 0104 (`doc/upgrade.md:611`). It is additive: two columns with defaults and one new table.

### F6. BLOCKER (CI): the TypeScript contract suite needs routes this commit does not have
With `SDK_CONTRACT=1` against a real server, local Docker (transcript `contract.log`): 7 tests pass and 2 fail.
- `test/contract/bot.test.ts:117` "a button click reaches the SDK...": `posted.components` is `undefined`. The server still drops `components`.
- `test/contract/bot.test.ts:134` "a modal opened on a click...": `POST /api/channels/{id}/messages/{id}/components` answers `404 route not found`.

The routes coming later is expected and not a finding in itself. What is a finding is that a required check now depends on them: `sdk` is red on this commit, so `backend` is red too.

Fix: land these two contract tests together with the routes, or skip them behind a capability check until then. Do not weaken the job.

### F7. HIGH (API stability / desktop bundles): the generated guard turns new component types or styles into a whole-channel load failure
- An old 3.0.x bundle is fine. `isChatMessage` on origin/main (`web/src/types/rest.ts:2040`) does not reject unknown fields, so a message with `components`, unknown fields or unknown component types passes. Tested.
- The bundle generated from this commit is not. With `enums:"1,2,3,4,5,6,8"` on `Component.Type`, `enums:"1,2,3,4,5"` on `Style` and `maxLength` on `Label`/`Placeholder`/`URL`/`Value` (`internal/chat/core/component_types.go:36-56`), the regenerated `isChatComponent` requires `type ∈ {1..6,8}`, `style ∈ 1..5`, `label ≤ 80`, and so on. `isChatMessage` then rejects any message that carries a future component. `arrayDecoder('messages', isChatMessage)` (`web/src/stores/chat/messageWindow.ts:17`, `pins.ts:9`) throws `ContractError` for the whole page, so the channel history does not load. The same holds for `message_create`/`message_update` events (`web/src/types/events.ts:250-251`).
- `doc/bot-platform.md:181-185` explicitly plans mentionable selects (type 7) and layout components "on demand". Every desktop client still running a bundle built from this commit would lose any channel that contains such a message.
- Reproduction: `evidence/review_components.test.ts`, 4 tests passing against the regenerated `rest.ts`. One of them is "one such message fails the whole page of history".

Smallest fix: keep server-side validation where it is (in the coming `components_validate.go`), but describe `components` to OpenAPI as an opaque array, e.g. `swaggertype:"array,object"` on `Message.Components`, or drop the `enums`/`maxLength` tags from the wire type. The renderer then ignores what it does not know. Add a web contract test that a message with an unknown component type decodes.

### F8. MEDIUM (SDK robustness): a malformed frame from the server crashes the bot process
`sdk/typescript/src/gateway/gateway.ts:209-214` does `JSON.parse(text) as GatewayFrame`, then `f.s`. Line 217 does `f.d.heartbeat_interval` and line 280 does `ready.session_id`. All of this runs inside the WebSocket `message` listener. Node's EventTarget rethrows listener errors on `process.nextTick`, so the process dies. Transcript (`evidence/sdk/crash.ts`):
```
== null-frame   TypeError: Cannot read properties of null (reading 's') at Gateway.onFrame (gateway.ts:214)  exit code 1
== ready-null   TypeError: Cannot read properties of null (reading 'session_id') at Gateway.onReady (gateway.ts:280)  exit code 1
== garbage      (not JSON) ignored, READY ok, survived
```
A buggy proxy or server is enough to trigger it. The SDK's emitter already guards the listeners, but not this path.

Fix: in `onFrame`, drop anything that is not a plain object with a numeric `op`. Check that `d` is an object for HELLO and READY, and that `heartbeat_interval` is a finite number ≥ 1000. Wrap the handler in try/catch and route errors to `report()`. Add the two cases above as tests.

### F9. MEDIUM (SDK robustness): an unbounded `retry_after` hangs or spins the REST client
`sdk/typescript/src/rest/core.ts:170-174` takes `retry_after` as given, with no upper bound, and the pause persists on the bucket or globally. Above 2^31-1 ms (about 24.8 days), Node fires `setTimeout` after 1 ms, so `acquire()` (`core.ts:118-133`) loops at 1 ms forever and logs a `TimeoutOverflowWarning` on each turn. Transcript (`evidence/sdk/retry_after.ts`):
```
retry_after=3000000s global=true: request done=false, requests seen=1, TimeoutOverflowWarnings=1561, cpu 127.499 ms in 2000 ms
```
With `global: true` this stalls every route. Smaller values (for example a day) silently hang requests.

Fix: clamp at a sane maximum, for example 60 s plus a little. Beyond it, throw a `RestError` 429 with `retryAfter` and let the bot decide. Also clamp `sleep()` to 2^31-1.

### F10. LOW (release UX): the packed `package.json` keeps `prepare`, so npm warns every consumer
On `npm install ./mnema-talk-sdk-3.1.0.tgz` in clean `node:24-alpine` (npm 11.19.1) and `node:26-alpine` (npm 11.20.0):
```
npm warn install-scripts 1 package has install scripts not yet covered by allowScripts:
npm warn install-scripts   mnema-talk-sdk@3.1.0 (prepare: npm run build)
```
This contradicts "no runtime dependencies" in spirit and invites `npm install-scripts approve`. Approving would not work anyway, because `scripts/clean.mjs` and TypeScript are not in the tarball.

Fix: in `sdk-package`, before `npm pack`, run `npm pkg delete scripts.prepare scripts.test scripts.test:contract` in the sealed container. Or use `prepack` only and document the pnpm/git path differently.

### F11. LOW (design, doc/bot-platform.md): install-time allows override channel denies set by Manage-channels admins
Role overwrites combine as "all role denies, then all role allows" (`doc/roles-and-permissions.md:231`). An install writes allows on the bot's own role (`doc/bots.md:241`). That overrides a deny an admin put on the shared Bot role, or on another role the bot holds, in that channel. It is done by a member with *Manage bots* who lacks *Manage channels*. The same is already possible through `PUT /bots/{id}/roles` with a role carrying channel overwrites (`doc/bots.md` table, row 1), so this is not a new escalation. The doc's sentence "they change nothing for any member" is true but incomplete.

Suggested: the confirm screen shows, and the install refuses or leaves unticked, any permission a channel denies to the Bot role, unless the installer holds *Manage channels* there. Or the doc states the override explicitly.

### F12. LOW (design): the defaults on the confirm screen and manifest phishing
- "All ticked, each can be unticked" makes accepting the whole request the default, including `mention_everyone` and `manage_messages`. Safer: leave the two elevated-ish ones unticked by default.
- `name`, `description` (200 characters) and `homepage` come from the attacker. The confirm screen should mark them as "from the manifest" so that, for example, "Official Mnema Talk security bot" cannot pass as a system statement.
- A manifest command can share a built-in's name (`/ban`, `/timeout`). They are listed side by side (`doc/bots.md:759`), so a member can pick the bot's `/ban` and hand the bot the target and the reason. Consider marking manifest commands that collide with built-ins on the confirm screen.

The rest of the model holds against a malicious manifest:
- Channel permissions only.
- The installer can only untick.
- Overwrites go on a role only the bot holds, capped per channel by the bot invariant.
- Nothing is fetched by URL.
- The avatar goes through the ordinary avatar upload.
- The fragment is never sent to the server.
- Opening the link does nothing without a click on Add.

### F13. LOW
- `Component.Value` is `omitempty` (`component_types.go:56`). If the later type-5 payload reuses it, an empty text input loses `"value": ""`, which `doc/bots.md` (modal submit example) and Discord include. The SDK copes (`c.value ?? ''`, `interaction.ts:82`). Use a separate submit type or drop `omitempty`.
- `uploadFile` appends `file` before `content`, `parent_id` and `reply_to_id` (`rest/client.ts:92-96`). The server streams the file to storage before it sees those fields and only rejects an invalid target afterwards (`internal/media/stream.go:55-60,138-146`). Append the text fields first, as the web app does.
- Heartbeats bypass the outbox budget (`outbox.ts:40-43`), and `heartbeat_interval` is not clamped. A server sending `0` gets 198 heartbeats in 300 ms (break.test.ts). Only a hostile server is affected. Clamp it.
- The CI `sdk` job tests only Node 26 (`ci.yml`, setup-node `node-version: 26`), while `engines` and the docs promise Node ≥ 24. Add 24.
- `Interaction.user` is typed as non-optional but is `payload.member?.user` (`interaction.ts:73`).
- `validateManifest` (`manifest.ts:32-44`) does not check intent or permission names or the avatar's 256 KB limit, so `encodeManifest` produces links the server will refuse.
- The SDK tarball is unsigned. Its `.sha256` is uploaded with the same token, so it protects against corrupt downloads only, unlike the signed web bundle. Consider build provenance (`actions/attest-build-provenance`) later. The sealed, reproducible build makes an independent rebuild possible today.

## Checked and found right
- **The release job** (`release.yml` sdk-package):
  - Runs only after `release_created`.
  - Job-level `permissions: contents: write`, the minimum for upload, under top-level `permissions: {}`.
  - Actions pinned by SHA.
  - `persist-credentials: false`, checkout of the tag.
  - The token appears only in the upload step.
  - No artifact hand-off between jobs, so nothing can be swapped between build and upload.
  - The version reaches `run:` through `env`, not `${{ }}`, so there is no expression injection. It is release-please's semver, used in sed and grep only.
  - The container has no capabilities and no-new-privileges, runs as the runner's user and gets only `HOME`.
  - The network is on only for `npm ci --ignore-scripts`, and `npm version`/`npm pack` run with `--network none`.
  - The placeholder guard fails when `0.0.0-dev` is missing.
  - The release commit still touches only version.txt, CHANGELOG.md and the manifest.
- **The rehearsal**: I ran the step's script verbatim (only GNU `sed -i` shimmed for macOS) against `git archive 010fa72fd` with Docker and the Dockerfile's pinned `node:26-alpine@sha256:8996…`, twice.
  - Both runs produced the identical sha256 `cf756c00…94a36`, so the build is reproducible.
  - 57 files: `dist/**`, `README.md`, `LICENSE`, `package.json`. No tests, fixtures, src, examples or secrets (grep for `mnb_` tokens, key headers and passwords found nothing).
  - `dist/version.js` and `package.json` both say 3.1.0.
  - `exports` and `types` resolve. In clean `node:24-alpine`, `npm install <tgz>` works, an ESM import of 42 exports and of `mnema-talk-sdk/package.json` works, and a strict `tsc` (TS 7.0.2, `skipLibCheck: false`, nodenext) of a consumer file passes.
- **CI**:
  - actionlint is clean on ci.yml and release.yml.
  - The required check names are unchanged. `backend` only gains `sdk` in `needs`.
  - Docs-only PRs skip every sdk step, so they stay green.
  - Flake risk once green: real-timer tests (the Retry-After test about 1 s, the contract 429 test about 5 s, timeouts 20 s and 60 s). That is acceptable, but `sdk` now gates every PR.
- **Gateway against `internal/gateway`**:
  - Opcodes 0/1/2/3/6/7/9/10/11 match.
  - IDENTIFY sends the token and the intents. The bits 1, 2, 512 and 1024 match `bot.AllIntents`.
  - The Authorization header is sent on the upgrade.
  - The first heartbeat is jittered and comes within the server's 1.5-interval deadline.
  - A missing ACK at the next beat counts as a zombie, so the SDK drops with 4900 and resumes.
  - RESUME carries the session and the seq. INVALID_SESSION gets a re-IDENTIFY on the same socket, which is what the server expects (`serve.go:236-242` leaves the connection open).
  - The fatal close codes are exactly the server's: 4004 (also bot disabled, deleted or banned, and token revoked), 4013, 4014.
  - 4000 ("resumed elsewhere", "Internal error") gets a new session. 4008 ("Too many connections", rate limits) waits at least 30 s. 4009, 1000, 1013 and 1006 resume.
  - Backoff is exponential with jitter, capped at 60 s, with no tight loop.
  - My test "dropped mid-resume (no RESUMED)" passes: the third connection resumes with the same session and seq.
- **REST**:
  - `Authorization: Bot`, the base path, the Discord error shape `{message, code}`.
  - Buckets: 50/s global and 5 per 5 s per channel. 429 handling honours `retry_after` and `X-RateLimit-Global`.
  - The multipart field names match `internal/media/stream.go:29`.
  - `allowed_mentions`: the SDK sends none by default and the server then never notifies @all, @here or roles (`doc/bots.md:308-310`), so there is no footgun.
  - A 429 storm stays bounded (1+3 sends per request, then `RestError` 429).
- **Interactions**: callback types 4/5/6/7/9 and their allowed origins match `doc/bots.md`. The SDK does not track the 15-minute lifetime; the server enforces it (`interaction_store.go:19`).
- **Security of JSON handling**: no prototype pollution (no deep merges; `compact` uses `Object.fromEntries`). The regexes are anchored and linear on server data. An unbounded response body is bounded by the 15 s per-attempt AbortSignal.
- **The SDK's own gates**: `npm ci`, `typecheck`, `build`, `test` (64/64) and `pack --dry-run` all exit 0. My adversarial file `evidence/sdk/break.test.ts` passes 5 of 5.
- **Migration 0106**, on a database migrated to 0104 with a real bot row:
  - It applies cleanly and the defaults are `''`.
  - The 201-character description CHECK fires, and the non-array JSON CHECK fires.
  - A message delete cascades to its components. A bot delete keeps its messages and components (clicks will 404 per the doc). A delete of the bot's user removes both.
  - Locks are brief: ACCESS EXCLUSIVE on the tiny `bots` table, and a short SHARE ROW EXCLUSIVE on `messages` for the FK of an empty new table.
- **The clash with 0105** (feat-timed-messages, 4d6c75fab): the two are schema-independent. 0105 touches messages, scheduled_messages, channels, instance_settings and media; 0106 touches bots and a new table. The runner applies unapplied files by name (`internal/db/db.go:123-132`) and refuses only applied files newer than the ones it ships (`db.go:280-310`), so both merge orders work. The only conflict is two adjacent rows in `doc/upgrade.md`.
- **Other gates**: `go build`, `go vet` with and without `-tags=integration`, `make complexity-check`, `go test ./internal/repo/`, `make openapi-diff` (no breaking change, also with the regenerated document), `make contracts-check`, `make english-check`, `make docs-check`, web `npm run lint` and `npm run typecheck` all exit 0. Integration runs of `internal/chat/core`, `internal/bot` and `internal/gateway` pass. `internal/db` fails only F5.
- **AGENTS.md rules**: English only and no AI attribution, OK. Size limits OK (`internal/repo` passes). No applied migration was edited. Every gate failure listed in F1 to F5 comes from wiring or missing regeneration, not from a gate being weakened.

## Not checked
- The full `go test -tags=integration ./internal/server`. I ran only `TestSDKContract` from it.
- e2e, the Docker image build, upgrade-drill and CodeQL.
- A real release run: the `gh release upload` and the job summary. They mirror the web-bundle job.
- Dependabot's real behaviour on GitHub.
- The coming routes (components, modals, install). The doc model was argued in F11 and F12, not tested.
- Node 24.12 locally, not 24.0. Some unit and integration runs started while the load average was briefly between 12 and 22.

## Evidence (in this directory)
- `evidence/review_components.test.ts`: F7 (vitest, run against `git show origin/main:web/src/types/rest.ts` and the regenerated `rest.ts`).
- `evidence/openapi-regenerated.diff`: F3.
- `evidence/sdk/crash.ts`: F8. `evidence/sdk/retry_after.ts`: F9. `evidence/sdk/break.test.ts`: mid-resume drop, malformed frame, 429 storms, heartbeat 0.
- `pack.sh`, `pack.log`, `rehearsal/sdk-out/`: the release rehearsal. `tar-list.txt`: the tarball's file list.
- `contract.log`: F6 and F1 (TestSDKContract). `gates/*.log`: every gate's output.

---

## Round 2: the Go SDK at 6f46c8b33

The verdict for the branch at 6f46c8b33 is unchanged: **MERGE AFTER FIXES**.
- F1 is fixed. `sdk/go` is a real module: `github.com/msch128/mnema-talk/sdk/go`, go 1.27.0, its only dependency gorilla/websocket v1.5.3.
- Still blocking: F2 (`make test-scripts` fails 2 of 5), F3, F4, F5, F6 and F7. None of them is in `sdk/go`.
- F6 now also covers the Go contract suite: `TestContractWithARunningServer/a_button_click_...` fails at `contract_test.go:176` with "posted components = []", and the later steps do not run.

### Go findings

**G1. MEDIUM: no read limit on gateway frames.** A malicious server can exhaust the bot's memory.
- `gateway_conn.go:95,120` calls `ws.ReadJSON` and never `SetReadLimit`, and gorilla's default is unlimited.
- `evidence/go/review_break_test.go` `TestReviewHugeFrameIsReadWhole`: "a 67108866-byte dispatch was read and delivered (no read limit)".
- Fix: `ws.SetReadLimit(...)` right after dial, at a few MiB (a message is at most 4000 characters plus embeds and components), plus a test.

**G2. MEDIUM: an unbounded `retry_after` pauses the whole client for weeks.** Same class as F9.
- `rest.go:195-210` accepts any value. `ratelimit.go:114-127` stores the pause, and every later request on that bucket (all of them, if global) sleeps until its context ends.
- `TestReviewHugeRetryAfterPausesTheClient`: after one 429 with `retry_after: 3e6, global: true`, an unrelated `Channels` call times out without sending a request, with "pause left 833h0m0s".
- A bot using `context.Background()` hangs for good. In addition, a float-to-Duration conversion past MaxInt64 is implementation-defined in Go.
- Fix: clamp the wait (for example to 60 s). Beyond it, return the `*RESTError` with `RetryAfter` and do not store the pause.

**G3. LOW-MEDIUM: one goroutine per dispatch, unbounded.**
- `client.go:173-193` starts a goroutine for every event before it waits for a `HandlerConcurrency` slot. Each one holds its payload.
- `TestReviewDispatchFloodGrowsGoroutines`: with one blocked handler, 20000 dispatches give 11 -> 20011 goroutines.
- A chatty channel and a slow handler grow memory without bound; a hostile server makes it trivial.
- Fix: a bounded queue in front of the slots (block the read loop, or drop and log above N). Or document that handlers must not block for long.

**G4. LOW: `go 1.27.0` in `sdk/go/go.mod`** forces every consumer onto the newest Go. With `GOTOOLCHAIN=local` on 1.26 the module does not build. Unless a 1.27 feature is used, set the oldest supported Go (1.26).

**G5. LOW: govulncheck does not cover the SDK module.** CI runs `govulncheck ./...` at the root only (`ci.yml:573`), and nested modules are excluded, so `sdk/go` (a shipped artifact) is not scanned. The same is true of `loadtest/viewer`. Fix: add a `cd sdk/go && govulncheck ./...` step.

**G6. INFO: `go get` also downloads the root module.**
- `go get github.com/msch128/mnema-talk/sdk/go@<commit>` also fetches the 24 MB root module zip (`github.com/msch128/mnema-talk v1.5.2-0.2026…`, plus `v1.5.1.mod`) to prove that the root module does not provide the package.
- It is one-time and harmless. The `v1.5.x` pseudo-version comes from the root module ignoring v2+ tags.
- Mention it in the README if people ask why it pulls the server.

### Go: checked and found right
- **Module placement:**
  - Own `go.mod` and `go.sum`, no go.work.
  - The root `go list ./...` contains no `sdk` package (0), so the root build, vet and tests exclude it.
  - `make staticcheck` now runs `sdk/go` with and without `-tags=contract` and passes (exit 0).
  - `make complexity-check` passes with `sdk/go` included.
  - `internal/repo` limits walk the whole tree, so they cover `sdk/go`; they pass.
  - Dependabot's gomod `/sdk/go` entry now points at a real go.mod.
- **`go get` from a clean module outside the repo**, with an empty GOPATH and modcache, against the pushed commit:
  - `go get github.com/msch128/mnema-talk/sdk/go@6f46c8b33` resolved `v0.0.0-20261113015249-6f46c8b335a4` with gorilla/websocket v1.5.3.
  - A program using `New`, `OnMessageCreate` and `AddBotLink` builds and runs.
- **Gates in `sdk/go`:** gofmt clean; `go vet ./...` and `go vet -tags=contract ./...` exit 0; `go test -race ./...` passes.
- **Gateway against `internal/gateway`:**
  - Opcodes match.
  - HELLO is required, with `heartbeat_interval > 0` checked before `rand.Int64N` (`gateway_conn.go:127`).
  - IDENTIFY sends the token, intents, properties and presence, with the Authorization header on the upgrade.
  - Heartbeats are jittered, carry the seq, and a missing ACK means 4900 and a resume.
  - RECONNECT gives 4901 and a resume.
  - INVALID_SESSION gets a re-IDENTIFY on the same socket, as the server expects.
  - The fatal codes are exactly 4004, 4013 and 4014, returned as `*CloseError`.
  - 4000, 4003 and 4005 start a new session. 4008 waits at least 30 s; backoff is 1 to 60 s with jitter.
  - A 401 or 403 on GET /gateway is fatal.
  - A failed dial clears the resume URL.
  - The resume after a drop mid-resume repeats the same session and seq (`TestReviewDropMidResumeResumesAgain`).
  - A malformed frame does not crash the client: `null`, READY with `d: null` and non-JSON end the connection and lead to a reconnect (`TestReviewMalformedFramesDoNotCrash`). The handlers recover from panics.
  - Outgoing frames are capped at 4096 bytes and 120 per minute.
- **REST:**
  - `Authorization: Bot` and the User-Agent carry no personal data.
  - Response bodies are capped at 16 MiB.
  - Errors use the Discord shape.
  - Buckets match the TypeScript SDK and the server (50/s global, 5 per 5 s per channel, reactions grouped).
  - 429 retries are bounded (3). `X-RateLimit-Global` is honoured.
  - Upload fields are written before the file part, which fixes the order issue the TypeScript SDK has (F13).
- **allowed_mentions:** it is a pointer with omitempty, so nothing is sent by default and the server then never notifies @all, @here or roles. An empty `AllowedMentions{}` encodes as `{}`, which the server reads as "notify nobody" (`internal/chat/messages/send_context.go:110-124`). That is safe. `Edit.Components *[]Component` sends `[]` to remove components, as documented.
- **Contract against a real server** (`TestSDKContract/go`, Docker): me, gateway, post, get, command register and list, run command and reply all pass. It fails at the click step only (F6).

### Go: not checked
- The Go contract steps after the click (they do not run once a step fails).
- Windows and Linux builds of the examples; macOS arm64 only.
- A real `sdk/go/vX.Y.Z` tag flow, which does not exist yet.

### Go evidence
- `evidence/go/review_break_test.go`: copy it into `sdk/go` and run `go test -run Review -v .`.
- `gates-go/*.log`.

---

## Round 3 (lead's "round 2"): server routes and fixes at 6cce1efec

**Head reviewed: 6cce1efec** (origin/msch128/feat-bot-platform). The new commits:
- f532305e7: components, modals, callbacks 4/5/6/7/9, @original, install from a manifest.
- 8a967ff95: SDK hardening.
- 6cce1efec: CI, release and docs.

I worked in a fresh worktree at `../mnema-talk-review-sdk`, read-only, and removed it at the end.

### Verdict on the whole branch as a merge candidate: MERGE AFTER FIXES

- The security review of the server part found **no exploitable defect**. Every attack I tried through the real routes was refused.
- Every finding from rounds 1 and 2 is fixed, except the provenance suggestion, which was left for later.
- **Two gates are still red**, and both are required:
  - R1: the English-only check.
  - R2: the Go per-file coverage floor. It fails in four new or changed files, and the untested lines include the guard branches of the one new function in `internal/perm`.

Both are small. Fix them on current main and re-run the gates, and the branch is mergeable.

### Blocking

**R1. BLOCKER: `make english-check` fails** (exit 2):
```
German text outside the allowlist (the code and docs are English only):
internal/chat/core/components_validate_test.go:184: ... strings.Repeat("ä", MaxInputLen+1) ...
```
The test needs a multi-byte rune, not a German one. Fix: use `"é"`, `"日"` or `"é"`. Do not add an allowlist entry for it.

**R2. BLOCKER: `make coverage-go` fails its per-file floor.** Total coverage is 94.4 %, but four files are under 80 %:
- `internal/perm/store_overwrites_bot.go`: 58.3 % (7 of 12 statements).
- `internal/chat/botapi/botapi_edit.go`: 74.4 %.
- `internal/bot/interaction_callbacks.go`: 78.6 %.
- `internal/chat/core/components_store.go`: 79.2 %.

The uncovered blocks are behaviour, not padding:
- In `AllowBotRoleInChannel` (`store_overwrites_bot.go:29-47`): refusing non-channel-scoped bits, refusing a role that is not a bot's own, and refusing a temporary Talk.
- The note branch of `updateTarget` (`interaction_callbacks.go:89-102,111`): callback 7 or `PATCH @original` on an ephemeral note when the bot can no longer send (`CheckBotSend`), the empty-note refusal, and embeds on a note.
- The error paths of `SaveComponents`, and in `botapi_edit.go` the decode errors and the paths for a message in another channel or one that is not the bot's own.

Fix: add behaviour tests for those refusals: a unit test for the perm guard, and an integration case "bot loses Send messages, then edits its note". Lowering the gate would be against AGENTS.md.

### Earlier findings: re-verified with my own reproductions, and a revert of each fix fails its test

| ID | Status at 6cce1efec | How I checked |
|---|---|---|
| F1 Go module | fixed | `sdk/go` gofmt, vet (plain and `-tags=contract`), `test -race`: pass |
| F2 test-scripts | fixed | `make test-scripts` exit 0 (the fixture now creates `sdk/go`) |
| F3 openapi stale | fixed | `make openapi-check` and `make openapi-diff` exit 0 (no breaking change since v3.0.11); `make contracts-check` exit 0 |
| F4 registry recursion | fixed | `go test ./internal/events/...` passes. **Mutation**: removing `\|\| d.open[t]` from `schema.go` gives `fatal error: stack overflow` |
| F5 upgrade.md | fixed | `go test ./internal/db` passes |
| F6 contract suites | fixed | `SDK_CONTRACT=1 TestSDKContract`: TypeScript 9/9, Go passes |
| F7 forward-compatible guard | fixed | `ActionRow`/`Component` are wire types without enums or limits. My round-1 vitest now fails on its "rejects a future type" assertions, which means old and new bundles accept type 7 and 17, style 6 and long labels |
| F8 TS crash on a malformed frame | fixed | `evidence/sdk/crash.ts`: null-frame and ready-null survive (exit 0). **Mutation**: removing the guard and try/catch in `onFrame` makes `test/robustness.test.ts` fail with `TypeError: Cannot read properties of null (reading 's')` |
| F9 TS `retry_after` | fixed | A 3e6 s 429 rejects at once with `RestError` 429 and stores no pause (`MAX_RETRY_AFTER_S = 60`). **Mutation**: with `withinCap = true` the robustness test did not finish (still waiting after 300 s), so it does not pass, but it hangs rather than failing fast (see R8) |
| F10 `prepare` in the tarball | fixed | Release step rehearsed with Docker: the packed scripts have no `prepare`, and the step now checks for it. `npm install` in clean `node:24-alpine` prints no install-scripts warning, and the import gives `VERSION` 3.1.0 |
| F11 deny override | documented | `doc/bots.md:255-269` and `doc/bot-platform.md:76` state the override and how to keep a bot out |
| F12 defaults and labels | fixed | Mention everyone and Manage messages start unticked; manifest text is labelled as the bot's own words; commands with a built-in's name are marked (`doc/bots.md:236-241`) |
| F13 | fixed | Upload fields come first (TS test). The HELLO interval is clamped (`break.test.ts`: 1 heartbeat in 300 ms instead of 198). The CI `sdk` job is a Node 24 and 26 matrix. `Interaction.user` is optional. The manifest checks intents, permissions and the 256 KB avatar. The modal submit builds `value` explicitly |
| G1 Go read limit | fixed | `ws.SetReadLimit(MaxGatewayFrame)` (`gateway.go:149`). My 64 MiB frame now ends the connection. **Mutation**: removing it fails `TestAFrameAboveTheReadLimitEndsTheConnectionUndelivered` |
| G2 Go `retry_after` | fixed | A 3e6 s 429 returns at once and nothing is paused: the later request goes through. **Mutation**: disabling the cap fails `TestARetryAfterBeyondTheCapFailsAtOnceAndPausesNothing` |
| G3 Go goroutines | fixed | 20000 dispatches with a blocked handler: 11 -> 23 goroutines. **Mutation**: breaking the queue bound fails `TestADispatchFloodWithASlowHandlerKeepsGoroutinesBounded`. See R7 |
| G4 go directive | fixed | `go 1.26.0` |
| G5 govulncheck | fixed | ci.yml now scans `sdk/go` and `loadtest/viewer` |
| G6 root zip download | info, unchanged | |

### Security review of the server part

**Install from a manifest** (`internal/bot/handler_install.go`, `internal/perm/store_overwrites_bot.go`):
- One `LockedCreatingBot` transaction, which re-checks Manage bots under the lock (`handler_install.go:191`).
- Every channel must be known, not a direct message conversation and viewable by the installer (`:235-247`); anything else is 404 like a hidden channel.
- Permissions are capped to the manifest's keys and to channel-scoped bits (`store_overwrites_bot.go:28`).
- The overwrite is only on a role with `bot_user_id` (`:31-37`).
- The per-channel limit "only what the installer holds there" is enforced by the bot invariant at commit; it is not a pre-check. The tests confirm it: `TestInstallRefusalsCreateNothing` covers manage_messages without holding it -> 403 naming "manage messages in #open", with no row left behind and nothing in the audit log, and also covers elevated permissions, admin-area permissions, unknown names, categories, DMs and hidden channels.
- The parallel-install and demotion races are all-or-nothing in deep mode.
- The audit entry `bot.installed` carries the homepage, permissions, intents, channel ids and commands, never the secret (`installEntry`).
- The token's secret is only in the 201 answer. The issuer binding still applies.
- A Manage-bots member cannot gain anything through the bot: everything the bot gets is capped at the installer's rights per channel, and the installer already had those.

**Clicks and modal submits** (`components.go`, `component_values.go`, `modals.go`):
- `clickGate` checks View channels (404), accepted rules, timeouts (403) and the shared 30-per-minute limit, before the body is even decoded.
- The message must be in that channel and authored by a bot that is enabled, active and still able to view the channel. The component must be on the stored message and not disabled.
- String-select values must be its options. User selects take only active non-bot members who can view the channel. Role selects exclude `@everyone` and managed roles. Channel selects take only text, voice and forum channels that both the clicker **and the bot** can view, never a DM.
- A note is bound to its invoker and channel and lives 15 minutes from its interaction; editing does not extend it. A modal is bound to its invoker and channel, can be taken once under the store mutex, and is given back only if delivery failed (409).
- Interaction tokens are 24 random bytes, stored as SHA-256 and compared in constant time, bound to the bot. Interactions expire after 15 minutes and the store is capped per bot and per invoker.

My attack tests (`evidence/r2/zz_review_attacks_integration_test.go`, all passing, meaning refused):
- Another bot using bot A's interaction id and token: callback, `PATCH @original`, `DELETE @original` and follow-ups all get 404. A second first answer is refused. A tampered token gets 404.
- A bot editing a member's message, or another bot's, with components: refused, and no `message_components` row appears. A member posting `components` through the member route stores none.
- Select values naming a private channel, a channel the bot cannot view, a member who cannot view the channel, the bot itself or a random id: 400. A click by a member who cannot view the channel: 404. Nothing was delivered.
- A delivered interaction answered after the bot was disabled: 401. See R4.

**@original edit and delete:** the target is only the interaction's own response, or the bot's own component message or note. Edits go through `ownMessageIn` plus `EditBotMessage`/`DeleteOwnMessage`, which require the bot's own message in that channel and Send messages for edits. A bot cannot reach another bot's message or a member's message.

**Visibility:**
- Components are loaded only through `messages.Enrich` (`core.LoadComponents`), so they appear exactly where the message does: REST, the gateway's existing per-event visibility filter, and outgoing webhooks, whose payload is the message.
- Notes and modals are sent only to the invoker (`SendToUsers([]uuid.UUID{invoker})`).
- Incoming webhooks ignore `components` (`internal/webhook/execute.go:123`).
- The canary GET sweep and the visibility sweep passed as part of the full suite. The canary world does not seed components, though (R6).

**Races through the real routes, `RACE_SWEEP=1`:**
- Install against demotion, and parallel installs: pass.
- Click against message delete: passes. Delete-wins happened in 1 of 600 deep rounds over three runs (199/200, 200/200, 200/200 delivered first), so the branch exists but is rare. Its handling (404, or delivery of the pre-delete state followed by callback 7 -> 10008) also holds by reading the code.
- Click against bot disable, token regeneration and member ban: these **do not run in deep mode** (R3).

**Crash points:** both multi-table writes are single transactions: the bot post or edit plus `SaveComponents` via `InTx`, and the install. The crash-point suite does not list them (R6).

**Permission matrix and IDOR:** the matrix rows exist (`POST /api/admin/bots/install` and `.../manifests/check` -> Admin,Bots). IDOR rows exist for the component, note-component and modal routes. Both suites passed in the full run.

### Other findings

- **R3. MEDIUM (tests): the deep variants of three click races cannot run, and none of the click races are in the nightly.** With `RACE_SWEEP=1`:
  - `TestAClickRacingTheDisablingOfItsBot` (`components_races_integration_test.go:96`) and `...RegenerationOfTheBotsToken` (`:152`) fail with "want READY, got """. Each round redials the gateway, and the server's limits (30 upgrades a minute per IP, 20 IDENTIFYs a minute per bot) refuse it.
  - `...BanOfTheMemberWhoClicks` (`:164`) fails at "register banned9: 429 ... retry in 3600 s".
  - `sim-sweep.yml:358` runs `-run 'Parallel|BothSides|...'` deep, which picks up `TestParallelInstallsOfOneManifest` but none of the `TestAClickRacing*` tests and not the install-demotion race.

  So "the delete-wins branch shows in the deep sweep" is not exercised by CI. Fix: raise the limits through the test app (as `SetLimits` does for bots) or reuse the connection, and add the click races to the nightly regex.
- **R4. LOW: disabling a bot keeps its pending interactions.** Re-enabled within 15 minutes, it can still answer them: `TestReviewAnInteractionOfADisabledBotCannotBeAnswered` logs "answer after re-enable: 204". Regenerating a token deliberately keeps them too. Consider dropping a bot's interactions, notes and modals when it is disabled or deleted.
- **R5. LOW: ephemeral notes and modals are sent to the invoker without re-checking that the invoker can still view the channel** (`interactions_respond.go:282`, `interaction_callbacks.go:149`). A member who lost access within the 15 minutes can still get the bot's ephemeral text. This is the same as the earlier ephemeral replies.
- **R6. LOW: test gaps.** The canary world has no component canary: a canary string in a button label or select option would catch a future loader that bypasses Enrich. The crash-point list has no "install a bot" or "post a bot message with components".
- **R7. LOW (Go SDK): back-pressure turns into reconnects.** When the dispatch queue is full, the read loop blocks, so heartbeat ACKs are not read. After one interval the SDK closes as a zombie and resumes, and the resume replays the buffer. In my flood test this showed as the server's writes failing after about 41 s. Document that a handler must not block for longer than a heartbeat interval, or read ACKs on their own path.
- **R8. INFO: the author's TS retry-cap test hangs rather than fails when the cap is removed.** The node test runner kept going past 300 s despite `--test-timeout=20000`. Give the test its own `AbortSignal.timeout` so a regression fails fast.

### Gates run at 6cce1efec

**Pass:**
- `go build ./...`; `go vet ./...` and `go vet -tags=integration ./...`; `make staticcheck`; `make complexity-check`; `make test-scripts`.
- `go test -p 1 ./...`.
- The **full `go test -tags=integration ./internal/server`**: ok, 367 s.
- `make openapi-check`, `make openapi-diff`, `make contracts-check`, `make docs-check`; actionlint on all workflows.
- Web: `npm run lint`, `npm run typecheck`, vitest (693 files, 12383 tests), `npm run test:coverage` (gate passes, 98.0 % statements, no file under 80 %).
- TypeScript SDK: typecheck, build, test 72/72. Go SDK: gofmt, vet x2, `test -race`.
- `TestSDKContract` (both SDKs).
- The release pack rehearsal.
- **e2e `bot-platform.spec.ts` in Chromium**: 1 passed. It is the spec's only test: add link, confirm, button update, select, modal.

**Fail:** `make english-check` (R1) and `make coverage-go` (R2).

### Not checked
- e2e in WebKit and Firefox.
- The deep variants of the three click races, which cannot run (R3).
- The full nightly sweeps (sim, visibility at full depth, store-trace, plain).
- The Docker image build and smoke test, and the upgrade drill with 0106 on top of a real release.
- A real release run.
- CodeQL.

### Round-3 evidence
- `evidence/r2/zz_review_attacks_integration_test.go`: copy it into `internal/server`.
- `r2/*.log`: every gate, the full server suite (`server-integ.log`), the deep races (`races-deep.log`), coverage (`coverage.log`), e2e (`e2e.log`), contract (`contract.log`) and the pack rehearsal (`pack.log`).

---

## Round 4: head 9d5c8a665 (after 57d7d1e57)

**Head reviewed: 9d5c8a665** on origin/msch128/feat-bot-platform. Since 6cce1efec:
- 57d7d1e57: the English-only test string (R1).
- 9d5c8a665:
  - Interaction answers take `allowed_mentions`: callbacks 4 and 7, follow-ups, `@original`.
  - `flags` on callbacks 4 and 5 and on follow-ups; `@original` refuses flags.
  - `handle` in READY and `@me`.
  - `Interaction.user` is required in both SDKs.

I worked in a fresh worktree at `../mnema-talk-review-sdk`, read-only, and removed it at the end.

### Verdict on the whole branch: MERGE AFTER FIXES
- R1 is fixed: `make english-check` exits 0.
- The mention change is correct and additive. My parity test shows an interaction answer notifies exactly like a REST post of the same bot in every permission state I tried (details below).
- Two required checks are red:
  - **R2 (unchanged):** the Go per-file coverage floor.
  - **R9 (new):** the web compat test rejects `bot.Self` from every older server, because the new `handle` field is required.

Both are small fixes. Once they are fixed and the gates re-run on current main, I have nothing left that blocks.

### New blocker

**R9. BLOCKER (`frontend` check): `handle` is a required field, so the web compat suite fails.** `vitest run` gives 4 failures, all in `src/compat/responses.compat.test.ts`: "answers of server v1.0.0 / v2.0.0 / v2.9.12 / v2.9.18 > bot.Self decodes as sent by that release", with `ContractError: Invalid server response (bot.Self)`. At 6cce1efec the same suite passed in full (12383 tests).
- Cause: `Handle string json:"handle"` has neither `binding:"optional"` nor omitempty (`internal/bot/api.go`), so the generated guard requires it (`web/src/types/rest.ts:1248`, `hasOwn(value, "handle") && ...`).
- The web app itself never decodes `bot.Self`, so no screen breaks. It is still the class of break the compat suite exists to catch, and it turns `frontend` red.
- `make openapi-diff` does not flag it: a new required response field is not breaking for oasdiff.
- Fix: mark `handle` `binding:"optional"` (older servers do not send it), or register an older-server normaliser for `isBotSelf` in `shared/lib/compat/olderServers.ts`.
- Separately, both SDKs should tolerate a READY or `@me` without `handle` from a 3.0.x server. The TypeScript type says `string`, so an old server would give `undefined`.

### Still open

**R2 (unchanged): `make coverage-go` fails at 94.4 % total.** The same four files are under the floor with the same numbers: `store_overwrites_bot.go` 58.3 %, `botapi_edit.go` 74.4 %, `interaction_callbacks.go` 78.6 %, `components_store.go` 79.2 %. Nothing in 9d5c8a665 adds tests for the uncovered refusal branches. This coverage run also served as this round's full integration run: every package, `internal/server` included, passed (`ok ... internal/server 499.7s`).

### The mention change, looked at hard

What changed: `respond` (`internal/bot/interactions_respond.go`) used a fixed policy `{NoEveryone: true, CanView}`. It now uses `messages.BotMentionPolicy(snap, ch, p, m.allowed)`. Edits (callback 7, `@original`) pass the request's `allowed_mentions` to `editContext` (`internal/chat/botapi/botapi_edit.go`).

**It is the same check as a post, by construction:**
- A REST post (`botCreateMessage` -> `Posts.Post` -> `decide`, `service_gate.go:26-50`): for a bot, `perms = snap.Channel(bot, ch)` must pass `CheckChannel(SendMessages)`, and the mentions are `BotMentionPolicy(snap, ch, perms, am)`.
- An answer (`CheckBotSend`, `botapi.go:174-186`): exactly `snap.Channel(bot, ch)` plus `CheckChannel(SendMessages)` plus `ChannelForMessages`, then `BotMentionPolicy(snap, ch, p, am)` on the same snapshot and set.
- `snap.Channel` applies category and channel overwrites and then `hold`/`silence` (the timeout mask), so channel overwrites and timeouts act on both paths identically (`perm/snapshot_channels.go:114-123`).
- Members who cannot view the channel are dropped by the same `CanView` (`viewersOnly`).

**It is additive for existing bots:** `BotMentionPolicy(..., nil)` returns `viewersOnly(snap, ch)` = `{NoEveryone: true, CanView}`. That is field for field the old fixed policy, with no roles. A bot that sends no `allowed_mentions` behaves exactly as before.

**Proof through the real routes** (`evidence/r3/zz_review_mentions_integration_test.go`). Each case sends the same text through five routes: REST post, callback 4, follow-up, `@original` edit, and the first `PATCH` after callback 5. `max` can view the channel; `hidden` cannot.

| Case | All five routes |
|---|---|
| bot without *Mention everyone*, `@all` with `parse: ["everyone"]` | nobody notified |
| bot with *Mention everyone* (a role), `@all` with `parse: ["everyone"]` | `max` notified, `hidden` never |
| the same bot, `@all` **without** `allowed_mentions` (an existing bot) | nobody notified |
| `@max @hidden` without `allowed_mentions` | `max` notified, `hidden` never |
| *Mention everyone* denied to that role by a channel overwrite | nobody notified |

`TestReviewATimedOutBotCannotAnswerADeliveredInteraction`: the interaction was delivered first, then the bot was timed out. REST post 403, callback 4 403, callback 7 403. Follow-ups (ephemeral too) and the `@original` edit get 409, because the interaction was never answered. A timed-out bot also has its commands hidden: running one answers 404 "command not found".

**Low (R10): an interaction edit can add `@all` to an existing message, and a REST edit cannot.**
- `PATCH /api/bot/v1/channels/{id}/messages/{id}` still takes no `allowed_mentions` and so never notifies `@all`.
- Callback 7 and `@original` edits now can, for a bot that holds *Mention everyone* and asks for it.
- This is documented (`doc/bots.md`, "the new text of an update or an `@original` edit take `allowed_mentions` exactly like a post").
- It grants nothing new: the same bot can post `@all`. Edits recount mention badges (`Recount.Tell`) rather than pushing, and the badge of one message counts once.
- It does open a second, edit-based path that the 5-per-5-seconds post bucket does not cover. Consider giving the REST edit the same `allowed_mentions` for symmetry, or noting the asymmetry in the compatibility table.

### Other checks of 9d5c8a665
- **`flags`:** `@original` with `flags` gives 400 with the documented message (the author's test, and the routes in my parity test). Callback 5 with `flags: 0` and an ephemeral follow-up work.
- **`Interaction.user` required:**
  - TypeScript throws `TypeError('an interaction without member.user')` in the constructor, which runs inside the emitter's guarded listener. `evidence/r3/no_member.ts`: the bot survives, gets one `error` event, and no `interactionCreate` fires.
  - Go returns `errNoInteractionUser` from decoding, so the event is dropped.
  - The server sends `member.user` with every interaction it builds (`memberPayload`).
- **`handle`:** the integration test `TestTheBotLearnsItsHandle` passes (`bot-` prefix, display name kept as `username`). See R9 for the compat side.

### Gates at 9d5c8a665

**Pass:**
- `make english-check`; `go build`; `go vet` with and without `-tags=integration`; `make staticcheck`; `make complexity-check`; `make test-scripts`.
- `make openapi-check`, `make openapi-diff`, `make contracts-check`, `make docs-check`.
- Unit and integration tests of every package inside `make coverage-go`; only the floor failed.
- TypeScript SDK: typecheck, build, test 75/75. Go SDK: gofmt, vet x2, `test -race`.
- `TestSDKContract` (TypeScript 10/10, Go).
- Web lint and typecheck.
- e2e `bot-platform.spec.ts` in Chromium: 1/1.

**Fail:** `make coverage-go` (R2) and web vitest, 4 failures in the compat suite (R9).

**Not rerun at this head:** the release rehearsal (9d5c8a665 does not touch release.yml or the SDK packaging), the deep races (R3 unchanged), and web coverage.

### Round-4 evidence
- `evidence/r3/zz_review_mentions_integration_test.go`: copy it into `internal/server`. Both tests passed at 9d5c8a665.
- `evidence/r3/no_member.ts`.
- `r3/*.log`: `coverage.log` (it includes the full integration run), `webtest.log`, `contract.log`, `e2e.log` and the gate logs.

---

## Round 5 (final): head 2edd1175b

**Head reviewed: 2edd1175b** on origin/msch128/feat-bot-platform. Since 9d5c8a665: 57d7d1e57, fbd1a53ca, b4a09f8f2, cc784b2c5, 75967d7fc, 8d49554e4, 2edd1175b.
- I did not review the UI of 2edd1175b (another reviewer has it), except the BOT tag contrast, as asked.
- I worked in a fresh worktree at `../mnema-talk-review-sdk`, read-only, and removed it at the end.

### Verdict on the whole branch: MERGE

Every required gate I can run locally is green at 2edd1175b, including `make coverage-go`, which failed in rounds 3 and 4. Every finding from R1 to R10 is fixed. For each fix in code, I undid it and its test fails. The security review of rounds 3 and 4 still holds: my attack and mention-parity tests pass at this head.

What remains:
- Two low notes (L1, L2) that do not block.
- The checks I could not run here: CodeQL, the WebKit and Firefox e2e, the Docker image build and smoke test, the upgrade drill, and the full nightlies.

Per AGENTS.md, merge on current main with green CI on that exact state.

### R2 to R10: re-checked against the code, each with its fix undone

| ID | Fix at 2edd1175b | Fix undone -> test result |
|---|---|---|
| R2 coverage floor | `make coverage-go` **passes**: 94.4 % total, no file under 80 %. `store_overwrites_bot.go` 91.7 %; the other three files are over the floor | own-role guard `if !own` -> `if false`: `TestABotsOwnRoleIsTheOnlyRoleAManifestInstallCanAllowInAChannel` FAILS. Channel-scoped guard -> `if false`: same test FAILS |
| R3 deep races | `Gateway.SetLimits` and members created through SQL let the deep sweeps run. sim-sweep.yml's regex now includes `AClickRacing\|InstallRacing` | My run with `RACE_SWEEP=1`: install vs demotion 100/100 overlapped, 0 violated. Parallel installs 2x100, 0. Click vs delete 200/200, 0 (199 of 200 delivered first). Click vs disable 60, click vs token regeneration 60, click vs ban 30: all overlapped, 0 violated |
| R4 pending interactions | `ForgettingOnRevoke` wraps `gw.Revoke` in `OnRevoke` (`router_build.go`) and calls `ForgetBot`. It runs on disable, delete, token regenerate or revoke, and intent change | Without `c.ForgetBot(botID)`: `TestDisablingABotOrRegeneratingItsTokenEndsItsPendingInteractions` FAILS. My round-3 test now logs "answer after re-enable: **404**" (it was 204) |
| R5 notes and modals | `invokerSees` -> 403 "the member can no longer see this channel" in the ephemeral `respond`, the note edit and `answerModal` | Check bypassed: `TestANoteOrAModalNeedsAMemberWhoStillSeesTheChannel` FAILS ("ephemeral reply: got 204, want 403") |
| R6 test gaps | The canary world posts bot components with their own canary words in the open and Staff channels. The crash-point list has "install a bot from a manifest" and "post a bot message with components" | Both pass inside the full suite. The visibility of components still comes only through `messages.Enrich` |
| R7 Go back-pressure | When the queue is full, events are shed with a throttled warning instead of blocking the read loop; documented in README and doc.go | Shedding replaced by blocking: `TestAFloodTheHandlersCannotKeepUpWithIsShedAndTheConnectionStays` FAILS ("the client closed the connection"). See L1 |
| R8 TS test hangs | Each request in the test now has `AbortSignal.timeout(3000)` | `withinCap = true`: the test FAILS **after 4 s** (it hung over 300 s before) |
| R9 `handle` required | `handle,omitempty` with `binding:"optional"` on `@me` and READY; the generated guard is optional (`rest.ts:1248`); the SDK types allow it to be absent | Guard made required again: the compat suite FAILS 5 tests ("bot.Self decodes as sent by that release"). Restored: 1481/1481 |
| R10 REST edit mentions | `PATCH /channels/{id}/messages/{id}` takes `allowed_mentions`, and an edit carrying it takes one post from the per-channel bucket (`withinSendLimit`, `botapi_edit.go:103,245`) | Check removed: `TestABotEditTakesAllowedMentionsAndCountsAsAPostWhenItDoes` FAILS. See L2 |

### The points you asked about

**The install answer is read in the transaction** (`handler_install.go`):
- `readBack` loads the bot (`Get(ctx, tx, id)`) and its `user_roles` inside `installIn`, after the token and the audit entry.
- After the commit, the handler only invalidates caches (`Perms.Changed`, `membersChanged`) and writes the answer. Nothing that can fail runs between the commit and the only time the secret is shown.
- A commit that fails returns the error; the secret is never shown for an install that did not land.
- One small difference: `RoleIDs` is now ordered by id instead of the snapshot's order. That is cosmetic.

**`Gateway.SetLimits` is test-only** (`internal/gateway/gateway.go:162`):
- It has no caller outside `_test.go` files: grep over the non-test Go code finds only its definition, plus the similar existing `SetLimits` of webhooks and bot auth.
- No config field, no environment variable, no route.
- It is an exported method of an `internal` package, so only this module's code can reach it.
- It replaces the limiters without a lock, so it must not be called while the gateway serves. Tests call it at setup.

**R10:** see the table. The `send` middleware is the same bucket as `POST /channels/{id}/messages`. Without `allowed_mentions` an edit is not counted (it cannot notify `@all` then). See L2.

**BOT tag contrast** (`web/src/shared/components/BotBadge.vue`: `text-mnema-text` #E6EAE8 on `bg-mnema-band` #143D2E, border `mint/30`). There is one theme; tokens are from `web/src/style.css`. Computed with `evidence/r5/badge_contrast.py` (WCAG 2.x):

| Where | Text contrast |
|---|---|
| tag text on its own band: messages, replies, threads, profile, mention picker (normal and active), audit log | **9.96 : 1** (old tag: 5.31) |
| member list, offline rows at `opacity-75`, on raised | **6.59 : 1** |
| member list, offline rows at `opacity-75`, on hover | **6.48 : 1** |

- Everything is above 4.5 : 1 for 10 px bold text.
- The tag's edge against its surroundings is only 1.30 to 1.57 : 1. The tag carries its meaning in text, so 1.4.11 does not apply; it is a decoration.
- `MemberList.vue:93` is the only place that dims the tag.

### Low notes (not blocking)

- **L1 (Go SDK, by design): shedding drops any event,** `READY`, `RESUMED` and `INTERACTION_CREATE` included. A shed interaction leaves a member's click unanswered until it times out. Consider never shedding `INTERACTION_CREATE`, `READY` and `RESUMED`, or making the policy an option (block / shed).
- **L2: interaction edits with `allowed_mentions` are not counted against the post bucket.** `@original` `PATCH` and callback 7 have no send middleware (`internal/bot/interactions.go:67`); only the 50/s global limit applies. The effect is small: edits re-record mentions and recount badges, they do not push. But the symmetry with R10 is incomplete. Count interaction edits that carry `allowed_mentions` the same way.

### Gates at 2edd1175b

**All pass:**
- `make coverage-go`: unit plus integration of every package, `internal/server` included (490 s); gate passes.
- `make english-check`; `go build`; `go vet` with and without `-tags=integration`; `make staticcheck`; `make complexity-check`; `make test-scripts`.
- `make openapi-check`, `make openapi-diff`, `make contracts-check`, `make docs-check`; actionlint.
- Web: `npm run lint`, `npm run typecheck`, `npm run test:coverage` (12399 tests, gate passes).
- TypeScript SDK: typecheck, build, test 75/75. Go SDK: gofmt, vet x2, `test -race`.
- **Both SDK contract runs:** `TestSDKContract` TypeScript 10/10, Go pass.
- The deep races (above).
- e2e `bot-platform.spec.ts` in Chromium: 2/2.
- My earlier evidence tests: round-3 attacks 4/4, round-4 mention parity and timed-out bot 2/2.

**Not run:** the release rehearsal (release.yml and the packaging are unchanged since round 3), WebKit and Firefox e2e, the full nightly sweeps beyond the bot races, the Docker image build and smoke test, the upgrade drill, CodeQL. The UI of 2edd1175b went to another reviewer.

### Round-5 evidence
- `evidence/r5/badge_contrast.py`.
- `r5/*.log`: coverage (with the full integration run), deep races, the revert runs (`mut*.log`), contract, web, e2e and the gate logs.

---

## Short check: L1 and L2 at bf7afa85e (584fe36cb, bf7afa85e)

**Verdict: both fixes are correct, and each test fails with its fix undone. L1 leaves one low residue that does not block. MERGE stands.**

**L1 (the Go SDK never sheds READY, RESUMED or INTERACTION_CREATE):**
- The wait for a handler slot happens only inside the goroutine `dispatch` starts (`sdk/go/client.go:204-213`), never on the read loop.
- `TestReviewInteractionFloodWithABlockedHandler` (`evidence/r6/review_flood_test.go`) checks this: after 20000 INTERACTION_CREATE frames with a blocked interaction handler, the client still answers a server heartbeat request at once. So R7 does not come back.
- With `neverShed` emptied, `TestAnInteractionIsNeverShedEvenWhenTheQueueIsFull` fails ("timed out waiting for the interaction behind a full queue").
- **The residue (low):** the bypass has no bound of its own. The same flood takes goroutines from 11 to **20011**, each holding its payload, so a hostile or broken server can again grow memory through this one event type (G3 for INTERACTION_CREATE only).
- The real server caps pending interactions at 2000 per bot, so an honest server cannot cause it.
- Smallest fix: give the never-shed events their own bound, for example 2048, matching the server's per-bot cap. Beyond it, drop with an error-level log or close the connection.

**L2 (callback 7 and `@original` edits that carry `allowed_mentions` take from the post bucket):**
- `AllowSend` (`internal/bot/authn.go`) is the same per-channel bucket as `LimitSends`, and it refunds the global request on refusal.
- The order is right: in `callback` the limit is taken before `in.op` and before `answer`, so a 429 consumes nothing, and the interaction can still be answered. In `editOriginal` it is taken after `originalOf`, which only reads, and only for an edit, not for the deferred post.
- With the limit check disabled, `TestAnUpdateOrOriginalEditThatMayNotifyCountsAsAPost` fails: "update that may notify, over the limit: got 204, want 429".
- With the order broken (the answer consumed before the limit), the same test fails: "plain update: got 409 ... already has a response". So the test guards the order too.
- A minor point: an update that passes the limit and then fails for another reason (already answered) spends its bucket token without a post. That is harmless.

**Gates run at bf7afa85e:**
- Go SDK: gofmt, vet, `test -race`.
- gofmt of the changed packages, `go vet -tags=integration` of internal/bot and internal/server.
- `make staticcheck`, `make complexity-check`, `internal/repo`, `make english-check`, `make openapi-check`.
- The two touched integration tests.

All pass. I did not run a full gate round, as you asked for a short check. The worktree is removed.

---

## Last delta: head 430fd7d90 (82c80dce1, 835cdbdef, 430fd7d90)

**Verdict: MERGE stands.** Both changes are correct and each one's test fails with the fix undone. Two low notes, both test or placement gaps; neither blocks.

**1. The L1 bound in the Go SDK (2048 in flight, then `errUnshedOverflow`):**
- **The seq is kept only after the hand-off** (`sdk/go/gateway_conn.go`: `d.dispatch` returns first, then `updateSession(seq)`), so the resume asks for the overflowing event again.
  - `TestAnInteractionFloodBeyondTheBoundIsResumedNotDroppedOrGrown` checks that the resume is at seq 2049, the last event handed on.
  - With the seq stored before the dispatch, it fails ("resumed at seq 2050, want 2049").
  - With an unbounded lane (the bound removed), it fails ("timed out waiting for a gateway connection") because no resume happens.
- **No interaction is lost through this path.** `evidence/r7/review_resume_loop_test.go` keeps the handlers stuck: 101 resumes in 2 s, all at seq 2049, and the event is never skipped. The only losses are the server's limits, which apply whatever the SDK does: a resume window over 2 minutes, or more than 512 buffered events, ends in INVALID_SESSION and a new session.
- **The loop exists, but it is bounded.** While handlers stay stuck, every connection reaches RESUMED and is marked healthy, so the backoff resets to its minimum: 0.5 to 1 s with the real defaults (the 101 resumes above used the test's 5 to 20 ms backoff). After 20 resumes a minute the server answers 4008, and the client then waits at least 30 s. That is reconnect churn, not a CPU loop, and it ends when a handler returns.
- **Low:** do not count a connection that ended in `errUnshedOverflow` as healthy, so the backoff grows to its 60 s cap instead.

**2. `interaction_deferred {bot_id, channel_id, interaction_id}`:**
- **Recipients:** it goes only to the member who caused the interaction, to all of their sessions (`SendToUsers([]uuid.UUID{in.invoker}, ...)`, `interaction_callbacks.go:187`), on callback 5 or 6 only. My negative test (`evidence/r7/zz_review_deferred_integration_test.go`) passes: the Owner, watching the same channel, gets nothing.
- **What it reveals:** nothing new. The three ids are the bot, the channel and the interaction the member's own run or click was answered with. A member who lost access to the channel within the 15 minutes still gets the bare acknowledgement, but no content.
- **Tests:** without the event, `TestADeferralTellsTheMemberTheBotIsOnIt` and `TestWireFormatOfBotComponentEvents` fail. **Low gap:** with the event broadcast to the channel instead, every test of the branch still passes. Only my negative test catches it. The wire golden records the payload and the call site, not the recipients, and the canary world has no deferral. Add a negative test like mine, and ideally a deferral to the canary world.
- **Rule 10:**
  - The name is a constant (`events.InteractionDeferred`) and the event is published as a named type.
  - It is in `internal/events/registry`, in `events.golden.json`, and in the regenerated `api/events.json` and `eventShapes.ts`.
  - It has its web type and guard (`web/src/types/botEvents.ts`, without `onlyFields`, so new fields will not be refused).
  - `go test ./internal/events/...` and `make contracts-check` pass.
  - **One deviation (low):** the type has plain fields only (three UUIDs), so rule 10 places it in `internal/events/wire`, not next to `InteractionResponse`/`InteractionModal` in `internal/bot`. Those two carry domain types; this one does not.

**Gates run at 430fd7d90 (a short check):**
- Go SDK: gofmt, vet, `test -race`.
- `go test ./internal/events/...`, `internal/repo`, `make staticcheck`, `make complexity-check`, `make english-check`, `make openapi-check`, `make contracts-check`.
- Web lint and typecheck, and the web tests of types, chat interactions and message components (2127).
- The touched integration tests.

All pass. I did not rerun the WebKit e2e that 430fd7d90 adds to CI, nor a full gate round. The worktree is removed.
