import { describe, expect, it } from 'vitest'
import { formatEventStart, relativeEventDay } from './eventTime'

const NOW = new Date(2026, 9, 30, 20, 0).getTime()

describe('relativeEventDay', () => {
  it('counts calendar days, so a late event today is "today" and the next morning "tomorrow"', () => {
    expect(relativeEventDay(new Date(2026, 9, 30, 23, 30).toISOString(), NOW, 'en')).toBe('today')
    expect(relativeEventDay(new Date(2026, 9, 31, 7, 0).toISOString(), NOW, 'en')).toBe('tomorrow')
    expect(relativeEventDay(new Date(2026, 10, 2, 12, 0).toISOString(), NOW, 'en')).toBe('in 3 days')
    expect(relativeEventDay(new Date(2026, 9, 28, 12, 0).toISOString(), NOW, 'en')).toBe('2 days ago')
  })

  it('speaks German for a German member', () => {
    expect(relativeEventDay(new Date(2026, 9, 31, 7, 0).toISOString(), NOW, 'de')).toBe('morgen')
  })

  it('gives nothing for a date that does not parse', () => {
    expect(relativeEventDay('soon', NOW, 'en')).toBe('')
    expect(formatEventStart('soon', 'en')).toBe('')
  })
})

describe('formatEventStart', () => {
  it('starts with the weekday and ends with the time', () => {
    const text = formatEventStart(new Date(2026, 10, 1, 18, 5).toISOString(), 'en')
    expect(text).toMatch(/^Sun/)
    expect(text).toMatch(/6:05\s?PM$/)
  })
})
