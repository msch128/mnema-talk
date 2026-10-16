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
payloads and cannot read them. An active or compromised server is not
excluded (it serves the code and asserts group membership), and metadata such
as who speaks when stays visible. The transport underneath is DTLS-SRTP. See [SECURITY.md](../SECURITY.md).

## Experimental Windows desktop path

The Windows desktop app lives in the separate
[mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop) repository
and is an unsigned Tauri DEV client, including the Gaming integration described
in the [usage guide](desktop-app.md#windows-gaming-dev). Its local instance
selector checks HTTPS and `web_client_api: 1`; it then opens the same Vue app
served by the chosen instance inside WebView2. That remote page uses same-origin
browser cookies, REST and WebSockets and receives no native IPC permissions
beyond the gaming integration's session-bound, selected-origin Talk-state sync
command; local gaming surfaces hold their separate display/control permissions.
It does not grant the instance cryptographic or selector authority.
The selected instance supplies executable web code as well as community data.
A locally bundled protected communication client remains an outstanding target;
the current web-client mode does not establish that boundary.

Voice in the desktop client uses the instance's browser path, so microphone,
camera and screen payloads are sealed in WebView2 by the same code as in a
browser. This is not yet qualified on real Windows hardware.

Releases of this repository through
[v0.7.3](https://github.com/msch128/mnema-talk/releases) attach the Desktop DEV
Windows ZIP and checksum file; later releases do not, and new desktop builds
come from mnema-talk-desktop. Extract every file and run
`Mnema Desktop DEV.exe`. WebView2 Runtime is required. Checksums
identify package bytes; they are not publisher signatures. No desktop automatic
updater or signing setup exists.

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

1. Keep documentation, release downloads and reachable features consistent; address client defects and remaining platform parity.
2. Chat messages and attachments stay readable by the server and are not deleted by age. Microphone, camera, screen video and screen audio are encrypted in the browser against an honest server, not an active one; the safety code in the connection details lets members compare keys. The desktop client reuses the browser path in WebView2, which is not yet qualified on real Windows hardware.
3. Verify receiver-specific quality adaptation: degrading one viewer must not reduce other viewers' quality or disrupt audio. Then extend the first load tests ([capacity notes](architecture.md#capacity): one publisher to 99 members, real decrypting clients up to 24 viewers) to a reproducible one-publisher/99-viewer profile with real decrypting clients, difficult networks and long sessions. Other source/room profiles need separate results. The additional 250-endpoint target is deferred.
4. Validate independent operation. `main` is protected by a ruleset (pull request required, CI and CodeQL checks must pass, squash merge only). Review approvals are not required. Dependency advisories remain part of release checks; a green build does not mean zero risk.

[Desktop requirements](desktop-app.md), [cryptographic persistence limits](crypto-state.md)
and [the native research checkpoint](https://github.com/msch128/mnema-talk-desktop/blob/main/experimental/README.md) describe
these boundaries in more detail. Existing coverage and security gates remain
in force.
