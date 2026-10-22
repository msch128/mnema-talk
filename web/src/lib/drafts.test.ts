import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { channelDraftKey, clearAllDrafts, DRAFT_KEY_PREFIX, getDraft, hasDraft, MAX_DRAFT_TEXT, MAX_DRAFTS, setDraft, threadDraftKey } from './drafts'

beforeEach(() => {
  localStorage.clear()
  clearAllDrafts()
})
afterEach(() => vi.restoreAllMocks())

describe('drafts', () => {
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

  it('works in memory when storage is blocked', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'key').mockImplementation(() => { throw new Error('blocked') })
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
})
