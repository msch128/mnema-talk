import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import ConnectionBanner from './ConnectionBanner.vue'
import { useChatStore } from '../stores/chat'
import { setLocale } from '../i18n'

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('ConnectionBanner', () => {
  it('is hidden before the first connection and while connected', async () => {
    const chat = useChatStore()
    const w = mount(ConnectionBanner)
    expect(w.find('[data-testid="connection-banner"]').exists()).toBe(false)
    chat.isConnected = true
    chat.wasConnected = true
    await nextTick()
    expect(w.find('[data-testid="connection-banner"]').exists()).toBe(false)
  })

  it('shows the lost banner with a retry button after a drop, and hides on reconnect', async () => {
    const chat = useChatStore()
    chat.isConnected = true
    chat.wasConnected = true
    const w = mount(ConnectionBanner)
    chat.isConnected = false
    chat.reconnectAttempt = 3
    chat.nextRetryAt = Date.now() + 4000
    await nextTick()
    const banner = w.find('[data-testid="connection-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.attributes('role')).toBe('status')
    expect(banner.text()).toContain('Verbindung getrennt – verbinde neu …')
    expect(banner.text()).toContain('nächster Versuch in 4 s · Versuch 3')

    const retry = vi.spyOn(chat, 'retryNow').mockImplementation(() => {})
    await w.find('[data-testid="connection-retry"]').trigger('click')
    expect(retry).toHaveBeenCalledTimes(1)

    // countdown ticks
    vi.advanceTimersByTime(2000)
    await nextTick()
    expect(w.find('[data-testid="connection-banner"]').text()).toContain('in 2 s')

    chat.isConnected = true
    await nextTick()
    expect(w.find('[data-testid="connection-banner"]').exists()).toBe(false)
    expect(w.find('[data-testid="connection-restored"]').text()).toContain('Wieder verbunden')
    vi.advanceTimersByTime(2100)
    await nextTick()
    expect(w.find('[data-testid="connection-restored"]').exists()).toBe(false)
  })

  it('speaks English when the locale is English', async () => {
    setLocale('en')
    const chat = useChatStore()
    chat.wasConnected = true
    const w = mount(ConnectionBanner)
    await nextTick()
    expect(w.text()).toContain('Connection lost – reconnecting …')
    expect(w.text()).toContain('Retry now')
  })
})
