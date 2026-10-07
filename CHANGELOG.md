# Changelog

## [0.6.1](https://github.com/msch128/mnema-talk/compare/v0.6.0...v0.6.1) (2026-10-07)


### Bug Fixes

* isolate active members and session lifecycle races ([#73](https://github.com/msch128/mnema-talk/issues/73)) ([58704c3](https://github.com/msch128/mnema-talk/commit/58704c36572c7c9ebeb478059c420b94cb04429a))

## [0.6.0](https://github.com/msch128/mnema-talk/compare/v0.5.0...v0.6.0) (2026-10-07)


### ⚠ BREAKING CHANGES

* existing installs must run ./scripts/upgrade-postgres.sh once instead of docker compose up -d (doc/upgrade.md, 0.4.x -> 0.5.0).

### Bug Fixes

* validate frontend contracts with strict TypeScript ([#70](https://github.com/msch128/mnema-talk/issues/70)) ([d03dac5](https://github.com/msch128/mnema-talk/commit/d03dac59ff89492f38de94e68116f56eccf51918))

## [0.5.0](https://github.com/msch128/mnema-talk/compare/v0.4.4...v0.5.0) (2026-10-07)


### ⚠ BREAKING CHANGES

* existing installs must run ./scripts/upgrade-postgres.sh once instead of docker compose up -d (doc/upgrade.md, 0.4.x -> 0.5.0).

### Features

* harden backend, voice and CI; PostgreSQL 18 ([#69](https://github.com/msch128/mnema-talk/issues/69)) ([57e8915](https://github.com/msch128/mnema-talk/commit/57e891537bec348a46259bd5fd1d4cc9a63effb0))

## [0.4.4](https://github.com/msch128/mnema-talk/compare/v0.4.3...v0.4.4) (2026-10-07)


### Bug Fixes

* recover audio filtering after temporary worker stalls ([0ffcc14](https://github.com/msch128/mnema-talk/commit/0ffcc142798a80f90b5bc92ddea66b977c660151))
* stabilize media startup, playback and recovery ([db1931f](https://github.com/msch128/mnema-talk/commit/db1931f6cb2f5e0d141d201c38c82594708de8a5))

## [0.4.3](https://github.com/msch128/mnema-talk/compare/v0.4.2...v0.4.3) (2026-10-07)


### Bug Fixes

* harden media signaling and live connection recovery ([1e91ff8](https://github.com/msch128/mnema-talk/commit/1e91ff86a1b017ebf8de918c2314513a831cc898))

## [0.4.2](https://github.com/msch128/mnema-talk/compare/v0.4.1...v0.4.2) (2026-10-06)


### Features

* **talk:** expose stream settings in compact talk bar and add menu tests ([417fd09](https://github.com/msch128/mnema-talk/commit/417fd09305a13d74d429d87cdbbcc229ffcc9e7c))
* **talk:** screen share quality selection modal (720p–Source, 15/30/60fps) ([3d0ffbe](https://github.com/msch128/mnema-talk/commit/3d0ffbe02a2962c6ce98dd0e1859f602cd3cd594))


### Bug Fixes

* **talk:** unify screen quality storage key across store and modal ([781f5d3](https://github.com/msch128/mnema-talk/commit/781f5d399f66eb3e43a4b84ca00468ff75f90ef1))

## [0.4.1](https://github.com/msch128/mnema-talk/compare/v0.4.0...v0.4.1) (2026-10-06)


### Features

* **talk:** WIP floating auto-hide control bar and collapsible participant strip ([#62](https://github.com/msch128/mnema-talk/issues/62)) ([355e7a4](https://github.com/msch128/mnema-talk/commit/355e7a4e60cf6f1cb83c2e0470b5b6eed792a534))

## [0.4.0](https://github.com/msch128/mnema-talk/compare/v0.3.4...v0.4.0) (2026-10-06)


### Continuous Integration

* don't fail the release run when the tip guard skips release-please ([#63](https://github.com/msch128/mnema-talk/issues/63)) ([8433baa](https://github.com/msch128/mnema-talk/commit/8433baaf81571e07cccff40827dbfba03901a0a7))

## [0.3.4](https://github.com/msch128/mnema-talk/compare/v0.3.3...v0.3.4) (2026-10-06)


### Features

* **talk:** stream quality menu like Discord (mode, resolution, frame rate) ([9e0bf6c](https://github.com/msch128/mnema-talk/commit/9e0bf6c7e08eed32042eed5d4a83b3d3ee6b773c))


### Bug Fixes

* **talk:** the stage's volume slider controls the stream's audio, 0-100 % ([a98ffed](https://github.com/msch128/mnema-talk/commit/a98ffed63801d91888a17c0c6d639d7076919b7b))

## [0.3.3](https://github.com/msch128/mnema-talk/compare/v0.3.2...v0.3.3) (2026-10-06)


### Features

* **talk:** text chat as a resizable panel under the stage, like Discord ([9dca44a](https://github.com/msch128/mnema-talk/commit/9dca44a819df8868b19103994a27f0962915aefe))


### Bug Fixes

* **chat:** voice channels have their own text chat ([761e54f](https://github.com/msch128/mnema-talk/commit/761e54f8eec423266a5141989e2e403e47d8d774))
* **talk:** a /v/:id/chat deep link stays open when the call resumes first ([9446cb4](https://github.com/msch128/mnema-talk/commit/9446cb43705f57086d55ff88d60f23eb0fdf3c1a))
* **talk:** the Talk chat's header shows the whole channel name ([f3d743e](https://github.com/msch128/mnema-talk/commit/f3d743e47f51b5ac2cb19cd8bf1458dd06effd20))

## [0.3.2](https://github.com/msch128/mnema-talk/compare/v0.3.1...v0.3.2) (2026-10-06)


### Features

* **admin:** check GitHub for new releases every 30 minutes ([5d0ec7e](https://github.com/msch128/mnema-talk/commit/5d0ec7eabbd8493b531d0f749258c58ec5316b68))
* **admin:** optional self-update through an isolated updater sidecar ([017e08c](https://github.com/msch128/mnema-talk/commit/017e08c28d9d840e18623a0bb8e54cb0ddab1dff))
* **admin:** system tab with version and health ([4fa19db](https://github.com/msch128/mnema-talk/commit/4fa19db7cc9680811d5664571ea53469a5a4293d))
* **build:** bake version and revision into the binary and the web app ([7e47588](https://github.com/msch128/mnema-talk/commit/7e475883ebb09a8b9f0e201ae7b1c27b27a6158e))
* **web:** offer a reload when the server runs a newer version ([c112178](https://github.com/msch128/mnema-talk/commit/c1121782957407dd5277d9b723ab7f5fa7cb2c75))

## [0.3.1](https://github.com/msch128/mnema-talk/compare/v0.3.0...v0.3.1) (2026-10-06)


### Features

* **docker:** publish release images for linux/amd64 and linux/arm64 ([efab9e7](https://github.com/msch128/mnema-talk/commit/efab9e786ee99b7ef2f0ebe973860772cd4e1246))

## [0.3.0](https://github.com/msch128/mnema-talk/compare/v0.1.16...v0.3.0) (2026-10-05)


### Features

* Discord-style sidebar management and Talk stage (0.3.0) ([#42](https://github.com/msch128/mnema-talk/issues/42)) ([4e514c4](https://github.com/msch128/mnema-talk/commit/4e514c4ee3d762ab34d41388e62c7840eda94566))


### Bug Fixes

* **talk:** keep a camera decodable when the SFU renegotiates ([6434340](https://github.com/msch128/mnema-talk/commit/6434340b48c1f244085943a279d5cf5ca5642862))

## [0.1.16](https://github.com/msch128/mnema-talk/compare/v0.1.15...v0.1.16) (2026-10-05)


### Features

* **auth:** add AuthenticateRequest returning the session's token version ([978a6a2](https://github.com/msch128/mnema-talk/commit/978a6a279cf97541e87d8d6bdedf57f7457ed122))


### Bug Fixes

* **admin:** keep unsaved layout edits, enforce the 10-char password minimum, surface load errors; split the dashboard per tab ([5d0509d](https://github.com/msch128/mnema-talk/commit/5d0509d9636bc14df9cd140cdf01b57defc7aaf9))
* **audio:** play sound effects on the chosen output device ([293e8a0](https://github.com/msch128/mnema-talk/commit/293e8a01574bb4406264b63c514d8e8afe3d00c6))
* **auth:** log only the generated initial admin password, never a configured one ([3923e0f](https://github.com/msch128/mnema-talk/commit/3923e0fa61420ab4a3e7bde87f26cc3d7d10c205))
* **auth:** never lock the owner out, bound login limiter keys, close sockets on revocation ([d6a1385](https://github.com/msch128/mnema-talk/commit/d6a13856b4a51109488173251c90b70469cc4369))
* **auth:** seed the administrator through auth with full validation ([f36d063](https://github.com/msch128/mnema-talk/commit/f36d0637699992dd7f1e824bf53bd477c1cd84d8))
* **chat:** bound reactions, harden edits and deletes, cheap member list ([df55710](https://github.com/msch128/mnema-talk/commit/df55710f81ef84df8128079419436217228bcafb))
* **chat:** disconnect everyone from a deleted voice channel ([103a970](https://github.com/msch128/mnema-talk/commit/103a97017dbb6c5ebb735bb61f3862c85f3b0a08))
* **chat:** drop stale thread and profile loads, keep partial user updates ([78fa283](https://github.com/msch128/mnema-talk/commit/78fa283933cd2959f7aadfd8c4f42f5503056f29))
* **chat:** strip only the markdown that is rendered in previews ([ec59027](https://github.com/msch128/mnema-talk/commit/ec5902741d96baeaecb18d76d7e61f4e4db77cd0))
* **compose:** require S3 credentials on the SeaweedFS gateway ([62f5b99](https://github.com/msch128/mnema-talk/commit/62f5b997238a53a27967bfc2b64e6541be369533))
* **config:** reject all example placeholders and invalid values, trust only loopback proxies by default ([a00e6e2](https://github.com/msch128/mnema-talk/commit/a00e6e29f04d67659d925be7664f196fa01f2b4a))
* **db:** enforce case-insensitive unique usernames and drop redundant indexes ([df70630](https://github.com/msch128/mnema-talk/commit/df70630b33b34797789b6442fb715ba01518bd84))
* **db:** record migration checksums and refuse edited migrations ([f5256f5](https://github.com/msch128/mnema-talk/commit/f5256f57e79b9db26002aebbc24497c9abc574ef))
* **httpx:** bound limiter sweeps and size, key IPv6 per /64, join X-Forwarded-For lines ([11859c1](https://github.com/msch128/mnema-talk/commit/11859c156c5d9a0eec597784b5aabda42f6613fb))
* **i18n:** add chat.uploadFailed and drop unused keys ([470ca7b](https://github.com/msch128/mnema-talk/commit/470ca7b0f50c25354f05c1713769d52c343eeee5))
* **linkpreview:** keep denied addresses on DNS failure, share fetches and cache images ([cf5a238](https://github.com/msch128/mnema-talk/commit/cf5a238ac4c6672f65066acb7492bd244675a6bd))
* **make:** tolerate a missing scorecard, require Node 24 and gofmt api/ ([0e1df45](https://github.com/msch128/mnema-talk/commit/0e1df45cb968d38198b17e8fd81c1370cf07edd4))
* **media:** serve missing objects as 410 and prune in marked batches ([9f58f9d](https://github.com/msch128/mnema-talk/commit/9f58f9d1c56455d01acb3d9d155ea6858795cc93))
* **nav:** stop a superseded route after each await ([e7c3c14](https://github.com/msch128/mnema-talk/commit/e7c3c14bddeab79eb38d91300ce7921f24faa372))
* profile changes (avatar, name, status) reach voice lists and open threads ([6e24044](https://github.com/msch128/mnema-talk/commit/6e240444283cfe01eb59e2251c59f770b98cfbc7))
* **s3:** only create the bucket when it is missing and retry storage init ([6db4f88](https://github.com/msch128/mnema-talk/commit/6db4f88029f423da54b9691b997c09a5c7cf5c47))
* **scripts:** keep backups out of the checkout and copy SeaweedFS consistently ([fe981a2](https://github.com/msch128/mnema-talk/commit/fe981a2e97378cac7511c246cf9fb00177a445f3))
* **server:** cache health checks, report storage outages and stop background work ([376b099](https://github.com/msch128/mnema-talk/commit/376b099387ac42afc5f2e3fe3cc69e572893a920))
* **sfu:** keep forwarding when one subscriber's write fails ([d4363ba](https://github.com/msch128/mnema-talk/commit/d4363bae6fee8f7ed873f00face2ce2a8bde7137))
* **talk:** only watch a screen after the join went ahead ([8c7a5e4](https://github.com/msch128/mnema-talk/commit/8c7a5e453a235068b3c2425e34adcc931ced3431))
* **thread:** keyboard access, video attachments and upload retry in the thread panel ([63a1e7b](https://github.com/msch128/mnema-talk/commit/63a1e7bfa5ce42c3e9e8468efd2e42e503636e27))
* **voice:** end the call locally when an admin removes me from voice ([d388cf5](https://github.com/msch128/mnema-talk/commit/d388cf5d52dfd3e4d4bcb8186e5fa8e5433526ec))
* **voice:** key remote audio elements by track ([fe29995](https://github.com/msch128/mnema-talk/commit/fe29995aff45726928680dce3b282bd28226665a))
* **voice:** reapply sender limits after a reconnect or channel switch ([b301b65](https://github.com/msch128/mnema-talk/commit/b301b6571d5c4ed3e66db625ddbfd418983b16d8))
* **voice:** register the settings watchers of useWebRTC once ([9eaae9e](https://github.com/msch128/mnema-talk/commit/9eaae9e92650ed481f28a42cb667aa63e5a9d7b5))
* **voice:** release the mic when the mic test is stopped mid-prompt ([b5426b7](https://github.com/msch128/mnema-talk/commit/b5426b71e633c42e466b0891a81afc50cff09ee4))
* **voice:** serialize audio settings changes so screen audio survives ([04c1a3c](https://github.com/msch128/mnema-talk/commit/04c1a3c2c8354e420eb966c86014ba2e669473ea))
* **voice:** stop media that a superseded join or share acquired ([dfe0971](https://github.com/msch128/mnema-talk/commit/dfe09712e6fb44b2b756881649b5fdad58657a4a))
* **web:** build the search modal on BaseDialog and format dates in the UI language ([5609913](https://github.com/msch128/mnema-talk/commit/5609913aec52dba33722e8f1f1f7ec40ef2b02be))
* **web:** load the current bio when editing the profile instead of the stub ([d57cbfc](https://github.com/msch128/mnema-talk/commit/d57cbfc6fc86eb00ab1f78099d72104ff0d67e1a))
* **web:** show the existing name-required message in the rename dialog ([ad6daa9](https://github.com/msch128/mnema-talk/commit/ad6daa9425b2754644e01f1e57381997f6adf26b))
* **web:** stop the mic test when audio settings close while it starts ([0b9fcb3](https://github.com/msch128/mnema-talk/commit/0b9fcb3bfbb419aeebf13fe6343e1b516f12b689))
* **ws:** keep the session cookie's token version on the connection ([9b3174f](https://github.com/msch128/mnema-talk/commit/9b3174f6c11aba964a5f2b9c298016f419a2fb35))
* **ws:** never close a client's send channel ([d7bc4b1](https://github.com/msch128/mnema-talk/commit/d7bc4b195414865a15ce92ed0aa358179ec63e9a))
* **ws:** serialize voice joins and leaves and scope voice state per room ([0cfb3a6](https://github.com/msch128/mnema-talk/commit/0cfb3a63c6ced405efc621c44ce8756261e50ca4))

## [0.1.15](https://github.com/msch128/mnema-talk/compare/v0.1.14...v0.1.15) (2026-10-05)


### Bug Fixes

* **voice:** stop the audio loop when two people share system audio ([#34](https://github.com/msch128/mnema-talk/issues/34)) ([78c8fd0](https://github.com/msch128/mnema-talk/commit/78c8fd0cab5de85730504df6b38c73b00faa7a52))

## [0.1.14](https://github.com/msch128/mnema-talk/compare/v0.1.13...v0.1.14) (2026-10-05)


### Bug Fixes

* **voice:** no self-echo through shared system audio; visible per-user volume slider ([#32](https://github.com/msch128/mnema-talk/issues/32)) ([c0fd610](https://github.com/msch128/mnema-talk/commit/c0fd6104947ce81f94262e6ce89232c6aff23904))

## [0.1.13](https://github.com/msch128/mnema-talk/compare/v0.1.12...v0.1.13) (2026-10-05)


### Features

* **voice:** screen share prefers H.264 and starts at a higher bitrate ([#30](https://github.com/msch128/mnema-talk/issues/30)) ([86e8bfb](https://github.com/msch128/mnema-talk/commit/86e8bfbb6198b228b5373f6c197c102957631ccb))

## [0.1.12](https://github.com/msch128/mnema-talk/compare/v0.1.11...v0.1.12) (2026-10-05)


### Bug Fixes

* **voice:** late joiners are heard; stream diagnostics ([7c646d4](https://github.com/msch128/mnema-talk/commit/7c646d465e8b59be7f94670827c86d14bc7a2629))

## [0.1.11](https://github.com/msch128/mnema-talk/compare/v0.1.10...v0.1.11) (2026-10-05)


### Features

* **voice:** volumes, output device, mute marks and a quiet own preview ([5c00e22](https://github.com/msch128/mnema-talk/commit/5c00e22c7b659d4939998c5bab1aba00e189b38c))

## [0.1.10](https://github.com/msch128/mnema-talk/compare/v0.1.9...v0.1.10) (2026-10-05)


### Bug Fixes

* **voice:** screen share and camera reach the server ([79f409f](https://github.com/msch128/mnema-talk/commit/79f409f76e36849aeae2046c5861dfbffac8d2fd))

## [0.1.9](https://github.com/msch128/mnema-talk/compare/v0.1.8...v0.1.9) (2026-10-05)


### Bug Fixes

* **voice:** pin the TURN relay address to the voice server's LAN IP ([1feb1d8](https://github.com/msch128/mnema-talk/commit/1feb1d8c60445fbfefbdfd71760b56c8e6d1b0ce))
* **voice:** TURN relay can reach the voice server; 404 for stale assets ([a66a25f](https://github.com/msch128/mnema-talk/commit/a66a25fb8a697beee250d02f9dbb023bb8218b0b))

## [0.1.8](https://github.com/msch128/mnema-talk/compare/v0.1.7...v0.1.8) (2026-10-05)


### Features

* **voice:** browsers report their connection diagnostics ([3e247ed](https://github.com/msch128/mnema-talk/commit/3e247ed3388c558a0f2b96d23f8a8171ba1074d0))

## [0.1.7](https://github.com/msch128/mnema-talk/compare/v0.1.6...v0.1.7) (2026-10-05)


### Features

* **voice:** Discord-parity screen sharing, mic test loopback, QoS and granular sound effects ([368eca9](https://github.com/msch128/mnema-talk/commit/368eca933095c9779e0653e7a25b71f5750a3697))

## [0.1.6](https://github.com/msch128/mnema-talk/compare/v0.1.5...v0.1.6) (2026-10-05)


### Features

* **ops:** LOG_LEVEL=debug for voice connection troubleshooting ([4a69bfe](https://github.com/msch128/mnema-talk/commit/4a69bfe382857d598c1fb44b8559fc97b4129e6c))

## [0.1.5](https://github.com/msch128/mnema-talk/compare/v0.1.4...v0.1.5) (2026-10-05)


### Bug Fixes

* **voice:** announce public and LAN addresses, log connection states ([a6bbd64](https://github.com/msch128/mnema-talk/commit/a6bbd64dd3c28635ddd709d2310659520d9c1529))

## [0.1.4](https://github.com/msch128/mnema-talk/compare/v0.1.3...v0.1.4) (2026-10-05)


### Features

* **api:** generated OpenAPI 3.1 docs and Swagger UI at /api/docs ([0ec32d2](https://github.com/msch128/mnema-talk/commit/0ec32d2c661c7c9873790cb8e7cc8b5b7619f31d))
* Talk timers and activity totals ([6f9c39f](https://github.com/msch128/mnema-talk/commit/6f9c39f2945fc689eb12ae7ed3411c3bb0a100ee))

## [0.1.3](https://github.com/msch128/mnema-talk/compare/v0.1.2...v0.1.3) (2026-10-05)


### Features

* **api:** presence choice, status text and server-side mentions ([1de9a5c](https://github.com/msch128/mnema-talk/commit/1de9a5c037ac9ca0d9560d2a9fafb4a2bf0141b1))
* **web:** presence menu, status text, [@mentions](https://github.com/mentions) and emoji picker ([a8827eb](https://github.com/msch128/mnema-talk/commit/a8827eb8a47cb51f4a4636f4a9d005446c215d45))

## [0.1.2](https://github.com/msch128/mnema-talk/compare/v0.1.1...v0.1.2) (2026-10-05)


### Bug Fixes

* resolve CodeQL findings in test code ([#17](https://github.com/msch128/mnema-talk/issues/17)) ([644b34c](https://github.com/msch128/mnema-talk/commit/644b34c2275dcf7b3140bdf59ecc47e6a28eb325))

## [0.1.1](https://github.com/msch128/mnema-talk/compare/v0.1.0...v0.1.1) (2026-10-05)


### Features

* **admin:** add user moderation and channel layout management ([e67816a](https://github.com/msch128/mnema-talk/commit/e67816a8cbcf4c79f25481148e380aadd0563e5a))
* **admin:** rename channels and categories, reorder by drag and drop ([fe6dfc7](https://github.com/msch128/mnema-talk/commit/fe6dfc7a1dbec262ce5ee72e2d5854d385b141e3))
* **chat:** mark the open channel read, notification levels and typing expiry in the store ([5a4211a](https://github.com/msch128/mnema-talk/commit/5a4211a9119774dfef6c7664f9f79b27244680eb))
* **chat:** page thread replies with limit and cursor ([dfa5d3f](https://github.com/msch128/mnema-talk/commit/dfa5d3fb7d5c5e02c38739e62988a363d4e2501c))
* **chat:** search filters for channel, author and attachments with jump to result ([e544f6d](https://github.com/msch128/mnema-talk/commit/e544f6dfb81bf50101f70a6f9a807f31e754a6bc))
* **chat:** unread divider, typing line, notification controls and message keyboard navigation ([f437fe7](https://github.com/msch128/mnema-talk/commit/f437fe7c3ce159725ee9e0636de3d90981a80774))
* **nav:** add context menus for channels, categories and members ([cffedd6](https://github.com/msch128/mnema-talk/commit/cffedd6798a94199ea57bb81c6e61d26d8c91e50))
* **ops:** add Prometheus metrics, health check script and coturn compose profile ([7d8e834](https://github.com/msch128/mnema-talk/commit/7d8e8347f8cfeeb2ed35bf342d90a2dfa57eeb9d))
* **sfu:** forward the camera as its own video source next to the screen share ([969eaeb](https://github.com/msch128/mnema-talk/commit/969eaeb6a0e33a38d01753bf6ed09686f1ffef39))
* **sfu:** forward video only to viewers who subscribed ([8e19e13](https://github.com/msch128/mnema-talk/commit/8e19e13ca5625f9ff2aab2acd37a5788cd5ffee0))
* **ui:** make context menu keyboard accessible with anchor positioning ([8b3b55e](https://github.com/msch128/mnema-talk/commit/8b3b55e6f04aec701714a597a53dde6069483f82))
* **voice:** AI noise suppression with DeepFilterNet3 ([4e0ef4f](https://github.com/msch128/mnema-talk/commit/4e0ef4f34942f95a19dab01fc046246381c00c0b))
* **voice:** cap screen and camera bitrate and keep screen resolution under congestion ([1ef0c1c](https://github.com/msch128/mnema-talk/commit/1ef0c1cd37658a3153a8039dd51129382855e22d))
* **voice:** opt in to screen shares, opt out of cameras ([28b7a4e](https://github.com/msch128/mnema-talk/commit/28b7a4e4be6f059f6aa6a08758c9d10148369cb9))
* **voice:** preview a roundtable without joining and show camera tiles ([33676be](https://github.com/msch128/mnema-talk/commit/33676be19557b6983b2a8f5b1248c0221790d9a5))
* **voice:** seamless screenshare start and stop using video transceiver ([cf006e3](https://github.com/msch128/mnema-talk/commit/cf006e311565c09983ffc9eaa1730fdfd8bbf2ff))
* **voice:** share screen audio, per-user volume and webcam in the client ([bfa861b](https://github.com/msch128/mnema-talk/commit/bfa861b4c39c630773004f9682f19daa159d3bf1))
* **voice:** TURN relay support with time-limited credentials ([475ff9d](https://github.com/msch128/mnema-talk/commit/475ff9dc7ba537b7d6b98b75849ab9c0730ae304))
* **web:** i18n (de/en), toasts, confirm dialogs, accessible dialogs, voice status panel ([54c6749](https://github.com/msch128/mnema-talk/commit/54c674902c81f3134d68c8b77d677dcb088356b0))
* **web:** serve precompressed static assets ([e13f0cc](https://github.com/msch128/mnema-talk/commit/e13f0cca172814500c8438541f251a8e456b94a1))
* **web:** URL routes, context menus, read state and link preview groundwork ([79c82f9](https://github.com/msch128/mnema-talk/commit/79c82f9d7f57b0ac36257ca1efb36629ea61f121))


### Bug Fixes

* **admin:** drop duplicate user admin routes, validate admin-set passwords ([6a7b746](https://github.com/msch128/mnema-talk/commit/6a7b7463891940c5ad3893845d76b644f37ad3ca))
* **chat:** cache link previews and skip links in code and spoilers ([e2b38ee](https://github.com/msch128/mnema-talk/commit/e2b38eef68f813790c656d3eca1e9fa5c2369318))
* **ops:** refuse to start coturn without a secret, close relay bypasses ([476b5f6](https://github.com/msch128/mnema-talk/commit/476b5f6f6be43035d7fd9e910faad58b1837554a))
* **ops:** require METRICS_TOKEN for /api/metrics, harden coturn relay ([c6a3fa9](https://github.com/msch128/mnema-talk/commit/c6a3fa95ebd4549bcffa1a733ee84842de5aeb3c))
* **release:** stop re-releasing 0.1.0 after main was rewritten ([2dbe341](https://github.com/msch128/mnema-talk/commit/2dbe341bd9ce016e921ce6c9bf0099b59d102ec8))
* **routes:** guard admin routes, follow joined voice channel and fix admin history ([d4b992f](https://github.com/msch128/mnema-talk/commit/d4b992f3bb8114772dc6882430153d02190c9d75))
* **voice:** allow the webcam in the Permissions-Policy ([d699e90](https://github.com/msch128/mnema-talk/commit/d699e90a3a7ee7aaa472fc44c2fe150b637f2ee0))
* **voice:** keep voice active and prevent disconnect in background tabs ([d5cfdc5](https://github.com/msch128/mnema-talk/commit/d5cfdc506ac5e9f79ebad230776094db73feaa7d))
* **voice:** open connection details from the ping in the voice panel ([febbcde](https://github.com/msch128/mnema-talk/commit/febbcde4fd6f72757f0a0ba44cfc9e2d363be8af))
* **web:** tooltip on the search dialog close button ([32627c6](https://github.com/msch128/mnema-talk/commit/32627c6533a658adca511e5b6448892a0af2882a))

## 0.1.0 (2026-10-05)


### ⚠ BREAKING CHANGES

* direct messages are removed. Auth moved from bearer tokens to session cookies. Migrations moved to internal/db/migrations and new env variables are required in production (see .env.example).

### Features

* add direct messaging (DMs) and server-routed 1-on-1 calls ([2403e14](https://github.com/msch128/mnema-talk/commit/2403e14f69610e375ad0d48f19818cb50102ec9f))
* add env-configurable privacy policy, legal disclosures, and terms ([71f2f77](https://github.com/msch128/mnema-talk/commit/71f2f771dc0427513f470dfb9baf2c369864ab0e))
* add real-time ping telemetry, RTC connection stats modal, right member list and voice center chat ([cec54e8](https://github.com/msch128/mnema-talk/commit/cec54e87b75a2465985696d418b910b32b6dff11))
* add Rocket.Chat threads, fix channel messaging and inline image uploads ([e84a44a](https://github.com/msch128/mnema-talk/commit/e84a44aa96d6ac358abdf959dadf9b359d5e2934))
* **admin:** user management, voice kick and last seen
* **audio:** add Discord-identical input sensitivity, live visual noise gate and acoustic settings ([d36a7eb](https://github.com/msch128/mnema-talk/commit/d36a7ebb3582d5d15969eda585f65ee016f31bb3))
* **auth:** per-account UI language
* **channels:** add channel & category management, default channel seeding and creation modal ([c4cb3ea](https://github.com/msch128/mnema-talk/commit/c4cb3eac046ea752992831b0d8140488b667f216))
* **chat:** message search with filters
* **chat:** server-side read state, mentions and notification levels
* **frontend:** Vue 3 Discord UI, WebRTC voice/screen, Docker multi-stage build ([ae87297](https://github.com/msch128/mnema-talk/commit/ae87297257b40f39e4a84283cf95833da75a24c8))
* harden, restructure and test the whole app ([f6df9f4](https://github.com/msch128/mnema-talk/commit/f6df9f4339506e248e4ad829826cd0bb4a94492b))
* implement backend channels, chat, s3 upload, websockets, sfu and admin retention engine ([b0fa965](https://github.com/msch128/mnema-talk/commit/b0fa9655efc0f3d8971a65ae43661bc34112b237))
* implement phase 1 foundation (postgres 17, seaweedfs s3, go core & auth) ([7497c52](https://github.com/msch128/mnema-talk/commit/7497c52b8fadc970884e8cfda351aeada5507c3c))
* implement SFU audio & screensharing, message edit/delete, emoji reactions, user bio, and resolve dependabot security vulnerabilities ([22c2dbf](https://github.com/msch128/mnema-talk/commit/22c2dbf1a182d9774a8df0b78ec34fe1869df9fb))
* live mic meter and noise gate test in audio settings ([38a2b1c](https://github.com/msch128/mnema-talk/commit/38a2b1c3c73c44f39c188e172f1d97ba1e6add11))
* **ops:** backup and restore scripts with a verify mode
* SSRF-safe link previews with an image proxy
* **talk:** implement dedicated VoiceStage hangout view, 4K screen showcase & Mnema layout ([d603aaa](https://github.com/msch128/mnema-talk/commit/d603aaa1860affb561fb8e727ea086f2a94faefa))
* **ws:** relay typing notices to the others, throttled


### Bug Fixes

* **audio:** keep the input level visible below the noise gate threshold
* **auth:** per-address login lockouts, log out everywhere, one APP_ENV
* build with go1.27 toolchain and x/text v0.39 to close stdlib vulnerabilities ([375a7ed](https://github.com/msch128/mnema-talk/commit/375a7ed2f9ac67ed943437245321ff7299bf8387))
* **config:** parse WebRTC UDP ports as uint16
* **deps:** bump chi, x/net, x/crypto and govulncheck v1.8.0 ([957f164](https://github.com/msch128/mnema-talk/commit/957f164aa1ed627ff96a4b61c341c66f044c999f))
* ensure channels and messages serialize as empty arrays rather than null ([ba27c49](https://github.com/msch128/mnema-talk/commit/ba27c49527db40ec23e2038b0b19b7aad2f970a0))
* ensure parent_id column exists before creating index ([695efc5](https://github.com/msch128/mnema-talk/commit/695efc55156dbc2a6ec9df6eead3ceaee96f52d0))
* harden link preview SSRF guard and release auto-merge
* import encoding/json in upload.go ([26a7444](https://github.com/msch128/mnema-talk/commit/26a7444e176bebf62cca00490a1b5c3d54c9a2af))
* **media:** range requests for media and delete objects with their messages
* **member-list:** dock member list to right window edge like discord ([fad2de2](https://github.com/msch128/mnema-talk/commit/fad2de259e9928dbcf4395b0f86cd2133ab2192f))
* meter mic level on a cloned track so the noise gate can reopen ([fd9d6d3](https://github.com/msch128/mnema-talk/commit/fd9d6d3d65f1598ea9592a920a1c23e79882055a))
* place reactions at message bottom and improve hover tolerance with safe hitbox ([1ad9b6a](https://github.com/msch128/mnema-talk/commit/1ad9b6a78cff4bed7d2ed8e4cf8096672452a836))
* remove hardcoded secret defaults, add auth rate limiting and password change ([f5721d8](https://github.com/msch128/mnema-talk/commit/f5721d823d09d23e44706aefd466aaf417d4f082))
* **s3:** remove unused time import ([3c2f54b](https://github.com/msch128/mnema-talk/commit/3c2f54b7ca989147366259d2bb950657120d81ac))
* **sfu:** forward keyframe requests to publishers and keep peers on rejoin
* update Dockerfile to golang:alpine and sync go.sum for go 1.25 ([18a2dcc](https://github.com/msch128/mnema-talk/commit/18a2dcc60b1422887c6329ac15961668c3ba5c22))
* **voice:** meter the live mic level independently of the noise gate
* **web:** survive reconnects and fix voice races in the client


### Miscellaneous Chores

* keep releases on 0.x until 1.0.0 is cut deliberately

## Changelog

All notable changes are recorded here by release-please from conventional commits.
