# Experimental desktop third-party notices

This development source checkpoint uses the pinned dependencies in app/Cargo.lock and the canonical ../../web/package-lock.json. The full original package license and notice texts are in licenses/texts/; licenses/manifest.json maps package/version/checksum to those texts. The graph includes optional and build dependencies conservatively and does not claim measured linking.

OpenMLS source is pinned to commit 3a3e35de3feeca8f6605143c464d5452ae584d43 under its original MIT license. SFrame source is pinned to commit c11d7f452c6c947ed5ea6a13c35ce600ba1584e2 under its original MIT/Apache-2.0 choices. Both complete source workspaces and original notices are retained in vendor/. The complete Rust 1.99.0 standard-library copyright notice, Inter OFL attribution, and build CSS notices are retained in licenses/.

SQLCipher/OpenSSL source packages and their original notices are included in the locked registry inventory. No signing credentials or automatic updater are configured.

The copied existing web DeepFilterNet WASM/model has an additional provenance and transitive-license qualification gap. Its existing notices are preserved; this checkpoint makes no claim that a final binary distribution license closure is complete.

See the project root THIRD_PARTY_NOTICES.md for existing web/backend dependencies and LICENSE for project terms.
