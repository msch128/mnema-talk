import { describe, it, expect } from 'vitest'
import { summarizeStats, rateJitter } from './rtcStats'
import { micStatus } from './rtcFlowRows'
import { setLocale, t } from '../i18n'

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
    expect(s.sample).toMatchObject({ timestamp: 500, bytesSent: 50, bytesReceived: 80 })
    expect(s.rttMs).toBeNull()
    expect(s.localCandidate).toBeNull()
    expect(s.remoteCandidate).toBeNull()
    expect(s.packetsReceived).toBe(0)
    expect(summarizeStats([{ id: 'transport', type: 'transport', selectedCandidatePairId: 'missing' }]).connected).toBe(false)
  })
  it('uses outbound RTP counters when candidate stats are absent and never reports a negative bitrate', () => {
    const s = summarizeStats([{ id: 'out', type: 'outbound-rtp', kind: 'audio', timestamp: 2000, bytesSent: 10 }], { timestamp: 1000, bytesSent: 100, bytesReceived: 100 })
    expect(s.sample).toMatchObject({ timestamp: 2000, bytesSent: 10, bytesReceived: 0 })
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

it('ignores audio media-source records that carry no RTP counters', () => {
  const summary = summarizeStats([{ id: 'source', type: 'media-source', kind: 'audio', timestamp: 1000 }])
  expect(summary.connected).toBe(false)
  expect(summary.packetsReceived).toBe(0)
  expect(summary.packetsSent).toBe(0)
  expect(summary.codec).toBeNull()
})

describe('received video', () => {
  const video = (id: string, frameWidth: number | undefined, frameHeight: number | undefined, framesPerSecond?: number) => ({
    id, type: 'inbound-rtp', kind: 'video', ...(frameWidth === undefined ? {} : { frameWidth }), ...(frameHeight === undefined ? {} : { frameHeight }),
    ...(framesPerSecond === undefined ? {} : { framesPerSecond })
  })

  it('lists the pictures that arrive, largest first, with their frame rate', () => {
    const s = summarizeStats([
      video('cam', 640, 360, 29.6),
      video('screen', 1920, 1080, 30),
      video('still', 1280, 720),
      { id: 'out', type: 'outbound-rtp', kind: 'video', frameWidth: 3840, frameHeight: 2160 },
      { id: 'a', type: 'inbound-rtp', kind: 'audio', frameWidth: 99, frameHeight: 99 },
    ])
    expect(s.videoIn).toEqual([{ width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 0 }, { width: 640, height: 360, fps: 30 }])
  })

  it('leaves out streams that have not shown a picture yet', () => {
    expect(summarizeStats([video('a', undefined, undefined), video('b', 640, undefined), video('c', undefined, 360)]).videoIn).toEqual([])
    expect(summarizeStats([]).videoIn).toEqual([])
  })
})

describe('a quiet microphone with discontinuous transmission', () => {
  // Opus DTX stops the packets while nobody speaks (about five small ones a second): the
  // counters stand still or creep, which is not a broken microphone.
  const outbound = (packetsSent: number, bytesSent: number, timestamp: number) => [
    { id: 'cp', type: 'candidate-pair', state: 'succeeded', nominated: true, currentRoundTripTime: 0.02, localCandidateId: 'l', remoteCandidateId: 'r', bytesSent, bytesReceived: 0, timestamp },
    { id: 'l', type: 'local-candidate', candidateType: 'host', protocol: 'udp' },
    { id: 'r', type: 'remote-candidate', candidateType: 'host', protocol: 'udp' },
    { id: 'out', type: 'outbound-rtp', kind: 'audio', packetsSent, bytesSent, codecId: 'c' },
    { id: 'c', type: 'codec', mimeType: 'audio/opus', clockRate: 48000, channels: 2 }
  ]

  it('shows no bitrate and no loss while no packets go out, and the line is still the sending one', () => {
    const talking = summarizeStats(outbound(500, 40_000, 0), null, { micState: 'sending' })
    const quiet = summarizeStats(outbound(500, 40_000, 5000), talking.sample, { micState: 'sending' })
    expect(quiet.sendKbps).toBe(0)
    expect(quiet.packetsSent).toBe(500)
    expect(quiet.packetsLost).toBe(0)
    expect(quiet.lossPercent).toBe(0)
    expect(quiet.micState).toBe('sending')
  })

  it('says the microphone is sending, from the track, not from the packet counters', () => {
    setLocale('en')
    expect(micStatus('sending', false, 'activity')).toBe(t('stats.mic.sending'))
    expect(micStatus('sending', false, 'activity')).not.toBe(t('stats.mic.pausedSilent'))
    expect(micStatus('sending', false, 'ptt')).toBe(t('stats.mic.sending'))
  })
})
