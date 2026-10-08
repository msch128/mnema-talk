# Desktop development build license bundle

Distribute this entire directory, `../THIRD_PARTY_NOTICES.md`, and the project's
root `LICENSE` with a desktop probe binary. The license inventory in the UI does
not replace these full texts. These files contain third-party public copyright
and attribution notices; addresses inside those upstream notices are not Mnema
deployment contacts or credentials.

`manifest.json` maps exact locked Rust packages and production npm dependencies
to their original license and notice texts. `texts/<sha256>.txt` preserves those
texts verbatim and deduplicates identical bytes. The package graph is deliberately
conservative: it includes build-only and optional platform Rust crates, and npm
packages absent from the final Vite bundle. It does not describe measured linking.

Most texts come directly from the installed registry archives. A few crate
archives omit their upstream workspace's license files. For those,
`upstream-overrides.json` records files retrieved at the exact Git commit declared
by the crate's `.cargo_vcs_info.json`, with immutable URLs and SHA-256 hashes.
`libappindicator-sys` and the GNU import-library winapi crates use license texts
from their matching published upstream workspace sibling. `r-efi` stores its MIT
permission text and attribution in `AUTHORS`; we select that MIT option. The
Objective-C crates' upstream `LICENSE.md` selects MIT or offers MIT as an option
but links to its full terms; its notice is accompanied by the standard MIT text
from a pinned SPDX license-list release. No copyright holder has been invented
to fill the standard template.

The `selectors` sources name MPL-2.0 without bundling its text. Its entry preserves
the complete standard MPL-2.0 text from the locked `cssparser` archive. Unmodified
source for each MPL-covered dependency is available through the exact crate
version link in `manifest.json` and the inventory. Source changes to MPL-covered
files must be made available under MPL-2.0 before distributing a modified build.
Do not remove existing copyright, license, or patent notices from that source.

`RUST-COPYRIGHT-library.html` is the complete notice file supplied with the pinned
Rust 1.99.0 toolchain for its standard library and built-in dependencies. Its
provenance and digest are in `supplemental.json`. Refresh and review this file when
the pinned toolchain changes; Cargo's dependency inventory alone does not cover
the standard library.

`INTER-OFL.txt` preserves the SIL Open Font License and original attribution for
the unchanged Inter Latin 400/500/600 WOFF2 files copied from the existing locked
`@fontsource/inter` 5.3.0 web dependency. Their upstream asset hashes are listed
in `supplemental.json`; this does not add an npm runtime dependency to the probe.
The collector verifies the actual copied font bytes and accompanying OFL against
these hashes, so a font replacement requires a reviewed provenance update.
Fonts must remain accompanied by their copyright notice and OFL license, and
must not be sold by themselves. Respect the license's reserved-name requirements
if a later change modifies the font software.

To regenerate after installing the locked npm and Cargo dependencies:

```sh
node desktop/scripts/collect-licenses.mjs
node desktop/scripts/collect-licenses.mjs --check
```

The collector also accepts a `cargo metadata --locked --features shell
--format-version 1` JSON file as its first argument. Its output contains no local
registry path, user directory, machine name, or build timestamp. Regeneration and
verification are offline; resolving new omitted licenses requires a separate
reviewed addition to the pinned override file. A missing notice fails the check.

Operating-system libraries are separate from this source graph. The probe expects
an already-installed Windows WebView2 runtime and does not redistribute that
runtime. Any future installer that includes or downloads WebView2 must satisfy
Microsoft's applicable runtime redistribution terms. macOS frameworks and Linux
system libraries are not copied into this bundle. Assess their redistribution
requirements if future packages bundle them.

This is license evidence for a development build, not a code-signing credential,
security audit, or approval of an official updater package.
