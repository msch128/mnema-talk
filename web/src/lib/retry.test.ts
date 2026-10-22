import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { retryWithBackoff } from './retry'

beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('retryWithBackoff', () => {
  it('tries once more after each wait and stops at the first success', async () => {
    const task = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true)
    const result = retryWithBackoff(task, [100, 200, 400])
    expect(task).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(100)
    expect(await result).toBe(true)
    expect(task).toHaveBeenCalledTimes(2)
  })

  it('gives up after the last wait', async () => {
    const task = vi.fn().mockResolvedValue(false)
    const result = retryWithBackoff(task, [10, 20])
    await vi.advanceTimersByTimeAsync(100)
    expect(await result).toBe(false)
    expect(task).toHaveBeenCalledTimes(3)
  })

  it('stops when the result is no longer wanted', async () => {
    const task = vi.fn().mockResolvedValue(false)
    let wanted = true
    const result = retryWithBackoff(task, [10, 20], () => wanted)
    await vi.advanceTimersByTimeAsync(0)
    wanted = false
    await vi.advanceTimersByTimeAsync(100)
    expect(await result).toBe(false)
    expect(task).toHaveBeenCalledTimes(1)
  })
})
