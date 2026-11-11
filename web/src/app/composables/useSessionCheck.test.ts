import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSessionCheck } from './useSessionCheck'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('useSessionCheck', () => {
  it('keeps trying by itself while the server does not answer and then returns the answer', async () => {
    const check = vi.fn<() => Promise<string | null>>().mockResolvedValueOnce(null).mockResolvedValueOnce('me')
    const s = useSessionCheck(check)
    const done = s.run()
    await vi.advanceTimersByTimeAsync(0)
    expect(s.retrying.value).toBe(true)
    await vi.advanceTimersByTimeAsync(1000)
    expect(await done).toBe('me')
    expect(s.retrying.value).toBe(false)
  })

  it('tries at once when asked, instead of waiting out the pause, and remembers when it last tried', async () => {
    vi.setSystemTime(new Date('2026-11-06T10:00:00Z'))
    const check = vi.fn<() => Promise<string | null>>().mockResolvedValue(null)
    const s = useSessionCheck(check, 60000)
    void s.run()
    await vi.advanceTimersByTimeAsync(0)
    expect(check).toHaveBeenCalledTimes(1)
    expect(s.lastTriedAt.value).toBe(Date.now())
    s.retryNow()
    await vi.advanceTimersByTimeAsync(0)
    expect(check).toHaveBeenCalledTimes(2)
    s.dispose()
  })

  it('shows that a try is running while it runs', async () => {
    let answer: (v: string | null) => void = () => {}
    const s = useSessionCheck(() => new Promise<string | null>(r => { answer = r }))
    const done = s.run()
    expect(s.trying.value).toBe(true)
    answer('me')
    await done
    expect(s.trying.value).toBe(false)
  })
})
