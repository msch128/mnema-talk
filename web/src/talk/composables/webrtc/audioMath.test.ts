import { describe, it, expect, vi } from 'vitest'
import { aiModel, calculateRMSLevel, setNodeGain } from './audioMath'

function analyserWith(sample: number) {
  return { getByteTimeDomainData: (buf: Uint8Array) => buf.fill(sample) } as unknown as AnalyserNode
}

describe('microphone level and gain helpers', () => {
  it('maps the noise modes to their models', () => {
    expect(aiModel('ai')).toBe('dfn3')
    expect(aiModel('ai-lite')).toBe('gtcrn')
    expect(aiModel('browser')).toBeNull()
    expect(aiModel(null)).toBeNull()
  })

  it('reports silence, speech and clipping on a 0-100 scale', () => {
    const buf = new Uint8Array(64)
    expect(calculateRMSLevel(null, buf)).toBe(0)
    expect(calculateRMSLevel(analyserWith(128), buf)).toBe(0)
    const speech = calculateRMSLevel(analyserWith(128 + 16), buf)
    expect(speech).toBeGreaterThan(40)
    expect(speech).toBeLessThan(100)
    expect(calculateRMSLevel(analyserWith(255), buf)).toBe(100)
    expect(calculateRMSLevel(analyserWith(128 + 1), buf)).toBeGreaterThanOrEqual(0)
  })

  it('sets a gain on the audio clock, or directly where scheduling is missing', () => {
    const scheduled = { gain: { setValueAtTime: vi.fn(), value: 1 } } as unknown as GainNode
    setNodeGain(scheduled, 0.5, 2)
    expect(scheduled.gain.setValueAtTime).toHaveBeenCalledWith(0.5, 2)
    const plain = { gain: { value: 1 } } as unknown as GainNode
    setNodeGain(plain, 0.25)
    expect(plain.gain.value).toBe(0.25)
    const throwing = { gain: { setValueAtTime: () => { throw new Error('closed') } } } as unknown as GainNode
    expect(() => setNodeGain(throwing, 1)).not.toThrow()
    expect(() => setNodeGain(null, 1)).not.toThrow()
  })
})
