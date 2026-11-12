// Small rooms, all schedules up to a bound. A room of 2 to 4 members is driven through a script of joins, one
// leave, reload or sleep, on a relay that is in order and on time. Then every way the relay can deviate from that
// within a budget is played, one run each: at the first `horizon` handshakes it may lose one, or let the next one
// to the same member overtake it, at most `depth` times per run. After the script the relay behaves and the four
// invariants of groupSimChecks.fixture.ts must hold (the room agrees and keeps the rules).
//
// What this is not: the simulator's random seeds reach many more situations, but never "every" one. Here the
// same situations (a script, a deviation at a point) are enumerated, so a failure comes with the shortest
// schedule that shows it: fewest deviations first, then the earliest.
import type { WireControl } from './groupSim.fixture'
import { runScript, type Script, type SimResult } from './groupSimRun.fixture'

export type Deviation = 'drop' | 'swap'
export type Schedule = ReadonlyMap<number, Deviation>

export interface NamedScript { name: string, script: Script }

const GAPS = [50, 3000] as const
const ACTIONS = ['leave', 'reload', 'sleep'] as const
const ACT_AT = [8000, 15_000] as const

/** The scripts of a room of `members` (2 to 4): joins one after another, then one of `ACTIONS` by one member. */
export function scripts(members: number): NamedScript[] {
  const out: NamedScript[] = []
  const gapSets = members === 2 ? GAPS.map(gap => [gap]) : GAPS.flatMap(first => (members === 3 ? [[first, 50], [first, 3000]] : GAPS.flatMap(second => GAPS.map(third => [first, second, third]))))
  for (const gaps of gapSets) {
    const joins = gaps.reduce<number[]>((times, gap) => [...times, (times.at(-1) ?? 0) + gap], [0])
    for (const action of ACTIONS) {
      for (let member = 1; member <= members; member++) {
        for (const at of ACT_AT) {
          out.push({
            name: `${members} members, joins at ${joins.join('/')} ms, ${action} of m${member} at ${at} ms`,
            script: { latency: {}, steps: [...joins.map((time, index) => [time, 'join', index + 1] as const), [at, action, member] as const], quietAt: 75_000 },
          })
        }
      }
    }
  }
  return out
}

/** A control that carries out `schedule` and counts the handshakes it was asked about. */
export function controlFor(schedule: Schedule): { control: WireControl, asked: () => number } {
  let asked = 0
  return {
    control: () => schedule.get(asked++) ?? 'run',
    asked: () => asked,
  }
}

export interface Run { result: SimResult, asked: number }

export async function runSchedule(named: NamedScript, schedule: Schedule): Promise<Run> {
  const { control, asked } = controlFor(schedule)
  const result = await runScript({ ...named.script, control })
  return { result, asked: asked() }
}

/** Every schedule of at most `depth` deviations among the first `points` handshakes: the fewest deviations first, the earliest first. */
export function* schedules(points: number, depth: number): Generator<Schedule> {
  yield new Map()
  const choices: Deviation[] = ['drop', 'swap']
  const extend = function* (from: number, left: number, chosen: [number, Deviation][]): Generator<Schedule> {
    for (let at = from; at < points; at++) {
      for (const choice of choices) {
        const next: [number, Deviation][] = [...chosen, [at, choice]]
        if (left === 1) yield new Map(next)
        else yield* extend(at + 1, left - 1, next)
      }
    }
  }
  for (let size = 1; size <= depth; size++) yield* extend(0, size, [])
}

export function describeSchedule(schedule: Schedule): string {
  return [...schedule].map(([at, choice]) => `${choice}@${at}`).join(' ') || 'no deviation'
}
