# Third-party notices

Mnema Talk is licensed under the MIT license (see `LICENSE`). It is built
from, and ships, the open-source software listed here. The license texts are
in each project's repository and, for Go modules and npm packages, in the
module or package itself. The legal dialog in the app shows a summary of this
list (`web/src/lib/thirdParty.js`).

Keep this file current when dependencies change. `go list -deps -f
'{{with .Module}}{{.Path}} {{.Version}}{{end}}' ./cmd/server | sort -u`
lists the Go modules compiled into the binary; `web/src/lib/thirdParty.test.js`
fails when a web dependency is missing from the in-app list.

## Web app (shipped to every browser)

| Component | Version | License |
|---|---|---|
| [Vue](https://vuejs.org) (`vue`, incl. `@vue/*` runtime) | 3.5.43 | MIT |
| [Pinia](https://pinia.vuejs.org) | 4.0.3 | MIT |
| [Lucide](https://lucide.dev) (`@lucide/vue`) | 1.52.0 | ISC |
| [Inter](https://rsms.me/inter/) (`@fontsource/inter`) | 5.3.0 | OFL-1.1 |
| [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (`@fontsource/jetbrains-mono`) | 5.3.0 | OFL-1.1 |
| [Tailwind CSS](https://tailwindcss.com) (only the generated CSS is shipped) | 3.4.19 | MIT |

## AI noise suppression (shipped, runs in the browser)

| Component | Version | License |
|---|---|---|
| [DeepFilterNet3](https://github.com/Rikorose/DeepFilterNet) model and libDF, © Hendrik Schröter | 3 | MIT OR Apache-2.0 |
| [mezon-noise-suppression](https://github.com/mezonai/mezon-noise-suppression) (`deepfilternet3-noise-filter`: wasm build and AudioWorklet, vendored in `web/src/third_party/deepfilternet3`) | 1.3.0 | MIT OR Apache-2.0 |
| Rust crates compiled into `df_bg.wasm`: [tract](https://github.com/sonos/tract) (core, data, hir, linalg, nnef, onnx, onnx-opl, pulse, pulse-opl, transformers 0.23.3), ndarray, rustfft, realfft, transpose, strength_reduce, primal-check, num-integer, flate2, miniz_oxide, tar, serde, serde_json, erased-serde, safetensors, minijinja, memo-map, rust-ini, ordered-multimap, dlv-list, nom, scan_fmt, regex, aho-corasick, memchr, hashbrown, smallvec, itertools, string-interner, bit-set, bit-vec, bytes, rand, chacha20, lazy_static, lock_api, parking_lot_core, log, anyhow, dlmalloc, rustc-demangle, wasm-bindgen | see crate | MIT and/or Apache-2.0 (aho-corasick, memchr: Unlicense OR MIT; miniz_oxide: also Zlib) |
| [web-noise-suppressor](https://github.com/sapphi-red/web-noise-suppressor) (`@sapphi-red/web-noise-suppressor`) | 0.4.1 | MIT |
| [GTCRN](https://github.com/Xiaobin-Rong/gtcrn), © Rong Xiaobin (inside web-noise-suppressor) | – | MIT |
| [PFFFT](https://github.com/marton78/pffft), © Julien Pommier, UCAR and others (inside web-noise-suppressor) | – | BSD-style (FFTPACK license) |

## Server binary

The [Go](https://go.dev) standard library and runtime (BSD-3-Clause) and these
modules are compiled into `mnema-talk`:

| Module | Version | License |
|---|---|---|
| `github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream` | v1.7.20 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/config` | v1.33.6 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/credentials` | v1.20.6 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/feature/ec2/imds` | v1.20.1 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/configsources` | v1.5.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2` | v2.8.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/internal/v4a` | v1.5.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding` | v1.13.19 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/checksum` | v1.11.5 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url` | v1.14.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/internal/s3shared` | v1.20.4 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/s3` | v1.114.0 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/signin` | v1.10.1 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/sso` | v1.38.1 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/ssooidc` | v1.43.1 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2/service/sts` | v1.51.1 | Apache-2.0 |
| `github.com/aws/aws-sdk-go-v2` | v1.47.1 | Apache-2.0 |
| `github.com/aws/smithy-go` | v1.28.1 | Apache-2.0 |
| `github.com/go-chi/chi/v5` | v5.3.2 | MIT |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | MIT |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/gorilla/websocket` | v1.5.3 | BSD-2-Clause |
| `github.com/jackc/pgpassfile` | v1.0.0 | MIT |
| `github.com/jackc/pgservicefile` | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| `github.com/jackc/pgx/v5` | v5.11.0 | MIT |
| `github.com/jackc/puddle/v2` | v2.2.2 | MIT |
| `github.com/joho/godotenv` | v1.5.1 | MIT |
| `github.com/pion/datachannel` | v1.6.3 | MIT |
| `github.com/pion/dtls/v3` | v3.1.9 | MIT |
| `github.com/pion/ice/v4` | v4.4.4 | MIT |
| `github.com/pion/interceptor` | v0.1.49 | MIT |
| `github.com/pion/logging` | v0.2.4 | MIT |
| `github.com/pion/mdns/v2` | v2.2.1 | MIT |
| `github.com/pion/randutil` | v0.1.0 | MIT |
| `github.com/pion/rtcp` | v1.2.19 | MIT |
| `github.com/pion/rtp` | v1.10.5 | MIT |
| `github.com/pion/sctp` | v1.11.3 | MIT |
| `github.com/pion/sdp/v3` | v3.0.20 | MIT |
| `github.com/pion/srtp/v3` | v3.1.0 | MIT |
| `github.com/pion/stun/v4` | v4.0.1 | MIT |
| `github.com/pion/transport/v5` | v5.1.1 | MIT |
| `github.com/pion/turn/v5` | v5.1.2 | MIT |
| `github.com/pion/webrtc/v4` | v4.2.22 | MIT |
| `github.com/wlynxg/anet` | v0.0.5 | BSD-3-Clause |
| `golang.org/x/crypto` | v0.57.0 | BSD-3-Clause |
| `golang.org/x/net` | v0.58.0 | BSD-3-Clause |
| `golang.org/x/sync` | v0.23.0 | BSD-3-Clause |
| `golang.org/x/sys` | v0.48.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.42.0 | BSD-3-Clause |
| `golang.org/x/time` | v0.14.0 | BSD-3-Clause |

## Bundled services (docker-compose.yml)

These run as separate containers next to the app; they are not part of the
binary.

| Component | License |
|---|---|
| [PostgreSQL 17](https://www.postgresql.org) (`postgres:17-alpine`) | PostgreSQL License |
| [SeaweedFS](https://github.com/seaweedfs/seaweedfs) (`chrislusf/seaweedfs`) | Apache-2.0 |
| [coturn](https://github.com/coturn/coturn) (`coturn/coturn`, optional `turn` profile) | BSD-3-Clause |
| [Alpine Linux](https://alpinelinux.org) (base of the app image `alpine:3.24` and the Postgres image) | GPL-2.0 and others, per package |

## Build and development tools (not shipped)

Vite, `@vitejs/plugin-vue`, Tailwind CSS tooling, Vitest, ESLint, happy-dom,
`@vue/test-utils`, dockertest, gitleaks, govulncheck and release-please are
used to build and test Mnema Talk. They are not part of the binary or the web
app; see `web/package.json` (`devDependencies`) and `go.mod`.
