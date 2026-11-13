# Review: the pre-merge change to TestSubscriptionTogglesMakeQuietStreams (H-L8-07)

Reviewer: independent, read-only. The outside audit of 3.0.10 flagged this change as unreviewed. The change is
commit b33520cda on origin/msch128/hunt-bundle-2, squashed into origin/main as 8fcf44700; I read the test and the
cap at origin/main (e6c9d9394) in a temporary worktree, removed afterwards. Loopback only, no audio device, camera or
microphone; `nice -n 10`, `go test -p 1`. Load was 2 to 3.

Verdict: **the change is right as it is.** The explanation is correct, the stray is understood and bounded by a cap
the H-L8-07 review already settled on, it is not a defect worth a finding, the loosened numbers still catch the
regressions this test guards, and the SECURITY.md clause is true and well placed. One optional hardening is noted.

## 1. Is the explanation right? Yes, and it is the "just made" case

I instrumented a local build (trace removed afterwards) to record, per SSRC, every `sentTrack`, `stoppedTrack`,
`buffer` and `closed` call with timestamps and the Go caller, and ran the test 24 times under the failing
configuration (GOMAXPROCS=2, -race, -covermode=atomic -coverpkg=./internal/...). The stray appeared on run 10. Its
timeline (one SSRC, times from the fill phase start):

```
1988.1ms sentTrack (media)      -- the SFU made this sender; c.sent[ssrc]=true (read)
2003.4ms buffer  quiet=false read=true sent=true retired=false declared=false  from incoming packet
2010.5ms stoppedTrack           -- the sender was removed ~7ms later
(no "closed" ever)
```

So the window is "just made", not "as it stops":
- `addSender` makes the Pion sender and calls `sentTrack`, which records the media SSRC with `read=true`
  (room_signal.go:225, transport.go:253-273).
- Before Pion's `RTPSender` opens its own SRTCP read stream (srtpWriterFuture.init → `OpenReadStream`), one of the
  viewer's own receiver reports names that SSRC. Pion's SRTCP demux then creates the read stream through the buffer
  factory (`getOrCreateReadStream` from `SessionSRTCP.decrypt`). The factory sees `read=true`, so `quiet=false`: the
  stream gets the capped `rtcpBuffer` a live sender's stream should get, and `buffered++`. This is correct for a live
  sender: the SFU reads NACK/PLI on that SSRC.
- The toggle then stops the sender almost at once. `stoppedTrack` retires the SSRC (transport.go:276-290), and
  `RemoveTrack` stops the Pion sender. But `srtpWriterFuture.Close` (webrtc srtp_writer_future.go:82-98) closes the
  read stream only if the future's own `init` had already opened it; here the demux opened it and the future's
  `init` never ran, so `rtcpReadStream` is nil and nothing is closed. `rtcpBuffer.Close` is never called, so
  `closed()` never runs and `buffered` is not decremented.

The orphaned stream is the SFU's own former sender SSRC (not an arbitrary one), with a capped buffer, held until the
PeerConnection closes. "No unknown stream, counted by the buffered backstop, connection renewed long before" is
accurate: the SSRC is `sent`/`retired`, never `unknown`, and `buffered` is the counter checked against
`maxBufferedRTCPStreams` (1024). The symmetric "as it stops" wording in the commit is plausible but I only observed
the "just made" interleaving.

## 2. Test matter or a defect? Bounded; not a finding

The stray costs about 16 to 20 KB (a filled capped buffer) and lives until the connection ends. Two questions decide
whether it is a finding.

**How many can an honest long call collect?** Each needs a report to land in the few-millisecond gap between
`addSender` and Pion's own `OpenReadStream`, for a sender that is then stopped before that open. In 24 ordinary runs
of 300 toggles it happened 0 to 1 times. So well under 1 per 300 renegotiations; negligible over a real call.

**Can a hostile client farm them to the 1024 backstop (about 20 MB)?** I wrote a forcing test: a viewer that reports
on every new sender the instant the SFU makes it and delays its own answer by 150 ms to widen the gap, then stops the
sender, for 120 cycles. Result: **2 strays**, 121 quiet, heap +3.6 MB, connection still up. So even under adversarial
timing the stray is a low-probability race (about 1 to 2 %), not a reliable farm. Two further points bound it:
- A stray increments `buffered` and is never decremented, so strays **do** count toward `maxBufferedRTCPStreams`.
  Farming them therefore cannot exceed that cap: at 1024 the connection trips and closes, exactly the arbitrary-SSRC
  ceiling the H-L8-07 review already accepted (about 20 MB, then self-disconnect). It opens no new ceiling.
- Reaching 1024 at about 1 to 2 % per renegotiation needs on the order of 50,000 to 100,000 renegotiations, each a
  Subscribe/answer round under the 40-events-a-second budget: hours, ending in the member disconnecting itself.

So it is a real imperfection, not a farmable DoS beyond the settled bound. No H-L8-10 finding. I did not write one.

**Optional hardening (NOTE):** `retire` (transport.go:292-305) already deletes the SSRC from `c.sent`; it could also,
when a buffered stream for that SSRC is still open, count it so it cannot accumulate (for example decrement
`buffered` and treat the orphan as quiet), or `stoppedTrack` could close the stream. Closing a Pion-owned stream from
our side is awkward and risky on the hot path; given the stray is bounded by the 1024 backstop and is rare, I would
leave the cap as is and not add code to a protected area for it. Recording the behaviour (the test and SECURITY.md)
is the right response.

## 3. Are 3 strays and 12 KB justified? Yes; the target regressions still fail

I ran the three mutations the task named against the changed test:

| Mutation | Result |
|---|---|
| `declare` disabled (`_ = declared`) | PASS (0 unknown, 0 buffered, 300 quiet) |
| `retire` disabled (`retired := false`) | FAIL: 73 unknown, viewer disconnected |
| buffers not capped (no quiet buffers) | FAIL: 300 buffered, 18,350 B/SSRC, over both the 3-stream and 12 KB bounds |

- The two regressions this test guards, the retire path and the quiet buffer, both still fail, by a wide margin
  (300 buffered against the cap of 3; 18.4 KB/SSRC against 12 KB).
- `declare` disabled passes, and that is correct, not a loosening: the toggle viewer declares no SSRC for the
  screen senders; they are forgiven because the SFU stopped sending them (`retired`), not because they were
  declared. The declare path is guarded by TestFirefoxLikeViewerStaysConnected and TestRTCPStreamCapSortsStreams
  (I confirmed in earlier rounds that disabling declare fails those). The original, stricter numbers also passed
  declare-disabled, so nothing regressed here.
- The bounds are tight enough to catch a real regression of the stray mechanism: if orphaned buffers became
  systematic (tens or hundreds), `buffered > 3` fires at once, and the 12 KB bound fires well before all 300 are
  buffered (that is 19 KB each). 12 KB sits above the measured noise (up to 7 KB under race + coverage on two cores)
  and below a single extra buffered stream's share, so it is neither flaky nor blind.

24 of 24 runs of the changed test passed in the failing configuration (the run that produced the stray among them,
with 1 buffered, still inside the cap of 3).

## 4. Is the SECURITY.md clause true and placed right? Yes

The clause reads: "a report that arrives in the moment a sender is made or stopped can leave that one stream with its
capped buffer, which the buffered limit counts." True: the stray keeps the capped (not quiet) buffer, and it is
counted by the buffered backstop (my section 1 and 2 confirm both). "Made or stopped" is a fair description; I
observed the "made" side. It is placed inside the "media connection limits" bullet, right after the sentence on
quiet streams and before the limits and counter, which is where a reader meets the quiet/buffered distinction. Good
placement.

## What I ran

- 24 runs of TestSubscriptionTogglesMakeQuietStreams under GOMAXPROCS=2, -race, -covermode=atomic
  -coverpkg=./internal/... (the failing configuration); a stray on run 10, instrumented to the timeline above.
- A forcing test (120 cycles, reports on each new sender, delayed answer): 2 strays, connection up.
- The three mutations against the changed test.

## What I did not check

- A real browser; the "as it stops" interleaving (only "just made" was observed); the deep env-gated runs.
- Whether a future Pion release closes a demux-created stream on sender stop (it does not in v4.2.26).
