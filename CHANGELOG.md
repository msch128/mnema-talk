# Changelog

## [0.1.10](https://github.com/msch128/mnema-talk/compare/v0.1.9...v0.1.10) (2026-10-05)


### Bug Fixes

* **voice:** screen share and camera reach the server ([ff19bde](https://github.com/msch128/mnema-talk/commit/ff19bde3fb2e16d0c2812c8f14c54d1a8d31b096))

## [0.1.9](https://github.com/msch128/mnema-talk/compare/v0.1.8...v0.1.9) (2026-10-05)


### Bug Fixes

* **voice:** pin the TURN relay address to the voice server's LAN IP ([1e64a29](https://github.com/msch128/mnema-talk/commit/1e64a29dfd387365b30d8d8f58fe41c2a0e7bea3))
* **voice:** TURN relay can reach the voice server; 404 for stale assets ([00b491f](https://github.com/msch128/mnema-talk/commit/00b491f681926279592491e7548d53463b1ec848))

## [0.1.8](https://github.com/msch128/mnema-talk/compare/v0.1.7...v0.1.8) (2026-10-05)


### Features

* **voice:** browsers report their connection diagnostics ([836e738](https://github.com/msch128/mnema-talk/commit/836e738d32c0fb35e542f7c8c9f8b9ca2c2ffba3))

## [0.1.7](https://github.com/msch128/mnema-talk/compare/v0.1.6...v0.1.7) (2026-10-05)


### Features

* **voice:** Discord-parity screen sharing, mic test loopback, QoS and granular sound effects ([5ab4760](https://github.com/msch128/mnema-talk/commit/5ab4760bd144993d5900ca2ad2b9c2eb1174d13d))

## [0.1.6](https://github.com/msch128/mnema-talk/compare/v0.1.5...v0.1.6) (2026-10-05)


### Features

* **ops:** LOG_LEVEL=debug for voice connection troubleshooting ([f1c6ef9](https://github.com/msch128/mnema-talk/commit/f1c6ef98358d1f8ce4ef7172520f9f90c71bd5be))

## [0.1.5](https://github.com/msch128/mnema-talk/compare/v0.1.4...v0.1.5) (2026-10-05)


### Bug Fixes

* **voice:** announce public and LAN addresses, log connection states ([70aa20d](https://github.com/msch128/mnema-talk/commit/70aa20dbb2aac5cf9b63f926e1aa6d626cbf8340))

## [0.1.4](https://github.com/msch128/mnema-talk/compare/v0.1.3...v0.1.4) (2026-10-05)


### Features

* **api:** generated OpenAPI 3.1 docs and Swagger UI at /api/docs ([00b8ce0](https://github.com/msch128/mnema-talk/commit/00b8ce0a6d22514b5a4bbe68f541d47d67dd4a50))
* Talk timers and activity totals ([26a976f](https://github.com/msch128/mnema-talk/commit/26a976f8e4096054ab588bbcc074b3def708b608))

## [0.1.3](https://github.com/msch128/mnema-talk/compare/v0.1.2...v0.1.3) (2026-10-05)


### Features

* **api:** presence choice, status text and server-side mentions ([3d99c0d](https://github.com/msch128/mnema-talk/commit/3d99c0de2dc7ef2f38e002a9c12537e6df172c89))
* **web:** presence menu, status text, [@mentions](https://github.com/mentions) and emoji picker ([aa34060](https://github.com/msch128/mnema-talk/commit/aa34060a40506eb193838566e1207fdbeeb6dec6))

## [0.1.2](https://github.com/msch128/mnema-talk/compare/v0.1.1...v0.1.2) (2026-10-05)


### Bug Fixes

* resolve CodeQL findings in test code ([#17](https://github.com/msch128/mnema-talk/issues/17)) ([3e72e9c](https://github.com/msch128/mnema-talk/commit/3e72e9c5457d6754cf9560406f20802da6b1bf56))

## [0.1.1](https://github.com/msch128/mnema-talk/compare/v0.1.0...v0.1.1) (2026-10-05)


### Features

* **admin:** add user moderation and channel layout management ([5a24c9a](https://github.com/msch128/mnema-talk/commit/5a24c9a3a077828288746a5d622b7436f89959fe))
* **admin:** rename channels and categories, reorder by drag and drop ([a1d710f](https://github.com/msch128/mnema-talk/commit/a1d710fdc25009fa1bb3bc90733eb06bdab6023f))
* **chat:** mark the open channel read, notification levels and typing expiry in the store ([fe6f05b](https://github.com/msch128/mnema-talk/commit/fe6f05b3dc4166ccc4a01adde82f649261b4d73f))
* **chat:** page thread replies with limit and cursor ([13617e8](https://github.com/msch128/mnema-talk/commit/13617e8e1fb88b4fe302dd5f6a7197adaaa1b888))
* **chat:** search filters for channel, author and attachments with jump to result ([e1783a5](https://github.com/msch128/mnema-talk/commit/e1783a5755d9949a0e67f0c1ec76c839790b36cf))
* **chat:** unread divider, typing line, notification controls and message keyboard navigation ([4de94aa](https://github.com/msch128/mnema-talk/commit/4de94aaaf9389502b9d3fa05f11cfe8f4ea5ed01))
* **nav:** add context menus for channels, categories and members ([4f8c204](https://github.com/msch128/mnema-talk/commit/4f8c2040a41745a7afa98eddc46fca2e2a7cf8d1))
* **ops:** add Prometheus metrics, health check script and coturn compose profile ([d5e9d58](https://github.com/msch128/mnema-talk/commit/d5e9d583b3b4ba50f9bb9e1cffbb8e48b27a93de))
* **sfu:** forward the camera as its own video source next to the screen share ([bf2b774](https://github.com/msch128/mnema-talk/commit/bf2b77488589568fd124bf9fcdc9c488461a6d3a))
* **sfu:** forward video only to viewers who subscribed ([802e451](https://github.com/msch128/mnema-talk/commit/802e451c9c1414c9de94ad48fb3218708c7e2838))
* **ui:** make context menu keyboard accessible with anchor positioning ([13dee3c](https://github.com/msch128/mnema-talk/commit/13dee3cbe10adbcfcba118914b330c1409564c0b))
* **voice:** AI noise suppression with DeepFilterNet3 ([45d82a5](https://github.com/msch128/mnema-talk/commit/45d82a5bcfb91a9af664b02635ed3be69a64f4e1))
* **voice:** cap screen and camera bitrate and keep screen resolution under congestion ([b22b944](https://github.com/msch128/mnema-talk/commit/b22b9449087bd78a0eb7da102d76ffe66a5d10c6))
* **voice:** opt in to screen shares, opt out of cameras ([d4c3f4c](https://github.com/msch128/mnema-talk/commit/d4c3f4c2c464e9f6433ac56cfa317606681ab87b))
* **voice:** preview a roundtable without joining and show camera tiles ([5127093](https://github.com/msch128/mnema-talk/commit/5127093868ab7bde8be50e72ab7c1f6284cd3e77))
* **voice:** seamless screenshare start and stop using video transceiver ([22570c5](https://github.com/msch128/mnema-talk/commit/22570c578d9fc236fa0951cdb6447e8c7ed5f5ec))
* **voice:** share screen audio, per-user volume and webcam in the client ([56a7dd2](https://github.com/msch128/mnema-talk/commit/56a7dd2b0f01f5dc382a14f66bf7d6e59495b89a))
* **voice:** TURN relay support with time-limited credentials ([1335d47](https://github.com/msch128/mnema-talk/commit/1335d476d6d6eb298eb96ee2f82b80f8f6de5194))
* **web:** i18n (de/en), toasts, confirm dialogs, accessible dialogs, voice status panel ([385880c](https://github.com/msch128/mnema-talk/commit/385880c74703c7a8fd1a995ac48557df03603d04))
* **web:** serve precompressed static assets ([4892081](https://github.com/msch128/mnema-talk/commit/48920816b9c55c6533752236039ad66fd9daef66))
* **web:** URL routes, context menus, read state and link preview groundwork ([7c00a60](https://github.com/msch128/mnema-talk/commit/7c00a6041b9c7747e06e9f16fd51aeffaaa28df1))


### Bug Fixes

* **admin:** drop duplicate user admin routes, validate admin-set passwords ([bc80806](https://github.com/msch128/mnema-talk/commit/bc808060780eab26f938f85bba74e72a8ca65dbe))
* **chat:** cache link previews and skip links in code and spoilers ([d5837bf](https://github.com/msch128/mnema-talk/commit/d5837bf8b0d08efe65a6ab6556aefbdd969648a4))
* **ops:** refuse to start coturn without a secret, close relay bypasses ([169012d](https://github.com/msch128/mnema-talk/commit/169012da2c458d5d94f219f4f7cb9b2fd864c1f4))
* **ops:** require METRICS_TOKEN for /api/metrics, harden coturn relay ([205a383](https://github.com/msch128/mnema-talk/commit/205a3833fa0e2c65a8b5e9849942503c1cbc2969))
* **release:** stop re-releasing 0.1.0 after main was rewritten ([b892bcd](https://github.com/msch128/mnema-talk/commit/b892bcd917a8dfa27103e2c4e56ebb41a39f1e36))
* **routes:** guard admin routes, follow joined voice channel and fix admin history ([1dbc7e0](https://github.com/msch128/mnema-talk/commit/1dbc7e0226903c7e8cfa35ffc2a25d641a8f180e))
* **voice:** allow the webcam in the Permissions-Policy ([644df19](https://github.com/msch128/mnema-talk/commit/644df19236abb1946afad4aed48a9217c7cd5c6a))
* **voice:** keep voice active and prevent disconnect in background tabs ([9d7ac2e](https://github.com/msch128/mnema-talk/commit/9d7ac2ee08124aa06795ca6660c6f47abdc7521c))
* **voice:** open connection details from the ping in the voice panel ([0890904](https://github.com/msch128/mnema-talk/commit/089090462deb1497b72d9493b92ccf2441f7bfd5))
* **web:** tooltip on the search dialog close button ([2e45c02](https://github.com/msch128/mnema-talk/commit/2e45c0286ba731c4b779823c0c9765cdab3faa70))

## 0.1.0 (2026-10-05)


### ⚠ BREAKING CHANGES

* direct messages are removed. Auth moved from bearer tokens to session cookies. Migrations moved to internal/db/migrations and new env variables are required in production (see .env.example).

### Features

* add direct messaging (DMs) and server-routed 1-on-1 calls ([a1beba3](https://github.com/msch128/mnema-talk/commit/a1beba3fa1fe7e67ee25c7fc9c2d872b6a96f06f))
* add env-configurable privacy policy, legal disclosures, and terms ([28c4ede](https://github.com/msch128/mnema-talk/commit/28c4ede36ec3df0783d6cd11044fb091f0a28557))
* add real-time ping telemetry, RTC connection stats modal, right member list and voice center chat ([b0423e8](https://github.com/msch128/mnema-talk/commit/b0423e83f37c60555476cd79d6f75067a72a8f25))
* add Rocket.Chat threads, fix channel messaging and inline image uploads ([437b6bb](https://github.com/msch128/mnema-talk/commit/437b6bbd675ef1c0a6d16dea56edf3671433dd53))
* **admin:** user management, voice kick and last seen ([582b9d5](https://github.com/msch128/mnema-talk/commit/582b9d5b63f47fa4dcc0fbcc2ff12d2764f96440))
* **audio:** add Discord-identical input sensitivity, live visual noise gate and acoustic settings ([02bd618](https://github.com/msch128/mnema-talk/commit/02bd61850ebe5b0e18d5104920262cb11026d8c9))
* **auth:** per-account UI language ([b1cdc7e](https://github.com/msch128/mnema-talk/commit/b1cdc7ed8301cd1e96d2c33b4e6ce3a260cd908e))
* **channels:** add channel & category management, default channel seeding and creation modal ([6cfe8e6](https://github.com/msch128/mnema-talk/commit/6cfe8e687909c69cedcbf3163021c4e322f58d42))
* **chat:** message search with filters ([7605b00](https://github.com/msch128/mnema-talk/commit/7605b0058f879062bf840640c7935f9e23729d07))
* **chat:** server-side read state, mentions and notification levels ([a8b154f](https://github.com/msch128/mnema-talk/commit/a8b154f7bee38fee633f1633c870bab9efc9b42d))
* **frontend:** Vue 3 Discord UI, WebRTC voice/screen, Docker multi-stage build ([9ffbfdc](https://github.com/msch128/mnema-talk/commit/9ffbfdc15b5296b4c979995d8a47fc073de4fd9e))
* harden, restructure and test the whole app ([379635c](https://github.com/msch128/mnema-talk/commit/379635c6baab2c99ec25bc232dd0ef18fe40b735))
* implement backend channels, chat, s3 upload, websockets, sfu and admin retention engine ([8bf7982](https://github.com/msch128/mnema-talk/commit/8bf79825600d26c21a10958d8c02e606efbacb53))
* implement phase 1 foundation (postgres 17, seaweedfs s3, go core & auth) ([a83a15f](https://github.com/msch128/mnema-talk/commit/a83a15feeb6c65b3f07a2e7a3468c808cbf784c6))
* implement SFU audio & screensharing, message edit/delete, emoji reactions, user bio, and resolve dependabot security vulnerabilities ([4ee831a](https://github.com/msch128/mnema-talk/commit/4ee831a5cc9c25ac5a5b675fb8c22170713ec881))
* live mic meter and noise gate test in audio settings ([5c0953f](https://github.com/msch128/mnema-talk/commit/5c0953f33ce2f5925b084beb39ae9a9423771361))
* **ops:** backup and restore scripts with a verify mode ([01d1a12](https://github.com/msch128/mnema-talk/commit/01d1a12ff52c0f2e85ec1190ecb291bdc86e01c6))
* SSRF-safe link previews with an image proxy ([ae264c1](https://github.com/msch128/mnema-talk/commit/ae264c14edb64e55b33e1ca8a178c1911b05c8e5))
* **talk:** implement dedicated VoiceStage hangout view, 4K screen showcase & Mnema layout ([151725f](https://github.com/msch128/mnema-talk/commit/151725f704156dc9a7c7d0ce897e393e29e865ae))
* **ws:** relay typing notices to the others, throttled ([30d4032](https://github.com/msch128/mnema-talk/commit/30d403262ea2ffd0e820b6c526d00890e089d918))


### Bug Fixes

* **audio:** keep the input level visible below the noise gate threshold ([960bd8d](https://github.com/msch128/mnema-talk/commit/960bd8dff11e4d0025817b94d04267870de9b71f))
* **auth:** per-address login lockouts, log out everywhere, one APP_ENV ([e92b14e](https://github.com/msch128/mnema-talk/commit/e92b14e0ff69d44e493be86ebefe72e324c83f35))
* build with go1.27 toolchain and x/text v0.39 to close stdlib vulnerabilities ([5717cfb](https://github.com/msch128/mnema-talk/commit/5717cfb5f23a6a74019c959139ad2c5aee8e322e))
* **config:** parse WebRTC UDP ports as uint16 ([e3e2e6b](https://github.com/msch128/mnema-talk/commit/e3e2e6bfca61e2f964a1b5372f6ac91d225f8dfb))
* **deps:** bump chi, x/net, x/crypto and govulncheck v1.8.0 ([2afe5f7](https://github.com/msch128/mnema-talk/commit/2afe5f78838a192032c1c4f7f261b57391cc6758))
* ensure channels and messages serialize as empty arrays rather than null ([a29c134](https://github.com/msch128/mnema-talk/commit/a29c1343c1eb298664ef29fec89a52a900febc0b))
* ensure parent_id column exists before creating index ([764d156](https://github.com/msch128/mnema-talk/commit/764d156493c24a4c8b0355cd770e5d6c113e4571))
* harden link preview SSRF guard and release auto-merge ([72c67fc](https://github.com/msch128/mnema-talk/commit/72c67fc74139dcea728d2d82a12cd5fc80a5b9a7))
* import encoding/json in upload.go ([087622f](https://github.com/msch128/mnema-talk/commit/087622f217bbde520a3b98121df23d10248ce8fe))
* **media:** range requests for media and delete objects with their messages ([38bafd0](https://github.com/msch128/mnema-talk/commit/38bafd00174f5a3bf03acf9153a2ddd4351a4b34))
* **member-list:** dock member list to right window edge like discord ([3cbe5fa](https://github.com/msch128/mnema-talk/commit/3cbe5fa1ee2bdb0960f46d472bbf0b71eceb9604))
* meter mic level on a cloned track so the noise gate can reopen ([557f453](https://github.com/msch128/mnema-talk/commit/557f453c6d9ffe0abf5d708ec6fbfd6ef1974a1c))
* place reactions at message bottom and improve hover tolerance with safe hitbox ([a2d75dc](https://github.com/msch128/mnema-talk/commit/a2d75dcf0f6c845d53a614b18167996a6a5f2e5a))
* remove hardcoded secret defaults, add auth rate limiting and password change ([ca406a6](https://github.com/msch128/mnema-talk/commit/ca406a6ad4eca7f9ee3c22e13f4b2ad7e3216778))
* **s3:** remove unused time import ([3538646](https://github.com/msch128/mnema-talk/commit/3538646ef48bb366e74ea957d4c20366609b21b7))
* **sfu:** forward keyframe requests to publishers and keep peers on rejoin ([81283b3](https://github.com/msch128/mnema-talk/commit/81283b307104d4ef9672db0b877c9991a1db7d95))
* update Dockerfile to golang:alpine and sync go.sum for go 1.25 ([a699ac4](https://github.com/msch128/mnema-talk/commit/a699ac4fd80ab807028573acf391fa0dc1f784d1))
* **voice:** meter the live mic level independently of the noise gate ([7f57772](https://github.com/msch128/mnema-talk/commit/7f57772f2948cbc37496b6e4b43697cac74dd3cb))
* **web:** survive reconnects and fix voice races in the client ([04c7137](https://github.com/msch128/mnema-talk/commit/04c7137cc6a67c077c545eb1346f063f76595620))


### Miscellaneous Chores

* keep releases on 0.x until 1.0.0 is cut deliberately ([c58f0b2](https://github.com/msch128/mnema-talk/commit/c58f0b2b65107fe8211c00e8e09e5f3d6d7001f7))

## Changelog

All notable changes are recorded here by release-please from conventional commits.
