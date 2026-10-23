import { describe, it, expect } from 'vitest'
import type { Poll } from '../types/domain'
import {
  POLL_DURATIONS, draftProblem, draftRequest, emptyDraft, isEnded, isPollDuration, leaders, nextSelection, percent, remaining, votedFor
} from './polls'

const NOW = Date.parse('2026-10-22T12:00:00Z')
const at = (offsetMs: number) => new Date(NOW + offsetMs).toISOString()

function poll(overrides: Partial<Poll> = {}): Poll {
  return {
    multiple: false, anonymous: false, closed: false, closes_at: at(3_600_000), total_voters: 3,
    options: [
      { id: 'a', label: 'A', votes: 2, voters: ['u1', 'u2'] },
      { id: 'b', label: 'B', votes: 1, voters: ['u3'] },
      { id: 'c', label: 'C', votes: 0 }
    ],
    ...overrides
  }
}

describe('when voting is over', () => {
  it('follows the server, or the clock once the end time has passed', () => {
    expect(isEnded(poll(), NOW)).toBe(false)
    expect(isEnded(poll({ closed: true }), NOW)).toBe(true)
    expect(isEnded(poll({ closes_at: at(-1) }), NOW)).toBe(true)
    expect(isEnded(poll({ closes_at: at(0) }), NOW)).toBe(true)
  })

  it('names the time left in the unit that reads best, rounding up', () => {
    expect(remaining(poll({ closes_at: at(30_000) }), NOW)).toEqual({ unit: 'minute', count: 1 })
    expect(remaining(poll({ closes_at: at(59 * 60_000) }), NOW)).toEqual({ unit: 'minute', count: 59 })
    expect(remaining(poll({ closes_at: at(61 * 60_000) }), NOW)).toEqual({ unit: 'hour', count: 2 })
    expect(remaining(poll({ closes_at: at(47 * 3_600_000) }), NOW)).toEqual({ unit: 'hour', count: 47 })
    expect(remaining(poll({ closes_at: at(72 * 3_600_000) }), NOW)).toEqual({ unit: 'day', count: 3 })
    expect(remaining(poll({ closes_at: at(7 * 86_400_000) }), NOW)).toEqual({ unit: 'day', count: 7 })
    expect(remaining(poll({ closes_at: at(-5) }), NOW)).toBeNull()
    expect(remaining(poll({ closed: true }), NOW)).toBeNull()
  })
})

describe('tallies', () => {
  it('shows the share of voters, and 0 while nobody voted', () => {
    expect(percent(1, 3)).toBe(33)
    expect(percent(2, 3)).toBe(67)
    expect(percent(3, 3)).toBe(100)
    expect(percent(0, 0)).toBe(0)
  })

  it('finds the leaders, none before the first vote', () => {
    expect(leaders(poll())).toEqual(['a'])
    expect(leaders(poll({ options: [{ id: 'a', label: 'A', votes: 1 }, { id: 'b', label: 'B', votes: 1 }] }))).toEqual(['a', 'b'])
    expect(leaders(poll({ options: [{ id: 'a', label: 'A', votes: 0 }] }))).toEqual([])
  })

  it('reads a member\'s own votes from the voters, if the poll names them', () => {
    expect(votedFor(poll(), 'u3')).toEqual(['b'])
    expect(votedFor(poll(), 'nobody')).toEqual([])
    expect(votedFor(poll(), undefined)).toEqual([])
    expect(votedFor(poll({ anonymous: true, options: [{ id: 'a', label: 'A', votes: 2 }] }), 'u1')).toEqual([])
  })
})

describe('what a click does', () => {
  it('replaces the answer in a single-choice poll and takes it back on a second click', () => {
    expect(nextSelection([], 'a', false)).toEqual(['a'])
    expect(nextSelection(['a'], 'b', false)).toEqual(['b'])
    expect(nextSelection(['a'], 'a', false)).toEqual([])
  })

  it('adds and removes answers in a multiple-choice poll', () => {
    expect(nextSelection(['a'], 'b', true)).toEqual(['a', 'b'])
    expect(nextSelection(['a', 'b'], 'a', true)).toEqual(['b'])
  })
})

describe('the poll to post', () => {
  it('needs a question, two different answers, and ignores empty fields', () => {
    const draft = emptyDraft()
    expect(draftProblem(draft)).toBe('question')
    draft.question = '  Pizza?  '
    expect(draftProblem(draft)).toBe('options')
    draft.options = ['Yes', ' ', '']
    expect(draftProblem(draft)).toBe('options')
    draft.options = ['Yes', 'yes ']
    expect(draftProblem(draft)).toBe('duplicate')
    draft.options = [' Yes ', 'No', '']
    expect(draftProblem(draft)).toBeNull()
    expect(draftRequest(draft)).toEqual({ question: 'Pizza?', options: ['Yes', 'No'], duration: '24h', multiple: false, anonymous: false })
  })

  it('offers the durations the server accepts', () => {
    expect(POLL_DURATIONS.every(isPollDuration)).toBe(true)
    expect(isPollDuration('5m')).toBe(false)
    expect(isPollDuration(undefined)).toBe(false)
  })
})
