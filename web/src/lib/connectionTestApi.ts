// The browser side of "Test my connection": where its ICE servers come from and how the offer reaches the server.
import { api } from './api'
import type { ConnectionTestDeps } from './connectionTest'
import { iceServers, loadIceServers } from '../composables/webrtc/iceServers'
import { decodeServerConnectionProbeAnswer } from '../types/rest'

export const browserTestDeps: ConnectionTestDeps = {
  iceServers: async () => {
    await loadIceServers()
    return iceServers()
  },
  probe: async sdp => (await api('/api/webrtc/probe', { method: 'POST', json: { sdp }, decode: decodeServerConnectionProbeAnswer })).sdp,
  createPeer: config => new RTCPeerConnection(config),
  download: async () => {
    const res = await fetch('/api/webrtc/speedtest', { credentials: 'same-origin', cache: 'no-store' })
    if (!res.ok || !res.body) throw new Error(`speed test: HTTP ${res.status}`)
    const reader = res.body.getReader()
    // Timed from the first bytes, so the request's round trip does not count.
    let bytes = 0
    let start = 0
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      if (start === 0) start = performance.now()
      else bytes += value.byteLength
    }
    return { bytes, ms: performance.now() - start }
  }
}
