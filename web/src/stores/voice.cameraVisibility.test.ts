import { streamFixture } from '../store-test-support.fixture'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { useVoiceStore } from './voice'
import { CAMERA_BATCH } from './voice/cameraVisibility'
import type { SubscribeRequest } from '../types/events'

let sent: SubscribeRequest[]
const CAMERA_ON = { channel_id: 'room', screen: false, camera: true }

function setup(withCameras: string[]) {
  const voice = useVoiceStore()
  voice.setSubscriptionSink(request => { sent.push(request) })
  voice.setChannel('room')
  for (const id of withCameras) voice.mediaState[id] = CAMERA_ON
  return voice
}

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  vi.useFakeTimers()
  sent = []
})
afterEach(() => vi.useRealTimers())

describe('receiving only the cameras that are on screen', () => {
  it('receives every camera until the view reports', () => {
    const voice = setup(['a', 'b'])
    vi.advanceTimersByTime(5000)
    expect(sent).toEqual([])
    expect(voice.isCameraOffscreen('a')).toBe(false)
  })

  it('switches off the cameras that are not on screen, together, after a short wait', () => {
    const voice = setup(['a', 'b', 'c'])
    voice.setVisibleCameras(['b'])
    vi.advanceTimersByTime(100)
    expect(sent).toEqual([])
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([{ kind: 'camera', user_ids: ['a', 'c'], on: false }])
    expect(voice.isCameraOffscreen('a')).toBe(true)
    expect(voice.isCameraOffscreen('b')).toBe(false)
    // The picture of a camera that is gone from the screen is dropped at once.
    expect(voice.userVideoStreams['a']).toBeUndefined()
  })

  it('names a single camera the old way', () => {
    const voice = setup(['a', 'b'])
    voice.setVisibleCameras(['b'])
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'a', on: false }])
  })

  it('asks for a camera again soon after its tile comes back, and only what changed', () => {
    const voice = setup(['a', 'b'])
    voice.setVisibleCameras([])
    vi.advanceTimersByTime(1000)
    sent.length = 0
    voice.setVisibleCameras(['a'])
    vi.advanceTimersByTime(150)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'a', on: true }])
    voice.setVisibleCameras(['a'])
    vi.advanceTimersByTime(1000)
    expect(sent).toHaveLength(1)
  })

  it('collects a burst of changes into the last state', () => {
    const voice = setup(['a', 'b'])
    voice.setVisibleCameras(['a'])
    vi.advanceTimersByTime(300)
    voice.setVisibleCameras(['b'])
    vi.advanceTimersByTime(300)
    voice.setVisibleCameras(['a', 'b'])
    vi.advanceTimersByTime(2000)
    expect(sent).toEqual([])
  })

  it('splits a big change into events of at most 16 cameras', () => {
    const ids = Array.from({ length: CAMERA_BATCH * 2 + 3 }, (_, i) => `u${i}`)
    const voice = setup(ids)
    voice.setVisibleCameras([])
    vi.advanceTimersByTime(1000)
    expect(sent.map(r => ('user_ids' in r ? r.user_ids?.length : 1))).toEqual([16, 16, 3])
  })

  it('keeps the camera on the stage, and a camera that starts later, in view or off as the tiles say', async () => {
    const voice = setup(['a', 'b'])
    voice.userVideoStreams = { a: streamFixture() }
    voice.focusedCamera = 'a'
    voice.setVisibleCameras([])
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'b', on: false }])
    sent.length = 0
    voice.mediaState['c'] = CAMERA_ON
    await nextTick()
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'c', on: false }])
    sent.length = 0
    voice.focusedCamera = null
    await nextTick()
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'a', on: false }])
  })

  it('does not ask again for a camera the member hid, and keeps an off-screen one off when they stop hiding it', () => {
    const voice = setup(['a', 'b'])
    voice.setCameraHidden('a', true)
    sent.length = 0
    voice.setVisibleCameras([])
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'b', on: false }])
    sent.length = 0
    voice.setCameraHidden('a', false)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'a', on: false }])
    sent.length = 0
    voice.setVisibleCameras(['a', 'b'])
    vi.advanceTimersByTime(200)
    expect(sent).toEqual([{ kind: 'camera', user_ids: ['a', 'b'], on: true }])
  })

  it('a hidden camera that comes on screen is not asked for', () => {
    const voice = setup(['a'])
    voice.setVisibleCameras([])
    vi.advanceTimersByTime(1000)
    voice.setCameraHidden('a', true)
    sent.length = 0
    voice.setVisibleCameras(['a'])
    vi.advanceTimersByTime(300)
    expect(sent).toEqual([])
    expect(voice.isCameraHidden('a')).toBe(true)
  })

  it('asks for everything again after the server forgot (a reconnect)', () => {
    const voice = setup(['a', 'b'])
    voice.setVisibleCameras(['a'])
    vi.advanceTimersByTime(1000)
    sent.length = 0
    voice.resendSubscriptions()
    voice.resetCameraVisibility()
    expect(voice.isCameraOffscreen('b')).toBe(false)
    vi.advanceTimersByTime(1000)
    expect(sent.at(-1)).toEqual({ kind: 'camera', user_id: 'b', on: false })
  })

  it('stops reporting: nothing is held back for a view that does not tell', async () => {
    const voice = setup(['a'])
    voice.setVisibleCameras(['a'])
    voice.setVisibleCameras(null)
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([])
    voice.mediaState['b'] = CAMERA_ON
    await nextTick()
    vi.advanceTimersByTime(1000)
    expect(sent).toEqual([])
  })
})

describe('cameras the server does not forward', () => {
  it('shows who is capped, only for the call I am in, and not for what I chose myself', () => {
    const voice = setup(['a', 'b'])
    voice.userVideoStreams = { a: streamFixture() }
    voice.handleCamerasCapped({ channel_id: 'elsewhere', user_ids: ['a'] })
    expect(voice.isCameraCapped('a')).toBe(false)
    voice.handleCamerasCapped({ channel_id: 'room', user_ids: ['a', 'b'] })
    expect(voice.isCameraCapped('a')).toBe(true)
    expect(voice.userVideoStreams['a']).toBeUndefined()
    voice.setCameraHidden('b', true)
    expect(voice.isCameraCapped('b')).toBe(false)
    voice.handleCamerasCapped({ channel_id: 'room', user_ids: [] })
    expect(voice.isCameraCapped('a')).toBe(false)
    voice.handleCamerasCapped(undefined as never)
    expect(voice.isCameraCapped(null)).toBe(false)
  })

  it('forgets who was capped when the room changes', () => {
    const voice = setup(['a'])
    voice.handleCamerasCapped({ channel_id: 'room', user_ids: ['a'] })
    voice.resetRemoteMedia()
    expect(voice.isCameraCapped('a')).toBe(false)
  })
})
