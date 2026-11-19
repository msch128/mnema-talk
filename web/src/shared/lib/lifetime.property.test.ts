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

type ScheduleModel = { schedule: EndSchedule<string>; ends: Map<string, number>; handed: Set<string>; now: number }

// One random step: set an end, delete one, or let time pass and collect what is due.
function applyRandomStep(m: ScheduleModel, r: () => number, seed: number) {
  const roll = r()
  if (roll < 0.45) {
    const id = `m${Math.floor(r() * 30)}`
    const at = m.now + Math.floor(r() * 5000) - 500
    m.schedule.set(id, at, id)
    m.ends.set(id, at)
    m.handed.delete(id)
  } else if (roll < 0.55) {
    const id = `m${Math.floor(r() * 30)}`
    m.schedule.delete(id)
    m.ends.delete(id)
  } else {
    m.now += Math.floor(r() * 1500)
    collectDue(m, seed)
  }
}

function collectDue(m: ScheduleModel, seed: number) {
  for (const id of m.schedule.due(m.now)) {
    expect(m.ends.get(id)! <= m.now, `seed ${seed}: ${id} handed out before its end`).toBe(true)
    expect(m.handed.has(id), `seed ${seed}: ${id} handed out twice`).toBe(false)
    m.handed.add(id)
    m.ends.delete(id)
  }
  for (const at of m.ends.values()) expect(at > m.now, `seed ${seed}: an ended message stayed`).toBe(true)
}

describe('the end schedule of messages with a lifetime', () => {
  it('hands out every end exactly once, never before it, and always knows the next one', () => {
    for (let seed = 1; seed <= 200; seed++) {
      const r = rng(seed)
      const model: ScheduleModel = { schedule: new EndSchedule<string>(), ends: new Map(), handed: new Set(), now: 0 }
      for (let step = 0; step < 60; step++) {
        applyRandomStep(model, r, seed)
        const expected = model.ends.size ? Math.min(...model.ends.values()) : null
        expect(model.schedule.next()).toBe(expected)
        expect(model.schedule.size).toBe(model.ends.size)
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
