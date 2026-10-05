import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { COLLAPSED_KEY } from '../lib/channelTree'
import { DRAG_DEFAULTS } from '../composables/useSortableDrag'

// GET /api/channels answers from `server`; PUT /api/admin/layout and the
// duplicate endpoint wait until the test answers them.
const h = vi.hoisted(() => ({ server: null, waiting: [], calls: [] }))
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal()),
  api: vi.fn((url, opts = {}) => {
    h.calls.push({ url, method: opts.method || 'GET', json: opts.json })
    if (url === '/api/channels') return Promise.resolve(JSON.parse(JSON.stringify(h.server)))
    if (url === '/api/admin/layout' || url.endsWith('/duplicate') || (url === '/api/admin/categories' && opts.method === 'POST')) {
      return new Promise((resolve, reject) => h.waiting.push({ url, json: opts.json, resolve, reject }))
    }
    return Promise.resolve(null)
  })
}))

const joinVoiceChannel = vi.fn()
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ joinVoiceChannel }) }))

const confirmMock = vi.fn(() => Promise.resolve(true))
vi.mock('../lib/confirm', () => ({ confirm: (...a) => confirmMock(...a) }))

import Sidebar from './Sidebar.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { applyLayoutPayload } from '../lib/channelLayout'

const ch = (id, category_id, sort_order, type = 'text') => ({ id, name: id, type, topic: '', category_id, sort_order })

// welcome | Text: general random | Voice: lounge(voice) | Empty
function serverData() {
  return {
    uncategorized: [ch('welcome', null, 0)],
    categories: [
      { id: 'text', name: 'Text', sort_order: 0, channels: [ch('general', 'text', 0), ch('random', 'text', 1)] },
      { id: 'voice', name: 'Voice', sort_order: 1, channels: [ch('lounge', 'voice', 0, 'voice')] },
      { id: 'empty', name: 'Empty', sort_order: 2, channels: [] }
    ]
  }
}

let wrapper, auth, chat, voice, toasts

async function mountSidebar({ role = 'admin', collapsed = [] } = {}) {
  localStorage.setItem(COLLAPSED_KEY, JSON.stringify(collapsed))
  auth = useAuthStore()
  chat = useChatStore()
  voice = useVoiceStore()
  toasts = useToastStore()
  auth.user = { id: 'me', username: 'max', role }
  h.server = serverData()
  chat.categories = serverData().categories
  chat.uncategorized = serverData().uncategorized
  chat.activeChannel = chat.categories[0].channels[0]
  voice.activeView = 'chat'
  wrapper = mount(Sidebar, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

// ---- DOM helpers ----

const nav = () => wrapper.find('nav')
const rowOf = id => wrapper.find(`[data-drop="channel"][data-id="${id}"]`)
const headerOf = id => wrapper.find(`[data-drop="header"][data-id="${id}"]`)
const sectionIds = () => wrapper.findAll('[data-drop-section]').map(s => s.attributes('data-drop-section'))
const channelIds = sectionId =>
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
  const rect = (top, height) => ({ top, bottom: top + height, height, left: 0, right: 240, width: 240, x: 0, y: top })
  let y = 0
  for (const section of nav().element.querySelectorAll('[data-drop-section]')) {
    const top = y
    for (const el of section.querySelectorAll('[data-drop]')) {
      const r = rect(y, SIZES[el.getAttribute('data-drop')])
      el.getBoundingClientRect = () => r
      y += r.height
    }
    const r = rect(top, y - top)
    section.getBoundingClientRect = () => r
    y += 12
  }
  const r = rect(0, 1000)
  nav().element.getBoundingClientRect = () => r
}
// Middle of an element's top or bottom half.
function yIn(el, part = 0.25) {
  layoutDom()
  const r = el.element.getBoundingClientRect()
  return r.top + r.height * part
}

function pointerEvent(type, { x = 10, y = 0, pointerType = 'mouse' } = {}) {
  const e = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, button: 0 })
  Object.defineProperty(e, 'pointerId', { value: 1 })
  Object.defineProperty(e, 'pointerType', { value: pointerType })
  Object.defineProperty(e, 'isPrimary', { value: true })
  return e
}

// Mouse drag from an element to a y position; returns before the drop.
async function dragTo(source, y, opts = {}) {
  layoutDom()
  const start = yIn(source, 0.5)
  source.element.dispatchEvent(pointerEvent('pointerdown', { y: start, ...opts }))
  window.dispatchEvent(pointerEvent('pointermove', { y: start + DRAG_DEFAULTS.mouseThreshold + 1, ...opts }))
  await nextTick()
  layoutDom()
  window.dispatchEvent(pointerEvent('pointermove', { y, ...opts }))
  await nextTick()
}
async function drop(y, opts = {}) {
  window.dispatchEvent(pointerEvent('pointerup', { y, ...opts }))
  await nextTick()
}

// The server keeps what a PUT sends (new places, dense sort orders).
function store(payload) {
  const t = applyLayoutPayload(h.server, payload)
  t.categories.forEach((c, i) => {
    c.sort_order = i
    c.channels = c.channels.map((x, j) => ({ ...x, category_id: c.id, sort_order: j }))
  })
  t.uncategorized = t.uncategorized.map((x, j) => ({ ...x, category_id: null, sort_order: j }))
  h.server = JSON.parse(JSON.stringify(t))
}
async function answer(match, { ok = true, body = null } = {}) {
  const i = h.waiting.findIndex(w => w.url.includes(match))
  if (i < 0) throw new Error(`nothing waiting for ${match}`)
  const [w] = h.waiting.splice(i, 1)
  if (ok) {
    if (w.url === '/api/admin/layout') store(w.json)
    w.resolve(body)
  } else {
    w.reject(new Error('Nicht gefunden'))
  }
  await flush()
  return w
}
const puts = () => h.calls.filter(c => c.url === '/api/admin/layout')

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
  wrapper = null
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
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(() => {})
    await rowOf('random').trigger('click')
    expect(select).toHaveBeenCalledWith(expect.objectContaining({ id: 'random' }))
    expect(voice.activeView).toBe('chat')
    await rowOf('lounge').trigger('click')
    await flushPromises()
    expect(joinVoiceChannel).toHaveBeenCalledWith('lounge')
    expect(voice.activeView).toBe('voice')
  })

  it('shows unread and mention badges and the people in a voice channel', async () => {
    await mountSidebar({ role: 'user' })
    chat.readStates = { random: { unread_count: 3, mention_count: 0 }, welcome: { unread_count: 5, mention_count: 2 } }
    voice.channelUsers = { lounge: { u2: { id: 'u2', username: 'zoe', display_name: 'Zoe', role: 'user' } } }
    await nextTick()
    expect(rowOf('random').text()).toContain('3')
    expect(rowOf('welcome').text()).toContain('2')
    expect(wrapper.find('[data-voice-user]').text()).toContain('Zoe')
  })

  it('collapses a category (kept per user) but keeps the open channel visible', async () => {
    await mountSidebar({ role: 'user' })
    await headerOf('text').find('button').trigger('click')
    expect(channelIds('text')).toEqual(['general'])
    expect(JSON.parse(localStorage.getItem(COLLAPSED_KEY))).toEqual(['text'])
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
    expect(document.querySelector('[data-testid="drag-ghost"]').textContent).toContain('random')
    expect(rowOf('general').element.parentElement.querySelector('[data-drop-indicator]')).not.toBeNull()
    await drop(yIn(rowOf('general'), 0.2))

    expect(order()).toBe('welcome | text: random general | voice: lounge | empty: ')
    expect(puts()).toHaveLength(1)
    expect(puts()[0].json.channels.filter(c => c.category_id === 'text')).toEqual([
      { id: 'random', category_id: 'text', sort_order: 0 },
      { id: 'general', category_id: 'text', sort_order: 1 }
    ])
    expect(document.querySelector('[data-testid="drag-ghost"]')).toBeNull()

    await answer('/api/admin/layout')
    expect(order()).toBe('welcome | text: random general | voice: lounge | empty: ')
    const toast = toasts.toasts.find(t => t.action)
    expect(toast.text).toBe('Kanal verschoben')
    expect(toast.action.label).toBe('Rückgängig')
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
    expect(puts().at(-1).json.channels.find(c => c.id === 'welcome')).toEqual({ id: 'welcome', category_id: 'empty', sort_order: 0 })
  })

  it('drops onto a collapsed category: appended there, which stays collapsed', async () => {
    await mountSidebar({ collapsed: ['voice'] })
    await dragTo(rowOf('random'), yIn(headerOf('voice'), 0.6))
    expect(headerOf('voice').classes()).toContain('ring-mnema-accent')
    await drop(yIn(headerOf('voice'), 0.6))
    expect(puts()[0].json.channels.filter(c => c.category_id === 'voice').map(c => c.id)).toEqual(['lounge', 'random'])
    expect(channelIds('voice')).toEqual([])
    expect(JSON.parse(localStorage.getItem(COLLAPSED_KEY))).toEqual(['voice'])
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
    expect(JSON.parse(localStorage.getItem(COLLAPSED_KEY))).toEqual([])
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
    expect(document.querySelector('[data-testid="drag-ghost"]').textContent).toContain('Empty')
    await drop(yIn(headerOf('text'), 0.1))
    expect(sectionIds()).toEqual(['__uncategorized', 'empty', 'text', 'voice'])
    expect(puts()[0].json.categories).toEqual([
      { id: 'empty', sort_order: 0 }, { id: 'text', sort_order: 1 }, { id: 'voice', sort_order: 2 }
    ])
    await answer('/api/admin/layout')
    expect(toasts.toasts.find(t => t.action).text).toBe('Kategorie verschoben')
    expect(wrapper.find('[data-testid="sidebar-announcer"]').text()).toBe('Empty verschoben: Position 1 von 3')
  })

  it('rolls back and reports an error when saving fails', async () => {
    await mountSidebar()
    await dragTo(rowOf('welcome'), yIn(rowOf('random'), 0.8))
    await drop(yIn(rowOf('random'), 0.8))
    expect(order()).toBe('text: general random welcome | voice: lounge | empty: ')
    await answer('/api/admin/layout', { ok: false })
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
    expect(toasts.toasts.find(t => t.type === 'error').text).toBe('Die neue Reihenfolge konnte nicht gespeichert werden.')
    expect(toasts.toasts.some(t => t.action)).toBe(false)
  })

  it('undo restores the previous order with another save', async () => {
    await mountSidebar()
    await dragTo(rowOf('lounge'), yIn(rowOf('general'), 0.2))
    await drop(yIn(rowOf('general'), 0.2))
    await answer('/api/admin/layout')
    expect(order()).toBe('welcome | text: lounge general random | voice:  | empty: ')

    toasts.toasts.find(t => t.action).action.onClick()
    await nextTick()
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
    const sent = await answer('/api/admin/layout')
    expect(sent.json.channels.find(c => c.id === 'lounge')).toEqual({ id: 'lounge', category_id: 'voice', sort_order: 0 })
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
    chat.handleWSEvent({ type: 'channels_changed' })
    await flush()
    expect(order()).toBe('text: random general | voice: lounge welcome | empty: ')

    await answer('/api/admin/layout')
    expect(puts()).toHaveLength(2)
    await answer('/api/admin/layout')
    expect(order()).toBe('text: random general | voice: lounge welcome | empty: ')
    expect(chat.categories[1].channels.map(c => c.id)).toEqual(['lounge', 'welcome'])
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
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(() => {})
    await dragTo(rowOf('random'), yIn(rowOf('general'), 0.2))
    await drop(yIn(rowOf('general'), 0.2))
    rowOf('random').element.click()
    expect(select).not.toHaveBeenCalled()
  })

  it('a plain click still opens the channel for admins', async () => {
    await mountSidebar()
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(() => {})
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
    expect(document.querySelector('[role="menu"]').textContent).toContain('Kanal bearbeiten')
    expect(puts()).toHaveLength(0)
  })
})
