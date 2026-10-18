import { decodeServerICEConfig } from '../../types/rest'

// ICE servers come from the server (WEBRTC_STUN_URLS); empty by default so no
// third-party STUN server learns the user's IP
let servers: RTCIceServer[] = []
let loaded: Promise<void> | null = null

export function iceServers() {
  return servers
}

// Loaded once, on the first voice join rather than at import time.
export function loadIceServers() {
  if (!loaded) {
    loaded = fetch('/api/webrtc/config', { credentials: 'same-origin' })
      .then(async (res): Promise<unknown> => res.ok ? res.json() : null)
      .then(data => { if (data !== null) servers = decodeServerICEConfig(data).ice_servers })
      .catch(() => { loaded = null })
  }
  return loaded
}
