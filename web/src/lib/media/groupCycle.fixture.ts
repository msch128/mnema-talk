// Liveness by recurrence. A deadline cannot tell a room that is slow from one
// that never agrees; a room that comes back to a state it was in, again and
// again, without ever agreeing, is going round in a circle.
//
// The state is what a member can feel, taken from outside: for every member in
// the Talk whether it holds keys, how far its epoch is from the newest epoch
// any member holds, and how many members its group has. Epoch numbers climb
// in a loop, so only the distances count. Time is left out on purpose: a state
// is visited again when the room has left it for another and returned.
import type { GroupSim } from './groupSim.fixture'

export interface Visit {
  /** The state, readable: `m3:-1/5` is member 3, one epoch behind, in a group of 5; `m3:none` holds no keys. */
  state: string
  visits: number
  /** Virtual seconds of the first and the last visit, and the trace line the first one is at. */
  firstAtS: number
  lastAtS: number
  firstTraceLine: number
}

export function signature(sim: GroupSim): string {
  const live = sim.live().sort((a, b) => (a.id < b.id ? -1 : 1))
  const held = live.filter(member => member.current && member.staleAt === null).map(member => (member.current as { epoch: number }).epoch)
  const newest = held.length ? Math.max(...held) : 0
  return live.map((member) => {
    const verification = member.current
    const name = `m${Number.parseInt(member.id.slice(-12), 16)}`
    return verification && member.staleAt === null ? `${name}:${verification.epoch - newest}/${verification.members.length}` : `${name}:none`
  }).join(' ')
}

export class CycleDetector {
  private previous: string | null = null
  private readonly seen = new Map<string, Visit>()

  /** Call after every event; only a change of state counts as a visit. */
  observe = (sim: GroupSim) => {
    const state = signature(sim)
    if (state === this.previous) return
    this.previous = state
    const atS = (sim.now - Date.UTC(2026, 0, 1)) / 1000
    const known = this.seen.get(state)
    if (known) {
      known.visits++
      known.lastAtS = atS
    } else {
      this.seen.set(state, { state, visits: 1, firstAtS: atS, lastAtS: atS, firstTraceLine: sim.trace.length })
    }
  }

  /** The most visited state. */
  top(): Visit | null {
    let best: Visit | null = null
    for (const visit of this.seen.values()) if (!best || visit.visits > best.visits) best = visit
    return best
  }

  /** States visited at least `times` times before the room agreed. */
  recurring(times: number): Visit[] {
    return [...this.seen.values()].filter(visit => visit.visits >= times).sort((a, b) => b.visits - a.visits)
  }

  states(): number {
    return this.seen.size
  }
}
