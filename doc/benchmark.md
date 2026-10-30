# Benchmark

A capacity measurement of Mnema Talk 2.2.0 on a small home server. It answers
one question: how much of a low-power machine do chat and voice use at a given
number of members? It is a single run on a single host, so read the figures as
orders of magnitude, not as a guarantee.

## Test system

| | |
|---|---|
| Server version | 2.2.0 (the published container image) |
| CPU | Intel N100, 4 cores, no hyper-threading |
| Memory | 16 GB |
| Storage | NVMe SSD (btrfs cache pool); PostgreSQL and the application data live on it |
| Operating system | Unraid 7.3.4, Linux 6.18 |
| Container runtime | Docker 29.8.2, the repository's `docker-compose.yml` (application, PostgreSQL 18, SeaweedFS) |
| Application memory limit | the default `APP_MEM_LIMIT=1g` |
| Network | loopback: the load generator ran on the same machine |
| Other load | one real member was online during the run; the instance was otherwise idle |

## Method

The tools are those of [`loadtest/`](../loadtest/README.md): static binaries run
by `loadtest/host-run.sh` on the server host. No browsers are involved. Members
are synthetic accounts that cannot sign in; they are created before and deleted
after the run, and a fingerprint of every database table before and after must
match.

**The load generator ran on the same host as the server.** Its CPU and memory
are part of what the machine spends, and it shares the four cores with the
server and the database. The tables list the server's own CPU; the generator's
CPU is given where it was significant. Numbers measured from a separate machine
would be somewhat better, and on a network they would also include the network.

CPU is the application process in cores (`process_cpu_seconds_total`), averaged
over the steady "hold" phase of each scenario; 4.0 would be the whole machine.
Memory is the peak resident set of the application process. The database ran
in its own container; its share is not included (it stayed below 0.1 core and
about 260 MB in all scenarios).

| Scenario | What it measures |
|----------|------------------|
| Chat, N sessions | N WebSocket sessions in one text channel. 10 % of the sessions write 0.2 messages per second each, with typing indicators; 5 % of the messages get a reaction and 3 % an edit. Everyone reads. Latency is measured from the start of the send request to the moment each session reads the event. Loss is the share of expected deliveries that never arrived. |
| Voice, direct | One shared screen (synthetic VP8, 1500 kbit/s, 15 fps) watched by 99 members, over direct UDP. |
| Voice, through the relay | The same screen and 3 speakers, 100 members; every member may use relayed candidates only, so all media goes through the built-in TURN relay. |
| Voice, mixed | One screen, 10 cameras (300 kbit/s, 24 fps) and 3 speakers; all 100 members watch the screen and all cameras. |

Each scenario held for two minutes after connecting. Voice media is synthetic
(valid packets sized to the bitrate, keyframes every three seconds); nothing is
captured, encoded or decoded.

## Results

### Chat

Delivery latency is over all receiving sessions, so the 1,000-session run is
2.36 million measured deliveries. No message, edit or reaction was lost or
duplicated in any run, and no send request failed.

| Sessions | Writers | Messages p50 / p95 / p99 | Edits p95 | Reactions p95 | Loss | Server CPU (hold / peak) | Server memory (peak) |
|---------:|--------:|-------------------------:|----------:|--------------:|-----:|-------------------------:|---------------------:|
| 100 | 10 | 12 / 18 / 26 ms | 30 ms | 13 ms | 0 % | 0.02 / 0.02 cores | 47 MB |
| 300 | 30 | 14 / 21 / 44 ms | 29 ms | 15 ms | 0 % | 0.09 / 0.10 cores | 62 MB |
| 1,000 | 100 | 33 / 84 / 115 ms | 147 ms | 65 ms | 0 % | 0.67 / 0.70 cores | 102 MB |

Connecting took 20 ms at most per session. Goroutines returned to their
starting value (about 60) after every run.

### Voice, direct

99 members received one screen at 1500 kbit/s.

| Members | Received per member | Loss | Freezes | Connect p50 / p95 | First video p50 / p95 | Server CPU (hold / peak) | Server memory (peak) |
|--------:|--------------------:|-----:|--------:|------------------:|----------------------:|-------------------------:|---------------------:|
| 100 | 1585 kbit/s (1532 to 1631) | 0.00 % | 0 | 31 / 55 ms | 66 / 374 ms | 0.56 / 0.73 cores | 312 MB |

Longest gap in any member's video: 67 ms. The generator used 0.54 cores.

### Voice, through the built-in TURN relay

| Members | Through the relay | Screen per member | Audio per member | Loss | Freezes | Connect p50 / p95 | Server CPU (hold / peak) | Server memory (peak) |
|--------:|------------------:|------------------:|-----------------:|-----:|--------:|------------------:|-------------------------:|---------------------:|
| 100 | 100 of 100 | 1601 kbit/s | 119 kbit/s (3 speakers) | 0.00 % | 1 member, one 12.6 s gap | 1038 / 1058 ms | 1.61 / 1.67 cores | 312 MB |

One member lost its connection once (a "flap") and was frozen for 12.6 seconds
before it recovered; the scenario's threshold allows no flaps, so it is
recorded as failed although the other 99 members saw no freeze. The generator
used 0.82 cores. About 3.3 GB left the server in the two minutes.

### Voice, mixed (does not hold)

One screen, 10 cameras and 3 speakers, every member watching everything:
roughly 4.5 Mbit/s of video per member, 450 Mbit/s in total out of one machine
that also runs the generator.

| Members | Screen received | Camera received (per track) | Screen loss | Camera loss | Audio loss | Server CPU (hold / peak) | Server memory (peak) |
|--------:|----------------:|----------------------------:|------------:|------------:|-----------:|-------------------------:|---------------------:|
| 100 | 200 kbit/s of 1500 | 134 kbit/s of 300 | 81.6 % | 33.5 % | 0.3 % | 1.87 / 2.06 cores | 1044 MB |

All members connected and none timed out, but most of the video did not arrive.
The server used about two cores and reached the 1 GB memory limit, and the
generator used 0.97 cores, so on this machine the run is limited by the shared
host. We did not determine whether the server, the generator or the loopback
path was the first bottleneck. Goroutines and heap returned to their starting
values afterwards, so there is no leak.

## What this shows

- Chat is cheap: 1,000 concurrent sessions used about two thirds of a core and
  100 MB, and delivered every event within 190 ms.
- One screen share to about 100 members used about half a core directly and
  about 1.6 cores through the relay.
- A Talk in which 100 members watch ten cameras and a screen at the same time
  is beyond this machine.

## Limits and caveats

- **One host, one run.** There is no repeat, no variance and no other hardware.
  A different CPU, storage or kernel will differ.
- **Generator on the same host.** See above. It inflates latency a little and
  takes CPU the server could have used.
- **Loopback network.** No packet loss, jitter or bandwidth limit between the
  generator and the server. Real members on the internet add their own latency,
  and the relay figure does not include a real uplink.
- **Rate limits shape the setup, not the capacity.** The server allows 120
  WebSocket upgrades per minute per address, so the chat test connected 100
  sessions a minute (1,000 sessions took ten minutes to connect), and the relay
  accepts 120 allocations per minute and 256 in total. These are configured
  limits, not what the machine could do. The test also stays under the API
  limit of 600 requests per minute per account.
- **Synthetic media.** Voice packets are not real encoded video, and there is
  no end-to-end encryption work on the generator side. A real browser decrypts
  and decodes every stream; the server only forwards packets, so server CPU is
  comparable but client CPU is not covered.
- **Short holds.** Each scenario ran for two minutes. Memory growth over hours
  was not measured here.
- **Not tested:** more than 1,000 chat sessions, more than 100 voice members,
  several Talks at once, mixed chat and voice at the same time, large
  attachments, and many members signing in at once.
- Push notifications reached the one real member who was online while the
  messages were sent (24 deliveries in each chat run).

## Reproduce

```sh
make load-test-host        # bin/loadtest-host/mnema-loadtest-linux-{amd64,arm64}.tar.gz
scp bin/loadtest-host/mnema-loadtest-linux-amd64.tar.gz <host>:
# on the host: tar xzf ... && cd mnema-loadtest-linux-amd64 && ./host-run.sh
```

Take a backup first and run it when nobody is using the instance. The full
procedure, every setting and the thresholds are in
[`loadtest/README.md`](../loadtest/README.md#on-the-host-capacity-test-without-browsers).
The capacity table in [doc/architecture.md](architecture.md#capacity) lists the
same figures in short form.
