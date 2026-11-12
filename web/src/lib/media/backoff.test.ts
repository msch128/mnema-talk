import { afterEach, describe, expect, it, vi } from 'vitest'
import { Backoff } from './backoff'
import { clock } from './clock'

const testNow = clock.now
afterEach(() => {
  clock.now = testNow
  vi.useRealTimers()
})

describe('backoff', () => {
  it('allows once, then waits longer each time, per key', () => {
    let now = 0
    const gate = new Backoff(10, 60, () => now)
    expect(gate.allow('a')).toBe(true)
    expect(gate.allow('a')).toBe(false)
    expect(gate.allow('b')).toBe(true)
    now = 10
    expect(gate.allow('a')).toBe(true)
    now = 29
    expect(gate.allow('a')).toBe(false)
    now = 30
    expect(gate.allow('a')).toBe(true)
    now = 70
    expect(gate.allow('a')).toBe(true)
    now = 130
    expect(gate.allow('a')).toBe(true)
    now = 189
    expect(gate.allow('a')).toBe(false)
    now = 190
    expect(gate.allow('a')).toBe(true)
  })

  it('starts over after a quiet spell or a reset', () => {
    let now = 0
    const gate = new Backoff(10, 60, () => now)
    expect(gate.allow('a')).toBe(true)
    now = 10
    expect(gate.allow('a')).toBe(true)
    now = 500
    expect(gate.allow('a')).toBe(true)
    now = 509
    expect(gate.allow('a')).toBe(false)
    now = 510
    expect(gate.allow('a')).toBe(true)
    gate.reset('a')
    expect(gate.allow('a')).toBe(true)
    expect(gate.allow('b')).toBe(true)
    gate.reset()
    expect(gate.allow('a')).toBe(true)
    expect(gate.allow('b')).toBe(true)
  })

  it('says how long is left', () => {
    let now = 0
    const gate = new Backoff(10, 60, () => now)
    expect(gate.waitMs('a')).toBe(0)
    gate.allow('a')
    now = 4
    expect(gate.waitMs('a')).toBe(6)
    now = 12
    expect(gate.waitMs('a')).toBe(0)
  })

  it('reads the clock when asked, so a test can move it', () => {
    const gate = new Backoff(1000, 2000)
    expect(gate.allow('a')).toBe(true)
    expect(gate.allow('a')).toBe(false)
  })

  // H-L9-02: on the wall clock a step back kept an open wait shut for the size of the step.
  it('waits on the monotonic clock of the key lifecycle, which a wall clock step does not move', () => {
    vi.useFakeTimers({ toFake: ['Date'], now: Date.UTC(2026, 0, 1) })
    let mono = 0
    clock.now = () => mono
    const gate = new Backoff(3000, 60_000)
    expect(gate.allow('a')).toBe(true)
    vi.setSystemTime(Date.now() - 60 * 60_000)
    mono = 3000
    expect(gate.waitMs('a')).toBe(0)
    expect(gate.allow('a')).toBe(true)
  })

  // Whether the monotonic clock advances in sleep depends on the browser; both are safe.
  it('after a sleep the monotonic clock stood still through: the wait ends its own length after waking, not later', () => {
    let mono = 0
    const gate = new Backoff(3000, 60_000, () => mono)
    expect(gate.allow('a')).toBe(true)
    mono = 1000
    expect(gate.allow('a')).toBe(false)
    // Asleep for an hour: the clock did not move, so 2000 ms of awake time remain, as before the sleep.
    expect(gate.waitMs('a')).toBe(2000)
    mono = 3000
    expect(gate.allow('a')).toBe(true)
    expect(gate.waitMs('a')).toBe(6000)
  })

  it('after a sleep the monotonic clock jumped through: the wait is over and starts again from the base', () => {
    let mono = 0
    const gate = new Backoff(3000, 60_000, () => mono)
    expect(gate.allow('a')).toBe(true)
    mono = 3000
    expect(gate.allow('a')).toBe(true)
    mono += 60 * 60_000
    expect(gate.allow('a')).toBe(true)
    expect(gate.waitMs('a')).toBe(3000)
  })
})
