import { describe, expect, it } from 'vitest'
import { Backoff } from './backoff'

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
})
