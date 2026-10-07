import { describe, it, expect } from 'vitest'
import { isContinuation, continuationIds, GROUP_WINDOW_MS } from './messageGrouping'

const at = (min: number) => new Date(Date.UTC(2026, 0, 1, 12, 0) + min * 60000).toISOString()

describe('message grouping', () => {
  it('groups same author within the window', () => {
    expect(isContinuation({ user_id: 'a', created_at: at(0) }, { user_id: 'a', created_at: at(6) })).toBe(true)
  })

  it('breaks on author change, long gaps, bad dates and reordering', () => {
    expect(isContinuation({ user_id: 'a', created_at: at(0) }, { user_id: 'b', created_at: at(1) })).toBe(false)
    expect(isContinuation({ user_id: 'a', created_at: at(0) }, { user_id: 'a', created_at: at(7) })).toBe(false)
    expect(isContinuation({ user_id: 'a', created_at: 'nope' }, { user_id: 'a', created_at: at(1) })).toBe(false)
    expect(isContinuation({ user_id: 'a', created_at: at(5) }, { user_id: 'a', created_at: at(1) })).toBe(false)
    expect(isContinuation(null, { user_id: 'a', created_at: at(1) })).toBe(false)
  })

  it('always starts a new group for replies', () => {
    const prev = { user_id: 'a', created_at: at(0) }
    expect(isContinuation(prev, { user_id: 'a', created_at: at(1), reply_to_id: 'x' })).toBe(false)
    expect(isContinuation(prev, { user_id: 'a', created_at: at(1), reply_to: { id: 'x', deleted: false } })).toBe(false)
    // A normal message after a reply may still continue the reply's group.
    expect(isContinuation({ user_id: 'a', created_at: at(1), reply_to_id: 'x' }, { user_id: 'a', created_at: at(2) })).toBe(true)
  })

  it('collects continuation ids across a list', () => {
    const msgs = [
      { id: '1', user_id: 'a', created_at: at(0) },
      { id: '2', user_id: 'a', created_at: at(1) },
      { id: '3', user_id: 'b', created_at: at(2) },
      { id: '4', user_id: 'b', created_at: at(2 + GROUP_WINDOW_MS / 60000) },
      { id: '5', user_id: 'b', created_at: at(10) }
    ]
    expect([...continuationIds(msgs)]).toEqual(['2', '5'])
    expect(continuationIds(undefined).size).toBe(0)
  })
})

it('starts a new group for a malformed nonempty timestamp', () => {
  expect(isContinuation({ user_id: 'same', created_at: 'invalid' }, { user_id: 'same', created_at: '2026-10-07T00:00:00Z' })).toBe(false)
})

it('starts a new group when either partial message has no timestamp yet', () => {
  expect(isContinuation({ user_id: 'same' }, { user_id: 'same', created_at: '2026-10-07T00:00:00Z' })).toBe(false)
  expect(isContinuation({ user_id: 'same', created_at: '2026-10-07T00:00:00Z' }, { user_id: 'same' })).toBe(false)
})
