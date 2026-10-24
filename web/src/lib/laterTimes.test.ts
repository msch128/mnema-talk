import { describe, expect, it } from 'vitest'
import {
  earliestInputValue, fromLocalInputValue, isAcceptableTime, presetTime, toLocalInputValue, tomorrowMorning
} from './laterTimes'

const now = new Date(2026, 9, 24, 21, 30, 15) // 24 October 2026, 21:30:15 local

describe('the times of Remind me and Send later', () => {
  it('counts the relative presets from now', () => {
    expect(presetTime('in20m', now).getTime() - now.getTime()).toBe(20 * 60_000)
    expect(presetTime('in30m', now).getTime() - now.getTime()).toBe(30 * 60_000)
    expect(presetTime('in1h', now).getTime() - now.getTime()).toBe(3_600_000)
    expect(presetTime('in3h', now).getTime() - now.getTime()).toBe(3 * 3_600_000)
  })

  it('means 08:00 on the local clock of the next day for tomorrow morning', () => {
    const at = tomorrowMorning(now)
    expect([at.getFullYear(), at.getMonth(), at.getDate(), at.getHours(), at.getMinutes(), at.getSeconds()]).toEqual([2026, 9, 25, 8, 0, 0])
    // Also across the end of a month and a year.
    const eve = tomorrowMorning(new Date(2026, 11, 31, 23, 59))
    expect([eve.getFullYear(), eve.getMonth(), eve.getDate(), eve.getHours()]).toEqual([2027, 0, 1, 8])
  })

  it('writes and reads the value of a datetime-local input in local time', () => {
    expect(toLocalInputValue(new Date(2026, 0, 5, 7, 4))).toBe('2026-01-05T07:04')
    const back = fromLocalInputValue('2026-10-25T08:00')
    expect(back?.getTime()).toBe(new Date(2026, 9, 25, 8, 0).getTime())
    expect(fromLocalInputValue('')).toBeNull()
    expect(fromLocalInputValue('tomorrow')).toBeNull()
    expect(fromLocalInputValue('2026-02-31T10:00')).toBeNull()
  })

  it('accepts a time between ten seconds and a year ahead, as the server does', () => {
    expect(isAcceptableTime(new Date(now.getTime() + 5_000), now)).toBe(false)
    expect(isAcceptableTime(new Date(now.getTime() - 60_000), now)).toBe(false)
    expect(isAcceptableTime(new Date(now.getTime() + 60_000), now)).toBe(true)
    expect(isAcceptableTime(new Date(now.getTime() + 367 * 86_400_000), now)).toBe(false)
  })

  it('offers a custom picker that starts at the next full minute or later', () => {
    expect(earliestInputValue(now)).toBe('2026-10-24T21:32')
  })
})
