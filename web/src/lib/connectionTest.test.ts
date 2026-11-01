import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from './api'
import { gatherCandidates, onlyUrls, ProbeRefused, SpeedRefused, refusalOf, runConnectionTest, selectedPair, tryPath, type ConnectionTestDeps } from './connectionTest'

// A stand-in for RTCPeerConnection that gathers a scripted list of candidates, connects (or not) after the
// answer is set, and reports scripted statistics. It records how each connection was configured.
interface Script {
  candidates: string[]
  connects: boolean
  stats: Array<Record<string, unknown>>
}

const made: Array<{ config: RTCConfiguration; closed: boolean; offered: boolean }> = []

class FakePeer {
  onicecandidate: ((e: { candidate: { type: string } | null }) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  connectionState = 'new'
  localDescription: { sdp: string } | null = null
  record: { config: RTCConfiguration; closed: boolean; offered: boolean }
  constructor(readonly config: RTCConfiguration, readonly script: Script) {
    this.record = { config, closed: false, offered: false }
    made.push(this.record)
  }
  createDataChannel() { return {} }
  async createOffer() { return { type: 'offer', sdp: 'v=0' } }
  async setLocalDescription() {
    this.record.offered = true
    const lines = this.script.candidates.map(type => `a=candidate:1 1 udp 1 1.2.3.4 9 typ ${type}`)
    this.localDescription = { sdp: ['v=0', ...lines].join('\r\n') }
    queueMicrotask(() => {
      for (const type of this.script.candidates) this.onicecandidate?.({ candidate: { type } })
      this.onicecandidate?.({ candidate: null })
    })
  }
  async setRemoteDescription() {
    queueMicrotask(() => {
      this.connectionState = this.script.connects ? 'connected' : 'failed'
      this.onconnectionstatechange?.()
    })
  }
  async getStats() {
    const map = new Map<string, Record<string, unknown>>()
    for (const s of this.script.stats) map.set(String(s.id), s)
    return map
  }
  close() { this.record.closed = true }
}

const goodStats = (localType: string, remoteAddress: string) => [
  { id: 'p', type: 'candidate-pair', state: 'succeeded', nominated: true, localCandidateId: 'l', remoteCandidateId: 'r' },
  { id: 'l', type: 'local-candidate', candidateType: localType },
  { id: 'r', type: 'remote-candidate', candidateType: 'host', address: remoteAddress }
]

const servers: RTCIceServer[] = [
  { urls: ['stun:stun.example:3478'] },
  { urls: ['turn:chat.example:3479?transport=udp', 'turn:chat.example:3479?transport=tcp'], username: 'u', credential: 'c' }
]

function deps(scripts: (config: RTCConfiguration) => Script, probe: ConnectionTestDeps['probe'] = async () => 'v=0 answer'): ConnectionTestDeps {
  return {
    iceServers: async () => servers,
    probe,
    createPeer: config => new FakePeer(config, scripts(config)) as unknown as RTCPeerConnection,
    gatherMs: 50,
    connectMs: 50
  }
}

beforeEach(() => { made.length = 0 })

describe('onlyUrls', () => {
  it('keeps the credentials and drops servers left without a URL', () => {
    expect(onlyUrls(servers, u => u.includes('transport=tcp'))).toEqual([{ urls: ['turn:chat.example:3479?transport=tcp'], username: 'u', credential: 'c' }])
    expect(onlyUrls(servers, () => false)).toEqual([])
  })
})

describe('gatherCandidates', () => {
  it('counts host, srflx and relay candidates and ignores other types', async () => {
    const d = deps(() => ({ candidates: ['host', 'host', 'srflx', 'relay', 'prflx'], connects: false, stats: [] }))
    expect(await gatherCandidates(d, servers)).toEqual({ host: 2, srflx: 1, relay: 1 })
    expect(made[0]?.closed).toBe(true)
  })
})

describe('selectedPair', () => {
  it('reads the nominated, succeeded pair and the remote address', async () => {
    const pc = new FakePeer({}, { candidates: [], connects: true, stats: goodStats('relay', '203.0.113.9') }) as unknown as RTCPeerConnection
    expect(await selectedPair(pc)).toEqual({ localType: 'relay', remoteType: 'host', remoteAddress: '203.0.113.9' })
  })

  it('has no pair without a succeeded one', async () => {
    const pc = new FakePeer({}, { candidates: [], connects: true, stats: [{ id: 'p', type: 'candidate-pair', state: 'waiting' }] }) as unknown as RTCPeerConnection
    expect(await selectedPair(pc)).toBeUndefined()
  })
})

describe('tryPath', () => {
  it('connects with an offer carrying no media, and closes the connection', async () => {
    const probe = vi.fn(async (_sdp: string) => 'answer')
    const d = deps(() => ({ candidates: ['host'], connects: true, stats: goodStats('host', '198.51.100.2') }), probe)
    const result = await tryPath(d, [], 'all')
    expect(result.outcome).toBe('ok')
    expect(probe).toHaveBeenCalledOnce()
    expect(String(probe.mock.calls[0]?.[0])).not.toContain('m=audio')
    expect(made[0]?.closed).toBe(true)
  })

  it('fails when the connection does not come up', async () => {
    const d = deps(() => ({ candidates: ['host'], connects: false, stats: [] }))
    expect((await tryPath(d, [], 'all')).outcome).toBe('fail')
  })

  it('does not bother the server when a relay-only attempt gathered no relay candidate', async () => {
    const probe = vi.fn(async () => 'answer')
    const d = deps(() => ({ candidates: ['host'], connects: true, stats: [] }), probe)
    expect((await tryPath(d, servers, 'relay')).outcome).toBe('fail')
    expect(probe).not.toHaveBeenCalled()
  })

  it('reports a refused probe as such', async () => {
    const d = deps(() => ({ candidates: ['host'], connects: true, stats: [] }), async () => { throw new ApiError(429, 'RATE_LIMITED', 'slow down') })
    await expect(tryPath(d, [], 'all')).rejects.toMatchObject({ kind: 'limited' })
  })
})

describe('refusalOf', () => {
  it('maps the statuses the probe endpoint uses', () => {
    expect(refusalOf(new ApiError(429, 'RATE_LIMITED', '')).kind).toBe('limited')
    expect(refusalOf(new ApiError(503, 'UNAVAILABLE', '')).kind).toBe('voice_off')
    expect(refusalOf(new ApiError(500, 'INTERNAL_ERROR', '')).kind).toBe('failed')
    expect(refusalOf(new Error('offline'))).toBeInstanceOf(ProbeRefused)
  })
})

describe('runConnectionTest', () => {
  const scripted = (direct: boolean, relayUdp: boolean, relayTcp: boolean) => (config: RTCConfiguration): Script => {
    const urls = (config.iceServers ?? []).flatMap(s => [s.urls].flat())
    const relayOnly = config.iceTransportPolicy === 'relay'
    const tcp = urls.some(u => u.includes('transport=tcp'))
    const connects = !relayOnly ? direct : tcp ? relayTcp : relayUdp
    return { candidates: relayOnly ? ['relay'] : ['host', 'srflx', 'relay'], connects, stats: goodStats(relayOnly ? 'relay' : 'srflx', '203.0.113.9') }
  }

  it('tests the direct route and the relay over UDP and over TCP, each separately', async () => {
    const steps: string[] = []
    const report = await runConnectionTest(deps(scripted(true, true, true)), s => steps.push(s))
    expect(steps).toEqual(['candidates', 'direct', 'relayUdp', 'relayTcp'])
    expect(report.direct.outcome).toBe('ok')
    expect(report.relayUdp.outcome).toBe('ok')
    expect(report.relayTcp.outcome).toBe('ok')
    expect(report.candidates).toEqual({ host: 1, srflx: 1, relay: 1 })
    expect(report.stunOffered && report.turnOffered).toBe(true)

    const relayConnections = made.filter(m => m.config.iceTransportPolicy === 'relay')
    expect(relayConnections).toHaveLength(2)
    const udp = relayConnections.find(m => JSON.stringify(m.config).includes('transport=udp'))
    const tcp = relayConnections.find(m => JSON.stringify(m.config).includes('transport=tcp'))
    expect(JSON.stringify(udp?.config)).not.toContain('transport=tcp')
    expect(JSON.stringify(tcp?.config)).not.toContain('transport=udp')
    // The direct attempt never carries TURN: success must mean the voice port itself answered.
    const direct = made.find(m => m.config.iceTransportPolicy === 'all' && m.offered && JSON.stringify(m.config).includes('stun:') && !JSON.stringify(m.config).includes('turn:'))
    expect(direct).toBeDefined()
  })

  it('finds the relay-only case', async () => {
    const report = await runConnectionTest(deps(scripted(false, true, true)))
    expect([report.direct.outcome, report.relayUdp.outcome, report.relayTcp.outcome]).toEqual(['fail', 'ok', 'ok'])
  })

  it('skips the relay paths when the server offers no TURN', async () => {
    const d = { ...deps(scripted(true, false, false)), iceServers: async () => [servers[0] as RTCIceServer] }
    const report = await runConnectionTest(d)
    expect(report.turnOffered).toBe(false)
    expect(report.relayUdp.outcome).toBe('unavailable')
    expect(report.relayTcp.outcome).toBe('unavailable')
  })

  it('stops at a refusal and says which', async () => {
    const limited = deps(scripted(true, true, true), async () => { throw new ApiError(429, 'RATE_LIMITED', '') })
    expect((await runConnectionTest(limited)).limited).toBe(true)
    const off = deps(scripted(true, true, true), async () => { throw new ApiError(503, 'UNAVAILABLE', '') })
    const report = await runConnectionTest(off)
    expect(report.voiceOff).toBe(true)
    expect(report.relayUdp.outcome).toBe('unavailable')
  })

  it('counts a probe error that is neither as a failed path and goes on', async () => {
    let calls = 0
    const d = deps(scripted(true, true, true), async () => { calls++; if (calls === 1) throw new Error('network'); return 'answer' })
    const report = await runConnectionTest(d)
    expect(report.direct.outcome).toBe('fail')
    expect(report.relayUdp.outcome).toBe('ok')
  })
})

describe('the download speed test', () => {
  const scripted = () => ({ candidates: ['host'], connects: true, stats: goodStats('srflx', '203.0.113.9') })
  const withDownload = (download: NonNullable<ConnectionTestDeps['download']>, extra: Partial<ConnectionTestDeps> = {}) => ({ ...deps(scripted), download, ...extra })
  const speed = async (d: ConnectionTestDeps) => (await runConnectionTest(d)).speed

  it('measures Mbit/s from the bytes and the time, after the paths, and announces the step', async () => {
    const steps: string[] = []
    const report = await runConnectionTest(withDownload(async () => ({ bytes: 5_000_000, ms: 1000 })), s => steps.push(s))
    expect(steps.at(-1)).toBe('speed')
    expect(report.speed).toEqual({ outcome: 'ok', mbps: 40 })
  })

  it('does not measure from the same network, by the direct path or by the page address', async () => {
    const download = vi.fn(async () => ({ bytes: 5_000_000, ms: 1000 }))
    const local = { ...deps(() => ({ candidates: ['host'], connects: true, stats: goodStats('host', '192.168.1.5') })), download }
    expect(await speed(local)).toEqual({ outcome: 'lan' })
    expect(await speed(withDownload(download, { pageHost: '192.168.1.5' }))).toEqual({ outcome: 'lan' })
    expect(await speed(withDownload(download, { pageHost: 'chat.example.com' }))).toMatchObject({ outcome: 'ok' })
    expect(download).toHaveBeenCalledTimes(1)
  })

  it('takes a speed no home uplink has as the LAN (split-horizon DNS), not as a measurement', async () => {
    expect(await speed(withDownload(async () => ({ bytes: 16_000_000, ms: 100 })))).toEqual({ outcome: 'lan' })
  })

  it('needs a stream long or big enough to mean something', async () => {
    const fail = { outcome: 'fail' }
    expect(await speed(withDownload(async () => ({ bytes: 1000, ms: 5 })))).toEqual(fail)
    expect(await speed(withDownload(async () => ({ bytes: 5_000_000, ms: 0 })))).toEqual(fail)
    expect(await speed(withDownload(async () => ({ bytes: 500_000, ms: 100 })))).toEqual(fail) // too short and too small
    expect(await speed(withDownload(async () => ({ bytes: 300_000, ms: 400 })))).toMatchObject({ outcome: 'ok' }) // slow but long enough
    expect(await speed(withDownload(async () => ({ bytes: 2_500_000, ms: 100 })))).toMatchObject({ outcome: 'ok' }) // fast but big enough
  })

  it('tells a refusal from a failure', async () => {
    expect(await speed(withDownload(async () => { throw new SpeedRefused('limited') }))).toEqual({ outcome: 'limited' })
    expect(await speed(withDownload(async () => { throw new SpeedRefused('busy') }))).toEqual({ outcome: 'busy' })
    expect(await speed(withDownload(async () => { throw new Error('network') }))).toEqual({ outcome: 'fail' })
    const limited = withDownload(async () => ({ bytes: 5_000_000, ms: 1000 }))
    limited.probe = async () => { throw new ApiError(429, 'RATE_LIMITED', '') }
    expect(await speed(limited)).toEqual({ outcome: 'fail' })
  })

  it('hands the abort signal to the download', async () => {
    const seen: Array<AbortSignal | undefined> = []
    const signal = new AbortController().signal
    await runConnectionTest(withDownload(async s => { seen.push(s); return { bytes: 5_000_000, ms: 1000 } }), () => {}, signal)
    expect(seen).toEqual([signal])
  })

  it('leaves the speed out when the deps have no download', async () => {
    expect(await speed(deps(scripted))).toBeUndefined()
  })
})
