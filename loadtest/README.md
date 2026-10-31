# Load test

A reproducible regression check for the voice and screen-share path: one member
shares a screen in a Talk, real browsers and lightweight Pion clients watch it,
and the report fails on stalls, members that never connect, join errors, server
heap growth and goroutine leaks.

Two ways to run it: `make load-test` below drives real browsers from a
development machine; [On the host](#on-the-host-capacity-test-without-browsers)
runs a full capacity test, chat and voice, on the server host itself with
static binaries and one script, no browsers, Node or Python needed.

```sh
make load-test                 # PROFILE=smoke, about 2 minutes plus build
PROFILE=full make load-test    # 1 sharer + 15 browsers + 84 viewers = 100 members
SKIP_BUILD=1 PROFILE=join make load-test
```

Needs Docker (throwaway PostgreSQL and SeaweedFS from `e2e/docker-compose.yml`,
own compose project `mnema-loadtest` and ports, so it does not collide with
`make e2e`), Go, Node 22+ and the Chromium of `e2e/`
(`LOADTEST_INSTALL_BROWSER=1` runs `playwright install --with-deps chromium`).
`LOADTEST_UDP_PORT`, `LOADTEST_APP_PORT`, `LOADTEST_PG_PORT` and `LOADTEST_S3_PORT`
move the ports when something else on the machine uses one.

## What runs

| Part | What |
|------|------|
| `run.sh` | Orchestrator: builds, starts the stack and the server with random secrets and a `METRICS_TOKEN`, seeds users and a Talk, mints session tokens, runs the scenarios, samples `/api/metrics` every 10 s, prints the report, tears everything down. |
| `browsers.mjs` | Sharer + N real Chromium members (Playwright from `e2e/`). Media is sealed and opened with the MLS keys exactly as in production. Join time, time to first decoded frame, fps. |
| `viewer/` | Own Go module (does not touch the main module's coverage or govulncheck; all Go tools live in it). Lightweight Pion members: WebSocket signalling, answers the SFU's offers, subscribes to the screen, counts RTP bytes. They hold no MLS keys. Optional packet loss and jitter. It can also publish synthetic VP8 and Opus (a screen, cameras, speakers), connect through the built-in TURN relay, and judge the run against thresholds. |
| `join.mjs` | Video stalls (more than 1 s without a decoded frame) while members join: sequential, parallel, and sequential in descending user-id order. |
| `reload.mjs` | Members reload in turn; after each reload everyone must hear the others again. |
| `mint.mjs` | Mints HS256 session tokens with the claims of `auth.IssueToken`. |
| `report.mjs` | Summary and thresholds; exit code 1 on a violation. |
| `viewer/cmd/chat` | Chat load test (Go, static). N WebSocket sessions, a share writes, all read; delivery latency, loss, server CPU and memory. |
| `viewer/cmd/mint` | `mint.mjs` in Go, for hosts without Node. |
| `host-run.sh` | Orchestrator for the server host: snapshot, seed, mint, run the scenarios, clean up, verify. |

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
bitrates), and the default `auto` lets the server choose: it starts on the best
layer and moves viewers down when their receiver reports show trouble, so
`LOSS=0.15 WIDTH=1280 make load-test` ends with all viewers on the smaller
layers (compare their `video_bytes` in `viewers.json`). The browsers decode
whichever layer the server gives them.

**Bad networks.** `LOSS=0.05 JITTER_MS=20 make load-test` makes the Pion viewers
drop 5 % of incoming RTP packets before Pion's NACK generator (so NACK and
retransmission paths run) and delay each packet by up to 20 ms. Browsers stay on
clean loopback.

**Slow links.** `BW_KBPS=1500 QUALITY=auto make load-test` gives each Pion
viewer its own 1.5 Mbit/s link with a 1.5 s queue (`BW_QUEUE_MS`) in front of
it. Nothing is lost while the queue fills, so only the bandwidth estimate (and
later the round trip time) can tell the SFU that layer 0 does not fit: the
viewers should end on the layer that does, with `mnema_sfu_bandwidth_layer_moves_total`
counting the moves.

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

## On the host: capacity test without browsers

`host-run.sh` runs the whole capacity test on the machine that hosts the
server (a NAS, say), against its docker compose deployment, so the numbers do
not depend on a laptop or the network between. Everything the host needs is
`bash`, `docker`, `curl` and four static Go binaries; nothing is installed.

| Scenario | What it does | Users |
|----------|--------------|-------|
| `chat100`, `chat300`, `chat1000` (any `chat<N>`) | N WebSocket sessions in one text channel; 10 % of them write 0.2 messages/s each with typing indicators, 5 % of the messages get a reaction and 3 % an edit; everyone reads | N |
| `voice` | 1 shared screen (synthetic VP8, 1500 kbit/s, 15 fps) watched by 99 members, direct | 100 |
| `relay` | the same screen and 3 speakers, but every member allowed relayed candidates only, so all media goes through the built-in TURN relay | 100 |
| `mixed` | 1 shared screen, 10 cameras (300 kbit/s, 24 fps), 3 speakers, every one of the 100 members watching the screen and all cameras | 100 |
| `voice:N:K:C:S[:relay]` | any shape: N members, of which K speakers, C cameras and S screens publish (K+C+S <= N); `:relay` sends everything through the TURN relay. `voice:100:3:10:1` is `mixed`, `voice:50:3:4:1:relay` a relay run with 4 cameras, `voice:25:3:0:0` audio only; plain numbers, N at least 2 | N |

### Build and copy

```sh
make load-test-host        # bin/loadtest-host/mnema-loadtest-linux-{amd64,arm64}.tar.gz
scp bin/loadtest-host/mnema-loadtest-linux-amd64.tar.gz <host>:     # arm64 for an ARM NAS
```

On the host: `tar xzf mnema-loadtest-linux-amd64.tar.gz && cd mnema-loadtest-linux-amd64`.
It holds `host-run.sh` and `bin/loadtest-{mint,chat,voice}`.

### Run

```sh
./host-run.sh --list               # the scenarios with users and expected run time
./host-run.sh                      # all six, in order, about 40 minutes
SCENARIOS="chat300 mixed" ./host-run.sh
```

The defaults fit the compose file of this repository: containers `mnema-talk`
and `mnema-postgres` (`APP_CONTAINER`, `DB_CONTAINER`), the app's published
port for `BASE_URL`, and `JWT_SECRET`, `METRICS_TOKEN`, `PUBLIC_URL` and the
database user and name read from the app container's configuration. Set any of
them in the environment to override. Results go to `./loadtest-results/<time>/`
(`RESULT_DIR`): per scenario `<name>.json`, `<name>.log`,
`<name>.docker-stats.tsv`, plus `summary.json` and the table fingerprints.
Nothing in it is a secret.

Run it when nobody else uses the instance (the clean-up check compares the
whole database), and take your normal backup first anyway: the script makes its
own `pg_dump` but that is a safety net, not a backup plan.

The exit code is 0 when every scenario met its thresholds, 1 when one failed,
2 for a setup error, 3 when the database does not match the snapshot after the
clean-up (the dump is kept, see below), 130 after Ctrl-C.

### Expected run time

Connecting is the slow part, because the server limits one address to 120
WebSocket upgrades a minute (the chat test connects 100 a minute) and the
relay to 120 allocations a minute. Add 30 s between scenarios, a few seconds for
the snapshot of a small database, and the clean-up.

| Scenario | Connect | Hold | Total |
|----------|---------|------|-------|
| `chat100` | 1 min | 2 min | about 4 min |
| `chat300` | 3 min | 2 min | about 6 min |
| `chat1000` | 10 min | 2 min | about 13 min |
| `voice` | 30 s | 2 min | about 3 min |
| `relay` | 60 s | 2 min | about 4 min |
| `mixed` | 30 s | 2 min | about 3 min |
| all six | | | about 40 min |

`CHAT_HOLD_S` and `VOICE_HOLD_S` change the hold; a longer hold (10 min) is
better for judging memory growth.

### What it does, in order

1. **Snapshot**: `pg_dump` to a temp file (mode 0700 directory), the count and
   an md5 of every row of every table, and the sequences.
2. **Seed**: the users `loadtest-NNNN` (unusable password hash, rules
   accepted), a voice Talk `Loadtest` and a text channel `Loadtest Chat`, by
   SQL in the database container; the same method as the manual procedure under
   [Remote mode](#remote-mode).
3. **Tokens**: HS256 session JWTs minted on the host from `JWT_SECRET`
   (`bin/loadtest-mint`) into a 0600 file that lives for the run.
4. **Scenarios**, one after the other with `PAUSE_S` (30) between them. During
   each, the tool samples `/api/metrics` every 5 s (CPU seconds, resident memory,
   heap, goroutines, counters) and the script samples `docker stats` of the app
   and database containers.
5. **Clean-up**, on success, on an error and on Ctrl-C: the audit log rows the
   test users wrote, the two channels and the users are deleted (rows that
   reference them cascade), the sequences that moved are set back, and the
   fingerprint is compared with the snapshot. Identical: the tokens and the dump
   are removed. Different: the differing tables are listed, the dump and the
   directory stay for you (`pg_restore` restores it), and the exit code is 3. A
   table that changes by itself while the test runs (a real user browsing
   updates `users.last_seen_at`, for instance) can differ legitimately;
   `FINGERPRINT_SKIP="table1 table2"` leaves tables out.

Secrets (`JWT_SECRET`, `METRICS_TOKEN`, the tokens) are read into shell
variables or the 0600 file, passed to the tools in their environment, and never
printed or written to the results; do not run the script with `bash -x`. If a
run is killed with SIGKILL nothing is cleaned up: delete the users
`loadtest-%`, the channels `Loadtest` and `Loadtest Chat` and the audit rows of
those users by hand (the script refuses to start while `loadtest-*` users
exist).

### What is measured

**Chat** (`bin/loadtest-chat`). Each message carries a marker with a sequence
number and is timed on one clock, from the start of the REST request to the
moment a session reads the `message_create` event, once per receiving session;
edits (`message_update`) and reactions (`message_reaction`) the same way. So
the percentiles are over sessions x messages. Loss is the share of expected
deliveries that never arrived (every session connected at send time is
expected to get every event; duplicates are counted apart). Also: WebSocket
errors and reconnects, requests that failed with the first server answer as a
sample, typing indicators sent and received, the connect time, the server's CPU
and memory, and the CPU the generator itself used. It uses the web client's
protocol: the WebSocket at `/api/ws` for everything pushed and for typing, REST
for send, edit and react, and loads the channel history on connect.

Rate limits are respected, not circumvented: the server allows 600 API requests
a minute per account, 120 WebSocket upgrades a minute per address and 30 per
account; the tool refuses a `RATE` that would exceed the first and paces the
connects (`RAMP_PER_MIN`, default 100). To connect faster, run the tool where
the server sees a trusted proxy address and set `SPOOF_XFF=1`, which gives every
session its own `X-Forwarded-For`; that only works if the sender is in
`TRUSTED_PROXY_CIDRS` or on loopback.

**Voice** (`bin/loadtest-voice`, the Pion viewer). Headless members join the
Talk over the WebSocket and answer the SFU's offers. Publishers attach synthetic
VP8 (a valid payload descriptor and keyframe bit, random bytes sized to the
bitrate, keyframes every 3 s and on a PLI) to the screen or camera line, and
20 ms Opus-sized packets to the microphone line; no device is touched and
nothing is played. Every member subscribes to the screens. Cameras reach
everybody by default, as in the web client. `WATCH_CAMERAS=<n>` keeps only n
of them visible per member (the others are opted out one by one, and the
visible ones are spread over the members like different people's tiles),
`WATCH_CAMERAS=none` hides all. (Before this option existed `WATCH_CAMERAS=1`
meant "all cameras"; it now means one camera, and "all" is the default.) The
camera choice is sent when the member joins and once more when its connection
comes up, not repeatedly, because every subscription makes the server
renegotiate with the room. Per class (screen, camera, audio) the result
has, per receiving member, the received bitrate (average, minimum, maximum),
the packet loss that nothing repaired, and freezes (a video gap above
`FREEZE_MS`, 1000), plus the connect time, time to first video, and the same
server numbers as the chat test. `RELAY=1` fetches the ICE servers from
`/api/webrtc/config` as the web client does and allows relayed candidates only;
the result counts how many members really connected through the relay (all of
them must). `RELAY_HOST=<address>` replaces the host of the TURN URLs when the
host cannot reach its own public address.

The built-in relay limits what one address can do: 120 allocations a minute
(burst 60), 256 allocations in total, 4 per member. `RELAY_MEMBERS` therefore
tops out below 256 from one host, and members join 600 ms apart.

### Origin, webhooks and other side effects

- **Origin.** The server compares the `Origin` header of WebSocket upgrades and
  writes with its `PUBLIC_URL` (or `CORS_ALLOWED_ORIGINS`). `host-run.sh` takes
  that from the app container and passes it as `ORIGIN`, so a run against a
  loopback `BASE_URL` works behind an https `PUBLIC_URL`. When you run a tool by
  hand against such a server, set `ORIGIN` yourself; the default is the origin of
  `BASE_URL`.
- **Webhooks and bots.** Events of the test channels reach every enabled
  outgoing webhook and every bot that listens, as real requests to their URLs.
  The script refuses to start while one exists; `ALLOW_WEBHOOKS=1` overrides
  that. Their queue rows for the test channels are removed in the clean-up.
- **Members see it.** While the test channels exist, real members can see them
  and get unread badges and, with Web Push enabled, notifications for the
  messages; run it when nobody is on.
- **Ctrl-C.** The first one stops the tools and cleans up; further ones are
  ignored until the clean-up is done (tools still running after 5 s are
  killed). Channels are created and deleted by their ids, and the script
  refuses to start when channels named `Loadtest` or `Loadtest Chat` exist.

### Thresholds

The tools exit 1 when one is violated (the voice tool when `EVALUATE=1`, which
the script sets). Override with the environment variable of the same name.

| Chat | Default | Meaning |
|------|---------|---------|
| `MAX_LOSS_PCT` | 0.1 | lost deliveries, per kind (message, edit, reaction) |
| `MAX_P95_MS`, `MAX_P99_MS` | 500, 1500 | delivery latency |
| `MAX_SEND_ERROR_PCT` | 1 | failed send requests |
| `MAX_WS_ERRORS`, `MAX_RECONNECTS`, `MAX_NEVER_CONNECTED` | 0 | sessions |
| `MAX_GOROUTINE_LEAK` | 30 | server goroutines after everyone left, against before |
| `MAX_CPU_CORES`, `MAX_RSS_MB` | off | server CPU (hold average) and resident memory |

| Voice | Default | Meaning |
|-------|---------|---------|
| `MAX_NEVER_CONNECTED`, `MAX_FAILED`, `MAX_FLAPS` | 0 | members that never reached ICE connected, failed signalling, connection drops |
| `MAX_NEVER_RECEIVED` | 0 | members that should have got a class of media (not their own) and did not |
| `MAX_LOSS_PCT` | 1 | unrepaired packet loss, per class |
| `MAX_FROZEN_PCT` | 5 | members with a video freeze, per class |
| `MIN_BITRATE_PCT` | 50 | received bitrate per track against the published one |
| `MAX_GOROUTINE_LEAK` | 30 | as above |
| `MAX_CPU_CORES`, `MAX_RSS_MB` | off | as above |

Relay mode also fails when a connected member did not use the relay.

### Where packets go missing, kernel counters and profiles

Every voice result carries a **packet split** (`packet_stages` in the JSON,
the `packets` lines in the log) that follows the packets through the three hops:

1. *into the SFU*: packets the publishers wrote against the server's
   `mnema_sfu_ingress_packets_total`;
2. *fan-out*: what the SFU should have written (the packets that reached it,
   times the members watching each track) against
   `mnema_sfu_egress_packets_total`;
3. *out of the SFU*: egress against the packets the generator's receivers read.

The hop with the missing share is where loss starts: the first points at the
generator or its socket, the second at the SFU falling behind, the third at
the path back or the generator's own CPU. The split needs `METRICS_TOKEN`.

All numbers cover the same window: it starts `WINDOW_WARMUP_S` (10) seconds into
the hold, when every member has had time to join, and ends with the hold; the
generator's counters and the server's counters are read at the same two
moments. Only members that were connected at both ends count as audience, for
the expected egress as well as the received packets. What bias remains:
packets in flight at either end (a fraction of a second of traffic),
retransmissions (the SFU and the receivers count them like any packet, so a
surplus is never shown as negative loss), and packets the SFU drops on purpose
(its ingress rate limit, a viewer waiting for a keyframe), which show as missing
at the hop where they are dropped. Read a small percentage at one hop as noise,
and compare hops with each other.

After each scenario the script prints and stores (`"kernel"` in the scenario
JSON, a `kernel:` line in the log) the growth of the UDP counters `InErrors`,
`RcvbufErrors` and `SndbufErrors` from `/proc/net/snmp`, on the host and inside
the app container, and of the app container's cgroup `memory.events` (`max`,
`oom`, `oom_kill`; cgroup v2). A non-zero receive-buffer count means packets
were dropped by the kernel before the program read them; a non-zero `max` or
`oom` means the memory limit was hit. Counters that cannot be read show `n/a`.

`PPROF=1 ./host-run.sh` also captures, during the hold of each scenario, a 30 s
CPU profile, `allocs` and `heap` of the server (`<scenario>.pprof-{cpu,allocs,heap}`,
open with `go tool pprof`). The server serves them on the address in its
`PPROF_ADDR` (added to the server together with this option; the profiles need
a server that has it), which listens on loopback inside the container, so the
script fetches them with `wget` in the container; without `PPROF_ADDR` it says
so and captures nothing. The CPU profile needs a hold of at least 60 s; with a
shorter hold it is skipped, the log names it, and allocs and heap are still
captured.

### Reading the numbers

- **Server CPU is the app process in cores** (`process_cpu_seconds_total` over
  the loaded phases; `cpu_cores_hold` is the steady phase alone). On a 4-core
  NAS 4.0 is everything. The database's share is in `docker stats`.
- **The generator shares the host.** `generator_cpu_cores` is what the tool
  itself used. Chat at 1000 sessions parses every event a thousand times; when
  the generator takes a large part of the cores, the latency it reports includes
  its own queueing. Lower `CHAT_WRITER_PCT`/`CHAT_RATE` or read the result as a
  worst case.
- A scenario that fails thresholds still fills in its numbers; the capacity is
  the largest run that passes.

### Other knobs

`CHAT_HOLD_S` (120), `CHAT_WRITER_PCT` (10), `CHAT_RATE` (0.2),
`CHAT_RAMP_PER_MIN` (100), `VOICE_HOLD_S` (120), `VOICE_MEMBERS`,
`RELAY_MEMBERS`, `MIXED_MEMBERS` (100), `MIXED_CAMERAS` (10),
`MIXED_SPEAKERS` (3), `WATCH_CAMERAS` (all, a number, or none), `PPROF` (0). The tools take everything else from the environment of
the script: `REACT_PCT`, `EDIT_PCT`, `MSG_BYTES`, `TYPING=0` for chat;
`SCREEN_KBPS`, `SCREEN_FPS`, `CAMERA_KBPS`, `CAMERA_FPS`, `KEYFRAME_S`,
`QUALITY`, `LOSS`, `JITTER_MS`, `BW_KBPS` for voice (see the header of
`loadtest/viewer/main.go` and `loadtest/viewer/cmd/chat/main.go`). `ABORT_ON_FAIL=1`
stops at the first failed scenario.

The tools also run on a development machine against a throwaway stack; the
script's own end-to-end test is a compose project with the repository's
`docker-compose.yml`, a local image and `APP_CONTAINER`/`DB_CONTAINER` set to
its container names.

## Notes on the Pion viewer

It queues remote ICE candidates until the SFU's offer is applied and its own
candidates until the answer is sent. Without that, a candidate can reach Pion
before `SetRemoteDescription` (`AddICECandidate` then fails) or reach the server
before it has the answer (it is dropped). `LEGACY_SIGNALLING=1` switches the
queues off to reproduce the old behaviour for comparison.

### When viewers never connect

`viewers.json` and the console list every member that did not reach ICE
connected: a timeline of its state changes with times, the candidates it
gathered and the ones the server sent (and whether Pion accepted them), and
Pion's candidate pair statistics (requests and responses per pair). A member
that is still not connected after `ICE_TIMEOUT_S` (default 25) is recorded as
failed with that data and closed; the result is written before the members are
closed, and closing is bounded by `CLOSE_TIMEOUT_S` (default 10), so a stuck
connection cannot hide the summary.

`ICE agent unresponsive ... goroutines blocked in a UDP write` means the
viewer's own ICE agent is frozen, not the server: Pion sends its connectivity
checks from inside the agent's single task loop, so one `sendto` that the OS
refuses (EAGAIN) stops the whole agent for good, and `Close` then waits
forever on it. This showed on a multi-homed Mac, where the OS stopped
accepting UDP datagrams from a socket to private-network addresses after a
fixed number of them, which a viewer with several interfaces reaches within
about a second when none of its first pairs is answered. Such a viewer
connects in under a second or never. `ICE_INTERFACES=en0` (or any other
interface list) and `ICE_UDP4_ONLY=1` restrict what the viewer gathers, to
compare a multi-homed host with a single-homed one.
