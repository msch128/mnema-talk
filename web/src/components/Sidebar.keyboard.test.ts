import type { VueWrapper, DOMWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import type { Channel, ChannelType, Category, LayoutRequest } from '../types/domain'
import type { ConfirmRequest } from '../lib/confirm'
import { userFixture, channelFixture, categoryFixture, requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import { COLLAPSED_KEY } from '../lib/channelTree'

// GET /api/channels answers from `server`; PUT /api/admin/layout and the
// duplicate endpoint wait until the test answers them.
interface Tree { uncategorized: Channel[]; categories: Category[] }
interface Call { url: string; method: string; json: unknown }
interface Waiting { url: string; json: unknown; resolve: (value: unknown) => void; reject: (reason: unknown) => void }
const h = vi.hoisted((): { server: Tree; waiting: Waiting[]; calls: Call[] } => ({ server: { uncategorized: [], categories: [] }, waiting: [], calls: [] }))
function layoutBody(value: unknown): Required<LayoutRequest> {
  if (!value || typeof value !== 'object' || !('channels' in value) || !('categories' in value) || !Array.isArray(value.channels) || !Array.isArray(value.categories)) throw new Error('Expected layout request')
  return {
    channels: value.channels.map((entry: unknown) => {
      if (!entry || typeof entry !== 'object' || !('id' in entry) || typeof entry.id !== 'string' || !('sort_order' in entry) || typeof entry.sort_order !== 'number' || !('category_id' in entry) || (entry.category_id !== null && typeof entry.category_id !== 'string')) throw new Error('Expected channel move')
      return { id: entry.id, sort_order: entry.sort_order, category_id: entry.category_id }
    }),
    categories: value.categories.map((entry: unknown) => {
      if (!entry || typeof entry !== 'object' || !('id' in entry) || typeof entry.id !== 'string' || !('sort_order' in entry) || typeof entry.sort_order !== 'number') throw new Error('Expected category move')
      return { id: entry.id, sort_order: entry.sort_order }
    }),
  }
}
function layoutCall(call: Call | undefined) { const c = requireValue(call); return { ...c, json: layoutBody(c.json) } }

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, opts: ApiOptions = {}) => {
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

const confirmMock = vi.fn<(_options: ConfirmRequest) => Promise<boolean>>(() => Promise.resolve(true))
vi.mock('../lib/confirm', () => ({ confirm: (options: ConfirmRequest) => confirmMock(options) }))

import Sidebar from './Sidebar.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
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

const rowOf = (id: string) => wrapper.find(`[data-drop="channel"][data-id="${id}"]`)
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
// Middle of an element's top or bottom half.

// Mouse drag from an element to a y position; returns before the drop.

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



async function altKey(el: DOMWrapper<Element> | HTMLElement, key: string) {
  const target = el instanceof HTMLElement ? el : el.element
  if (!(target instanceof HTMLElement)) throw new Error('Expected focusable sidebar item')
  target.focus()
  const e = new KeyboardEvent('keydown', { key, altKey: true, bubbles: true, cancelable: true })
  target.dispatchEvent(e)
  await flush()
  return e
}
const toggleOf = (id: string) => wrapper.find(`[data-category-toggle="${id}"]`)
const announced = () => wrapper.find('[data-testid="sidebar-announcer"]').text()


describe('Sidebar keyboard moves (Alt+Arrow)', () => {
  it('moves a channel within its category, keeps focus and announces the new place', async () => {
    await mountSidebar()
    const e = await altKey(rowOf('general'), 'ArrowDown')
    expect(e.defaultPrevented).toBe(true)
    expect(order()).toBe('welcome | text: random general | voice: lounge | empty: ')
    expect(puts()).toHaveLength(1)
    expect(document.activeElement).toBe(rowOf('general').element)
    expect(announced()).toBe('general verschoben: Position 2 von 2 in Text')
    await answer('/api/admin/layout')
    expect(toasts.toasts.find(t => t.action)!.text).toBe('Kanal verschoben')
  })

  it('crosses into the next category at the bottom edge and the previous one at the top edge', async () => {
    await mountSidebar()
    await altKey(rowOf('random'), 'ArrowDown')
    expect(order()).toBe('welcome | text: general | voice: random lounge | empty: ')
    expect(announced()).toBe('random verschoben: Position 1 von 2 in Voice')
    expect(document.activeElement).toBe(rowOf('random').element)

    await altKey(rowOf('random'), 'ArrowUp')
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
    await altKey(rowOf('general'), 'ArrowUp')
    expect(order()).toBe('welcome general | text: random | voice: lounge | empty: ')
    expect(announced()).toBe('general verschoben: Position 2 von 2 in Ohne Kategorie')
    expect(document.activeElement).toBe(rowOf('general').element)
  })

  it('reaches empty categories and stops at the very bottom', async () => {
    await mountSidebar()
    await altKey(rowOf('lounge'), 'ArrowDown')
    expect(order()).toBe('welcome | text: general random | voice:  | empty: lounge')
    const saves = puts().length
    await altKey(rowOf('lounge'), 'ArrowDown')
    expect(puts()).toHaveLength(saves)
    expect(announced()).toBe('lounge ist schon ganz unten')
  })

  it('stops at the very top', async () => {
    await mountSidebar()
    await altKey(rowOf('welcome'), 'ArrowUp')
    expect(puts()).toHaveLength(0)
    expect(announced()).toBe('welcome ist schon ganz oben')
  })

  it('opens a collapsed category a channel moves into', async () => {
    await mountSidebar({ collapsed: ['voice'] })
    await altKey(rowOf('random'), 'ArrowDown')
    expect(channelIds('voice')).toEqual(['random', 'lounge'])
    expect(JSON.parse(requireValue(localStorage.getItem(COLLAPSED_KEY)))).toEqual([])
    expect(document.activeElement).toBe(rowOf('random').element)
  })

  it('moves categories from their header and keeps focus there', async () => {
    await mountSidebar()
    await altKey(toggleOf('text'), 'ArrowDown')
    expect(sectionIds()).toEqual(['__uncategorized', 'voice', 'text', 'empty'])
    expect(puts()[0]!.json.categories.map(c => c.id)).toEqual(['voice', 'text', 'empty'])
    expect(document.activeElement).toBe(toggleOf('text').element)
    expect(announced()).toBe('Text verschoben: Position 2 von 3')
    await altKey(toggleOf('voice'), 'ArrowUp')
    expect(announced()).toBe('Voice ist schon ganz oben')
  })

  it('quick presses share the save queue', async () => {
    await mountSidebar()
    await altKey(rowOf('welcome'), 'ArrowDown')
    await altKey(rowOf('welcome'), 'ArrowDown')
    await altKey(rowOf('welcome'), 'ArrowDown')
    expect(order()).toBe('text: general random welcome | voice: lounge | empty: ')
    expect(puts()).toHaveLength(1)
    await answer('/api/admin/layout')
    expect(puts()).toHaveLength(2)
    expect(puts()[1]!.json.channels.filter(c => c.category_id === 'text').map(c => c.id)).toEqual(['general', 'random', 'welcome'])
    await answer('/api/admin/layout')
    expect(toasts.toasts.filter(t => t.action)).toHaveLength(1)
  })

  it('describes the shortcut to admins only; members get no moves', async () => {
    await mountSidebar()
    const hint = rowOf('general').attributes('aria-describedby')
    expect(requireValue(document.getElementById(requireValue(hint))).textContent).toContain('Alt')
    expect(rowOf('general').attributes('aria-keyshortcuts')).toBe('Alt+ArrowUp Alt+ArrowDown')
    wrapper.unmount()
    setActivePinia(createPinia())
    await mountSidebar({ role: 'user' })
    expect(rowOf('general').attributes('aria-describedby')).toBeUndefined()
    const e = await altKey(rowOf('general'), 'ArrowDown')
    expect(e.defaultPrevented).toBe(false)
    expect(order()).toBe('welcome | text: general random | voice: lounge | empty: ')
    expect(puts()).toHaveLength(0)
  })
})
