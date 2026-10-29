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

function isStreamResolution(value: unknown): value is StreamResolution {
  if (value === 'source' || value === 720) return true
  return value === 1080 || value === 1440
}

function isStreamFrameRate(value: unknown): value is StreamFrameRate {
  if (value === 15 || value === 30) return true
  return value === 60
}

/** A valid choice: unknown values fall back to the default ones; Custom picked on purpose is kept. */
export function normalizeQuality(q: unknown): StreamQuality {
  const input = typeof q === 'object' && q !== null ? q : {}
  const chosenResolution = 'resolution' in input ? input.resolution : undefined
  const chosenFps = 'fps' in input ? input.fps : undefined
  const resolution = isStreamResolution(chosenResolution) ? chosenResolution : DEFAULT_STREAM_QUALITY.resolution
  const fps = isStreamFrameRate(chosenFps) ? chosenFps : DEFAULT_STREAM_QUALITY.fps
  return { resolution, fps, ...(markedCustom(q) ? { custom: true } : {}) }
}

/**
 * 'gaming' or 'screen' when the choice is that preset, else 'custom'. Custom
 * picked on purpose (q.custom) stays custom even on a preset's values.
 */
function markedCustom(q: unknown): boolean {
  if (typeof q !== 'object' || q === null) return false
  return 'custom' in q && q.custom === true
}

export function streamModeOf(q: unknown): StreamMode {
  if (markedCustom(q)) return 'custom'
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
export function sentSize(q: unknown, settings: MediaTrackSettings = {}) {
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

/** Bitrate cap (bit/s) for pictures of this size and frame rate. */
export function bitrateFor(width: number, height: number, fps: number) {
  const bits = width * height * BITS_PER_PIXEL * Math.pow(fps / 30, 0.7)
  const rounded = Math.round(bits / 100_000) * 100_000
  return Math.min(STREAM_MAX_BITRATE, Math.max(STREAM_MIN_BITRATE, rounded))
}

/** Bitrate cap (bit/s) for a choice at the size the capture delivers. */
export function streamBitrate(q: unknown, settings: MediaTrackSettings = {}) {
  const { fps } = normalizeQuality(q)
  const size = sentSize(q, settings)
  if (!size) return STREAM_MAX_BITRATE
  return bitrateFor(size.width, size.height, fps)
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

/** The active encoding: the video sender sending the most (only one without simulcast). */
function activeVideoSender(list: MediaStat[]) {
  return list
    .filter(s => s.type === 'outbound-rtp' && (s.kind === 'video' || s.mediaType === 'video'))
    .sort((a, b) => (b.bytesSent || 0) - (a.bytesSent || 0))[0]
}

function reportedFps(out: MediaStat): number | null {
  return isFiniteNumber(out.framesPerSecond) ? Math.round(out.framesPerSecond) : null
}

/** Frames encoded since the previous sample, or null when the two cannot be compared. */
function frameDelta(previous: SendStatsSample, ts: number, frames: number | undefined): number | null {
  if (ts <= previous.ts || !isFiniteNumber(frames)) return null
  if (!isFiniteNumber(previous.frames)) return null
  return frames - previous.frames
}

function fpsSince(count: number, reported: number | null, ts: number, previousTs: number): number | null {
  if (count < 0 || reported !== null) return reported
  return Math.round(count * 1000 / (ts - previousTs))
}

function encodeMsSince(count: number, encodeTime: number | undefined, previousEncode: number | undefined): number | null {
  if (count <= 0 || !isFiniteNumber(encodeTime)) return null
  if (!isFiniteNumber(previousEncode) || encodeTime < previousEncode) return null
  return (encodeTime - previousEncode) * 1000 / count
}

function lifetimeEncodeMs(frames: number | undefined, encodeTime: number | undefined): number | null {
  if (!isFiniteNumber(frames) || frames <= 0) return null
  if (!isFiniteNumber(encodeTime)) return null
  return encodeTime * 1000 / frames
}

/** Encoded frame rate (the browser's own, else counted since the previous sample) and the encode time per frame. */
function encodeFrameStats(out: MediaStat, previous: SendStatsSample | null, sameStream: boolean) {
  const ts = out.timestamp || 0
  const frames = out.framesEncoded
  const reported = reportedFps(out)
  const count = previous && sameStream ? frameDelta(previous, ts, frames) : null
  if (count !== null && previous) {
    return { fps: fpsSince(count, reported, ts, previous.ts), encodeMs: encodeMsSince(count, out.totalEncodeTime, previous.encodeTime) }
  }
  return { fps: reported, encodeMs: lifetimeEncodeMs(frames, out.totalEncodeTime) }
}

/**
 * What the screen sender actually sends, from RTCRtpSender.getStats():
 * codec, size, frame rate and the bitrate since the previous sample.
 * Returns { codec, width, height, fps, kbps, sample }; pass `sample` back in
 * as `previous` next time.
 */
function codecNames(list: MediaStat[]): Map<string, string | undefined> {
  const codecs = new Map<string, string | undefined>()
  for (const stat of list) if (stat.type === 'codec') codecs.set(stat.id, stat.mimeType)
  return codecs
}

function statById(list: MediaStat[], type: string, id: string | undefined): MediaStat | undefined {
  if (!id) return undefined
  return list.find(stat => stat.type === type && stat.id === id)
}

function sameSendStream(previous: SendStatsSample | null, id: string | undefined): boolean {
  if (!previous) return false
  return previous.id === undefined || previous.id === id
}

function bitrateSince(previous: SendStatsSample | null, sameStream: boolean, ts: number, bytes: number): number | null {
  if (!previous || !sameStream) return null
  if (ts <= previous.ts || bytes < previous.bytes) return null
  return Math.round(((bytes - previous.bytes) * 8) / (ts - previous.ts))
}

function captureFpsOf(source: MediaStat | undefined): number | null {
  const fps = source?.framesPerSecond
  return isFiniteNumber(fps) ? Math.round(fps) : null
}

function efficientOf(out: MediaStat): boolean | null {
  return typeof out.powerEfficientEncoder === 'boolean' ? out.powerEfficientEncoder : null
}

function knownLimitation(reason: string): boolean {
  if (reason === 'none' || reason === 'cpu') return true
  return reason === 'bandwidth' || reason === 'other'
}

function limitationOf(reason: string | undefined): string | null {
  if (!reason || !knownLimitation(reason)) return null
  return reason
}

function textOrEmpty(value: string | undefined): string {
  if (!value) return ''
  return value
}

function numberOrZero(value: number | undefined): number {
  if (!value) return 0
  return value
}

function statList(stats: Iterable<MediaStat> | null | undefined): MediaStat[] {
  if (!stats) return []
  return [...stats]
}

function codecLabel(list: MediaStat[], codecId: string | undefined): string {
  const mime = codecNames(list).get(textOrEmpty(codecId))
  return textOrEmpty(mime).replace(/^video\//i, '').toUpperCase()
}

function codecLine(codec: MediaStat | undefined): string {
  if (!codec) return ''
  return textOrEmpty(codec.sdpFmtpLine)
}

function sampleOf(out: MediaStat, bytes: number, ts: number): SendStatsSample {
  const frames = out.framesEncoded
  const encodeTime = out.totalEncodeTime
  return {
    id: out.id,
    bytes,
    ts,
    ...(frames === undefined ? {} : { frames }),
    ...(encodeTime === undefined ? {} : { encodeTime }),
  }
}

function buildSendSummary(list: MediaStat[], out: MediaStat, previous: SendStatsSample | null): SendStatsSummary {
  const source = statById(list, 'media-source', out.mediaSourceId)
  const codec = statById(list, 'codec', out.codecId)
  const bytes = numberOrZero(out.bytesSent)
  const ts = numberOrZero(out.timestamp)
  const sameStream = sameSendStream(previous, out.id)
  const rates = encodeFrameStats(out, previous, sameStream)
  return {
    codec: codecLabel(list, out.codecId),
    width: numberOrZero(out.frameWidth),
    height: numberOrZero(out.frameHeight),
    fps: rates.fps,
    kbps: bitrateSince(previous, sameStream, ts, bytes),
    captureFps: captureFpsOf(source),
    encoder: textOrEmpty(out.encoderImplementation),
    efficientEncoder: efficientOf(out),
    limitation: limitationOf(out.qualityLimitationReason),
    encodeMs: rates.encodeMs,
    codecParameters: codecLine(codec),
    sample: sampleOf(out, bytes, ts),
  }
}

export function summarizeSendStats(stats: Iterable<MediaStat> | null | undefined, previous: SendStatsSample | null = null): SendStatsSummary | null {
  const list = statList(stats)
  const out = activeVideoSender(list)
  if (!out) return null
  return buildSendSummary(list, out, previous)
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

function formatBitrate(kbps: number | null | undefined, lang: string): string {
  const dash = '–'
  if (kbps === null || kbps === undefined) return dash
  const digits = kbps >= 1000 ? 1 : 0
  const num = new Intl.NumberFormat(lang, { maximumFractionDigits: digits, minimumFractionDigits: digits })
  if (kbps >= 1000) return `${num.format(kbps / 1000)} Mbit/s`
  return `${num.format(kbps)} kbit/s`
}

function formatResolution(width: number, height: number): string | null {
  if (!width || !height) return null
  return `${width}×${height}`
}

function formatFps(fps: number | null | undefined, lang: string): string | null {
  if (fps === null || fps === undefined) return null
  const num = new Intl.NumberFormat(lang, { maximumFractionDigits: 0 }).format(fps)
  return `${num} fps`
}

/** Display strings for summarizeSendStats; '–' where nothing is known yet. */
export function formatSendStats(summary: SendStatsSummary | null | undefined, lang: string = locale.value) {
  const dash = '–'
  if (!summary) return { codec: dash, resolution: dash, fps: dash, bitrate: dash }
  return {
    codec: summary.codec || dash,
    resolution: formatResolution(summary.width, summary.height) ?? dash,
    fps: formatFps(summary.fps, lang) ?? dash,
    bitrate: formatBitrate(summary.kbps, lang),
  }
}
