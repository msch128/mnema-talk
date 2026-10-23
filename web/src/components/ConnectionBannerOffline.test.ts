import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import ConnectionBanner from './ConnectionBanner.vue'
import { useChatStore } from '../stores/chat'
import { setLocale } from '../i18n'

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  setLocale('de')
})

describe('ConnectionBanner and the browser offline event', () => {
  it('shows at once when the browser goes offline although the socket still looks open, and hides when it is back', async () => {
    const chat = useChatStore()
    chat.isConnected = true
    chat.wasConnected = true
    const w = mount(ConnectionBanner)
    expect(w.find('[data-testid="connection-banner"]').exists()).toBe(false)

    window.dispatchEvent(new Event('offline'))
    await nextTick()
    const banner = w.find('[data-testid="connection-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('You are offline')

    window.dispatchEvent(new Event('online'))
    await nextTick()
    expect(w.find('[data-testid="connection-banner"]').exists()).toBe(false)
    expect(w.find('[data-testid="connection-restored"]').exists()).toBe(true)
    w.unmount()
  })

  it('shows before the first connection was ever made, and stops listening when unmounted', async () => {
    const w = mount(ConnectionBanner)
    window.dispatchEvent(new Event('offline'))
    await nextTick()
    expect(w.find('[data-testid="connection-banner"]').exists()).toBe(true)
    const removed = vi.spyOn(window, 'removeEventListener')
    w.unmount()
    expect(removed.mock.calls.map(c => c[0])).toEqual(expect.arrayContaining(['offline', 'online']))
    removed.mockRestore()
    window.dispatchEvent(new Event('online'))
  })

  it('keeps the plain wording for a dropped socket while the browser is online', async () => {
    const chat = useChatStore()
    chat.isConnected = true
    chat.wasConnected = true
    const w = mount(ConnectionBanner)
    chat.isConnected = false
    await nextTick()
    expect(w.find('[data-testid="connection-banner"]').text()).toContain('Connection lost')
    w.unmount()
  })
})
