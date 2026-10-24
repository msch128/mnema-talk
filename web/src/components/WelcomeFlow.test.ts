import type { ApiOptions } from '../lib/api'
import type { OnboardingState } from '../types/rest'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import WelcomeFlow from './WelcomeFlow.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { channelFixture, userFixture } from '../test-fixtures.fixture'

type Call = { path: string; method: string; json: unknown }
let calls: Call[]
let server: OnboardingState
let rejectAccept: boolean

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (path: string, options: ApiOptions = {}) => {
    calls.push({ path, method: options.method ?? 'GET', json: options.json })
    if (path === '/api/onboarding/accept') {
      if (rejectAccept) return Promise.reject(new Error('conflict'))
      server = { ...server, rules_pending: false, rules_accepted_version: server.rules_version }
      return Promise.resolve(undefined)
    }
    if (path === '/api/onboarding/tips') {
      const body = options.json as { welcome_done?: boolean; dismissed_tips?: string[] }
      server = { ...server, welcome_done: body.welcome_done ?? server.welcome_done, dismissed_tips: [...server.dismissed_tips, ...(body.dismissed_tips ?? [])] }
    }
    return Promise.resolve(structuredClone(server))
  },
  onUnauthorized: vi.fn(() => () => true)
}))

const START = channelFixture({ id: 'a0000000-0000-4000-8000-000000000042', name: 'start' })

function state(overrides: Partial<OnboardingState> = {}): OnboardingState {
  return {
    community_name: 'Heimat', welcome_text: 'Hello **there**', rules_text: '', rules_version: 0, rules_accepted_version: 0,
    rules_pending: false, start_channel_id: null, dismissed_tips: [], welcome_done: false, ...overrides
  }
}

function account(createdAt = new Date().toISOString()) {
  useAuthStore().user = userFixture({ created_at: createdAt, display_name: 'Ada', bio: 'hi' })
}

async function flow() {
  const w = mount(WelcomeFlow, { attachTo: document.body, global: { plugins: [i18nPlugin], directives: { tooltip } } })
  await flushPromises()
  return w
}

const click = async (testId: string) => {
  document.querySelector<HTMLElement>(`[data-testid="${testId}"]`)?.click()
  await flushPromises()
}
const title = () => document.querySelector('[data-testid="welcome-title"]')?.textContent
const saved = () => calls.filter(c => c.method === 'PUT').map(c => c.json)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  calls = []
  rejectAccept = false
  server = state()
  window.history.replaceState(null, '', '/')
  useChatStore().notificationPermission = 'default'
})
afterEach(() => { document.body.innerHTML = '' })

describe('WelcomeFlow on a first run', () => {
  it('walks welcome, rules, profile and notifications, then opens the start channel', async () => {
    server = state({ rules_text: 'Be kind', rules_version: 2, rules_pending: true, start_channel_id: START.id })
    account()
    useChatStore().uncategorized = [START]
    const w = await flow()

    expect(title()).toBe('Welcome to Heimat')
    expect(document.body.textContent).toContain('Step 1 of 4')
    expect(document.querySelector('[data-testid="welcome-text"] strong')?.textContent).toBe('there')

    await click('welcome-next')
    expect(title()).toBe('The rules')
    await click('welcome-accept')
    expect(calls).toContainEqual({ path: '/api/onboarding/accept', method: 'POST', json: { version: 2 } })

    expect(title()).toBe('Your profile')
    await click('welcome-skip')
    expect(title()).toBe('Stay in the loop')
    const ask = vi.spyOn(useChatStore(), 'requestNotificationPermission').mockResolvedValue('granted')
    await click('welcome-enable')

    expect(ask).toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(saved()).toEqual([{ welcome_done: true }])
    expect(window.location.pathname).toBe(`/c/${START.id}`)
    w.unmount()
  })

  it('drops the notification step where permission cannot be asked', async () => {
    useChatStore().notificationPermission = 'unsupported'
    account()
    await flow()
    expect(document.body.textContent).toContain('Step 1 of 2')
  })

  it('skips the empty welcome page when the Owner wrote no welcome text', async () => {
    server = state({ welcome_text: '', community_name: '' })
    account()
    await flow()
    expect(title()).toBe('Your profile')
    expect(document.querySelector('[data-testid="welcome-text"]')).toBeNull()
    expect(document.body.textContent).toContain('Step 1 of 2')
  })

  it('saves a changed profile before moving on', async () => {
    account()
    const update = vi.spyOn(useAuthStore(), 'updateProfile').mockResolvedValue(userFixture())
    await flow()
    await click('welcome-next')
    const name = document.querySelector<HTMLInputElement>('#welcome-displayname')
    expect(name?.value).toBe('Ada')
    if (name) { name.value = 'Ada L'; name.dispatchEvent(new Event('input')) }
    await click('welcome-profile-save')
    expect(update).toHaveBeenCalledWith({ displayName: 'Ada L', bio: 'hi' })
    expect(title()).toBe('Stay in the loop')
  })

  it('closing early still lands on the rules first, and a finished flow never returns', async () => {
    server = state({ rules_text: 'Be kind', rules_version: 1, rules_pending: true })
    account()
    await flow()
    await click('welcome-next')
    expect(document.querySelector('[data-dialog-close]')).toBeNull()
    await click('welcome-accept')
    await click('welcome-skip')
    await click('welcome-skip')
    expect(server.welcome_done).toBe(true)

    document.body.replaceChildren()
    await flow()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('jumps from the welcome text to the rules when the member tries to close', async () => {
    server = state({ rules_text: 'Be kind', rules_version: 1, rules_pending: true })
    account()
    await flow()
    await click('welcome-next')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(title()).toBe('The rules')
  })
})

describe('WelcomeFlow rules', () => {
  it('blocks on the rules step: no close button, Escape and the scrim do nothing', async () => {
    server = state({ rules_text: 'Be kind', rules_version: 3, rules_pending: true, welcome_done: true })
    account('2020-01-01T00:00:00Z')
    await flow()
    expect(title()).toBe('The rules')
    expect(document.body.textContent).toContain('Step 1 of 1')
    expect(document.querySelector('[data-dialog-close]')).toBeNull()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    document.querySelector('[role="dialog"]')?.parentElement?.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    document.querySelector('[role="dialog"]')?.parentElement?.dispatchEvent(new PointerEvent('pointerup', { bubbles: true }))
    await flushPromises()
    expect(title()).toBe('The rules')
  })

  it('asks again for only the rules after the Owner raises the version', async () => {
    server = state({ rules_text: 'Be kind', rules_version: 4, rules_accepted_version: 3, rules_pending: true, welcome_done: true })
    account('2020-01-01T00:00:00Z')
    await flow()
    expect(title()).toBe('The rules')
    await click('welcome-accept')
    expect(calls).toContainEqual({ path: '/api/onboarding/accept', method: 'POST', json: { version: 4 } })
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(saved()).toEqual([])
    expect(window.location.pathname).toBe('/')
  })

  it('stays on the rules and shows the new ones when they changed while it was open', async () => {
    server = state({ rules_text: 'Old', rules_version: 1, rules_pending: true, welcome_done: true })
    account('2020-01-01T00:00:00Z')
    await flow()
    rejectAccept = true
    server = state({ rules_text: 'New rules', rules_version: 2, rules_pending: true, welcome_done: true })
    await click('welcome-accept')
    expect(title()).toBe('The rules')
    expect(document.querySelector('[data-testid="welcome-rules"]')?.textContent).toContain('New rules')
  })

  it('picks up a rules version raised while the app was in the background', async () => {
    server = state({ rules_text: 'Be kind', rules_version: 1, rules_accepted_version: 1, welcome_done: true })
    account('2020-01-01T00:00:00Z')
    await flow()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    server = state({ rules_text: 'Stricter', rules_version: 2, rules_accepted_version: 1, rules_pending: true, welcome_done: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(title()).toBe('The rules')
  })
})

describe('WelcomeFlow after an upgrade', () => {
  it('does not walk an existing member through the flow, it marks them done', async () => {
    account('2020-01-01T00:00:00Z')
    await flow()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(saved()).toEqual([{ welcome_done: true }])
  })

  it('still asks an existing member for pending rules, and only for those', async () => {
    server = state({ rules_text: 'Be kind', rules_version: 1, rules_pending: true })
    account('2020-01-01T00:00:00Z')
    await flow()
    expect(title()).toBe('The rules')
    expect(document.body.textContent).toContain('Step 1 of 1')
  })
})
