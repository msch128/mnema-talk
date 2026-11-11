import { describe, expect, it } from 'vitest'
import { pttKeyLabel } from './pttKey'

describe('pttKeyLabel', () => {
  it('shows keys as the word a member would say', () => {
    expect(pttKeyLabel('KeyV')).toBe('V')
    expect(pttKeyLabel('Digit4')).toBe('4')
    expect(pttKeyLabel('Numpad5')).toBe('Num 5')
    expect(pttKeyLabel('F8')).toBe('F8')
    expect(pttKeyLabel('Backquote')).toBe('`')
    expect(pttKeyLabel('Space')).toBe('Space')
    expect(pttKeyLabel('ArrowUp')).toBe('Up')
    expect(pttKeyLabel('PageDown')).toBe('Page Down')
    expect(pttKeyLabel('x')).toBe('x')
    expect(pttKeyLabel('CapsLock')).toBe('Caps Lock')
    expect(pttKeyLabel('Backslash')).toBe('\\')
  })
})
