import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import FirstRunCard from './FirstRunCard.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useFirstRunStore } from '../stores/firstRun'
import { useOnboardingStore } from '../stores/onboarding'
import { useToastStore } from '../stores/toast'
import { adminTabRequest } from '../lib/adminRequest'
import { talkSettingsFor } from '../lib/talks'
import { setLocale } from '../i18n'
import { channelFixture, fixtureId, onboardingStateFixture, userFixture } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const steps = (done: string[] = []) => ['name', 'invite', 'channels', 'voice', 'status', 'roles_rules'].map(id => ({ id, done: done.includes(id), optional: id === 'roles_rules' }))
const checklist = (done: string[] = [], over: Record<string, unknown> = {}) => ({ steps: steps(done), hidden: false, complete: false, ...over })

function mountCard() {
  return mount(FirstRunCard, { global: { directives: { tooltip: () => undefined } } })
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  adminTabRequest.value = null
  talkSettingsFor.value = null
  useAuthStore().user = userFixture({ role: 'admin' })
  useOnboardingStore().state = onboardingStateFixture({ welcome_done: true, community_name: 'Home' })
  const chat = useChatStore()
  chat.activeChannel = channelFixture({ id: fixtureId(1), type: 'text' })
  useFirstRunStore().state = checklist(['name'])
})

describe('FirstRunCard', () => {
  it('lists the steps, ticks the done ones and offers an action for the rest', async () => {
    apiMock.mockResolvedValue(checklist(['name']))
    const w = mountCard()
    await flushPromises()
    expect(w.get('h2').text()).toBe('Get started with Home')
    expect(w.text()).toContain('1 of 6 done')
    expect(w.get('[data-step="name"]').attributes('data-done')).toBe('true')
    expect(w.find('[data-testid="first-run-name"]').exists()).toBe(false)
    expect(w.get('[data-step="roles_rules"]').text()).toContain('optional')
    for (const id of ['invite', 'channels', 'voice', 'status', 'roles_rules']) expect(w.find(`[data-testid="first-run-${id}"]`).exists()).toBe(true)
  })

  it('opens the console page of a step', async () => {
    apiMock.mockResolvedValue(checklist())
    const w = mountCard()
    await flushPromises()
    for (const [step, tab] of [['name', 'onboarding'], ['channels', 'channels'], ['status', 'status'], ['roles_rules', 'onboarding']] as const) {
      adminTabRequest.value = null
      await w.get(`[data-testid="first-run-${step}"]`).trigger('click')
      expect(adminTabRequest.value).toBe(tab)
    }
  })

  it('opens the settings of the first Talk for voice, else the channel page', async () => {
    apiMock.mockResolvedValue(checklist())
    const w = mountCard()
    await flushPromises()
    await w.get('[data-testid="first-run-voice"]').trigger('click')
    expect(adminTabRequest.value).toBe('channels')

    useChatStore().categories = [{ id: fixtureId(9), name: 'Talks', sort_order: 0, channels: [channelFixture({ id: fixtureId(7), type: 'voice', name: 'Lounge' })] }] as never
    await w.get('[data-testid="first-run-voice"]').trigger('click')
    expect(talkSettingsFor.value).toBe(fixtureId(7))
  })

  it('creates a week-long invite, copies its link and reads the checklist again', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    apiMock.mockResolvedValue(checklist())
    const w = mountCard()
    await flushPromises()
    apiMock.mockReset()
    apiMock.mockResolvedValueOnce({ id: fixtureId(5), code: 'abc 123', max_uses: 5, uses_count: 0, expires_at: null, created_at: '2026-01-01T00:00:00Z' })
    apiMock.mockResolvedValueOnce(checklist(['name', 'invite']))
    await w.get('[data-testid="first-run-invite"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenNthCalledWith(1, '/api/admin/invites', expect.objectContaining({ method: 'POST', json: { max_uses: 5, expires_in_hours: 168 } }))
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/?invite=abc%20123`)
    expect(useToastStore().toasts.at(-1)?.text).toContain('Invite link copied')
    expect(w.get('[data-step="invite"]').attributes('data-done')).toBe('true')
    vi.unstubAllGlobals()
  })

  it('hides itself on request and stays out of the way elsewhere', async () => {
    apiMock.mockResolvedValue(checklist(['name']))
    const w = mountCard()
    await flushPromises()
    apiMock.mockResolvedValue(checklist(['name'], { hidden: true }))
    await w.get('[data-testid="first-run-hide"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/first-run', expect.objectContaining({ method: 'PUT', json: { hidden: true } }))
    expect(w.find('[data-testid="first-run"]').exists()).toBe(false)
  })

  it('is not shown before the welcome flow is done, in a Talk or to a member', async () => {
    apiMock.mockResolvedValue(checklist(['name']))
    useOnboardingStore().state = onboardingStateFixture({ welcome_done: false })
    expect(mountCard().find('[data-testid="first-run"]').exists()).toBe(false)

    useOnboardingStore().state = onboardingStateFixture({ welcome_done: true })
    useChatStore().activeChannel = channelFixture({ id: fixtureId(2), type: 'voice' })
    expect(mountCard().find('[data-testid="first-run"]').exists()).toBe(false)

    useChatStore().activeChannel = channelFixture({ id: fixtureId(1), type: 'text' })
    useAuthStore().user = userFixture({ role: 'user' })
    useFirstRunStore().reset()
    expect(mountCard().find('[data-testid="first-run"]').exists()).toBe(false)
  })

  it('says so when everything is done and lets the Owner finish', async () => {
    useFirstRunStore().state = checklist(['name', 'invite', 'channels', 'voice', 'status'], { complete: true })
    apiMock.mockResolvedValue(checklist(['name', 'invite', 'channels', 'voice', 'status'], { complete: true }))
    const w = mountCard()
    await flushPromises()
    expect(w.get('h2').text()).toBe('Your community is ready')
    apiMock.mockResolvedValue(checklist([], { hidden: true }))
    await w.get('[data-testid="first-run-done"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="first-run"]').exists()).toBe(false)
  })
})
