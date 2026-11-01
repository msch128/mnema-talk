// Turns what "Test my connection" measured into one plain verdict with a
// likely cause. Pure: the browser work is in connectionTest.ts.

/** How one way to reach the server turned out. */
export type PathOutcome = 'ok' | 'fail' | 'unavailable'

export interface PathResult {
  outcome: PathOutcome
  /** The pair the connection settled on, when it connected. */
  pair?: { localType: string; remoteType: string; remoteAddress: string }
}

export interface CandidateCounts { host: number; srflx: number; relay: number }

/** What the download speed test found. */
export type SpeedResult =
  | { outcome: 'ok'; mbps: number }
  /** Run from the server's own network: a download there says nothing about the upload. */
  | { outcome: 'lan' }
  | { outcome: 'fail' }

/** Mbit/s of upload one screen watcher needs: 1.5 Mbit/s plus 30 % for headers, keyframes and retransmissions (doc/benchmark.md). */
export const SCREEN_VIEWER_MBPS = 1.95

/** How many members can watch one screen share over this much upload. */
export function screenViewersFor(mbps: number): number {
  return Math.max(0, Math.floor(mbps / SCREEN_VIEWER_MBPS))
}

export interface ConnectionReport {
  candidates: CandidateCounts
  /** The server hands out STUN servers / a TURN relay to this member. */
  stunOffered: boolean
  turnOffered: boolean
  direct: PathResult
  relayUdp: PathResult
  relayTcp: PathResult
  /** The server's voice engine is off (503). */
  voiceOff: boolean
  /** The server refused the test as too frequent (429). */
  limited: boolean
  /** The download speed test, when the test ran one. */
  speed?: SpeedResult
}

export type VerdictKind = 'direct' | 'relay_only' | 'none' | 'inconclusive'

/** The likely reason; each has an i18n text under connectionTest.cause.*. */
export type Cause =
  | 'ok'
  | 'same_network'
  | 'udp_not_forwarded'
  | 'udp_blocked'
  | 'voice_off'
  | 'no_relay_offered'
  | 'only_local_addresses'
  | 'relay_unreachable'
  | 'limited'

export interface Verdict {
  kind: VerdictKind
  cause: Cause
}

const PRIVATE_V4 = /^(10\.|127\.|169\.254\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\.)/

/** True for an address on a private, loopback, link-local or shared (CGNAT) network. */
export function isLocalAddress(address: string): boolean {
  const a = address.toLowerCase()
  return PRIVATE_V4.test(a) || a === '::1' || a.startsWith('fe80:') || a.startsWith('fc') || a.startsWith('fd') || a.endsWith('.local')
}

const works = (p: PathResult) => p.outcome === 'ok'

function relayOnly(r: ConnectionReport): Verdict {
  // UDP to the relay passes, so the network lets UDP out: the voice port is
  // what is wrong. Only TCP passing means the network blocks UDP altogether.
  return { kind: 'relay_only', cause: works(r.relayUdp) ? 'udp_not_forwarded' : 'udp_blocked' }
}

function nothingWorks(r: ConnectionReport): Verdict {
  if (!r.turnOffered) return { kind: 'none', cause: 'no_relay_offered' }
  if (r.candidates.srflx === 0 && r.candidates.relay === 0) return { kind: 'none', cause: 'only_local_addresses' }
  if (r.candidates.relay === 0) return { kind: 'none', cause: 'relay_unreachable' }
  return { kind: 'none', cause: 'udp_not_forwarded' }
}

export function verdictOf(r: ConnectionReport): Verdict {
  if (works(r.direct)) {
    const remote = r.direct.pair?.remoteAddress
    return { kind: 'direct', cause: remote && isLocalAddress(remote) ? 'same_network' : 'ok' }
  }
  if (works(r.relayUdp) || works(r.relayTcp)) return relayOnly(r)
  // Nothing connected; a refused test says why before a failed one does.
  if (r.limited) return { kind: 'inconclusive', cause: 'limited' }
  if (r.voiceOff) return { kind: 'none', cause: 'voice_off' }
  return nothingWorks(r)
}
