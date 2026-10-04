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
