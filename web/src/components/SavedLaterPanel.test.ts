import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SavedLaterPanel from './SavedLaterPanel.vue'
import { useAuthStore } from '../stores/auth'
import { useOnboardingStore } from '../stores/onboarding'
import { useSavedStore } from '../stores/saved'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { openSavedPanel, pendingPanel, SAVED_HINT_TIP } from '../lib/savedPanel'
import { setLocale, t } from '../i18n'
import { fixtureId, messageFixture, onboardingStateFixture, userFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)

const dueReminder = {
  id: fixtureId(60), remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: FIXTURE_TIMESTAMP, available: true,
  message: { id: fixtureId(40), channel_id: fixtureId(2), parent_id: null, channel_name: 'general', dm: false, author: 'Alice', snippet: 'call mum' }
}
const savedEntry = (id: number) => ({
  message_id: fixtureId(id), note: '', saved_at: FIXTURE_TIMESTAMP, available: true, message: messageFixture({ id: fixtureId(id), content: `message ${id}` })
})

let features: string[]
let savedItems: unknown[]
let reminders: unknown[]
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  pendingPanel.value = null
  useAuthStore().user = userFixture({ id: fixtureId(1) })
  features = ['saved', 'reminders', 'messages.scheduled']
  savedItems = []
  reminders = []
  const store = useServerFeaturesStore()
  store.list = features
  store.loaded = true
  apiMock.mockImplementation(async path => {
    if (path === '/api/health') return { status: 'ok', version: 'test', web_client_api: 1, features }
    if (path.startsWith('/api/me/saved?')) return { items: savedItems, has_more: false }
    if (path === '/api/me/reminders') return { items: reminders }
    if (path === '/api/me/scheduled') return { items: [] }
    if (path === '/api/onboarding') return onboardingStateFixture()
    return null
  })
})

const mountPanel = () => mount(SavedLaterPanel, { global: { directives: { tooltip: () => undefined } } })

describe('the saved and reminders panel', () => {
  it('is one header button that explains itself and opens on the saved tab', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="saved-button"]').attributes('aria-label')).toBe(t('panel.tooltip'))
    await w.get('[data-testid="saved-button"]').trigger('click')
    expect(w.get('[data-testid="panel-tab-saved"]').attributes('aria-selected')).toBe('true')
    expect(w.get('[data-testid="panel-description"]').text()).toBe(t('panel.descSaved'))
    expect(w.find('[data-testid="saved-list"]').exists()).toBe(true)
  })

  it('switches to the reminders tab with a click and with the arrow keys', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="saved-button"]').trigger('click')
    await w.get('[data-testid="panel-tab-later"]').trigger('click')
    expect(w.find('[data-testid="later-list"]').exists()).toBe(true)
    expect(w.get('[data-testid="panel-description"]').text()).toBe(t('panel.descLater'))
    await w.get('[role="tablist"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(w.get('[data-testid="panel-tab-saved"]').attributes('aria-selected')).toBe('true')
    await w.get('[role="tablist"]').trigger('keydown', { key: 'ArrowRight' })
    expect(w.get('[data-testid="panel-tab-later"]').attributes('aria-selected')).toBe('true')
    await w.get('[role="tablist"]').trigger('keydown', { key: 'x' })
    expect(w.get('[data-testid="panel-tab-later"]').attributes('aria-selected')).toBe('true')
  })

  it('shows no tabs when the server has only one of the two, and names just that one', async () => {
    features = ['saved']
    useServerFeaturesStore().list = features
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="saved-button"]').attributes('aria-label')).toBe(t('panel.tooltipSaved'))
    await w.get('[data-testid="saved-button"]').trigger('click')
    expect(w.find('[role="tablist"]').exists()).toBe(false)
    expect(w.find('[data-testid="saved-list"]').exists()).toBe(true)
  })

  it('counts due reminders on the button and on the reminders tab', async () => {
    reminders = [dueReminder]
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="later-badge"]').text()).toBe('1')
    await w.get('[data-testid="saved-button"]').trigger('click')
    expect(w.get('[data-testid="panel-tab-later"]').text()).toContain('1')
  })

  it('opens on a request from the toast and scrolls to the saved message it names', async () => {
    savedItems = [savedEntry(31), savedEntry(30)]
    const w = mountPanel()
    await flushPromises()
    openSavedPanel({ tab: 'saved', messageId: fixtureId(30) })
    await flushPromises()
    expect(w.find('[data-testid="saved-panel"]').exists()).toBe(true)
    expect(pendingPanel.value).toBeNull()
    const flashed = w.findAll('[data-testid="saved-item"]').filter(r => r.classes().includes('msg-flash'))
    expect(flashed.map(r => r.attributes('data-message-id'))).toEqual([fixtureId(30)])
  })

  it('opens on the reminders tab when asked to', async () => {
    const w = mountPanel()
    await flushPromises()
    openSavedPanel({ tab: 'later' })
    await flushPromises()
    expect(w.get('[data-testid="panel-tab-later"]').attributes('aria-selected')).toBe('true')
  })
})

describe('snoozing a reminder that is due', () => {
  it('sets a new reminder, drops the old one and says when it will be back', async () => {
    reminders = [dueReminder]
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="saved-button"]').trigger('click')
    await w.get('[data-testid="panel-tab-later"]').trigger('click')
    expect(w.find('[data-testid="reminder-snooze"]').exists()).toBe(true)
    const next = { ...dueReminder, id: fixtureId(61), delivered_at: null }
    apiMock.mockResolvedValueOnce(next).mockResolvedValueOnce(null)
    await w.get('[data-testid="reminder-snooze-in1h"]').trigger('click')
    await flushPromises()
    const calls = apiMock.mock.calls.slice(-2)
    expect(calls[0]).toEqual(['/api/me/reminders', expect.objectContaining({ method: 'POST', json: expect.objectContaining({ message_id: dueReminder.message.id }) })])
    expect(calls[1]).toEqual([`/api/me/reminders/${dueReminder.id}`, expect.objectContaining({ method: 'DELETE' })])
    const items = w.findAll('[data-testid="reminder-item"]')
    expect(items).toHaveLength(1)
    expect(items[0]!.find('[data-testid="reminder-snooze"]').exists()).toBe(false)
  })

  it('marks a due reminder done', async () => {
    reminders = [dueReminder]
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="saved-button"]').trigger('click')
    await w.get('[data-testid="panel-tab-later"]').trigger('click')
    expect(w.get('[data-testid="reminder-remove"]').attributes('aria-label')).toBe(t('later.done'))
  })
})

describe('the hint after the first save', () => {
  it('points at the button once, until it is closed or the panel is opened', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="saved-hint"]').exists()).toBe(false)
    useSavedStore().hintPending = true
    await flushPromises()
    expect(w.get('[data-testid="saved-hint"]').text()).toContain(t('saved.hintTitle'))
    await w.get('[data-testid="saved-hint-dismiss"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="saved-hint"]').exists()).toBe(false)
    expect(apiMock).toHaveBeenCalledWith('/api/onboarding/tips', expect.objectContaining({ json: { dismissed_tips: [SAVED_HINT_TIP] } }))
  })

  it('goes away when the member opens the panel themselves', async () => {
    const w = mountPanel()
    await flushPromises()
    useSavedStore().hintPending = true
    await flushPromises()
    await w.get('[data-testid="saved-button"]').trigger('click')
    expect(w.find('[data-testid="saved-hint"]').exists()).toBe(false)
    expect(useSavedStore().hintPending).toBe(false)
  })

  it('does not come back once it was closed on any device', async () => {
    apiMock.mockImplementation(async path => path === '/api/onboarding'
      ? onboardingStateFixture({ dismissed_tips: [SAVED_HINT_TIP] })
      : path === '/api/health' ? { status: 'ok', version: 'test', web_client_api: 1, features } : { items: [], has_more: false })
    const w = mountPanel()
    await flushPromises()
    await useOnboardingStore().ensureLoaded()
    useSavedStore().hintPending = true
    await flushPromises()
    expect(w.find('[data-testid="saved-hint"]').exists()).toBe(false)
  })
})
