import { describe, expect, it } from 'vitest'
import { clockTimeOptions, dateTime, dayLabel, relative, timeShort } from './time'

// Local-time dates, so the tests do not depend on the machine's time zone.
const now = new Date(2026, 9, 25, 14, 30)
const at = (day: number, hour = 12, minute = 19, month = 9, year = 2026) => new Date(year, month, day, hour, minute)

describe('clockTimeOptions', () => {
  it('uses two digits on a 24-hour clock and none-padded hours on a 12-hour one', () => {
    expect(clockTimeOptions('de')).toEqual({ hour: '2-digit', minute: '2-digit' })
    expect(clockTimeOptions('en-US')).toEqual({ hour: 'numeric', minute: '2-digit' })
  })

  it('falls back to the 24-hour form for an unknown locale tag', () => {
    expect(clockTimeOptions('not a locale')).toEqual({ hour: '2-digit', minute: '2-digit' })
  })
})

describe('timeShort', () => {
  it('writes the time of day', () => {
    expect(timeShort(at(24, 9, 5), 'de')).toBe('09:05')
  })

  it.each([null, undefined, '', 'nonsense'])('is empty for %s', value => {
    expect(timeShort(value as string, 'de')).toBe('')
  })
})

describe('dayLabel', () => {
  it('names today and its neighbours', () => {
    expect(dayLabel(at(25), 'en', now)).toBe('Today')
    expect(dayLabel(at(24), 'en', now)).toBe('Yesterday')
    expect(dayLabel(at(26), 'en', now)).toBe('Tomorrow')
    expect(dayLabel(at(24), 'de', now)).toBe('Gestern')
  })

  it('names the weekday inside the week', () => {
    expect(dayLabel(at(21), 'en', now)).toBe('Wed')
    expect(dayLabel(at(29), 'en', now)).toBe('Thu')
  })

  it('writes the date further away, with the year only when it is not this one', () => {
    expect(dayLabel(at(10), 'en', now)).toBe('Oct 10')
    expect(dayLabel(at(10, 12, 0, 9, 2025), 'en', now)).toBe('Oct 10, 2025')
  })

  it('counts calendar days, not 24-hour spans', () => {
    expect(dayLabel(new Date(2026, 9, 24, 23, 59), 'en', new Date(2026, 9, 25, 0, 1))).toBe('Yesterday')
  })

  it('is empty for a missing or invalid value', () => {
    expect(dayLabel('', 'en', now)).toBe('')
    expect(dayLabel('nonsense', 'en', now)).toBe('')
  })
})

describe('dateTime', () => {
  it('joins the day and the time', () => {
    expect(dateTime(at(24), 'de', now)).toBe('Gestern, 12:19')
    expect(dateTime(at(10), 'de', now)).toBe('10. Okt., 12:19')
  })

  it('is empty for a missing value', () => {
    expect(dateTime(null, 'de', now)).toBe('')
  })
})

describe('relative', () => {
  it('uses the largest whole unit, past and future', () => {
    expect(relative(new Date(now.getTime() - 5 * 60_000), 'en', now)).toBe('5 min. ago')
    expect(relative(new Date(now.getTime() + 2 * 3_600_000), 'en', now)).toBe('in 2 hr.')
    expect(relative(new Date(now.getTime() - 3 * 86_400_000), 'en', now)).toBe('3 days ago')
  })

  it('says now for less than a minute', () => {
    expect(relative(new Date(now.getTime() - 10_000), 'en', now)).toBe('now')
  })

  it('is empty for a missing value', () => {
    expect(relative(undefined, 'en', now)).toBe('')
  })
})
