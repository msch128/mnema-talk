// The pacer of media handshakes (pacer.ts) counts the last second of sends by the wall clock (Date.now). When the
// wall clock is set back, every send already counted looks like it is still in the future: none of them expires,
// the count only grows, and after 8 sends the pacer holds back everything until the clock has caught up with the
// step. A member whose clock is set back by D (NTP after a wrong clock, a resumed virtual machine) then cannot send
// a heartbeat, a commit or a key package for up to D. Expected: a step back costs nothing.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { WirePacer } from './pacer'

afterEach(() => { vi.useRealTimers() })

async function sendsAfterStep(stepMs: number): Promise<number> {
  vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: Date.UTC(2026, 0, 1) })
  const pacer = new WirePacer()
  let sent = 0
  const send = () => { sent++ }
  for (let i = 0; i < 3; i++) pacer.push('x', send)
  await vi.advanceTimersByTimeAsync(2000)
  vi.setSystemTime(Date.now() + stepMs)
  sent = 0
  // Ten messages, one a second apart: well inside the budget of 8 a second.
  for (let i = 0; i < 10; i++) {
    pacer.push('x', send)
    await vi.advanceTimersByTimeAsync(1000)
  }
  return sent
}

describe('the pacer of media handshakes when the wall clock is set', () => {
  it('forward: every message goes out', async () => {
    expect(await sendsAfterStep(10 * 60_000)).toBe(10)
  })

  // The defect (found with the simulator's wall clock steps): known, it must turn red the day the pacer stops reading the wall clock.
  it.fails('back by ten minutes: every message goes out', async () => {
    expect(await sendsAfterStep(-10 * 60_000)).toBe(10)
  })

  it('back by ten minutes: what is held back waits for the clock to catch up (the defect, measured)', async () => {
    expect(await sendsAfterStep(-10 * 60_000)).toBeLessThan(10)
  })
})
