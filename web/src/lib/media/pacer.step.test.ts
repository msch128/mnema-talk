// The pacer of media handshakes (pacer.ts) counts the last second of sends. On the wall clock (Date.now, as before
// the fix of H-L2-05) a step back made every send already counted look like it was still in the future: none of them
// expired, the count only grew, and after 8 sends the pacer held back everything until the clock had caught up with
// the step. A member whose clock was set back by D (NTP after a wrong clock, a resumed virtual machine) then could not
// send a heartbeat, a commit or a key package for up to D. The pacer now counts on the monotonic clock of the key
// lifecycle (clock.ts), which a wall-clock step does not move: a step either way costs nothing.
//
// Whether the monotonic clock advances while the machine sleeps depends on the browser. Both are safe for a window
// of one second: a clock that stood still holds back what is queued at most one second of awake time (and never lets
// more than the budget through within a second of it), a clock that jumped forward lets the queue go at once.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clock, monotonicNow } from './clock'
import { HANDSHAKES_PER_SEC, WirePacer } from './pacer'

const testNow = clock.now
afterEach(() => {
  clock.now = testNow
  vi.useRealTimers()
})

/** Three sends, then the wall clock is set by `stepMs`, then ten sends one second apart. */
async function sendsAfterStep(stepMs: number): Promise<number> {
  // Faking performance too, as in a browser: setSystemTime moves the wall clock, not the monotonic one.
  vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout', 'performance'], now: Date.UTC(2026, 0, 1) })
  clock.now = monotonicNow
  const pacer = new WirePacer()
  let sent = 0
  const send = () => { sent++ }
  for (let i = 0; i < 3; i++) pacer.push('x', send)
  await vi.advanceTimersByTimeAsync(2000)
  const mono = clock.now()
  vi.setSystemTime(Date.now() + stepMs)
  // The rig: the step moved the wall clock only.
  expect(clock.now()).toBe(mono)
  sent = 0
  // Ten messages, one a second apart: well inside the budget of 8 a second.
  for (let i = 0; i < 10; i++) {
    pacer.push('x', send)
    await vi.advanceTimersByTimeAsync(1000)
  }
  return sent
}

/** A pacer on a hand-moved monotonic clock, with `HANDSHAKES_PER_SEC + 4` messages queued at once. */
function burst() {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  const mono = { at: 0 }
  const pacer = new WirePacer(() => mono.at)
  const sent: number[] = []
  for (let n = 0; n < HANDSHAKES_PER_SEC + 4; n++) pacer.push('x', () => sent.push(mono.at))
  return { mono, sent }
}

describe('the pacer of media handshakes when the wall clock is set', () => {
  it('forward: every message goes out', async () => {
    expect(await sendsAfterStep(10 * 60_000)).toBe(10)
  })

  // H-L2-05: on the wall clock fewer than 10 went out.
  it('back by ten minutes: every message goes out', async () => {
    expect(await sendsAfterStep(-10 * 60_000)).toBe(10)
  })
})

describe('the pacer of media handshakes across a sleep', () => {
  it('with a monotonic clock that stood still: holds back within the budget, then sends one second later', () => {
    const { mono, sent } = burst()
    expect(sent).toHaveLength(HANDSHAKES_PER_SEC)
    // The clock stands still while timers still fire: nothing more goes out, and nothing is lost.
    vi.advanceTimersByTime(10_000)
    expect(sent).toHaveLength(HANDSHAKES_PER_SEC)
    // Awake again: the rest goes once one second of the clock has passed.
    mono.at = 1001
    vi.advanceTimersByTime(1001)
    expect(sent).toHaveLength(HANDSHAKES_PER_SEC + 4)
    expect(sent.slice(HANDSHAKES_PER_SEC).every(at => at === 1001)).toBe(true)
  })

  it('with a monotonic clock that jumped forward: sends what waits at the next timer, no longer', () => {
    const { mono, sent } = burst()
    expect(sent).toHaveLength(HANDSHAKES_PER_SEC)
    mono.at = 60 * 60_000
    vi.advanceTimersByTime(1001)
    expect(sent).toHaveLength(HANDSHAKES_PER_SEC + 4)
  })
})
