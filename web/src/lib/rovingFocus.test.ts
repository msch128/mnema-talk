import { describe, expect, it } from 'vitest'
import { rovingTarget } from './rovingFocus'

describe('rovingTarget', () => {
  it('wraps the arrows around the ends', () => {
    expect(rovingTarget('ArrowRight', 3, 4)).toBe(0)
    expect(rovingTarget('ArrowLeft', 0, 4)).toBe(3)
    expect(rovingTarget('ArrowDown', 1, 4)).toBe(2)
    expect(rovingTarget('ArrowUp', 1, 4)).toBe(0)
  })
  it('jumps with Home and End', () => {
    expect(rovingTarget('Home', 2, 4)).toBe(0)
    expect(rovingTarget('End', 1, 4)).toBe(3)
  })
  it('ignores other keys and empty groups', () => {
    expect(rovingTarget('a', 1, 4)).toBeNull()
    expect(rovingTarget('ArrowRight', 0, 0)).toBeNull()
  })
})
