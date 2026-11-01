import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useCameraFit } from './useCameraFit'
import { useVoiceStore } from '../stores/voice'
import { CAMERA_FIT_SETTLE_MS } from '../stores/voice/cameraFit'
import type { SubscribeRequest } from '../types/events'

class FakeObserver {
  constructor(private cb: () => void) { observers.push(this) }
  observe() {}
  disconnect() {}
  fire() { this.cb() }
}
const observers: FakeObserver[] = []

function show(userId: string | null, size: { width: number; height: number }, video?: { w: number; h: number }) {
  const voice = useVoiceStore()
  voice.serverCameraLayers = 3
  voice.setChannel('v1')
  const sent: SubscribeRequest[] = []
  voice.setSubscriptionSink(msg => { sent.push(msg) })
  const who = ref(userId)
  const wrapper = mount(defineComponent({
    setup() {
      const el = ref<HTMLElement | null>(null)
      useCameraFit(() => who.value, el, 'tile')
      return () => h('video', { ref: el })
    }
  }), { attachTo: document.body })
  const el = wrapper.element as HTMLVideoElement
  Object.defineProperty(el, 'clientWidth', { get: () => size.width, configurable: true })
  Object.defineProperty(el, 'clientHeight', { get: () => size.height, configurable: true })
  if (video) {
    Object.defineProperty(el, 'videoWidth', { get: () => video.w, configurable: true })
    Object.defineProperty(el, 'videoHeight', { get: () => video.h, configurable: true })
  }
  return { voice, sent, who, wrapper, resize: async () => { observers.forEach(o => o.fire()); await nextTick() } }
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
  observers.length = 0
  vi.stubGlobal('ResizeObserver', FakeObserver)
  vi.stubGlobal('devicePixelRatio', 2)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('the size of my picture of a camera', () => {
  it('reports the picture in device pixels, as tall as the tile and its width allow', async () => {
    const { sent, resize } = show('anna', { width: 176, height: 99 })
    await resize()
    vi.advanceTimersByTime(0)
    // 176 px wide shows a 16:9 picture 99 px tall; at a pixel ratio of 2 that is 198 device pixels.
    expect(sent).toEqual([{ kind: 'camera', user_id: 'anna', on: true, fit_px: 198 }])
  })

  it('uses the shape of the video: a tall box around a wide picture shows less than its height', async () => {
    const { sent, resize } = show('anna', { width: 640, height: 640 }, { w: 1280, h: 720 })
    await resize()
    vi.advanceTimersByTime(0)
    expect(sent.at(-1)).toMatchObject({ fit_px: 720 })
  })

  it('says nothing about nobody, and takes the picture back when the person goes or the tile does', async () => {
    const { sent, who, wrapper, resize, voice } = show(null, { width: 176, height: 99 })
    await resize()
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent).toEqual([])

    who.value = 'anna'
    await nextTick()
    vi.advanceTimersByTime(0)
    expect(sent.at(-1)).toMatchObject({ user_id: 'anna', fit_px: 198 })

    who.value = 'bob'
    await nextTick()
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent.map(m => [m.user_id, m.fit_px])).toEqual([['anna', 198], ['bob', 198], ['anna', 0]])

    wrapper.unmount()
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent.at(-1)).toEqual({ kind: 'camera', user_id: 'bob', on: true, fit_px: 0 })
    expect(voice.cameraSizes).toEqual({})
  })

  it('follows the pixel ratio when the window moves to another screen', async () => {
    const { sent, resize } = show('anna', { width: 176, height: 99 })
    await resize()
    vi.advanceTimersByTime(0)
    vi.stubGlobal('devicePixelRatio', 1)
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    expect(sent.at(-1)).toMatchObject({ fit_px: 99 })
  })
})

describe('how a picture sits in its element', () => {
  function tile(fit: 'cover' | 'contain', size: { width: number; height: number }, video: { w: number; h: number }) {
    const voice = useVoiceStore()
    voice.serverCameraLayers = 3
    voice.setChannel('v1')
    const sent: SubscribeRequest[] = []
    voice.setSubscriptionSink(msg => { sent.push(msg) })
    const wrapper = mount(defineComponent({
      setup() {
        const el = ref<HTMLElement | null>(null)
        useCameraFit(() => 'anna', el, 'tile', fit)
        return () => h('video', { ref: el })
      }
    }), { attachTo: document.body })
    const el = wrapper.element as HTMLVideoElement
    Object.defineProperty(el, 'clientWidth', { get: () => size.width, configurable: true })
    Object.defineProperty(el, 'clientHeight', { get: () => size.height, configurable: true })
    Object.defineProperty(el, 'videoWidth', { get: () => video.w, configurable: true })
    Object.defineProperty(el, 'videoHeight', { get: () => video.h, configurable: true })
    return { sent, el, resize: async () => { observers.forEach(o => o.fire()); await nextTick() } }
  }

  it('counts a square tile that covers by its height: the picture is scaled up until it fills the tile', async () => {
    // 180x180 css px at a pixel ratio of 2: a 16:9 picture covering it is 180 css px tall (and cropped at the sides).
    const covering = tile('cover', { width: 180, height: 180 }, { w: 1280, h: 720 })
    await covering.resize()
    vi.advanceTimersByTime(0)
    expect(covering.sent.at(-1)).toMatchObject({ fit_px: 360 })
  })

  it('counts a portrait tile of a wide picture by its height, not by its width', async () => {
    const portrait = tile('cover', { width: 100, height: 300 }, { w: 1280, h: 720 })
    await portrait.resize()
    vi.advanceTimersByTime(0)
    expect(portrait.sent.at(-1)).toMatchObject({ fit_px: 600 })
  })

  it('counts a stage that contains the picture by what shows', async () => {
    const contained = tile('contain', { width: 180, height: 180 }, { w: 1280, h: 720 })
    await contained.resize()
    vi.advanceTimersByTime(0)
    // The picture is 180 css px wide and 101 tall: 202 device pixels.
    expect(contained.sent.at(-1)).toMatchObject({ fit_px: 203 })
  })

  it('reports again once the video says what shape it is', async () => {
    const t = tile('cover', { width: 200, height: 100 }, { w: 0, h: 0 })
    await t.resize()
    vi.advanceTimersByTime(0)
    // Unknown shape: 16:9 is assumed (200 px wide is 112 tall, more than the 100 of the element).
    expect(t.sent.at(-1)).toMatchObject({ fit_px: 225 })
    Object.defineProperty(t.el, 'videoWidth', { get: () => 640, configurable: true })
    Object.defineProperty(t.el, 'videoHeight', { get: () => 640, configurable: true })
    t.el.dispatchEvent(new Event('loadedmetadata'))
    await nextTick()
    vi.advanceTimersByTime(CAMERA_FIT_SETTLE_MS)
    // A square picture covering 200 px of width is 200 px tall: 400 device pixels.
    expect(t.sent.at(-1)).toMatchObject({ fit_px: 400 })
  })
})
