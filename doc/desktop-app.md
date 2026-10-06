# Desktop app (plan)

Status: **plan only, no code yet.** This document compares the options and
proposes an approach for a Mnema Talk desktop app.

## Goal

The main reason for a desktop app is **global hotkeys**: push-to-talk, mute
and deafen must work while another application (a game, an IDE) has the
focus. In the browser that is impossible: the web app listens to `keydown` /
`keyup` on its own window only and deliberately releases push-to-talk when
the window loses focus or is hidden
([`useWebRTC.js`](../web/src/composables/useWebRTC.js), `setupPttListeners`,
`releasePtt`), so a microphone can never stay open by accident.

Secondary goals: a dock/taskbar icon with unread badge, start with the
system, notifications that work like a native app, no browser tab to lose.

Non-goals: a second UI. The desktop app shows the same web app the server
already serves; there are no desktop-only features beyond the hotkeys and
shell integration.

## Requirements

- Loads the user's own Mnema URL (entered once, remembered); one server per
  install is enough (single-server model).
- Full voice features of the browser version: microphone, camera, **screen
  share with system audio**, H.264 hardware encoding, picture-in-picture.
- Global hotkeys with **press and release** events (push-to-talk needs the
  release), configurable, without swallowing the key for other apps where
  possible.
- Windows 10/11 and macOS first; Linux (X11 and Wayland) best effort.
- Signed builds and automatic updates of the shell.
- No weakening of the web app's security model (CSP, cookies, CSRF origin
  check, see [SECURITY.md](../SECURITY.md)).

## Tauri vs. Electron

| | Electron | Tauri 2 |
|---|---|---|
| Engine | Bundled Chromium (same on every OS) + Node.js | System webview: WebView2 (Chromium) on Windows, WKWebView (WebKit) on macOS, WebKitGTK on Linux; Rust core |
| Installer size | ~80–120 MB | ~3–15 MB (Windows may need the WebView2 runtime, preinstalled on Windows 11) |
| Idle RAM | ~150–300 MB (Chromium main, GPU, renderer processes) | Lower shell overhead; the webview itself still needs ~100–200 MB on Windows (WebView2 is Chromium) |
| WebRTC | Chromium's full stack, identical to Chrome: `getUserMedia`, `getDisplayMedia`, H.264 hardware encoding, simulcast, stats | Depends on the OS webview, see below |
| Screen capture | `session.setDisplayMediaRequestHandler` + `desktopCapturer` (we draw our own source picker); system audio via loopback on Windows; on macOS depends on the Electron version and ScreenCaptureKit | WebView2: works like Chromium (needs the permission event handled). WKWebView: `getDisplayMedia` only on recent macOS versions and without system audio; must be verified per macOS release. WebKitGTK: WebRTC support is recent, often built without it by distributions; screen capture goes through the PipeWire portal and is unreliable |
| Global shortcuts | `globalShortcut` (key **down** only, no release event, consumes the combination); push-to-talk needs a native keyboard hook (e.g. `uiohook-napi`) for press and release | `tauri-plugin-global-shortcut` reports pressed **and** released (consumes the combination); a hook (e.g. `rdev`) for non-consuming keys |
| Wayland | Global shortcuts only through the xdg-desktop-portal GlobalShortcuts portal, support varies by Electron version and compositor | Same limitation (portal or nothing) |
| Auto-update | Mature: `electron-updater` (GitHub Releases feed), Squirrel on Windows/macOS | Built-in updater plugin with mandatory signature (own key pair) on top of OS code signing |
| Code signing | Windows: Authenticode (OV/EV certificate or Azure Trusted Signing) to avoid SmartScreen warnings. macOS: Developer ID + notarisation (Apple Developer Program, yearly fee); hardened runtime with camera/microphone/screen-recording entitlements | Same OS requirements; plus the updater's own signing key |
| Security surface | Node.js in the main process; renderer must be locked down (context isolation, sandbox, no `nodeIntegration`) | Rust core, no Node; IPC limited by capability files, remote URLs need an explicit capability |
| Toolchain | JS/TS only, fits the existing web stack | Rust toolchain plus JS; three webviews to test |

### What matters for Mnema Talk

The deciding factor is **screen sharing**. Mnema Talk's core feature is
source-quality screen sharing with system audio and H.264 hardware encoding,
tuned for Chromium ([`tuneScreenOffer`](../web/src/composables/useWebRTC.js),
the stream quality menu). Electron ships exactly the engine the web app is
tested against (CI e2e runs Chromium). With Tauri, Windows would be fine
(WebView2), but macOS (WKWebView) and Linux (WebKitGTK) would behave like a
different browser with known gaps in `getDisplayMedia`, system audio and
codec support, and would need their own testing and workarounds.

Size and RAM favour Tauri, but for a voice/screen-share client the
difference is small in practice: the webview dominates the memory either
way, and an 80–120 MB download once is acceptable for a desktop client.

## Recommendation

**Electron**, as a thin shell around the existing web app:

- identical WebRTC behaviour on all platforms (one engine to test, the same
  as CI),
- reliable screen capture with our own source picker and Windows system
  audio,
- mature signing and auto-update tooling,
- the team's stack stays JavaScript.

Push-to-talk uses a native keyboard hook (`uiohook-napi` or an equivalent
maintained module) for press and release, because `globalShortcut` has no
release event; mute and deafen toggles can use `globalShortcut`. Revisit
Tauri if WKWebView/WebKitGTK screen capture matures or if Windows-only is
acceptable.

## Architecture

```
┌──────────── Electron main process ────────────┐
│ window (BrowserWindow) ─ loads https://<your Mnema URL>
│ keyboard hook / globalShortcut ─► IPC events  │
│ permission + display-media handlers           │
│ tray, badge, notifications, auto-update       │
└───────────────┬───────────────────────────────┘
                │ contextBridge (preload, sandboxed)
                ▼
   window.mnemaDesktop = {
     onPushToTalk(cb),   // cb(pressed: boolean)
     onToggleMute(cb), onToggleDeafen(cb),
     setBadge(count), version
   }
                │
                ▼
   Web app (served by the Mnema server, unchanged origin)
   voice store: isPttPressed, toggleMute(), toggleDeafen()
```

- **Thin shell.** The window loads the user's Mnema URL as a normal
  top-level page (the server's CSP sets `frame-ancestors 'none'`, so no
  iframe). Session cookie, CSRF origin check and CSP work exactly as in a
  browser. The web app is updated by the server, the shell only by its own
  updater.
- **First start.** A small local page asks for the server URL (must be
  `https://`, or `http://localhost` for development), stores it, then loads
  it.
- **Hotkeys → existing logic.** The preload exposes a minimal API through
  `contextBridge`. The web app checks for `window.mnemaDesktop` and, when
  present:
  - push-to-talk events set `voiceStore.isPttPressed` (the same state the
    keyboard handler in [`useWebRTC.js`](../web/src/composables/useWebRTC.js)
    sets, including the PTT sounds),
  - mute and deafen call `toggleMute()` / `toggleDeafen()` of the
    [voice store](../web/src/stores/voice.js),
  - the blur/visibility release of push-to-talk is skipped while the desktop
    bridge owns the key (the hook delivers the release even in the
    background), and the settings dialog
    ([`AudioSettingsModal.vue`](../web/src/components/AudioSettingsModal.vue))
    records the global key through the bridge.
  The browser version stays unchanged when the bridge is absent.
- **Background behaviour.** `backgroundThrottling: false` for the window, so
  audio, speaking detection and the WebSocket keep full speed while the app
  is minimised.
- **Screen picker.** `setDisplayMediaRequestHandler` shows our own picker
  (screens and windows with thumbnails from `desktopCapturer`), with
  "share audio" on Windows.

## Security considerations

- Renderer locked down: `contextIsolation: true`, `sandbox: true`,
  `nodeIntegration: false`, no `webviewTag`, no remote module; the preload
  exposes only the functions above (no generic `send`/`invoke`).
- **Origin pinning.** Navigation (`will-navigate`, redirects) is allowed only
  within the configured origin; `setWindowOpenHandler` opens every other link
  in the system browser. The bridge is only exposed when the page's origin is
  the configured one.
- **Permissions.** `setPermissionRequestHandler` /
  `setPermissionCheckHandler` grant `media` and `display-capture` (and
  notifications) to the configured origin only; everything else is denied.
- **TLS.** No certificate-error overrides; a self-signed server needs a
  properly installed CA, not an "ignore" switch.
- **Keyboard hook privacy.** A global hook sees every key press. Process
  only the configured key in the main process, never log or forward other
  keys, and document this. macOS requires the Accessibility / Input
  Monitoring permission for it; explain that in the first-run flow.
- **Updates.** Signed releases only (Authenticode, Developer ID +
  notarisation); the updater accepts only signed artifacts from this
  repository's GitHub Releases. Electron versions follow upstream security
  releases promptly (Chromium fixes).
- **Repository rules.** Signing keys and certificates live in CI secrets
  only, never in the repository (see [AGENTS.md](../AGENTS.md)).

## Milestones and effort

| # | Milestone | Content | Effort |
|---|---|---|---|
| 1 | Shell prototype | Electron window loading a configured URL, origin pinning, permission handlers, first-run URL dialog | 2–3 days |
| 2 | Screen share | `setDisplayMediaRequestHandler`, own source picker, Windows system audio, verification with the stream quality menu | 3–4 days |
| 3 | Global hotkeys | Keyboard hook for push-to-talk (press/release), mute/deafen shortcuts, bridge API, web app integration behind `window.mnemaDesktop`, key recording in audio settings, tests | 3–5 days |
| 4 | Shell integration | Tray, unread badge, notifications, start with system, single instance | 2–3 days |
| 5 | Packaging and updates | electron-builder for Windows (NSIS/MSI) and macOS (dmg, universal), code signing and notarisation in CI, auto-update from GitHub Releases | 3–5 days (plus certificate procurement) |
| 6 | Linux (optional) | AppImage/deb, X11 hotkeys, Wayland via portal where available | 2–4 days |

Total for Windows + macOS: roughly **3–4 weeks** of focused work, plus lead
time for the Apple Developer account and a Windows signing certificate.

## Open questions

- Is Windows-only acceptable for a first release (simplifies signing and
  makes Tauri a viable lighter alternative)?
- Should push-to-talk support mouse buttons (common for gamers)? The hook
  approach supports it; `globalShortcut` would not.
- Where does the desktop app live: this repository (`desktop/`, own release
  tag prefix) or a separate repository with its own release cycle?
