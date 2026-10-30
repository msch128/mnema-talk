import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SavedLaterPanel from './SavedLaterPanel.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { toLocalInputValue } from '../lib/laterTimes'
import { setLocale, t } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)

const ref = { id: fixtureId(40), channel_id: fixtureId(2), parent_id: null, channel_name: 'general', dm: false, author: 'Alice', snippet: '**call** mum' }
const reminder = (id: number, over: Record<string, unknown> = {}) => ({
  id: fixtureId(id), remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: null, available: true, message: ref, ...over
})
const scheduled = (id: number, over: Record<string, unknown> = {}) => ({
  id: fixtureId(id), channel_id: fixtureId(2), channel_name: 'general', dm: false, parent_id: null, reply_to_id: null, content: 'good morning',
  send_at: FIXTURE_TIMESTAMP, state: 'pending', failure_reason: '', created_at: FIXTURE_TIMESTAMP, updated_at: FIXTURE_TIMESTAMP, ...over
})

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  features = ['reminders', 'messages.scheduled']
  useServerFeaturesStore().list = features
  useServerFeaturesStore().loaded = true
})

const mountPopover = () => mount(SavedLaterPanel, { global: { directives: { tooltip: () => undefined } } })
async function openPanel(w: ReturnType<typeof mountPopover>) {
  await w.find('[data-testid="saved-button"]').trigger('click')
  await flushPromises()
  const tab = w.find('[data-testid="panel-tab-later"]')
  if (tab.exists()) await tab.trigger('click')
}

let features = ['reminders', 'messages.scheduled']

function answer(reminders: unknown[], scheduledList: unknown[]) {
  apiMock.mockImplementation(async path => {
    if (path === '/api/health') return { status: 'ok', version: 'test', web_client_api: 1, features }
    if (path === '/api/me/reminders') return { items: reminders }
    if (path === '/api/me/scheduled') return { items: scheduledList }
    return null
  })
}

describe('the reminders and scheduled tab', () => {
  it('stays hidden on a server without the features', async () => {
    features = []
    useServerFeaturesStore().list = []
    answer([], [])
    const w = mountPopover()
    await flushPromises()
    expect(w.find('[data-testid="saved-button"]').exists()).toBe(false)
    expect(apiMock.mock.calls.every(([path]) => path === '/api/health')).toBe(true)
  })

  it('shows only the section of a feature the server has', async () => {
    features = ['reminders']
    useServerFeaturesStore().list = features
    answer([reminder(1)], [])
    const w = mountPopover()
    await openPanel(w)
    const paths = apiMock.mock.calls.map(([path]) => path)
    expect(paths).toContain('/api/me/reminders')
    expect(paths).not.toContain('/api/me/scheduled')
    expect(w.findAll('[data-testid="reminder-item"]')).toHaveLength(1)
  })

  it('lists reminders and scheduled messages, and counts what needs attention in the badge', async () => {
    answer([reminder(1), reminder(2, { delivered_at: FIXTURE_TIMESTAMP })], [scheduled(3), scheduled(4, { state: 'failed', failure_reason: 'slow_mode' })])
    const w = mountPopover()
    await flushPromises()
    expect(w.find('[data-testid="later-badge"]').text()).toBe('2')
    await openPanel(w)
    expect(w.findAll('[data-testid="reminder-item"]')).toHaveLength(2)
    expect(w.find('[data-testid="reminder-item"]').text()).toContain('call mum')
    expect(w.findAll('[data-testid="scheduled-item"]')).toHaveLength(2)
    expect(w.find('[data-testid="scheduled-failed"]').text()).toContain(t('later.reason.slow_mode'))
  })

  it('shows a reminder of a message the member can no longer see without any text', async () => {
    answer([reminder(1, { available: false, message: undefined, delivered_at: FIXTURE_TIMESTAMP })], [])
    const w = mountPopover()
    await openPanel(w)
    expect(w.find('[data-testid="reminder-unavailable"]').text()).toBe(t('later.unavailable'))
    expect(w.find('[data-testid="reminder-open"]').exists()).toBe(false)
  })

  it('jumps to the message, and cancels or dismisses a reminder', async () => {
    answer([reminder(1)], [])
    const go = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => undefined)
    const w = mountPopover()
    await openPanel(w)
    await w.find('[data-testid="reminder-remove"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/reminders/${fixtureId(1)}`, expect.objectContaining({ method: 'DELETE' }))
    expect(w.find('[data-testid="reminder-item"]').exists()).toBe(false)

    answer([reminder(5)], [])
    await w.find('[data-testid="saved-button"]').trigger('click')
    await openPanel(w)
    await w.find('[data-testid="reminder-open"]').trigger('click')
    expect(go).toHaveBeenCalledWith({ id: ref.id, channel_id: ref.channel_id })
  })

  it('edits a scheduled message and reschedules a failed draft with a new time', async () => {
    answer([], [scheduled(4, { state: 'failed', failure_reason: 'not_allowed' })])
    const w = mountPopover()
    await openPanel(w)
    await w.find('[data-testid="scheduled-edit"]').trigger('click')
    await w.find('[data-testid="scheduled-text"]').setValue('good morning, everyone')
    const next = new Date(Date.now() + 30 * 86_400_000)
    next.setHours(8, 0, 0, 0)
    await w.find('[data-testid="scheduled-when"]').setValue(toLocalInputValue(next))
    apiMock.mockResolvedValueOnce(scheduled(4, { content: 'good morning, everyone' }))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/scheduled/${fixtureId(4)}`, expect.objectContaining({
      method: 'PATCH', json: { content: 'good morning, everyone', send_at: next.toISOString() }
    }))
    expect(w.find('[data-testid="scheduled-failed"]').exists()).toBe(false)
  })

  it('deletes a scheduled message', async () => {
    answer([], [scheduled(3)])
    const w = mountPopover()
    await openPanel(w)
    await w.find('[data-testid="scheduled-delete"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/scheduled/${fixtureId(3)}`, expect.objectContaining({ method: 'DELETE' }))
    expect(w.find('[data-testid="scheduled-item"]').exists()).toBe(false)
  })

  it('says so when there is nothing, and when loading fails', async () => {
    answer([], [])
    const w = mountPopover()
    await openPanel(w)
    expect(w.find('[data-testid="later-empty"]').exists()).toBe(true)
    apiMock.mockReset()
    apiMock.mockRejectedValue(new Error('offline'))
    await w.find('[data-testid="saved-button"]').trigger('click')
    await openPanel(w)
    expect(w.find('[role="alert"]').text()).toContain(t('later.failed'))
  })

  it('offers to try again after a failed load', async () => {
    apiMock.mockRejectedValue(new Error('offline'))
    const w = mountPopover()
    await openPanel(w)
    answer([reminder(1)], [])
    await w.get('[data-testid="state-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="reminder-item"]').exists()).toBe(true)
  })
})
