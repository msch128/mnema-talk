import { describe, it, expect } from 'vitest'
import { aggregateActivity, titlePrefix } from './channelActivity'

describe('aggregateActivity', () => {
  const channels = [{ id: 'a' }, { id: 'b' }, { id: 'c' }, { id: 'unknown' }]

  it('counts channels with news and adds up the mentions', () => {
    const states = {
      a: { unread_count: 3, mention_count: 1, notify_level: 'all' },
      b: { unread_count: 0, mention_count: 0, notify_level: 'all' },
      c: { unread_count: 1, mention_count: 2, notify_level: 'mentions' }
    }
    expect(aggregateActivity(channels, states)).toEqual({ unreadChannels: 2, mentions: 3 })
  })

  it('does not count the unreads of a muted channel but keeps its mentions', () => {
    const states = { a: { unread_count: 9, mention_count: 0, notify_level: 'mute' }, b: { unread_count: 1, mention_count: 2, notify_level: 'mute' } }
    expect(aggregateActivity(channels, states)).toEqual({ unreadChannels: 0, mentions: 2 })
  })

  it('treats missing counts as zero and an empty group as quiet', () => {
    expect(aggregateActivity(channels, { a: {} })).toEqual({ unreadChannels: 0, mentions: 0 })
    expect(aggregateActivity([], {})).toEqual({ unreadChannels: 0, mentions: 0 })
  })
})

describe('titlePrefix', () => {
  it('shows mentions first, then a dot, otherwise nothing', () => {
    expect(titlePrefix({ unreadChannels: 2, mentions: 3 })).toBe('(3) ')
    expect(titlePrefix({ unreadChannels: 0, mentions: 250 })).toBe('(99+) ')
    expect(titlePrefix({ unreadChannels: 1, mentions: 0 })).toBe('• ')
    expect(titlePrefix({ unreadChannels: 0, mentions: 0 })).toBe('')
  })
})
