# Mnema Talk 0.5 / 0.6 execution roadmap

Updated 2026-10-07. This is an implementation and acceptance plan, not a capacity claim or security certification. The deployed baseline is 0.4.3. Version and changelog files remain release-please managed.

## Product contract

One self-hosted community. Persistent channels and immediate voice-room joins. New protected chat, reactions, attachments, microphone, camera, screen and screen audio must be end-to-end encrypted. No automatic plaintext fallback. Mobile qualification begins in 0.6. Desktop, global hotkeys and overlays remain later work.

100 endpoints in a channel is the normal large-room target; 250 is an additional large-room target. A person and a connected device are counted separately. Interactive media uses direct SFU forwarding. An independently self-hosted media host is an optional deployment profile; viewer relay chains are not the default.

The embedded browser client trusts its code supplier. Protecting against a hostile backend requires independently controlled client delivery. Server administrator access must not automatically confer content access.

## Current evidence

0.4.3 implements per-peer signaling outside room locks, optional shared UDP transport, initial WebSocket catch-up, upgraded-socket shutdown, and viewport-bounded participant menus. Unit/integration/race checks and the complete small browser suite passed. This does not qualify 100/250 endpoints.

An isolated three-browser OpenMLS prototype demonstrated membership, replay/tampering rejection and removal rotation. It has no production device authorization, durable provider, recovery or independent client delivery. A tested SFrame candidate failed a normative vector through its high-level API. A separate experimental low-level integration matched the RFC encryption/decryption vector and demonstrated progressing video, audible synthetic audio and media-key rotation after removal. That proof does not approve the defective high-level API, the integration or its provider for production.

Current work addresses audio-render-thread initialization, redundant screen encoder configuration, stale connection callbacks, duplicate audio sinks and superseded SFU sources, explicit local microphone testing, consistent backups, and an opaque atomic cryptographic-state/outbox adapter. Advanced stream diagnostics distinguish the requested frame rate, capture settings, measured capture/output and browser-reported encoder limitations; a selected 60 fps is not a guarantee. Implementation, tests, measurements and release status must be recorded separately. An adapter alone is not an E2EE feature.

The cold-filter experiment measured a 243–245 ms gap when model initialization ran on the same audio rendering thread. The Worker path initialized in approximately 300 ms, processed 100 PCM frames and showed no sampled gap over 30 ms in that Chromium run. An independent AudioContext did not reproduce the reported Windows/Opera YouTube interruption on either path. These are local diagnostic results, not a browser support or latency qualification.

## 0.5: complete secure operation

| Order | Work | Acceptance evidence |
|---|---|---|
| 1 | Startup/capture lifecycle | Independent playback remains responsive during cold filter startup; no stale callbacks or unintended capture; advancing screen frames and declared supported browsers. |
| 2 | Threat model and providers | Bounded browser bindings; normative vectors; complete audio/video/chat/file prototype; explicit audit/provider boundaries. |
| 3 | Device enrollment and group policy | Known-device approval or independent recovery; visible identity changes; backend cannot invent a trusted member. |
| 4 | Durable state and event delivery | State/ciphertext committed atomically before send; retries reuse accepted ciphertext; uncertain or restored state requires a fresh authorized sending context. Local revision counters cannot detect arbitrary snapshot rollback. |
| 5 | All protected content | Ciphertext-only payloads and attachments; every media source protected before first frame; separate share/viewer membership with future-key revocation. |
| 6 | History, recovery and legacy migration | Login reset grants no content keys; explicit archive/history policy; idempotent legacy import; incompatible clients fail closed. |
| 7 | Client delivery and operation | Independently controlled static client/relay; consistent backup; full separate application/object restore; installation and network preflight. |
| 8 | Release qualification | Independent expert review, negative security tests, 16-endpoint eight-hour mixed run, upgrade/restore and independent operator pilot. |

A full application restore includes login, history, avatars, object retrieval and authorized client decryption. Importing SQL and checking archive hashes is only structural verification.

## 0.6: measured scale and mobile

1. Bounded per-receiver packet and feedback queues, pacing, admission budgets and transport cancellation. One slow receiver must not stall other receivers.
2. E2EE-compatible simulcast/source metadata, per-receiver bandwidth and decoder budgets, stable quality selection and keyframe/key-epoch coordination.
3. Audio stability, disclosed active-speaker policies and fair resource limits; microphone count is not a promise to decode every simultaneous speaker.
4. Distributed CHAT, VOICE, WATCH and MIXED profiles at 100 and 250 endpoints with explicit source/subscription matrices, actual decrypting browser decoders, loss/TURN/weak-client cases and churn.
5. Focused screen plus budgeted camera views, searchable/virtualized membership, accurate source/receiver quality, local diagnostics and protected operational metrics.
6. Mobile panels and controls, accessibility and independently observed installation/update/recovery pilots.
7. Soak tests, upgrade/backward compatibility and integrated independent security review before public capacity promises.

## Bandwidth and latency

UDP multiplexing reduces port use, not outgoing media traffic. A direct forwarder serving average bitrate `b` to `w` viewers sends approximately `b * w` of payload, plus transport/retransmission overhead. Lower receiver bitrates, fewer received views and a separately connected media host can meet a limited origin uplink; they do not eliminate total traffic.

Latency is measured capture-to-playback with synthetic markers and calibrated clocks. WebSocket ping, incoming bytes and an unpaused video element are insufficient. Published profiles must state hardware, network path, source quality, device count, subscriptions, E2EE state, freezes, audio gaps and latency distributions. No user-count or low-latency promise follows from a small local browser test.
