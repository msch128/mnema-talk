# Native desktop development checkpoint

This directory preserves the current native desktop implementation while it is
being integrated. It is an experimental source checkpoint, not a released 0.7
application. The production server and released web application remain separate.

The client uses the existing Mnema Vue interface in a local Tauri webview. Its
native broker selects an HTTPS community, keeps authentication credentials out
of the renderer, and exposes fixed commands for account and community metadata.
The protected-chat implementation combines a native confirmation dialog,
protected operating-system storage, SQLCipher and the pinned OpenMLS provider.
The UI presents public results; it does not issue cryptographic permissions.

The source was copied from the independently reviewed development candidate.
Actual HTTPS lifecycle tests cover selection, credential rotation and rejected
login recovery. That review permits a controlled macOS test with one owned
session. The full native confirmation and protected-chat GUI qualification is
still in progress. Delayed actions across successive logins to the same profile
require additional native authentication-lifetime binding before general use.

Native voice, complete web feature parity, game detection and interactive gaming
surfaces are being integrated separately. A Windows executable has not yet been
linked and qualified. Development builds have no code-signing certificate or
approved automatic updater. This checkpoint does not activate experimental
server routes or deploy an application.

## Source layout

- `app/`: native Tauri host and local-window command permissions.
- `crates/`: the canonical broker, native storage and cryptographic integration.
- `vendor/`: pinned provider source required by the native dependency graph.
- `ui/`: the existing Mnema interface and native transport adapters.

Install the locked UI dependencies with `npm ci --prefix ui`, then run
`npm run check --prefix ui`. Native builds use Rust 1.99.0 and the committed
Cargo lockfile. Build the local application with
`cargo build --locked --manifest-path app/Cargo.toml --features shell,native-crypto`.
Operating-system toolchains are required for native dependency compilation.
The synthetic transport and media fixture features are disabled by default.

License and source-provenance records accompany the native graph. Existing
vendored noise-filter assets retain their upstream notices; attribution of every
transitive component in that prebuilt WASM is still being verified before binary
distribution. No test account credentials, local database, captured user data,
native keychain, build artifact or private handoff belongs in this checkpoint.
