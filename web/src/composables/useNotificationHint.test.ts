import { required } from '../store-test-support.fixture'
import type { EffectScope } from 'vue'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useNotificationHint } from './useNotificationHint'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

let scope: EffectScope | undefined
let chat: ReturnType<typeof useChatStore>

function setup(panel = false) {
  scope = effectScope()
  return required(scope.run(() => useNotificationHint(() => panel)))
}

beforeEach(() => {
  setLocale('en')
  localStorage.clear()
  setActivePinia(createPinia())
  chat = useChatStore()
  chat.notificationPermission = 'default'
})

afterEach(() => {
  scope?.stop()
  vi.restoreAllMocks()
})

describe('useNotificationHint', () => {
  it('shows the hint only while permission is undecided and not in a panel', () => {
    expect(setup().showNotifHint.value).toBe(true)
    scope?.stop()
    expect(setup(true).showNotifHint.value).toBe(false)
    scope?.stop()
    chat.notificationPermission = 'granted'
    expect(setup().showNotifHint.value).toBe(false)
  })

  it('remembers a dismissal', () => {
    const h = setup()
    h.dismissNotifHint()
    expect(h.showNotifHint.value).toBe(false)
    expect(localStorage.getItem('mnema_notif_hint_dismissed')).toBe('1')
    scope?.stop()
    expect(setup().showNotifHint.value).toBe(false)
  })

  it('survives unavailable storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const h = setup()
    expect(h.showNotifHint.value).toBe(true)
    h.dismissNotifHint()
    expect(h.showNotifHint.value).toBe(false)
  })

  it('toasts and dismisses after a granted or denied answer, but not a dismissed prompt', async () => {
    const toasts = useToastStore()
    const success = vi.spyOn(toasts, 'success')
    const error = vi.spyOn(toasts, 'error')
    const ask = vi.spyOn(chat, 'requestNotificationPermission')
    const h = setup()

    ask.mockResolvedValueOnce('default')
    await h.enableNotifications()
    expect(h.showNotifHint.value).toBe(true)

    ask.mockResolvedValueOnce('granted')
    await h.enableNotifications()
    expect(success).toHaveBeenCalled()
    expect(h.showNotifHint.value).toBe(false)

    ask.mockResolvedValueOnce('denied')
    await h.enableNotifications()
    expect(error).toHaveBeenCalled()
  })

  it('builds the level menu with the stored setting ticked and a mute submenu', () => {
    const setLevel = vi.spyOn(chat, 'setNotificationLevel').mockResolvedValue(true)
    chat.notifySettings = { channels: [{ channel_id: 'ch1', level: 'mentions', muted_until: null }], categories: [] }
    const items = setup().notificationMenuItems('ch1')
    const radios = items.filter(i => i.type === 'radio')
    expect(radios.map(i => i.type === 'radio' && i.checked)).toEqual([false, false, true])
    const mute = items.find(i => i.type === 'submenu')
    expect(mute && 'items' in mute ? mute.items.map(i => i.id) : []).toEqual(['mute-15m', 'mute-1h', 'mute-8h', 'mute-24h', 'mute-forever'])
    radios[1]?.action?.()
    expect(setLevel).toHaveBeenCalledWith('ch1', 'all', null)
  })
})
