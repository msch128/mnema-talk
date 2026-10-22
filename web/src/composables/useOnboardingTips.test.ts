import type { ApiOptions } from '../lib/api'
import type { OnboardingState } from '../types/rest'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import TipBar from '../components/TipBar.vue'
import { useAuthStore } from '../stores/auth'
import { useOnboardingStore } from '../stores/onboarding'
import { i18nPlugin, setLocale } from '../i18n'
import { userFixture } from '../test-fixtures.fixture'

let server: OnboardingState
let puts: unknown[]
let failPut = false

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (path: string, options: ApiOptions = {}) => {
    if (options.method === 'PUT') {
      puts.push(options.json)
      if (failPut) return Promise.reject(new Error('offline'))
      const body = options.json as { dismissed_tips?: string[] }
      server = { ...server, dismissed_tips: [...new Set([...server.dismissed_tips, ...(body.dismissed_tips ?? [])])] }
    }
    return Promise.resolve(structuredClone(server))
  },
  onUnauthorized: vi.fn(() => () => true)
}))

function serverState(overrides: Partial<OnboardingState> = {}): OnboardingState {
  return {
    community_name: '', welcome_text: '', rules_text: '', rules_version: 0, rules_accepted_version: 0,
    rules_pending: false, start_channel_id: null, dismissed_tips: [], welcome_done: true, ...overrides
  }
}

async function bar() {
  const w = mount(TipBar, { global: { plugins: [i18nPlugin] } })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  localStorage.clear()
  puts = []
  failPut = false
  server = serverState()
  useAuthStore().user = userFixture()
})
afterEach(() => { vi.restoreAllMocks() })

describe('tips', () => {
  it('show one at a time, in order, and each stays closed once dismissed', async () => {
    const w = await bar()
    expect(w.text()).toContain('Press Ctrl or Cmd + K')
    await w.get('[data-testid="tip-dismiss"]').trigger('click')
    await flushPromises()
    expect(puts).toEqual([{ dismissed_tips: ['search'] }])
    expect(w.text()).toContain('Type @ to mention someone')
    await w.get('[data-testid="tip-dismiss"]').trigger('click')
    await w.get('[data-testid="tip-dismiss"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="tip-bar"]').exists()).toBe(false)
    expect(server.dismissed_tips).toEqual(['search', 'mention', 'talk'])
  })

  it('can be closed with the small cross as well', async () => {
    const w = await bar()
    await w.get('button[aria-label="Close"]').trigger('click')
    await flushPromises()
    expect(server.dismissed_tips).toEqual(['search'])
  })

  it('follow the account: a tip closed elsewhere does not come back', async () => {
    server = serverState({ dismissed_tips: ['search', 'mention'] })
    const w = await bar()
    expect(w.text()).toContain('Click a Talk')
  })

  it('wait for the welcome flow and for members who have not finished it', async () => {
    server = serverState({ welcome_done: false })
    const w = await bar()
    expect(w.find('[data-testid="tip-bar"]').exists()).toBe(false)
    useOnboardingStore().state = serverState()
    useOnboardingStore().welcomeOpen = true
    await flushPromises()
    expect(w.find('[data-testid="tip-bar"]').exists()).toBe(false)
    useOnboardingStore().welcomeOpen = false
    await flushPromises()
    expect(w.find('[data-testid="tip-bar"]').exists()).toBe(true)
  })

  it('stay closed on screen even when saving fails', async () => {
    const w = await bar()
    failPut = true
    await w.get('[data-testid="tip-dismiss"]').trigger('click')
    await flushPromises()
    expect(w.text()).not.toContain('Press Ctrl or Cmd + K')
  })
})

describe('the notification hint dismissal moves from this browser to the account', () => {
  it('uploads the old localStorage flag once and removes it', async () => {
    localStorage.setItem('mnema_notif_hint_dismissed', '1')
    await bar()
    expect(puts).toEqual([{ dismissed_tips: ['notif-hint'] }])
    expect(localStorage.getItem('mnema_notif_hint_dismissed')).toBeNull()
  })

  it('only clears the flag when the account already knows', async () => {
    server = serverState({ dismissed_tips: ['notif-hint'] })
    localStorage.setItem('mnema_notif_hint_dismissed', '1')
    await bar()
    expect(puts).toEqual([])
    expect(localStorage.getItem('mnema_notif_hint_dismissed')).toBeNull()
  })

  it('keeps the flag for the next start when the upload fails', async () => {
    failPut = true
    localStorage.setItem('mnema_notif_hint_dismissed', '1')
    await bar()
    expect(localStorage.getItem('mnema_notif_hint_dismissed')).toBe('1')
  })

  it('does nothing without the flag', async () => {
    await bar()
    expect(puts).toEqual([])
  })
})

describe('the onboarding record', () => {
  it('is read once for concurrent callers and again for another account', async () => {
    const store = useOnboardingStore()
    await Promise.all([store.ensureLoaded(), store.ensureLoaded()])
    await store.ensureLoaded()
    server = serverState({ community_name: 'Second' })
    useAuthStore().user = userFixture({ id: 'a0000000-0000-4000-8000-000000000099' })
    await store.ensureLoaded()
    expect(store.state?.community_name).toBe('Second')
  })

  it('ignores an answer that arrives after the account changed', async () => {
    const store = useOnboardingStore()
    const pending = store.ensureLoaded()
    useAuthStore().user = null
    await pending
    expect(store.state).toBeNull()
  })

  it('does nothing without a signed-in account', async () => {
    useAuthStore().user = null
    const store = useOnboardingStore()
    await store.ensureLoaded()
    await store.reload()
    expect(store.state).toBeNull()
  })

  it('treats the legacy hint as not dismissed when browser storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked') })
    await bar()
    expect(puts).toEqual([])
  })

  it('survives a browser that will not let the old flag go', async () => {
    server = serverState({ dismissed_tips: ['notif-hint'] })
    localStorage.setItem('mnema_notif_hint_dismissed', '1')
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked') })
    await bar()
    expect(puts).toEqual([])
  })

  it('accepting the rules lifts the pending state', async () => {
    server = serverState({ rules_text: 'x', rules_version: 2, rules_pending: true })
    const store = useOnboardingStore()
    await store.ensureLoaded()
    expect(await store.acceptRules(2)).toBe(true)
    expect(store.state).toMatchObject({ rules_pending: false, rules_accepted_version: 2 })
  })
})
