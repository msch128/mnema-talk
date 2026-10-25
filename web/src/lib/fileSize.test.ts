import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import { formatFileSize } from './fileSize'

beforeEach(() => setLocale('en'))

describe('formatFileSize', () => {
  it('picks the unit that fits, with one decimal at most', () => {
    expect(formatFileSize(0, 'en')).toBe('0 B')
    expect(formatFileSize(16, 'en')).toBe('16 B')
    expect(formatFileSize(1536, 'en')).toBe('1.5 KB')
    expect(formatFileSize(1048576, 'en')).toBe('1 MB')
    expect(formatFileSize(5.25 * 1048576, 'en')).toBe('5.3 MB')
    expect(formatFileSize(3 * 1024 ** 3, 'en')).toBe('3 GB')
  })

  it('writes the decimal mark of the locale and treats a missing size as empty', () => {
    expect(formatFileSize(1536, 'de')).toBe('1,5 KB')
    expect(formatFileSize(undefined, 'en')).toBe('0 B')
    expect(formatFileSize(-4, 'en')).toBe('0 B')
  })
})
