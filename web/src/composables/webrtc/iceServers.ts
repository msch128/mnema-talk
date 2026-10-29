import { decodeServerICEConfig } from '../../types/rest'

// ICE servers come from the server (WEBRTC_STUN_URLS); empty by default so no
// third-party STUN server learns the user's IP
let servers: RTCIceServer[] = []
let loaded: Promise<void> | null = null
let loadedAt = 0

// The TURN credential in the config is time-limited, so a tab that stays open
// for days must not keep using the one it got on its first join. A join reuses
// the config for this long and fetches a fresh one after that.
const MAX_AGE_MS = 60 * 60 * 1000

export function iceServers() {
  return servers
}

/** True when the server offers a TURN relay, which carries media where UDP to the server is blocked. */
export function relayOffered() {
  return servers.some(server => [server.urls].flat().some(url => /^turns?:/i.test(url)))
}

// Loaded on a voice join rather than at import time, and again when the last
// one is older than MAX_AGE_MS.
export function loadIceServers() {
  if (loaded && Date.now() - loadedAt >= MAX_AGE_MS) loaded = null
  if (!loaded) {
    loaded = fetch('/api/webrtc/config', { credentials: 'same-origin' })
      .then(async (res): Promise<unknown> => res.ok ? res.json() : null)
      .then(data => {
        if (data === null) return
        servers = decodeServerICEConfig(data).ice_servers
        loadedAt = Date.now()
      })
      .catch(() => { loaded = null })
  }
  return loaded
}
