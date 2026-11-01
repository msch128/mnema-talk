// "Test my connection": finds out, in the browser, whether this device can
// reach the server's voice port directly and through the built-in TURN relay
// (over UDP and over TCP). It needs no microphone: each probe is one data
// channel to a throwaway connection on the server (POST /api/webrtc/probe),
// which is no Talk, shows no presence and closes itself.
import { ApiError } from './api'
import { isLocalAddress, type CandidateCounts, type ConnectionReport, type PathResult, type SpeedResult } from './connectionVerdict'

export interface ConnectionTestDeps {
  /** The ICE servers the server offers this member (STUN, TURN with credentials). */
  iceServers: () => Promise<RTCIceServer[]>
  /** Sends the offer to the server and returns its answer. */
  probe: (offerSdp: string) => Promise<string>
  createPeer: (config: RTCConfiguration) => RTCPeerConnection
  /** Reads the server's speed test stream; absent: no speed test. */
  download?: () => Promise<{ bytes: number; ms: number }>
  /** Milliseconds to wait for candidates and for a connection. */
  gatherMs?: number
  connectMs?: number
}

const GATHER_MS = 4000
const CONNECT_MS = 10000

const urlsOf = (s: RTCIceServer) => [s.urls].flat()
const isTurn = (url: string) => /^turns?:/i.test(url)
const isStun = (url: string) => /^stuns?:/i.test(url)
// A TURN URL carries TCP when it says so or uses TLS; without a transport it is UDP.
const isTcp = (url: string) => /transport=tcp/i.test(url) || /^turns:/i.test(url)

/** The offered servers cut down to the URLs that pass `keep`; servers left without a URL are dropped. */
export function onlyUrls(servers: RTCIceServer[], keep: (url: string) => boolean): RTCIceServer[] {
  return servers.flatMap(s => {
    const urls = urlsOf(s).filter(keep)
    return urls.length ? [{ ...s, urls }] : []
  })
}

function emptyCounts(): CandidateCounts {
  return { host: 0, srflx: 0, relay: 0 }
}

/** Opens a fresh connection with one data channel, offers, and waits until its candidates are gathered. */
async function gatheredOffer(pc: RTCPeerConnection, gatherMs: number, onCandidate?: (c: RTCIceCandidate) => void): Promise<string> {
  const done = new Promise<void>(resolve => {
    const finish = () => resolve()
    const timer = setTimeout(finish, gatherMs)
    pc.onicecandidate = e => {
      if (e.candidate) onCandidate?.(e.candidate)
      else { clearTimeout(timer); finish() }
    }
  })
  pc.createDataChannel('probe')
  await pc.setLocalDescription(await pc.createOffer())
  await done
  return pc.localDescription?.sdp ?? ''
}

/** Counts the candidate types the offered servers let this device gather. */
export async function gatherCandidates(deps: ConnectionTestDeps, servers: RTCIceServer[]): Promise<CandidateCounts> {
  const counts = emptyCounts()
  const pc = deps.createPeer({ iceServers: servers })
  try {
    await gatheredOffer(pc, deps.gatherMs ?? GATHER_MS, c => {
      const type = c.type
      if (type === 'host' || type === 'srflx' || type === 'relay') counts[type]++
    })
  } finally {
    pc.close()
  }
  return counts
}

function waitConnected(pc: RTCPeerConnection, ms: number): Promise<boolean> {
  return new Promise(resolve => {
    const timer = setTimeout(() => resolve(false), ms)
    const check = () => {
      if (pc.connectionState === 'connected') { clearTimeout(timer); resolve(true) }
      else if (pc.connectionState === 'failed' || pc.connectionState === 'closed') { clearTimeout(timer); resolve(false) }
    }
    pc.onconnectionstatechange = check
    check()
  })
}

/** The nominated, succeeded candidate pair of a connection, from its statistics. */
export async function selectedPair(pc: RTCPeerConnection): Promise<PathResult['pair']> {
  const stats = await pc.getStats()
  const byId = new Map<string, Record<string, unknown>>()
  stats.forEach((s: Record<string, unknown>) => byId.set(String(s.id), s))
  let pair: Record<string, unknown> | undefined
  for (const s of byId.values()) {
    if (s.type === 'candidate-pair' && s.state === 'succeeded' && (s.nominated || s.selected)) pair = s
  }
  if (!pair) return undefined
  const local = byId.get(String(pair.localCandidateId))
  const remote = byId.get(String(pair.remoteCandidateId))
  const address = remote?.address ?? remote?.ip
  return {
    localType: String(local?.candidateType ?? ''),
    remoteType: String(remote?.candidateType ?? ''),
    remoteAddress: typeof address === 'string' ? address : ''
  }
}

export class ProbeRefused extends Error {
  constructor(readonly kind: 'limited' | 'voice_off' | 'failed') {
    super(kind)
  }
}

/** Maps what the probe endpoint answered to the reason a test cannot run. */
export function refusalOf(e: unknown): ProbeRefused {
  if (e instanceof ApiError) {
    if (e.status === 429) return new ProbeRefused('limited')
    if (e.status === 503) return new ProbeRefused('voice_off')
  }
  return new ProbeRefused('failed')
}

/** Connects to the server's voice port with the given ICE servers and policy. */
export async function tryPath(deps: ConnectionTestDeps, servers: RTCIceServer[], policy: RTCIceTransportPolicy): Promise<PathResult> {
  const pc = deps.createPeer({ iceServers: servers, iceTransportPolicy: policy })
  try {
    const offer = await gatheredOffer(pc, deps.gatherMs ?? GATHER_MS)
    if (policy === 'relay' && !offer.includes(' typ relay')) return { outcome: 'fail' }
    let answer: string
    try {
      answer = await deps.probe(offer)
    } catch (e) {
      throw refusalOf(e)
    }
    await pc.setRemoteDescription({ type: 'answer', sdp: answer })
    if (!(await waitConnected(pc, deps.connectMs ?? CONNECT_MS))) return { outcome: 'fail' }
    const pair = await selectedPair(pc)
    return pair ? { outcome: 'ok', pair } : { outcome: 'ok' }
  } finally {
    pc.close()
  }
}

const unavailable: PathResult = { outcome: 'unavailable' }

// A stream shorter than this says nothing about a speed.
const MIN_SPEED_BYTES = 256 * 1024

/** The download speed from the server, or why there is none. From the server's own network it is not measured. */
export async function measureSpeed(deps: ConnectionTestDeps, report: ConnectionReport): Promise<SpeedResult> {
  const remote = report.direct.pair?.remoteAddress
  if (report.direct.outcome === 'ok' && remote && isLocalAddress(remote)) return { outcome: 'lan' }
  if (!deps.download || report.limited) return { outcome: 'fail' }
  try {
    const { bytes, ms } = await deps.download()
    if (bytes < MIN_SPEED_BYTES || ms <= 0) return { outcome: 'fail' }
    return { outcome: 'ok', mbps: (bytes * 8) / ms / 1000 }
  } catch {
    return { outcome: 'fail' }
  }
}

/** Runs the whole test. `onStep` hears which part starts, for a progress line. */
export async function runConnectionTest(deps: ConnectionTestDeps, onStep: (step: 'candidates' | 'direct' | 'relayUdp' | 'relayTcp' | 'speed') => void = () => {}): Promise<ConnectionReport> {
  const servers = await deps.iceServers()
  const turn = onlyUrls(servers, isTurn)
  const report: ConnectionReport = {
    candidates: emptyCounts(),
    stunOffered: servers.some(s => urlsOf(s).some(isStun)),
    turnOffered: turn.length > 0,
    direct: unavailable, relayUdp: unavailable, relayTcp: unavailable,
    voiceOff: false, limited: false
  }
  onStep('candidates')
  report.candidates = await gatherCandidates(deps, servers)
  const paths: Array<['direct' | 'relayUdp' | 'relayTcp', RTCIceServer[], RTCIceTransportPolicy]> = [
    ['direct', onlyUrls(servers, isStun), 'all'],
    ['relayUdp', onlyUrls(turn, u => !isTcp(u)), 'relay'],
    ['relayTcp', onlyUrls(turn, isTcp), 'relay']
  ]
  for (const [name, list, policy] of paths) {
    if (policy === 'relay' && list.length === 0) continue
    onStep(name)
    try {
      report[name] = await tryPath(deps, list, policy)
    } catch (e) {
      if (!(e instanceof ProbeRefused) || e.kind === 'failed') report[name] = { outcome: 'fail' }
      else if (e.kind === 'limited') { report.limited = true; break }
      else { report.voiceOff = true; break }
    }
  }
  if (deps.download) {
    onStep('speed')
    report.speed = await measureSpeed(deps, report)
  }
  return report
}
