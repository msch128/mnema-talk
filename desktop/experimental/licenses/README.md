# Experimental desktop license sources

This source checkpoint includes full original notices for a conservative locked Rust graph and production npm dependencies. The inventory does not claim every dependency is linked. Keep this directory and the project LICENSE with any later development distribution.

The package manifest binds the app Cargo.lock and UI package-lock.json. The scripts/collect-licenses.mjs collector verifies full license bytes from installed registry archives and explicitly pinned upstream overrides. It performs no downloads, signing or updater installation. Run it after installing the locked dependencies.

OpenMLS and SFrame retain their full pinned source and original notices in vendor/. Rust standard-library copyright and Inter OFL notices are preserved separately. Upstream copyright contacts are public attribution, not deployment credentials.

The existing DeepFilterNet WASM/model provenance and complete transitive license closure require additional qualification before a distributable binary can be claimed complete. Existing texts and component notices are retained; a package inventory alone does not close that gap.

The source checkpoint is development work with fixture features disabled by default. It is not a signed release, supported desktop client, or verified updater package.
