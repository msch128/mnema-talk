# Load test

A reproducible regression check for the voice and screen-share path: one member
shares a screen in a Talk, real browsers and lightweight Pion clients watch it,
and the report fails on stalls, members that never connect, join errors, server
heap growth and goroutine leaks.

```sh
make load-test                 # PROFILE=smoke, about 2 minutes plus build
PROFILE=full make load-test    # 1 sharer + 15 browsers + 84 viewers = 100 members
SKIP_BUILD=1 PROFILE=join make load-test
```

Needs Docker (throwaway PostgreSQL and SeaweedFS from `e2e/docker-compose.yml`,
own compose project `mnema-loadtest` and ports, so it does not collide with
`make e2e`), Go, Node 22+ and the Chromium of `e2e/`
(`LOADTEST_INSTALL_BROWSER=1` runs `playwright install --with-deps chromium`).

## What runs

| Part | What |
|------|------|
| `run.sh` | Orchestrator: builds, starts the stack and the server with random secrets and a `METRICS_TOKEN`, seeds users and a Talk, mints session tokens, runs the scenarios, samples `/api/metrics` every 10 s, prints the report, tears everything down. |
| `browsers.mjs` | Sharer + N real Chromium members (Playwright from `e2e/`). Media is sealed and opened with the MLS keys exactly as in production. Join time, time to first decoded frame, fps. |
| `viewer/` | Own Go module (does not touch the main module's coverage or govulncheck). Lightweight Pion members: WebSocket signalling, answers the SFU's offers, subscribes to the screen, counts RTP bytes. They hold no MLS keys. Optional packet loss and jitter. |
| `join.mjs` | Video stalls (more than 1 s without a decoded frame) while members join: sequential, parallel, and sequential in descending user-id order. |
| `reload.mjs` | Members reload in turn; after each reload everyone must hear the others again. |
| `mint.mjs` | Mints HS256 session tokens with the claims of `auth.IssueToken`. |
| `report.mjs` | Summary and thresholds; exit code 1 on a violation. |

## Profiles

| `PROFILE` | Scenario | Defaults |
|-----------|----------|----------|
| `smoke` (default) | `watch` | sharer + 3 browsers + 10 viewers, hold 30 s |
| `full` | `watch` | sharer + 15 browsers + 84 viewers, hold 120 s |
| `join` | `join` | 6 members; sequential, parallel, descending ids |
| `reload` | `reload` | 2 members, each reloads once |
| `long` | `watch` | like `full`, hold 2 h (`HOLD_S=7200`): memory and goroutine trend |

Every knob can be overridden from the environment: `BROWSERS`, `PION`, `HOLD_S`,
`MEMBERS`, `SCENARIOS` (space separated: `watch join reload`), `WIDTH`, `FPS`, `QUALITY`,
`BATCH`, `BATCH_MS`, `PER_BROWSER`, `SPACING_MS` (pause between two viewer
joins), `GAP_MS`.

**Layers.** A screen share is sent in up to three layers when the sharer's
capture is larger than 480p, so `WIDTH=1280` (a 720p share, layers 720p, 480p,
360p) exercises the SFU's layer switching. `QUALITY=2 make load-test` makes
every Pion viewer ask for the smallest layer, `QUALITY=spread` spreads them over
the layers by index (`avgKbps` of the viewers then shows the different
bitrates), and the default `auto` takes the best layer. The browsers decode
whichever layer the server gives them.

**Bad networks.** `LOSS=0.05 JITTER_MS=20 make load-test` makes the Pion viewers
drop 5 % of incoming RTP packets before Pion's NACK generator (so NACK and
retransmission paths run) and delay each packet by up to 20 ms. Browsers stay on
clean loopback.

**A single laptop is the limit of `full`.** The server, 16 Chromium processes
and 84 viewers share the machine; on 10 cores the sharer's bitrate ramps up for
a while and viewers see their first frame late. Use the same profile against a
separate server (remote mode) for capacity numbers, and treat local `full`
results as a regression signal only.

## Results and thresholds

Each run writes `loadtest/results/<time>-<profile>/` (gitignored): JSON per
scenario (`browsers.json`, `viewers.json`, `join-*.json`, `reload.json`), logs,
`metrics.tsv` (epoch, phase, goroutines, heap, sys) and `summary.json`. Tokens
of the local stack are deleted at the end; never commit anything from that
directory.

Thresholds (environment): `MAX_STALL_S` (3), `MAX_FIRST_FRAME_MS` (15000),
`MAX_HEAP_GROWTH_PCT` (50, last vs first third of the hold phase; judged from
60 s of hold), `MAX_GOROUTINE_LEAK` (30, goroutines after everyone left vs
before the run), `MAX_NEVER_CONNECTED` (0), `MAX_VIEWER_STALL_MS` (30000).
`node loadtest/report.mjs` re-reads a result directory (`RESULT_DIR=...`).

## Remote mode

To test a real instance (for example a NAS) set `BASE_URL`, `TOKENS`
(JSON file `[{"id": "...", "token": "..."}]`, the first entry is the sharer) and
`CHANNEL_ID`; `CHANNEL_NAME` (default `Loadtest`) is the Talk's name for the
browsers and `METRICS_TOKEN` enables the memory and goroutine checks. No
infrastructure is started and nothing is seeded:

```sh
BASE_URL=https://talk.example.org TOKENS=./tokens.json CHANNEL_ID=<uuid> \
  METRICS_TOKEN=<token> PROFILE=full make load-test
```

Certificates are verified (browsers and the Pion viewers). For an instance with a
self-signed certificate, `LOADTEST_INSECURE_TLS=1` makes the browsers accept it;
the tokens are session cookies, so only use it on networks you trust. The Pion
viewers always verify, so use a trusted certificate or plain `http://` on a
trusted network for them.

Preparing, and afterwards cleaning up, an instance:

1. Take a snapshot or backup first (`scripts/backup.sh`), and note a per-table
   fingerprint, for example `select count(*), md5(string_agg(t::text, '' order
   by t::text)) from <table> t` for `users`, `channels`, `messages`.
2. Create N users with an unusable password hash (nobody can log in as them) and
   one voice Talk, with psql in the database container:

   ```sql
   insert into users (username, display_name, password_hash)
   select format('loadtest-%s', lpad(i::text, 3, '0')),
          format('Load %s', lpad(i::text, 3, '0')), '!loadtest-no-login'
   from generate_series(0, 99) i;
   insert into channels (category_id, name, type, sort_order)
   select id, 'Loadtest', 'voice', 99 from categories limit 1;
   select id, token_version from users where username like 'loadtest-%' order by username;
   select id from channels where name = 'Loadtest';
   ```

3. Mint the tokens from the instance's `JWT_SECRET` (the second query's output,
   one `id,token_version` per line):
   `JWT_SECRET=... node loadtest/mint.mjs < ids.csv > tokens.json`. Tokens are
   valid sessions: keep the file out of the repository and delete it afterwards.
4. Run the test.
5. Clean up: delete the Talk and the `loadtest-*` users (rows that reference a
   user cascade), then `alter sequence channels_number_seq restart with <next
   number before the test>`, and compare the fingerprints from step 1.
   Log in as an admin once and check that no `loadtest-*` user is left in the
   member list.

## Notes on the Pion viewer

It queues remote ICE candidates until the SFU's offer is applied and its own
candidates until the answer is sent. Without that, a candidate can reach Pion
before `SetRemoteDescription` (`AddICECandidate` then fails) or reach the server
before it has the answer (it is dropped). `LEGACY_SIGNALLING=1` switches the
queues off to reproduce the old behaviour for comparison.
