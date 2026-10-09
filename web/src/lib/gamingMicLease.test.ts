import { expect, it, vi } from 'vitest'
import { gamingMicLease } from './gamingMicLease'
it('schedules microphone silence on the audio clock without a JS watchdog callback', () => {
  const gain = { gain: { cancelScheduledValues: vi.fn(), setValueAtTime: vi.fn() } } as unknown as GainNode
  const context = { currentTime: 10 } as AudioContext
  gamingMicLease(gain, context, true, true)
  expect(gain.gain.cancelScheduledValues).toHaveBeenCalledWith(10)
  expect(gain.gain.setValueAtTime).toHaveBeenNthCalledWith(1, 1, 10)
  expect(gain.gain.setValueAtTime).toHaveBeenNthCalledWith(2, 0, 10.3)
  vi.mocked(gain.gain.setValueAtTime).mockClear()
  gamingMicLease(gain, context, true, false)
  expect(gain.gain.setValueAtTime).toHaveBeenCalledExactlyOnceWith(0, 10)
  vi.mocked(gain.gain.setValueAtTime).mockClear()
  gamingMicLease(gain, context, false, false)
  expect(gain.gain.setValueAtTime).toHaveBeenCalledExactlyOnceWith(1, 10)
})
