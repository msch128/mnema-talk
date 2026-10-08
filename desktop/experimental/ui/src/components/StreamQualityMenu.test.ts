import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import StreamQualityMenu from './StreamQualityMenu.vue'
import type { ContextMenuItem } from './menuTypes'
import type { MediaStat } from '../lib/mediaStats'
const state = reactive({ open: false, x: 0, y: 0, anchor: null })
const stop = vi.fn()
const items = vi.fn((options: { onChangeSource: () => void }): ContextMenuItem[] => [{ id: 'source', label: 'Source', action: options.onChangeSource }])
const show = vi.fn((_event: unknown, factory: () => ContextMenuItem[]) => { factory(); state.open = true })
const start = vi.fn()
const stats = vi.fn((): MediaStat[] => [])
let statsAvailable = true
const capture = vi.fn((): MediaTrackSettings => ({ width: 1920 }))
let getStats: (() => unknown) | undefined
let getCapture: (() => unknown) | undefined
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ startScreenShare: start, getScreenSendStats: statsAvailable ? stats : undefined, getScreenCaptureSettings: capture }) }))
vi.mock('../composables/useNavMenus', () => ({ useMenuState: () => ({ state, items: { value: [] }, show }) }))
vi.mock('../composables/useStreamQuality', () => ({ useStreamQuality: (options: { getStats: () => unknown; getCaptureSettings: () => unknown }) => {
  getStats = options.getStats; getCapture = options.getCaptureSettings
  return { stopStats: stop, menuItems: items }
} }))
let wrapper: ReturnType<typeof mount<typeof StreamQualityMenu>> | undefined
beforeEach(() => { setActivePinia(createPinia()); state.open = false; statsAvailable = true; vi.clearAllMocks() })
afterEach(() => { wrapper?.unmount() })
describe('StreamQualityMenu', () => {
  it('opens anchored menu and keeps stage informed; pointerdown on open menu only closes', async () => {
    wrapper = mount(StreamQualityMenu, { global: { stubs: { ContextMenu: true } } })
    const button = wrapper.get('button')
    await button.trigger('pointerdown'); await button.trigger('click'); await flushPromises()
    expect(show).toHaveBeenCalledOnce(); expect(button.attributes('aria-expanded')).toBe('true')
    expect(wrapper.emitted('open-change')).toEqual([[true]])
    expect(getStats?.()).toEqual([]); expect(getCapture?.()).toEqual({ width: 1920 })
    const options = items.mock.calls[0]?.[0]
    if (!options) throw new Error('Menu factory not called')
    options.onChangeSource(); expect(start).toHaveBeenCalledOnce()
    await button.trigger('pointerdown'); state.open = false; await button.trigger('click')
    expect(show).toHaveBeenCalledOnce(); expect(stop).toHaveBeenCalledOnce()
    expect(wrapper.emitted('open-change')).toEqual([[true], [false]])
  })
  it('closes through click without preceding pointerdown and renders card controls', async () => {
    statsAvailable = false
    wrapper = mount(StreamQualityMenu, { props: { variant: 'card' }, global: { stubs: { ContextMenu: true } } })
    expect(getStats?.()).toBeNull()
    const button = wrapper.get('button'); expect(button.classes()).toContain('w-7')
    await button.trigger('click'); await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false'); expect(stop).toHaveBeenCalledOnce()
  })
})
