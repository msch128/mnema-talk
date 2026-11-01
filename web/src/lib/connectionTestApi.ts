// The browser side of "Test my connection": where its ICE servers come from and how the offer reaches the server.
import { api } from './api'
import { SpeedRefused, type ConnectionTestDeps } from './connectionTest'
import { iceServers, loadIceServers } from '../composables/webrtc/iceServers'
import { decodeServerConnectionProbeAnswer } from '../types/rest'

export const browserTestDeps: ConnectionTestDeps = {
  iceServers: async () => {
    await loadIceServers()
    return iceServers()
  },
  probe: async sdp => (await api('/api/webrtc/probe', { method: 'POST', json: { sdp }, decode: decodeServerConnectionProbeAnswer })).sdp,
  createPeer: config => new RTCPeerConnection(config),
  pageHost: globalThis.location?.hostname,
  download: readSpeedStream
}

const SPEED_TIMEOUT_MS = 8000

/** Reads the speed test stream; a stream cut short (timeout, leaving the page, a server that stopped writing) still counts for what arrived. */
async function readSpeedStream(signal?: AbortSignal): Promise<{ bytes: number; ms: number }> {
  const limit = AbortSignal.timeout(SPEED_TIMEOUT_MS)
  const abort = signal ? AbortSignal.any([signal, limit]) : limit
  const res = await fetch('/api/webrtc/speedtest', { credentials: 'same-origin', cache: 'no-store', signal: abort })
  if (res.status === 429) throw new SpeedRefused('limited')
  if (res.status === 503) throw new SpeedRefused('busy')
  if (!res.ok || !res.body) throw new Error(`speed test: HTTP ${res.status}`)
  const reader = res.body.getReader()
  // Timed from the first bytes, so the request's round trip does not count.
  let bytes = 0
  let start = 0
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      if (start === 0) start = performance.now()
      else bytes += value.byteLength
    }
  } catch {
    if (start === 0) throw new Error('speed test: no data')
  }
  return { bytes, ms: performance.now() - start }
}
