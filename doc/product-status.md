# Product status and acceptance

This page defines product status independently of release numbering. A build,
a passing unit test or an isolated cryptographic component cannot qualify an
entire communication path.

| Category | Meaning |
| --- | --- |
| Supported | Normal maintained product path. Named tests and explicit limits apply. |
| Experimental | Available to execute or test; outstanding acceptance prevents a production qualification. |
| Beta | Usable and maintained, but outstanding acceptance (for example platform signing) keeps it out of Supported. Expect rough edges. |
| Planned | Requirements exist, but no qualified usable product path is available. |

## Status at 1.1

Mnema Talk 1.0 was the first release under the
[API stability contract](api-stability.md): its public surfaces change
incompatibly only in a major release. 1.1 keeps that contract and only adds to
it. What the 1.x label does and does not say:

**Supported versions.** Security fixes go to the latest 1.x minor and, once a
newer minor exists, to the previous minor for 90 days
([SECURITY.md](../SECURITY.md#supported-versions)). Once 1.1.0 is published,
that means 1.1.x and, for 90 days, 1.0.x; 0.9.x and older get no fixes. CI
upgrades from 0.8.16, 0.9.0 and 1.0.0 to prove the upgrade path; that is a
test, not a support promise.

| Area | Category at 1.1 |
| --- | --- |
| Server (Docker, linux/amd64 and arm64), browser app, text chat, roles and permissions, webhooks and bots, notifications, search and onboarding, voice, cameras and screen share in Chromium-based browsers | Supported |
| Direct messages (two to ten members), blocking and reports, saved messages, forwarding, thread follow, search filters, built-in slash commands, reading a Talk's chat without joining | Supported (new in 1.1; each has server tests and a browser spec) |
| Browser app on WebKit (Safari, iOS, installed iOS web apps) and Firefox: text chat, search, voice, cameras and screen share with sealed media, as tested in [Browsers](#browsers) | Supported for text chat, search and administration. Sealed calls pass on macOS WebKit locally; the CI WebKit job cannot run them because Playwright's Linux WebKit build has no encoded-media API (`RTCRtpScriptTransform`); see the limits there (not yet tested on real iOS or Android hardware) |
| Operations: Prometheus metrics and [alert rules](../deploy/monitoring/mnema-alerts.yml), backup and restore, upgrade tested from 0.8.16, 0.9.0 and 1.0.0, cosign-signed images, self-update that verifies the image's signature and provenance before it restarts the app | Supported |
| Native mobile apps, text end-to-end encryption | Not offered. Text stays readable by the server, by design. The browser app has a phone layout (channel list as a drawer, full-screen thread and member list, 44 px touch targets, safe areas) that is covered by the `mobile-*` e2e projects in Chromium and WebKit device emulation, not by tests on real phones. |
| Desktop client on Windows | Beta (unsigned, no Authenticode; SmartScreen warns) |
| Desktop client on macOS and Linux | Beta (DEV builds, not qualified on real hardware; not notarised; Gatekeeper warns) |

**Internal security review, 2026-10-22.** Before 1.0 the code was reviewed in
six lanes (authentication and sessions, permissions, content and media,
integrations, realtime and voice, supply chain and updates). It found no
Critical and no High issue; every Medium finding is fixed, and a short list is
deliberately deferred (see the
[summary in SECURITY.md](../SECURITY.md#10-internal-security-review)). This was
done by the project itself with automated help. It is **not an independent or
external audit**, and none has been commissioned.

What 1.x does not claim: real 4K/60 capture, system audio from physical
hardware, an external security audit, the 250-endpoint capacity target, load
results on dedicated hardware, Authenticode or notarised desktop signing, and
sealed media qualified in WebView2 on real hardware. These stay outstanding
under "Remaining acceptance" below.

## Supported browser and server path

The Vue browser app and Docker server for Linux amd64/arm64 are the normal
product path. CI exercises backend/frontend regressions, browser end-to-end
flows, advancing screen-share frames, image smoke tests, backup/restore with
message and attachment checks, and an isolated PostgreSQL upgrade drill.
These tests have different scopes: synthetic two-browser screen sharing does
not establish real 4K/60 capture, system audio or 100-person capacity.

HTTPS protects browser traffic in transit. Chat messages and attachments stay
readable by the server. Microphone, camera, screen video and screen audio are
encrypted in the browser with MLS-derived keys; an honest SFU forwards those
payloads and cannot read them. From 0.8 on the keys rotate every 30 minutes,
members silent for 20 minutes are removed and re-admitted, every sealed frame
is signed with its sender's leaf key, and the connection details show a safety
code to compare. An active or compromised server is not excluded (it serves
the code and asserts group membership), and metadata such as who speaks when
stays visible. The transport underneath is DTLS-SRTP. See [SECURITY.md](../SECURITY.md).

Other capabilities on this path, each with automated tests:

- **Screen-share layers** (source up to 4K at 60 FPS plus smaller layers) with a
  per-viewer choice or Auto; the browser tests switch layers mid-stream and
  check that other viewers are not disturbed.
- **Roles and permissions** with a protected Owner, overwrites and private
  channels and Talks, enforced in REST, the hub, media serving and the SFU.
- **Incoming and outgoing webhooks** with an SSRF guard for outgoing delivery.
- **Operations**: Prometheus metrics, the live operator view, alert examples,
  cosign-signed release images, and the backup, upgrade (release to release)
  and PostgreSQL upgrade drills in CI.

Not covered by these tests: real 4K/60 capture, system audio from physical
hardware, an external security audit and capacity beyond the recorded runs.
A six-lane internal security review preceded 1.0 (see "Status at 1.1"); it is
not an independent audit.

## Browsers

Sealed media needs three things in the browser: a frame transform that
reaches encoded media (`RTCRtpScriptTransform`), Ed25519 in WebCrypto and a
module worker. Chrome and Edge 137+, Safari 17+ (including iOS 17+ and installed
iOS web apps) and Firefox 129+ have them. A browser without them **does not
join a call at all**: the join is refused with a message, no microphone is
requested and nothing is sent. Media is never sent unsealed, in any engine.

What the end-to-end suite showed (Playwright with the engines it ships:
Chromium 156, WebKit 27.2, Firefox 157, two members per call with fake
devices; run `make e2e` or see [development](development.md#browser-tests-e2e)):

| | Chromium (Chrome, Edge) | WebKit (Safari, iOS) | Firefox |
| --- | --- | --- | --- |
| Sign-in, chat, search, roles, moderation, webhooks, notifications | Supported | Supported | Supported |
| Microphone and camera, sealed on every line, with key rotation, late join and reload | Supported | Passes on macOS WebKit; in CI only in `e2e (webkit-macos-calls)` (Playwright's Linux WebKit has no `RTCRtpScriptTransform`, so the app refuses the join there, see below) | Supported |
| Screen share with viewer-chosen layers (simulcast) | Supported | Passes on macOS WebKit; in CI only in `e2e (webkit-macos-calls)` | Supported (VP8) |
| Screen share frames advance for the viewer | Supported | Passes on macOS WebKit; in CI only in `e2e (webkit-macos-calls)` | Supported |
| Required CI check | Yes | No (`e2e (webkit 1/2)` and `e2e (webkit 2/2)`: every spec that does not join a call, plus the refusal spec) | No (`e2e (firefox)`, smoke and sealed-call subset) |
| Not covered by the suite | | Real Safari and iOS hardware; calls on Linux WebKit (no frame transform in that build, they pass on macOS WebKit) | Real screen capture |

A real-device call (real camera and microphone, real Google Chrome on macOS,
sealed both ways) was verified on 2026-10-22.

What it took:

- **Firefox, screen share.** Screen lines asked for H.264 first. Firefox lists
  H.264 but encodes it with a downloadable OpenH264 module; without it the
  encoder never produced a frame and the share stayed black without an error.
  Firefox now keeps VP8, which it encodes itself (sealed the same way).
- **All engines, joining.** A browser that cannot seal media is refused at the
  join (before any device is requested) instead of joining with microphone,
  camera and screen switched off.
- **Linux WebKit in CI cannot take part in a sealed call.** This is the engine
  build, not the app. Playwright's Linux WebKit (26.6 in 1.63, 27.2 in 1.64.1, in
  both cases the version of its macOS build) reports `RTCPeerConnection` and `getUserMedia`, but
  `RTCRtpScriptTransform`, `RTCRtpSender.prototype.createEncodedStreams` and
  `RTCEncodedAudioFrame` are all `undefined` (checked on x64 and arm64 Linux
  with both versions; the macOS build has `RTCRtpScriptTransform` and `RTCEncodedAudioFrame`). With
  no way to reach encoded media the app does what it promises for such a
  browser: it refuses the join with a message, requests no microphone and sends
  nothing (`browser-sealing.spec.ts`, second spec, runs in the WebKit CI job). The
  first spec of that file skips itself only on Linux WebKit and only while
  `RTCRtpScriptTransform` is missing, so a build that gains it runs the call
  again. Every spec that joins a call (calls, screen share, Talk chat, roles
  rights) stays out of `e2e (webkit 1/2)` and `e2e (webkit 2/2)` and runs in
  four short `e2e (webkit-macos-calls, <group>)` jobs on macOS runners, not a required check.
- **WebKit 27.2, `<select>` and the style policy.** Playwright 1.64's WebKit
  (27.2) reports one `style-src-attr` violation for every `<select>` a page
  builds (empty sample, the script that created the select as source; the
  browser's own shadow tree sets an inline style). Nothing is refused: a page
  with selects renders pixel-identically under `'unsafe-inline'` and under
  `style-src-attr 'none'`, and 26.6 does not report it. The policy is left as
  it is; the e2e fixtures ignore this one signature in WebKit only (the
  Chromium and Firefox jobs run the same code and still fail on any
  `style-src-attr` event). Not yet checked in a release Safari 27.
- **Firefox, navigations the driver loses.** On Linux Firefox (Playwright 1.63
  and later) `page.goto` sometimes never resolves although the server answered
  200 and `document.readyState` is `complete`: the driver's frame stays on
  about:blank (no `navigated` event; 3 to 12 of 40 navigations of a bare page
  on a 2-CPU container). Not specific to this app; the e2e fixtures retry the
  navigation once the first attempt has timed out (`tests/navigation-diagnostics.ts`).
- WebKit mutes the microphone of all but one page per browser process (as
  Safari does between tabs). That limits two members in one Safari window, not
  two devices; the suite therefore runs each WebKit member in its own process.

Stability: Chromium passed every spec on every run. Firefox passed the
whole suite apart from the specs that need Chrome's DevTools protocol, and its
CI subset (7 files) 10 of 10. WebKit passed the suite apart from those too,
but with one or two timing failures per full run on a loaded machine (a sealed
call taking longer than the spec waits to re-key after a reload, a simulcast
layer not up in time, a voice-chat leave); each passes when run alone. They are
retried once in CI, and the WebKit and Firefox jobs stay out of the required
checks until they have gone green for a while.

The AI noise filter's worker pipeline was seen processing audio on macOS
Firefox but not on CI's Linux Firefox, so its spec (`late-join`) is not in the
Firefox CI subset.

Known limits: Firefox's fake microphone is a steady tone, which the AI noise
filter removes, so the specs measure decoded samples rather than loudness.
Real-device behaviour of iOS audio sessions, background tabs and installed web
apps is not covered by the suite and stays on the "Remaining acceptance" list.

## Beta desktop path

The desktop client is Beta on every platform. It is unsigned by decision: no paid code-signing certificate will be bought (see [installing the unsigned Beta](desktop-app.md#installing-the-unsigned-beta)). The Windows path is the
furthest along and is described first; macOS and Linux builds are published by
the desktop repository as DEV builds (a macOS disk image, Linux AppImage and
.deb packages, each with an updater signature) and run the same signed web
bundle. That repository's CI builds the packages and has a Linux workflow, but
they are not qualified on real hardware, are not
notarised or otherwise platform-signed, and desktop-specific features such as
the gaming integration are Windows only. Everything below about the signed
bundle, the security floor and updates applies to all three.

### Windows

The Windows desktop app lives in the separate
[mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop) repository
and is an unsigned Tauri DEV client, including the Gaming integration described
in the [usage guide](desktop-app.md#windows-gaming-dev). Its local instance
selector checks HTTPS (or a loopback address) and the server's `web_client_api`
and release; it then opens the Vue app from a **signed bundle that the client
verified** (built into the installer or downloaded from this repository's
releases, Ed25519 key pinned in the client) at the client's own origin. The
client forwards `/api/` to the instance, keeps the session cookie and relays the
WebSocket, so the instance supplies community data and never executable code
([design and threat model](desktop-trusted-client.md)). The page receives only
the WebSocket commands and the gaming integration's session-bound Talk-state
sync; local gaming surfaces hold their separate display/control permissions.
Status: unit-tested and run on macOS during development; the bundle verifier
has a high-water mark, a security floor that rises with releases whose
security fixes carry a `Security-Floor: raise` trailer and a session cookie kept in the OS keychain. Windows CI on the
desktop repository
([run 39423960431](https://github.com/msch128/mnema-talk-desktop/actions/runs/39423960431))
checks the current web-client host. WebView2 presents the custom protocol as
`https://mnema.localhost/` for the selector, the instance window and the
gaming surfaces. The instance window leaves the microphone on WebView2's own
prompt; the check does not click it. The selector and the gaming surfaces are
refused. The host passes a synthetic capture device, and each probe window
has its own profile because a refusal is remembered for the origin. A human
prompt, a physical device, sealed media and an Authenticode signature remain
outstanding. The installer is unsigned.

Voice in the desktop client uses the instance's browser path, so microphone,
camera and screen payloads are sealed in WebView2 by the same code as in a
browser. This is not yet qualified on real Windows hardware.

Desktop builds come from the mnema-talk-desktop repository (releases of this
repository stopped attaching them after v0.7.3). WebView2 Runtime is required on
Windows. Checksums identify package bytes; they are not publisher signatures. The
desktop installer of a release updates itself with signature-verified, newer-only updates; no
Authenticode signing exists.

The desktop repository's own CI runs a limited Windows flow (instance selection,
sign-in, a persisted message, a live message over the hub connection, sign-out)
on generated test accounts and an isolated server.

These checks qualify only that limited flow. They do not qualify production-account
usage, S3 uploads, physical microphone/camera capture, system audio, device
recovery, E2EE, gaming, overlays, exclusive fullscreen or complete browser parity.
The earlier feasibility probe and native-crypto checkpoint are research
foundations, not alternate supported clients.

## Remaining acceptance, in order

The Windows-first client and its Gaming integration have been released.
Versions remain managed by the normal release pipeline. Further runtime
qualification and the remaining requirements below are still outstanding.

The Gaming integration connects these surfaces to the existing client:
left-hand sidepeek with participants, speaking, mute and screen-share state;
Alt+M interactive overlay; controls for the actual own Talk; persisted,
configurable mute, deafen, push-to-talk and overlay bindings. Both surfaces
require an own connected Talk and an active detected game. See the
[Windows Gaming DEV usage and limits](desktop-app.md#windows-gaming-dev).
Topmost windows do not establish exclusive fullscreen or anti-cheat support.

The remaining fixes and acceptance work follow:

1. Client parity: Windows CI checks the trusted bundle's custom-protocol
   origin (`https://mnema.localhost/`), that the instance window leaves the
   microphone on WebView2's own prompt, and that the selector and the gaming
   surfaces refuse one. The check uses a synthetic capture device and does
   not click the prompt. A human prompt, a physical microphone or camera,
   display capture, sealed media on real hardware and a signed installer
   remain outstanding. The macOS and Linux builds have no hardware qualification either.
2. Chat messages and attachments stay readable by the server and are not deleted by age. Sealed media is verified in Chromium, WebKit and Firefox (see [Browsers](#browsers)), and in Chromium for calls between current and one-release-older tabs; it is not yet qualified in WebView2 on real Windows hardware, and no external audit has reviewed it.
3. Capacity: [`make load-test`](../loadtest/README.md) is reproducible (smoke, the 100-member `full` profile, join and reload stall checks, packet loss and jitter on the lightweight viewers, a two-hour `long` profile). On a loaded single test machine the `full` profile misses its gap and heap thresholds while all 84 viewers stay connected, so what remains is recording results on dedicated hardware with more real decrypting viewers ([capacity notes](architecture.md#capacity)). A viewer's bad reports move only that viewer to a lower screen layer (`TestOneViewersTroubleLeavesTheOtherOnTheHighLayer`); voice forwarding does not follow screen layers. The additional 250-endpoint target is deferred.
4. Independent operation. `main` is protected by a ruleset (pull request required, CI and CodeQL checks must pass, squash merge only). Review approvals are not required. Dependency advisories remain part of release checks; a green build does not mean zero risk.

[Desktop requirements](desktop-app.md) and [the native research checkpoint](https://github.com/msch128/mnema-talk-desktop/blob/main/experimental/README.md) describe
these boundaries in more detail. Existing coverage and security gates remain
in force.
