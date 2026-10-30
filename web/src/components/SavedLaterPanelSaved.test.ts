import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SavedLaterPanel from './SavedLaterPanel.vue'
import { useChatStore } from '../stores/chat'
import { useSavedStore } from '../stores/saved'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { openSavedPanel } from '../lib/savedPanel'
import { dateTime } from '../lib/time'
import { locale, setLocale, t } from '../i18n'
import { channelFixture, fixtureId, messageFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)
const channel = channelFixture({ id: fixtureId(2), name: 'general' })
const entry = (id: number, content: string, note = '', over: Record<string, unknown> = {}) => ({
  message_id: fixtureId(id), note, saved_at: FIXTURE_TIMESTAMP, available: true, message: messageFixture({ id: fixtureId(id), content }), ...over
})
const gone = (id: number, note = '') => ({ message_id: fixtureId(id), note, saved_at: FIXTURE_TIMESTAMP, available: false })
const page = (items: unknown[], more = false) => ({ items, has_more: more })

/** The server of the saved list: every read of the list gets the next queued page (the last one repeats). */
function serve(...pages: unknown[]) {
  const queue = [...pages]
  apiMock.mockImplementation(async path => {
    if (path === '/api/health') return { status: 'ok', version: 'test', web_client_api: 1, features: ['saved'] }
    if (path.startsWith('/api/me/saved?')) return queue.length > 1 ? queue.shift() : queue[0]
    return null
  })
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  useChatStore().uncategorized = [channel]
  const features = useServerFeaturesStore()
  features.list = ['saved']
  features.loaded = true
})

const mountSaved = () => mount(SavedLaterPanel, { global: { directives: { tooltip: () => undefined } } })
async function openPanel(w: ReturnType<typeof mountSaved>) {
  await w.find('[data-testid="saved-button"]').trigger('click')
  await flushPromises()
}

describe('the saved messages tab', () => {
  it('lists the saved messages and jumps to the chosen one', async () => {
    serve(page([entry(30, '**Read** me later', 'for Friday')]))
    const go = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => undefined)
    const w = mountSaved()
    await openPanel(w)
    expect(apiMock).toHaveBeenCalledWith('/api/me/saved?limit=50', expect.anything())
    expect(w.find('[data-testid="saved-item"]').text()).toContain('Read me later')
    expect(w.find('[data-testid="saved-context"]').text()).toBe('general')
    expect(w.find('[data-testid="saved-note"]').text()).toContain('for Friday')
    expect(w.find('[data-testid="saved-note"]').text()).toContain(t('saved.noteLabel'))
    await w.find('[data-testid="saved-open"]').trigger('click')
    expect(go).toHaveBeenCalledWith(expect.objectContaining({ id: fixtureId(30) }))
    expect(w.find('[data-testid="saved-panel"]').exists()).toBe(false)
  })

  it('explains what saving is for when nothing is saved, and offers to try again when loading fails', async () => {
    serve(page([]))
    const w = mountSaved()
    await openPanel(w)
    const empty = w.get('[data-testid="saved-empty"]').text()
    expect(empty).toContain(t('saved.emptyBody'))
    expect(empty).toContain(t('saved.emptyExample'))
    expect(empty).toContain(t('saved.emptyHow'))
    expect(w.find('[data-testid="saved-search"]').exists()).toBe(false)

    await w.find('[data-testid="saved-button"]').trigger('click')
    useSavedStore().reset()
    apiMock.mockRejectedValue(new Error('offline'))
    await openPanel(w)
    expect(w.find('[role="alert"]').text()).toContain(t('saved.failed'))
    serve(page([entry(30, 'back again')]))
    await w.get('[data-testid="state-retry"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="saved-item"]').text()).toContain('back again')
  })

  it('writes the date like the other lists', async () => {
    const now = new Date().toISOString()
    serve(page([{ ...entry(30, 'today'), message: messageFixture({ id: fixtureId(30), content: 'today', created_at: now }) }]))
    const w = mountSaved()
    await openPanel(w)
    expect(w.get('[data-testid="saved-open"]').text()).toContain(dateTime(now, locale.value))
  })

  it('shows a message the member can no longer see calmly and lets them remove it', async () => {
    serve(page([gone(31, 'my words'), entry(30, 'still here')]))
    const w = mountSaved()
    await openPanel(w)
    expect(w.find('[data-testid="saved-unavailable"]').text()).toContain(t('saved.unavailable'))
    expect(w.find('[data-testid="saved-unavailable"]').text()).toContain(t('saved.unavailableHint'))
    expect(w.findAll('[data-testid="saved-open"]')).toHaveLength(1)
    expect(w.text()).toContain('my words')
    await w.findAll('[data-testid="saved-remove"]')[0]!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved/${fixtureId(31)}`, expect.objectContaining({ method: 'DELETE' }))
    expect(w.find('[data-testid="saved-unavailable"]').exists()).toBe(false)
    expect(w.findAll('[data-testid="saved-item"]')).toHaveLength(1)
  })

  it('keeps the entry and tells the member when removing fails', async () => {
    serve(page([entry(30, 'one')]))
    const error = vi.spyOn(useToastStore(), 'error')
    const w = mountSaved()
    await openPanel(w)
    apiMock.mockRejectedValueOnce(null)
    await w.find('[data-testid="saved-remove"]').trigger('click')
    await flushPromises()
    expect(error).toHaveBeenCalledWith(t('saved.removeFailed'))
    expect(w.find('[data-testid="saved-item"]').exists()).toBe(true)
  })

  it('edits a private note in place and says that only the member sees it', async () => {
    serve(page([entry(30, 'one')]))
    const w = mountSaved()
    await openPanel(w)
    await w.find('[data-testid="saved-note-edit"]').trigger('click')
    expect(w.text()).toContain(t('saved.noteHint'))
    await w.find('[data-testid="saved-note-input"]').setValue('  later  ')
    apiMock.mockResolvedValueOnce(entry(30, 'one', 'later'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved/${fixtureId(30)}`, expect.objectContaining({ method: 'PUT', json: { note: 'later' } }))
    expect(w.find('[data-testid="saved-note"]').text()).toContain('later')
    expect(w.find('[data-testid="saved-note-input"]').exists()).toBe(false)
  })

  it('pages older entries behind the last one shown', async () => {
    serve(page([entry(30, 'one')], true))
    const w = mountSaved()
    await openPanel(w)
    serve(page([entry(29, 'two')]))
    await w.find('[data-testid="saved-more"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/saved?limit=50&before=${fixtureId(30)}`, expect.anything())
    expect(w.findAll('[data-testid="saved-item"]')).toHaveLength(2)
    expect(w.find('[data-testid="saved-more"]').exists()).toBe(false)
  })

  it('follows a change made in another session of the member', async () => {
    serve(page([entry(30, 'one')]))
    const w = mountSaved()
    await openPanel(w)
    const saved = useSavedStore()
    saved.onUpdated({ message_id: fixtureId(30), saved: false })
    await flushPromises()
    expect(w.find('[data-testid="saved-empty"]').exists()).toBe(true)
    expect(saved.isSaved(fixtureId(30))).toBe(false)
  })

  it('searches the text, the note, the author and the channel', async () => {
    serve(page([
      entry(30, 'the wifi password is hunter2', 'router'),
      entry(29, 'lunch at noon', '', { message: messageFixture({ id: fixtureId(29), content: 'lunch at noon', display_name: 'Alice', username: 'alice' }) })
    ]))
    const w = mountSaved()
    await openPanel(w)
    const search = w.get('[data-testid="saved-search"]')
    await search.setValue('wifi')
    expect(w.findAll('[data-testid="saved-item"]')).toHaveLength(1)
    await search.setValue('ROUTER')
    expect(w.get('[data-testid="saved-item"]').text()).toContain('wifi')
    await search.setValue('alice')
    expect(w.get('[data-testid="saved-item"]').text()).toContain('lunch')
    await search.setValue('general')
    expect(w.findAll('[data-testid="saved-item"]')).toHaveLength(2)
    await search.setValue('nothing like this')
    expect(w.find('[data-testid="saved-item"]').exists()).toBe(false)
    expect(w.get('[data-testid="saved-no-match"]').text()).toBe(t('saved.noMatches', { query: 'nothing like this' }))
  })

  it('scrolls to the entry a toast asked for once, and leaves a later search alone', async () => {
    serve(page([entry(30, 'one'), entry(29, 'two')]))
    const w = mountSaved()
    openSavedPanel({ tab: 'saved', messageId: fixtureId(30) })
    await flushPromises()
    await w.get('[data-testid="saved-search"]').setValue('two')
    useSavedStore().items = [...useSavedStore().items]
    await flushPromises()
    expect((w.get('[data-testid="saved-search"]').element as HTMLInputElement).value).toBe('two')
    expect(w.findAll('[data-testid="saved-item"]')).toHaveLength(1)
  })

  it('groups by channel on request and keeps messages that are gone together at the end', async () => {
    const other = channelFixture({ id: fixtureId(3), name: 'random' })
    useChatStore().uncategorized = [channel, other]
    serve(page([
      entry(30, 'in general'),
      gone(31),
      entry(29, 'in random', '', { message: messageFixture({ id: fixtureId(29), content: 'in random', channel_id: other.id }) }),
      entry(28, 'general again')
    ]))
    const w = mountSaved()
    await openPanel(w)
    expect(w.findAll('[data-testid="saved-group"]')).toHaveLength(1)
    await w.get('[data-testid="saved-sort-channel"]').trigger('click')
    const groups = w.findAll('[data-testid="saved-group"]')
    expect(groups.map(g => g.find('h3').text())).toEqual(['general', 'random', t('saved.groupGone')])
    expect(groups[0]!.findAll('[data-testid="saved-item"]')).toHaveLength(2)
    expect(groups[0]!.find('[data-testid="saved-context"]').exists()).toBe(false)
    await w.get('[data-testid="saved-sort-recent"]').trigger('click')
    expect(w.findAll('[data-testid="saved-group"]')).toHaveLength(1)
  })
})
