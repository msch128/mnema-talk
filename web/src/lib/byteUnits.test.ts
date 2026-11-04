import { describe, expect, it } from 'vitest'
import { formatBytes } from './byteUnits'

describe('formatBytes', () => {
  it('shows zero and plain bytes without a decimal', () => {
    expect(formatBytes(0, 'en')).toBe('0 B')
    expect(formatBytes(512, 'en')).toBe('512 B')
  })

  it('steps through the 1024 based units with one decimal by default', () => {
    expect(formatBytes(1024, 'en')).toBe('1 KB')
    expect(formatBytes(1536, 'en')).toBe('1.5 KB')
    expect(formatBytes(3 * 1024 * 1024, 'en')).toBe('3 MB')
    expect(formatBytes(5 * 1024 ** 3, 'en')).toBe('5 GB')
  })

  it('keeps a size above the last unit in TB instead of reading past the list', () => {
    expect(formatBytes(2048 * 1024 ** 4, 'en')).toBe('2,048 TB')
  })

  it('writes the number in the locale and honours the digits asked for', () => {
    expect(formatBytes(1536, 'de')).toBe('1,5 KB')
    expect(formatBytes(1024 * 1024 * 1.234, 'en', 2)).toBe('1.23 MB')
  })
})
