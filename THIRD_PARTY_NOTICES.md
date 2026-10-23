# Third-party notices

Mnema Talk is licensed under the GNU AGPL-3.0 (see `LICENSE`). It is built
from, and ships, the open-source software listed here. The license texts are
in each project's repository and, for Go modules and npm packages, in the
module or package itself. The legal dialog in the app shows a summary of this
list (`web/src/lib/thirdParty.ts`).

Keep this file current when dependencies change. `go list -deps -f
'{{with .Module}}{{.Path}} {{.Version}}{{end}}' ./cmd/server | sort -u`
lists the Go modules compiled into the binary; `web/src/lib/thirdParty.test.ts`
fails when a web dependency is missing from the in-app list.

## Web app (shipped to every browser)

The experimental native desktop source checkpoint now lives in
[mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop).
Its dependency inventory is
[experimental/THIRD_PARTY_NOTICES.md](https://github.com/msch128/mnema-talk-desktop/blob/main/experimental/THIRD_PARTY_NOTICES.md).
Its locked Rust and UI graph includes the pinned OpenMLS and SFrame providers;
the corresponding full license texts are preserved under
`experimental/licenses/` in that repository. This source checkpoint is not a desktop
binary release.

| Component | Version | License |
|---|---|---|
| [Vue](https://vuejs.org) (`vue`, incl. `@vue/*` runtime) | 3.5.44 | MIT |
| [Pinia](https://pinia.vuejs.org) | 4.1.0 | MIT |
| [Lucide](https://lucide.dev) (`@lucide/vue`) | 1.57.0 | ISC |
| [Inter](https://rsms.me/inter/) (`@fontsource/inter`) | 5.3.0 | OFL-1.1 |
| [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (`@fontsource/jetbrains-mono`) | 5.3.0 | OFL-1.1 |
| [emoji-picker-element](https://github.com/nolanlawson/emoji-picker-element) | 1.29.1 | Apache-2.0 |
| [emoji-picker-element-data](https://github.com/nolanlawson/emoji-picker-element-data) (emoji names and keywords built from [Emojibase](https://emojibase.dev) data, MIT, and [Unicode CLDR](https://cldr.unicode.org) annotations, Unicode License v3) | 1.8.0 | Apache-2.0 |
| [highlight.js](https://highlightjs.org) (syntax highlighting of code blocks in messages; core build with a fixed language list) | 11.11.1 | BSD-3-Clause |
| [Swagger UI](https://github.com/swagger-api/swagger-ui) (`swagger-ui-dist`, only on the API reference page `/api/docs`; NOTICE: "swagger-ui, Copyright 2020-2021 SmartBear Software Inc.") | 5.33.2 | Apache-2.0 |
| [Tailwind CSS](https://tailwindcss.com) (only the generated CSS is shipped) | 3.4.19 | MIT |
| [ts-mls](https://github.com/LukaJCB/ts-mls) | 1.6.4 | MIT |
| [@noble/ciphers](https://github.com/paulmillr/noble-ciphers) | 2.1.1 | MIT |
| [@noble/hashes](https://github.com/paulmillr/noble-hashes) | 2.3.0 | MIT |
| [@noble/curves](https://github.com/paulmillr/noble-curves) | 2.0.1 | MIT |
| [qrcode-generator](https://github.com/kazuhikoarase/qrcode-generator) (draws the QR code of the two-factor setup in the browser) | 2.0.4 | MIT |

`ts-mls@1.6.4` depends on `@hpke/core@1.9.0` (MIT). That package is not a direct dependency.

### Bundled inside Swagger UI

The Swagger UI build ships these components; their license texts are in
`swagger-ui-dist/swagger-ui-es-bundle.js.LICENSE.txt`, which the web build
copies to `/licenses/swagger-ui/bundled-components.txt` (with Swagger UI's
`LICENSE` and `NOTICE` next to it).

| Component | License |
|---|---|
| React, react-dom, scheduler, use-sync-external-store (Meta Platforms) | MIT |
| [Immutable.js](https://github.com/immutable-js/immutable-js) | MIT |
| [DOMPurify](https://github.com/cure53/DOMPurify) | Apache-2.0 OR MPL-2.0 |
| [classnames](https://github.com/JedWatson/classnames), [deep-extend](https://github.com/unclechu/node-deep-extend), [buffer](https://github.com/feross/buffer), [safe-buffer](https://github.com/feross/safe-buffer), [fast-json-patch](https://github.com/Starcounter-Jack/JSON-Patch), [repeat-string](https://github.com/jonschlinkert/repeat-string) | MIT |
| [ieee754](https://github.com/feross/ieee754) | BSD-3-Clause |

The license texts of highlight.js, emoji-picker-element and emoji-picker-element-data are
shipped under `/licenses/` as well.

## AI noise suppression (shipped, runs in the browser)

| Component | Version | License |
|---|---|---|
| [DeepFilterNet3](https://github.com/Rikorose/DeepFilterNet) model and libDF, © Hendrik Schröter | 3 | MIT OR Apache-2.0 |
| [mezon-noise-suppression](https://github.com/mezonai/mezon-noise-suppression) (`deepfilternet3-noise-filter`: wasm build and glue, vendored in `web/src/third_party/deepfilternet3`; Mnema's adapted `worker-glue.js` runs initialization and inference in a dedicated Worker) | 1.3.0 | MIT OR Apache-2.0 |
| Rust crates compiled into `df_bg.wasm`: [tract](https://github.com/sonos/tract) (core, data, hir, linalg, nnef, onnx, onnx-opl, pulse, pulse-opl, transformers 0.23.3), ndarray, rustfft, realfft, transpose, strength_reduce, primal-check, num-integer, flate2, miniz_oxide, tar, serde, serde_json, erased-serde, safetensors, minijinja, memo-map, rust-ini, ordered-multimap, dlv-list, nom, scan_fmt, regex, aho-corasick, memchr, hashbrown, smallvec, itertools, string-interner, bit-set, bit-vec, bytes, rand, chacha20, lazy_static, lock_api, parking_lot_core, log, anyhow, dlmalloc, rustc-demangle, wasm-bindgen | see crate | MIT and/or Apache-2.0 (aho-corasick, memchr: Unlicense OR MIT; miniz_oxide: also Zlib) |
| [web-noise-suppressor](https://github.com/sapphi-red/web-noise-suppressor) (`@sapphi-red/web-noise-suppressor`) | 0.4.1 | MIT |
| [GTCRN](https://github.com/Xiaobin-Rong/gtcrn), © Rong Xiaobin (inside web-noise-suppressor) | – | MIT |
| [PFFFT](https://github.com/marton78/pffft), © Julien Pommier, UCAR and others (inside web-noise-suppressor) | – | BSD-style (FFTPACK license) |

## Server binary

The [Go](https://go.dev) standard library and runtime (BSD-3-Clause) and these
modules are compiled into `mnema-talk`:

| Module | Version | License |
|---|---|---|
| `github.com/SherClockHolmes/webpush-go` | v1.4.0 | MIT |
| `github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream` | v1.7.20 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/config` | v1.33.7 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/credentials` | v1.20.7 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/feature/ec2/imds` | v1.20.1 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/configsources` | v1.5.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2` | v2.8.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/v4a` | v1.5.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding` | v1.13.19 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/checksum` | v1.11.5 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url` | v1.14.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/s3shared` | v1.20.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/s3` | v1.114.1 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/signin` | v1.10.2 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/sso` | v1.38.2 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/ssooidc` | v1.43.2 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/sts` | v1.51.2 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2` | v1.47.1 | Apache-2.0 |
| `github.com/aws/smithy-go` | v1.28.4 | Apache-2.0 |
| `github.com/go-chi/chi/v5` | v5.5.0 | MIT |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | MIT |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/gorilla/websocket` | v1.5.3 | BSD-2-Clause |
| `github.com/jackc/pgpassfile` | v1.0.0 | MIT |
| `github.com/jackc/pgservicefile` | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| `github.com/jackc/pgx/v5` | v5.11.0 | MIT |
| `github.com/jackc/puddle/v2` | v2.2.3 | MIT |
| `github.com/joho/godotenv` | v1.5.1 | MIT |
| `github.com/pion/datachannel` | v1.6.3 | MIT |
| `github.com/pion/dtls/v3` | v3.1.10 | MIT |
| `github.com/pion/ice/v4` | v4.4.7 | MIT |
| `github.com/pion/interceptor` | v0.1.49 | MIT |
| `github.com/pion/logging` | v0.2.4 | MIT |
| `github.com/pion/mdns/v2` | v2.2.2 | MIT |
| `github.com/pion/randutil` | v0.1.0 | MIT |
| `github.com/pion/rtcp` | v1.2.19 | MIT |
| `github.com/pion/rtp` | v1.10.5 | MIT |
| `github.com/pion/sctp` | v1.12.0 | MIT |
| `github.com/pion/sdp/v3` | v3.0.20 | MIT |
| `github.com/pion/srtp/v3` | v3.1.3 | MIT |
| `github.com/pion/stun/v4` | v4.0.1 | MIT |
| `github.com/pion/transport/v5` | v5.1.1 | MIT |
| `github.com/pion/turn/v5` | v5.1.2 | MIT |
| `github.com/pion/webrtc/v4` | v4.2.23 | MIT |
| `github.com/wlynxg/anet` | v0.0.5 | BSD-3-Clause |
| `golang.org/x/crypto` | v0.57.0 | BSD-3-Clause |
| `golang.org/x/image` | v0.47.0 | BSD-3-Clause |
| `golang.org/x/net` | v0.60.0 | BSD-3-Clause |
| `golang.org/x/sync` | v0.23.0 | BSD-3-Clause |
| `golang.org/x/sys` | v0.48.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.42.0 | BSD-3-Clause |
| `golang.org/x/time` | v0.16.0 | BSD-3-Clause |

## Updater binary

The self-update sidecar (`mnema-updater`, compose profile `autoupdate`) is a
second, separate binary in the app image. It verifies the cosign signature and
build provenance of a new release image (SECURITY.md, "Self-update sidecar").
Besides the Go standard library and the modules above that it shares with the
server (`golang.org/x/*`, `github.com/google/uuid`), these modules are compiled
into it; `go list -deps -f '{{with .Module}}{{.Path}} {{.Version}}{{end}}'
./cmd/updater | sort -u` lists them:

| Module | Version | License |
|---|---|---|
| `github.com/asaskevich/govalidator` | v0.0.0-20230301143203-a9d515a09cc2 | MIT |
| `github.com/blang/semver` | v3.5.1+incompatible | MIT |
| `github.com/cenkalti/backoff/v5` | v5.0.3 | MIT |
| `github.com/cespare/xxhash/v2` | v2.3.0 | MIT |
| `github.com/cyberphone/json-canonicalization` | v0.0.0-20241213102144-19d51d7fe467 | Apache-2.0 |
| `github.com/digitorus/pkcs7` | v0.0.0-20230818184609-3a137a874352 | MIT |
| `github.com/digitorus/timestamp` | v0.0.0-20231217203849-220c5c2851b7 | BSD-2-Clause |
| `github.com/docker/cli` | v29.7.2+incompatible | Apache-2.0 |
| `github.com/docker/docker-credential-helpers` | v0.9.3 | MIT |
| `github.com/go-logr/logr` | v1.4.4 | Apache-2.0 |
| `github.com/go-logr/stdr` | v1.2.2 | Apache-2.0 |
| `github.com/go-openapi/analysis` | v0.25.5 | Apache-2.0 |
| `github.com/go-openapi/errors` | v0.22.8 | Apache-2.0 |
| `github.com/go-openapi/jsonpointer` | v1.0.0 | Apache-2.0 |
| `github.com/go-openapi/jsonreference` | v1.0.0 | Apache-2.0 |
| `github.com/go-openapi/loads` | v0.25.0 | Apache-2.0 |
| `github.com/go-openapi/runtime` | v0.33.0 | Apache-2.0 |
| `github.com/go-openapi/runtime/server-middleware` | v0.30.0 | Apache-2.0 |
| `github.com/go-openapi/spec` | v0.22.9 | Apache-2.0 |
| `github.com/go-openapi/strfmt` | v0.27.0 | Apache-2.0 |
| `github.com/go-openapi/swag` | v0.26.1 | Apache-2.0 |
| `github.com/go-openapi/swag/cmdutils` | v0.27.0 | Apache-2.0 |
| `github.com/go-openapi/swag/conv` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/fileutils` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/jsonname` | v0.26.1 | Apache-2.0 |
| `github.com/go-openapi/swag/jsonutils` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/loading` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/mangling` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/netutils` | v0.27.0 | Apache-2.0 |
| `github.com/go-openapi/swag/pools` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/stringutils` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/typeutils` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/swag/yamlutils` | v0.27.3 | Apache-2.0 |
| `github.com/go-openapi/validate` | v0.26.1 | Apache-2.0 |
| `github.com/go-viper/mapstructure/v2` | v2.5.0 | MIT |
| `github.com/google/certificate-transparency-go` | v1.3.3 | Apache-2.0 |
| `github.com/google/go-containerregistry` | v0.22.1 | Apache-2.0 |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | v2.29.0 | BSD-3-Clause |
| `github.com/in-toto/attestation` | v1.2.0 | Apache-2.0 |
| `github.com/in-toto/in-toto-golang` | v0.11.0 | Apache-2.0 |
| `github.com/klauspost/compress` | v1.19.2 | Apache-2.0 |
| `github.com/oklog/ulid/v2` | v2.1.1 | Apache-2.0 |
| `github.com/opencontainers/go-digest` | v1.0.0 | Apache-2.0 |
| `github.com/opencontainers/image-spec` | v1.1.1 | Apache-2.0 |
| `github.com/pkg/errors` | v0.9.1 | BSD-2-Clause |
| `github.com/secure-systems-lab/go-securesystemslib` | v0.11.0 | MIT |
| `github.com/shibumi/go-pathspec` | v1.3.0 | Apache-2.0 |
| `github.com/sigstore/protobuf-specs` | v0.5.1 | Apache-2.0 |
| `github.com/sigstore/rekor` | v1.5.3 | Apache-2.0 |
| `github.com/sigstore/rekor-tiles/v2` | v2.3.0 | Apache-2.0 |
| `github.com/sigstore/sigstore` | v1.10.8 | Apache-2.0 |
| `github.com/sigstore/sigstore-go` | v1.3.0 | Apache-2.0 |
| `github.com/sigstore/timestamp-authority/v2` | v2.1.3 | Apache-2.0 |
| `github.com/sirupsen/logrus` | v1.9.4 | MIT |
| `github.com/theupdateframework/go-tuf/v2` | v2.4.2 | Apache-2.0 |
| `github.com/transparency-dev/formats` | v0.1.1 | Apache-2.0 |
| `github.com/transparency-dev/merkle` | v0.0.2 | Apache-2.0 |
| `github.com/youmark/pkcs8` | v0.0.0-20240726163527-a2c0da244d78 | MIT |
| `go.opentelemetry.io/auto/sdk` | v1.2.1 | Apache-2.0 |
| `go.opentelemetry.io/otel` | v1.44.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/metric` | v1.44.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/trace` | v1.44.0 | Apache-2.0 |
| `go.yaml.in/yaml/v3` | v3.0.5 | Apache-2.0 |
| `golang.org/x/mod` | v0.41.0 | BSD-3-Clause |
| `golang.org/x/term` | v0.47.0 | BSD-3-Clause |
| `google.golang.org/genproto/googleapis/api` | v0.0.0-20260706201446-f0a921348800 | Apache-2.0 |
| `google.golang.org/genproto/googleapis/rpc` | v0.0.0-20260706201446-f0a921348800 | Apache-2.0 |
| `google.golang.org/grpc` | v1.84.0 | Apache-2.0 |
| `google.golang.org/protobuf` | v1.36.11 | BSD-3-Clause |
| `k8s.io/klog/v2` | v2.140.0 | Apache-2.0 |

## Bundled services (docker-compose.yml)

These run as separate containers next to the app; they are not part of the
binary.

| Component | License |
|---|---|
| [PostgreSQL 18](https://www.postgresql.org) (`postgres:18-alpine`) | PostgreSQL License |
| [SeaweedFS](https://github.com/seaweedfs/seaweedfs) (`chrislusf/seaweedfs`) | Apache-2.0 |
| [coturn](https://github.com/coturn/coturn) (`coturn/coturn`, optional `turn` profile) | BSD-3-Clause |
| [Watchtower](https://github.com/nicholas-fedor/watchtower) (`nickfedor/watchtower`, maintained fork of containrrr/watchtower, optional `autoupdate` profile; restarts the app from the image the updater verified, never pulls) | Apache-2.0 |
| [Alpine Linux](https://alpinelinux.org) (base of the app image `alpine:3.24` and the Postgres image) | GPL-2.0 and others, per package |

## Build and development tools (not shipped)

Vite, `@vitejs/plugin-vue`, Tailwind CSS tooling, [swag](https://github.com/swaggo/swag) (MIT, generates `api/openapi.json`), Vitest, ESLint, happy-dom,
`@vue/test-utils`, dockertest, gitleaks, govulncheck and release-please are
used to build and test Mnema Talk. They are not part of the binary or the web
app; see `web/package.json` (`devDependencies`) and `go.mod`.

## Desktop feasibility probe

The Windows-first probe additionally uses Tauri 2.12.1, its JavaScript API
2.12.1, reqwest 0.12.28, serde 1.0.229, serde_json 1.0.151, url 2.5.8 and
windows-sys 0.61.2 (MIT OR Apache-2.0). It reuses Vue under the MIT license.
The locked Rust inventory, including optional platform and build dependencies,
is in [THIRD_PARTY_NOTICES.md](https://github.com/msch128/mnema-talk-desktop/blob/main/THIRD_PARTY_NOTICES.md)
in [mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop); the
frontend lockfile is `ui/package-lock.json` there. This prototype is not
an official desktop release. Distribution must include the applicable license
texts; inventory generation alone is not redistribution compliance.

The desktop UI bundles unchanged Inter Latin 400/500/600 fonts from the existing
@fontsource/inter 5.3.0 dependency (OFL-1.1). Full original attribution and license
are included in `licenses/INTER-OFL.txt` in that repository and the distribution bundle.

The shared Vue client also bundles `@tauri-apps/api` 2.12.1 (MIT OR
Apache-2.0) for the experimental desktop IPC adapter. Browser requests retain
their same-origin transport. License texts ship in `web/dist/licenses/tauri-api/`.
