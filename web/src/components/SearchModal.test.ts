import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import SearchModal from './SearchModal.vue'
import GlobalSearch from './GlobalSearch.vue'
import { searchOpen, searchText } from '../lib/globalSearch'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { channelFixture, messageFixture, userFixture, fixtureId, requireValue } from '../test-fixtures.fixture'
import { i18nPlugin, setLocale, t } from '../i18n'
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
  searchText.value = ''
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
  it('sends free text, turns filter tokens into chips and parameters, and clears an empty query', async () => {
    setLocale('en')
    const chat = useChatStore()
    chat.uncategorized = [channelFixture(), channelFixture({ id: fixtureId(30), name: 'Lounge', type: 'voice' })]
    chat.members = [userFixture(), userFixture({ id: fixtureId(31), username: 'ada', display_name: 'Displayed member' })]
    mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
    await open()
    key('ArrowDown')
    await query('  hello ')
    expect(mockApi).toHaveBeenLastCalledWith('/api/search?limit=25&q=hello', expect.any(Object))
    expect(dialog()!.textContent).toContain('No messages found')
    await query('hello in:Lounge from:ada has:video mentions:ada after:2026-01-02 before:2026-01-03 ')
    const params = new URL(requireValue(mockApi.mock.calls.at(-1))[0], 'http://localhost').searchParams
    expect(params.get('q')).toBe('hello')
    expect(params.get('channel_id')).toBe(fixtureId(30))
    expect(params.get('author_id')).toBe(fixtureId(31))
    expect(params.get('mentions')).toBe(fixtureId(31))
    expect(params.get('has')).toBe('video')
    expect(new Date(requireValue(params.get('before_date'))).getTime()).toBeGreaterThan(new Date(requireValue(params.get('after'))).getTime())
    expect(document.querySelectorAll('[data-search-chip]')).toHaveLength(6)
    expect(input().value).toBe('hello ')
    click('[data-search-chip] button')
    await new Promise(resolve => setTimeout(resolve, 280)); await flushPromises()
    expect(document.querySelectorAll('[data-search-chip]')).toHaveLength(5)
    expect(requireValue(mockApi.mock.calls.at(-1))[0]).not.toContain('author_id')
    await query('')
    for (const chip of [...document.querySelectorAll<HTMLButtonElement>('[data-search-chip] button')]) chip.click()
    await query('')
    expect(document.querySelector('[role="option"]')).toBeNull()
    expect(input().hasAttribute('aria-activedescendant')).toBe(false)
    click('[data-dialog-close]')
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
  })

  it('does not search for a filter that matches nothing and flags its chip', async () => {
    mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
    await open()
    await query('from:nobody ')
    expect(mockApi).not.toHaveBeenCalled()
    expect(document.querySelector('[data-search-chip]')!.className).toContain('danger')
  })

  it('shows the root snippet of a reply and highlights matches without HTML', async () => {
    const rootId = fixtureId(60)
    const reply = messageFixture({ parent_id: rootId, content: 'a <b>bold</b> Needle' })
    mockApi.mockResolvedValue({ messages: [reply], has_more: false, roots: { [rootId]: { id: rootId, channel_id: reply.channel_id, username: 'ada', display_name: 'Ada', snippet: 'the needle thread' }, junk: 5 } })
    await open(); await query('needle')
    const root = requireValue(document.querySelector('[data-search-root]'))
    expect(root.textContent).toContain('the needle thread')
    const item = requireValue(document.querySelector('#search-result-0'))
    expect(item.querySelector('b')).toBeNull()
    expect([...item.querySelectorAll('mark')].map(m => m.textContent)).toEqual(['needle', 'Needle'])
  })

  it('keeps the last query for the session', async () => {
    mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
    await open(); await query('remember me')
    await wrapper.setProps({ modelValue: false }); await wrapper.setProps({ modelValue: true }); await nextTick()
    expect(input().value).toBe('remember me')
  })

  it('navigates results, wraps selection, chooses by Enter or click', async () => {
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

it('pages the list on screen with its own query while a new one is still being typed', async () => {
  mockApi.mockResolvedValueOnce({ messages: [messageFixture()], has_more: true }).mockResolvedValue({ messages: [], has_more: false })
  await open(); await query('old')
  input().value = 'brand new'
  input().dispatchEvent(new Event('input'))
  const more = requireValue([...document.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent.includes('Mehr laden')))
  more.click(); await flushPromises()
  const page = new URLSearchParams(mockApi.mock.calls[1]![0].split('?')[1])
  expect(page.get('q')).toBe('old')
  expect(page.get('before')).toBe(messageFixture().id)
})

it('tells when another page could not be loaded', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {})
  mockApi.mockResolvedValueOnce({ messages: [messageFixture()], has_more: true }).mockRejectedValueOnce(new Error('offline'))
  await open(); await query('hello')
  requireValue([...document.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent.includes('Mehr laden'))).click()
  await flushPromises()
  expect(useToastStore().toasts.map(x => x.text)).toContain(t('chat.searchFailed'))
  vi.restoreAllMocks()
})

it('leaves Enter to the IME while a composition is committed', async () => {
  const chat = useChatStore()
  chat.goToMessage = vi.fn()
  mockApi.mockResolvedValue({ messages: [messageFixture()], has_more: false })
  await open(); await query('result')
  input().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true, cancelable: true }))
  input().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', keyCode: 229, bubbles: true, cancelable: true }))
  expect(chat.goToMessage).not.toHaveBeenCalled()
  key('Enter')
  expect(chat.goToMessage).toHaveBeenCalledTimes(1)
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
  mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
  await open()
  await query(`in:${ch.name} `)
  const request = requireValue(mockApi.mock.calls.at(-1))
  const params = new URL(request[0], 'http://localhost').searchParams
  expect(params.get('channel_id')).toBe(ch.id)
  expect(params.has('q')).toBe(false)
})

describe('SearchModal calm behaviour', () => {
  it('has a fixed height, so the dialog does not move when the content changes', async () => {
    await open()
    expect(dialog()!.className).toContain('h-192')
    expect(document.querySelector('[data-search-filter-row]')).toBeNull()
  })

  it('explains a filter that matches nothing and does not search at all', async () => {
    setLocale('en')
    useChatStore().members = [userFixture({ username: 'ada' })]
    mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
    await open()
    await query('Stand from:nobody ')
    expect(mockApi).not.toHaveBeenCalled()
    expect(requireValue(document.querySelector('[data-testid="search-problems"]')).textContent).toContain('No member “nobody” found')
    await query('Stand in:nowhere has:foo before:soon ')
    expect(mockApi).not.toHaveBeenCalled()
    expect(document.querySelectorAll('[data-search-problem]')).toHaveLength(4)
  })

  it('waits quietly while a filter value is still being typed', async () => {
    mockApi.mockResolvedValue({ messages: [messageFixture()], has_more: false, roots: {} })
    await open()
    await query('hello')
    expect(mockApi).toHaveBeenCalledTimes(1)
    await query('hello in:gen')
    await query('hello has:fo')
    expect(mockApi).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[data-testid="search-problems"]')).toBeNull()
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(1)
  })

  it('understands today and yesterday as dates', async () => {
    mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
    await open()
    await query('hello after:gestern ')
    const params = new URL(requireValue(mockApi.mock.calls.at(-1))[0], 'http://localhost').searchParams
    expect(params.get('after')).toBeTruthy()
    expect(document.querySelector('[data-testid="search-problems"]')).toBeNull()
  })

  it('does not search for a single character, and sends phrases and exclusions on as typed', async () => {
    mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
    await open()
    await query('a')
    expect(mockApi).not.toHaveBeenCalled()
    await query('"kurzer Stand" -Release')
    const params = new URL(requireValue(mockApi.mock.calls.at(-1))[0], 'http://localhost').searchParams
    expect(params.get('q')).toBe('"kurzer Stand" -Release')
  })

  it('retries a failed search with the same query', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    mockApi.mockRejectedValueOnce(new Error('synthetic failure')).mockResolvedValueOnce({ messages: [messageFixture()], has_more: false, roots: {} })
    await open()
    await query('hello')
    expect(requireValue(document.querySelector('[data-testid="search-failed"]')).textContent).toContain('Suche')
    click('[data-search-retry]')
    await flushPromises()
    expect(document.querySelector('[data-testid="search-failed"]')).toBeNull()
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(1)
    expect(mockApi).toHaveBeenCalledTimes(2)
    warn.mockRestore()
  })

  it('removes the filters from an empty result with one click', async () => {
    setLocale('en')
    const ch = channelFixture()
    useChatStore().uncategorized = [ch]
    mockApi.mockResolvedValue({ messages: [], has_more: false, roots: {} })
    await open()
    await query(`hello in:${ch.name} `)
    click('[data-search-clear-filters]')
    await new Promise(resolve => setTimeout(resolve, 280)); await flushPromises()
    expect(searchText.value).toBe('hello')
    expect(document.querySelectorAll('[data-search-chip]')).toHaveLength(0)
  })

  it('opens the selected result with Enter from the list, but leaves Enter on a button alone', async () => {
    const chat = useChatStore()
    chat.goToMessage = vi.fn()
    const first = messageFixture()
    useServerFeaturesStore().list = ['search.filters.v3']
    mockApi.mockResolvedValue({ messages: [first], has_more: false, roots: {} })
    await open(); await query('hello')
    const list = requireValue(document.querySelector<HTMLElement>('#search-results'))
    expect(list.getAttribute('tabindex')).toBe('0')
    key('Enter', requireValue(document.querySelector<HTMLElement>('[data-sort="oldest"]')))
    expect(chat.goToMessage).not.toHaveBeenCalled()
    key('Enter', list)
    expect(chat.goToMessage).toHaveBeenCalledWith(first)
  })

  it('moves through the results with the arrows from the sort choice, too', async () => {
    useServerFeaturesStore().list = ['search.filters.v3']
    mockApi.mockResolvedValue({ messages: [messageFixture(), messageFixture({ id: fixtureId(34), content: 'second hello' })], has_more: false, roots: {} })
    await open(); await query('hello')
    key('ArrowDown', requireValue(document.querySelector<HTMLElement>('[data-sort="newest"]')))
    await nextTick()
    expect(input().getAttribute('aria-activedescendant')).toBe('search-result-1')
  })

  it('lets Escape peel one layer at a time: suggestions first, then the dialog', async () => {
    useChatStore().members = [userFixture({ username: 'ada' })]
    await open()
    input().value = 'from:a'
    input().dispatchEvent(new Event('input'))
    await nextTick()
    expect(document.querySelector('[data-search-suggestions]')).not.toBeNull()
    key('Escape'); await nextTick()
    expect(document.querySelector('[data-search-suggestions]')).toBeNull()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    key('Escape')
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
  })

  it('does not lose a click on the sort choice: the first click sorts', async () => {
    useServerFeaturesStore().list = ['search.filters.v3']
    mockApi.mockResolvedValue({ messages: [messageFixture()], has_more: false, roots: {} })
    await open(); await query('hello')
    const oldest = requireValue(document.querySelector<HTMLElement>('[data-sort="oldest"]'))
    const down = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    oldest.dispatchEvent(down)
    expect(down.defaultPrevented).toBe(true)
    oldest.click()
    await new Promise(resolve => setTimeout(resolve, 10)); await flushPromises()
    expect(requireValue(mockApi.mock.calls.at(-1))[0]).toContain('sort=oldest')
  })
})
