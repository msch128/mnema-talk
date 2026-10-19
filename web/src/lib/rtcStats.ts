// Turns an RTCStatsReport into the numbers the connection panel shows. Every
// value is measured by the browser; nothing here is estimated or invented.
import { t } from '../i18n'
import type { MediaStat } from './mediaStats'

export interface RTCStatsSample { timestamp: number; bytesSent: number; bytesReceived: number }
export interface ReceivedVideo { width: number; height: number; fps: number }
interface AudioCounters { packetsReceived: number; packetsLost: number; jitter: number; bytesReceived: number; codecId: string | undefined }

/** The nominated, succeeded candidate pair, or the one the selected transport names. */
function selectedPair(byId: Map<string, MediaStat>) {
  for (const s of byId.values()) {
    if (s.type === 'candidate-pair' && (s.nominated || s.selected) && s.state === 'succeeded') return s
  }
  const transport = [...byId.values()].find(s => s.type === 'transport' && s.selectedCandidatePairId)
  return transport ? byId.get(transport.selectedCandidatePairId || '') || null : null
}

// With several remote speakers, aggregate their streams.
function addInboundAudio(total: AudioCounters | null, s: MediaStat): AudioCounters {
  if (!total) {
    return {
      packetsReceived: s.packetsReceived || 0,
      packetsLost: s.packetsLost || 0,
      jitter: s.jitter || 0,
      bytesReceived: s.bytesReceived || 0,
      codecId: s.codecId
    }
  }
  return {
    packetsReceived: total.packetsReceived + (s.packetsReceived || 0),
    packetsLost: total.packetsLost + (s.packetsLost || 0),
    jitter: Math.max(total.jitter, s.jitter || 0),
    bytesReceived: total.bytesReceived + (s.bytesReceived || 0),
    codecId: total.codecId || s.codecId
  }
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

// The pictures received now (screens and cameras), largest first.
function receivedPictures(byId: Map<string, MediaStat>) {
  const videoIn: ReceivedVideo[] = []
  for (const s of byId.values()) {
    if (s.kind !== 'video' || s.type !== 'inbound-rtp' || !s.frameWidth || !s.frameHeight) continue
    videoIn.push({ width: s.frameWidth, height: s.frameHeight, fps: Math.round(s.framesPerSecond || 0) })
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
export function summarizeStats(stats: Iterable<MediaStat>, prev: RTCStatsSample | null = null) {
  const byId = new Map<string, MediaStat>()
  for (const s of stats) byId.set(s.id, s)

  const pair = selectedPair(byId)
  const { inAudio, outAudio } = audioStreams(byId)
  const codecStat = byId.get(outAudio?.codecId || '') || byId.get(inAudio?.codecId || '')
  const transport = [...byId.values()].find(s => s.type === 'transport')

  const timestamp = pair?.timestamp ?? outAudio?.timestamp ?? Date.now()
  const bytesSent = pair?.bytesSent ?? outAudio?.bytesSent ?? 0
  const bytesReceived = pair?.bytesReceived ?? inAudio?.bytesReceived ?? 0
  const sample = { timestamp, bytesSent, bytesReceived }

  const received = inAudio?.packetsReceived ?? 0
  const lost = Math.max(0, inAudio?.packetsLost ?? 0)

  return {
    connected: !!pair,
    rttMs: pair?.currentRoundTripTime != null ? Math.round(pair.currentRoundTripTime * 1000) : null,
    jitterMs: inAudio ? Math.round(inAudio.jitter * 1000 * 10) / 10 : null,
    packetsReceived: received,
    packetsLost: lost,
    packetsSent: outAudio?.packetsSent ?? 0,
    lossPercent: received + lost > 0 ? Math.round((lost / (received + lost)) * 1000) / 10 : 0,
    ...bitrates(prev, sample),
    codec: codecLabel(codecStat),
    localCandidate: candidateLabel(pair && byId.get(pair.localCandidateId || '')),
    remoteCandidate: candidateLabel(pair && byId.get(pair.remoteCandidateId || '')),
    videoIn: receivedPictures(byId),
    dtlsCipher: transport?.dtlsCipher || null,
    srtpCipher: transport?.srtpCipher || null,
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
