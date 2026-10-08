# Desktop client roadmap

Status: implementation started; **0.7 is not released or qualified**. The current
[`desktop/`](../desktop/README.md) is an unsigned feasibility probe. It cannot
log in, join Voice, capture media or display an overlay. Existing 0.6.1 browser
sessions and transport encryption do not provide the proposed end-to-end encryption.

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

End-to-end encryption is required for chat, reactions, attachments, microphone,
camera, screen sharing and system audio. The server must not receive content keys
or plaintext content; there is no plaintext fallback or admin decryption bypass.
Device admission, removal, recovery, persistent nonce safety and authenticated
membership need a reviewed protocol and provider. Connection and routing metadata
remain a separate documented disclosure boundary.

A shared session and media owner coordinates the main window, widget and overlay.
Opening the overlay must not create a second microphone, media connection or
independent cryptographic sender context. Local search, previews and attachment
validation must respect the encrypted-content boundary.

## Compatibility, development builds and updates

The server advertises versioned compatibility states: supported, deprecated or
unsupported. API compatibility is separate from encryption capability and device
trust. A discovery response alone cannot enable an encrypted session.

Administrators may either mirror official client packages or direct users to
GitHub Releases. A mirror is a delivery location, not a release-signing authority.
The client must independently verify package authenticity and prevent rollback or
stale metadata from installing an unsafe release.

No Windows signing certificate or updater signing setup currently exists.
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

## Current evidence and limits

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

See the [probe instructions](../desktop/README.md) for the current build and testing
scope, and [SECURITY.md](../SECURITY.md) for the released server security model.
