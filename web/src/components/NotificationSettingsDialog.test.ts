import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { setLocale } from '../i18n'
import NotificationSettingsDialog from './NotificationSettingsDialog.vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'

const NOW = new Date('2026-01-01T12:00:00Z')
let w: VueWrapper

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
  setLocale('en')
  setActivePinia(createPinia())
})
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
  vi.useRealTimers()
})

function open(server: { level: 'all' | 'mentions' | 'mute'; muted_until?: string | null; suppress_everyone?: boolean } = { level: 'all' }) {
  const chat = useChatStore()
  chat.notifySettings = {
    channels: [], categories: [],
    server: { level: server.level, muted_until: server.muted_until ?? null, suppress_everyone: server.suppress_everyone ?? false }
  }
  const set = vi.spyOn(chat, 'setServerNotification').mockResolvedValue(true)
  w = mount(NotificationSettingsDialog, { attachTo: document.body })
  return set
}

const byId = (id: string) => w.find(`[data-testid="${id}"]`)

describe('server notification settings', () => {
  it('ticks the current level and saves another at once, keeping the other options', async () => {
    const set = open({ level: 'all', suppress_everyone: true })
    expect(byId('notify-server-all').attributes('aria-checked')).toBe('true')
    expect(byId('notify-server-mentions').attributes('aria-checked')).toBe('false')
    await byId('notify-server-mentions').trigger('click')
    expect(set).toHaveBeenCalledWith('mentions', null, true)
    await byId('notify-server-mute').trigger('click')
    expect(set).toHaveBeenLastCalledWith('mute', null, true)
  })

  it('mutes the server for an hour, eight hours, a day or until turned back on', async () => {
    const set = open()
    await byId('notify-server-mute-1h').trigger('click')
    expect(set).toHaveBeenLastCalledWith('mute', new Date(NOW.getTime() + 3_600_000).toISOString(), false)
    await byId('notify-server-mute-8h').trigger('click')
    expect(set).toHaveBeenLastCalledWith('mute', new Date(NOW.getTime() + 8 * 3_600_000).toISOString(), false)
    await byId('notify-server-mute-24h').trigger('click')
    expect(set).toHaveBeenLastCalledWith('mute', new Date(NOW.getTime() + 24 * 3_600_000).toISOString(), false)
    await byId('notify-server-mute-forever').trigger('click')
    expect(set).toHaveBeenLastCalledWith('mute', null, false)
    expect(byId('notify-server-mute-15m').exists()).toBe(false)
  })

  it('says when a mute ends and turns notifications back on', async () => {
    const set = open({ level: 'mute', muted_until: '2026-01-01T13:00:00Z' })
    expect(byId('notify-server-status').text()).toContain('Muted until')
    await byId('notify-server-unmute').trigger('click')
    expect(set).toHaveBeenCalledWith('all', null, false)
  })

  it('shows an open-ended mute without a time and hides the status when not muted', () => {
    open({ level: 'mute' })
    expect(byId('notify-server-status').text()).toContain('Muted until you turn it back on')
    w.unmount()
    open({ level: 'mentions' })
    expect(byId('notify-server-status').exists()).toBe(false)
  })

  it('toggles suppressing @all and @here without changing the level', async () => {
    const set = open({ level: 'mentions', suppress_everyone: false })
    const toggle = byId('notify-server-suppress')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    expect(set).toHaveBeenCalledWith('mentions', null, true)
  })

  it('tells the person when saving fails', async () => {
    const set = open()
    set.mockResolvedValue(false)
    await byId('notify-server-mentions').trigger('click')
    await vi.waitFor(() => expect(useToastStore().toasts.some(t => t.text.includes('notification settings'))).toBe(true))
  })

  it('closes with the button', async () => {
    open()
    await w.find('footer button').trigger('click')
    expect(w.emitted('close')).toBeTruthy()
  })
})

describe('mute durations', () => {
  it('are outlined buttons in a grid, and the open-ended one shows when it is on', () => {
    open({ level: 'all' })
    const buttons = ['1h', '8h', '24h', 'forever'].map(id => byId(`notify-server-mute-${id}`))
    for (const b of buttons) expect(b.classes()).toContain('btn-secondary')
    expect(buttons[3]?.attributes('aria-pressed')).toBe('false')
    w.unmount()
    open({ level: 'mute', muted_until: null })
    expect(byId('notify-server-mute-forever').attributes('aria-pressed')).toBe('true')
    expect(byId('notify-server-mute-1h').attributes('aria-pressed')).toBe('false')
    expect(byId('notify-server-unmute').classes()).toContain('btn-secondary')
  })

  it('confirms a saved choice with a quiet "Saved" that fades, and reports a failed one with a toast', async () => {
    const set = open()
    expect(byId('saved-indicator').text()).toBe('')
    await byId('notify-server-mentions').trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    expect(byId('saved-indicator').text()).toBe('Saved')
    await vi.advanceTimersByTimeAsync(2500)
    expect(byId('saved-indicator').text()).toBe('')
    set.mockResolvedValueOnce(false)
    await byId('notify-server-mute').trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    expect(byId('saved-indicator').text()).toBe('')
    expect(useToastStore().toasts.length === 1).toBe(true)
  })
})
