// Turns an RTCStatsReport into the numbers the connection panel shows. Every
// value is measured by the browser; nothing here is estimated or invented.
import { t } from '../i18n'

/**
 * @param {Iterable<object>} stats  values of an RTCStatsReport
 * @param {{timestamp:number, bytesSent:number, bytesReceived:number}|null} prev
 *        the previous sample, for bitrate calculation
 */
export function summarizeStats(stats, prev = null) {
  const byId = new Map()
  for (const s of stats) byId.set(s.id, s)

  let pair = null
  for (const s of byId.values()) {
    if (s.type === 'candidate-pair' && (s.nominated || s.selected) && s.state === 'succeeded') {
      pair = s
      break
    }
  }
  if (!pair) {
    const transport = [...byId.values()].find(s => s.type === 'transport' && s.selectedCandidatePairId)
    if (transport) pair = byId.get(transport.selectedCandidatePairId) || null
  }

  let inAudio = null
  let outAudio = null
  for (const s of byId.values()) {
    if (s.kind !== 'audio') continue
    if (s.type === 'inbound-rtp') {
      // With several remote speakers, aggregate their streams.
      inAudio = inAudio
        ? {
            packetsReceived: inAudio.packetsReceived + (s.packetsReceived || 0),
            packetsLost: inAudio.packetsLost + (s.packetsLost || 0),
            jitter: Math.max(inAudio.jitter, s.jitter || 0),
            bytesReceived: inAudio.bytesReceived + (s.bytesReceived || 0),
            codecId: inAudio.codecId || s.codecId
          }
        : {
            packetsReceived: s.packetsReceived || 0,
            packetsLost: s.packetsLost || 0,
            jitter: s.jitter || 0,
            bytesReceived: s.bytesReceived || 0,
            codecId: s.codecId
          }
    } else if (s.type === 'outbound-rtp') {
      outAudio = s
    }
  }

  const codecStat = byId.get(outAudio?.codecId) || byId.get(inAudio?.codecId)
  const transport = [...byId.values()].find(s => s.type === 'transport')
  const local = pair && byId.get(pair.localCandidateId)
  const remote = pair && byId.get(pair.remoteCandidateId)

  const timestamp = pair?.timestamp ?? outAudio?.timestamp ?? Date.now()
  const bytesSent = pair?.bytesSent ?? outAudio?.bytesSent ?? 0
  const bytesReceived = pair?.bytesReceived ?? inAudio?.bytesReceived ?? 0

  let sendKbps = null
  let recvKbps = null
  if (prev && timestamp > prev.timestamp) {
    const seconds = (timestamp - prev.timestamp) / 1000
    sendKbps = Math.max(0, Math.round(((bytesSent - prev.bytesSent) * 8) / 1000 / seconds))
    recvKbps = Math.max(0, Math.round(((bytesReceived - prev.bytesReceived) * 8) / 1000 / seconds))
  }

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
    sendKbps,
    recvKbps,
    codec: codecStat
      ? `${codecStat.mimeType?.replace(/^audio\//, '') || '?'} ${codecStat.clockRate ? codecStat.clockRate / 1000 + ' kHz' : ''}${codecStat.channels ? ', ' + t('stats.channels', { count: codecStat.channels }) : ''}`.trim()
      : null,
    localCandidate: local ? `${local.candidateType} / ${local.protocol}` : null,
    remoteCandidate: remote ? `${remote.candidateType} / ${remote.protocol}` : null,
    dtlsCipher: transport?.dtlsCipher || null,
    srtpCipher: transport?.srtpCipher || null,
    sample: { timestamp, bytesSent, bytesReceived }
  }
}

/** Human rating for a measured jitter in milliseconds. */
export function rateJitter(ms) {
  if (ms == null) return t('stats.jitter.none')
  if (ms < 10) return t('stats.jitter.veryGood')
  if (ms < 30) return t('stats.jitter.good')
  if (ms < 60) return t('stats.jitter.noticeable')
  return t('stats.jitter.bad')
}
