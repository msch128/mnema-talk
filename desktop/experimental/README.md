# Native desktop development checkpoint

This directory preserves the current native desktop implementation while it is
being integrated. It is an experimental source checkpoint, not a released 0.7
application. The production server does not enable its native preview routes.

The client builds the canonical `web/src` Vue interface into `web/dist` and loads
that same bundle in a local Tauri webview. Browser requests retain their existing
cookie transport; the desktop runtime uses fixed native commands. Its
native broker selects an HTTPS community, keeps authentication credentials out
of the renderer, and exposes fixed commands for account and community metadata.
The protected-chat implementation combines a native confirmation dialog,
protected operating-system storage, SQLCipher and the pinned OpenMLS provider.
The UI presents public results; it does not issue cryptographic permissions.

Native operations capture both profile and authentication lifetime before
admission and retain the original authenticated scope through queue processing
and publication. Typed chat events carry native author, receipt, timestamp and
revision data into the existing ChatArea, MessageRow and ThreadSidebar. Renderer
inputs supply text and target claims, never cryptographic authority.

A controlled macOS DEV run against an isolated test instance exercised discovery,
login, native OS root confirmation, message creation, edit, quoted reply,
reaction, thread creation and deletion. This demonstrates a single-owner local
flow. It does not qualify multi-device enrollment, persistent/restored history,
native voice or complete E2EE. The combined integration still requires
independent source review and the full repository checks before publication.

Native voice, complete web feature parity, game detection and interactive gaming
surfaces are being integrated separately. A Windows executable has not yet been
linked and qualified. Development builds have no code-signing certificate or
approved automatic updater. This checkpoint does not activate experimental
server routes or deploy an application.

The shared client rejects desktop call joins, automatic call restoration, camera
and screen sharing before capture or connection-state changes until the native
protected-media owner is integrated. Browser voice behavior remains available.
The browser's server-version reload banner is omitted for embedded desktop
assets, because reloading cannot install a new desktop build.

## Source layout

- `app/`: native Tauri host and local-window command permissions.
- `crates/`: the canonical broker, native storage and cryptographic integration.
- `vendor/`: pinned provider source required by the native dependency graph.
- `../../web/`: the canonical interface and runtime-specific transport adapters.
- `ui/`: historical checkpoint, no longer the application build input.

From the repository root, install the shared frontend with `npm ci --prefix web`,
then run `npm run build:client:dev --prefix desktop`. Native builds use Rust
1.99.0 and the committed Cargo lockfile. The equivalent native command after
building `web/dist` is
`cargo build --locked --manifest-path desktop/experimental/app/Cargo.toml --features shell,native-crypto,custom-protocol`.
This produces an unsigned DEV executable without configuring an automatic updater.
Operating-system toolchains are required for native dependency compilation.
The synthetic transport and media fixture features are disabled by default.

The `Shared desktop client DEV builds` workflow runs on matching pushes to
`main` and `codex/**`, and can also be started manually. It builds this same
`web/dist` and tests and links `app/` on Windows first. Successful Windows
checks are followed by macOS and Linux builds. Successful jobs upload explicitly
unsigned DEV artifacts with source revision/hash receipts and dependency
notices, retained for seven days. macOS and Linux tar archives preserve executable
permissions. This creates no release or updater package. Linux native key
custody remains unavailable. Linking alone does not qualify runtime behavior
or games; the artifacts record the outstanding qualifications.

License and source-provenance records accompany the native graph. Existing
vendored noise-filter assets retain their upstream notices; attribution of every
transitive component in that prebuilt WASM is still being verified before binary
distribution. No test account credentials, local database, captured user data,
native keychain, build artifact or private handoff belongs in this checkpoint.
