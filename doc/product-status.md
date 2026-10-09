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

HTTPS protects browser traffic in transit. Chat content and attachments remain
accessible to the server; media uses DTLS-SRTP between each client and the SFU.
These paths are **not end-to-end encrypted**. See [SECURITY.md](../SECURITY.md).

## Experimental Windows desktop path

The published v0.7.1 Windows package is an unsigned Tauri DEV client. Its local
instance selector checks HTTPS and `web_client_api: 1`; it then opens the same
Vue app served by the chosen instance. That remote page uses same-origin
browser cookies, REST and WebSockets. The v0.7.1 remote page receives no native
IPC permissions. The gaming integration adds only a session-bound, selected-origin
Talk-state sync command; local gaming surfaces hold their separate display/control
permissions. It does not grant the instance cryptographic or selector authority.
The selected instance supplies executable web code as well as community data.
A locally bundled protected communication client remains an outstanding target;
the current web-client mode does not establish that boundary.

Download the complete Desktop DEV Windows ZIP and checksum file from
[GitHub Releases](https://github.com/msch128/mnema-talk/releases), extract every
file, and run `Mnema Desktop DEV.exe`. WebView2 Runtime is required. Checksums
identify package bytes; they are not publisher signatures. No desktop automatic
updater or signing setup exists.

The actual published v0.7.1 EXE passed two hosted Windows runtime checks:

- [Instance selection and language interaction](https://github.com/msch128/mnema-talk/actions/runs/37874166418): the real EXE reached a compatible HTTPS instance's canonical sign-in page.
- [Authenticated chat and logout](https://github.com/msch128/mnema-talk/actions/runs/37888635202): generated test accounts on an isolated normal server, login 200, a persisted outgoing message, a live peer message over the desktop's hub connection without history fallback, logout 204 and return to sign-in. The owned database was PostgreSQL 17.11; media storage was an in-memory test fixture.

These results qualify that limited flow. They do not qualify production-account
usage, S3 uploads, physical microphone/camera capture, system audio, device
recovery, E2EE, gaming, overlays, exclusive fullscreen or complete browser parity.
The earlier feasibility probe and native-crypto checkpoint are research
foundations, not alternate supported clients.

## Remaining acceptance, in order

The Windows-first 0.7.1 delivery precedes these requirements. Versions remain
managed by the normal release pipeline; this list is an acceptance sequence,
not a claim that later releases are available.

For 0.7.2: integrate the Windows gaming surfaces with the existing client:
left-hand sidepeek with participants, speaking, mute and screen-share state;
Alt+M interactive overlay; controls for the actual own Talk; persisted,
configurable mute, deafen, push-to-talk and overlay bindings. Both surfaces
require an own connected Talk and an active detected game. See the
[Windows Gaming DEV usage and limits](desktop-app.md#windows-gaming-dev).
Topmost windows do not establish exclusive fullscreen or anti-cheat support.

For 0.7.3, the remaining fixes and acceptance work follow:

1. Keep documentation, release downloads and reachable features consistent; address client defects and remaining platform parity.
2. Complete a protected chat flow between independent users/devices: authorized device admission, device removal, restart and recovery. Removed devices receive no future content; the server receives neither content keys nor a plaintext fallback. Extend the reviewed boundary to attachments and every media source.
3. Verify receiver-specific quality adaptation: degrading one viewer must not reduce other viewers' quality or disrupt audio. Then measure a reproducible one-publisher/99-viewer profile with real decrypting clients, difficult networks and long sessions. Other source/room profiles need separate results. The additional 250-endpoint target is deferred.
4. Validate independent operation and enforce CI/review requirements. Dependency advisories remain part of release checks; a green build does not mean zero risk.

[Desktop requirements](desktop-app.md), [cryptographic persistence limits](crypto-state.md)
and [the native research checkpoint](../desktop/experimental/README.md) describe
these boundaries in more detail. Existing coverage and security gates remain
in force.
