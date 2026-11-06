import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OUTBOX_KEY_PREFIX, clearStoredOutbox, loadStoredOutbox, removeStoredEntry, saveStoredEntry } from './outboxStorage'
import type { PendingMessage } from './outboxTypes'

const pending = (over: Partial<PendingMessage> = {}): PendingMessage => ({
  nonce: 'n1', channelId: 'c1', userId: 'u1', content: 'hello', parentId: null, replyToId: null,
  createdAt: '2026-01-01T00:00:00Z', status: 'waiting', error: '', cause: 'offline', attempts: 0, possiblyDelivered: true, notBefore: 0,
  ...over
})
// The older layout: the account's whole list under one key.
const raw = (uid: string, value: unknown) => localStorage.setItem(OUTBOX_KEY_PREFIX + uid, JSON.stringify(value))
const saveAll = (items: PendingMessage[]) => items.forEach(saveStoredEntry)

beforeEach(() => localStorage.clear())
afterEach(() => vi.unstubAllGlobals())

describe('messages kept on this device', () => {
  it('come back after a reload with their details', () => {
    saveAll([pending({ status: 'failed', error: 'x', cause: 'server', attempts: 3, notBefore: 5, parentId: 'p', replyToId: 'r' })])
    expect(loadStoredOutbox('u1')).toEqual([pending({ status: 'failed', error: 'x', cause: 'server', attempts: 3, notBefore: 5, parentId: 'p', replyToId: 'r' })])
  })

  it('are added to, not overwritten, when two tabs store a message each', () => {
    saveStoredEntry(pending({ nonce: 'fromA', createdAt: '2026-01-01T00:00:01Z' }))
    saveStoredEntry(pending({ nonce: 'fromB', createdAt: '2026-01-01T00:00:02Z' }))
    removeStoredEntry('u1', 'fromA')
    expect(loadStoredOutbox('u1').map(m => m.nonce)).toEqual(['fromB'])
  })

  it('move from the older single-list layout to one key each', () => {
    raw('u1', [pending({ nonce: 'old1' }), pending({ nonce: 'old2' })])
    expect(loadStoredOutbox('u1').map(m => m.nonce)).toEqual(['old1', 'old2'])
    expect(localStorage.getItem(OUTBOX_KEY_PREFIX + 'u1')).toBeNull()
    expect(loadStoredOutbox('u1')).toHaveLength(2)
  })

  it('treat a message that was sending when the page closed as waiting again', () => {
    raw('u1', [{ ...pending(), status: 'sending', possiblyDelivered: false }])
    expect(loadStoredOutbox('u1')[0]).toMatchObject({ status: 'waiting', possiblyDelivered: true })
  })

  it('are filled with defaults when old entries lack fields', () => {
    raw('u1', [{ nonce: 'n', channelId: 'c', userId: 'u1', content: 'x' }])
    const [m] = loadStoredOutbox('u1')
    expect(m).toMatchObject({ parentId: null, replyToId: null, error: '', cause: 'offline', attempts: 0, possiblyDelivered: true, notBefore: 0, status: 'waiting' })
    expect(typeof m?.createdAt).toBe('string')
  })

  it('drop entries that are damaged or belong to another account', () => {
    raw('u1', [null, 'x', { nonce: '', channelId: 'c', userId: 'u1', content: 'x' }, pending({ userId: 'u2' }), pending({ nonce: 'ok' })])
    expect(loadStoredOutbox('u1').map(m => m.nonce)).toEqual(['ok'])
  })

  it('are empty for unreadable or non-list storage', () => {
    localStorage.setItem(OUTBOX_KEY_PREFIX + 'u1', '{not json')
    expect(loadStoredOutbox('u1')).toEqual([])
    raw('u1', { a: 1 })
    expect(loadStoredOutbox('u1')).toEqual([])
  })

  it('keep only the newest 50 and cut overlong text', () => {
    saveAll(Array.from({ length: 60 }, (_, i) => pending({ nonce: 'n' + i, createdAt: new Date(Date.UTC(2026, 0, 1, 0, i)).toISOString() })))
    const kept = loadStoredOutbox('u1')
    expect(kept).toHaveLength(50)
    expect(kept[49]?.nonce).toBe('n59')
    localStorage.clear()
    saveAll([pending({ content: 'a'.repeat(5000), error: 'e'.repeat(400) })])
    const [m] = loadStoredOutbox('u1')
    expect(m?.content).toHaveLength(4000)
    expect(m?.error).toHaveLength(300)
  })

  it('cut overlong text by characters, never inside an emoji', () => {
    saveAll([pending({ content: '😀'.repeat(4001) })])
    const [m] = loadStoredOutbox('u1')
    expect(Array.from(m?.content ?? '')).toHaveLength(4000)
    expect(m?.content.endsWith('😀')).toBe(true)
  })

  it('do not store another account\'s messages, and forget everything when none are left', () => {
    saveAll([pending(), pending({ userId: 'u2', nonce: 'x' })])
    expect(loadStoredOutbox('u1')).toHaveLength(1)
    removeStoredEntry('u1', 'n1')
    expect(loadStoredOutbox('u1')).toEqual([])
    expect(loadStoredOutbox('u2')).toHaveLength(1)
  })

  it('are removed for every account on sign-out, other keys stay', () => {
    saveAll([pending(), pending({ nonce: 'n2' }), pending({ userId: 'u2' })])
    localStorage.setItem('other', '1')
    clearStoredOutbox()
    expect(loadStoredOutbox('u1')).toEqual([])
    expect(loadStoredOutbox('u2')).toEqual([])
    expect(localStorage.getItem('other')).toBe('1')
  })

  it('survive blocked storage without an error', () => {
    const blocked = () => { throw new Error('blocked') }
    vi.stubGlobal('localStorage', { getItem: blocked, setItem: blocked, removeItem: blocked, key: blocked, get length(): number { return blocked() } })
    expect(() => saveStoredEntry(pending())).not.toThrow()
    expect(() => removeStoredEntry('u1', 'n1')).not.toThrow()
    expect(loadStoredOutbox('u1')).toEqual([])
    expect(() => clearStoredOutbox()).not.toThrow()
  })
})

describe('what the device keeps for an account', () => {
  const entriesOf = () => Object.keys(localStorage).filter(k => k.startsWith(OUTBOX_KEY_PREFIX + 'u1.'))

  it('does not grow past the limit: the oldest messages that are left out are removed from storage', () => {
    for (let i = 0; i < 53; i++) saveStoredEntry(pending({ nonce: `m${String(i).padStart(2, '0')}`, createdAt: new Date(Date.UTC(2026, 0, 1, 0, 0, i)).toISOString() }))
    const loaded = loadStoredOutbox('u1')
    expect(loaded).toHaveLength(50)
    expect(entriesOf()).toHaveLength(50)
    expect(localStorage.getItem(OUTBOX_KEY_PREFIX + 'u1.m00')).toBeNull()
    expect(loadStoredOutbox('u1').map(m => m.nonce)[0]).toBe('m03')
  })

  it('still load per-message entries when the older single list is unreadable, and drop that list', () => {
    saveStoredEntry(pending({ nonce: 'fine' }))
    localStorage.setItem(OUTBOX_KEY_PREFIX + 'u1', '{not json')
    expect(loadStoredOutbox('u1').map(m => m.nonce)).toEqual(['fine'])
    expect(localStorage.getItem(OUTBOX_KEY_PREFIX + 'u1')).toBeNull()
  })

  it('keep another tab\'s sending mark with who set it and when', () => {
    raw('u1', [{ ...pending(), status: 'sending', owner: 'tab-x', sendingSince: 1234 }])
    expect(loadStoredOutbox('u1')[0]).toMatchObject({ status: 'sending', owner: 'tab-x', sendingSince: 1234 })
  })
})
