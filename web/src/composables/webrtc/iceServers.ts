import { decodeServerICEConfig } from '../../types/rest'

// ICE servers come from the server (WEBRTC_STUN_URLS); empty by default so no
// third-party STUN server learns the user's IP
let servers: RTCIceServer[] = []
let loaded: Promise<void> | null = null
// Set once the fetch settles: a request in flight is never replaced.
let loadedAt: number | null = null

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
  if (loaded && loadedAt !== null && Date.now() - loadedAt >= MAX_AGE_MS) loaded = null
  if (!loaded) {
    loadedAt = null
    const request: Promise<void> = fetch('/api/webrtc/config', { credentials: 'same-origin' })
      .then(async (res): Promise<unknown> => res.ok ? res.json() : null)
      .then(data => {
        if (data === null) {
          if (loaded === request) loaded = null
          return
        }
        servers = decodeServerICEConfig(data).ice_servers
        loadedAt = Date.now()
      })
      .catch(() => { if (loaded === request) loaded = null })
    loaded = request
  }
  return loaded
}
