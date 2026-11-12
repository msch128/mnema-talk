// How many frames one epoch's key can seal before the random 96-bit nonce of
// frame.ts (one per frame, under one AES-128-GCM key that every sender of the
// epoch shares) gets near the limit of NIST SP 800-38D for random nonces: 2^32
// encryptions per key, which keeps the chance of two equal nonces below 2^-32.
//
// Frames per second of one member at its busiest: microphone and screen audio
// (20 ms Opus packets, 50 per second each), a camera at 60 fps and a screen
// share at 60 fps, each with every layer the code plans (cameraLayers.ts, screenLayers.ts).
// Members: far above what a Talk holds (the SFU forwards every sender to every
// viewer, so a Talk of 100 senders is already out of reach of one server link).
//
// How long one key lives: the committer advances the epoch once it is
// rotateMs +- jitter old. If the committer is silent (a hung tab) nobody
// rotates until the others take it for absent and a new committer commits, so
// the age bound is rotate + jitter + the absence limit + the patience.
import { describe, expect, it } from 'vitest'
import { planCameraLayers } from '../../talk/lib/cameraLayers'
import { planScreenLayers } from '../../talk/lib/screenLayers'
import { ABSENT_MS, DEFAULT_TIMING, MAINTENANCE_MS, ROTATE_JITTER_MS, ROTATE_MS } from './bounds'

const NIST_LIMIT = 2 ** 32
// Opus packets of 20 ms: 50 a second, for the microphone and for the sound of a screen share.
const AUDIO_FPS = 2 * 50
const sum = (plans: readonly { maxFramerate: number }[]) => plans.reduce((total, plan) => total + plan.maxFramerate, 0)
// A 1080p camera at 60 fps and a 4K screen at 60 fps, each with all the layers the code plans for them.
const CAMERA_FPS = sum(planCameraLayers({ width: 1920, height: 1080, frameRate: 60 } as MediaTrackSettings))
const SCREEN_FPS = sum(planScreenLayers({ fps: 60 }, { width: 3840, height: 2160, frameRate: 60 } as MediaTrackSettings))
const FRAMES_PER_SECOND_PER_MEMBER = AUDIO_FPS + CAMERA_FPS + SCREEN_FPS
const MEMBERS = 100

/** The longest a key can live: rotated late by its jitter, with a silent committer that is noticed after the absence limit and the patience. */
function longestKeyLifeSeconds(): number {
  return (ROTATE_MS + ROTATE_JITTER_MS + ABSENT_MS + 2 * MAINTENANCE_MS + MAINTENANCE_MS) / 1000
}

describe('the nonce bound of one epoch key', () => {
  it('uses the timing the product ships', () => {
    expect(DEFAULT_TIMING).toMatchObject({ rotateMs: ROTATE_MS, rotateJitterMs: ROTATE_JITTER_MS, absentMs: ABSENT_MS, maintenanceMs: MAINTENANCE_MS })
  })

  it('takes its frame rates from the layers the code plans', () => {
    // 100 audio, a camera of three layers (60, 30, 15) and a screen of three (60, 30, 30): a change of layers or rates changes this.
    expect(CAMERA_FPS).toBeGreaterThan(60)
    expect(SCREEN_FPS).toBeGreaterThan(60)
    expect(FRAMES_PER_SECOND_PER_MEMBER).toBeLessThan(340)
  })

  it('keeps a key under 2^27 frames in a room of 100 senders that all stream everything for the longest key life', () => {
    const frames = MEMBERS * FRAMES_PER_SECOND_PER_MEMBER * longestKeyLifeSeconds()
    // Under 2^26.7 frames: a margin of 40 to the limit, and a chance of a repeated nonce (n^2 / 2^97, birthday) below 2^-43.
    expect(Math.log2(frames ** 2) - 97).toBeLessThan(-43)
    expect(frames).toBeLessThan(2 ** 27)
    expect(NIST_LIMIT / frames).toBeGreaterThan(16)
  })

  it('names the room size where the margin of 16 is gone, so a change of timing shows what it costs', () => {
    const perMember = FRAMES_PER_SECOND_PER_MEMBER * longestKeyLifeSeconds()
    const members = Math.floor(NIST_LIMIT / 16 / perMember)
    expect(members).toBeGreaterThan(MEMBERS)
    expect(members).toBeLessThan(10_000)
  })
})
