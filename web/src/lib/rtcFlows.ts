// The media lines of one call, told apart: what goes out as microphone,
// camera, screen and screen sound, and what comes in as audio and video.
// A peer connection also carries lines that send nothing right now (the
// screen share's sound before any share); they must never stand in for the
// microphone, so every outbound stream is matched to its line by mid.
import type { MediaStat } from './mediaStats'

export type SendLine = 'mic' | 'screenAudio' | 'camera' | 'screen'
export type FlowKey = SendLine | 'audioIn' | 'videoIn'
/** The mids of the lines this client sends on, from its senders. */
export type SendLines = ReadonlyMap<string, SendLine>
/** Whether the microphone is feeding the line, from the track the sender holds. */
export type MicState = 'none' | 'sending' | 'paused'

export interface Flow { packets: number; bytes: number; kbps: number | null; lost: number }
export type Flows = Partial<Record<FlowKey, Flow>>
/** Bytes per flow at a moment, for the rate of the next sample. */
export interface FlowBytes { timestamp: number; bytes: Partial<Record<FlowKey, number>> }

export const NO_LINES: SendLines = new Map()

function count(n: number | undefined): number {
  return n && n > 0 ? n : 0
}

function sendLine(s: MediaStat, lines: SendLines): SendLine | null {
  if (s.mid != null && lines.size > 0) return lines.get(s.mid) ?? null
  if (s.kind === 'audio') return 'mic'
  return s.contentType === 'screenshare' ? 'screen' : 'camera'
}

function flowKey(s: MediaStat, lines: SendLines): FlowKey | null {
  if (s.type === 'outbound-rtp') return sendLine(s, lines)
  if (s.type !== 'inbound-rtp') return null
  return s.kind === 'video' ? 'videoIn' : 'audioIn'
}

function rate(key: FlowKey, bytes: number, prev: FlowBytes | null, timestamp: number): number | null {
  const before = prev?.bytes[key]
  if (!prev || before == null || timestamp <= prev.timestamp) return null
  return Math.max(0, Math.round(((bytes - before) * 8) / 1000 / ((timestamp - prev.timestamp) / 1000)))
}

function added(flow: Flow | undefined, s: MediaStat): Flow {
  const packets = count(s.packetsSent) + count(s.packetsReceived)
  return {
    packets: (flow?.packets ?? 0) + packets,
    bytes: (flow?.bytes ?? 0) + count(s.bytesSent) + count(s.bytesReceived),
    kbps: null,
    lost: (flow?.lost ?? 0) + count(s.packetsLost)
  }
}

/**
 * Totals per line since the connection began, and the rate since `prev`.
 * Streams of a line (simulcast layers, several speakers) add up.
 */
export function summarizeFlows(stats: Iterable<MediaStat>, lines: SendLines, prev: FlowBytes | null, timestamp: number) {
  const flows: Flows = {}
  for (const s of stats) {
    const key = flowKey(s, lines)
    if (key) flows[key] = added(flows[key], s)
  }
  const bytes: FlowBytes['bytes'] = {}
  for (const key of Object.keys(flows) as FlowKey[]) {
    const flow = flows[key]
    if (!flow) continue
    bytes[key] = flow.bytes
    flow.kbps = rate(key, flow.bytes, prev, timestamp)
  }
  return { flows, sample: { timestamp, bytes } }
}

/** The outbound stream of the microphone line that carries the most, for its codec. */
export function micStream(stats: Iterable<MediaStat>, lines: SendLines): MediaStat | null {
  let best: MediaStat | null = null
  for (const s of stats) {
    if (s.type !== 'outbound-rtp' || sendLine(s, lines) !== 'mic') continue
    if (!best || count(s.packetsSent) >= count(best.packetsSent)) best = s
  }
  return best
}
