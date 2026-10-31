# Changelog

## [2.3.0](https://github.com/msch128/mnema-talk/compare/v2.2.0...v2.3.0) (2026-10-31)


### Features

* **web:** translate every server error sentence and make the i18n check strict ([#575](https://github.com/msch128/mnema-talk/issues/575)) ([df5a6b8](https://github.com/msch128/mnema-talk/commit/df5a6b8185b37ace632b61ed9c2b9415404ee55d))


### Bug Fixes

* ask the SFU for the sender's own camera keyframe after a camera change ([#574](https://github.com/msch128/mnema-talk/issues/574)) ([df5a6b8](https://github.com/msch128/mnema-talk/commit/df5a6b8185b37ace632b61ed9c2b9415404ee55d))
* **web:** mark everything read when the desktop rail asks for it ([#573](https://github.com/msch128/mnema-talk/issues/573)) ([df5a6b8](https://github.com/msch128/mnema-talk/commit/df5a6b8185b37ace632b61ed9c2b9415404ee55d))

## [2.2.0](https://github.com/msch128/mnema-talk/compare/v2.1.1...v2.2.0) (2026-10-30)


### Features

* **chat:** edit your last message with ArrowUp ([#546](https://github.com/msch128/mnema-talk/issues/546)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **chat:** several attachments per message through staged uploads ([#551](https://github.com/msch128/mnema-talk/issues/551)) ([f941a51](https://github.com/msch128/mnema-talk/commit/f941a51021d2b5684c2589edb2be453323c3e0bf))
* **community:** let the Owner set a community icon ([#529](https://github.com/msch128/mnema-talk/issues/529)) ([f941a51](https://github.com/msch128/mnema-talk/commit/f941a51021d2b5684c2589edb2be453323c3e0bf))
* **dm:** ring the members of a conversation when a Talk is started in it ([#540](https://github.com/msch128/mnema-talk/issues/540)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **forum:** tag colours, order and moderated tags, rules, solved posts, search, unread and post uploads ([#552](https://github.com/msch128/mnema-talk/issues/552)) ([f941a51](https://github.com/msch128/mnema-talk/commit/f941a51021d2b5684c2589edb2be453323c3e0bf))
* **loadtest:** capacity test for the server host (chat, headless voice publishers, TURN relay) ([#539](https://github.com/msch128/mnema-talk/issues/539)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **onboarding:** guided first run for the Owner with a self-ticking Get started checklist ([#566](https://github.com/msch128/mnema-talk/issues/566)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **onboarding:** Test my connection, voice reach confirmed from outside, WebSocket check ([#533](https://github.com/msch128/mnema-talk/issues/533)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **setup:** guided assistant, CGNAT detection, doctor and a localhost trial ([#530](https://github.com/msch128/mnema-talk/issues/530)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **talks:** name temporary Talks after their lobby ([#531](https://github.com/msch128/mnema-talk/issues/531)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **updater:** trust the Docker Hub mirror next to GHCR and update from the running image's registry ([#522](https://github.com/msch128/mnema-talk/issues/522)) ([84c794c](https://github.com/msch128/mnema-talk/commit/84c794ca609e18ff74cb200e136cf7819674a529))
* **voice:** show N/limit on a Talk and let Disconnect members exceed the user limit ([#538](https://github.com/msch128/mnema-talk/issues/538)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **voice:** take over a call from another device ([#532](https://github.com/msch128/mnema-talk/issues/532)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** a first microphone check, a chosen push-to-talk key and settings that open where the member left off ([#557](https://github.com/msch128/mnema-talk/issues/557)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** a keyboard shortcut list, threads one click from a message, and clearer presence and notification choices ([#559](https://github.com/msch128/mnema-talk/issues/559)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** an attachment tray above the composer, up to 10 files per message ([#560](https://github.com/msch128/mnema-talk/issues/560)) ([f941a51](https://github.com/msch128/mnema-talk/commit/f941a51021d2b5684c2589edb2be453323c3e0bf))
* **web:** clearer admin defaults for invites, channels, settings and events ([#554](https://github.com/msch128/mnema-talk/issues/554)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** designed placeholder for deleted images ([#537](https://github.com/msch128/mnema-talk/issues/537)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** forum tags you can edit, colours and rules, search, unread markers and a post header ([#562](https://github.com/msch128/mnema-talk/issues/562)) ([f941a51](https://github.com/msch128/mnema-talk/commit/f941a51021d2b5684c2589edb2be453323c3e0bf))
* **web:** i18n audit check (warn mode) and translated event load error ([#542](https://github.com/msch128/mnema-talk/issues/542)) ([f941a51](https://github.com/msch128/mnema-talk/commit/f941a51021d2b5684c2589edb2be453323c3e0bf))
* **web:** image gallery layout and a media viewer with metadata ([#550](https://github.com/msch128/mnema-talk/issues/550)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** live stream quality, motion/clarity and a remembered choice ([#563](https://github.com/msch128/mnema-talk/issues/563)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** one Saved & reminders panel that says what saving means ([#556](https://github.com/msch128/mnema-talk/issues/556)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** redesign the formatting help in the composer ([#528](https://github.com/msch128/mnema-talk/issues/528)) ([729432f](https://github.com/msch128/mnema-talk/commit/729432fb2a7ca4495cfaa718dbabeb4b854d3ff4))
* **web:** right-click menus for the call buttons ([#536](https://github.com/msch128/mnema-talk/issues/536)) ([84c794c](https://github.com/msch128/mnema-talk/commit/84c794ca609e18ff74cb200e136cf7819674a529))
* **web:** show restrictions before they refuse, and test every quick-menu preset ([#545](https://github.com/msch128/mnema-talk/issues/545)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))


### Bug Fixes

* **chat:** react with every emoji in text channels, DMs and Talk chats ([#523](https://github.com/msch128/mnema-talk/issues/523)) ([84c794c](https://github.com/msch128/mnema-talk/commit/84c794ca609e18ff74cb200e136cf7819674a529))
* **chat:** sending reads the channel; feat(web): message sounds ([#553](https://github.com/msch128/mnema-talk/issues/553)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **legal:** privacy policy describes the built-in TURN relay and external TURN servers ([#524](https://github.com/msch128/mnema-talk/issues/524)) ([84c794c](https://github.com/msch128/mnema-talk/commit/84c794ca609e18ff74cb200e136cf7819674a529))
* **presence:** being in a Talk keeps the member from going auto-away ([#549](https://github.com/msch128/mnema-talk/issues/549)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **sfu:** Auto reaches the best layer that fits the viewer's picture and shows which one it chose ([#548](https://github.com/msch128/mnema-talk/issues/548)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **text:** count characters, not bytes or UTF-16 units, and never cut through an emoji ([#547](https://github.com/msch128/mnema-talk/issues/547)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **voice:** packets sent in the connection panel never increased ([#544](https://github.com/msch128/mnema-talk/issues/544)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** a closed forum post stays closed, and Escape closes the thread panel ([#534](https://github.com/msch128/mnema-talk/issues/534)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** a conversation list read before a call event no longer undoes it ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** game list editor and roomier layout in the gaming settings ([#526](https://github.com/msch128/mnema-talk/issues/526)) ([84c794c](https://github.com/msch128/mnema-talk/commit/84c794ca609e18ff74cb200e136cf7819674a529))
* **web:** notices that name who and where, and links that land on the exact field ([#558](https://github.com/msch128/mnema-talk/issues/558)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** one save pattern per settings screen, Talk settings in a single draft ([#555](https://github.com/msch128/mnema-talk/issues/555)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** onVideoSeenChange works without a document ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))
* **web:** set a status message in a dialog of its own and give the profile room ([#535](https://github.com/msch128/mnema-talk/issues/535)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))


### Performance Improvements

* **web:** idle means idle: pause timers, polling and video while the page is hidden ([#561](https://github.com/msch128/mnema-talk/issues/561)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))


### Documentation

* newcomer onboarding rewrite ([#525](https://github.com/msch128/mnema-talk/issues/525)) ([519ec91](https://github.com/msch128/mnema-talk/commit/519ec9168ef04ec04c33f4d7f580107449f1cd73))
* **operations:** check codes for the connection test ([#533](https://github.com/msch128/mnema-talk/issues/533)) ([2a5e969](https://github.com/msch128/mnema-talk/commit/2a5e969f241498f4754871e8283b2aaf9c491f7a))

## [2.1.1](https://github.com/msch128/mnema-talk/compare/v2.1.0...v2.1.1) (2026-10-29)


### Documentation

* README facts for 2.1.0 (composer, mic notice, invite edit, role mapping) ([#519](https://github.com/msch128/mnema-talk/issues/519)) ([7c85b9e](https://github.com/msch128/mnema-talk/commit/7c85b9eb9f65a079c72aa8bd512fd7a0f0eda3b3))

## [2.1.0](https://github.com/msch128/mnema-talk/compare/v2.0.0...v2.1.0) (2026-10-29)


### Features

* **admin:** show the status checks in the display language ([#512](https://github.com/msch128/mnema-talk/issues/512)) ([b80419a](https://github.com/msch128/mnema-talk/commit/b80419aa69d665b55e23b80bb4a55e8683597554))
* **import:** connect Discord roles to existing roles ([#515](https://github.com/msch128/mnema-talk/issues/515)) ([2b1ac1e](https://github.com/msch128/mnema-talk/commit/2b1ac1eb18844df70dd00b50b598b7d8afb56a75))
* **invites:** edit the expiry and use limit of an existing invite ([#511](https://github.com/msch128/mnema-talk/issues/511)) ([720ba1c](https://github.com/msch128/mnema-talk/commit/720ba1c68713e47304100d22038d03705000cde0))
* **web:** auto-growing composer, paste and drag-and-drop for files ([#509](https://github.com/msch128/mnema-talk/issues/509)) ([26292e3](https://github.com/msch128/mnema-talk/commit/26292e3b99a1f11a90765931bf891916adb842a8))
* **web:** copy button and selectable text for connect errors and alerts ([#514](https://github.com/msch128/mnema-talk/issues/514)) ([edb4df1](https://github.com/msch128/mnema-talk/commit/edb4df19238a346a8ab83ff16c80ab7cda058123))
* **web:** translate the desktop client's TLS version error ([#513](https://github.com/msch128/mnema-talk/issues/513)) ([66eb95f](https://github.com/msch128/mnema-talk/commit/66eb95fd337794a7dfd5e755b687cc2cd8e71629))


### Bug Fixes

* **voice:** warn about a silent microphone from the raw capture, once, and let it be dismissed ([#510](https://github.com/msch128/mnema-talk/issues/510)) ([d0dde93](https://github.com/msch128/mnema-talk/commit/d0dde93211c7cbee7e8e6138d022f786825f7116))


### Documentation

* note that Synology Web Station needs a literal port ([#517](https://github.com/msch128/mnema-talk/issues/517)) ([436fd62](https://github.com/msch128/mnema-talk/commit/436fd62fe7ec8372f8abd65d18654c3c99942a92))
* one-click updates and the app port for the Synology guide ([#516](https://github.com/msch128/mnema-talk/issues/516)) ([8638dbf](https://github.com/msch128/mnema-talk/commit/8638dbf0fac40c8239420ecd07c6c787b424dc5d))

## [2.0.0](https://github.com/msch128/mnema-talk/compare/v1.5.1...v2.0.0) (2026-10-29)


### Features

* **ops:** setup checks and assistant know the built-in TURN relay ([#494](https://github.com/msch128/mnema-talk/issues/494)) ([8c2d7be](https://github.com/msch128/mnema-talk/commit/8c2d7bef1c867bdb00e7f4bcec96150b596fe7b9))
* **voice:** built-in TURN relay for members behind UDP-blocking firewalls ([#490](https://github.com/msch128/mnema-talk/issues/490)) ([5597aab](https://github.com/msch128/mnema-talk/commit/5597aab6e9a8f6ea1cd4567f9135eccf2a833fcf))


### Documentation

* one reference for network ports and outbound connections ([#506](https://github.com/msch128/mnema-talk/issues/506)) ([49e52cf](https://github.com/msch128/mnema-talk/commit/49e52cf3436de48b13a691d45de77d2d1495f116))

## [1.5.1](https://github.com/msch128/mnema-talk/compare/v1.5.0...v1.5.1) (2026-10-29)


### Bug Fixes

* **auth,setup:** two-character names sign in, setup writes what the server seeds ([#495](https://github.com/msch128/mnema-talk/issues/495)) ([f826152](https://github.com/msch128/mnema-talk/commit/f82615244036f8058d52992e942fcdabc0293896))
* **server:** ban expiry audit, event cancel order, reaction removal, GIF edges, 413 bodies, prune count, voice eviction ([#496](https://github.com/msch128/mnema-talk/issues/496)) ([20a038f](https://github.com/msch128/mnema-talk/commit/20a038f1e576589c7057fd2f0beed0e2eec25564))
* **setup,voice,web:** re-ask an invalid Owner name, restore the AI filter after a late audio start, watch only after a joined Talk ([#501](https://github.com/msch128/mnema-talk/issues/501)) ([a9bab7f](https://github.com/msch128/mnema-talk/commit/a9bab7f9076c2188618da461d388ad9cda7701c2))
* **sfu:** ask again for the keyframe of a layer that sends nothing ([#500](https://github.com/msch128/mnema-talk/issues/500)) ([06f2e3e](https://github.com/msch128/mnema-talk/commit/06f2e3e5109759226ab7a28c13760b5eceed381e))
* **voice:** never send silence when the audio engine or the AI filter does not run ([#493](https://github.com/msch128/mnema-talk/issues/493)) ([0555400](https://github.com/msch128/mnema-talk/commit/0555400cbcbf949469666b9c1bd08f55f51218a2))
* **web:** keep live messages through reconnect refreshes, media line swaps and unwritable draft storage ([#498](https://github.com/msch128/mnema-talk/issues/498)) ([71039bc](https://github.com/msch128/mnema-talk/commit/71039bcf298029e9a969a05873a4a3b1036b84aa))
* **web:** watch a screen after the join settles, keep the UDP mux port count, bound post lookup, relax unknown 2FA code ([#497](https://github.com/msch128/mnema-talk/issues/497)) ([2935112](https://github.com/msch128/mnema-talk/commit/293511289036d6f505df8bdcb7aa69c8d400c3cc))
* **web:** WebKit calls stay audible after a reload, call specs no longer starve a macOS runner ([#478](https://github.com/msch128/mnema-talk/issues/478)) ([d7d89a8](https://github.com/msch128/mnema-talk/commit/d7d89a87896551ab9f42974f5057434cf235d2d3))


### Code Refactoring

* **sfu:** remove the native media authorization hooks ([#483](https://github.com/msch128/mnema-talk/issues/483)) ([201af9c](https://github.com/msch128/mnema-talk/commit/201af9ccf77880f3a538688f21657a44ea427835))

## [1.5.0](https://github.com/msch128/mnema-talk/compare/v1.4.0...v1.5.0) (2026-10-29)


### Features

* **auth:** Discord-style username rules for new accounts ([#489](https://github.com/msch128/mnema-talk/issues/489)) ([25fdbaf](https://github.com/msch128/mnema-talk/commit/25fdbaf47e932c6bfb8c23c160348c7b93ffc0b7))
* call the server by its community name everywhere ([#488](https://github.com/msch128/mnema-talk/issues/488)) ([85c026c](https://github.com/msch128/mnema-talk/commit/85c026c089c000eb5aaca9805073457c4eca21b5))
* **linkpreview:** rich link previews with click-to-play video and share tags ([#484](https://github.com/msch128/mnema-talk/issues/484)) ([24d3460](https://github.com/msch128/mnema-talk/commit/24d346025f97cee7764137ec27ed8dfc9c49d807))
* **ops:** setup assistant, setup checks and automatic voice address ([#487](https://github.com/msch128/mnema-talk/issues/487)) ([251ef62](https://github.com/msch128/mnema-talk/commit/251ef624dcc9f4f43ce50a7fb6cca6787a4473f9))
* **web:** show the desktop clients' connect errors in the interface language ([#486](https://github.com/msch128/mnema-talk/issues/486)) ([833839d](https://github.com/msch128/mnema-talk/commit/833839dceb052d304e94e0ba5b9e9b98f174a1cd))


### Bug Fixes

* **media:** keep one bad key package from blocking every add commit ([#491](https://github.com/msch128/mnema-talk/issues/491)) ([5bba62e](https://github.com/msch128/mnema-talk/commit/5bba62eb86ae34cd442ea8c649f3c9cb65a55e39))
* **voice:** say when no media connection comes up and keep unreachable IPv6 out of the offer ([#482](https://github.com/msch128/mnema-talk/issues/482)) ([234443a](https://github.com/msch128/mnema-talk/commit/234443af4c40f587b1cde6d75d850948b03e13f7))
* web client review findings ([#480](https://github.com/msch128/mnema-talk/issues/480)) ([f3d1a01](https://github.com/msch128/mnema-talk/commit/f3d1a0115d9e051556fca72468b913bb07406dd9))

## [1.4.0](https://github.com/msch128/mnema-talk/compare/v1.3.0...v1.4.0) (2026-10-28)


### Features

* **auth:** Owner confirms identity before account takeover routes ([#475](https://github.com/msch128/mnema-talk/issues/475)) ([0a5947c](https://github.com/msch128/mnema-talk/commit/0a5947cc6369c1c1b33d90f68dc8ef09c0447c87))

## [1.3.0](https://github.com/msch128/mnema-talk/compare/v1.2.6...v1.3.0) (2026-10-28)


### Features

* **voice:** disconnect members per Talk through role overwrites ([#472](https://github.com/msch128/mnema-talk/issues/472)) ([f3d9e0b](https://github.com/msch128/mnema-talk/commit/f3d9e0ba72c1377123746166299807865323f4fc))


### Performance Improvements

* **chat:** count unread messages through a covering index ([#471](https://github.com/msch128/mnema-talk/issues/471)) ([e1fb7bc](https://github.com/msch128/mnema-talk/commit/e1fb7bc559c093b58dc8180befe2d6a441fd5fba))


### Code Refactoring

* **perm:** move the test fixture into permtest ([#473](https://github.com/msch128/mnema-talk/issues/473)) ([3c217d0](https://github.com/msch128/mnema-talk/commit/3c217d0658eec7203c9547ae723b4b1e3aba358c))

## [1.2.6](https://github.com/msch128/mnema-talk/compare/v1.2.5...v1.2.6) (2026-10-28)


### Bug Fixes

* code review findings for the server ([#469](https://github.com/msch128/mnema-talk/issues/469)) ([d981362](https://github.com/msch128/mnema-talk/commit/d9813626ddd0ad69968bcb4ebcf8e46bb095e159))

## [1.2.5](https://github.com/msch128/mnema-talk/compare/v1.2.4...v1.2.5) (2026-10-28)


### Bug Fixes

* **web:** keys typed into the emoji picker no longer trigger message shortcuts ([#467](https://github.com/msch128/mnema-talk/issues/467)) ([d0c3e74](https://github.com/msch128/mnema-talk/commit/d0c3e74456d6c44d50a596f92cd82a6cb74763bb))

## [1.2.4](https://github.com/msch128/mnema-talk/compare/v1.2.3...v1.2.4) (2026-10-28)


### Documentation

* the macOS and Linux desktop clients are Beta again ([#464](https://github.com/msch128/mnema-talk/issues/464)) ([7fb48df](https://github.com/msch128/mnema-talk/commit/7fb48df5d8d558b013dcc24557fe446b25192521))

## [1.2.3](https://github.com/msch128/mnema-talk/compare/v1.2.2...v1.2.3) (2026-10-26)


### Miscellaneous Chores

* release 1.2.3 ([#462](https://github.com/msch128/mnema-talk/issues/462)) ([9d94230](https://github.com/msch128/mnema-talk/commit/9d94230ddd03cdbf71a010929de713aa1f03fbe3))

## [1.2.2](https://github.com/msch128/mnema-talk/compare/v1.2.1...v1.2.2) (2026-10-26)


### Bug Fixes

* **web:** navigate to text channel on sidebar click and settle initial route ([#459](https://github.com/msch128/mnema-talk/issues/459)) ([891a923](https://github.com/msch128/mnema-talk/commit/891a9232f71e6d3ced1c824e48037b396aff3c12))

## [1.2.1](https://github.com/msch128/mnema-talk/compare/v1.2.0...v1.2.1) (2026-10-26)


### Documentation

* update desktop availability, 1.2 upgrade notes and admin screenshots ([#457](https://github.com/msch128/mnema-talk/issues/457)) ([3100759](https://github.com/msch128/mnema-talk/commit/3100759f585750457466936adade6fdf01ea0e24))
* update product status and api stability for 1.2.0 ([#456](https://github.com/msch128/mnema-talk/issues/456)) ([fa2d447](https://github.com/msch128/mnema-talk/commit/fa2d4472e4162ce4637d95f59049eb576eb65e4d))

## [1.2.0](https://github.com/msch128/mnema-talk/compare/v1.1.0...v1.2.0) (2026-10-26)


### ⚠ BREAKING CHANGES

* admin report entries created by automod carry no message_id, so message_id is optional in GET /api/admin/reports, GET /api/admin/reports/{id} and POST /api/admin/reports/{id}/resolve.

### Features

* 1.2.0 community, moderation, Talks and a calmer UI ([#448](https://github.com/msch128/mnema-talk/issues/448)) ([c50b688](https://github.com/msch128/mnema-talk/commit/c50b6886e87cc1171cc458ec8e372393f0e45963))

## [1.1.0](https://github.com/msch128/mnema-talk/compare/v1.0.1...v1.1.0) (2026-10-24)


### Features

* 1.1.0 direct messages, blocking and community features ([#401](https://github.com/msch128/mnema-talk/issues/401)) ([cd6cd53](https://github.com/msch128/mnema-talk/commit/cd6cd53d224bb707cc85dd57fd8b4ed1ee842d60))

## [1.0.1](https://github.com/msch128/mnema-talk/compare/v1.0.0...v1.0.1) (2026-10-23)


### Bug Fixes

* 1.0.1 coherence, polish and documentation ([#366](https://github.com/msch128/mnema-talk/issues/366)) ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **account:** the data export includes your polls, pins, push devices and uploaded emoji ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **audit:** every audit action has a readable label and a short summary of what changed ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **auth:** recovery codes keep working while the authenticator step is locked ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **chat:** bot and webhook names keep joiners only inside emoji and scripts that need them; reserved names are compared on a normalized skeleton ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **chat:** mentions addressed to you break a channel mute; [@all](https://github.com/all), [@here](https://github.com/here) and Do Not Disturb stay silent ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **chat:** named rate limits for polls, pins and emoji uploads; unknown or deleted emoji cannot be added as reactions ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* one vocabulary and one voice across the interface copy (glossary in doc/glossary.md) ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **push:** OS notifications and Web Push show plain text; spoilers stay hidden ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **web:** 12-hour times have no leading zero; all mails to a member use the same language ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **web:** custom emoji never appear as raw tokens in the inbox, search, editing or edit history ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **web:** drafts stay private when another tab signs out; drafts of deleted channels are tidied; threads show a draft marker ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **web:** header title, late-loading media scroll, admin console tabs and title, phone layouts of dialogs and pop-overs, connection details on one line ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **web:** server errors of the 1.0 features show their real reason in English and German ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))
* **web:** the admin console lives only in the server menu; the sidebar shows the community name ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))


### Documentation

* README, deployment, upgrade, operations and status pages describe 1.0 accurately; OpenAPI states each operation's real requirement; new README screenshots from one demo community; community files and issue templates ([7a82111](https://github.com/msch128/mnema-talk/commit/7a821114cc9f4aafea3c2b686aa657eed7b7d4f3))

## [1.0.0](https://github.com/msch128/mnema-talk/compare/v0.9.0...v1.0.0) (2026-10-23)


### Features

* 1.0.0 stability, hardening and quality of life ([#320](https://github.com/msch128/mnema-talk/issues/320)) ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **account:** delete my account and export my data ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **api:** API stability contract with an OpenAPI breaking-change check in CI and a server-side WebSocket event registry ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **auth:** optional e-mail with SMTP and password reset by mail; optional TOTP two-factor sign-in with recovery codes ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **chat:** custom emoji per instance (Manage emoji) ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **chat:** Discord-compatible Markdown with code highlighting and confirmed external links ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **chat:** jump to the first unread message; read markers scale to 10,000-member channels ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **chat:** pinned messages per channel ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **chat:** slash-command framework with /poll ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **moderation:** edit history for moderators ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **web:** drafts per channel and thread ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **webhook:** role mentions for webhooks and bots through allowed_mentions; role pills keep their colour under the CSP ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **web:** media is refused in browsers that cannot seal it; Firefox screen share works (VP8) ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **web:** phone layout with drawer navigation and touch targets ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **web:** Web Push notifications and an installable web app ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))


### Bug Fixes

* **audit:** category and layout entries are visible to Owner and Administrators only ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **auth:** new-device sign-in cannot be locked out from one client; password hashing is bounded; weak custom invite codes are refused ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **bot:** send limits key on the canonical channel id; tokens record and re-check their issuer ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **chat:** mention fan-out is capped; invisible and bidi characters are stripped from file and display names ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **ci:** the web bundle is built and signed inside the protected job; the security floor rises only with an explicit trailer; the trust statement expiry is watched ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **linkpreview:** slow hosts are held to their own hop; the address filter matches the webhook SSRF guard ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **media:** daily upload budget per account and optional storage guards ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **sfu:** ICE candidates in answers are vetted like trickled ones and each distinct candidate counts once ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* **ws:** WebSockets end with their session and token; event payloads and rates are capped per type and account ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))


### Performance Improvements

* **perm:** role, overwrite and layout writes are batched; the duplicate trigram index is dropped ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))


### Documentation

* operator readiness for 1.0 (upgrade notes, alert rules file checked in CI, supported versions, fresh-install check) ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))
* the desktop client is Beta ([b5b06dd](https://github.com/msch128/mnema-talk/commit/b5b06ddf6816bfd2fed5a645efb2993e052ef1ac))

## [0.9.0](https://github.com/msch128/mnema-talk/compare/v0.8.16...v0.9.0) (2026-10-22)


### Features

* 0.9.0 notifications, search and onboarding ([#304](https://github.com/msch128/mnema-talk/issues/304)) ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **chat:** [@role](https://github.com/role) mentions and a mentions inbox ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **chat:** notification levels per channel and category, timed mutes, mark all read and category unread badges ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **onboarding:** community rules with acceptance, welcome text, start channel and a public invite landing page ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **search:** date, mention and video filters, thread roots in results and a filter-token search input ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **web:** first-run welcome flow and dismissible tips ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **web:** OS notifications and app badge through the platform adapter ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))


### Bug Fixes

* **auth:** native registration reports rules_pending ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **chat:** bot edits follow the mention policy of bot sends; a multi-word role mention no longer also mentions a user named after its first word ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **chat:** the mentions inbox query uses its indexes ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **web:** the desktop host's real notification permission is reported to the page ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))
* **web:** the welcome flow does not reopen after a failed save, a refused write opens the rules step, a failed mute is rolled back ([7a70c3d](https://github.com/msch128/mnema-talk/commit/7a70c3d18a21e3b001c0085376ca2a9c337eb09f))

## [0.8.16](https://github.com/msch128/mnema-talk/compare/v0.8.15...v0.8.16) (2026-10-21)


### Bug Fixes

* **audit:** audit entries report a JSON encoding failure instead of storing null, and take the target name from the post-change state when none was known ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* **auth:** the unknown-device login throttle reports the longer Retry-After ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* **bot:** bot tokens are written at most once a minute again ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* **gateway:** close reasons stay constant; error details go to the log only ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* migration checksum backfill runs in one batched transaction ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* **sfu:** a failed receive-transceiver add aborts the join with an error ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* **update:** semver components that overflow are rejected ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* **webhook:** the outgoing-webhook test endpoint sends Retry-After when rate limited; secret generation fails closed ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))
* **ws:** typing and voice room lookups re-check the native lease ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))


### Code Refactoring

* quality sweep, functions split by responsibility ([#289](https://github.com/msch128/mnema-talk/issues/289)) ([8e18e11](https://github.com/msch128/mnema-talk/commit/8e18e117b59c50afb4a100feaf48057c4abc434d))

## [0.8.15](https://github.com/msch128/mnema-talk/compare/v0.8.14...v0.8.15) (2026-10-21)


### Bug Fixes

* build with Go 1.27.3 (crypto/x509 GO-2026-6809) ([#290](https://github.com/msch128/mnema-talk/issues/290)) ([492eac1](https://github.com/msch128/mnema-talk/commit/492eac17be8d3a7ac3939e792731ed80316399f6))

## [0.8.14](https://github.com/msch128/mnema-talk/compare/v0.8.13...v0.8.14) (2026-10-20)


### Documentation

* record the Windows WebView2 origin check ([2b61e27](https://github.com/msch128/mnema-talk/commit/2b61e273bb436a45ccddcde1302847cceb6d6a66))

## [0.8.13](https://github.com/msch128/mnema-talk/compare/v0.8.12...v0.8.13) (2026-10-20)


### Bug Fixes

* refuse broad trusted proxy ranges in production ([#283](https://github.com/msch128/mnema-talk/issues/283)) ([7be5c52](https://github.com/msch128/mnema-talk/commit/7be5c522f1a3181e5ce6963905ce30fbab0501b6))

## [0.8.12](https://github.com/msch128/mnema-talk/compare/v0.8.11...v0.8.12) (2026-10-19)


### Bug Fixes

* release refactor and documentation commits ([#281](https://github.com/msch128/mnema-talk/issues/281)) ([202dd37](https://github.com/msch128/mnema-talk/commit/202dd37985f146c6665b7a819f6e9a6b0c8b710f))


### Code Refactoring

* keep native session fields inside the native hub files ([#277](https://github.com/msch128/mnema-talk/issues/277)) ([96dafc0](https://github.com/msch128/mnema-talk/commit/96dafc0b329af8c10429569edf5d683221b29dfe))
* keep the native transport off the browser platform ([#269](https://github.com/msch128/mnema-talk/issues/269)) ([2dd67af](https://github.com/msch128/mnema-talk/commit/2dd67af1eca2ede6f1f9e7804c2fa1b44401e4be))
* split the desktop selector from the native preview ([#271](https://github.com/msch128/mnema-talk/issues/271)) ([1716a34](https://github.com/msch128/mnema-talk/commit/1716a34577248c53cce8efbf9f08b19718b952fe))


### Documentation

* close the two audit cells that are not holes ([#280](https://github.com/msch128/mnema-talk/issues/280)) ([db197d8](https://github.com/msch128/mnema-talk/commit/db197d85d661752ce9c44451355304e84a1054b8))
* mark enforcement cells that are not holes ([#279](https://github.com/msch128/mnema-talk/issues/279)) ([3df6182](https://github.com/msch128/mnema-talk/commit/3df6182beaa66808bb57f797049fe8435a3dba6c))
* record who may send, talk and moderate ([#278](https://github.com/msch128/mnema-talk/issues/278)) ([0ada384](https://github.com/msch128/mnema-talk/commit/0ada384fbca3e8b64b84e4330302dd8c60959c51))

## [0.8.11](https://github.com/msch128/mnema-talk/compare/v0.8.10...v0.8.11) (2026-10-19)


### Features

* seed Moderator and Admin roles with no members ([#267](https://github.com/msch128/mnema-talk/issues/267)) ([15d64c2](https://github.com/msch128/mnema-talk/commit/15d64c25c502cd498baeffb093a688693d698599))

## [0.8.10](https://github.com/msch128/mnema-talk/compare/v0.8.9...v0.8.10) (2026-10-19)


### Documentation

* document web lib subdirectories in AGENTS.md ([#261](https://github.com/msch128/mnema-talk/issues/261)) ([2f35998](https://github.com/msch128/mnema-talk/commit/2f35998251e32a570722f6779f35bbc04ef1db90))

## [0.8.9](https://github.com/msch128/mnema-talk/compare/v0.8.8...v0.8.9) (2026-10-19)


### Code Refactoring

* every web function within the size and complexity limits ([#253](https://github.com/msch128/mnema-talk/issues/253)) ([03151c8](https://github.com/msch128/mnema-talk/commit/03151c8e558da576206c71ace8b9356d15abae60))

## [0.8.8](https://github.com/msch128/mnema-talk/compare/v0.8.7...v0.8.8) (2026-10-19)


### Code Refactoring

* function size and complexity limits for Go and TypeScript ([#247](https://github.com/msch128/mnema-talk/issues/247)) ([8320469](https://github.com/msch128/mnema-talk/commit/832046995e1cc927a575b05c9e71eea32fffe8a1))

## [0.8.7](https://github.com/msch128/mnema-talk/compare/v0.8.6...v0.8.7) (2026-10-19)


### Code Refactoring

* Go files under 400 lines, size and complexity guards ([#236](https://github.com/msch128/mnema-talk/issues/236)) ([fd8a9f3](https://github.com/msch128/mnema-talk/commit/fd8a9f3b8ff4463782a88ad9ea28208a39d4dcc7))

## [0.8.6](https://github.com/msch128/mnema-talk/compare/v0.8.5...v0.8.6) (2026-10-18)


### Code Refactoring

* web client files under 400 lines, one typed call state ([#226](https://github.com/msch128/mnema-talk/issues/226)) ([c2fb710](https://github.com/msch128/mnema-talk/commit/c2fb71031e73b9bb0ef0545db6ff06fe41f80430))

## [0.8.5](https://github.com/msch128/mnema-talk/compare/v0.8.4...v0.8.5) (2026-10-18)


### Code Refactoring

* clean-code pass on the web client ([#218](https://github.com/msch128/mnema-talk/issues/218)) ([be5d30d](https://github.com/msch128/mnema-talk/commit/be5d30d450005e306f2cac1282d4660b46087baa))

## [0.8.4](https://github.com/msch128/mnema-talk/compare/v0.8.3...v0.8.4) (2026-10-18)


### Code Refactoring

* behaviour test suites, one platform adapter, coverage gates at 90/80 ([#209](https://github.com/msch128/mnema-talk/issues/209)) ([c136d1c](https://github.com/msch128/mnema-talk/commit/c136d1c1ef7663b7951d2471b972b3b24178a1cc))

## [0.8.3](https://github.com/msch128/mnema-talk/compare/v0.8.2...v0.8.3) (2026-10-18)


### Features

* **bots:** gateway op 3 presence (online, idle, dnd, invisible) with one activity, kept across RESUME ([0e0b7c6](https://github.com/msch128/mnema-talk/commit/0e0b7c618f66cc8c952be04dcd4d8df9f200339e))
* **bots:** per-bot managed roles and gateway presence statuses ([#202](https://github.com/msch128/mnema-talk/issues/202)) ([0e0b7c6](https://github.com/msch128/mnema-talk/commit/0e0b7c618f66cc8c952be04dcd4d8df9f200339e))


### Bug Fixes

* a new role never lands at or above its creator's highest role ([0e0b7c6](https://github.com/msch128/mnema-talk/commit/0e0b7c618f66cc8c952be04dcd4d8df9f200339e))
* **bots:** reject a banned or disabled bot's token and close its gateway sessions ([0e0b7c6](https://github.com/msch128/mnema-talk/commit/0e0b7c618f66cc8c952be04dcd4d8df9f200339e))

## [0.8.2](https://github.com/msch128/mnema-talk/compare/v0.8.1...v0.8.2) (2026-10-18)


### Features

* append-only audit log for administrative actions ([#187](https://github.com/msch128/mnema-talk/issues/187)) ([9a01547](https://github.com/msch128/mnema-talk/commit/9a01547865ea0293c5c4148f4b3e636dddfa638a))
* audit message deletions and edits, Talk presence and sign-ins ([#188](https://github.com/msch128/mnema-talk/issues/188)) ([1bd8bf8](https://github.com/msch128/mnema-talk/commit/1bd8bf820b791d4d436a8394314f079f08448565))
* bind the media line into the E2EE frame (version 3) ([#184](https://github.com/msch128/mnema-talk/issues/184)) ([270e454](https://github.com/msch128/mnema-talk/commit/270e454cd347d7a7fc06eefebd5603741966e39e))
* **bots:** admin Bots tab, slash commands in the composer, e2e ([#191](https://github.com/msch128/mnema-talk/issues/191)) ([3441dcf](https://github.com/msch128/mnema-talk/commit/3441dcf666108944698c44b1690bf54f8d8cda00))
* **bots:** bot accounts, REST API, gateway and slash commands ([#185](https://github.com/msch128/mnema-talk/issues/185)) ([25fca14](https://github.com/msch128/mnema-talk/commit/25fca14c16659b9a6a4b995cdb565dabcfea4fef))
* **bots:** built-in Bot role, bots in the member list with presence and a shared BOT tag ([#193](https://github.com/msch128/mnema-talk/issues/193)) ([ea6ff9a](https://github.com/msch128/mnema-talk/commit/ea6ff9adf588d86cce04a5f52a93e48886b9b338))
* member timeouts and bans ([#190](https://github.com/msch128/mnema-talk/issues/190)) ([8f9cc85](https://github.com/msch128/mnema-talk/commit/8f9cc85577bab5b6df591b6e433e897847e957d0))
* Owner-editable upload limits, globally and per role ([#183](https://github.com/msch128/mnema-talk/issues/183)) ([d1082cd](https://github.com/msch128/mnema-talk/commit/d1082cd076ed54938035e8a777f2ba5fa39e4ddf))
* slow mode and message moderation ([#192](https://github.com/msch128/mnema-talk/issues/192)) ([2b9adc6](https://github.com/msch128/mnema-talk/commit/2b9adc651ccb780ea990a499b6bcd130c532c069))
* **webbundle:** expiry for trust statements, one-command renewal ([#186](https://github.com/msch128/mnema-talk/issues/186)) ([bfbd830](https://github.com/msch128/mnema-talk/commit/bfbd8306e3b6556d612b1e1bf815865922bb19db))
* **webbundle:** offline root key and signed trust statement for desktop key revocation ([#182](https://github.com/msch128/mnema-talk/issues/182)) ([414fa12](https://github.com/msch128/mnema-talk/commit/414fa12758ea21c4c5195013308a99d167a001c9))


### Bug Fixes

* **webbundle:** trust sign verifies the previous statement, shows the result and asks; max_version caps ([#189](https://github.com/msch128/mnema-talk/issues/189)) ([f034695](https://github.com/msch128/mnema-talk/commit/f034695fc63e38a16d5483f503731543d28f52da))

## [0.8.1](https://github.com/msch128/mnema-talk/compare/v0.8.0...v0.8.1) (2026-10-17)


### Features

* bandwidth-aware Auto layers and live layer labels ([#176](https://github.com/msch128/mnema-talk/issues/176)) ([81ca29a](https://github.com/msch128/mnema-talk/commit/81ca29a06e3f320e39b635879fa5bdf2bda91618))
* clear login lockouts without a restart and show operator warnings in the System tab ([#173](https://github.com/msch128/mnema-talk/issues/173)) ([dd6c18f](https://github.com/msch128/mnema-talk/commit/dd6c18f1460a7d6ad73a67a18cfe4b78fe0d5766))
* outgoing webhook deliveries survive restarts ([#172](https://github.com/msch128/mnema-talk/issues/172)) ([a61f4d6](https://github.com/msch128/mnema-talk/commit/a61f4d6909059b4a974f5dee4729905a2f33cacc))


### Bug Fixes

* give the login unlock migration its own number ([#177](https://github.com/msch128/mnema-talk/issues/177)) ([7cad3ea](https://github.com/msch128/mnema-talk/commit/7cad3eab6f0ece430ab76b93d2913432dd71dd2b))
* **web:** point the updater hint at the deployment doc ([#178](https://github.com/msch128/mnema-talk/issues/178)) ([9c7bf5f](https://github.com/msch128/mnema-talk/commit/9c7bf5f7a2da40809ba1e545191341c68f8c0a4f))

## [0.8.0](https://github.com/msch128/mnema-talk/compare/v0.7.25...v0.8.0) (2026-10-17)


### Features

* Mnema Talk 0.8 ([#170](https://github.com/msch128/mnema-talk/issues/170)) ([71dd384](https://github.com/msch128/mnema-talk/commit/71dd384988c14aca353010bb6df6fabc0abd9a41))

## [0.7.25](https://github.com/msch128/mnema-talk/compare/v0.7.24...v0.7.25) (2026-10-17)


### Bug Fixes

* supply-chain hardening and a security floor that rises with security fixes ([#164](https://github.com/msch128/mnema-talk/issues/164)) ([4c06083](https://github.com/msch128/mnema-talk/commit/4c060836c572912574d369faf06f4d262b6fcd20))

## [0.7.24](https://github.com/msch128/mnema-talk/compare/v0.7.23...v0.7.24) (2026-10-17)


### Features

* **web:** manage webhooks in the channel menu ([#157](https://github.com/msch128/mnema-talk/issues/157)) ([ba4afd3](https://github.com/msch128/mnema-talk/commit/ba4afd3735029fca0280b09c1f93274051ee6cc4))

## [0.7.23](https://github.com/msch128/mnema-talk/compare/v0.7.22...v0.7.23) (2026-10-17)


### Features

* outgoing webhooks, and webhook settings managed by administrators ([#154](https://github.com/msch128/mnema-talk/issues/154)) ([0a46331](https://github.com/msch128/mnema-talk/commit/0a46331f86bd91e647b63e808b84a79c0f2c93ee))

## [0.7.22](https://github.com/msch128/mnema-talk/compare/v0.7.21...v0.7.22) (2026-10-17)


### Bug Fixes

* a regenerated webhook token belongs to whoever receives it ([#155](https://github.com/msch128/mnema-talk/issues/155)) ([7b21229](https://github.com/msch128/mnema-talk/commit/7b21229291f377bbbd1e1ef52629529862638194))

## [0.7.21](https://github.com/msch128/mnema-talk/compare/v0.7.20...v0.7.21) (2026-10-17)


### Features

* incoming webhooks in Discord's execute format ([#150](https://github.com/msch128/mnema-talk/issues/150)) ([43236ef](https://github.com/msch128/mnema-talk/commit/43236efbabd4275639ff188e4f937810ce8aec82))

## [0.7.20](https://github.com/msch128/mnema-talk/compare/v0.7.19...v0.7.20) (2026-10-17)


### Features

* manage roles and permissions in the web app ([#149](https://github.com/msch128/mnema-talk/issues/149)) ([450a0b3](https://github.com/msch128/mnema-talk/commit/450a0b333658251ed531552063091a555953472d))
* roles and permissions with a protected Owner ([#145](https://github.com/msch128/mnema-talk/issues/145)) ([82421ab](https://github.com/msch128/mnema-talk/commit/82421abdf5aa5989499f62bb6e37d1be8fa4f91e))

## [0.7.19](https://github.com/msch128/mnema-talk/compare/v0.7.18...v0.7.19) (2026-10-17)


### Features

* expose operator metrics for HTTP, realtime, SFU, storage and the process ([#142](https://github.com/msch128/mnema-talk/issues/142)) ([cdb898f](https://github.com/msch128/mnema-talk/commit/cdb898fa970d9a6f0ac2ac7eaf6946deaab80dc5))
* show a live operator view with sparklines in the admin System tab ([#144](https://github.com/msch128/mnema-talk/issues/144)) ([863614d](https://github.com/msch128/mnema-talk/commit/863614d706fa5ca24a83f72bbae48552ddad627f))

## [0.7.18](https://github.com/msch128/mnema-talk/compare/v0.7.17...v0.7.18) (2026-10-17)


### Features

* run from a verified bundle in the trusted desktop client ([#139](https://github.com/msch128/mnema-talk/issues/139)) ([7500081](https://github.com/msch128/mnema-talk/commit/75000818fe5f59a4602e5d17ef272014e26a1a5d))
* sign the web app as a bundle for the trusted desktop client ([#138](https://github.com/msch128/mnema-talk/issues/138)) ([6bdf4b6](https://github.com/msch128/mnema-talk/commit/6bdf4b6d45453a63ff2480c57ec98b8e0dbeb732))

## [0.7.17](https://github.com/msch128/mnema-talk/compare/v0.7.16...v0.7.17) (2026-10-17)


### Bug Fixes

* bound keyframe-request accounting and harden screen layer restarts ([#136](https://github.com/msch128/mnema-talk/issues/136)) ([f2dd4c6](https://github.com/msch128/mnema-talk/commit/f2dd4c6b2d81d0c04753a73420c5155c1d01d724))
* hold trickle ICE candidates for their offer or answer, and diagnose failing load-test viewers ([#135](https://github.com/msch128/mnema-talk/issues/135)) ([6d2d9f0](https://github.com/msch128/mnema-talk/commit/6d2d9f01a082a97ec379b1e2e60abf356c89bd37))

## [0.7.16](https://github.com/msch128/mnema-talk/compare/v0.7.15...v0.7.16) (2026-10-17)


### Features

* choose the layer of an automatic viewer from its receiver reports ([#133](https://github.com/msch128/mnema-talk/issues/133)) ([9f286be](https://github.com/msch128/mnema-talk/commit/9f286be32cac992f52726c555b50dcc4fb730ca5))

## [0.7.15](https://github.com/msch128/mnema-talk/compare/v0.7.14...v0.7.15) (2026-10-17)


### Features

* let viewers choose the quality of a screen share ([#131](https://github.com/msch128/mnema-talk/issues/131)) ([d626afa](https://github.com/msch128/mnema-talk/commit/d626afa42d40e7950c5f2a831485f5405829ec10))

## [0.7.14](https://github.com/msch128/mnema-talk/compare/v0.7.13...v0.7.14) (2026-10-17)


### Features

* forward a screen share's layers per viewer in the SFU ([#129](https://github.com/msch128/mnema-talk/issues/129)) ([2c151bf](https://github.com/msch128/mnema-talk/commit/2c151bf936cba7d8bd3d78d1d9489f215fa6ec7e))

## [0.7.13](https://github.com/msch128/mnema-talk/compare/v0.7.12...v0.7.13) (2026-10-17)


### Features

* add reproducible load test (make load-test) ([#120](https://github.com/msch128/mnema-talk/issues/120)) ([731f37a](https://github.com/msch128/mnema-talk/commit/731f37a416c1c0aadf6bff7e7b0af9dfc031419c))


### Bug Fixes

* tighten media key lifecycle after review of 30-minute rotation ([#119](https://github.com/msch128/mnema-talk/issues/119)) ([801e799](https://github.com/msch128/mnema-talk/commit/801e799a82b32aff3e4d924b378cf94cce832ebf))

## [0.7.12](https://github.com/msch128/mnema-talk/compare/v0.7.11...v0.7.12) (2026-10-17)


### Features

* rotate media keys every 30 minutes and drop absent members after 20 ([#117](https://github.com/msch128/mnema-talk/issues/117)) ([26c9408](https://github.com/msch128/mnema-talk/commit/26c94086a74675d787f34b1087cc732105eba6d0))

## [0.7.11](https://github.com/msch128/mnema-talk/compare/v0.7.10...v0.7.11) (2026-10-17)


### Bug Fixes

* record last seen before the database closes on shutdown ([#115](https://github.com/msch128/mnema-talk/issues/115)) ([5fe7199](https://github.com/msch128/mnema-talk/commit/5fe71990716c54110f450198180b67dd3dd6102c))

## [0.7.10](https://github.com/msch128/mnema-talk/compare/v0.7.9...v0.7.10) (2026-10-17)


### Bug Fixes

* keep a Talk's media group when members join or reload ([#113](https://github.com/msch128/mnema-talk/issues/113)) ([cfe64dc](https://github.com/msch128/mnema-talk/commit/cfe64dc2d7da6bb213babe29eb4256ec049f6558))

## [0.7.9](https://github.com/msch128/mnema-talk/compare/v0.7.8...v0.7.9) (2026-10-16)


### Bug Fixes

* stop video freezing when several members join a Talk at once ([#111](https://github.com/msch128/mnema-talk/issues/111)) ([9c90856](https://github.com/msch128/mnema-talk/commit/9c908563c34448ceba85f01f3f6f5e1efe797aed))

## [0.7.8](https://github.com/msch128/mnema-talk/compare/v0.7.7...v0.7.8) (2026-10-16)


### Features

* authenticate media frames per sender and reject replays ([#109](https://github.com/msch128/mnema-talk/issues/109)) ([436ced8](https://github.com/msch128/mnema-talk/commit/436ced80d68930e8c49d37223e21a82cfaffb204))

## [0.7.7](https://github.com/msch128/mnema-talk/compare/v0.7.6...v0.7.7) (2026-10-16)


### Performance Improvements

* shrink the retransmission history as the SFU fills up ([#107](https://github.com/msch128/mnema-talk/issues/107)) ([2c6d9c3](https://github.com/msch128/mnema-talk/commit/2c6d9c3ce49954b9c5d298e4321725be4fd71c53))

## [0.7.6](https://github.com/msch128/mnema-talk/compare/v0.7.5...v0.7.6) (2026-10-16)


### Features

* show a safety code for the media keys of a call ([#103](https://github.com/msch128/mnema-talk/issues/103)) ([86dd884](https://github.com/msch128/mnema-talk/commit/86dd884baf1387c0df0aa401e26c8857e5879bcc))

## [0.7.5](https://github.com/msch128/mnema-talk/compare/v0.7.4...v0.7.5) (2026-10-14)


### Features

* seal microphone, camera and screen before the SFU ([#89](https://github.com/msch128/mnema-talk/issues/89)) ([783f30b](https://github.com/msch128/mnema-talk/commit/783f30b4214b44d4480ba872a2bbccafc6141580))

## [0.7.4](https://github.com/msch128/mnema-talk/compare/v0.7.3...v0.7.4) (2026-10-12)


### Features

* move the native desktop client to mnema-talk-desktop ([a2391df](https://github.com/msch128/mnema-talk/commit/a2391df387496b5e7caf381ebac210f412e69d4c))
* move the native desktop client to mnema-talk-desktop ([2f5e306](https://github.com/msch128/mnema-talk/commit/2f5e3067d7f113c933905cc2ec82713c98202b34))

## [0.7.3](https://github.com/msch128/mnema-talk/compare/v0.7.2...v0.7.3) (2026-10-09)


### Bug Fixes

* configure gaming overlays and expand game detection ([#85](https://github.com/msch128/mnema-talk/issues/85)) ([36e399a](https://github.com/msch128/mnema-talk/commit/36e399a9fc1285aa1e2de6388b802bc4642866fb))

## [0.7.2](https://github.com/msch128/mnema-talk/compare/v0.7.1...v0.7.2) (2026-10-09)


### Bug Fixes

* deliver shared Windows desktop gaming controls ([7638bce](https://github.com/msch128/mnema-talk/commit/7638bce7a42ebcd9b0b44d6525f9110f79f5b7be))

## [0.7.1](https://github.com/msch128/mnema-talk/compare/v0.7.0...v0.7.1) (2026-10-09)


### Bug Fixes

* ship the full web app as the Windows desktop client ([8d7581b](https://github.com/msch128/mnema-talk/commit/8d7581b119d1787399d7856bf84dec6de8f1d310))

## [0.7.0](https://github.com/msch128/mnema-talk/compare/v0.6.1...v0.7.0) (2026-10-08)


### Features

* integrate shared desktop client and automatic DEV builds ([e873878](https://github.com/msch128/mnema-talk/commit/e8738787fbecfc8d5eea62e79c0e0537c5f19e4c))


### Bug Fixes

* update Go security patches before 0.7 release ([6b9ae64](https://github.com/msch128/mnema-talk/commit/6b9ae64a911e60fe049b7fe9189a5e384aca2efb))

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
