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
  createPeer: config => new RTCPeerConnection(config)
}
