import { beforeEach, describe, expect, it } from 'vitest'
import { NO_WAIT, newMemberTimes, nextOpening, waitLeft, waitSentence } from './newMember'
import { setLocale } from '../i18n'

beforeEach(() => setLocale('en'))

describe('the waits of a new member', () => {
  const now = Date.parse('2026-11-02T12:00:00Z')
  const times = newMemberTimes({ write_at: '2026-11-02T12:10:00Z', react_at: '2026-11-02T12:30:00Z' })

  it('reads the moments the server names and leaves the rest open', () => {
    expect(times).toEqual({ write: now + 600_000, react: now + 1_800_000, speak: null })
    expect(newMemberTimes(undefined)).toEqual(NO_WAIT)
    expect(newMemberTimes({ write_at: 'not a time' }).write).toBeNull()
  })

  it('counts what is left per action, never below zero', () => {
    expect(waitLeft(times, 'write', now)).toBe(600_000)
    expect(waitLeft(times, 'write', now + 700_000)).toBe(0)
    expect(waitLeft(times, 'speak', now)).toBe(0)
  })

  it('finds the first moment something opens up', () => {
    expect(nextOpening(times, now)).toBe(now + 600_000)
    expect(nextOpening(times, now + 700_000)).toBe(now + 1_800_000)
    expect(nextOpening(times, now + 2_000_000)).toBeNull()
    expect(nextOpening(NO_WAIT, now)).toBeNull()
  })

  it('says when in the calm words of the action, rounding up to whole minutes', () => {
    expect(waitSentence('write', 12 * 60_000 - 5_000)).toBe('You can write in 12 min.')
    expect(waitSentence('react', 3_600_000)).toBe('You can react in 1 h.')
    expect(waitSentence('speak', 30_000)).toBe('You can speak in 30 s.')
  })
})
