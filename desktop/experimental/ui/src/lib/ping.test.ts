import { describe, it, expect } from 'vitest'
import { pingTone } from './ping'

describe('pingTone', () => {
  it('uses forest ≤80 ms, warning ≤200 ms, danger above', () => {
    expect(pingTone(4)).toBe('good')
    expect(pingTone(80)).toBe('good')
    expect(pingTone(81)).toBe('warn')
    expect(pingTone(200)).toBe('warn')
    expect(pingTone(201)).toBe('bad')
  })
  it('is neutral without a measurement', () => {
    expect(pingTone(null)).toBe('none')
    expect(pingTone(undefined)).toBe('none')
    expect(pingTone(NaN)).toBe('none')
  })
})
