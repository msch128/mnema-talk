import { afterEach, describe, expect, it } from 'vitest'
import { formatTime } from './useMessageActions'
import { setLocale } from '../i18n'

// Local wall-clock time, so the expectation does not depend on the runner's zone.
const at = (h: number, m: number) => new Date(2026, 9, 23, h, m).toISOString()
// Some ICU builds use a narrow no-break space before AM/PM.
const plain = (s: string) => s.replace(/\s/g, " ")

describe('formatTime', () => {
  afterEach(() => setLocale('en'))

  it('writes a 12-hour clock without a leading zero in English', () => {
    setLocale('en')
    expect(plain(formatTime(at(13, 13)))).toBe('1:13 PM')
    expect(plain(formatTime(at(11, 52)))).toBe('11:52 AM')
    expect(plain(formatTime(at(12, 4)))).toBe('12:04 PM')
    expect(plain(formatTime(at(0, 5)))).toBe('12:05 AM')
    expect(plain(formatTime(at(9, 0)))).toBe('9:00 AM')
  })

  it('keeps the 24-hour clock in German', () => {
    setLocale('de')
    expect(formatTime(at(13, 13))).toBe('13:13')
    expect(formatTime(at(9, 5))).toBe('09:05')
  })

  it('is empty without a date', () => {
    expect(formatTime(null)).toBe('')
  })
})
