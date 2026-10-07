// Stream quality of my own screen share, picked in the streamer's menu like
// Discord's: a mode (Gaming, Screen or Custom), a resolution and a frame rate.
// These functions turn a choice into what the browser needs: constraints for
// the captured track and parameters for the sender; useWebRTC applies them,
// live and again after every reconnect. The choice lives for one share:
// every new share starts at DEFAULT_STREAM_QUALITY (see useStreamQuality).
import { locale } from '../i18n'
import { isFiniteNumber } from './mediaStats'
import type { MediaStat } from './mediaStats'

export type StreamResolution = 720 | 1080 | 1440 | 'source'
export type StreamFrameRate = 15 | 30 | 60
export type StreamMode = 'gaming' | 'screen' | 'custom'
export type ScreenQuality = StreamQuality
export interface StreamQuality { resolution: StreamResolution; fps: StreamFrameRate; custom?: boolean }
export interface SendStatsSample { id?: string; bytes: number; ts: number; frames?: number; encodeTime?: number }
export interface SendStatsSummary {
  codec: string; width: number; height: number; fps: number | null; kbps: number | null
  captureFps: number | null; encoder: string; efficientEncoder: boolean | null
  limitation: string | null; encodeMs: number | null; codecParameters: string; sample: SendStatsSample
}


// Resolutions by height; 'source' is whatever the capture delivers natively.
export const STREAM_RESOLUTIONS = [720, 1080, 1440, 'source'] as const
export const STREAM_FRAME_RATES = [15, 30, 60] as const
export const STREAM_PRESETS = {
  gaming: { resolution: 1440, fps: 60 },
  screen: { resolution: 'source', fps: 15 }
} satisfies Record<Exclude<StreamMode, 'custom'>, StreamQuality>
export const STREAM_MODES = ['gaming', 'screen', 'custom'] as const
export const DEFAULT_STREAM_QUALITY = Object.freeze<StreamQuality>({ resolution: 1080, fps: 30 })
export const SCREEN_QUALITY_STORAGE_KEY = 'mnema_screen_quality'

// Bitrate bounds (bits per second): the floor matches the start-bitrate
// floor of the screen line (see tuneScreenOffer), the ceiling the former
// fixed cap for 4K at 60 fps.
export const STREAM_MIN_BITRATE = 1_000_000
export const STREAM_MAX_BITRATE = 12_000_000
// About 5 Mbit/s for 1080p at 30 fps, which keeps text sharp; more frames
// cost less than proportionally more bits.
const BITS_PER_PIXEL = 2.5

/** A valid choice: unknown values fall back to the default ones. */
export function normalizeQuality(q: unknown): StreamQuality {
  const input = typeof q === 'object' && q !== null ? q : {}
  const chosenResolution = 'resolution' in input ? input.resolution : undefined
  const chosenFps = 'fps' in input ? input.fps : undefined
  const resolution = chosenResolution === 'source' || chosenResolution === 720 || chosenResolution === 1080 || chosenResolution === 1440
    ? chosenResolution : DEFAULT_STREAM_QUALITY.resolution
  const fps = chosenFps === 15 || chosenFps === 30 || chosenFps === 60 ? chosenFps : DEFAULT_STREAM_QUALITY.fps
  return { resolution, fps }
}

/**
 * 'gaming' or 'screen' when the choice is that preset, else 'custom'. Custom
 * picked on purpose (q.custom) stays custom even on a preset's values.
 */
export function streamModeOf(q: unknown): StreamMode {
  if (typeof q === 'object' && q !== null && 'custom' in q && q.custom === true) return 'custom'
  const { resolution, fps } = normalizeQuality(q)
  for (const mode of ['gaming', 'screen'] as const) {
    const preset = STREAM_PRESETS[mode]
    if (preset.resolution === resolution && preset.fps === fps) return mode
  }
  return 'custom'
}

/**
 * Video constraints for getDisplayMedia and applyConstraints: the frame rate
 * as a cap, the size by height only (the width follows the screen's aspect
 * ratio). The source resolution sets no size at all, so applying it lifts an
 * earlier cap again.
 */
export function trackConstraints(q: unknown): MediaTrackConstraints {
  const { resolution, fps } = normalizeQuality(q)
  const c: MediaTrackConstraints = { frameRate: { ideal: fps, max: fps } }
  if (resolution !== 'source') c.height = { max: resolution }
  return c
}

// The size that goes out: the capture, scaled down to the chosen height.
function sentSize(q: unknown, settings: MediaTrackSettings = {}) {
  const { resolution } = normalizeQuality(q)
  const w = Number(settings.width) || 0
  const h = Number(settings.height) || 0
  if (resolution === 'source') return w && h ? { width: w, height: h } : null
  if (w && h) {
    const height = Math.min(h, resolution)
    return { width: Math.round(w * (height / h)), height }
  }
  return { width: Math.round(resolution * 16 / 9), height: resolution }
}

/** Bitrate cap (bit/s) for a choice at the size the capture delivers. */
export function streamBitrate(q: unknown, settings: MediaTrackSettings = {}) {
  const { fps } = normalizeQuality(q)
  const size = sentSize(q, settings)
  if (!size) return STREAM_MAX_BITRATE
  const bits = size.width * size.height * BITS_PER_PIXEL * Math.pow(fps / 30, 0.7)
  const rounded = Math.round(bits / 100_000) * 100_000
  return Math.min(STREAM_MAX_BITRATE, Math.max(STREAM_MIN_BITRATE, rounded))
}

/**
 * Encoding parameters for the screen sender. scaleResolutionDownBy only does
 * something where the browser could not scale the capture itself (the track
 * is still taller than chosen after applyConstraints); then the encoder
 * scales it down.
 */
export function streamEncoding(q: unknown, settings: MediaTrackSettings = {}) {
  const { resolution, fps } = normalizeQuality(q)
  const h = Number(settings.height) || 0
  const scale = resolution !== 'source' && h > resolution ? Math.round((h / resolution) * 1000) / 1000 : 1
  return { maxBitrate: streamBitrate(q, settings), maxFramerate: fps, scaleResolutionDownBy: scale }
}

/**
 * Smooth video (60 fps, Gaming) is encoded as motion and may give up some
 * resolution under load; everything else is encoded as detail and keeps its
 * resolution, dropping frames instead, so text stays readable.
 */
export function streamTuning(q: unknown): { contentHint: string; degradationPreference: RTCDegradationPreference } {
  const { fps } = normalizeQuality(q)
  return fps >= 60
    ? { contentHint: 'motion', degradationPreference: 'balanced' }
    : { contentHint: 'detail', degradationPreference: 'maintain-resolution' }
}

/**
 * What the screen sender actually sends, from RTCRtpSender.getStats():
 * codec, size, frame rate and the bitrate since the previous sample.
 * Returns { codec, width, height, fps, kbps, sample }; pass `sample` back in
 * as `previous` next time.
 */
export function summarizeSendStats(stats: Iterable<MediaStat> | null | undefined, previous: SendStatsSample | null = null): SendStatsSummary | null {
  const list = [...(stats || [])]
  const codecs = new Map<string, string | undefined>()
  for (const s of list) if (s.type === 'codec') codecs.set(s.id, s.mimeType)
  // The active encoding: the one sending the most (only one without simulcast).
  const out = list
    .filter(s => s.type === 'outbound-rtp' && (s.kind === 'video' || s.mediaType === 'video'))
    .sort((a, b) => (b.bytesSent || 0) - (a.bytesSent || 0))[0]
  if (!out) return null
  const source = list.find(s => s.type === 'media-source' && s.id === out.mediaSourceId)
  const codec = list.find(s => s.type === 'codec' && s.id === out.codecId)
  const mime = codecs.get(out.codecId || '') || ''
  const bytes = out.bytesSent || 0
  const ts = out.timestamp || 0
  const sameStream = previous && (previous.id === undefined || previous.id === out.id)
  let kbps = null
  if (sameStream && ts > previous.ts && bytes >= previous.bytes) {
    kbps = Math.round(((bytes - previous.bytes) * 8) / (ts - previous.ts))
  }
  let fps = isFiniteNumber(out.framesPerSecond) ? Math.round(out.framesPerSecond) : null
  let encodeMs = null
  const frames = out.framesEncoded
  const encodeTime = out.totalEncodeTime
  if (sameStream && ts > previous.ts && isFiniteNumber(frames) && isFiniteNumber(previous.frames)) {
    const count = frames - previous.frames
    if (count >= 0 && fps === null) fps = Math.round(count * 1000 / (ts - previous.ts))
    if (count > 0 && isFiniteNumber(encodeTime) && isFiniteNumber(previous.encodeTime) && encodeTime >= previous.encodeTime) {
      encodeMs = (encodeTime - previous.encodeTime) * 1000 / count
    }
  } else if (isFiniteNumber(frames) && frames > 0 && isFiniteNumber(encodeTime)) {
    encodeMs = encodeTime * 1000 / frames
  }
  return {
    codec: mime.replace(/^video\//i, '').toUpperCase(),
    width: out.frameWidth || 0,
    height: out.frameHeight || 0,
    fps,
    kbps,
    captureFps: isFiniteNumber(source?.framesPerSecond) ? Math.round(source.framesPerSecond) : null,
    encoder: out.encoderImplementation || '',
    efficientEncoder: typeof out.powerEfficientEncoder === 'boolean' ? out.powerEfficientEncoder : null,
    limitation: ['none', 'cpu', 'bandwidth', 'other'].includes(out.qualityLimitationReason || '') ? out.qualityLimitationReason ?? null : null,
    encodeMs,
    codecParameters: codec?.sdpFmtpLine || '',
    sample: { id: out.id, bytes, ts, ...(frames === undefined ? {} : { frames }), ...(encodeTime === undefined ? {} : { encodeTime }) }
  }
}

/** Values used by the advanced diagnosis, distinct from the selected target. */
export function formatStreamDiagnostics(summary: SendStatsSummary | null | undefined, captureSettings: MediaTrackSettings | null = {}, lang: string = locale.value) {
  const dash = '–'
  const num = (n: number) => new Intl.NumberFormat(lang, { maximumFractionDigits: 1 }).format(n)
  return {
    captureSetting: isFiniteNumber(captureSettings?.frameRate) ? `${num(captureSettings.frameRate)} fps` : dash,
    captureFps: summary?.captureFps !== null && summary?.captureFps !== undefined ? `${num(summary.captureFps)} fps` : dash,
    encoder: summary?.encoder || dash,
    encodeTime: isFiniteNumber(summary?.encodeMs) ? `${num(summary.encodeMs)} ms` : dash,
    codecProfile: /(?:^|;)\s*profile-level-id=([0-9a-f]{6})(?:;|$)/i.exec(summary?.codecParameters || '')?.[1] || dash
  }
}

/** Display strings for summarizeSendStats; '–' where nothing is known yet. */
export function formatSendStats(summary: SendStatsSummary | null | undefined, lang: string = locale.value) {
  const dash = '–'
  if (!summary) return { codec: dash, resolution: dash, fps: dash, bitrate: dash }
  const num = (n: number, digits = 0) => new Intl.NumberFormat(lang, { maximumFractionDigits: digits, minimumFractionDigits: digits }).format(n)
  let bitrate = dash
  if (summary.kbps !== null && summary.kbps !== undefined) {
    bitrate = summary.kbps >= 1000 ? `${num(summary.kbps / 1000, 1)} Mbit/s` : `${num(summary.kbps)} kbit/s`
  }
  return {
    codec: summary.codec || dash,
    resolution: summary.width && summary.height ? `${summary.width}×${summary.height}` : dash,
    fps: summary.fps !== null && summary.fps !== undefined ? `${num(summary.fps)} fps` : dash,
    bitrate
  }
}
