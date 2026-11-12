// Crash points. A tab of the call can die after any `await` of a protocol step: after the key package was made
// and before it was sent, after a commit was created and before it was adopted, between two signatures. This
// finds every such point in a scripted run and replays the run once per point with the tab reloaded there.
//
// The await boundaries are the ones the group's own code waits on: every call into ts-mls (key packages, groups,
// commits, welcomes) and every call into WebCrypto (heartbeat and capability signatures, key import). A test file
// wraps both with `wrapModule` and `crashingCrypto`; the hook below counts the boundaries a tab passes and, at the
// chosen one, has the simulator reload that tab at the next instant. What the tab's last step does after its
// boundary is lost, as it is when the process dies: nothing it sends reaches the relay (GroupSim.dropWiresOfGone).
import type { GroupSim } from './groupSim.fixture'

export interface CrashPoint {
  /** The user whose tab dies, and which of that user's boundaries (from 1) it dies at. */
  user: string
  ordinal: number
  /** Whether the server notices the gap (the roster changes) or the tab is back before it does. */
  seen: boolean
}

export interface Boundaries {
  /** How many boundaries each user passed. */
  readonly counts: Map<string, number>
  crashed: boolean
}

let sim: GroupSim | null = null
let boundaries: Boundaries = { counts: new Map(), crashed: false }
let target: CrashPoint | null = null

/** Starts counting for `world`; with `point` the tab dies there. */
export function arm(world: GroupSim, point: CrashPoint | null): Boundaries {
  sim = world
  target = point
  boundaries = { counts: new Map(), crashed: false }
  world.dropWiresOfGone = true
  return boundaries
}

export function disarm() {
  sim = null
  target = null
}

/** One await boundary passed by the code of the tab that is running. */
export function boundary() {
  const member = sim?.running()
  if (!sim || !member || member.gone) return
  const ordinal = (boundaries.counts.get(member.id) ?? 0) + 1
  boundaries.counts.set(member.id, ordinal)
  if (!boundaries.crashed && target && target.user === member.id && target.ordinal === ordinal) {
    boundaries.crashed = true
    sim.crashSoon(member, target.seen)
  }
}

/** A function whose promise counts a boundary when it settles. Anything else goes through untouched. */
export function counting<T extends (...args: never[]) => unknown>(fn: T): T {
  return new Proxy(fn, {
    apply(original, self, args) {
      const out = Reflect.apply(original, self, args) as unknown
      if (!(out instanceof Promise)) return out
      return out.then((value: unknown) => {
        boundary()
        return value
      })
    },
  })
}

/** The module `ts-mls` with every function counted: for `vi.mock('ts-mls', ...)`. */
export function wrapModule<M extends object>(actual: M): M {
  return Object.fromEntries(Object.entries(actual).map(([name, value]) => [name, typeof value === 'function' ? counting(value as (...args: never[]) => unknown) : value])) as M
}

/** WebCrypto whose `subtle` counts a boundary after every call: for `vi.stubGlobal('crypto', ...)`. */
export function crashingCrypto(real: Crypto): Crypto {
  const subtle = new Proxy(real.subtle, {
    get(target, name) {
      const value = Reflect.get(target, name, target) as unknown
      return typeof value === 'function' ? counting((value as (...args: never[]) => unknown).bind(target)) : value
    },
  })
  return new Proxy(real, {
    get(target, name) {
      if (name === 'subtle') return subtle
      const value = Reflect.get(target, name, target) as unknown
      return typeof value === 'function' ? (value as (...args: never[]) => unknown).bind(target) : value
    },
  })
}
