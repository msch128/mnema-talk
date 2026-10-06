// Stream quality of my own screen share, picked in the streamer's menu like
// Discord's: a mode (Gaming, Screen or Custom), a resolution and a frame rate.
// These functions turn a choice into what the browser needs: constraints for
// the captured track and parameters for the sender; useWebRTC applies them,
// live and again after every reconnect. The choice lives for one share:
// every new share starts at DEFAULT_STREAM_QUALITY (see useStreamQuality).
import { locale } from '../i18n'

// Resolutions by height; 'source' is whatever the capture delivers natively.
export const STREAM_RESOLUTIONS = [720, 1080, 1440, 'source']
export const STREAM_FRAME_RATES = [15, 30, 60]
export const STREAM_PRESETS = {
  gaming: { resolution: 1440, fps: 60 },
  screen: { resolution: 'source', fps: 15 }
}
export const STREAM_MODES = ['gaming', 'screen', 'custom']
export const DEFAULT_STREAM_QUALITY = Object.freeze({ resolution: 1080, fps: 30 })

// Bitrate bounds (bits per second): the floor matches the start-bitrate
// floor of the screen line (see tuneScreenOffer), the ceiling the former
// fixed cap for 4K at 60 fps.
export const STREAM_MIN_BITRATE = 1_000_000
export const STREAM_MAX_BITRATE = 12_000_000
// About 5 Mbit/s for 1080p at 30 fps, which keeps text sharp; more frames
// cost less than proportionally more bits.
const BITS_PER_PIXEL = 2.5

/** A valid choice: unknown values fall back to the default ones. */
export function normalizeQuality(q) {
  const resolution = STREAM_RESOLUTIONS.includes(q?.resolution) ? q.resolution : DEFAULT_STREAM_QUALITY.resolution
  const fps = STREAM_FRAME_RATES.includes(q?.fps) ? q.fps : DEFAULT_STREAM_QUALITY.fps
  return { resolution, fps }
}

/**
 * 'gaming' or 'screen' when the choice is that preset, else 'custom'. Custom
 * picked on purpose (q.custom) stays custom even on a preset's values.
 */
export function streamModeOf(q) {
  if (q?.custom === true) return 'custom'
  const { resolution, fps } = normalizeQuality(q)
  for (const [mode, preset] of Object.entries(STREAM_PRESETS)) {
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
export function trackConstraints(q) {
  const { resolution, fps } = normalizeQuality(q)
  const c = { frameRate: { ideal: fps, max: fps } }
  if (resolution !== 'source') c.height = { max: resolution }
  return c
}

// The size that goes out: the capture, scaled down to the chosen height.
function sentSize(q, settings = {}) {
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
export function streamBitrate(q, settings = {}) {
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
export function streamEncoding(q, settings = {}) {
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
export function streamTuning(q) {
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
export function summarizeSendStats(stats, previous = null) {
  const list = [...(stats || [])]
  const codecs = new Map()
  for (const s of list) if (s.type === 'codec') codecs.set(s.id, s.mimeType)
  // The active encoding: the one sending the most (only one without simulcast).
  const out = list
    .filter(s => s.type === 'outbound-rtp' && (s.kind === 'video' || s.mediaType === 'video'))
    .sort((a, b) => (b.bytesSent || 0) - (a.bytesSent || 0))[0]
  if (!out) return null
  const mime = codecs.get(out.codecId) || ''
  const bytes = out.bytesSent || 0
  const ts = out.timestamp || 0
  let kbps = null
  if (previous && ts > previous.ts && bytes >= previous.bytes) {
    kbps = Math.round(((bytes - previous.bytes) * 8) / (ts - previous.ts))
  }
  return {
    codec: mime.replace(/^video\//i, '').toUpperCase(),
    width: out.frameWidth || 0,
    height: out.frameHeight || 0,
    fps: out.framesPerSecond !== undefined ? Math.round(out.framesPerSecond) : null,
    kbps,
    sample: { bytes, ts }
  }
}

/** Display strings for summarizeSendStats; '–' where nothing is known yet. */
export function formatSendStats(summary, lang = locale.value) {
  const dash = '–'
  if (!summary) return { codec: dash, resolution: dash, fps: dash, bitrate: dash }
  const num = (n, digits = 0) => new Intl.NumberFormat(lang, { maximumFractionDigits: digits, minimumFractionDigits: digits }).format(n)
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
