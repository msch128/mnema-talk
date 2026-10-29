import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { FIT_SETTLE_MS, useScreenFit } from './useScreenFit'
import { useVoiceStore } from '../stores/voice'
import type { SubscribeRequest } from '../types/events'

const layers = [{ width: 3840, height: 2160, fps: 60 }, { width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }]

let height = 0
let ratio = 1
class FakeObserver {
  constructor(private cb: () => void) { observers.push(this) }
  observe() {}
  disconnect() {}
  fire() { this.cb() }
}
const observers: FakeObserver[] = []

function stage() {
  const voice = useVoiceStore()
  voice.setChannel('v1')
  const sent: SubscribeRequest[] = []
  voice.setSubscriptionSink(msg => { sent.push(msg) })
  voice.handleMediaState({ channel_id: 'v1', user_id: 'anna', screen: true, camera: false })
  voice.watchScreen('anna')
  voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
  sent.length = 0
  const el = ref<HTMLElement | null>(null)
  const wrapper = mount(defineComponent({
    setup() {
      const root = ref<HTMLElement | null>(null)
      el.value = null
      useScreenFit(() => 'anna', root)
      return () => h('div', { ref: root })
    }
  }), { attachTo: document.body })
  const root = wrapper.element as HTMLElement
  Object.defineProperty(root, 'clientHeight', { get: () => height, configurable: true })
  Object.defineProperty(root, 'clientWidth', { get: () => Math.round(height * 16 / 9), configurable: true })
  return { voice, sent, wrapper, resize: async () => { observers.forEach(o => o.fire()); await nextTick() } }
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
  observers.length = 0
  height = 0
  ratio = 1
  vi.stubGlobal('ResizeObserver', FakeObserver)
  vi.stubGlobal('devicePixelRatio', ratio)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('what the stage tells the server about its size', () => {
  it('asks for the layer a large tile and a small tile can use, counting the pixel ratio', async () => {
    const { sent, resize, wrapper } = stage()
    height = 360
    await wrapper.vm.$nextTick()
    await resize()
    await vi.advanceTimersByTimeAsync(0)
    expect(sent.at(-1)).toEqual({ kind: 'screen', user_id: 'anna', on: true, fit: 2 })

    // Enlarged to a 4K fullscreen: the source, after the size has settled.
    height = 2160
    await resize()
    expect(sent.at(-1)).toMatchObject({ fit: 2 })
    await vi.advanceTimersByTimeAsync(FIT_SETTLE_MS)
    expect(sent.at(-1)).toEqual({ kind: 'screen', user_id: 'anna', on: true, fit: 0 })

    // Fullscreen on a 1080p screen needs the 1080p layer only.
    height = 1080
    await resize()
    await vi.advanceTimersByTimeAsync(FIT_SETTLE_MS)
    expect(sent.at(-1)).toMatchObject({ fit: 1 })
  })

  it('counts device pixels: a 540 px tile on a 2x screen can use 1080p', async () => {
    vi.stubGlobal('devicePixelRatio', 2)
    const { sent, resize } = stage()
    height = 540
    await resize()
    await vi.advanceTimersByTimeAsync(0)
    expect(sent.at(-1)).toMatchObject({ fit: 1 })
  })

  it('sends one message for a window being dragged, and none when the layer stays', async () => {
    const { sent, resize } = stage()
    height = 700
    await resize()
    await vi.advanceTimersByTimeAsync(0)
    const before = sent.length
    for (const h of [710, 690, 705, 700]) {
      height = h
      await resize()
      await vi.advanceTimersByTimeAsync(FIT_SETTLE_MS / 4)
    }
    await vi.advanceTimersByTimeAsync(FIT_SETTLE_MS)
    expect(sent.length).toBe(before)
  })
})
