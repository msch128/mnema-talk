# Review PR #815, delta fa6ce2f3d..57e5a87bb (merge of main excluded)

Also in the delta but not in the brief: GoWaited in internal/safego/safego.go with a test, and its two callers in communityevents. See (g).

## (a) RETURNING fix complete? Yes
Grepped every RETURNING in internal/*.go that touches channels.
- INSERT channels: channels.go:122 (create), channel_layout.go:237 (copy, used by duplicate and OpenTempTalk), talks.go:123 (DM Talk). All three now use core.AutoDeleteSQL.
- channels.go:268 (update) already had it before the delta.
- The others do not return a core.Channel: seed_layout.go:105 returns id, categories.go:43 returns a category, chattest/member.go:33 returns id, and the rest are media deletes.
- Only one other Scan into SlowModeSeconds exists, channel_layout.go:196 (LockSourceChannel). It reads the source for a copy, not a response.
- FillAutoDeleteDays is called in the create and copy paths. talks.go does not call it, and that is correct: a conversation Talk is private, so hours is 0 and days is 0.

## (b) Does the test fail without the fix? Yes, checked by mutation
Docker works through ~/.orbstack/run/docker.sock; DOCKER_HOST must point at it.
- Fixed tree: TestANewChannelAnswersTheDefaultItFollows passes.
- channels.go reverted to fa6ce2f3d: fails at "create", with scope "" and hours 0.
- channel_layout.go reverted to fa6ce2f3d: fails at "duplicate", with the same values.
So both fixed paths are covered. The test sets default 48h with scope attachments, so days 0 is a meaningful assertion. For scope "all" the days value is not tested; that is a minor gap.

## (c) talks.go path: untestable through HTTP, but testable below it
The HTTP response is core.ChannelInfo (id, type, limit, slow mode), so the bug is invisible there. OpenDMTalk is exported, though, and returns *core.Channel. A talks-package integration test could call it directly and assert hours 0 and a valid scope. That would be cheap, but it is non-blocking: the value is discarded today, and the SQL is identical to the tested one.
The real exposure is that later reads of the Talk use the same AutoDeleteSQL, so this is a consistency fix only.

## (d) layout.spec change: an honest adaptation, with a small weakening
- Honest: 'messages' is added to TABS, and scrollIntoViewIfNeeded precedes the toBeInViewport ratio-1 check. The 12th tab now needs a scroll at 900px, and the check still catches a tab that is cut or covered once scrolled to.
- Weakening, minor: "reachable without scrolling" is no longer asserted.
- The barAt helper measures y + scrollTop of the nearest ancestor that overflows. This is invariant to the scroll itself, which is intended. It also would not flag a bar that visually moves only because switching tabs reset scrollTop. The dialog-height check and the content-space position check still catch real layout shifts. Acceptable.
- The scroller walk falls back to scrollTop 0 if nothing overflows, which gives the plain y as before.
- Not run (no e2e locally).

## (e) a11y heading order: holds
The dialog title is the only h2. Every other admin tab uses an h3 section title in the same mono, uppercase style (Layout, Media, System, Onboarding, Access, Automod, Raid). AdminMessagesTab now matches. The spec's `getByRole('heading', {level: 2})` is strict-mode safe because a second h2 would have broken it. The Timer icon is aria-hidden.
Chip: @max-[40rem]:sr-only is a descendant of the header, which carries @container (ChatHeader.vue:131). The same syntax is used at line 154. The label is still in the DOM for screen readers, and the tooltip carries the full line. One behaviour change: viewports of 640-767px that used to hide the label now show it. That is acceptable.

## (f) Limits
- Files under 400 lines: channels.go 338, channel_layout.go 304, talks.go 243, the integration test file 312, safego_test.go 282.
- Tests are under 800 lines. New functions are about 10-20 lines and simple. No gates were touched.

## (g) GoWaited (extra)
- Add(1) before the goroutine, `defer wg.Done()` outside Run, so Done runs after the panic handling. The test holds onPanic open and checks the group stays held.
- With safego.Crash the panic is re-raised, so the process dies. The doc comment says "wg is never released", but the deferred Done still runs while unwinding. That comment is slightly inaccurate and harmless.
- The callers drop their manual Add/Done. Behaviour is the same, with Done now after the recover.

## Local runs
- go vet on chat, safego, communityevents and server: clean.
- go test for safego and communityevents: pass.
- The integration test: pass, plus the two mutations above.
- Web: vue-tsc --noEmit clean; eslint on the two changed .vue files clean.
- No e2e, no audio, no camera. Worktree removed.

## Verdict: MERGE
Non-blocking suggestions:
1. Add a direct OpenDMTalk integration test in the talks package, reading the returned channel (talks.go:123).
2. Fix the GoWaited comment about Crash in internal/safego/safego.go.
