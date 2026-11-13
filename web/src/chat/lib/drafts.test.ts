import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { beginDraftSession, channelDraftKey, clearAllDrafts, DRAFT_SESSION_KEY, DRAFT_KEY_PREFIX, draftsAtRisk, getDraft, hasDraft, notePendingDraft, MAX_DRAFT_TEXT, MAX_DRAFTS, setDraft, threadDraftKey } from './drafts'

beforeEach(() => {
  localStorage.clear()
  clearAllDrafts()
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

type StorageCall = 'getItem' | 'setItem' | 'removeItem' | 'key'

/**
 * Makes the named storage calls throw, as blocked or full storage does; the
 * others reach the real storage. A stand-in for the global, not a spy on
 * Storage.prototype: the test window's storage does not go through the
 * prototype, so such a spy never throws.
 */
function failStorage(...calls: StorageCall[]) {
  const real = window.localStorage
  const fail = (call: StorageCall) => calls.includes(call)
  const blocked = (): never => { throw new Error('blocked') }
  vi.stubGlobal('localStorage', {
    get length() { return real.length },
    key: (i: number) => (fail('key') ? blocked() : real.key(i)),
    getItem: (k: string) => (fail('getItem') ? blocked() : real.getItem(k)),
    setItem: (k: string, v: string) => (fail('setItem') ? blocked() : real.setItem(k, v)),
    removeItem: (k: string) => (fail('removeItem') ? blocked() : real.removeItem(k)),
    clear: () => real.clear(),
  })
}

describe('drafts', () => {
  it('puts no draft at risk once it is stored: it comes back after a reload', () => {
    expect(draftsAtRisk()).toBe(false)
    setDraft('u1', threadDraftKey('t1'), { text: 'in thread', reply: null })
    expect(draftsAtRisk()).toBe(false)
  })

  it('puts a draft at risk while text is typed and not written yet', () => {
    const composer = Symbol('composer')
    notePendingDraft(composer, true)
    expect(draftsAtRisk()).toBe(true)
    notePendingDraft(composer, false)
    expect(draftsAtRisk()).toBe(false)
  })

  it('puts a draft at risk when storage refused it, until a write succeeds', () => {
    failStorage('setItem')
    setDraft('u1', channelDraftKey('c1'), { text: 'nowhere to go', reply: null })
    expect(draftsAtRisk()).toBe(true)
    vi.unstubAllGlobals()
    setDraft('u1', channelDraftKey('c1'), { text: 'stored now', reply: null })
    expect(draftsAtRisk()).toBe(false)
  })

  it('puts nothing at risk once the last draft is gone, also when storage is fully blocked', () => {
    failStorage('setItem', 'getItem', 'key', 'removeItem')
    setDraft('u1', channelDraftKey('c1'), { text: 'only in memory', reply: null })
    expect(draftsAtRisk()).toBe(true)
    setDraft('u1', channelDraftKey('c1'), null)
    expect(draftsAtRisk()).toBe(false)
  })

  it('keeps text and reply target per channel and thread', () => {
    setDraft('u1', channelDraftKey('c1'), { text: 'hello', reply: { id: 'm1', display_name: 'Max' } })
    setDraft('u1', threadDraftKey('t1'), { text: 'in thread', reply: null })
    expect(getDraft('u1', channelDraftKey('c1'))).toEqual({ text: 'hello', reply: { id: 'm1', display_name: 'Max' } })
    expect(getDraft('u1', threadDraftKey('t1'))?.text).toBe('in thread')
    expect(getDraft('u1', channelDraftKey('c2'))).toBeNull()
    expect(hasDraft('u1', channelDraftKey('c1'))).toBe(true)
    expect(hasDraft('u1', channelDraftKey('c2'))).toBe(false)
  })

  it('survives a reload and stays with its account', () => {
    setDraft('u1', channelDraftKey('c1'), { text: 'mine', reply: null })
    setDraft('u2', channelDraftKey('c1'), { text: 'theirs', reply: null })
    expect(Object.keys(localStorage).sort()).toEqual([`${DRAFT_KEY_PREFIX}u1`, `${DRAFT_KEY_PREFIX}u2`])
    // A fresh page reads from storage: a different account first, then back.
    clearAllDrafts()
    setDraft('u3', 'c:x', { text: 'x', reply: null })
    expect(getDraft('u1', channelDraftKey('c1'))).toBeNull()
  })

  it('reloads from storage for the account asking', () => {
    localStorage.setItem(`${DRAFT_KEY_PREFIX}u1`, JSON.stringify({ 'c:c1': { text: 'kept', reply: { id: 'm9' }, at: 1 } }))
    expect(hasDraft('u1', 'c:c1')).toBe(false)
    expect(getDraft('u1', 'c:c1')).toEqual({ text: 'kept', reply: { id: 'm9' } })
    expect(hasDraft('u1', 'c:c1')).toBe(true)
    expect(hasDraft('u2', 'c:c1')).toBe(false)
  })

  it('forgets a draft that is emptied, as after sending', () => {
    setDraft('u1', 'c:c1', { text: 'x', reply: null })
    setDraft('u1', 'c:c1', { text: '  ', reply: null })
    expect(hasDraft('u1', 'c:c1')).toBe(false)
    expect(localStorage.getItem(`${DRAFT_KEY_PREFIX}u1`)).toBeNull()
  })

  it('keeps a reply target even without text', () => {
    setDraft('u1', 'c:c1', { text: '', reply: { id: 'm1' } })
    expect(getDraft('u1', 'c:c1')).toEqual({ text: '', reply: { id: 'm1' } })
  })

  it('caps the text and the number of drafts, dropping the oldest', () => {
    setDraft('u1', 'c:long', { text: 'a'.repeat(MAX_DRAFT_TEXT + 50), reply: null })
    expect(getDraft('u1', 'c:long')?.text).toHaveLength(MAX_DRAFT_TEXT)
    vi.useFakeTimers()
    for (let i = 0; i < MAX_DRAFTS + 5; i++) {
      vi.setSystemTime(1000 + i)
      setDraft('u1', `c:${i}`, { text: 'x', reply: null })
    }
    vi.useRealTimers()
    expect(hasDraft('u1', `c:${MAX_DRAFTS + 4}`)).toBe(true)
    expect(hasDraft('u1', 'c:0')).toBe(false)
  })

  it('ignores damaged storage', () => {
    localStorage.setItem(`${DRAFT_KEY_PREFIX}u1`, '{oops')
    expect(getDraft('u1', 'c:c1')).toBeNull()
    localStorage.setItem(`${DRAFT_KEY_PREFIX}u2`, JSON.stringify({ 'c:a': 5, 'c:b': { text: 7 }, 'c:ok': { text: 'fine', reply: { id: '' } } }))
    expect(getDraft('u2', 'c:ok')).toEqual({ text: 'fine', reply: null })
    expect(getDraft('u2', 'c:a')).toBeNull()
    localStorage.setItem(`${DRAFT_KEY_PREFIX}u3`, '[]')
    expect(getDraft('u3', 'c:a')).toBeNull()
  })

  it('keeps earlier drafts in memory when storage reads work but writes fail', () => {
    failStorage('setItem')
    setDraft('u1', 'c:one', { text: 'A', reply: null })
    setDraft('u1', 'c:two', { text: 'B', reply: null })
    expect(getDraft('u1', 'c:one')?.text).toBe('A')
    expect(getDraft('u1', 'c:two')?.text).toBe('B')
  })

  it('works in memory when storage is blocked', () => {
    failStorage('setItem', 'getItem', 'key', 'removeItem')
    expect(() => setDraft('u1', 'c:c1', { text: 'x', reply: null })).not.toThrow()
    expect(hasDraft('u1', 'c:c1')).toBe(true)
    expect(() => clearAllDrafts()).not.toThrow()
    expect(hasDraft('u1', 'c:c1')).toBe(false)
  })

  it('removes every account\'s drafts on sign-out but nothing else', () => {
    localStorage.setItem('mnema.sidebar.collapsedCategories', '[]')
    setDraft('u1', 'c:c1', { text: 'x', reply: null })
    localStorage.setItem(`${DRAFT_KEY_PREFIX}u2`, '{}')
    clearAllDrafts()
    expect(hasDraft('u1', 'c:c1')).toBe(false)
    expect(Object.keys(localStorage)).toEqual(['mnema.sidebar.collapsedCategories'])
  })

  describe('across tabs', () => {
    // A second tab is a second copy of the module sharing the same storage.
    async function openTab() {
      vi.resetModules()
      return import('./drafts')
    }

    it('a stale tab cannot write a draft back after sign-out', async () => {
      beginDraftSession('u1')
      setDraft('u1', 'c:c1', { text: 'mine', reply: null })
      const other = await openTab()
      other.beginDraftSession('u1')
      expect(other.getDraft('u1', 'c:c1')?.text).toBe('mine')

      clearAllDrafts() // sign-out in the first tab
      other.setDraft('u1', 'c:c1', { text: 'back again', reply: null })

      expect(localStorage.getItem(`${DRAFT_KEY_PREFIX}u1`)).toBeNull()
      expect(other.hasDraft('u1', 'c:c1')).toBe(false)
      expect(other.getDraft('u1', 'c:c1')).toBeNull()
    })

    it('stays stopped when the account signs in again elsewhere', async () => {
      beginDraftSession('u1')
      const stale = await openTab()
      stale.beginDraftSession('u1')
      clearAllDrafts()
      beginDraftSession('u1')
      stale.setDraft('u1', 'c:c1', { text: 'late', reply: null })
      expect(localStorage.getItem(`${DRAFT_KEY_PREFIX}u1`)).toBeNull()
      // Its own sign-in lets it write again.
      stale.beginDraftSession('u1')
      stale.setDraft('u1', 'c:c1', { text: 'fresh', reply: null })
      expect(stale.getDraft('u1', 'c:c1')?.text).toBe('fresh')
    })

    it('forgets its in-memory drafts as soon as another tab signs out', async () => {
      const other = await openTab()
      other.beginDraftSession('u1')
      other.setDraft('u1', 'c:c1', { text: 'unsent', reply: null })
      expect(other.hasDraft('u1', 'c:c1')).toBe(true)
      localStorage.removeItem(DRAFT_SESSION_KEY)
      window.dispatchEvent(new StorageEvent('storage', { key: DRAFT_SESSION_KEY, newValue: null }))
      expect(other.hasDraft('u1', 'c:c1')).toBe(false)
    })

    it('tabs of one session share their drafts', async () => {
      beginDraftSession('u1')
      const other = await openTab()
      other.beginDraftSession('u1')
      other.setDraft('u1', 'c:c1', { text: 'from the other tab', reply: null })
      expect(localStorage.getItem(`${DRAFT_KEY_PREFIX}u1`)).toContain('from the other tab')
    })

    it('keeps working in memory when storage is blocked', () => {
      failStorage('getItem', 'setItem')
      beginDraftSession('u1')
      setDraft('u1', 'c:c1', { text: 'x', reply: null })
      expect(hasDraft('u1', 'c:c1')).toBe(true)
    })
  })
})

describe('drafts across tabs', () => {
  it('keeps a draft another tab stored when this tab saves a different one', () => {
    setDraft('u1', 'c:random', { text: 'first', reply: null })
    // Another tab saves a draft this tab has not seen.
    const key = `${DRAFT_KEY_PREFIX}u1`
    const stored = JSON.parse(localStorage.getItem(key) || '{}')
    stored['c:general'] = { text: 'from tab A', reply: null, at: 5 }
    localStorage.setItem(key, JSON.stringify(stored))
    setDraft('u1', 'c:other', { text: 'second', reply: null })
    expect(Object.keys(JSON.parse(localStorage.getItem(key) || '{}')).sort()).toEqual(['c:general', 'c:other', 'c:random'])
  })

  it('does not revive a draft another tab sent', () => {
    setDraft('u1', 'c:a', { text: 'a', reply: null })
    setDraft('u1', 'c:b', { text: 'b', reply: null })
    const key = `${DRAFT_KEY_PREFIX}u1`
    const stored = JSON.parse(localStorage.getItem(key) || '{}')
    delete stored['c:a']
    localStorage.setItem(key, JSON.stringify(stored))
    setDraft('u1', 'c:c', { text: 'c', reply: null })
    expect(Object.keys(JSON.parse(localStorage.getItem(key) || '{}')).sort()).toEqual(['c:b', 'c:c'])
  })
})
