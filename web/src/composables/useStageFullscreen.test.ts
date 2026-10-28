import { describe, expect, it } from 'vitest'
import { isStageFullscreenKey } from './useStageFullscreen'

const press = (key: string, code: string) => new KeyboardEvent('keydown', { key, code })

describe('isStageFullscreenKey', () => {
  it('takes the letter f on a Latin layout', () => {
    expect(isStageFullscreenKey(press('f', 'KeyF'))).toBe(true)
    expect(isStageFullscreenKey(press('F', 'KeyF'))).toBe(true)
    // Dvorak: the letter f sits on another physical key; the key that is KeyF there types u.
    expect(isStageFullscreenKey(press('f', 'KeyY'))).toBe(true)
    expect(isStageFullscreenKey(press('u', 'KeyF'))).toBe(false)
  })

  it('takes the physical F key where the layout types no Latin letter', () => {
    expect(isStageFullscreenKey(press('а', 'KeyF'))).toBe(true)
    expect(isStageFullscreenKey(press('α', 'KeyF'))).toBe(true)
    expect(isStageFullscreenKey(press('а', 'KeyA'))).toBe(false)
  })

  it('ignores other keys', () => {
    expect(isStageFullscreenKey(press('g', 'KeyG'))).toBe(false)
    expect(isStageFullscreenKey(press('Enter', 'Enter'))).toBe(false)
  })
})
