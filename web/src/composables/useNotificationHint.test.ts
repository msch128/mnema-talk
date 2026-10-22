import { required } from '../store-test-support.fixture'
import type { EffectScope } from 'vue'
import type { OnboardingState } from '../types/rest'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useNotificationHint } from './useNotificationHint'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useOnboardingStore } from '../stores/onboarding'
import { setLocale } from '../i18n'

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (_path: string, options?: { json?: unknown }) => {
    if (options?.json) puts.push(options.json)
    return Promise.resolve(record({ dismissed_tips: puts.flatMap(p => (p as { dismissed_tips: string[] }).dismissed_tips) }))
  }
}))

const puts: unknown[] = []
function record(overrides: Partial<OnboardingState> = {}): OnboardingState {
  return {
    community_name: '', welcome_text: '', rules_text: '', rules_version: 0, rules_accepted_version: 0,
    rules_pending: false, start_channel_id: null, dismissed_tips: [], welcome_done: true, ...overrides
  }
}

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
  puts.length = 0
  useOnboardingStore().state = record()
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

  it('stays hidden until the account record is known, and while the welcome flow is open', () => {
    const onboarding = useOnboardingStore()
    onboarding.state = null
    const h = setup()
    expect(h.showNotifHint.value).toBe(false)
    onboarding.state = record()
    expect(h.showNotifHint.value).toBe(true)
    onboarding.welcomeOpen = true
    expect(h.showNotifHint.value).toBe(false)
  })

  it('remembers a dismissal on the account, not in the browser', async () => {
    const h = setup()
    await h.dismissNotifHint()
    expect(h.showNotifHint.value).toBe(false)
    expect(puts).toEqual([{ dismissed_tips: ['notif-hint'] }])
    expect(localStorage.getItem('mnema_notif_hint_dismissed')).toBeNull()
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
