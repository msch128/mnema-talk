import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { categoryFixture, channelFixture, readStateFixture, userFixture, fixtureId } from '../test-fixtures.fixture'
import type { ApiOptions } from '../lib/api'
import { api } from '../lib/api'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => Promise.resolve(http.handle(url, options)))
}))

const one = channelFixture({ id: fixtureId(2), category_id: fixtureId(10) })
const two = channelFixture({ id: fixtureId(3), category_id: fixtureId(10) })
const category = categoryFixture({ id: fixtureId(10), name: 'Music', channels: [one, two] })
const NOW = new Date('2026-01-01T12:00:00Z')

function setup() {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: fixtureId(1) })
  chat.categories = [category]
  chat.readStates = {
    [one.id]: readStateFixture({ channel_id: one.id, unread_count: 3, mention_count: 1 }),
    [two.id]: readStateFixture({ channel_id: two.id, unread_count: 2, mention_count: 0 })
  }
  return chat
}

function calls(method: string) {
  return vi.mocked(api).mock.calls.filter(([, o]) => o?.method === method)
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
  setActivePinia(createPinia())
  http.handle = url => url === '/api/notification-settings' ? { channels: [], categories: [] } : []
  vi.mocked(api).mockClear()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  useChatStore().resetCommunityState()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('notification settings', () => {
  it('loads the stored settings and ignores a failed load', async () => {
    const chat = setup()
    const stored = { channels: [{ channel_id: one.id, level: 'mentions', muted_until: null }], categories: [] }
    http.handle = () => stored
    await chat.fetchNotificationSettings()
    expect(chat.notifySettings).toEqual(stored)
    http.handle = () => Promise.reject(new Error('offline'))
    await chat.fetchNotificationSettings()
    expect(chat.notifySettings).toEqual(stored)
    expect(console.warn).toHaveBeenCalled()
  })

  it('shows a channel level at once, sends the mute end and reloads only the settings', async () => {
    const chat = setup()
    const until = new Date(NOW.getTime() + 3_600_000).toISOString()
    expect(await chat.setNotificationLevel(one.id, 'mute', until)).toBe(true)
    expect(chat.notificationLevel(one.id)).toBe('mute')
    const [url, options] = calls('PUT')[0]!
    expect(url).toBe(`/api/channels/${one.id}/notifications`)
    expect(options?.json).toEqual({ level: 'mute', muted_until: until })
    expect(vi.mocked(api).mock.calls.some(([u]) => u === '/api/read-state')).toBe(false)
    expect(vi.mocked(api).mock.calls.some(([u]) => u === '/api/notification-settings')).toBe(true)
  })

  it('creates a state for an unknown channel and ignores empty arguments', async () => {
    const chat = setup()
    await chat.setNotificationLevel(fixtureId(77), 'mentions')
    expect(chat.readStates[fixtureId(77)]).toMatchObject({ notify_level: 'mentions', unread_count: 0 })
    expect(await chat.setNotificationLevel('', 'all')).toBe(false)
    expect(await chat.setCategoryNotificationLevel('', 'all')).toBe(false)
    expect(chat.notificationLevel('absent')).toBe('all')
  })

  it('reads the effective levels back when a channel inherits again or a category changes', async () => {
    const chat = setup()
    await chat.setNotificationLevel(one.id, 'inherit')
    expect(vi.mocked(api).mock.calls.some(([u]) => u === '/api/read-state')).toBe(true)
    vi.mocked(api).mockClear()
    await chat.setCategoryNotificationLevel(category.id, 'mute')
    expect(calls('PUT')[0]![0]).toBe(`/api/categories/${category.id}/notifications`)
    expect(vi.mocked(api).mock.calls.some(([u]) => u === '/api/read-state')).toBe(true)
  })

  it('reports a failed write', async () => {
    const chat = setup()
    http.handle = () => Promise.reject(new Error('forbidden'))
    expect(await chat.setCategoryNotificationLevel(category.id, 'mute')).toBe(false)
  })

  it('reloads read states and settings when the next timed mute has run out', async () => {
    const chat = setup()
    const end = new Date(NOW.getTime() + 60_000).toISOString()
    http.handle = url => url === '/api/notification-settings'
      ? { channels: [{ channel_id: one.id, level: 'mute', muted_until: end }], categories: [{ category_id: category.id, level: 'mute', muted_until: new Date(NOW.getTime() + 120_000).toISOString() }] }
      : []
    await chat.fetchNotificationSettings()
    vi.mocked(api).mockClear()
    http.handle = () => []
    await vi.advanceTimersByTimeAsync(60_000)
    expect(vi.mocked(api).mock.calls.some(([u]) => u === '/api/read-state')).toBe(false)
    await vi.advanceTimersByTimeAsync(2_000)
    expect(vi.mocked(api).mock.calls.some(([u]) => u === '/api/read-state')).toBe(true)
  })

  it('does not arm a timer without a timed mute', async () => {
    const chat = setup()
    const before = vi.getTimerCount()
    http.handle = () => ({ channels: [{ channel_id: one.id, level: 'mute', muted_until: null }], categories: [] })
    await chat.fetchNotificationSettings()
    expect(vi.getTimerCount()).toBe(before)
  })
})

describe('mark all read', () => {
  it('clears every channel at once and tells the server', async () => {
    const chat = setup()
    http.handle = () => undefined
    expect(await chat.markAllRead()).toBe(true)
    expect(chat.readStates[one.id]).toMatchObject({ unread_count: 0, mention_count: 0 })
    expect(chat.readStates[two.id]).toMatchObject({ unread_count: 0 })
    const [url, options] = calls('POST')[0]!
    expect(url).toBe('/api/read-state/read-all')
    expect(options?.json).toEqual({})
  })

  it('limits itself to a category', async () => {
    const chat = setup()
    const outside = channelFixture({ id: fixtureId(4) })
    chat.uncategorized = [outside]
    chat.readStates = { ...chat.readStates, [outside.id]: readStateFixture({ channel_id: outside.id, unread_count: 5 }) }
    http.handle = () => undefined
    await chat.markAllRead(category.id)
    expect(chat.readStates[one.id]?.unread_count).toBe(0)
    expect(chat.readStates[outside.id]?.unread_count).toBe(5)
    expect(calls('POST')[0]![1]?.json).toEqual({ category_id: category.id })
  })

  it('takes the server state back when the call fails', async () => {
    const chat = setup()
    http.handle = url => url === '/api/read-state/read-all'
      ? Promise.reject(new Error('offline'))
      : [readStateFixture({ channel_id: one.id, unread_count: 3 })]
    expect(await chat.markAllRead()).toBe(false)
    expect(chat.readStates[one.id]?.unread_count).toBe(3)
  })
})

describe('read_state refresh without a channel', () => {
  it('reloads read states and settings', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'read_state', payload: { refresh: true } })
    await vi.advanceTimersByTimeAsync(0)
    const urls = vi.mocked(api).mock.calls.map(([u]) => u)
    expect(urls).toContain('/api/read-state')
    expect(urls).toContain('/api/notification-settings')
  })

  it('ignores a payload that is neither a refresh nor about a channel', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'read_state', payload: null as never })
    chat.handleWSEvent({ type: 'read_state', payload: { refresh: false } as never })
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.mocked(api)).not.toHaveBeenCalled()
  })
})
