import type { VueWrapper, DOMWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import type { Channel, ChannelType, Category } from '../types/domain'
import type { ConfirmRequest } from '../lib/confirm'
import { userFixture, channelFixture, categoryFixture, requireValue, parseLayoutRequestBody } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { COLLAPSED_KEY } from '../lib/channelTree'

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
const headerOf = (id: string) => wrapper.find(`[data-drop="header"][data-id="${id}"]`)
const sectionIds = () => wrapper.findAll('[data-drop-section]').map(s => requireValue(s.attributes('data-drop-section')))
const channelIds = (sectionId: string) =>
  wrapper.find(`[data-drop-section="${sectionId}"]`).findAll('[data-drop="channel"]').map(r => r.attributes('data-id'))
const flush = async () => {
  for (let i = 0; i < 4; i++) await flushPromises()
}

// happy-dom has no layout. Lay the sidebar out like the browser would: rows
// stacked from y = 0 (header 24px, row 34px, voice users 32px, "no channels"
// 28px, 12px between sections), 240px wide.
const tailOf = () => wrapper.find('[data-drop-tail]')
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
async function submitName(value: string) {
  const input = q<HTMLInputElement>('#edit-name')
  input.value = value
  input.dispatchEvent(new Event('input'))
  requireValue(input.form).dispatchEvent(new Event('submit', { cancelable: true }))
  await flush()
}


describe('Sidebar context menus', () => {
  it('admins can create a channel or a category from the empty part of the list', async () => {
    await mountSidebar()
    const e = await contextMenu(nav())
    expect(e.defaultPrevented).toBe(true)
    expect(menuLabels()).toEqual(['Kanal erstellen', 'Kategorie erstellen'])
    await choose('Kanal erstellen')
    expect(document.body.textContent).toContain('Neuen Kanal erstellen')
    expect(q<HTMLSelectElement>('#channel-category').value).toBe('')
  })

  it('the empty tail below the last channel is part of the empty list area', async () => {
    await mountSidebar()
    expect(tailOf().exists()).toBe(true)
    const e = await contextMenu(tailOf())
    expect(e.defaultPrevented).toBe(true)
    expect(menuLabels()).toEqual(['Kanal erstellen', 'Kategorie erstellen'])
  })

  it('members keep the browser menu on the empty part of the list', async () => {
    await mountSidebar({ role: 'user' })
    const e = await contextMenu(nav())
    expect(e.defaultPrevented).toBe(false)
    expect(document.querySelector('[role="menu"]')).toBeNull()
  })

  it('category menu: everyone can collapse and expand all', async () => {
    await mountSidebar({ role: 'user' })
    await contextMenu(headerOf('voice'))
    expect(menuLabels()).toEqual(['Alle als gelesen markieren', 'Standard (alle Nachrichten)', 'Alle Nachrichten', 'Nur Erwähnungen', 'Kategorie stummschalten', 'Alle einklappen', 'Alle ausklappen'])
    expect(menuItems().find(b => (b.textContent ?? '').includes('Alle ausklappen'))!.disabled).toBe(true)
    await choose('Alle einklappen')
    expect(JSON.parse(requireValue(localStorage.getItem(COLLAPSED_KEY)))).toEqual(['text', 'voice', 'empty'])
    // The open channel stays visible in its collapsed category.
    expect(channelIds('text')).toEqual(['general'])
    expect(channelIds('voice')).toEqual([])

    await contextMenu(headerOf('voice'))
    expect(menuItems().find(b => (b.textContent ?? '').includes('Alle einklappen'))!.disabled).toBe(true)
    await choose('Alle ausklappen')
    expect(JSON.parse(requireValue(localStorage.getItem(COLLAPSED_KEY)))).toEqual([])
    expect(channelIds('voice')).toEqual(['lounge'])
  })

  it('category menu: admins also create channels and categories there, besides edit and delete', async () => {
    await mountSidebar()
    await contextMenu(headerOf('voice'))
    expect(menuLabels()).toEqual([
      'Alle als gelesen markieren', 'Standard (alle Nachrichten)', 'Alle Nachrichten', 'Nur Erwähnungen', 'Kategorie stummschalten', 'Alle einklappen', 'Alle ausklappen',
      'Rechte …', 'Kanal erstellen', 'Kategorie erstellen', 'Kategorie bearbeiten', 'Kategorie löschen'
    ])
    await choose('Kanal erstellen')
    // The category is preselected; a voice-only category suggests a voice channel.
    expect(q<HTMLSelectElement>('#channel-category').value).toBe('voice')
    expect(q('[aria-pressed="true"]').textContent).toContain('Sprachkanal')
  })

  it('creates a category right below the one whose menu was used', async () => {
    await mountSidebar()
    await contextMenu(headerOf('text'))
    await choose('Kategorie erstellen')
    expect(q('[role="dialog"]').textContent).toContain('Kategorie erstellen')
    await submitName('Projekte')
    const post = requireValue(h.waiting.find(w => w.url === '/api/admin/categories'))
    expect(post.json).toEqual({ name: 'Projekte', sort_order: 3 })
    h.server.categories.push(categoryFixture({ id: 'new', name: 'Projekte', sort_order: 3, channels: [] }))
    await answer('/api/admin/categories', { body: { id: 'new', name: 'Projekte', sort_order: 3 } })

    expect(toasts.toasts.map(t => t.text)).toContain('Kategorie erstellt')
    expect(sectionIds()).toEqual(['__uncategorized', 'text', 'new', 'voice', 'empty'])
    expect(puts()[0]!.json.categories.map(c => c.id)).toEqual(['text', 'new', 'voice', 'empty'])
    await answer('/api/admin/layout')
    expect(sectionIds()).toEqual(['__uncategorized', 'text', 'new', 'voice', 'empty'])
    // A silent save: no "moved" toast with undo.
    expect(toasts.toasts.some(t => t.action)).toBe(false)
    expect(headerOf('new').classes()).toContain('nav-flash')
  })

  it('creating a category from the list background appends it without a layout save', async () => {
    await mountSidebar()
    await contextMenu(nav())
    await choose('Kategorie erstellen')
    await submitName('Archiv')
    h.server.categories.push(categoryFixture({ id: 'archiv', name: 'Archiv', sort_order: 3, channels: [] }))
    await answer('/api/admin/categories', { body: { id: 'archiv', name: 'Archiv', sort_order: 3 } })
    expect(sectionIds().at(-1)).toBe('archiv')
    expect(puts()).toHaveLength(0)
  })

  it('channel menu: admins duplicate a channel; the copy is highlighted, not opened', async () => {
    await mountSidebar({ collapsed: ['text'] })
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(async () => {})
    await contextMenu(rowOf('general'))
    expect(menuLabels().slice(-3)).toEqual(['Kanal bearbeiten', 'Kanal duplizieren', 'Kanal löschen'])
    await choose('Kanal duplizieren')
    const post = requireValue(h.calls.find(c => c.url === '/api/admin/channels/general/duplicate'))
    expect(post).toMatchObject({ method: 'POST' })
    expect(post.json).toBeUndefined()

    h.server.categories[0]!.channels.splice(1, 0, ch('general-2', 'text', 1))
    h.server.categories[0]!.channels[2]!.sort_order = 2
    await answer('/duplicate', { body: ch('general-2', 'text', 1) })
    expect(toasts.toasts.map(t => t.text)).toContain('„general“ dupliziert')
    // Its collapsed category opens so the copy can be seen.
    expect(channelIds('text')).toEqual(['general', 'general-2', 'random'])
    expect(rowOf('general-2').classes()).toContain('nav-flash')
    expect(select).not.toHaveBeenCalled()
  })

  it('channel and category menus open the permissions dialog', async () => {
    await mountSidebar()
    await contextMenu(rowOf('general'))
    await choose('Rechte \u2026')
    expect(document.body.textContent).toContain('Rechte von #general')
    expect(h.calls.some(c => c.url === '/api/admin/channels/general/overwrites')).toBe(true)
    ;(document.querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Schlie\u00dfen"]') ?? document.querySelector<HTMLButtonElement>('[role="dialog"] button'))?.click()
    await flush()
    await contextMenu(headerOf('voice'))
    await choose('Rechte \u2026')
    expect(h.calls.some(c => c.url.startsWith('/api/admin/categories/') && c.url.endsWith('/overwrites'))).toBe(true)
  })

  it('reports a failed duplicate', async () => {
    await mountSidebar()
    await contextMenu(rowOf('lounge'))
    await choose('Kanal duplizieren')
    await answer('/duplicate', { ok: false })
    expect(toasts.toasts.find(t => t.type === 'error')!.text).toBe('Nicht gefunden')
  })

  it('members see no duplicate, create or edit items', async () => {
    await mountSidebar({ role: 'user' })
    await contextMenu(rowOf('general'))
    expect(menuLabels()).not.toContain('Kanal duplizieren')
    expect(menuLabels()).not.toContain('Kanal bearbeiten')
  })

  it('the server menu offers to create a category too', async () => {
    await mountSidebar()
    await wrapper.find('[aria-controls="server-menu"]').trigger('click')
    await nextTick()
    const items = wrapper.findAll('#server-menu [role="menuitem"]')
    expect(items.map(b => b.text())).toEqual(['Admin-Konsole', 'Kanal erstellen', 'Kategorie erstellen', 'Alle als gelesen markieren', 'Rechtliches & Datenschutz'])
    await items[2]!.trigger('click')
    await flush()
    expect(document.querySelector('#edit-name')).not.toBeNull()
  })
})

// ---- Keyboard ----
