import type { MediaStat } from '../../lib/mediaStats'

function outboundFields(s: MediaStat) {
  return {
    limit: s.qualityLimitationReason || '',
    encoder: s.encoderImplementation || '',
    decoder: undefined,
    // Hardware (power efficient) encoder or decoder; Chrome reports it once
    // the page holds a capture permission, other browsers leave it out.
    hw: s.powerEfficientEncoder,
    lost: s.packetsLost ?? undefined,
    dropped: undefined,
    freezes: undefined,
    nacks: s.nackCount ?? undefined,
    plis: s.pliCount ?? undefined
  }
}

function inboundFields(s: MediaStat) {
  return {
    limit: undefined,
    encoder: undefined,
    decoder: s.decoderImplementation || '',
    hw: s.powerEfficientDecoder,
    lost: s.packetsLost ?? undefined,
    dropped: s.framesDropped ?? 0,
    freezes: s.freezeCount ?? 0,
    nacks: s.nackCount ?? undefined,
    plis: s.pliCount ?? undefined
  }
}

// What only one direction reports: why the sender holds back, which encoder
// or decoder works, and what the receiver dropped. The other side leaves them out.
function directionFields(s: MediaStat, sending: boolean) {
  return sending ? outboundFields(s) : inboundFields(s)
}

const kbpsSince = (bytes: number, before: number | undefined, seconds: number) =>
  before === undefined ? null : Math.round(((bytes - before) * 8) / 1000 / seconds)

const isVideoRtp = (s: MediaStat) => s.kind === 'video' && (s.type === 'outbound-rtp' || s.type === 'inbound-rtp')

function collectCodecs(stats: Iterable<MediaStat>): Map<string, string | undefined> {
  const codecs = new Map<string, string | undefined>()
  for (const s of stats) {
    if (s.type === 'codec') codecs.set(s.id, s.mimeType)
  }
  return codecs
}

function formatFrameDimensions(width?: number, height?: number): string {
  return width ? `${width}x${height}` : ''
}

function buildStreamEntry(
  s: MediaStat,
  codecs: Map<string, string | undefined>,
  previousBytes: Map<string, number>,
  seconds: number
) {
  const sending = s.type === 'outbound-rtp'
  const bytes = sending ? s.bytesSent : s.bytesReceived
  if (!bytes) return null
  const before = previousBytes.get(s.id)
  previousBytes.set(s.id, bytes)
  return {
    dir: sending ? 'out' : 'in',
    codec: codecs.get(s.codecId || '') || '',
    size: formatFrameDimensions(s.frameWidth, s.frameHeight),
    fps: Math.round(s.framesPerSecond || 0),
    kbps: kbpsSince(bytes, before, seconds),
    ...directionFields(s, sending)
  }
}

/**
 * What a shared screen looks like on the wire, for the server log: per video
 * stream its codec, size, frame rate, bitrate, losses and, when sending, why
 * the browser holds the quality back (cpu, bandwidth). Never addresses.
 */
export function streamDiag(stats: Iterable<MediaStat>, previousBytes = new Map<string, number>(), seconds = 10) {
  const codecs = collectCodecs(stats)
  const out = []
  for (const s of stats) {
    if (!isVideoRtp(s)) continue
    const entry = buildStreamEntry(s, codecs, previousBytes, seconds)
    if (entry) out.push(entry)
  }
  return out
}
