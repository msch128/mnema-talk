import { describe, it, expect } from 'vitest'
import { streamDiag } from './useWebRTC'

describe('streamDiag', () => {
  it('summarizes sent and received video with bitrate and limits', () => {
    const prev = new Map()
    const stats = (sent, recv) => [
      { type: 'codec', id: 'c1', mimeType: 'video/VP8' },
      { type: 'outbound-rtp', id: 'o', kind: 'video', codecId: 'c1', bytesSent: sent, frameWidth: 1920, frameHeight: 1080, framesPerSecond: 29.6, qualityLimitationReason: 'cpu', encoderImplementation: 'libvpx', powerEfficientEncoder: false },
      { type: 'inbound-rtp', id: 'i', kind: 'video', codecId: 'c1', bytesReceived: recv, framesPerSecond: 24, framesDropped: 3, freezeCount: 1, packetsLost: 5, powerEfficientDecoder: true },
      { type: 'outbound-rtp', id: 'a', kind: 'audio', bytesSent: 999 }
    ]
    expect(streamDiag(stats(1000, 500), prev)[0].kbps).toBeNull()
    const [out, inn] = streamDiag(stats(1_251_000, 625_500), prev, 10)
    expect(out).toMatchObject({ dir: 'out', codec: 'video/VP8', size: '1920x1080', fps: 30, kbps: 1000, limit: 'cpu', encoder: 'libvpx', hw: false })
    expect(inn).toMatchObject({ dir: 'in', fps: 24, kbps: 500, dropped: 3, freezes: 1, lost: 5, hw: true })
  })
})
