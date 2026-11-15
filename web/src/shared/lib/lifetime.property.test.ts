import { describe, expect, it } from 'vitest'
import { EndSchedule, remainingStep, validLifetime } from './lifetime'

// A small seeded generator, so a failing case can be replayed.
function rng(seed: number) {
  let s = seed >>> 0
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0
    return s / 2 ** 32
  }
}

describe('the end schedule of messages with a lifetime', () => {
  it('hands out every end exactly once, never before it, and always knows the next one', () => {
    for (let seed = 1; seed <= 200; seed++) {
      const r = rng(seed)
      const schedule = new EndSchedule<string>()
      const ends = new Map<string, number>()
      let now = 0
      const handed = new Set<string>()
      for (let step = 0; step < 60; step++) {
        const roll = r()
        if (roll < 0.45) {
          const id = `m${Math.floor(r() * 30)}`
          const at = now + Math.floor(r() * 5000) - 500
          schedule.set(id, at, id)
          ends.set(id, at)
          handed.delete(id)
        } else if (roll < 0.55) {
          const id = `m${Math.floor(r() * 30)}`
          schedule.delete(id)
          ends.delete(id)
        } else {
          now += Math.floor(r() * 1500)
          for (const id of schedule.due(now)) {
            expect(ends.get(id)! <= now, `seed ${seed}: ${id} handed out before its end`).toBe(true)
            expect(handed.has(id), `seed ${seed}: ${id} handed out twice`).toBe(false)
            handed.add(id)
            ends.delete(id)
          }
          for (const at of ends.values()) expect(at > now, `seed ${seed}: an ended message stayed`).toBe(true)
        }
        const expected = ends.size ? Math.min(...ends.values()) : null
        expect(schedule.next()).toBe(expected)
        expect(schedule.size).toBe(ends.size)
      }
    }
  })

  it('updates a mark at least as often as its text changes', () => {
    for (const ms of [0, 999, 59_999, 60_000, 3_599_999, 3_600_000, 86_400_000 * 3]) {
      const step = remainingStep(ms)
      expect(step).toBeGreaterThan(0)
      expect(step <= (ms < 60_000 ? 1000 : ms < 3_600_000 ? 60_000 : 3_600_000)).toBe(true)
    }
  })

  it('takes only lifetimes the server takes', () => {
    expect(validLifetime(59)).toBe(false)
    expect(validLifetime(60)).toBe(true)
    expect(validLifetime(30 * 24 * 3600)).toBe(true)
    expect(validLifetime(30 * 24 * 3600 + 1)).toBe(false)
    expect(validLifetime(90.5)).toBe(false)
    expect(validLifetime(600, 300)).toBe(false)
  })
})
