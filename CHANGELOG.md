# Changelog

## [0.3.4](https://github.com/msch128/mnema-talk/compare/v0.3.3...v0.3.4) (2026-10-06)


### Features

* **talk:** stream quality menu like Discord (mode, resolution, frame rate) ([4442972](https://github.com/msch128/mnema-talk/commit/44429720d262d4014d701d8a2ec21f006c7aa10a))


### Bug Fixes

* **talk:** the stage's volume slider controls the stream's audio, 0-100 % ([38078d5](https://github.com/msch128/mnema-talk/commit/38078d581b5f38ee12a8712c20c4434010f01de5))

## [0.3.3](https://github.com/msch128/mnema-talk/compare/v0.3.2...v0.3.3) (2026-10-06)


### Features

* **talk:** text chat as a resizable panel under the stage, like Discord ([df20ec8](https://github.com/msch128/mnema-talk/commit/df20ec8a5b41b89142e159febfec8edebafaacf6))


### Bug Fixes

* **chat:** voice channels have their own text chat ([48ab878](https://github.com/msch128/mnema-talk/commit/48ab878fd1f6c9920449072b912a29f1b5c62f0e))
* **talk:** a /v/:id/chat deep link stays open when the call resumes first ([af2afbf](https://github.com/msch128/mnema-talk/commit/af2afbfa8c4b0e50260c3b63d18c71735a9f8c2e))
* **talk:** the Talk chat's header shows the whole channel name ([e317411](https://github.com/msch128/mnema-talk/commit/e3174112193c4109736244b0a6113a54628fd2f8))

## [0.3.2](https://github.com/msch128/mnema-talk/compare/v0.3.1...v0.3.2) (2026-10-06)


### Features

* **admin:** check GitHub for new releases every 30 minutes ([b7d50c7](https://github.com/msch128/mnema-talk/commit/b7d50c7ad5254b9094c07e3d6b4036492503672f))
* **admin:** optional self-update through an isolated updater sidecar ([0818159](https://github.com/msch128/mnema-talk/commit/081815926830fe3ee0e1dc63975a6632585d5457))
* **admin:** system tab with version and health ([d4eec0c](https://github.com/msch128/mnema-talk/commit/d4eec0c68f4e5a3c03aeb9a319da1b7ecc1ea173))
* **build:** bake version and revision into the binary and the web app ([d1d31b6](https://github.com/msch128/mnema-talk/commit/d1d31b695ea31c4d267a67cae755e9d2bdbf47c7))
* **web:** offer a reload when the server runs a newer version ([9501625](https://github.com/msch128/mnema-talk/commit/950162567a3174ecc8bd779dea7e282199a95ff7))

## [0.3.1](https://github.com/msch128/mnema-talk/compare/v0.3.0...v0.3.1) (2026-10-06)


### Features

* **docker:** publish release images for linux/amd64 and linux/arm64 ([a2314a5](https://github.com/msch128/mnema-talk/commit/a2314a52ba4fbf4c1078f14b3f8449be6f499a65))

## [0.3.0](https://github.com/msch128/mnema-talk/compare/v0.1.16...v0.3.0) (2026-10-05)


### Features

* Discord-style sidebar management and Talk stage (0.3.0) ([#42](https://github.com/msch128/mnema-talk/issues/42)) ([35d3f92](https://github.com/msch128/mnema-talk/commit/35d3f92bdf87d63ad182cf9128a16bfd9d802ed2))


### Bug Fixes

* **talk:** keep a camera decodable when the SFU renegotiates ([d53febe](https://github.com/msch128/mnema-talk/commit/d53febe28669b37c22a358fab085fde13e53841c))

## [0.1.16](https://github.com/msch128/mnema-talk/compare/v0.1.15...v0.1.16) (2026-10-05)


### Features

* **auth:** add AuthenticateRequest returning the session's token version ([66115b7](https://github.com/msch128/mnema-talk/commit/66115b7e1d6551036108eb13084b3c1e7ca6b692))


### Bug Fixes

* **admin:** keep unsaved layout edits, enforce the 10-char password minimum, surface load errors; split the dashboard per tab ([f718390](https://github.com/msch128/mnema-talk/commit/f71839042d23bfe7e853c90aff4ae06c23db7abb))
* **audio:** play sound effects on the chosen output device ([f1d90fe](https://github.com/msch128/mnema-talk/commit/f1d90fe0421a87daa920c102257617756d9e1118))
* **auth:** log only the generated initial admin password, never a configured one ([051185a](https://github.com/msch128/mnema-talk/commit/051185a94af91cfa94b82024fdbcde789cedfcd9))
* **auth:** never lock the owner out, bound login limiter keys, close sockets on revocation ([89d2112](https://github.com/msch128/mnema-talk/commit/89d2112f14b10c8775460dfffd0aa8ccd262359f))
* **auth:** seed the administrator through auth with full validation ([cf62fa6](https://github.com/msch128/mnema-talk/commit/cf62fa630fb8dfe97e484c34fd3c47f791f1d1b9))
* **chat:** bound reactions, harden edits and deletes, cheap member list ([bc63cca](https://github.com/msch128/mnema-talk/commit/bc63cca78326d42f03ce1c77a135c3cfa993775c))
* **chat:** disconnect everyone from a deleted voice channel ([9ac82cb](https://github.com/msch128/mnema-talk/commit/9ac82cb9c821e51c5ff9303a2dbff30b8b79c92d))
* **chat:** drop stale thread and profile loads, keep partial user updates ([c3e21ae](https://github.com/msch128/mnema-talk/commit/c3e21ae74f1ed509de635ff5c07defa410401d86))
* **chat:** strip only the markdown that is rendered in previews ([ef574df](https://github.com/msch128/mnema-talk/commit/ef574dfe8a897e073397d04e470d7a833004f9f3))
* **compose:** require S3 credentials on the SeaweedFS gateway ([289d355](https://github.com/msch128/mnema-talk/commit/289d355da924de544efe85caf9ee6173176ea9f2))
* **config:** reject all example placeholders and invalid values, trust only loopback proxies by default ([6927ffb](https://github.com/msch128/mnema-talk/commit/6927ffbe5c6aca8359728f5576382a36c53b027e))
* **db:** enforce case-insensitive unique usernames and drop redundant indexes ([a4d139c](https://github.com/msch128/mnema-talk/commit/a4d139c47c73558f31b7f6df11bf7621d2148091))
* **db:** record migration checksums and refuse edited migrations ([e5d1bbb](https://github.com/msch128/mnema-talk/commit/e5d1bbbc9fc3f115452e0171be73d846f18660e3))
* **httpx:** bound limiter sweeps and size, key IPv6 per /64, join X-Forwarded-For lines ([8c95c55](https://github.com/msch128/mnema-talk/commit/8c95c55e0c95af22a6f3fb0251fc60d7bf6eb350))
* **i18n:** add chat.uploadFailed and drop unused keys ([534ebb7](https://github.com/msch128/mnema-talk/commit/534ebb7d1d1b5ee90b6731dc5cf4f0c9c3669624))
* **linkpreview:** keep denied addresses on DNS failure, share fetches and cache images ([2969211](https://github.com/msch128/mnema-talk/commit/296921100011728547f2c0a0bc9cb48a9601ecba))
* **make:** tolerate a missing scorecard, require Node 24 and gofmt api/ ([0867496](https://github.com/msch128/mnema-talk/commit/0867496b18865c7d82ed6540f84cb816b50e8c05))
* **media:** serve missing objects as 410 and prune in marked batches ([f62696b](https://github.com/msch128/mnema-talk/commit/f62696b0fe16d9dc82435a17d5324b17bfb53d30))
* **nav:** stop a superseded route after each await ([339e8d5](https://github.com/msch128/mnema-talk/commit/339e8d5dcb33d7c63f58ce38f2df7466fcffc650))
* profile changes (avatar, name, status) reach voice lists and open threads ([427e86c](https://github.com/msch128/mnema-talk/commit/427e86c007fc88e620d08578e6dec68ae13e37b9))
* **s3:** only create the bucket when it is missing and retry storage init ([a7d8f96](https://github.com/msch128/mnema-talk/commit/a7d8f9604ef083bf49e5db25f939f88498fdbea4))
* **scripts:** keep backups out of the checkout and copy SeaweedFS consistently ([06be8ca](https://github.com/msch128/mnema-talk/commit/06be8ca12c17608d73142cd8b86873ed4336f9ea))
* **server:** cache health checks, report storage outages and stop background work ([99a832c](https://github.com/msch128/mnema-talk/commit/99a832c4a1df01714df55d1893aec4ae80de2fe1))
* **sfu:** keep forwarding when one subscriber's write fails ([e7f5a3b](https://github.com/msch128/mnema-talk/commit/e7f5a3b7ad98f603b1b6b210433bff4f11b07251))
* **talk:** only watch a screen after the join went ahead ([dabe981](https://github.com/msch128/mnema-talk/commit/dabe98134b9300498a764ca55e09f19e5be18be6))
* **thread:** keyboard access, video attachments and upload retry in the thread panel ([158107f](https://github.com/msch128/mnema-talk/commit/158107fb74e42a7410fb32bac0eb02114f8b9d5d))
* **voice:** end the call locally when an admin removes me from voice ([1e4f62b](https://github.com/msch128/mnema-talk/commit/1e4f62b95abf93da611b23ba7c554ad5c6933c4a))
* **voice:** key remote audio elements by track ([0aba59b](https://github.com/msch128/mnema-talk/commit/0aba59b6457384235bdad4f479e88e04b5a21b06))
* **voice:** reapply sender limits after a reconnect or channel switch ([33fa3ae](https://github.com/msch128/mnema-talk/commit/33fa3ae97f1be319143fdaeeb2374b669f6f0855))
* **voice:** register the settings watchers of useWebRTC once ([b783bf9](https://github.com/msch128/mnema-talk/commit/b783bf9e69f814c8024bc35f27318876ac06df2d))
* **voice:** release the mic when the mic test is stopped mid-prompt ([dc3d6da](https://github.com/msch128/mnema-talk/commit/dc3d6daee61341c1045a357bb48df207855bbe80))
* **voice:** serialize audio settings changes so screen audio survives ([9fc797f](https://github.com/msch128/mnema-talk/commit/9fc797f5ad88790ce1ee84c90d1f0134ea2ca3cd))
* **voice:** stop media that a superseded join or share acquired ([3852f3a](https://github.com/msch128/mnema-talk/commit/3852f3a7914d5df16e23394f97e0a9c53c84ae67))
* **web:** build the search modal on BaseDialog and format dates in the UI language ([6c74029](https://github.com/msch128/mnema-talk/commit/6c740292ce6a2a8d27a91961e1cb913bed63c58c))
* **web:** load the current bio when editing the profile instead of the stub ([514b4cf](https://github.com/msch128/mnema-talk/commit/514b4cff4d6ae42fd5b07d3b071862d3b921c140))
* **web:** show the existing name-required message in the rename dialog ([fbde84d](https://github.com/msch128/mnema-talk/commit/fbde84d90ec7b43541a35416dc7198682dd40658))
* **web:** stop the mic test when audio settings close while it starts ([ad38de2](https://github.com/msch128/mnema-talk/commit/ad38de2b78b14ee3e113739839b2fd3cf3e46519))
* **ws:** keep the session cookie's token version on the connection ([403e402](https://github.com/msch128/mnema-talk/commit/403e4027560c3a07456a5e36cdbe4944b0dfb61a))
* **ws:** never close a client's send channel ([835c6fd](https://github.com/msch128/mnema-talk/commit/835c6fd8c61d58e03293f772ad37ef4b0a7988ee))
* **ws:** serialize voice joins and leaves and scope voice state per room ([e291894](https://github.com/msch128/mnema-talk/commit/e291894638956da22d8f674451fd29c53cade84e))

## [0.1.15](https://github.com/msch128/mnema-talk/compare/v0.1.14...v0.1.15) (2026-10-05)


### Bug Fixes

* **voice:** stop the audio loop when two people share system audio ([#34](https://github.com/msch128/mnema-talk/issues/34)) ([80746b0](https://github.com/msch128/mnema-talk/commit/80746b021c27a30be3d1e75aa54c61ae38755043))

## [0.1.14](https://github.com/msch128/mnema-talk/compare/v0.1.13...v0.1.14) (2026-10-05)


### Bug Fixes

* **voice:** no self-echo through shared system audio; visible per-user volume slider ([#32](https://github.com/msch128/mnema-talk/issues/32)) ([7d2c9ce](https://github.com/msch128/mnema-talk/commit/7d2c9ce886677396631ec1eade4674200b863c71))

## [0.1.13](https://github.com/msch128/mnema-talk/compare/v0.1.12...v0.1.13) (2026-10-05)


### Features

* **voice:** screen share prefers H.264 and starts at a higher bitrate ([#30](https://github.com/msch128/mnema-talk/issues/30)) ([1224a5e](https://github.com/msch128/mnema-talk/commit/1224a5e860b52653ae6b548095e53df4be97b6f3))

## [0.1.12](https://github.com/msch128/mnema-talk/compare/v0.1.11...v0.1.12) (2026-10-05)


### Bug Fixes

* **voice:** late joiners are heard; stream diagnostics ([adce50e](https://github.com/msch128/mnema-talk/commit/adce50ecc298c838bb485031372e957c38aac0e6))

## [0.1.11](https://github.com/msch128/mnema-talk/compare/v0.1.10...v0.1.11) (2026-10-05)


### Features

* **voice:** volumes, output device, mute marks and a quiet own preview ([c480985](https://github.com/msch128/mnema-talk/commit/c480985a9c76aec0be00ffb27fd8df679af3a3f7))

## [0.1.10](https://github.com/msch128/mnema-talk/compare/v0.1.9...v0.1.10) (2026-10-05)


### Bug Fixes

* **voice:** screen share and camera reach the server ([9587816](https://github.com/msch128/mnema-talk/commit/958781643dd4e3dfdee4539ca20f459d7c3922a6))

## [0.1.9](https://github.com/msch128/mnema-talk/compare/v0.1.8...v0.1.9) (2026-10-05)


### Bug Fixes

* **voice:** pin the TURN relay address to the voice server's LAN IP ([db8b980](https://github.com/msch128/mnema-talk/commit/db8b9804794c08a47c5cbc094d7eade0ad325d7d))
* **voice:** TURN relay can reach the voice server; 404 for stale assets ([352202d](https://github.com/msch128/mnema-talk/commit/352202d7d96fa679b1170219db3bd8805fafc23a))

## [0.1.8](https://github.com/msch128/mnema-talk/compare/v0.1.7...v0.1.8) (2026-10-05)


### Features

* **voice:** browsers report their connection diagnostics ([d535485](https://github.com/msch128/mnema-talk/commit/d535485b1a05eb65d3ae7ae726347415ae37d3e2))

## [0.1.7](https://github.com/msch128/mnema-talk/compare/v0.1.6...v0.1.7) (2026-10-05)


### Features

* **voice:** Discord-parity screen sharing, mic test loopback, QoS and granular sound effects ([469ca0e](https://github.com/msch128/mnema-talk/commit/469ca0e421991d8a1abaa2bae49f3ede43116690))

## [0.1.6](https://github.com/msch128/mnema-talk/compare/v0.1.5...v0.1.6) (2026-10-05)


### Features

* **ops:** LOG_LEVEL=debug for voice connection troubleshooting ([5781012](https://github.com/msch128/mnema-talk/commit/57810122a91327d2b73e0e9f0ce7261ffaacdd8d))

## [0.1.5](https://github.com/msch128/mnema-talk/compare/v0.1.4...v0.1.5) (2026-10-05)


### Bug Fixes

* **voice:** announce public and LAN addresses, log connection states ([b13e604](https://github.com/msch128/mnema-talk/commit/b13e60411d1e28490aefe901fe67a7e28fd02cd6))

## [0.1.4](https://github.com/msch128/mnema-talk/compare/v0.1.3...v0.1.4) (2026-10-05)


### Features

* **api:** generated OpenAPI 3.1 docs and Swagger UI at /api/docs ([a27b567](https://github.com/msch128/mnema-talk/commit/a27b567130296274d4112b0eda2f5b99593dee01))
* Talk timers and activity totals ([2075f4b](https://github.com/msch128/mnema-talk/commit/2075f4ba5b4c58bc627cf4822574189a8257644d))

## [0.1.3](https://github.com/msch128/mnema-talk/compare/v0.1.2...v0.1.3) (2026-10-05)


### Features

* **api:** presence choice, status text and server-side mentions ([59b4a2f](https://github.com/msch128/mnema-talk/commit/59b4a2fbdc46e0ac73c3955bad03c0f1b318715d))
* **web:** presence menu, status text, [@mentions](https://github.com/mentions) and emoji picker ([1762d96](https://github.com/msch128/mnema-talk/commit/1762d96f89f63c778ddf5c0dee951fee966a5d5d))

## [0.1.2](https://github.com/msch128/mnema-talk/compare/v0.1.1...v0.1.2) (2026-10-05)


### Bug Fixes

* resolve CodeQL findings in test code ([#17](https://github.com/msch128/mnema-talk/issues/17)) ([47c6257](https://github.com/msch128/mnema-talk/commit/47c62573e83b40534012fda6400777b0a53949c4))

## [0.1.1](https://github.com/msch128/mnema-talk/compare/v0.1.0...v0.1.1) (2026-10-05)


### Features

* **admin:** add user moderation and channel layout management ([0eec57b](https://github.com/msch128/mnema-talk/commit/0eec57bffe65355c2331b40c886c8317a9ac5a2b))
* **admin:** rename channels and categories, reorder by drag and drop ([00f6fb3](https://github.com/msch128/mnema-talk/commit/00f6fb3af116a4a622b00ad900b17056219a4db6))
* **chat:** mark the open channel read, notification levels and typing expiry in the store ([6ea2fb3](https://github.com/msch128/mnema-talk/commit/6ea2fb3f0f40b23048daaabe696cab00f799d810))
* **chat:** page thread replies with limit and cursor ([e358256](https://github.com/msch128/mnema-talk/commit/e35825648d4dbfbdde9fe08185da02340cd19811))
* **chat:** search filters for channel, author and attachments with jump to result ([3ce4b5e](https://github.com/msch128/mnema-talk/commit/3ce4b5e753303cf0466f595e21711bd89b7d1ddf))
* **chat:** unread divider, typing line, notification controls and message keyboard navigation ([b033cfc](https://github.com/msch128/mnema-talk/commit/b033cfc1e3040d133a4cae7dbc599a497d48c4c8))
* **nav:** add context menus for channels, categories and members ([8aab3cf](https://github.com/msch128/mnema-talk/commit/8aab3cf0dd9cc3e025add1074ea9ea12eac442ea))
* **ops:** add Prometheus metrics, health check script and coturn compose profile ([7e6e642](https://github.com/msch128/mnema-talk/commit/7e6e6425c57f9f24e50e5a5302dc30eebff50fca))
* **sfu:** forward the camera as its own video source next to the screen share ([1d5a560](https://github.com/msch128/mnema-talk/commit/1d5a56050f18d6a152b8209a1b2923efc70c97c2))
* **sfu:** forward video only to viewers who subscribed ([ea3cac6](https://github.com/msch128/mnema-talk/commit/ea3cac68ef685b3411470fbd939693d3c89beabc))
* **ui:** make context menu keyboard accessible with anchor positioning ([5723f76](https://github.com/msch128/mnema-talk/commit/5723f76d5a01547908e5df3a128fa08ba0963bc2))
* **voice:** AI noise suppression with DeepFilterNet3 ([acf0be3](https://github.com/msch128/mnema-talk/commit/acf0be3cfa73292c9d5efe82e9cb6a393f8b0ef3))
* **voice:** cap screen and camera bitrate and keep screen resolution under congestion ([44d6f6b](https://github.com/msch128/mnema-talk/commit/44d6f6b8ba4a22a46dbabb274b0bf460b9904413))
* **voice:** opt in to screen shares, opt out of cameras ([8ef1b97](https://github.com/msch128/mnema-talk/commit/8ef1b97ba2e8b506b00b2267f7298e70357e14de))
* **voice:** preview a roundtable without joining and show camera tiles ([b56b3d0](https://github.com/msch128/mnema-talk/commit/b56b3d0cb894d170218dd81fd867455dc6cf9cdc))
* **voice:** seamless screenshare start and stop using video transceiver ([083d1e9](https://github.com/msch128/mnema-talk/commit/083d1e99c21a12b62c1c25e441a7f3874f802a13))
* **voice:** share screen audio, per-user volume and webcam in the client ([eae7e7e](https://github.com/msch128/mnema-talk/commit/eae7e7e87a2ad9a53a029ba936c1ef10ba2b3918))
* **voice:** TURN relay support with time-limited credentials ([15be43c](https://github.com/msch128/mnema-talk/commit/15be43c98ade73b59e287ee96e2dc70656c4de17))
* **web:** i18n (de/en), toasts, confirm dialogs, accessible dialogs, voice status panel ([0724ce0](https://github.com/msch128/mnema-talk/commit/0724ce0662fc3731505bc3463889e6abe49de046))
* **web:** serve precompressed static assets ([d880dec](https://github.com/msch128/mnema-talk/commit/d880dec733e172ea30c0f82c62e51db574e68a15))
* **web:** URL routes, context menus, read state and link preview groundwork ([e975ecf](https://github.com/msch128/mnema-talk/commit/e975ecf2cf9bacfedceaf0680be518860be48547))


### Bug Fixes

* **admin:** drop duplicate user admin routes, validate admin-set passwords ([088f7ba](https://github.com/msch128/mnema-talk/commit/088f7ba92a11759a519a598fbf15bf15debc1b05))
* **chat:** cache link previews and skip links in code and spoilers ([882bde1](https://github.com/msch128/mnema-talk/commit/882bde1cc9e5b90915c461c223d1214f1e4176fd))
* **ops:** refuse to start coturn without a secret, close relay bypasses ([78966fc](https://github.com/msch128/mnema-talk/commit/78966fcb5438fe3fe4c480406278c852366bfda5))
* **ops:** require METRICS_TOKEN for /api/metrics, harden coturn relay ([ad8d267](https://github.com/msch128/mnema-talk/commit/ad8d2671a2c6df8bf655695954f390070cf201ee))
* **release:** stop re-releasing 0.1.0 after main was rewritten ([36f1eae](https://github.com/msch128/mnema-talk/commit/36f1eae3d08e97f5b9b8b004956d92efc98491d3))
* **routes:** guard admin routes, follow joined voice channel and fix admin history ([0ece6e0](https://github.com/msch128/mnema-talk/commit/0ece6e0c12db6611ce0d5cb519da451d98711067))
* **voice:** allow the webcam in the Permissions-Policy ([36110db](https://github.com/msch128/mnema-talk/commit/36110dbab818a24f045aea0d27c88e27a8ed1941))
* **voice:** keep voice active and prevent disconnect in background tabs ([bdf3777](https://github.com/msch128/mnema-talk/commit/bdf37779a5aafaa84e6bd988102e8aad15a4f921))
* **voice:** open connection details from the ping in the voice panel ([bd6df9d](https://github.com/msch128/mnema-talk/commit/bd6df9d0b58fcc18129d9b6837badea67f6876a0))
* **web:** tooltip on the search dialog close button ([01fd0e7](https://github.com/msch128/mnema-talk/commit/01fd0e7b71a845fab83cbd20032fae9b46119940))

## 0.1.0 (2026-10-05)


### ⚠ BREAKING CHANGES

* direct messages are removed. Auth moved from bearer tokens to session cookies. Migrations moved to internal/db/migrations and new env variables are required in production (see .env.example).

### Features

* add direct messaging (DMs) and server-routed 1-on-1 calls ([09f990a](https://github.com/msch128/mnema-talk/commit/09f990a7285149299cf2a1c67ee8977175637e2c))
* add env-configurable privacy policy, legal disclosures, and terms ([a1b21e2](https://github.com/msch128/mnema-talk/commit/a1b21e258bb7205d1731dc3b27010d694e108ad7))
* add real-time ping telemetry, RTC connection stats modal, right member list and voice center chat ([21d85e4](https://github.com/msch128/mnema-talk/commit/21d85e4828c3b7753bc6450fc83a81456254a2cf))
* add Rocket.Chat threads, fix channel messaging and inline image uploads ([ef91871](https://github.com/msch128/mnema-talk/commit/ef91871e5335e2c4d9d1a0a813e486a20d6c7389))
* **admin:** user management, voice kick and last seen ([582b9d5](https://github.com/msch128/mnema-talk/commit/582b9d5b63f47fa4dcc0fbcc2ff12d2764f96440))
* **audio:** add Discord-identical input sensitivity, live visual noise gate and acoustic settings ([c0f7dc1](https://github.com/msch128/mnema-talk/commit/c0f7dc1028edc3e8091c408e981c4011f80d6ccc))
* **auth:** per-account UI language ([b1cdc7e](https://github.com/msch128/mnema-talk/commit/b1cdc7ed8301cd1e96d2c33b4e6ce3a260cd908e))
* **channels:** add channel & category management, default channel seeding and creation modal ([9b4dd31](https://github.com/msch128/mnema-talk/commit/9b4dd31c848de87827bb3986904b24cd1cb7d7d5))
* **chat:** message search with filters ([7605b00](https://github.com/msch128/mnema-talk/commit/7605b0058f879062bf840640c7935f9e23729d07))
* **chat:** server-side read state, mentions and notification levels ([a8b154f](https://github.com/msch128/mnema-talk/commit/a8b154f7bee38fee633f1633c870bab9efc9b42d))
* **frontend:** Vue 3 Discord UI, WebRTC voice/screen, Docker multi-stage build ([634412f](https://github.com/msch128/mnema-talk/commit/634412fac453eebae01d8013311fe5bddd3a1970))
* harden, restructure and test the whole app ([e42a9ae](https://github.com/msch128/mnema-talk/commit/e42a9aed21a45b608f8d67fb936a0a6378c6db77))
* implement backend channels, chat, s3 upload, websockets, sfu and admin retention engine ([1edf561](https://github.com/msch128/mnema-talk/commit/1edf561db5426b0146ac806040b2e7692bf915d8))
* implement phase 1 foundation (postgres 17, seaweedfs s3, go core & auth) ([91d366c](https://github.com/msch128/mnema-talk/commit/91d366c44fafca8f5db0b62132b3d747a08b66aa))
* implement SFU audio & screensharing, message edit/delete, emoji reactions, user bio, and resolve dependabot security vulnerabilities ([d9d54f8](https://github.com/msch128/mnema-talk/commit/d9d54f8e40a5f85f43dd06930d3020d29cc99d3a))
* live mic meter and noise gate test in audio settings ([5a9f062](https://github.com/msch128/mnema-talk/commit/5a9f0622178f27214b4b1d5916dce25fab0771b8))
* **ops:** backup and restore scripts with a verify mode ([01d1a12](https://github.com/msch128/mnema-talk/commit/01d1a12ff52c0f2e85ec1190ecb291bdc86e01c6))
* SSRF-safe link previews with an image proxy ([ae264c1](https://github.com/msch128/mnema-talk/commit/ae264c14edb64e55b33e1ca8a178c1911b05c8e5))
* **talk:** implement dedicated VoiceStage hangout view, 4K screen showcase & Mnema layout ([f63abaf](https://github.com/msch128/mnema-talk/commit/f63abaf284c09b2263b552f7e3f5c935aa960eb8))
* **ws:** relay typing notices to the others, throttled ([30d4032](https://github.com/msch128/mnema-talk/commit/30d403262ea2ffd0e820b6c526d00890e089d918))


### Bug Fixes

* **audio:** keep the input level visible below the noise gate threshold ([960bd8d](https://github.com/msch128/mnema-talk/commit/960bd8dff11e4d0025817b94d04267870de9b71f))
* **auth:** per-address login lockouts, log out everywhere, one APP_ENV ([e92b14e](https://github.com/msch128/mnema-talk/commit/e92b14e0ff69d44e493be86ebefe72e324c83f35))
* build with go1.27 toolchain and x/text v0.39 to close stdlib vulnerabilities ([30d98cd](https://github.com/msch128/mnema-talk/commit/30d98cd8dd1b23e663d7a05cd31a8dd8d59c9ce8))
* **config:** parse WebRTC UDP ports as uint16 ([e3e2e6b](https://github.com/msch128/mnema-talk/commit/e3e2e6bfca61e2f964a1b5372f6ac91d225f8dfb))
* **deps:** bump chi, x/net, x/crypto and govulncheck v1.8.0 ([552a14e](https://github.com/msch128/mnema-talk/commit/552a14ee70f6457c7047c45096d7ff851d537328))
* ensure channels and messages serialize as empty arrays rather than null ([be25fa7](https://github.com/msch128/mnema-talk/commit/be25fa7b875714d5463dd617e8f35a9aaaefbaf0))
* ensure parent_id column exists before creating index ([29fdba0](https://github.com/msch128/mnema-talk/commit/29fdba06f8bcd9ea5dfa0d39c626dad57d3d9709))
* harden link preview SSRF guard and release auto-merge ([72c67fc](https://github.com/msch128/mnema-talk/commit/72c67fc74139dcea728d2d82a12cd5fc80a5b9a7))
* import encoding/json in upload.go ([2f74814](https://github.com/msch128/mnema-talk/commit/2f748148e5dc95a17f806180130f0e024db8c9c4))
* **media:** range requests for media and delete objects with their messages ([38bafd0](https://github.com/msch128/mnema-talk/commit/38bafd00174f5a3bf03acf9153a2ddd4351a4b34))
* **member-list:** dock member list to right window edge like discord ([9c6145f](https://github.com/msch128/mnema-talk/commit/9c6145f3da70e48d25a70add468b484ed50663e2))
* meter mic level on a cloned track so the noise gate can reopen ([e7570bd](https://github.com/msch128/mnema-talk/commit/e7570bd838112d52567879b96ae52cd9be67e860))
* place reactions at message bottom and improve hover tolerance with safe hitbox ([ea072f3](https://github.com/msch128/mnema-talk/commit/ea072f3d156342466f0d6953895f470a952d2df0))
* remove hardcoded secret defaults, add auth rate limiting and password change ([98ce3c4](https://github.com/msch128/mnema-talk/commit/98ce3c4deb244a0c32216ea523059ea9140f41a8))
* **s3:** remove unused time import ([03749f1](https://github.com/msch128/mnema-talk/commit/03749f15efa12aa645d3c8a9fc31a92281060c6b))
* **sfu:** forward keyframe requests to publishers and keep peers on rejoin ([81283b3](https://github.com/msch128/mnema-talk/commit/81283b307104d4ef9672db0b877c9991a1db7d95))
* update Dockerfile to golang:alpine and sync go.sum for go 1.25 ([9ac0ddf](https://github.com/msch128/mnema-talk/commit/9ac0ddfddb9ac822a0c8702c2d94cae4c6db67d8))
* **voice:** meter the live mic level independently of the noise gate ([7f57772](https://github.com/msch128/mnema-talk/commit/7f57772f2948cbc37496b6e4b43697cac74dd3cb))
* **web:** survive reconnects and fix voice races in the client ([04c7137](https://github.com/msch128/mnema-talk/commit/04c7137cc6a67c077c545eb1346f063f76595620))


### Miscellaneous Chores

* keep releases on 0.x until 1.0.0 is cut deliberately ([c58f0b2](https://github.com/msch128/mnema-talk/commit/c58f0b2b65107fe8211c00e8e09e5f3d6d7001f7))

## Changelog

All notable changes are recorded here by release-please from conventional commits.
