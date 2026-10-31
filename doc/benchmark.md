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
  is beyond this machine, also with 2.4.0; see the [sizing guide](#sizing-guide).

## Sizing guide

The question this section answers: how much hardware does a Talk of a given
size need? It comes from a second series on server 2.4.0 on the same host as
above: 27 shapes (3 speakers; plus a 1.5 Mbit/s screen share; plus 4 or 10
cameras at 300 kbit/s, or 4 cameras at 1 Mbit/s; 10, 25, 50 and 100 members;
direct and through the relay for the first two shapes), each run three times
with a 120 s hold (81 runs). The generator ran on the same host, as above.

### The model

Server CPU is well described by the number of packets the server handles. A
least-squares fit over the 23 shapes that ran without packet loss (all three
repeats averaged) gives:

| Term | Cost |
|---|---|
| each packet sent to a member (egress) | 24 µs of one core |
| each packet received from a publisher (ingress) | 57 µs |
| each connected member | 0.17 ms per second (0.00017 cores) |
| each relayed egress packet (built-in TURN) | +11 µs |

The fit has R² = 0.995; the largest difference to a measurement is 0.06 cores,
and the standard deviation over the three repeats of a shape is at most 0.03 cores. The packet rates
follow from the shape, with K speakers (50 packets/s each at 20 ms), C cameras
and S screen shares of bitrate b, and N members:

- ingress packets/s = 50·K + (C·b_camera + S·b_screen) / 6.5 kbit
- egress packets/s = (N − 1) · ingress packets/s
- upload ≈ 1.3 · (N − 1) · (K · 0.04 + C·b_camera + S·b_screen) Mbit/s (the
  factor covers packet headers, keyframes and retransmissions; measured 1.2 to 1.3)

6.5 kbit is the measured average video packet (about 800 bytes) of the
synthetic publishers. Real browsers send 720p cameras at up to 2.5 Mbit/s, so
use the real bitrate of your cameras.

Memory is dominated by the retransmission history Pion keeps for every
video stream a member receives. The live heap (peak, without garbage) fits
110 MB + 2.1 MB · (N − 1) · (C + S) − 0.6 MB · N (R² = 0.90, fitted up to
245 forwarded video streams). The process also needs about 250 MB for the
runtime and connections. The table sizes the container limit as
1.3 · (heap + 250 MB), at least 1 GB. Up to the measured range this is
consistent with what we saw: the 1 GB limit was reached (`memory.events` `max`
above zero) with 4 cameras from 50 members on and with 10 cameras from 25 members on.

### Sizing table

CPU is in cores of this host's Intel N100. "To plan for" is twice the fit,
because bursts lose packets well before 100 % CPU. A modern server vCPU is
usually at least as fast as an N100 core, but we did not calibrate this on
another CPU, so treat it as an assumption. Memory is the suggested
`APP_MEM_LIMIT` (the application sets its Go memory limit to 85 % of it);
upload is the connection the server needs. The last column says whether the
shape itself was run on the test host and kept 100 % of its packets.

| Shape | Members | Egress packets/s | CPU, fitted (cores) | CPU to plan for (x2) | `APP_MEM_LIMIT` | Upload (Mbit/s) | On the test host |
|---|---:|---:|---:|---:|---:|---:|---|
| 3 speakers | 10 | 1,350 | 0.04 | 0.1 | 1.0 GB | 1 | measured, no loss |
| 3 speakers | 25 | 3,600 | 0.10 | 0.2 | 1.0 GB | 4 | measured, no loss |
| 3 speakers | 50 | 7,350 | 0.20 | 0.4 | 1.0 GB | 8 | measured, no loss |
| 3 speakers | 100 | 14,850 | 0.39 | 0.8 | 1.0 GB | 15 | measured, no loss |
| 3 speakers + 1 screen share (1.5 Mbit/s) | 10 | 3,427 | 0.11 | 0.2 | 1.0 GB | 19 | measured, no loss |
| 3 speakers + 1 screen share (1.5 Mbit/s) | 25 | 9,138 | 0.25 | 0.5 | 1.0 GB | 51 | measured, no loss |
| 3 speakers + 1 screen share (1.5 Mbit/s) | 50 | 18,658 | 0.49 | 1.0 | 1.0 GB | 103 | measured, no loss |
| 3 speakers + 1 screen share (1.5 Mbit/s) | 100 | 37,696 | 0.96 | 1.9 | 1.0 GB | 208 | measured, no loss |
| + 4 cameras (300 kbit/s each) | 10 | 5,088 | 0.16 | 0.3 | 1.0 GB | 33 | measured, no loss |
| + 4 cameras (300 kbit/s each) | 25 | 13,569 | 0.37 | 0.7 | 1.0 GB | 88 | measured, no loss |
| + 4 cameras (300 kbit/s each) | 50 | 27,704 | 0.72 | 1.4 | 1.5 GB | 180 | measured, no loss |
| + 4 cameras (300 kbit/s each) | 100 | 55,973 | 1.41 | 2.8 | 2.0 GB | 363 | measured, **packet loss** |
| + 4 cameras (1 Mbit/s each) | 10 | 8,965 | 0.28 | 0.6 | 1.0 GB | 66 | measured, no loss |
| + 4 cameras (1 Mbit/s each) | 25 | 23,908 | 0.64 | 1.3 | 1.0 GB | 175 | measured, no loss |
| + 4 cameras (1 Mbit/s each) | 50 | 48,812 | 1.26 | 2.5 | 1.5 GB | 358 | measured, no loss |
| + 4 cameras (1 Mbit/s each) | 100 | 98,619 | 2.48 | 5.0 (more than 4) | 2.0 GB | 723 | measured, **packet loss** |
| + 10 cameras (300 kbit/s each) | 25 | 20,215 | 0.55 | 1.1 | 1.5 GB | 144 | measured, no loss |
| + 10 cameras (300 kbit/s each) | 50 | 41,273 | 1.06 | 2.1 | 2.0 GB | 294 | measured, **packet loss** |
| + 10 cameras (300 kbit/s each) | 100 | 83,388 | 2.10 | 4.2 (more than 4) | 3.5 GB | 595 | measured, **packet loss** |

Read the rows marked "packet loss" as "does not hold on this host", not as
the cost of the shape: the numbers in them are the model's, which is fitted
only on shapes without loss. They hold at most up to about 1.5 times the
largest shape that ran cleanly (the clean shapes reach 1.3 cores of server CPU
and 720 MB of heap), and only for one Talk at a time: several Talks at once were
not measured. The relay adds 60 to 70 % CPU for the screen-share shape.

### What was measured

Per shape, mean of three runs (server CPU is the average over the hold phase;
heap is the peak live heap; RSS is not shown because the process keeps memory
from earlier runs). The generator CPU column is the load generator on the same
host.

| Shape | Members | Path | Server CPU, cores (mean of 3, max spread) | Generator CPU | Heap peak MB | Egress packets/s | Egress Mbit/s | Worst media loss | Container `RcvbufErrors` | `memory.events` max |
|---|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|
| 3 speakers | 10 | direct | 0.06 (±0.00) | 0.06 | 257 | 1,448 | 1 | 0.0 % | 0 | 0 |
| 3 speakers | 10 | relay | 0.09 (±0.00) | 0.06 | 103 | 1,484 | 1 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 10 | direct | 0.10 (±0.00) | 0.10 | 51 | 3,146 | 16 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 10 | relay | 0.17 (±0.00) | 0.10 | 52 | 3,225 | 17 | 0.0 % | 0 | 0 |
| + 4 cameras @ 300 kbit/s | 10 | direct | 0.15 (±0.00) | 0.15 | 267 | 5,058 | 29 | 0.0 % | 0 | 0 |
| + 4 cameras @ 1000 kbit/s | 10 | direct | 0.22 (±0.00) | 0.21 | 261 | 7,987 | 57 | 0.0 % | 0 | 0 |
| 3 speakers | 25 | direct | 0.12 (±0.00) | 0.11 | 40 | 3,997 | 4 | 0.0 % | 0 | 0 |
| 3 speakers | 25 | relay | 0.21 (±0.01) | 0.12 | 35 | 4,147 | 4 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 25 | direct | 0.23 (±0.00) | 0.21 | 103 | 8,693 | 45 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 25 | relay | 0.38 (±0.00) | 0.20 | 103 | 9,065 | 47 | 0.0 % | 0 | 0 |
| + 4 cameras @ 300 kbit/s | 25 | direct | 0.37 (±0.00) | 0.34 | 378 | 14,004 | 79 | 0.0 % | 0 | 0 |
| + 4 cameras @ 1000 kbit/s | 25 | direct | 0.55 (±0.00) | 0.51 | 377 | 22,145 | 157 | 0.0 % | 0 | 0 |
| + 10 cameras @ 300 kbit/s | 25 | direct | 0.58 (±0.01) | 0.54 | 711 | 21,971 | 130 | 0.0 % | 0 | 2 |
| 3 speakers | 50 | direct | 0.23 (±0.00) | 0.20 | 82 | 8,465 | 8 | 0.0 % | 0 | 0 |
| 3 speakers | 50 | relay | 0.39 (±0.01) | 0.21 | 56 | 8,993 | 8 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 50 | direct | 0.45 (±0.00) | 0.40 | 154 | 18,541 | 97 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 50 | relay | 0.77 (±0.01) | 0.41 | 149 | 19,803 | 103 | 0.0 % | 0 | 3 |
| + 4 cameras @ 300 kbit/s | 50 | direct | 0.79 (±0.01) | 0.68 | 542 | 29,866 | 170 | 0.0 % | 3 | 5 |
| + 4 cameras @ 1000 kbit/s | 50 | direct | 1.28 (±0.03) | 1.04 | 547 | 47,204 | 335 | 0.0 % | 49 | 5 |
| + 10 cameras @ 300 kbit/s | 50 | direct | 1.39 (±0.00) | 1.06 | 721 | 45,231 | 264 | 14.6 % | 31 | 4 |
| 3 speakers | 100 | direct | 0.46 (±0.01) | 0.39 | 128 | 18,168 | 17 | 0.0 % | 0 | 0 |
| 3 speakers | 100 | relay | 0.75 (±0.01) | 0.38 | 93 | 20,218 | 19 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 100 | direct | 1.00 (±0.01) | 0.85 | 232 | 40,174 | 211 | 0.0 % | 0 | 0 |
| 3 speakers + screen | 100 | relay | 1.62 (±0.00) | 0.83 | 229 | 45,046 | 237 | 0.0 % | 0 | 0 |
| + 4 cameras @ 300 kbit/s | 100 | direct | 1.35 (±0.01) | 1.08 | 693 | 49,625 | 239 | 64.0 % | 76 | 17 |
| + 4 cameras @ 1000 kbit/s | 100 | direct | 1.41 (±0.01) | 1.10 | 664 | 48,714 | 285 | 68.1 % | 99 | 3 |
| + 10 cameras @ 300 kbit/s | 100 | direct | 3.11 (±0.00) | 0.73 | 815 | 28,277 | 155 | 81.4 % | 81 | 15 |

### Where the packet loss comes from

The shapes with loss all have a screen share and several cameras with 50 to
100 members: 4 cameras at 300 or 1000 kbit/s with 100 members (64 % and 68 %
loss in the worst media class) and 10 cameras with 50 and 100 members (15 %
and 81 %). The packet counters of the report split the path into three stages.
In these runs the packets are lost **on the way into the server**: for 100
members with 4 cameras at 1 Mbit/s the generator sent 90,769 packets in the
window, of which the server counted 42,634 at its ingress (53 % missing),
while 100 % of what the server forwarded arrived at the members. The kernel
counters in the same runs show a few dozen to about a hundred receive-buffer
errors in the container and 3 to 17 hits of the container memory limit
(`memory.events` `max`), and the generator used 1.1 cores (the plan allowed
at most 1.0), so the host was fully shared in these runs. We did not
determine the first bottleneck. Two causes are plausible and untested: the
generator cannot send as fast as it should while the server and database take
the other cores, and the 1 GB memory limit (the 100-member shape with 10
cameras reached a live heap of 815 MB and a resident set of up to 986 MB,
close to the 870 MB the application sets as its Go memory limit; its server
CPU of 3.1 cores for 28,000 packets/s is four times what the fit predicts at
that packet rate and fits garbage collection under that limit). A run with the generator on a second machine, and
one with a higher `APP_MEM_LIMIT`, would tell them apart; neither was done.

The 100-member shape of the first benchmark (screen, 10 cameras, 3 speakers)
was run again on 2.4.0, which sets the memory limit from the container,
requests larger UDP buffers and allocates less per packet: it still lost 81 %
of the screen's packets. `net.core.rmem_max` and `wmem_max` on this host are
already 4 MiB, which is what the server requests, so a larger kernel limit
alone would not have changed anything.

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
