# Windows desktop feasibility probe

This is the first implementation stage of the desktop roadmap. It is **not a
released 0.7 client**: it does not log in, join Voice, capture media, create an
overlay, or claim E2EE. Do not use it as a replacement for the current web app.

The Vue interface is bundled locally. Native Rust commands offer two operations:

- Explicit HTTPS discovery, with system certificate validation, no redirect
  following, no credentials, bounded response size/time and one request in flight.
- On Windows, foreground-only executable inspection against a user-selected
  list of basenames. Full paths and window titles are not sent to the interface
  or a server. Covering a monitor is **not** proof of exclusive fullscreen.

The independent gaming-policy tests cover Voice/trusted-media/game/input/lock
gating and held-PTT release. A native shortcut adapter polls only explicitly
configured keys and modifiers. It is not wired into Voice or the UI: short taps
can be missed and input-availability failures need an independent media watchdog.
These tests do not qualify runtime game input or GPU overlays.
The probe reports Web API presence separately from actual media qualification.

## Build on Windows

Requires Node 26, Rust 1.99.0, the Visual Studio C++ build tools/Windows SDK and
WebView2. From the repository root:

```powershell
npm ci --prefix desktop/ui
npm --prefix desktop/ui run test
cargo test --locked --manifest-path desktop/Cargo.toml
npm --prefix desktop run build
```

The unbundled probe executable is `desktop/target/release/mnema-desktop-probe.exe`.
Signing, installer packaging and production updates are later qualification
stages. This command does not produce an official signed release.
For local development: `npm --prefix desktop run dev`.

Full checks require cargo-audit 0.22.2 (`cargo install cargo-audit --version
0.22.2 --locked`). Run `make check-desktop` for the independent desktop checks
or `make check` for all repository checks. The small `desktop/go.mod` is only a
module boundary preventing Go server checks from scanning npm Go fixtures.

Checks:

```text
cargo fmt --manifest-path desktop/Cargo.toml --check
cargo clippy --locked --manifest-path desktop/Cargo.toml --all-targets --features shell -- -D warnings
npm --prefix desktop/ui run build
```

Non-Windows hosts can build the probe UI/shell and test portable policy, but
foreground inspection explicitly reports an unsupported probe host. A cross-
target typecheck is not a Windows runtime test.

## Discovery candidate

The temporary G0 document is read from `/.well-known/mnema` on a root HTTPS
origin. Existing 0.6.1 servers do not implement it; their failure is expected.
This format is an experimental test contract, not the final versioned API:

```json
{
  "protocol": "mnema-desktop-discovery-v1",
  "community_id": "test-community",
  "api_versions": [1],
  "e2ee_required": true
}
```

Discovery success confirms document compatibility only. It neither establishes
honest server behavior nor authenticates devices and always leaves login disabled.
Subpath deployments are not supported by this initial probe. No TLS bypass or
HTTP fallback is provided.

## Windows game test

Enter the actual executable basename (not a path). Start foreground inspection,
switch to the game, then return to the probe. Its last recognized local game is
kept as diagnostic evidence; that record never enables a widget or an overlay.
Stop clears the record and sampling. No whole-system process scan occurs.

Record Windows build, CPU/GPU/driver, game/version, rendering API, display mode,
monitor/DPI and the observed result. Exclusive-fullscreen evidence requires a
separate verified rendering-mode test; window geometry cannot provide it.

Libraries and locked Rust dependency inventory: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
The app icons are generated from the project's existing `web/public/favicon.svg`.

## Distribution evidence

`node --test desktop/scripts/*.test.mjs` tests the development checksum receipt
and license inventory checker. `node desktop/scripts/collect-licenses.mjs --check`
verifies notices against the current locked dependency graphs and copied fonts.
Distribute the complete `licenses/` directory, this notice file and the root
`LICENSE` beside a development binary. Checksums are not publisher signatures;
receipts explicitly remain ineligible for the updater.

The Rust advisory scan currently reports no vulnerability-class advisories, but
retains `RUSTSEC-2024-0370` (unmaintained proc-macro-error) and
`RUSTSEC-2024-0429` (glib VariantStrIter unsoundness in the Linux shell dependency
graph). The latter blocks Linux distribution qualification pending a compatible
fix or reviewed reachability assessment. Neither package appears in the inspected
Windows or macOS graphs; this is not a clean cross-platform dependency claim.
No advisory is ignored.
