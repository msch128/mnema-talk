import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { noteServerTime, resetServerTime, serverNowAtLeast } from './serverTime'

// The client's estimate of the server's clock never runs ahead of the server,
// whatever the network delays and however the device's wall clock is stepped
// (doc/timed-messages.md): a message is never ended before its end on the
// server. With one prompt sample it is close behind.

function rng(seed: number) {
  let s = seed >>> 0
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0
    return s / 2 ** 32
  }
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date', 'performance', 'setTimeout', 'clearTimeout'] })
  resetServerTime()
})
afterEach(() => vi.useRealTimers())

describe('the lower bound of the server clock', () => {
  it('is null before any server time was seen', () => {
    expect(serverNowAtLeast()).toBeNull()
  })

  it('never runs ahead of the server under delayed samples and stepped device clocks', () => {
    for (let seed = 1; seed <= 300; seed++) {
      const r = rng(seed)
      resetServerTime()
      // The true server time is the monotonic time plus a fixed offset; the
      // device's wall clock is off by its own amount, which steps now and then.
      const serverOffset = Math.floor((r() - 0.5) * 3_600_000)
      const wallSkew = Math.floor((r() - 0.5) * 600_000)
      vi.setSystemTime(1_800_000_000_000 + wallSkew)
      const start = performance.now()
      const trueServer = () => 1_800_000_000_000 + serverOffset + (performance.now() - start)
      for (let step = 0; step < 80; step++) {
        const roll = r()
        const advance = Math.floor(r() * 5000)
        vi.advanceTimersByTime(advance)
        if (roll < 0.4) {
          // A server timestamp that was taken `delay` ago (a slow answer, a crowded socket).
          const delay = Math.floor(r() ** 3 * 60_000)
          noteServerTime(trueServer() - delay)
        } else if (roll < 0.5) {
          // The device's clock is stepped (NTP, the user), either way.
          const jump = Math.floor((r() - 0.5) * 120_000)
          vi.setSystemTime(Date.now() + jump)
        }
        const est = serverNowAtLeast()
        if (est === null) continue
        expect(est, `seed ${seed} step ${step}: the estimate ran ahead`).toBeLessThanOrEqual(trueServer() + 1)
      }
    }
  })

  it('stays within the delay of its best sample', () => {
    vi.setSystemTime(1_800_000_000_000)
    const serverAhead = 7_000
    noteServerTime(Date.now() + serverAhead - 40_000) // a late one
    noteServerTime(Date.now() + serverAhead - 150) // a prompt one
    vi.advanceTimersByTime(10_000)
    const est = serverNowAtLeast()!
    const truth = Date.now() + serverAhead
    expect(est).toBeLessThanOrEqual(truth)
    expect(truth - est).toBeLessThanOrEqual(150)
  })
})
