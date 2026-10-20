// Turns an RTCStatsReport into the numbers the connection panel shows. Every
// value is measured by the browser; nothing here is estimated or invented.
import { t } from '../i18n'
import type { MediaStat } from './mediaStats'

export interface RTCStatsSample { timestamp: number; bytesSent: number; bytesReceived: number }
export interface ReceivedVideo { width: number; height: number; fps: number }
interface AudioCounters { packetsReceived: number; packetsLost: number; jitter: number; bytesReceived: number; codecId: string | undefined }

function succeededPair(s: MediaStat): boolean {
  if (s.type !== 'candidate-pair') return false
  if (s.state !== 'succeeded') return false
  return !!(s.nominated || s.selected)
}

function pairFromTransport(byId: Map<string, MediaStat>): MediaStat | null {
  let transport: MediaStat | undefined
  for (const s of byId.values()) {
    if (s.type === 'transport' && s.selectedCandidatePairId) {
      transport = s
      break
    }
  }
  if (!transport?.selectedCandidatePairId) return null
  return byId.get(transport.selectedCandidatePairId) || null
}

/** The nominated, succeeded candidate pair, or the one the selected transport names. */
function selectedPair(byId: Map<string, MediaStat>) {
  for (const s of byId.values()) {
    if (succeededPair(s)) return s
  }
  return pairFromTransport(byId)
}

function statCount(n: number | undefined): number {
  if (n) return n
  return 0
}

function freshAudio(s: MediaStat): AudioCounters {
  return {
    packetsReceived: statCount(s.packetsReceived),
    packetsLost: statCount(s.packetsLost),
    jitter: statCount(s.jitter),
    bytesReceived: statCount(s.bytesReceived),
    codecId: s.codecId
  }
}

function mergedAudio(total: AudioCounters, s: MediaStat): AudioCounters {
  return {
    packetsReceived: total.packetsReceived + statCount(s.packetsReceived),
    packetsLost: total.packetsLost + statCount(s.packetsLost),
    jitter: Math.max(total.jitter, statCount(s.jitter)),
    bytesReceived: total.bytesReceived + statCount(s.bytesReceived),
    codecId: codecIdOf(total.codecId, s.codecId)
  }
}

function codecIdOf(current: string | undefined, next: string | undefined): string | undefined {
  if (current) return current
  return next
}

// With several remote speakers, aggregate their streams.
function addInboundAudio(total: AudioCounters | null, s: MediaStat): AudioCounters {
  if (!total) return freshAudio(s)
  return mergedAudio(total, s)
}

function audioStreams(byId: Map<string, MediaStat>) {
  let inAudio: AudioCounters | null = null
  let outAudio: MediaStat | null = null
  for (const s of byId.values()) {
    if (s.kind !== 'audio') continue
    if (s.type === 'inbound-rtp') inAudio = addInboundAudio(inAudio, s)
    else if (s.type === 'outbound-rtp') outAudio = s
  }
  return { inAudio, outAudio }
}

function inboundPicture(s: MediaStat): ReceivedVideo | null {
  if (s.kind !== 'video') return null
  if (s.type !== 'inbound-rtp') return null
  if (!s.frameWidth || !s.frameHeight) return null
  return { width: s.frameWidth, height: s.frameHeight, fps: Math.round(statCount(s.framesPerSecond)) }
}

// The pictures received now (screens and cameras), largest first.
function receivedPictures(byId: Map<string, MediaStat>) {
  const videoIn: ReceivedVideo[] = []
  for (const s of byId.values()) {
    const picture = inboundPicture(s)
    if (picture) videoIn.push(picture)
  }
  return videoIn.sort((a, b) => b.width * b.height - a.width * a.height)
}

function codecLabel(codecStat: MediaStat | undefined) {
  if (!codecStat) return null
  return `${codecStat.mimeType?.replace(/^audio\//, '') || '?'} ${codecStat.clockRate ? codecStat.clockRate / 1000 + ' kHz' : ''}${codecStat.channels ? ', ' + t('stats.channels', { count: codecStat.channels }) : ''}`.trim()
}

/** Send and receive rate since the previous sample, or null for both without one. */
function bitrates(prev: RTCStatsSample | null, now: RTCStatsSample) {
  if (!prev || now.timestamp <= prev.timestamp) return { sendKbps: null, recvKbps: null }
  const seconds = (now.timestamp - prev.timestamp) / 1000
  const kbps = (bytes: number, before: number) => Math.max(0, Math.round(((bytes - before) * 8) / 1000 / seconds))
  return { sendKbps: kbps(now.bytesSent, prev.bytesSent), recvKbps: kbps(now.bytesReceived, prev.bytesReceived) }
}

const candidateLabel = (c: MediaStat | null | undefined) => (c ? `${c.candidateType} / ${c.protocol}` : null)

/**
 * @param {Iterable<object>} stats  values of an RTCStatsReport
 * @param {{timestamp:number, bytesSent:number, bytesReceived:number}|null} prev
 *        the previous sample, for bitrate calculation
 */
function codecStatOf(byId: Map<string, MediaStat>, outAudio: MediaStat | null, inAudio: AudioCounters | null) {
  if (outAudio?.codecId) {
    const stat = byId.get(outAudio.codecId)
    if (stat) return stat
  }
  if (!inAudio?.codecId) return undefined
  return byId.get(inAudio.codecId)
}

function roundTripMs(pair: MediaStat | null): number | null {
  if (pair?.currentRoundTripTime == null) return null
  return Math.round(pair.currentRoundTripTime * 1000)
}

function jitterMs(inAudio: AudioCounters | null): number | null {
  if (!inAudio) return null
  return Math.round(inAudio.jitter * 1000 * 10) / 10
}

function lossPercent(received: number, lost: number): number {
  if (received + lost <= 0) return 0
  return Math.round((lost / (received + lost)) * 1000) / 10
}

function namedCandidate(pair: MediaStat | null, byId: Map<string, MediaStat>, id: string | undefined) {
  if (!pair || !id) return null
  return candidateLabel(byId.get(id))
}

function presentCipher(value: string | undefined): string | null {
  if (!value) return null
  return value
}

export function summarizeStats(stats: Iterable<MediaStat>, prev: RTCStatsSample | null = null) {
  const byId = new Map<string, MediaStat>()
  for (const s of stats) byId.set(s.id, s)
  return describeStats(byId, prev)
}

function describeStats(byId: Map<string, MediaStat>, prev: RTCStatsSample | null) {
  const pair = selectedPair(byId)
  const { inAudio, outAudio } = audioStreams(byId)
  const codecStat = codecStatOf(byId, outAudio, inAudio)
  const transport = [...byId.values()].find(s => s.type === 'transport')
  const timestamp = pair?.timestamp ?? outAudio?.timestamp ?? Date.now()
  const bytesSent = pair?.bytesSent ?? outAudio?.bytesSent ?? 0
  const bytesReceived = pair?.bytesReceived ?? inAudio?.bytesReceived ?? 0
  const sample = { timestamp, bytesSent, bytesReceived }
  const received = inAudio?.packetsReceived ?? 0
  const lost = Math.max(0, inAudio?.packetsLost ?? 0)
  return {
    connected: !!pair,
    rttMs: roundTripMs(pair),
    jitterMs: jitterMs(inAudio),
    packetsReceived: received,
    packetsLost: lost,
    packetsSent: outAudio?.packetsSent ?? 0,
    lossPercent: lossPercent(received, lost),
    ...bitrates(prev, sample),
    codec: codecLabel(codecStat),
    localCandidate: namedCandidate(pair, byId, pair?.localCandidateId),
    remoteCandidate: namedCandidate(pair, byId, pair?.remoteCandidateId),
    videoIn: receivedPictures(byId),
    dtlsCipher: presentCipher(transport?.dtlsCipher),
    srtpCipher: presentCipher(transport?.srtpCipher),
    sample
  }
}

/** Human rating for a measured jitter in milliseconds. */
export function rateJitter(ms: number | null | undefined) {
  if (ms == null) return t('stats.jitter.none')
  if (ms < 10) return t('stats.jitter.veryGood')
  if (ms < 30) return t('stats.jitter.good')
  if (ms < 60) return t('stats.jitter.noticeable')
  return t('stats.jitter.bad')
}

export type RTCConnectionStats = ReturnType<typeof summarizeStats>
