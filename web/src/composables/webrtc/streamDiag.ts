import type { MediaStat } from '../../lib/mediaStats'

// What only one direction reports: why the sender holds back, which encoder
// or decoder works, and what the receiver dropped. The other side leaves them out.
function directionFields(s: MediaStat, sending: boolean) {
  return {
    limit: sending ? (s.qualityLimitationReason || '') : undefined,
    encoder: sending ? (s.encoderImplementation || '') : undefined,
    decoder: sending ? undefined : (s.decoderImplementation || ''),
    // Hardware (power efficient) encoder or decoder; Chrome reports it once
    // the page holds a capture permission, other browsers leave it out.
    hw: sending ? s.powerEfficientEncoder : s.powerEfficientDecoder,
    lost: s.packetsLost ?? undefined,
    dropped: sending ? undefined : (s.framesDropped ?? 0),
    freezes: sending ? undefined : (s.freezeCount ?? 0),
    nacks: s.nackCount ?? undefined,
    plis: s.pliCount ?? undefined
  }
}

const kbpsSince = (bytes: number, before: number | undefined, seconds: number) =>
  before === undefined ? null : Math.round(((bytes - before) * 8) / 1000 / seconds)

const isVideoRtp = (s: MediaStat) => s.kind === 'video' && (s.type === 'outbound-rtp' || s.type === 'inbound-rtp')

/**
 * What a shared screen looks like on the wire, for the server log: per video
 * stream its codec, size, frame rate, bitrate, losses and, when sending, why
 * the browser holds the quality back (cpu, bandwidth). Never addresses.
 */
export function streamDiag(stats: Iterable<MediaStat>, previousBytes = new Map<string, number>(), seconds = 10) {
  const codecs = new Map<string, string | undefined>()
  for (const s of stats) if (s.type === 'codec') codecs.set(s.id, s.mimeType)
  const out = []
  for (const s of stats) {
    if (!isVideoRtp(s)) continue
    const sending = s.type === 'outbound-rtp'
    const bytes = sending ? s.bytesSent : s.bytesReceived
    if (!bytes) continue
    const before = previousBytes.get(s.id)
    previousBytes.set(s.id, bytes)
    out.push({
      dir: sending ? 'out' : 'in',
      codec: codecs.get(s.codecId || '') || '',
      size: s.frameWidth ? `${s.frameWidth}x${s.frameHeight}` : '',
      fps: Math.round(s.framesPerSecond || 0),
      kbps: kbpsSince(bytes, before, seconds),
      ...directionFields(s, sending)
    })
  }
  return out
}
