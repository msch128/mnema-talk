# Product status and acceptance

This page defines product status independently of release numbering. A build,
a passing unit test or an isolated cryptographic component cannot qualify an
entire communication path.

| Category | Meaning |
| --- | --- |
| Supported | Normal maintained product path. Named tests and explicit limits apply. |
| Experimental | Available to execute or test; outstanding acceptance prevents a production qualification. |
| Planned | Requirements exist, but no qualified usable product path is available. |

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
payloads and cannot read them. Since 0.8 the keys rotate every 30 minutes,
members silent for 20 minutes are removed and re-admitted, every sealed frame
is signed with its sender's leaf key, and the connection details show a safety
code to compare. An active or compromised server is not excluded (it serves
the code and asserts group membership), and metadata such as who speaks when
stays visible. The transport underneath is DTLS-SRTP. See [SECURITY.md](../SECURITY.md).

Other 0.8 capabilities on this path, each with automated tests:

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
An internal five-area security review preceded 0.8 and its medium and low
findings are fixed; it is not an independent audit.

## Experimental Windows desktop path

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
has a high-water mark, a security floor that rises with `fix(security)`
releases and a session cookie kept in the OS keychain; Windows/WebView2
acceptance (custom-protocol mapping, media permission prompts, gaming surfaces,
sealed media on real hardware) is outstanding, and the installer is unsigned.

Voice in the desktop client uses the instance's browser path, so microphone,
camera and screen payloads are sealed in WebView2 by the same code as in a
browser. This is not yet qualified on real Windows hardware.

Releases of this repository through
[v0.7.3](https://github.com/msch128/mnema-talk/releases) attach the Desktop DEV
Windows ZIP and checksum file; later releases do not, and new desktop builds
come from mnema-talk-desktop. Extract every file and run
`Mnema Desktop DEV.exe`. WebView2 Runtime is required. Checksums
identify package bytes; they are not publisher signatures. The desktop installer of a
release updates itself with signature-verified, newer-only updates; no
Authenticode signing exists.

The earlier published v0.7.1 EXE passed two hosted Windows runtime checks
listed below. These are evidence for that version and flow; they do not
independently qualify every later build or the Gaming integration:

- [Instance selection and language interaction](https://github.com/msch128/mnema-talk/actions/runs/37874166418): the real EXE reached a compatible HTTPS instance's canonical sign-in page.
- [Authenticated chat and logout](https://github.com/msch128/mnema-talk/actions/runs/37888635202): generated test accounts on an isolated normal server, login 200, a persisted outgoing message, a live peer message over the desktop's hub connection without history fallback, logout 204 and return to sign-in. The owned database was PostgreSQL 17.11; media storage was an in-memory test fixture.

These results qualify that limited flow. They do not qualify production-account
usage, S3 uploads, physical microphone/camera capture, system audio, device
recovery, E2EE, gaming, overlays, exclusive fullscreen or complete browser parity.
The earlier feasibility probe and native-crypto checkpoint are research
foundations, not alternate supported clients.

## Remaining acceptance, in order

The Windows-first client and its Gaming integration have been released.
Versions remain managed by the normal release pipeline. Further runtime
qualification and the remaining requirements below are still outstanding.

The Gaming integration (first released with v0.7.2) connects these surfaces to the existing client:
left-hand sidepeek with participants, speaking, mute and screen-share state;
Alt+M interactive overlay; controls for the actual own Talk; persisted,
configurable mute, deafen, push-to-talk and overlay bindings. Both surfaces
require an own connected Talk and an active detected game. See the
[Windows Gaming DEV usage and limits](desktop-app.md#windows-gaming-dev).
Topmost windows do not establish exclusive fullscreen or anti-cheat support.

The remaining fixes and acceptance work follow:

1. Client parity: Windows/WebView2 acceptance of the trusted bundle (custom
   protocol mapping, media permission prompts, gaming surfaces) and a signed
   installer. macOS and Linux clients are not started.
2. Chat messages and attachments stay readable by the server and are not deleted by age. Sealed media is verified in Chromium browsers, including calls between 0.7.25 and 0.8 tabs; it is not yet qualified in WebView2 on real Windows hardware, and no external audit has reviewed it.
3. Capacity: [`make load-test`](../loadtest/README.md) is reproducible (smoke, the 100-member `full` profile, join and reload stall checks, packet loss and jitter on the lightweight viewers, a two-hour `long` profile). On a loaded single test machine the `full` profile misses its gap and heap thresholds for 0.8 and 0.7.25 alike while all 84 viewers stay connected, so what remains is recording results on dedicated hardware with more real decrypting viewers ([capacity notes](architecture.md#capacity)). A viewer's bad reports move only that viewer to a lower screen layer (`TestOneViewersTroubleLeavesTheOtherOnTheHighLayer`); voice forwarding does not follow screen layers. The additional 250-endpoint target is deferred.
4. Independent operation. `main` is protected by a ruleset (pull request required, CI and CodeQL checks must pass, squash merge only). Review approvals are not required. Dependency advisories remain part of release checks; a green build does not mean zero risk.

[Desktop requirements](desktop-app.md), [cryptographic persistence limits](crypto-state.md)
(not the shipped media path) and [the native research checkpoint](https://github.com/msch128/mnema-talk-desktop/blob/main/experimental/README.md) describe
these boundaries in more detail. Existing coverage and security gates remain
in force.
