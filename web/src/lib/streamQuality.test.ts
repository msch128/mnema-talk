import { describe, it, expect } from 'vitest'
import {
  normalizeQuality, streamModeOf, trackConstraints, streamBitrate, streamEncoding, streamTuning,
  summarizeSendStats, formatSendStats, DEFAULT_STREAM_QUALITY, STREAM_PRESETS, STREAM_MIN_BITRATE,
  STREAM_MAX_BITRATE, SCREEN_QUALITY_STORAGE_KEY, formatStreamDiagnostics
} from './streamQuality'

import type { StreamResolution, StreamFrameRate } from './streamQuality'
import { present } from '../media-test.fixture'

const UHD = { width: 3840, height: 2160 }

describe('stream modes', () => {
  it('a new stream is 1080p at 30 fps, which is no preset', () => {
    expect(DEFAULT_STREAM_QUALITY).toEqual({ resolution: 1080, fps: 30 })
    expect(streamModeOf(DEFAULT_STREAM_QUALITY)).toBe('custom')
  })

  it('maps the presets', () => {
    expect(STREAM_PRESETS.gaming).toEqual({ resolution: 1440, fps: 60 })
    expect(STREAM_PRESETS.screen).toEqual({ resolution: 'source', fps: 15 })
    expect(streamModeOf({ resolution: 1440, fps: 60 })).toBe('gaming')
    expect(streamModeOf({ resolution: 'source', fps: 15 })).toBe('screen')
  })

  it('anything else is custom, also a preset picked as custom on purpose', () => {
    expect(streamModeOf({ resolution: 1440, fps: 30 })).toBe('custom')
    expect(streamModeOf({ resolution: 'source', fps: 60 })).toBe('custom')
    expect(streamModeOf({ resolution: 720, fps: 15 })).toBe('custom')
    expect(streamModeOf({ resolution: 1440, fps: 60, custom: true })).toBe('custom')
  })

  it('normalizes unknown values to the default', () => {
    expect(normalizeQuality({ resolution: 2160, fps: 120 })).toEqual(DEFAULT_STREAM_QUALITY)
    expect(normalizeQuality(null)).toEqual(DEFAULT_STREAM_QUALITY)
    expect(normalizeQuality({ resolution: 720, fps: 60, custom: true })).toEqual({ resolution: 720, fps: 60 })
  })
})

describe('track constraints', () => {
  it('caps the height (the width follows the aspect ratio) and the frame rate', () => {
    expect(trackConstraints({ resolution: 720, fps: 15 })).toEqual({ frameRate: { ideal: 15, max: 15 }, height: { max: 720 } })
    expect(trackConstraints({ resolution: 1080, fps: 30 })).toEqual({ frameRate: { ideal: 30, max: 30 }, height: { max: 1080 } })
    expect(trackConstraints({ resolution: 1440, fps: 60 })).toEqual({ frameRate: { ideal: 60, max: 60 }, height: { max: 1440 } })
  })

  it('source sets no size at all', () => {
    expect(trackConstraints({ resolution: 'source', fps: 15 })).toEqual({ frameRate: { ideal: 15, max: 15 } })
  })
})

describe('encoding parameters', () => {
  it('grows the bitrate with size and frame rate, within the bounds', () => {
    const at = (resolution: StreamResolution, fps: StreamFrameRate) => streamBitrate({ resolution, fps }, UHD)
    expect(at(1080, 30)).toBe(5_200_000)
    expect(at(720, 30)).toBeLessThan(at(1080, 30))
    expect(at(1080, 30)).toBeLessThan(at(1440, 30))
    expect(at(1080, 15)).toBeLessThan(at(1080, 30))
    expect(at(1080, 30)).toBeLessThan(at(1080, 60))
    expect(at(720, 15)).toBeGreaterThanOrEqual(STREAM_MIN_BITRATE)
    expect(at(1440, 60)).toBe(STREAM_MAX_BITRATE)
    // Source at 4K is held at the ceiling.
    expect(streamBitrate({ resolution: 'source', fps: 30 }, UHD)).toBe(STREAM_MAX_BITRATE)
    // A small screen sends less than its cap.
    expect(streamBitrate({ resolution: 'source', fps: 15 }, { width: 1280, height: 720 })).toBe(at(720, 15))
    // Without settings: 16:9 at the chosen height, the ceiling for source.
    expect(streamBitrate({ resolution: 1080, fps: 30 })).toBe(at(1080, 30))
    expect(streamBitrate({ resolution: 'source', fps: 30 })).toBe(STREAM_MAX_BITRATE)
  })

  it('caps the frame rate and scales in the encoder only where the capture is still too tall', () => {
    expect(streamEncoding({ resolution: 720, fps: 15 }, UHD)).toEqual({ maxBitrate: streamBitrate({ resolution: 720, fps: 15 }, UHD), maxFramerate: 15, scaleResolutionDownBy: 3 })
    expect(streamEncoding({ resolution: 1080, fps: 30 }, UHD).scaleResolutionDownBy).toBe(2)
    expect(streamEncoding({ resolution: 1440, fps: 60 }, UHD).scaleResolutionDownBy).toBe(1.5)
    expect(streamEncoding({ resolution: 1080, fps: 30 }, { width: 1920, height: 1080 }).scaleResolutionDownBy).toBe(1)
    expect(streamEncoding({ resolution: 1440, fps: 60 }, { width: 1920, height: 1080 }).scaleResolutionDownBy).toBe(1)
    expect(streamEncoding({ resolution: 'source', fps: 60 }, UHD)).toMatchObject({ maxFramerate: 60, scaleResolutionDownBy: 1 })
    expect(streamEncoding({ resolution: 720, fps: 30 }, {}).scaleResolutionDownBy).toBe(1)
  })

  it('60 fps is encoded as motion, the rest keeps text sharp', () => {
    expect(streamTuning({ resolution: 1440, fps: 60 })).toEqual({ contentHint: 'motion', degradationPreference: 'balanced' })
    expect(streamTuning({ resolution: 'source', fps: 15 })).toEqual({ contentHint: 'detail', degradationPreference: 'maintain-resolution' })
    expect(streamTuning(DEFAULT_STREAM_QUALITY)).toEqual({ contentHint: 'detail', degradationPreference: 'maintain-resolution' })
  })
})

describe('send statistics', () => {
  const report = (bytesSent: number, timestamp: number) => [
    { id: 'c1', type: 'codec', mimeType: 'video/VP8' },
    { id: 'o1', type: 'outbound-rtp', kind: 'video', codecId: 'c1', bytesSent, timestamp, frameWidth: 1280, frameHeight: 720, framesPerSecond: 14.6 }
  ]

  it('summarizes codec, size, frame rate and the bitrate between samples', () => {
    const first = summarizeSendStats(report(1_000_000, 1000))
    expect(first).toMatchObject({ codec: 'VP8', width: 1280, height: 720, fps: 15, kbps: null })
    const second = summarizeSendStats(report(1_250_000, 2000), present(first).sample)
    expect(present(second).kbps).toBe(2000)
    expect(summarizeSendStats([])).toBeNull()
    expect(summarizeSendStats(null)).toBeNull()
  })

  it('formats for display, with dashes for what is not known yet', () => {
    expect(formatSendStats(null)).toEqual({ codec: '–', resolution: '–', fps: '–', bitrate: '–' })
    const s = { ...present(summarizeSendStats(report(0, 0))), codec: 'H264', width: 1920, height: 1080, fps: 30, kbps: 4500 }
    expect(formatSendStats(s, 'de')).toEqual({ codec: 'H264', resolution: '1920×1080', fps: '30 fps', bitrate: '4,5 Mbit/s' })
    expect(formatSendStats(s, 'en').bitrate).toBe('4.5 Mbit/s')
    expect(formatSendStats({ ...s, kbps: 800 }, 'en').bitrate).toBe('800 kbit/s')
    expect(formatSendStats({ ...s, kbps: null }, 'en').bitrate).toBe('–')
  })

  it('distinguishes capture cadence from encoded cadence and reports encoder limitations', () => {
    const stats = [
      { id: 'c', type: 'codec', mimeType: 'video/H264', sdpFmtpLine: 'packetization-mode=1;profile-level-id=42e01f;level-asymmetry-allowed=1' },
      { id: 'capture', type: 'media-source', kind: 'video', framesPerSecond: 59.7 },
      { id: 'other', type: 'media-source', kind: 'video', framesPerSecond: 15 },
      { id: 'video', type: 'outbound-rtp', kind: 'video', mediaSourceId: 'capture', codecId: 'c', framesPerSecond: 20.4,
        framesEncoded: 100, totalEncodeTime: 2, encoderImplementation: 'ExternalEncoder', powerEfficientEncoder: true, qualityLimitationReason: 'cpu' }
    ]
    const summary = summarizeSendStats(stats)
    expect(summary).toMatchObject({ captureFps: 60, fps: 20, encoder: 'ExternalEncoder', efficientEncoder: true, limitation: 'cpu', encodeMs: 20 })
    expect(formatStreamDiagnostics(summary, { frameRate: 60 }, 'de')).toMatchObject({ captureSetting: '60 fps', captureFps: '60 fps', encoder: 'ExternalEncoder', encodeTime: '20 ms', codecProfile: '42e01f' })
  })

  it('uses matching stream counter deltas when a browser omits framesPerSecond', () => {
    const first = summarizeSendStats([{ id: 'video', type: 'outbound-rtp', kind: 'video', timestamp: 1000, framesEncoded: 100, totalEncodeTime: 1 }])
    const second = summarizeSendStats([{ id: 'video', type: 'outbound-rtp', kind: 'video', timestamp: 2000, framesEncoded: 160, totalEncodeTime: 1.6 }], present(first).sample)
    expect(present(second).fps).toBe(60)
    expect(present(second).encodeMs).toBeCloseTo(10)
    const replacement = summarizeSendStats([{ id: 'new-video', type: 'outbound-rtp', kind: 'video', timestamp: 3000, framesEncoded: 5, totalEncodeTime: 0.1 }], present(second).sample)
    expect(present(replacement).fps).toBeNull()
    expect(present(replacement).kbps).toBeNull()
  })

  it('keeps unavailable diagnosis unknown and preserves a measured zero rate', () => {
    const missing = summarizeSendStats([{ id: 'video', type: 'outbound-rtp', kind: 'video' }])
    expect(missing).toMatchObject({ fps: null, captureFps: null, efficientEncoder: null, limitation: null, encodeMs: null })
    expect(formatStreamDiagnostics(missing)).toEqual({ captureSetting: '–', captureFps: '–', encoder: '–', encodeTime: '–', codecProfile: '–' })
    expect(present(summarizeSendStats([{ id: 'video', type: 'outbound-rtp', kind: 'video', framesPerSecond: 0 }])).fps).toBe(0)
  })
})

describe('storage key', () => {
  it('SCREEN_QUALITY_STORAGE_KEY is the shared localStorage key', () => {
    // Regression guard: one key across voice store and ScreenShareModal
    expect(SCREEN_QUALITY_STORAGE_KEY).toBe('mnema_screen_quality')
  })
})

describe('multiple encodings and reset statistics', () => {
  it('chooses the active outbound encoding by sent bytes, including mediaType-only browsers', () => {
    const s = summarizeSendStats([
      { id: 'small', type: 'outbound-rtp', kind: 'video', bytesSent: 1 },
      { id: 'empty', type: 'outbound-rtp', kind: 'video' },
      { id: 'active', type: 'outbound-rtp', mediaType: 'video', bytesSent: 100, timestamp: 2000, framesEncoded: 50, totalEncodeTime: 0.5 }
    ], { bytes: 0, ts: 1000, frames: 0, encodeTime: 0 })
    expect(s?.sample.id).toBe('active')
    expect(s?.kbps).toBe(1)
    expect(s?.fps).toBe(50)
    expect(s?.encodeMs).toBe(10)
  })
  it('does not compute deltas from counters moving backwards or nonfinite measurements', () => {
    const previous = { id: 'o', bytes: 1000, ts: 1000, frames: 10, encodeTime: 1 }
    const stat = { id: 'o', type: 'outbound-rtp', kind: 'video', timestamp: 2000, bytesSent: 0, framesEncoded: 5, totalEncodeTime: 0.1, framesPerSecond: NaN }
    expect(summarizeSendStats([stat], previous)).toMatchObject({ kbps: null, fps: null, encodeMs: null })
    expect(summarizeSendStats([{ ...stat, framesEncoded: 10 }], previous)).toMatchObject({ fps: 0, encodeMs: null })
    expect(summarizeSendStats([{ ...stat, framesEncoded: 11, totalEncodeTime: NaN }], previous)?.encodeMs).toBeNull()
  })
})
