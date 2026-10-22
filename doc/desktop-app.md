# Desktop client roadmap

Status: **the desktop client is Beta** (the Windows Desktop DEV client is the furthest along and testable)
(releases of this repository through v0.7.3 attach its ZIP; later builds come
from the desktop repository), including the Gaming integration described below.
The package is unsigned (no Authenticode); the installer of a desktop release updates itself from
signed releases, see [doc/desktop-trusted-client.md](desktop-trusted-client.md#updating-the-client-itself). Earlier published-client checks cover instance
selection, login, persisted chat, live peer reception and logout. Physical media
capture, real-game/fullscreen compatibility, sealed media in WebView2 and complete browser parity
remain separate acceptance work.

The current client source is
[experimental/app](https://github.com/msch128/mnema-talk-desktop/blob/main/experimental/README.md)
in `web-client` mode, in the
[mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop) repository.
It runs the shared Vue app from a signed bundle that the client verifies and
reaches the selected instance through its own proxy; the instance supplies data,
never executable code ([doc/desktop-trusted-client.md](desktop-trusted-client.md)).
The native-crypto boundary below remains a separate requirement. The older
[feasibility probe](https://github.com/msch128/mnema-talk-desktop/blob/main/README.md)
is a historical foundation.
See [product status and evidence](product-status.md) for the binding scope.

## Installing the unsigned Beta

The desktop app is Beta and its installers are not code-signed: the project
will not buy code-signing certificates. The operating system therefore warns
before the first start. This does not mean the file is malicious, and it is no
substitute for checking it: download only from the
[desktop releases](https://github.com/msch128/mnema-talk-desktop/releases) and
compare the published checksum. The app itself still verifies its web bundle and
its updates with the project's own signatures.

**Windows (SmartScreen).** Choose **More info**, then **Run anyway**. If the
file was downloaded in a browser and still refuses to start, open its
Properties and tick **Unblock**.

**macOS (Gatekeeper).** Right-click the app, choose **Open**, then **Open**
again. Alternatively, after a blocked first start open **System Settings,
Privacy & Security** and choose **Open Anyway** for Mnema. As a last resort,
remove the quarantine flag: `xattr -dr com.apple.quarantine "/Applications/<app name>.app"`.

**Linux.** AppImage and .deb packages need no such step; make the AppImage
executable (`chmod +x`) before running it.

If you cannot accept these warnings, use the browser app, which is the fully
supported client.

## Windows Gaming DEV

The shared-client host now connects gaming surfaces to the selected instance's
actual Vue Talk state. The instance must serve the updated shared web client;
an older web client without the gaming bridge cannot activate these surfaces.

- Join Talk, then focus a detected game. The passive left-hand sidepeek shows
  participants, speaking, mute and screen sharing. It ignores mouse input.
- **Alt+M** opens the interactive overlay. It offers mute, deafen, screen-share
  controls and push-to-talk mode. Starting a share returns to the existing
  capture chooser in the main client; stopping a share uses the active Talk.
- Open the overlay settings to rebind overlay, mute, deafen and push-to-talk.
  Defaults are Alt+M, Alt+N, Alt+D and Space. Bindings persist locally.
- In the next client update, settings also include a global **Gaming overlay and
  sidepeek** switch and a **background opacity** slider (default 55%). Use the
  native **Gaming** menu in the main client or instance selector to open these
  settings even without Talk or a game, including when the overlay is disabled.
- The next client update includes a [300-game executable reference](game-detection.md).
  Additional executable basenames can be configured for other games or installation
  variants. Existing custom names and shortcuts are preserved.
- Leaving Talk, losing the connection or moving to another application hides
  both surfaces. No own Talk connection means no sidepeek or voice overlay.
- Gaming PTT grants short microphone leases on the audio rendering clock.
  A renderer stall cannot extend the last granted lease.

This is an unsigned Windows DEV integration using ordinary topmost windows,
foreground process inspection and polling of configured keys. It does not inject
into games or capture a keyboard history. A matching basename is a user-selected
candidate, not a trusted executable identity. Exclusive fullscreen rendering,
anti-cheat compatibility and real-game behavior require testing on the target
machine. Windows behavior is not a macOS/Linux gaming qualification. The
web-client mode loads the instance's web app in WebView2, so voice uses the
same browser path: microphone, camera and screen payloads are sealed in the
page before the SFU sees them. This is not yet qualified on real Windows
hardware. Chat and attachments stay readable by the server.

## Product requirements

The desktop app bundles the existing Vue/TypeScript interface locally and connects
to one self-hosted community. The community supplies API data, never executable
client code. Tauri v2 is the preferred shell. An Electron fallback requires a
verified unresolved Tauri limitation; changing shells does not itself solve
end-to-end encryption or exclusive-fullscreen overlays.

Windows is the first implementation target. macOS and Linux, including X11 and
Wayland, remain required targets. The main app and interactive overlay must expose
the complete role-appropriate web feature set. Rewriting the web app in React is
not a prerequisite.

The gaming interface has two parts:

- A configurable participant widget, initially at the top left, visible only while
  the user is in their own active Voice channel and a game is active. It shows
  participants, speaking, mute/deafen and screen-sharing status. It disappears on
  leaving Voice, losing the game context or losing trusted media state.
- An interactive overlay opened by configurable Alt+M (Option+M on macOS),
  including the widget and normal app controls, such as mute and starting a share.
  Without an active Voice channel and game, the shortcut does nothing.

The widget must not appear on the desktop or an unrelated application. Push-to-talk
and overlay bindings are configurable. Losing input monitoring, locking the session
or leaving Voice releases held push-to-talk; restoring a context must not reopen
the microphone automatically. Exclusive fullscreen is a required qualification
case. A window covering a monitor is insufficient evidence.

## Security and client boundaries

First launch requests an HTTPS instance address. Bounded discovery verifies TLS,
API and client compatibility before offering credentials. Self-hosted certificate
authorities use normal system trust; there is no certificate bypass. Switching
instances must isolate credentials, device keys, cached data and active connections.

A native session broker must preserve the browser's existing cookie, CSRF, Origin
and CSP protections. Local Tauri pages cannot reuse the browser transport by
spoofing an Origin or enabling wildcard CORS. Renderer permissions are limited to
specific commands; there is no generic filesystem, shell or arbitrary HTTP bridge.

Microphone, camera, screen sharing and system audio are sealed in the browser
before the server sees them. The server forwards those payloads without
decrypting them. There is no plaintext fallback for those tracks. Chat,
reactions and attachments stay readable by the server and are not deleted by
age. Connection and routing metadata remain visible to the server.

A shared session and media owner coordinates the main window, widget and overlay.
Opening the overlay must not create a second microphone, media connection or
independent cryptographic sender context. Local search, previews and attachment
validation stay on the client.

## Compatibility, development builds and updates

The server advertises versioned compatibility states: supported, deprecated or
unsupported. API compatibility is separate from encryption capability and device
trust. A discovery response alone cannot enable an encrypted session.

Administrators may either mirror official client packages or direct users to
GitHub Releases. A mirror is a delivery location, not a release-signing authority.
The client must independently verify package authenticity and prevent rollback or
stale metadata from installing an unsafe release.

No Windows code-signing certificate exists and none will be bought (Beta; see [installing the unsigned Beta](#installing-the-unsigned-beta)). The self-update is signed separately
(minisign, key pinned in the client) and installs only verified, newer versions.
Development builds must be clearly identified as unsigned. Automatic installation
of unverifiable updates remains disabled. Production distribution additionally
requires updater signatures, platform signing requirements, complete license
notices and reproducible source/package identification.

## Delivery gates

| Gate | Required result |
|---|---|
| G0 — feasibility | Real target-hardware evidence for fullscreen overlay, press/release input and encrypted media in each required platform route |
| G1 — transport | Bounded discovery, explicit compatibility, isolated native sessions and narrow IPC |
| G2 — crypto | Reviewed provider/protocol selection, device trust, storage and recovery model |
| G3 — shared client | Locally bundled web feature parity with a single session/media owner |
| G4 — gaming | Game detection, gated participant widget, interactive overlay, rebindable shortcuts and safe input cleanup |
| G5 — encryption | All protected content paths, negative tests, membership changes, recovery and legacy-data handling |
| G6 — distribution | Verified updates, optional self-host mirrors, platform packages and licensing |
| G7 — qualification | Coverage gates, two independent code reviews, security review and actual platform/game tests |
| G8 — release | Normal reviewed release pipeline and verified NAS deployment with backup/restore evidence |

Design and isolated implementation work can proceed while hardware access is
pending. A missing hardware or security gate remains open; it cannot be replaced
with a successful build or a development deadline.

## Historical foundation evidence and limits

The following describes the earlier probe and native foundations, not the
released web-client mode. Current runtime evidence is listed in
[product status](product-status.md).


The probe includes HTTPS-only discovery and foreground-only Windows executable
inspection. Only explicitly selected executable basenames are returned; full paths,
window titles and unrelated process lists are not transmitted. Portable policy tests
exercise Voice/media/game/input/lock gating. The UI distinguishes API presence from
actual capture or encryption support.

The native-session foundation includes hash-only token persistence, bounded
session families, single-use refresh rotation with committed replay revocation,
and a shared browser/native credential and lockout policy. Its isolated HTTP
handler rejects browser Origin/cookie traffic and provides explicit bearer
authentication. These endpoints are not mounted in the server router; the probe
still cannot log in. Migration 0015 adds the storage tables and runs through the
normal forward-only migration mechanism when this server candidate starts.

The portable game-window owner binds focus operations to its native owner,
session, Voice and window generations. Stale operations and loss of context clear
views and release push-to-talk. This policy does not create overlay windows or
prove that OS events, game-window lifetime and media shutdown are wired correctly.

Windows CI builds and packages the unsigned executable with dependency notices.
Its checksum receipt identifies development artifact bytes and packaging inputs;
it is not a publisher signature, updater authorization or runtime test result.

A Windows CI build can establish compilation and unit-test results. It does not
establish game compatibility, anti-cheat acceptance, input delivery or exclusive
fullscreen operation. Tests must record OS, hardware, drivers, game/rendering mode,
display configuration and measured results. Process injection or anti-cheat
circumvention is not an approved shortcut.

See the [probe instructions](https://github.com/msch128/mnema-talk-desktop/blob/main/README.md) for the current build and testing
scope, and [SECURITY.md](../SECURITY.md) for the released server security model.
