import { describe, it, expect } from 'vitest'
import { effectiveThreshold, createPeakHold, AUTO_THRESHOLD, PEAK_HOLD_MS } from './levelMeter'

describe('effectiveThreshold', () => {
  it('uses the fixed auto value when auto sensitivity is on', () => {
    expect(effectiveThreshold({ autoSensitivity: true, sensitivityThreshold: 80 })).toBe(AUTO_THRESHOLD)
  })

  it('uses the slider value otherwise, also when stored as a string', () => {
    expect(effectiveThreshold({ autoSensitivity: false, sensitivityThreshold: '62' })).toBe(62)
  })
})

describe('createPeakHold', () => {
  it('rises immediately with the level', () => {
    const peak = createPeakHold()
    expect(peak(10, 0)).toBe(10)
    expect(peak(55, 50)).toBe(55)
  })

  it('holds the peak for the hold time, then falls back to the level', () => {
    const peak = createPeakHold()
    peak(70, 0)
    expect(peak(20, PEAK_HOLD_MS - 1)).toBe(70)
    expect(peak(20, PEAK_HOLD_MS + 1)).toBe(20)
  })

  it('restarts the hold when a new peak arrives', () => {
    const peak = createPeakHold()
    peak(70, 0)
    expect(peak(75, PEAK_HOLD_MS - 10)).toBe(75)
    expect(peak(30, PEAK_HOLD_MS + 10)).toBe(75)
    expect(peak(30, PEAK_HOLD_MS * 2)).toBe(30)
  })
})
