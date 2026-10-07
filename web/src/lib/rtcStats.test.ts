import { describe, it, expect } from 'vitest'
import { summarizeStats, rateJitter } from './rtcStats'

function report({ bytesSent = 10000, bytesReceived = 20000, timestamp = 1000 } = {}) {
  return [
    { id: 'cp', type: 'candidate-pair', state: 'succeeded', nominated: true, currentRoundTripTime: 0.023, localCandidateId: 'l', remoteCandidateId: 'r', bytesSent, bytesReceived, timestamp },
    { id: 'l', type: 'local-candidate', candidateType: 'host', protocol: 'udp' },
    { id: 'r', type: 'remote-candidate', candidateType: 'host', protocol: 'udp' },
    { id: 'in1', type: 'inbound-rtp', kind: 'audio', packetsReceived: 900, packetsLost: 10, jitter: 0.004, bytesReceived: 15000, codecId: 'c' },
    { id: 'in2', type: 'inbound-rtp', kind: 'audio', packetsReceived: 90, packetsLost: 0, jitter: 0.012, bytesReceived: 5000 },
    { id: 'out', type: 'outbound-rtp', kind: 'audio', packetsSent: 1200, bytesSent: 10000, codecId: 'c' },
    { id: 'c', type: 'codec', mimeType: 'audio/opus', clockRate: 48000, channels: 2 },
    { id: 't', type: 'transport', srtpCipher: 'AES_CM_128_HMAC_SHA1_80', dtlsCipher: 'TLS_ECDHE' }
  ]
}

describe('summarizeStats', () => {
  it('reports measured values only', () => {
    const s = summarizeStats(report())
    expect(s.connected).toBe(true)
    expect(s.rttMs).toBe(23)
    expect(s.packetsReceived).toBe(990)
    expect(s.packetsLost).toBe(10)
    expect(s.lossPercent).toBe(1)
    expect(s.jitterMs).toBe(12) // worst remote stream
    expect(s.packetsSent).toBe(1200)
    expect(s.codec).toBe('opus 48 kHz, 2 Kanäle')
    expect(s.localCandidate).toBe('host / udp')
    expect(s.srtpCipher).toBe('AES_CM_128_HMAC_SHA1_80')
    expect(s.sendKbps).toBeNull() // needs a previous sample
  })

  it('computes bitrate from two samples', () => {
    const first = summarizeStats(report({ bytesSent: 0, bytesReceived: 0, timestamp: 0 }))
    const second = summarizeStats(report({ bytesSent: 8000, bytesReceived: 16000, timestamp: 2000 }), first.sample)
    expect(second.sendKbps).toBe(32)
    expect(second.recvKbps).toBe(64)
  })

  it('is empty, not invented, without a connection', () => {
    const s = summarizeStats([])
    expect(s.connected).toBe(false)
    expect(s.rttMs).toBeNull()
    expect(s.jitterMs).toBeNull()
    expect(s.codec).toBeNull()
    expect(s.lossPercent).toBe(0)
  })
})

describe('rateJitter', () => {
  it('rates measured jitter', () => {
    expect(rateJitter(null)).toBe('keine Daten')
    expect(rateJitter(5)).toBe('sehr gut')
    expect(rateJitter(80)).toBe('schlecht')
  })
})

describe('browser stats fallbacks and counter changes', () => {
  it('resolves a transport-selected pair when nomination is unavailable', () => {
    const s = summarizeStats([
      { id: 'transport', type: 'transport', selectedCandidatePairId: 'pair' },
      { id: 'pair', type: 'candidate-pair', timestamp: 500, bytesSent: 50, bytesReceived: 80 },
      { id: 'video', type: 'inbound-rtp', kind: 'video', packetsReceived: 900 },
      { id: 'not-ready', type: 'candidate-pair', selected: true, state: 'waiting' }
    ])
    expect(s.connected).toBe(true)
    expect(s.sample).toEqual({ timestamp: 500, bytesSent: 50, bytesReceived: 80 })
    expect(s.rttMs).toBeNull()
    expect(s.localCandidate).toBeNull()
    expect(s.remoteCandidate).toBeNull()
    expect(s.packetsReceived).toBe(0)
    expect(summarizeStats([{ id: 'transport', type: 'transport', selectedCandidatePairId: 'missing' }]).connected).toBe(false)
  })
  it('uses outbound RTP counters when candidate stats are absent and never reports a negative bitrate', () => {
    const s = summarizeStats([{ id: 'out', type: 'outbound-rtp', kind: 'audio', timestamp: 2000, bytesSent: 10 }], { timestamp: 1000, bytesSent: 100, bytesReceived: 100 })
    expect(s.sample).toEqual({ timestamp: 2000, bytesSent: 10, bytesReceived: 0 })
    expect(s.sendKbps).toBe(0)
    expect(s.recvKbps).toBe(0)
    expect(summarizeStats([{ id: 'out', type: 'outbound-rtp', kind: 'audio', timestamp: 1000 }], s.sample).sendKbps).toBeNull()
    expect(summarizeStats([{ id: 'pair', type: 'candidate-pair', selected: true, state: 'succeeded' }]).connected).toBe(true)
  })
  it('aggregates missing audio counters, clamps loss and falls back to the inbound codec', () => {
    const s = summarizeStats([
      { id: 'first', type: 'inbound-rtp', kind: 'audio' },
      { id: 'second', type: 'inbound-rtp', kind: 'audio', packetsReceived: 12, packetsLost: -4, codecId: 'codec' },
      { id: 'third', type: 'inbound-rtp', kind: 'audio' },
      { id: 'codec', type: 'codec' }
    ])
    expect(s.packetsReceived).toBe(12)
    expect(s.packetsLost).toBe(0)
    expect(s.codec).toBe('?')
    expect(s.jitterMs).toBe(0)
    expect(summarizeStats([{ id: 'first', type: 'inbound-rtp', kind: 'audio', bytesReceived: 40, packetsLost: 2 }]).lossPercent).toBe(100)
  })
  it('rates boundary jitter values without smoothing the measurements', () => {
    expect(rateJitter(undefined)).toBe('keine Daten')
    expect(rateJitter(10)).toBe('gut')
    expect(rateJitter(29)).toBe('gut')
    expect(rateJitter(30)).toBe('spürbar')
    expect(rateJitter(60)).toBe('schlecht')
  })
})
