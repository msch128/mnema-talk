import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import SearchModal from './SearchModal.vue'
import GlobalSearch from './GlobalSearch.vue'
import { searchOpen } from '../lib/globalSearch'
import { useChatStore } from '../stores/chat'
import { channelFixture, messageFixture, userFixture, fixtureId, requireValue } from '../test-fixtures.fixture'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

let wrapper: VueWrapper
let opener: HTMLButtonElement

async function open() {
  opener = document.createElement('button')
  document.body.appendChild(opener)
  opener.focus()
  wrapper = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: false } })
  await wrapper.setProps({ modelValue: true })
  await nextTick()
  await nextTick()
}

const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')
const scrim = () => dialog()!.parentElement!

beforeEach(() => {
  mockApi.mockReset()
  setActivePinia(createPinia())
})

afterEach(() => {
  wrapper?.unmount()
  // Wrapper was unmounted above.
  document.body.innerHTML = ''
  setLocale('de')
})

describe('SearchModal', () => {
  it('is a labelled modal dialog that focuses the search input', async () => {
    await open()
    const dlg = dialog()
    expect(dlg!.getAttribute('aria-modal')).toBe('true')
    const label = document.getElementById(dlg!.getAttribute('aria-labelledby')!)
    expect(label?.textContent).toBe('Suche')
    expect(document.activeElement).toBe(document.querySelector<HTMLInputElement>('[data-search-input]')!)
  })

  it('closes on Escape and returns focus to the opener', async () => {
    await open()
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('update:modelValue')!).toEqual([[false]])
    await wrapper.setProps({ modelValue: false })
    expect(dialog()).toBeNull()
    expect(document.activeElement).toBe(opener)
  })

  it('does not close when a text selection is released on the scrim', async () => {
    await open()
    const input = document.querySelector<HTMLInputElement>('[data-search-input]')!
    input.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    scrim().dispatchEvent(new Event('pointerup', { bubbles: true }))
    expect(wrapper.emitted('update:modelValue')!).toBeUndefined()

    scrim().dispatchEvent(new Event('pointerdown', { bubbles: true }))
    scrim().dispatchEvent(new Event('pointerup', { bubbles: true }))
    expect(wrapper.emitted('update:modelValue')!).toEqual([[false]])
  })

  it('formats result dates in the UI language', async () => {
    const created = '2026-03-04T10:15:00Z'
    mockApi.mockResolvedValue({ messages: [messageFixture({ id: fixtureId(1017), channel_id: fixtureId(1006), username: 'ada', content: 'hello', created_at: created })], has_more: false })
    await open()
    const input = document.querySelector<HTMLInputElement>('[data-search-input]')!
    input.value = 'hello'
    input.dispatchEvent(new Event('input'))
    await vi.waitFor(() => expect(document.querySelector<HTMLElement>('#search-result-0')!).not.toBeNull())
    const expected = new Date(created).toLocaleDateString('de', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    expect(document.querySelector<HTMLElement>('#search-result-0')!.textContent).toContain(expected)
  })
})

function input() { return requireValue(document.querySelector<HTMLInputElement>('[data-search-input]')) }
async function query(value: string) {
  input().value = value
  input().dispatchEvent(new Event('input'))
  await new Promise(resolve => setTimeout(resolve, 280))
  await flushPromises()
}
function click(selector: string) { requireValue(document.querySelector<HTMLButtonElement>(selector)).click() }
function key(k: string, target: HTMLElement = input()) { target.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })) }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('SearchModal queries', () => {
  it('builds filter criteria, toggles attachment filters and clears an empty query', async () => {
    setLocale('en')
    const chat = useChatStore()
    chat.uncategorized = [channelFixture(), channelFixture({ id: fixtureId(30), name: 'Lounge', type: 'voice' })]
    chat.members = [userFixture(), userFixture({ id: fixtureId(31), display_name: 'Displayed member' })]
    mockApi.mockResolvedValue({ messages: [], has_more: false })
    await open()
    key('ArrowDown')
    await query('  hello ')
    expect(mockApi).toHaveBeenLastCalledWith('/api/search?limit=25&q=hello', expect.any(Object))
    expect(dialog()!.textContent).toContain('No messages found')
    const selects = [...document.querySelectorAll<HTMLSelectElement>('select')]
    requireValue(selects[0]).value = fixtureId(30)
    requireValue(selects[0]).dispatchEvent(new Event('change'))
    await flushPromises()
    requireValue(selects[1]).value = fixtureId(31)
    requireValue(selects[1]).dispatchEvent(new Event('change'))
    await flushPromises()
    click('button[aria-pressed]')
    await flushPromises()
    expect(mockApi.mock.calls.at(-1)?.[0]).toContain('channel_id=' + fixtureId(30))
    expect(mockApi.mock.calls.at(-1)?.[0]).toContain('author_id=' + fixtureId(31))
    expect(mockApi.mock.calls.at(-1)?.[0]).toContain('has=file')
    click('button[aria-pressed]')
    await flushPromises()
    expect(mockApi.mock.calls.at(-1)?.[0]).not.toContain('has=')
    for (const select of selects) { select.value = ''; select.dispatchEvent(new Event('change')) }
    await query('')
    expect(document.querySelector('[role="option"]')).toBeNull()
    expect(input().hasAttribute('aria-activedescendant')).toBe(false)
    click('[data-dialog-close]')
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
  })

  it('navigates results, wraps selection, ignores select navigation and chooses by Enter or click', async () => {
    const chat = useChatStore()
    chat.uncategorized = [channelFixture(), channelFixture({ id: fixtureId(30), name: 'Lounge', type: 'voice' })]
    chat.goToMessage = vi.fn()
    const first = messageFixture({ display_name: 'Ada', parent_id: fixtureId(33), attachments: [{ id: fixtureId(40), original_filename: 'test.txt', is_deleted: false, mime_type: 'text/plain', size_bytes: 4, url: '/synthetic.txt' }] })
    const second = messageFixture({ id: fixtureId(34), channel_id: fixtureId(30), content: 'voice result', created_at: '' })
    mockApi.mockResolvedValue({ messages: [first, second], has_more: false })
    await open(); await query('result')
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(2)
    key('ArrowUp'); await nextTick()
    expect(input().getAttribute('aria-activedescendant')).toBe('search-result-1')
    key('ArrowDown'); await nextTick()
    expect(input().getAttribute('aria-activedescendant')).toBe('search-result-0')
    key('ArrowDown', requireValue(document.querySelector<HTMLSelectElement>('select'))); await nextTick()
    expect(input().getAttribute('aria-activedescendant')).toBe('search-result-0')
    key('x'); key('Enter', requireValue(document.querySelector<HTMLButtonElement>('[role="option"]')))
    expect(chat.goToMessage).not.toHaveBeenCalled()
    key('Enter'); await nextTick()
    expect(chat.goToMessage).toHaveBeenCalledWith(first)
    click('#search-result-1')
    expect(chat.goToMessage).toHaveBeenLastCalledWith(second)
  })

  it('paginates with a before cursor and keeps results after pagination failure', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const page = deferred<{ messages: ReturnType<typeof messageFixture>[]; has_more: boolean }>()
    mockApi.mockResolvedValueOnce({ messages: [messageFixture()], has_more: true }).mockImplementationOnce(() => page.promise).mockRejectedValueOnce(new Error('synthetic pagination failure'))
    await open(); await query('hello')
    const more = requireValue([...document.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent.includes('Mehr laden')))
    more.click(); await nextTick()
    expect(more.disabled).toBe(true)
    more.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(mockApi).toHaveBeenCalledTimes(2)
    expect(mockApi.mock.calls[1]?.[0]).toContain('before=' + messageFixture().id)
    page.resolve({ messages: [messageFixture({ id: fixtureId(50) })], has_more: true }); await flushPromises()
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(2)
    more.click(); await flushPromises()
    expect(warn).toHaveBeenCalled()
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(2)
    vi.restoreAllMocks()
  })

  it('ignores stale successful and failed searches and refreshes retained criteria when reopening', async () => {
    const old = deferred<{ messages: ReturnType<typeof messageFixture>[]; has_more: boolean }>()
    const current = deferred<{ messages: ReturnType<typeof messageFixture>[]; has_more: boolean }>()
    mockApi.mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise).mockResolvedValue({ messages: [], has_more: false })
    await open(); await query('old'); await query('new')
    old.resolve({ messages: [messageFixture()], has_more: true }); await flushPromises()
    expect(document.querySelector('[role="option"]')).toBeNull()
    current.reject(new Error('synthetic search failure'))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    await flushPromises()
    expect(document.querySelector('[role="alert"]')).not.toBeNull()
    await wrapper.setProps({ modelValue: false }); await wrapper.setProps({ modelValue: true }); await flushPromises()
    expect(mockApi.mock.calls.at(-1)?.[0]).toContain('q=new')
    expect(document.querySelector('[role="alert"]')).toBeNull()
    vi.restoreAllMocks()
    expect(warn).toHaveBeenCalled()
  })

  it('GlobalSearch binds modal dismissal to shared state', async () => {
    searchOpen.value = true
    wrapper = mount(GlobalSearch, { attachTo: document.body })
    await nextTick()
    click('[data-dialog-close]'); await nextTick()
    expect(searchOpen.value).toBe(false)
    expect(dialog()).toBeNull()
  })
})

it('ignores a stale failed search and stale paginated results after clearing criteria', async () => {
  const old = deferred<{ messages: ReturnType<typeof messageFixture>[]; has_more: boolean }>()
  const page = deferred<{ messages: ReturnType<typeof messageFixture>[]; has_more: boolean }>()
  mockApi.mockImplementationOnce(() => old.promise).mockResolvedValueOnce({ messages: [messageFixture()], has_more: true }).mockImplementationOnce(() => page.promise)
  await open(); await query('old'); await query('current')
  old.reject(new Error('synthetic obsolete failure')); await flushPromises()
  expect(document.querySelector('[role="alert"]')).toBeNull()
  const more = requireValue([...document.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent.includes('Mehr laden')))
  more.click(); await nextTick()
  await query('')
  page.resolve({ messages: [messageFixture()], has_more: true }); await flushPromises()
  expect(document.querySelector('[role="option"]')).toBeNull()
  expect(document.querySelector('[role="alert"]')).toBeNull()
})

it('does not paginate an empty result page even if the server advertises more', async () => {
  mockApi.mockResolvedValue({ messages: [], has_more: true })
  await open(); await query('empty')
  const more = requireValue([...document.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent.includes('Mehr laden')))
  more.click(); await flushPromises()
  expect(mockApi).toHaveBeenCalledTimes(1)
})

it('searches by channel alone without sending an empty text query', async () => {
  const ch = channelFixture()
  useChatStore().uncategorized = [ch]
  mockApi.mockResolvedValue({ messages: [], has_more: false })
  await open()
  const channel = requireValue(document.querySelector<HTMLSelectElement>('[role="dialog"] select'))
  channel.value = ch.id
  channel.dispatchEvent(new Event('change', { bubbles: true }))
  await flushPromises()
  const request = requireValue(mockApi.mock.calls.at(-1))
  const params = new URL(request[0], 'http://localhost').searchParams
  expect(params.get('channel_id')).toBe(ch.id)
  expect(params.has('q')).toBe(false)
})
