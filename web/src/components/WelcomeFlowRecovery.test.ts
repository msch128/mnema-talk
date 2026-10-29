import type { ApiOptions } from '../lib/api'
import type { OnboardingState } from '../types/rest'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WelcomeFlow from './WelcomeFlow.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useOnboardingStore } from '../stores/onboarding'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { userFixture } from '../test-fixtures.fixture'

const bus = vi.hoisted(() => ({ rules: [] as Array<() => void> }))
let server: OnboardingState
let gets: number
let puts: number
let failGets: number
let failPuts: number
let failAccepts: number
let slowGets: Promise<void> | null

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (path: string, options: ApiOptions = {}) => {
    if (!path.startsWith('/api/onboarding')) return Promise.reject(new Error('not part of this test'))
    if (path === '/api/onboarding/accept') {
      if (failAccepts > 0) { failAccepts--; return Promise.reject(new Error('offline')) }
      server = { ...server, rules_accepted_version: server.rules_version, rules_pending: false }
      return Promise.resolve(undefined)
    }
    if (path === '/api/onboarding') {
      gets++
      if (slowGets) return slowGets.then(() => structuredClone(server))
      if (failGets > 0) { failGets--; return Promise.reject(new Error('rate limited')) }
    }
    if (path === '/api/onboarding/tips') {
      puts++
      if (failPuts > 0) { failPuts--; return Promise.reject(new Error('offline')) }
      server = { ...server, welcome_done: (options.json as { welcome_done?: boolean }).welcome_done ?? server.welcome_done }
    }
    return Promise.resolve(structuredClone(server))
  },
  onUnauthorized: vi.fn(() => () => true),
  onRulesRequired: (handler: () => void) => {
    bus.rules.push(handler)
    return () => true
  }
}))

function state(overrides: Partial<OnboardingState> = {}): OnboardingState {
  return {
    community_name: 'Heimat', welcome_text: 'Hello', rules_text: 'Be kind', rules_version: 1, rules_accepted_version: 0,
    rules_pending: false, start_channel_id: null, dismissed_tips: [], welcome_done: false, ...overrides
  }
}

async function flow() {
  const w = mount(WelcomeFlow, { attachTo: document.body, global: { plugins: [i18nPlugin], directives: { tooltip } } })
  await flushPromises()
  return w
}

const dialog = () => document.querySelector('[role="dialog"]')
const title = () => document.querySelector('[data-testid="welcome-title"]')?.textContent

async function backToTab() {
  document.dispatchEvent(new Event('visibilitychange'))
  await flushPromises()
}

async function finishFirstRun() {
  for (let i = 0; i < 4 && dialog(); i++) {
    const id = ['welcome-next', 'welcome-skip'].find(t => document.querySelector(`[data-testid="${t}"]`)) ?? 'welcome-next'
    document.querySelector<HTMLElement>(`[data-testid="${id}"]`)?.click()
    await flushPromises()
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  setLocale('en')
  server = state()
  gets = 0
  puts = 0
  failGets = 0
  failPuts = 0
  failAccepts = 0
  slowGets = null
  bus.rules = []
  window.history.replaceState(null, '', '/')
  useChatStore().notificationPermission = 'unsupported'
  useAuthStore().user = userFixture({ created_at: new Date().toISOString(), display_name: 'Ada', bio: 'hi' })
})
afterEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('finishing the welcome flow when the save does not go through', () => {
  it('stays closed on tab focus while the save fails, and retries a bounded number of times', async () => {
    failPuts = 99
    const w = await flow()
    expect(dialog()).not.toBeNull()
    await finishFirstRun()
    expect(dialog()).toBeNull()

    await backToTab()
    expect(dialog()).toBeNull()
    await vi.advanceTimersByTimeAsync(20_000)
    await backToTab()
    expect(dialog()).toBeNull()
    expect(puts).toBe(4)
    w.unmount()
  })

  it('stays closed when the tab changes while the save is in flight, and the retry then lands', async () => {
    failPuts = 1
    const w = await flow()
    await finishFirstRun()
    await backToTab()
    expect(dialog()).toBeNull()
    await vi.advanceTimersByTimeAsync(1000)
    expect(server.welcome_done).toBe(true)
    await backToTab()
    expect(dialog()).toBeNull()
    w.unmount()
  })
})

describe('the rules step after a failed load or changed rules', () => {
  it('says so when accepting the rules fails, and closes after a retry', async () => {
    server = state({ welcome_done: true, rules_pending: true })
    failAccepts = 1
    const w = await flow()
    const accept = () => document.querySelector<HTMLElement>('[data-testid="welcome-accept"]')
    accept()?.click()
    await flushPromises()
    expect(document.querySelector('[data-testid="welcome-accept-failed"]')).not.toBeNull()
    expect(title()).toBe('The rules')
    accept()?.click()
    await flushPromises()
    expect(dialog()).toBeNull()
    w.unmount()
  })

  it('opens nothing when the component is gone before the onboarding state arrives', async () => {
    server = state({ welcome_done: true, rules_pending: true })
    let release = () => {}
    slowGets = new Promise<void>(resolve => { release = resolve })
    const w = mount(WelcomeFlow, { attachTo: document.body, global: { plugins: [i18nPlugin], directives: { tooltip } } })
    await flushPromises()
    w.unmount()
    release()
    await flushPromises()
    expect(useOnboardingStore().welcomeOpen).toBe(false)
  })

  it('retries a failed first load once and opens the rules', async () => {
    server = state({ welcome_done: true, rules_pending: true })
    failGets = 1
    const w = await flow()
    expect(dialog()).toBeNull()
    await vi.advanceTimersByTimeAsync(2000)
    expect(title()).toBe('The rules')
    w.unmount()
  })

  it('opens the rules once when a burst of writes is refused with RULES_NOT_ACCEPTED', async () => {
    server = state({ welcome_done: true })
    const w = await flow()
    expect(dialog()).toBeNull()
    server = state({ welcome_done: true, rules_version: 2, rules_pending: true })
    const before = gets
    const refused = bus.rules[0]!
    refused()
    refused()
    refused()
    await flushPromises()
    expect(title()).toBe('The rules')
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(gets - before).toBe(1)
    w.unmount()
  })
})
