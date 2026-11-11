import { describe, expect, it } from 'vitest'
import { isFiniteNumber } from './mediaStats'
describe('browser media numeric validation', () => {
  it('preserves finite zero and negative counters but rejects absent, textual and nonfinite values', () => {
    for (const value of [0, 12.4, -1, Number.MAX_VALUE]) expect(isFiniteNumber(value)).toBe(true)
    for (const value of [undefined, null, '', '12', NaN, Infinity, -Infinity, {}, []]) expect(isFiniteNumber(value)).toBe(false)
  })
})
