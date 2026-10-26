import type { VueWrapper, DOMWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import type { Channel, ChannelType, Category } from '../types/domain'
import type { ConfirmRequest } from '../lib/confirm'
import { userFixture, voiceUserFixture, channelFixture, categoryFixture, readStateFixture, requireValue, parseLayoutRequestBody } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { COLLAPSED_KEY } from '../lib/channelTree'
import { DRAG_DEFAULTS } from '../composables/useSortableDrag'

// GET /api/channels answers from `server`; PUT /api/admin/layout and the
// duplicate endpoint wait until the test answers them.
interface Tree { uncategorized: Channel[]; categories: Category[] }
interface Call { url: string; method: string; json: unknown }
interface Waiting { url: string; json: unknown; resolve: (value: unknown) => void; reject: (reason: unknown) => void }
const h = vi.hoisted((): { server: Tree; waiting: Waiting[]; calls: Call[] } => ({ server: { uncategorized: [], categories: [] }, waiting: [], calls: [] }))
const layoutBody = parseLayoutRequestBody
function layoutCall(call: Call | undefined) { const c = requireValue(call); return { ...c, json: layoutBody(c.json) } }
function q<T extends Element = HTMLElement>(selector: string): T { return requireValue(document.querySelector<T>(selector)) }

function isWaitingEndpoint(url: string, method?: string): boolean {
  if (url === '/api/admin/layout' || url.endsWith('/duplicate')) return true
  return url === '/api/admin/categories' && method === 'POST'
}

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, opts: ApiOptions = {}) => {
    h.calls.push({ url, method: opts.method || 'GET', json: opts.json })
    if (url === '/api/channels') return Promise.resolve(JSON.parse(JSON.stringify(h.server)))
    if (isWaitingEndpoint(url, opts.method)) {
      return new Promise((resolve, reject) => h.waiting.push({ url, json: opts.json, resolve, reject }))
    }
    return Promise.resolve(null)
  })
}))

const joinVoiceChannel = vi.fn()
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ joinVoiceChannel }) }))

const confirmMock = vi.fn<(_options: ConfirmRequest) => Promise<boolean>>(() => Promise.resolve(true))
vi.mock('../lib/confirm', () => ({ confirm: (options: ConfirmRequest) => confirmMock(options) }))

import Sidebar from './Sidebar.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { navigate } from '../lib/router'
import { applyLayoutPayload } from '../lib/channelLayout'

const ch = (id: string, category_id: string | null, sort_order: number, type: ChannelType = 'text') => channelFixture({ id, name: id, type, category_id, sort_order })

// welcome | Text: general random | Voice: lounge(voice) | Empty
function serverData() {
  return {
    uncategorized: [ch('welcome', null, 0)],
    categories: [
      categoryFixture({ id: 'text', name: 'Text', sort_order: 0, channels: [ch('general', 'text', 0), ch('random', 'text', 1)] }),
      categoryFixture({ id: 'voice', name: 'Voice', sort_order: 1, channels: [ch('lounge', 'voice', 0, 'voice')] }),
      categoryFixture({ id: 'empty', name: 'Empty', sort_order: 2, channels: [] })
    ]
  }
}

let wrapper: VueWrapper
let auth: ReturnType<typeof useAuthStore>
let chat: ReturnType<typeof useChatStore>
let voice: ReturnType<typeof useVoiceStore>
let toasts: ReturnType<typeof useToastStore>

async function mountSidebar({ role = 'admin', collapsed = [] }: { role?: 'admin' | 'user'; collapsed?: string[] } = {}) {
  localStorage.setItem(COLLAPSED_KEY, JSON.stringify(collapsed))
  auth = useAuthStore()
  chat = useChatStore()
  voice = useVoiceStore()
  toasts = useToastStore()
  auth.user = userFixture({ id: 'me', username: 'max', role })
  h.server = serverData()
  chat.categories = serverData().categories
  chat.uncategorized = serverData().uncategorized
  chat.activeChannel = requireValue(chat.categories[0]!.channels[0])
  voice.activeView = 'chat'
  wrapper = mount(Sidebar, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

// ---- DOM helpers ----

const nav = () => wrapper.find('nav')
const rowOf = (id: string) => wrapper.find(`[data-drop="channel"][data-id="${id}"]`)
// The row and its action buttons (chat, delete) are siblings inside this wrapper.
const itemOf = (id: string) => wrapper.find(`[data-channel-item="${id}"]`)
const headerOf = (id: string) => wrapper.find(`[data-drop="header"][data-id="${id}"]`)
const sectionIds = () => wrapper.findAll('[data-drop-section]').map(s => requireValue(s.attributes('data-drop-section')))
const channelIds = (sectionId: string) =>
  wrapper.find(`[data-drop-section="${sectionId}"]`).findAll('[data-drop="channel"]').map(r => r.attributes('data-id'))
const order = () => sectionIds().map(s => `${s === '__uncategorized' ? '' : `${s}: `}${channelIds(s).join(' ')}`).join(' | ')
const flush = async () => {
  for (let i = 0; i < 4; i++) await flushPromises()
}

// happy-dom has no layout. Lay the sidebar out like the browser would: rows
// stacked from y = 0 (header 24px, row 34px, voice users 32px, "no channels"
// 28px, 12px between sections), 240px wide.
const SIZES = { header: 24, channel: 34, 'channel-tail': 32, empty: 28 }
function layoutDom() {
  const rect = (top: number, height: number) => new DOMRect(0, top, 240, height)
  let y = 0
  for (const section of nav().element.querySelectorAll('[data-drop-section]')) {
    const top = y
    for (const el of section.querySelectorAll('[data-drop]')) {
      const r = rect(y, requireValue(Object.entries(SIZES).find(([key]) => key === el.getAttribute('data-drop')))[1])
      el.getBoundingClientRect = () => r
      y += r.height
    }
    const r = rect(top, y - top)
    section.getBoundingClientRect = () => r
    y += 12
  }
  const tail = nav().element.querySelector('[data-drop-tail]')
  if (tail) {
    const t = rect(y, Math.max(48, 1000 - y))
    tail.getBoundingClientRect = () => t
  }
  const r = rect(0, 1000)
  nav().element.getBoundingClientRect = () => r
}
const tailOf = () => wrapper.find('[data-drop-tail]')
// Middle of an element's top or bottom half.
function yIn(el: DOMWrapper<Element>, part = 0.25) {
  layoutDom()
  const r = el.element.getBoundingClientRect()
  return r.top + r.height * part
}

interface PointerOptions { x?: number; y?: number; pointerType?: string }
function pointerEvent(type: string, { x = 10, y = 0, pointerType = 'mouse' }: PointerOptions = {}) {
  const e = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, button: 0 })
  Object.defineProperty(e, 'pointerId', { value: 1 })
  Object.defineProperty(e, 'pointerType', { value: pointerType })
  Object.defineProperty(e, 'isPrimary', { value: true })
  return e
}

// Mouse drag from an element to a y position; returns before the drop.
async function dragTo(source: DOMWrapper<Element>, y: number, opts: PointerOptions = {}) {
  layoutDom()
  const start = yIn(source, 0.5)
  source.element.dispatchEvent(pointerEvent('pointerdown', { y: start, ...opts }))
  window.dispatchEvent(pointerEvent('pointermove', { y: start + DRAG_DEFAULTS.mouseThreshold + 1, ...opts }))
  await nextTick()
  layoutDom()
  window.dispatchEvent(pointerEvent('pointermove', { y, ...opts }))
  await nextTick()
}
async function drop(y: number, opts: PointerOptions = {}) {
  window.dispatchEvent(pointerEvent('pointerup', { y, ...opts }))
  await nextTick()
}

// The server keeps what a PUT sends (new places, dense sort orders).
function store(payload: unknown) {
  const t = applyLayoutPayload(h.server, layoutBody(payload))
  t.categories.forEach((c, i) => {
    c.sort_order = i
    c.channels = c.channels.map((x, j) => ({ ...x, category_id: c.id, sort_order: j }))
  })
  t.uncategorized = t.uncategorized.map((x, j) => ({ ...x, category_id: null, sort_order: j }))
  h.server = JSON.parse(JSON.stringify(t))
}
async function answer(match: string, { ok = true, body = null }: { ok?: boolean; body?: unknown } = {}) {
  const i = h.waiting.findIndex(w => w.url.includes(match))
  if (i < 0) throw new Error(`nothing waiting for ${match}`)
  const w = requireValue(h.waiting.splice(i, 1)[0])
  if (ok) {
    if (w.url === '/api/admin/layout') store(w.json)
    w.resolve(body)
  } else {
    w.reject(new Error('Nicht gefunden'))
  }
  await flush()
  return w
}
const puts = () => h.calls.filter(c => c.url === '/api/admin/layout').map(layoutCall)

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  h.waiting.length = 0
  h.calls.length = 0
  joinVoiceChannel.mockClear()
  confirmMock.mockClear()
  localStorage.clear()
})

afterEach(() => {
  wrapper?.unmount()

  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('Sidebar basics', () => {
  it('lists uncategorized channels first, then the categories in order', async () => {
    await mountSidebar({ role: 'user' })
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
    expect(wrapper.text()).toContain('Keine Kanäle')
  })

  it('opens a text channel and joins a voice channel on click', async () => {
    await mountSidebar({ role: 'user' })
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(async () => {})
    await rowOf('random').trigger('click')
    expect(select).toHaveBeenCalledWith(expect.objectContaining({ id: 'random' }))
    expect(voice.activeView).toBe('chat')
    expect(window.location.pathname).toBe('/c/random')
    await rowOf('lounge').trigger('click')
    await flushPromises()
    expect(joinVoiceChannel).toHaveBeenCalledWith('lounge')
    expect(voice.activeView).toBe('voice')
  })

  it('opens a Talk\'s chat from its row without joining the Talk', async () => {
    await mountSidebar({ role: 'user' })
    const button = itemOf('lounge').get('[data-testid="talk-open-chat"]')
    expect(button.attributes('aria-label')).toBe('Chat öffnen')
    expect(itemOf('general').find('[data-testid="talk-open-chat"]').exists()).toBe(false)
    await button.trigger('click')
    await flushPromises()
    expect(window.location.pathname).toBe('/v/lounge/chat')
    expect(joinVoiceChannel).not.toHaveBeenCalled()
    expect(voice.currentChannelId).toBeNull()
    expect(confirmMock).not.toHaveBeenCalled()
  })

  it('shows unread and mention badges and the people in a voice channel', async () => {
    await mountSidebar({ role: 'user' })
    chat.readStates = { random: readStateFixture({ unread_count: 3, mention_count: 0 }), welcome: readStateFixture({ unread_count: 5, mention_count: 2 }) }
    voice.channelUsers = { lounge: { u2: voiceUserFixture({ id: 'u2', username: 'zoe', display_name: 'Zoe', role: 'user' }) } }
    await nextTick()
    expect(rowOf('random').text()).toContain('3')
    expect(rowOf('welcome').text()).toContain('2')
    expect(wrapper.find('[data-voice-user]').text()).toContain('Zoe')
  })

  it('collapses a category (kept per user) but keeps the open channel visible', async () => {
    await mountSidebar({ role: 'user' })
    await headerOf('text').find('button').trigger('click')
    expect(channelIds('text')).toEqual(['general'])
    expect(JSON.parse(requireValue(localStorage.getItem(COLLAPSED_KEY)))).toEqual(['text'])
    await headerOf('text').find('button').trigger('click')
    expect(channelIds('text')).toEqual(['general', 'random'])
  })

  it('members get no editing controls and cannot drag', async () => {
    await mountSidebar({ role: 'user' })
    expect(wrapper.find('[aria-label="Kanal löschen"]').exists()).toBe(false)
    expect(wrapper.find('[aria-label="Kategorie löschen"]').exists()).toBe(false)
    await dragTo(rowOf('random'), yIn(rowOf('general'), 0.2))
    expect(document.querySelector('[data-testid="drag-ghost"]')).toBeNull()
    await drop(0)
    expect(puts()).toHaveLength(0)
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
  })
})

describe('Sidebar drag and drop (admin)', () => {
  it('reorders a channel within its category and saves at once', async () => {
    await mountSidebar()
    await dragTo(rowOf('random'), yIn(rowOf('general'), 0.2))
    expect(q('[data-testid="drag-ghost"]').textContent).toContain('random')
    expect(rowOf('general').element.closest('[data-channel-item]')!.querySelector('[data-drop-indicator]')).not.toBeNull()
    await drop(yIn(rowOf('general'), 0.2))

    expect(order()).toBe('welcome | text: random general | voice: lounge | empty: ')
    expect(puts()).toHaveLength(1)
    expect(puts()[0]!.json.channels.filter(c => c.category_id === 'text')).toEqual([
      { id: 'random', category_id: 'text', sort_order: 0 },
      { id: 'general', category_id: 'text', sort_order: 1 }
    ])
    expect(document.querySelector('[data-testid="drag-ghost"]')).toBeNull()

    await answer('/api/admin/layout')
    expect(order()).toBe('welcome | text: random general | voice: lounge | empty: ')
    const toast = toasts.toasts.find(t => t.action)
    expect(toast!.text).toBe('Kanal verschoben')
    expect(toast!.action!.label).toBe('Rückgängig')
    expect(wrapper.find('[data-testid="sidebar-announcer"]').text())
      .toBe('random verschoben: Position 1 von 2 in Text')
  })

  it('moves a channel into another category, into the uncategorized group and into an empty category', async () => {
    await mountSidebar()
    await dragTo(rowOf('general'), yIn(rowOf('lounge'), 0.75))
    await drop(yIn(rowOf('lounge'), 0.75))
    expect(order()).toBe('welcome | text: random | voice: lounge general | empty: ')
    await answer('/api/admin/layout')

    // Upper half of the first category header: the end of the uncategorized group.
    await dragTo(rowOf('random'), yIn(headerOf('text'), 0.2))
    await drop(yIn(headerOf('text'), 0.2))
    expect(order()).toBe('welcome random | text:  | voice: lounge general | empty: ')
    await answer('/api/admin/layout')

    await dragTo(rowOf('welcome'), yIn(wrapper.find('[data-drop="empty"][data-id="empty"]'), 0.5))
    await drop(0)
    expect(puts().at(-1)!.json.channels.find(c => c.id === 'welcome')).toEqual({ id: 'welcome', category_id: 'empty', sort_order: 0 })
  })

  it('drops onto a collapsed category: appended there, which stays collapsed', async () => {
    await mountSidebar({ collapsed: ['voice'] })
    await dragTo(rowOf('random'), yIn(headerOf('voice'), 0.6))
    expect(headerOf('voice').classes()).toContain('ring-mnema-accent')
    await drop(yIn(headerOf('voice'), 0.6))
    expect(puts()[0]!.json.channels.filter(c => c.category_id === 'voice').map(c => c.id)).toEqual(['lounge', 'random'])
    expect(channelIds('voice')).toEqual([])
    expect(JSON.parse(requireValue(localStorage.getItem(COLLAPSED_KEY)))).toEqual(['voice'])
  })

  it('opens a collapsed category held over for a moment; dropping inside keeps it open', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    await mountSidebar({ collapsed: ['voice'] })
    await dragTo(rowOf('random'), yIn(headerOf('voice'), 0.6))
    expect(channelIds('voice')).toEqual([])
    vi.advanceTimersByTime(600)
    await nextTick()
    expect(channelIds('voice')).toEqual(['lounge'])
    layoutDom()
    window.dispatchEvent(pointerEvent('pointermove', { y: yIn(rowOf('lounge'), 0.2) }))
    await nextTick()
    await drop(yIn(rowOf('lounge'), 0.2))
    expect(channelIds('voice')).toEqual(['random', 'lounge'])
    expect(JSON.parse(requireValue(localStorage.getItem(COLLAPSED_KEY)))).toEqual([])
  })

  it('the empty tail of the list appends to the last group', async () => {
    await mountSidebar()
    await dragTo(rowOf('random'), yIn(tailOf(), 0.5))
    expect(headerOf('empty').find('[data-drop-indicator]').exists()).toBe(true)
    await drop(yIn(tailOf(), 0.5))
    expect(channelIds('empty')).toEqual(['random'])
    expect(puts()[0]!.json.channels.find(c => c.id === 'random')!.category_id).toBe('empty')
  })

  it('holding a channel in the empty tail does not open a collapsed last category; its header does', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    await mountSidebar({ collapsed: ['empty'] })
    const expanded = () => wrapper.find('[data-category-toggle="empty"]').attributes('aria-expanded')
    await dragTo(rowOf('random'), yIn(tailOf(), 0.5))
    // Dropping there would append to it ...
    expect(headerOf('empty').classes()).toContain('ring-mnema-accent')
    vi.advanceTimersByTime(1000)
    await nextTick()
    // ... but it stays closed while the pointer is not on its header.
    expect(expanded()).toBe('false')
    layoutDom()
    window.dispatchEvent(pointerEvent('pointermove', { y: yIn(headerOf('empty'), 0.6) }))
    await nextTick()
    vi.advanceTimersByTime(600)
    await nextTick()
    expect(expanded()).toBe('true')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(expanded()).toBe('false')
  })

  it('dropping in the empty tail onto a collapsed last category appends there and keeps it collapsed', async () => {
    await mountSidebar({ collapsed: ['empty'] })
    await dragTo(rowOf('random'), yIn(tailOf(), 0.5))
    await drop(yIn(tailOf(), 0.5))
    expect(puts()[0]!.json.channels.find(c => c.id === 'random')!.category_id).toBe('empty')
    expect(JSON.parse(requireValue(localStorage.getItem(COLLAPSED_KEY)))).toEqual(['empty'])
  })

  it('a category opened by hovering closes again when the channel goes elsewhere', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    await mountSidebar({ collapsed: ['voice'] })
    await dragTo(rowOf('random'), yIn(headerOf('voice'), 0.6))
    vi.advanceTimersByTime(600)
    await nextTick()
    expect(channelIds('voice')).toEqual(['lounge'])
    layoutDom()
    window.dispatchEvent(pointerEvent('pointermove', { y: yIn(rowOf('welcome'), 0.2) }))
    await nextTick()
    await drop(yIn(rowOf('welcome'), 0.2))
    expect(channelIds('voice')).toEqual([])
    expect(order().startsWith('random welcome')).toBe(true)
  })

  it('reorders categories by their header; the dragged one folds up meanwhile', async () => {
    await mountSidebar()
    await dragTo(headerOf('empty'), yIn(headerOf('text'), 0.1))
    expect(channelIds('empty')).toEqual([])
    expect(wrapper.find('[data-drop="empty"]').exists()).toBe(false)
    expect(q('[data-testid="drag-ghost"]').textContent).toContain('Empty')
    await drop(yIn(headerOf('text'), 0.1))
    expect(sectionIds()).toEqual(['__uncategorized', 'empty', 'text', 'voice'])
    expect(puts()[0]!.json.categories).toEqual([
      { id: 'empty', sort_order: 0 }, { id: 'text', sort_order: 1 }, { id: 'voice', sort_order: 2 }
    ])
    await answer('/api/admin/layout')
    expect(toasts.toasts.find(t => t.action)!.text).toBe('Kategorie verschoben')
    expect(wrapper.find('[data-testid="sidebar-announcer"]').text()).toBe('Empty verschoben: Position 1 von 3')
  })

  it('rolls back and reports an error when saving fails', async () => {
    await mountSidebar()
    await dragTo(rowOf('welcome'), yIn(rowOf('random'), 0.8))
    await drop(yIn(rowOf('random'), 0.8))
    expect(order()).toBe('text: general random welcome | voice: lounge | empty: ')
    await answer('/api/admin/layout', { ok: false })
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
    expect(toasts.toasts.find(t => t.type === 'error')!.text).toBe('Die neue Reihenfolge konnte nicht gespeichert werden. Versuche es noch einmal.')
    expect(toasts.toasts.some(t => t.action)).toBe(false)
  })

  it('undo restores the previous order with another save', async () => {
    await mountSidebar()
    await dragTo(rowOf('lounge'), yIn(rowOf('general'), 0.2))
    await drop(yIn(rowOf('general'), 0.2))
    await answer('/api/admin/layout')
    expect(order()).toBe('welcome | text: lounge general random | voice:  | empty: ')

    toasts.toasts.find(t => t.action)!.action!.onClick()
    await nextTick()
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
    const sent = await answer('/api/admin/layout')
    expect(layoutBody(sent.json).channels.find(c => c.id === 'lounge')).toEqual({ id: 'lounge', category_id: 'voice', sort_order: 0 })
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
  })

  it('a second drop while saving is queued, and a refetch in between does not flash the old order', async () => {
    await mountSidebar()
    await dragTo(rowOf('random'), yIn(rowOf('general'), 0.2))
    await drop(yIn(rowOf('general'), 0.2))
    await dragTo(rowOf('welcome'), yIn(rowOf('lounge'), 0.8))
    await drop(yIn(rowOf('lounge'), 0.8))
    expect(puts()).toHaveLength(1)
    // channels_changed from somewhere else arrives with the old order.
    chat.handleWSEvent({ type: 'channels_changed', payload: null })
    await flush()
    expect(order()).toBe('text: random general | voice: lounge welcome | empty: ')

    await answer('/api/admin/layout')
    expect(puts()).toHaveLength(2)
    await answer('/api/admin/layout')
    expect(order()).toBe('text: random general | voice: lounge welcome | empty: ')
    expect(chat.categories[1]!.channels.map(c => c.id)).toEqual(['lounge', 'welcome'])
  })

  it('Escape cancels a drag; dropping outside the list does nothing', async () => {
    await mountSidebar()
    await dragTo(rowOf('random'), yIn(rowOf('general'), 0.2))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await nextTick()
    expect(document.querySelector('[data-testid="drag-ghost"]')).toBeNull()
    await drop(yIn(rowOf('general'), 0.2))

    await dragTo(rowOf('random'), yIn(rowOf('general'), 0.2))
    window.dispatchEvent(pointerEvent('pointermove', { x: 600, y: 20 }))
    await drop(20, { x: 600 })
    expect(puts()).toHaveLength(0)
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
  })

  it('the click that ends a drag does not open the channel', async () => {
    await mountSidebar()
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(async () => {})
    await dragTo(rowOf('random'), yIn(rowOf('general'), 0.2))
    await drop(yIn(rowOf('general'), 0.2))
    rowOf('random').element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(select).not.toHaveBeenCalled()
  })

  it('a plain click still opens the channel for admins', async () => {
    await mountSidebar()
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(async () => {})
    layoutDom()
    rowOf('random').element.dispatchEvent(pointerEvent('pointerdown', { y: 50 }))
    window.dispatchEvent(pointerEvent('pointerup', { y: 50 }))
    await rowOf('random').trigger('click')
    expect(select).toHaveBeenCalledWith(expect.objectContaining({ id: 'random' }))
  })

  it('touch: a long press picks a row up, lifting without moving opens its menu', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    await mountSidebar()
    layoutDom()
    const y = yIn(rowOf('random'), 0.5)
    rowOf('random').element.dispatchEvent(pointerEvent('pointerdown', { y, pointerType: 'touch' }))
    vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs)
    await nextTick()
    expect(document.querySelector('[data-testid="drag-ghost"]')).not.toBeNull()
    window.dispatchEvent(pointerEvent('pointerup', { y, pointerType: 'touch' }))
    await flush()
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
    expect(q('[role="menu"]').textContent).toContain('Kanal bearbeiten')
    expect(puts()).toHaveLength(0)
  })
})

// ---- Context menus ----

async function contextMenu(el: DOMWrapper<Element> | HTMLElement) {
  const target = el instanceof HTMLElement ? el : el.element
  const e = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 5, clientY: 5 })
  target.dispatchEvent(e)
  await flush()
  return e
}
const menuItems = () => [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] [role^="menuitem"]')]
const menuLabels = () => menuItems().map(b => (b.textContent ?? '').trim())
async function choose(label: string) {
  const item = menuItems().find(b => (b.textContent ?? '').trim() === label)
  if (!item) throw new Error(`no menu item ${label}: ${menuLabels().join(', ')}`)
  item.click()
  await flush()
}

describe('Sidebar administrative lifecycle and navigation', () => {
  it('deletes text/voice channels and categories, handles cancellation and both error messages', async () => {
    await mountSidebar()
    const deleteChannel = vi.spyOn(chat, 'deleteChannel').mockResolvedValue()
    const deleteCategory = vi.spyOn(chat, 'deleteCategory').mockResolvedValue()
    confirmMock.mockResolvedValueOnce(false)
    await itemOf('general').find('button').trigger('click'); await flush()
    expect(deleteChannel).not.toHaveBeenCalled()
    await itemOf('general').find('button').trigger('click'); await flush()
    expect(deleteChannel).toHaveBeenCalledWith('general')
    expect(toasts.toasts.at(-1)?.text).toBe('Kanal gelöscht')
    await itemOf('lounge').find('[aria-label="Talk löschen"]').trigger('click'); await flush()
    expect(confirmMock.mock.calls.at(-1)?.[0].title).toContain('Talk')
    deleteChannel.mockRejectedValueOnce(new Error('offline')).mockRejectedValueOnce(new Error(''))
    await itemOf('general').find('button').trigger('click'); await flush(); expect(toasts.toasts.at(-1)?.text).toBe('offline')
    await itemOf('general').find('button').trigger('click'); await flush(); expect(toasts.toasts.at(-1)?.type).toBe('error')
    confirmMock.mockResolvedValueOnce(false)
    await headerOf('text').findAll('button')[2]!.trigger('click'); await flush(); expect(deleteCategory).not.toHaveBeenCalled()
    await headerOf('text').findAll('button')[2]!.trigger('click'); await flush(); expect(deleteCategory).toHaveBeenCalledWith('text')
    deleteCategory.mockRejectedValueOnce(new Error('category failed')).mockRejectedValueOnce(new Error(''))
    await headerOf('text').findAll('button')[2]!.trigger('click'); await flush(); expect(toasts.toasts.at(-1)?.text).toBe('category failed')
    await headerOf('text').findAll('button')[2]!.trigger('click'); await flush(); expect(toasts.toasts.at(-1)?.type).toBe('error')
  })
  it('routes context-menu deletion to the correct entity', async () => {
    await mountSidebar()
    const deleteChannel = vi.spyOn(chat, 'deleteChannel').mockResolvedValue(); const deleteCategory = vi.spyOn(chat, 'deleteCategory').mockResolvedValue()
    await contextMenu(rowOf('general')); await choose('Kanal löschen'); expect(deleteChannel).toHaveBeenCalledWith('general')
    await contextMenu(headerOf('voice')); await choose('Kategorie löschen'); expect(deleteCategory).toHaveBeenCalledWith('voice')
  })
  it('opens category and channel editors and closes their dialogs', async () => {
    await mountSidebar()
    await contextMenu(rowOf('general')); await choose('Kanal bearbeiten'); expect(q<HTMLInputElement>('#edit-name').value).toBe('general')
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen')!.trigger('click'); expect(document.querySelector('#edit-name')).toBeNull()
    await contextMenu(headerOf('text')); await choose('Kategorie bearbeiten'); expect(q<HTMLInputElement>('#edit-name').value).toBe('Text')
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen')!.trigger('click')
  })
  it('the category plus suggests voice only for voice-only categories and closes the create modal', async () => {
    await mountSidebar()
    await headerOf('voice').findAll('button')[1]!.trigger('click'); expect(q<HTMLSelectElement>('#channel-category').value).toBe('voice')
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen')!.trigger('click'); expect(document.querySelector('#channel-category')).toBeNull()
    await headerOf('empty').findAll('button')[1]!.trigger('click'); expect(q('[aria-pressed="true"]').textContent).toContain('Nachrichten und Dateien')
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen')!.trigger('click')
    await headerOf('text').findAll('button')[1]!.trigger('click'); expect(q('[aria-pressed="true"]').textContent).toContain('Nachrichten und Dateien')
  })
  it('confirms a voice switch, honours cancel and retains the voice-chat route', async () => {
    await mountSidebar({ role: 'user' }); vi.spyOn(chat, 'selectChannel').mockResolvedValue()
    voice.setChannel('other'); voice.warnSwitchChannel = true
    confirmMock.mockResolvedValueOnce(false)
    await rowOf('lounge').trigger('click'); await flush(); expect(joinVoiceChannel).not.toHaveBeenCalled()
    await rowOf('lounge').trigger('click'); await flush(); expect(joinVoiceChannel).toHaveBeenCalledWith('lounge')
    voice.setChannel('lounge'); await rowOf('lounge').trigger('click'); await flush(); expect(joinVoiceChannel).toHaveBeenCalledTimes(2)
    expect(confirmMock).toHaveBeenCalledTimes(2)
  })
  it('opens ordinary member profiles, their menus and live screens inside or outside the current Talk', async () => {
    await mountSidebar({ role: 'user' }); vi.spyOn(chat, 'selectChannel').mockResolvedValue()
    const person = voiceUserFixture({ id: 'viewer', username: 'viewer' }); voice.channelUsers.lounge = { viewer: person }
    const profile = vi.spyOn(chat, 'openUserProfile').mockResolvedValue(); const watch = vi.spyOn(voice, 'watchScreen')
    await nextTick(); await wrapper.find('[data-voice-user]').trigger('click'); expect(profile).toHaveBeenCalledWith(person)
    await wrapper.find('[data-voice-user]').trigger('contextmenu'); await flush(); expect(menuLabels()).toContain('Profil ansehen')
    voice.mediaState.viewer = { channel_id: 'lounge', screen: true, camera: false }
    await nextTick(); await wrapper.find('[data-voice-user]').trigger('click'); await flush(); expect(joinVoiceChannel).toHaveBeenCalledWith('lounge'); expect(watch).toHaveBeenCalledWith('viewer')
    voice.setChannel('lounge'); voice.activeView = 'chat'; await wrapper.find('[data-voice-user]').trigger('click'); await flush(); expect(voice.activeView).toBe('voice')
  })
  it('keeps a connected voice row visible inside its collapsed category', async () => {
    await mountSidebar({ collapsed: ['voice'] }); voice.setChannel('lounge'); await nextTick(); expect(channelIds('voice')).toEqual(['lounge'])
  })
  it('supports every server-menu key, focus return, admin/legal/create actions and role-change closure', async () => {
    await mountSidebar()
    const button = wrapper.find('[aria-controls="server-menu"]')
    await button.trigger('keydown', { key: 'ArrowDown' }); await flush(); const menu = wrapper.find('#server-menu'); const items = menu.findAll('[role="menuitem"]')
    expect(document.activeElement).toBe(items[0]!.element)
    await menu.trigger('keydown', { key: 'ArrowDown' }); expect(document.activeElement).toBe(items[1]!.element)
    await menu.trigger('keydown', { key: 'ArrowUp' }); expect(document.activeElement).toBe(items[0]!.element)
    await menu.trigger('keydown', { key: 'End' }); expect(document.activeElement).toBe(items.at(-1)!.element)
    await menu.trigger('keydown', { key: 'Home' }); expect(document.activeElement).toBe(items[0]!.element)
    await menu.trigger('keydown', { key: 'Enter' }); expect(menu.exists()).toBe(true)
    await items[0]!.trigger('click'); expect(wrapper.emitted('open-admin')).toHaveLength(1); expect(document.activeElement).toBe(button.element)
    await button.trigger('keydown', { key: 'ArrowUp' }); await flush(); await wrapper.find('#server-menu').findAll('[role="menuitem"]').at(-1)!.trigger('click'); expect(wrapper.emitted('open-legal')).toHaveLength(1)
    await button.trigger('click'); await wrapper.find('#server-menu').findAll('[role="menuitem"]')[1]!.trigger('click'); expect(document.querySelector('#channel-name')).not.toBeNull()
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen')!.trigger('click')
    await button.trigger('click'); await wrapper.find('#server-menu').trigger('keydown', { key: 'Tab' }); expect(wrapper.find('#server-menu').exists()).toBe(false)
    await button.trigger('click'); await button.trigger('click'); expect(wrapper.find('#server-menu').exists()).toBe(false)
    await button.trigger('click'); auth.user = userFixture({ role: 'user' }); await nextTick(); expect(wrapper.find('#server-menu').exists()).toBe(false)
  })
  it('exposes a category menu on touch long press without moving', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }); await mountSidebar(); layoutDom()
    const y = yIn(headerOf('voice'), 0.5)
    headerOf('voice').element.dispatchEvent(pointerEvent('pointerdown', { y, pointerType: 'touch' })); vi.advanceTimersByTime(DRAG_DEFAULTS.longPressMs); await nextTick()
    window.dispatchEvent(pointerEvent('pointerup', { y, pointerType: 'touch' })); await flush(); expect(menuLabels()).toContain('Kategorie bearbeiten')
  })
})

it('handles closed-menu role changes, ordinary header keys, missing active text and a voice-chat route', async () => {
  await mountSidebar({ collapsed: ['text'] })
  chat.activeChannel = null; await nextTick(); expect(channelIds('text')).toEqual([])
  auth.user = userFixture({ role: 'user' }); await nextTick(); expect(wrapper.find('#server-menu').exists()).toBe(false)
  const button = wrapper.find('[aria-controls="server-menu"]'); await button.trigger('keydown', { key: 'Enter' }); expect(wrapper.find('#server-menu').exists()).toBe(false)
  vi.spyOn(chat, 'selectChannel').mockResolvedValue(); navigate('/v/other/chat')
  await rowOf('lounge').trigger('click'); await flush(); expect(window.location.pathname).toBe('/v/lounge/chat')
})
it('folds a populated category during a drag and drops it after another category', async () => {
  await mountSidebar()
  await dragTo(headerOf('text'), yIn(headerOf('voice'), 0.9))
  expect(channelIds('text')).toEqual([])
  await drop(yIn(headerOf('voice'), 0.9)); expect(sectionIds()).toEqual(['__uncategorized', 'voice', 'text', 'empty'])
})

it('dismisses the server menu outside or with Escape and restores keyboard focus', async () => {
  await mountSidebar()
  const button = wrapper.find('[aria-controls="server-menu"]')
  await button.trigger('click'); await flush()
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await nextTick(); expect(wrapper.find('#server-menu').exists()).toBe(false); expect(document.activeElement).toBe(button.element)
  await button.trigger('click'); await flush()
  document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
  await nextTick(); expect(wrapper.find('#server-menu').exists()).toBe(false)
})
it('shows the legal action to ordinary members and stops queued focus after unmount', async () => {
  await mountSidebar({ role: 'user' })
  const button = wrapper.find<HTMLButtonElement>('[aria-controls="server-menu"]')
  await button.trigger('click'); await flush()
  expect(wrapper.findAll('#server-menu [role="menuitem"]').map(row => row.text())).toEqual(['Alle als gelesen markieren', 'Rechtliches & Datenschutz'])
  await button.trigger('click')
  button.element.click()
  wrapper.unmount()
  await nextTick()
  expect(document.querySelector('#server-menu')).toBeNull()
})
it('draws a bottom boundary when a category is dragged after the final section', async () => {
  await mountSidebar()
  await dragTo(headerOf('text'), yIn(tailOf(), 0.5))
  expect(wrapper.find('[data-drop-section="empty"] > [data-drop-indicator]').exists()).toBe(true)
  await drop(yIn(tailOf(), 0.5))
  expect(sectionIds()).toEqual(['__uncategorized', 'voice', 'empty', 'text'])
})
