import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SavedMessages from './SavedMessages.vue'
import { useChatStore } from '../stores/chat'
import { useSavedStore } from '../stores/saved'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import { channelFixture, fixtureId, messageFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)
const channel = channelFixture({ id: fixtureId(2), name: 'general' })
const entry = (id: number, content: string, note = '') => ({
  message_id: fixtureId(id), note, saved_at: FIXTURE_TIMESTAMP, available: true, message: messageFixture({ id: fixtureId(id), content })
})
const gone = (id: number, note = '') => ({ message_id: fixtureId(id), note, saved_at: FIXTURE_TIMESTAMP, available: false })
const page = (items: unknown[], more = false) => ({ items, has_more: more })

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  useChatStore().uncategorized = [channel]
})

const mountSaved = () => mount(SavedMessages, { global: { directives: { tooltip: () => undefined } } })
async function openPanel(w: ReturnType<typeof mountSaved>) {
  await w.find('[data-testid="saved-button"]').trigger('click')
  await flushPromises()
}

describe('SavedMessages', () => {
  it('lists the saved messages and jumps to the chosen one', async () => {
    apiMock.mockResolvedValue(page([entry(30, '**Read** me later', 'for Friday')]))
    const go = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => undefined)
    const w = mountSaved()
    await openPanel(w)
    expect(apiMock).toHaveBeenCalledWith('/api/me/saved?limit=50', expect.anything())
    expect(w.find('[data-testid="saved-item"]').text()).toContain('Read me later')
    expect(w.find('[data-testid="saved-note"]').text()).toBe('for Friday')
    await w.find('[data-testid="saved-open"]').trigger('click')
    expect(go).toHaveBeenCalledWith(expect.objectContaining({ id: fixtureId(30) }))
    expect(w.find('[data-testid="saved-panel"]').exists()).toBe(false)
  })

  it('says so when nothing is saved and when loading fails', async () => {
    apiMock.mockResolvedValueOnce(page([]))
    const w = mountSaved()
    await openPanel(w)
    expect(w.find('[data-testid="saved-empty"]').exists()).toBe(true)
    await w.find('[data-testid="saved-button"]').trigger('click')
    apiMock.mockRejectedValueOnce(new Error('offline'))
    await openPanel(w)
    expect(w.find('[role="alert"]').text()).toBe(t('saved.failed'))
  })

  it('shows a message the member can no longer see as unavailable and lets them remove it', async () => {
    apiMock.mockResolvedValueOnce(page([gone(31, 'my words'), entry(30, 'still here')]))
    const w = mountSaved()
    await openPanel(w)
    expect(w.find('[data-testid="saved-unavailable"]').text()).toBe(t('saved.unavailable'))
    expect(w.findAll('[data-testid="saved-open"]')).toHaveLength(1)
    expect(w.text()).toContain('my words')
    apiMock.mockResolvedValueOnce(null)
    await w.findAll('[data-testid="saved-remove"]')[0]!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved/${fixtureId(31)}`, expect.objectContaining({ method: 'DELETE' }))
    expect(w.find('[data-testid="saved-unavailable"]').exists()).toBe(false)
    expect(w.findAll('[data-testid="saved-item"]')).toHaveLength(1)
  })

  it('keeps the entry and tells the member when removing fails', async () => {
    apiMock.mockResolvedValueOnce(page([entry(30, 'one')]))
    const error = vi.spyOn(useToastStore(), 'error')
    const w = mountSaved()
    await openPanel(w)
    apiMock.mockRejectedValueOnce(null)
    await w.find('[data-testid="saved-remove"]').trigger('click')
    await flushPromises()
    expect(error).toHaveBeenCalledWith(t('saved.removeFailed'))
    expect(w.find('[data-testid="saved-item"]').exists()).toBe(true)
  })

  it('edits a note in place', async () => {
    apiMock.mockResolvedValueOnce(page([entry(30, 'one')]))
    const w = mountSaved()
    await openPanel(w)
    await w.find('[data-testid="saved-note-edit"]').trigger('click')
    await w.find('[data-testid="saved-note-input"]').setValue('  later  ')
    apiMock.mockResolvedValueOnce(entry(30, 'one', 'later'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved/${fixtureId(30)}`, expect.objectContaining({ method: 'PUT', json: { note: 'later' } }))
    expect(w.find('[data-testid="saved-note"]').text()).toBe('later')
    expect(w.find('[data-testid="saved-note-input"]').exists()).toBe(false)
  })

  it('pages older entries behind the last one shown', async () => {
    apiMock.mockResolvedValueOnce(page([entry(30, 'one')], true))
    const w = mountSaved()
    await openPanel(w)
    apiMock.mockResolvedValueOnce(page([entry(29, 'two')]))
    await w.find('[data-testid="saved-more"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved?limit=50&before=${fixtureId(30)}`, expect.anything())
    expect(w.findAll('[data-testid="saved-item"]')).toHaveLength(2)
    expect(w.find('[data-testid="saved-more"]').exists()).toBe(false)
  })

  it('follows a change made in another session of the member', async () => {
    apiMock.mockResolvedValueOnce(page([entry(30, 'one')]))
    const w = mountSaved()
    await openPanel(w)
    const saved = useSavedStore()
    saved.onUpdated({ message_id: fixtureId(30), saved: false })
    await flushPromises()
    expect(w.find('[data-testid="saved-empty"]').exists()).toBe(true)
    expect(saved.isSaved(fixtureId(30))).toBe(false)
  })
})
