import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useVoiceStore } from '../voice'
import { CAMERA_FIT_SETTLE_MS } from './cameraFit'
import type { SubscribeRequest } from '../../types/events'

function setup() {
  const voice = useVoiceStore()
  voice.serverCameraLayers = 3
  voice.setChannel('v1')
  const sent: SubscribeRequest[] = []
  voice.setSubscriptionSink(msg => { sent.push(msg) })
  return { voice, sent }
}

beforeEach(() => { setActivePinia(createPinia()); vi.useFakeTimers() })
afterEach(() => vi.useRealTimers())

describe('what my pictures of cameras tell the server', () => {
  it('reports the first size at once and later ones after they settle', () => {
    const { voice, sent } = setup()
    voice.setCameraSize('anna', 'tile', 180)
    vi.advanceTimersByTime(0)
    expect(sent).toEqual([{ kind: 'camera', user_id: 'anna', on: true, fit_px: 180 }])

    voice.setCameraSize('anna', 'tile', 200)
    voice.setCameraSize('anna', 'tile', 220)
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS - 1)
    expect(sent).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(sent.at(-1)).toEqual({ kind: 'camera', user_id: 'anna', on: true, fit_px: 220 })
  })

  it('counts the largest picture of a person, and says nothing when nothing changed', () => {
    const { voice, sent } = setup()
    voice.setCameraSize('anna', 'tile', 180)
    voice.setCameraSize('anna', 'stage', 700)
    vi.advanceTimersByTime(0)
    expect(sent.at(-1)).toMatchObject({ fit_px: 700 })
    const count = sent.length

    // A smaller picture beside the large one does not change what is needed.
    voice.setCameraSize('anna', 'tile', 150)
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent).toHaveLength(count)

    // The stage goes away: the tile is what is left.
    voice.setCameraSize('anna', 'stage', 0)
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent.at(-1)).toMatchObject({ fit_px: 150 })
  })

  it('says 0 once the last picture of a person is gone, and nothing about people never shown', () => {
    const { voice, sent } = setup()
    voice.setCameraSize('anna', 'tile', 180)
    vi.advanceTimersByTime(0)
    voice.setCameraSize('anna', 'tile', 0)
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent.at(-1)).toEqual({ kind: 'camera', user_id: 'anna', on: true, fit_px: 0 })
    expect(voice.cameraSizes).toEqual({})

    sent.length = 0
    voice.setCameraSize('bob', 'tile', 0)
    voice.setCameraSize(null, 'tile', 100)
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent).toEqual([])
  })

  it('says it all again after a reconnect, and forgets it when the call is left', () => {
    const { voice, sent } = setup()
    voice.setCameraSize('anna', 'tile', 180)
    voice.setCameraSize('bob', 'stage', 700)
    vi.advanceTimersByTime(0)
    sent.length = 0

    voice.resendSubscriptions()
    expect(sent.filter(m => m.kind === 'camera' && m.fit_px !== undefined).map(m => [m.user_id, m.fit_px])).toEqual([['anna', 180], ['bob', 700]])

    voice.disconnect()
    expect(voice.cameraSizes).toEqual({})
    sent.length = 0
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent).toEqual([])
  })

  it('says nothing to a server that does not take camera layers, until it does', () => {
    const { voice, sent } = setup()
    voice.serverCameraLayers = 0
    voice.setCameraSize('anna', 'tile', 180)
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent).toEqual([])
    // Once the server announces it (a new connection), what is shown is said.
    voice.serverCameraLayers = 3
    voice.resendSubscriptions()
    expect(sent.filter(m => m.fit_px !== undefined)).toEqual([{ kind: 'camera', user_id: 'anna', on: true, fit_px: 180 }])
  })
})
