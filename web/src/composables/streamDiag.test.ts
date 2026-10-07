import { describe, it, expect } from 'vitest'
import { streamDiag } from './useWebRTC'
import type { MediaStat } from '../lib/mediaStats'

describe('streamDiag', () => {
  it('summarizes sent and received video with bitrate and limits', () => {
    const prev = new Map<string, number>()
    const stats = (sent: number, recv: number): MediaStat[] => [
      { type: 'codec', id: 'c1', mimeType: 'video/VP8' },
      { type: 'outbound-rtp', id: 'o', kind: 'video', codecId: 'c1', bytesSent: sent, frameWidth: 1920, frameHeight: 1080, framesPerSecond: 29.6, qualityLimitationReason: 'cpu', encoderImplementation: 'libvpx', powerEfficientEncoder: false },
      { type: 'inbound-rtp', id: 'i', kind: 'video', codecId: 'c1', bytesReceived: recv, framesPerSecond: 24, framesDropped: 3, freezeCount: 1, packetsLost: 5, powerEfficientDecoder: true },
      { type: 'outbound-rtp', id: 'a', kind: 'audio', bytesSent: 999 }
    ]
    expect(streamDiag(stats(1000, 500), prev)[0]?.kbps).toBeNull()
    const [out, inn] = streamDiag(stats(1_251_000, 625_500), prev, 10)
    expect(out).toMatchObject({ dir: 'out', codec: 'video/VP8', size: '1920x1080', fps: 30, kbps: 1000, limit: 'cpu', encoder: 'libvpx', hw: false })
    expect(inn).toMatchObject({ dir: 'in', fps: 24, kbps: 500, dropped: 3, freezes: 1, lost: 5, hw: true })
  })
})


describe('streamDiag sparse browser statistics', () => {
  it('ignores non-video, unrelated and dormant RTP records without caching them', () => {
    const previous = new Map<string, number>()
    const stats: MediaStat[] = [
      { id: 'audio', type: 'outbound-rtp', kind: 'audio', bytesSent: 10 },
      { id: 'source', type: 'media-source', kind: 'video' },
      { id: 'dormant', type: 'outbound-rtp', kind: 'video', bytesSent: 0 },
      { id: 'missing', type: 'inbound-rtp', kind: 'video' }
    ]
    expect(streamDiag(stats, previous)).toEqual([])
    expect(previous.size).toBe(0)
  })

  it('uses sparse defaults and preserves zero-valued loss feedback', () => {
    const stats: MediaStat[] = [
      { id: 'out', type: 'outbound-rtp', kind: 'video', bytesSent: 2000, packetsLost: 0, nackCount: 0, pliCount: 0 },
      { id: 'in', type: 'inbound-rtp', kind: 'video', bytesReceived: 1000, codecId: 'unknown', decoderImplementation: 'hardware', freezeCount: 0, framesDropped: 0 }
    ]
    const rows = streamDiag(stats)
    expect(rows[0]).toMatchObject({ dir: 'out', codec: '', size: '', fps: 0, kbps: null, limit: '', encoder: '', lost: 0, nacks: 0, plis: 0 })
    expect(rows[1]).toMatchObject({ dir: 'in', codec: '', decoder: 'hardware', dropped: 0, freezes: 0 })
    expect(streamDiag([{ id: 'minimal', type: 'inbound-rtp', kind: 'video', bytesReceived: 10 }])[0]).toMatchObject({ decoder: '', dropped: 0, freezes: 0 })
  })
})
