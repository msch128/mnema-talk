import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OUTBOX_KEY_PREFIX, clearStoredOutbox, loadStoredOutbox, saveStoredOutbox } from './outboxStorage'
import type { PendingMessage } from './outboxTypes'

const pending = (over: Partial<PendingMessage> = {}): PendingMessage => ({
  nonce: 'n1', channelId: 'c1', userId: 'u1', content: 'hello', parentId: null, replyToId: null,
  createdAt: '2026-01-01T00:00:00Z', status: 'waiting', error: '', cause: 'offline', attempts: 0, attempted: true, notBefore: 0,
  ...over
})
const raw = (uid: string, value: unknown) => localStorage.setItem(OUTBOX_KEY_PREFIX + uid, JSON.stringify(value))

beforeEach(() => localStorage.clear())
afterEach(() => vi.unstubAllGlobals())

describe('messages kept on this device', () => {
  it('come back after a reload with their details', () => {
    saveStoredOutbox('u1', [pending({ status: 'failed', error: 'x', cause: 'server', attempts: 3, notBefore: 5, parentId: 'p', replyToId: 'r' })])
    expect(loadStoredOutbox('u1')).toEqual([pending({ status: 'failed', error: 'x', cause: 'server', attempts: 3, notBefore: 5, parentId: 'p', replyToId: 'r' })])
  })

  it('treat a message that was sending when the page closed as waiting again', () => {
    raw('u1', [{ ...pending(), status: 'sending' }])
    expect(loadStoredOutbox('u1')[0]?.status).toBe('waiting')
  })

  it('are filled with defaults when old entries lack fields', () => {
    raw('u1', [{ nonce: 'n', channelId: 'c', userId: 'u1', content: 'x' }])
    const [m] = loadStoredOutbox('u1')
    expect(m).toMatchObject({ parentId: null, replyToId: null, error: '', cause: 'offline', attempts: 0, attempted: true, notBefore: 0, status: 'waiting' })
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
    saveStoredOutbox('u1', Array.from({ length: 60 }, (_, i) => pending({ nonce: 'n' + i })))
    expect(loadStoredOutbox('u1')).toHaveLength(50)
    raw('u1', [pending({ content: 'a'.repeat(5000), error: 'e'.repeat(400) })])
    const [m] = loadStoredOutbox('u1')
    expect(m?.content).toHaveLength(4000)
    expect(m?.error).toHaveLength(300)
  })

  it('do not store another account\'s messages, and forget everything when none are left', () => {
    saveStoredOutbox('u1', [pending(), pending({ userId: 'u2', nonce: 'x' })])
    expect(loadStoredOutbox('u1')).toHaveLength(1)
    saveStoredOutbox('u1', [])
    expect(localStorage.getItem(OUTBOX_KEY_PREFIX + 'u1')).toBeNull()
  })

  it('are removed for every account on sign-out, other keys stay', () => {
    saveStoredOutbox('u1', [pending()])
    saveStoredOutbox('u2', [pending({ userId: 'u2' })])
    localStorage.setItem('other', '1')
    clearStoredOutbox()
    expect(localStorage.getItem(OUTBOX_KEY_PREFIX + 'u1')).toBeNull()
    expect(localStorage.getItem(OUTBOX_KEY_PREFIX + 'u2')).toBeNull()
    expect(localStorage.getItem('other')).toBe('1')
  })

  it('survive blocked storage without an error', () => {
    const blocked = () => { throw new Error('blocked') }
    vi.stubGlobal('localStorage', { getItem: blocked, setItem: blocked, removeItem: blocked, key: blocked, get length(): number { return blocked() } })
    expect(() => saveStoredOutbox('u1', [pending()])).not.toThrow()
    expect(loadStoredOutbox('u1')).toEqual([])
    expect(() => clearStoredOutbox()).not.toThrow()
  })
})
