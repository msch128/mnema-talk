import { describe, it, expect, vi, afterEach } from 'vitest'
import {
  escapeRegExp, mentionsUser, shouldNotify, extractPreviewUrls,
  createKeyedThrottle, firstUnreadId, typingLine
} from './chatLogic'

afterEach(() => vi.useRealTimers())

describe('escapeRegExp / mentionsUser', () => {
  it('escapes every regexp metacharacter', () => {
    expect(escapeRegExp('a.b*c+d?(e)[f]{g}|h^i$j\\k')).toBe('a\\.b\\*c\\+d\\?\\(e\\)\\[f\\]\\{g\\}\\|h\\^i\\$j\\\\k')
  })

  it('matches a username with special characters literally', () => {
    expect(mentionsUser('hi @a.b!', 'a.b')).toBe(true)
    expect(mentionsUser('hi @axb!', 'a.b')).toBe(false)
    expect(() => mentionsUser('hi @x', 'x(')).not.toThrow()
    expect(mentionsUser('hi @x(', 'x(')).toBe(true)
  })

  it('is case-insensitive and needs a word boundary', () => {
    expect(mentionsUser('@Max hallo', 'max')).toBe(true)
    expect(mentionsUser('mail@max.de', 'max')).toBe(false)
    expect(mentionsUser('@maxine', 'max')).toBe(false)
    expect(mentionsUser('', 'max')).toBe(false)
    expect(mentionsUser('@max', '')).toBe(false)
  })
})

describe('shouldNotify', () => {
  const base: Parameters<typeof shouldNotify>[0] = { permission: 'granted', level: 'all', isMention: false, hidden: false, isCurrentChannel: false }

  it('needs permission', () => {
    expect(shouldNotify({ ...base, permission: 'default' })).toBe(false)
    expect(shouldNotify({ ...base, permission: 'denied' })).toBe(false)
    expect(shouldNotify(base)).toBe(true)
  })

  it('respects the channel level', () => {
    expect(shouldNotify({ ...base, level: 'mute', isMention: true })).toBe(false)
    expect(shouldNotify({ ...base, level: 'mentions' })).toBe(false)
    expect(shouldNotify({ ...base, level: 'mentions', isMention: true })).toBe(true)
  })

  it('stays quiet for the open channel in a visible tab only', () => {
    expect(shouldNotify({ ...base, isCurrentChannel: true })).toBe(false)
    expect(shouldNotify({ ...base, isCurrentChannel: true, hidden: true })).toBe(true)
  })
})

describe('extractPreviewUrls', () => {
  it('finds plain links, deduplicated and capped', () => {
    const text = 'a https://a.de b https://a.de c https://b.de d https://c.de e https://d.de'
    expect(extractPreviewUrls(text)).toEqual(['https://a.de', 'https://b.de', 'https://c.de'])
  })

  it('ignores links in code and spoilers', () => {
    expect(extractPreviewUrls('`https://a.de`')).toEqual([])
    expect(extractPreviewUrls('```\nhttps://a.de\n```')).toEqual([])
    expect(extractPreviewUrls('||https://a.de||')).toEqual([])
    expect(extractPreviewUrls('`x` https://ok.de ||https://no.de||')).toEqual(['https://ok.de'])
  })

  it('keeps trailing punctuation out of the url', () => {
    expect(extractPreviewUrls('(siehe https://a.de/x.)')).toEqual(['https://a.de/x'])
    expect(extractPreviewUrls('')).toEqual([])
  })
})

describe('createKeyedThrottle', () => {
  it('retires every trailing callback and key history on community reset', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const th = createKeyedThrottle(fn, 2000)
    th.call('a'); th.call('b'); th.call('a'); th.call('b')
    th.reset()
    vi.advanceTimersByTime(5000)
    expect(fn).toHaveBeenCalledTimes(2)
    th.call('a')
    expect(fn).toHaveBeenCalledTimes(3)
    th.reset()
  })
  it('runs immediately, then at most once per interval with a trailing call', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const th = createKeyedThrottle(fn, 2000)
    th.call('a')
    th.call('a')
    th.call('a')
    expect(fn).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(1999)
    expect(fn).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(1)
    expect(fn).toHaveBeenCalledTimes(2)
    th.call('a')
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('throttles keys independently and can cancel', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const th = createKeyedThrottle(fn, 2000)
    th.call('a')
    th.call('b')
    expect(fn).toHaveBeenCalledTimes(2)
    th.call('a')
    th.cancel('a')
    vi.advanceTimersByTime(5000)
    expect(fn).toHaveBeenCalledTimes(2)
  })
})

describe('firstUnreadId', () => {
  const msgs = [
    { id: '1', user_id: 'o', created_at: '2026-01-01T10:00:00Z' },
    { id: '2', user_id: 'o', created_at: '2026-01-01T11:00:00Z' },
    { id: '3', user_id: 'me', created_at: '2026-01-01T11:30:00Z' },
    { id: '4', user_id: 'o', created_at: '2026-01-01T12:00:00Z' }
  ]

  it('points at the first newer message from someone else', () => {
    expect(firstUnreadId(msgs, '2026-01-01T10:30:00Z', 'me')).toBe('2')
    expect(firstUnreadId(msgs, '2026-01-01T11:10:00Z', 'me')).toBe('4')
  })

  it('is null when nothing is new or the read time is unknown', () => {
    expect(firstUnreadId(msgs, '2026-01-02T00:00:00Z', 'me')).toBeNull()
    expect(firstUnreadId(msgs, null, 'me')).toBeNull()
    expect(firstUnreadId([], '2026-01-01T00:00:00Z', 'me')).toBeNull()
  })

  it('does not guess when older history is not loaded', () => {
    expect(firstUnreadId(msgs, '2025-12-01T00:00:00Z', 'me', true)).toBeNull()
    expect(firstUnreadId(msgs, '2025-12-01T00:00:00Z', 'me', false)).toBe('1')
  })
})

describe('typingLine', () => {
  it('describes one, two and several typers', () => {
    expect(typingLine([])).toBeNull()
    expect(typingLine([{ display_name: 'Anna', username: 'anna' }])).toEqual({ key: 'chat.isTyping', params: { name: 'Anna' } })
    expect(typingLine([{ display_name: 'Anna', username: 'anna' }, { username: 'ben', display_name: '' }]))
      .toEqual({ key: 'chat.twoTyping', params: { a: 'Anna', b: 'ben' } })
    expect(typingLine([{ username: 'a', display_name: '' }, { username: 'b', display_name: '' }, { username: 'c', display_name: '' }])?.key).toBe('chat.severalTyping')
  })
})
