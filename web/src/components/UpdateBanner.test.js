import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import UpdateBanner from './UpdateBanner.vue'
import { useAppVersionStore } from '../stores/appVersion'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { setLocale } from '../i18n'

const realLocation = window.location

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
})
afterEach(() => {
  Object.defineProperty(window, 'location', { configurable: true, value: realLocation })
})

describe('UpdateBanner', () => {
  it('stays hidden while the server runs the same build, or a dev build', async () => {
    const store = useAppVersionStore()
    const w = mount(UpdateBanner)
    store.setServerVersion(store.clientVersion)
    await nextTick()
    expect(w.find('[data-testid="update-banner"]').exists()).toBe(false)
    store.setServerVersion('dev')
    await nextTick()
    expect(w.find('[data-testid="update-banner"]').exists()).toBe(false)
  })

  it('offers a reload on a new server version and only reloads on click', async () => {
    const reload = vi.fn()
    Object.defineProperty(window, 'location', { configurable: true, value: { ...realLocation, reload } })
    const w = mount(UpdateBanner)
    // The version arrives with the WebSocket's server_info event.
    useChatStore().handleWSEvent({ type: 'server_info', payload: { version: '99.0.0' } })
    await nextTick()
    const banner = w.find('[data-testid="update-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('Eine neue Version ist verfügbar – hier klicken zum Neuladen')
    expect(w.find('[data-testid="update-call-note"]').exists()).toBe(false)
    expect(reload).not.toHaveBeenCalled()
    await w.find('[data-testid="update-reload"]').trigger('click')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('tells callers that the call reconnects after the reload', async () => {
    useVoiceStore().currentChannelId = 'c1'
    useAppVersionStore().setServerVersion('99.0.0')
    const w = mount(UpdateBanner)
    await nextTick()
    expect(w.find('[data-testid="update-call-note"]').text()).toContain('automatisch wieder verbunden')
  })

  it('can be dismissed until the next version', async () => {
    const store = useAppVersionStore()
    store.setServerVersion('99.0.0')
    const w = mount(UpdateBanner)
    await nextTick()
    await w.find('[data-testid="update-dismiss"]').trigger('click')
    expect(w.find('[data-testid="update-banner"]').exists()).toBe(false)
    store.setServerVersion('99.0.1')
    await nextTick()
    expect(w.find('[data-testid="update-banner"]').exists()).toBe(true)
  })
})
