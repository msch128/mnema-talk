// Liveness by recurrence. A deadline cannot tell a room that is slow from one
// that never agrees; a room that comes back to a state it was in, again and
// again, without ever agreeing, is going round in a circle.
//
// The state is what a member can feel, taken from outside: for every member in
// the Talk whether it holds keys, how far its epoch is from the newest epoch
// any member holds, and how many members its group has. Epoch numbers climb
// in a loop, so only the distances count. Time is left out on purpose: a state
// is visited again when the room has left it for another and returned.
import { createHash } from 'node:crypto'
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

/**
 * The same, without who is who: how many members hold nothing, how many hold
 * the newest epoch and how many an older one, and how many members the newest
 * group has. A room that loses a member's keys and gives them back, again and
 * again, is in one such state each time, whichever member it is.
 */
export function counts(sim: GroupSim): string {
  const live = sim.live()
  const keyed = live.filter(member => member.current && member.staleAt === null)
  const newest = keyed.length ? Math.max(...keyed.map(member => (member.current as { epoch: number }).epoch)) : 0
  const atNewest = keyed.filter(member => (member.current as { epoch: number }).epoch === newest)
  const size = atNewest.length ? Math.max(...atNewest.map(member => (member.current as { members: string[] }).members.length)) : 0
  return `${live.length - keyed.length} none, ${atNewest.length} newest in a group of ${size}, ${keyed.length - atNewest.length} behind`
}

export class CycleDetector {
  private previous: string | null = null
  private readonly seen = new Map<string, Visit>()

  constructor(private readonly view: (sim: GroupSim) => string = signature) {}

  /** Call after every event; only a change of state counts as a visit. */
  observe = (sim: GroupSim) => {
    const state = this.view(sim)
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

interface Inside {
  current: { epoch: number, verification: { members: string[] } } | null
  alive: Map<string, number>
  resets: { waitMs(key: string): number }
  restarting: Set<string>
  pending: Map<string, unknown>
  heard: Set<string>
}

/**
 * What the whole room is, as far as it can decide what happens next: for every member the epoch (as a distance to
 * the newest), the size of its group, how long ago it heard of each member of it (in maintenance ticks), how long
 * its next start-over must wait, which members it takes for coming back or having offered; every handshake on
 * its way with how long it still has; and every timer of a tab with how long it still has (without them a tab that
 * waits, with no group yet, looks the same at every instant of its wait). Times are relative to now, so the same state is the same at any hour. What
 * it leaves out: the keys, and the random delays of epochs not yet reached (rotation does not happen in a churn).
 */
export function deep(sim: GroupSim): string {
  const live = sim.live().sort((a, b) => (a.id < b.id ? -1 : 1))
  const keyed = live.map(member => (member.group as unknown as Inside | null)?.current?.epoch).filter((epoch): epoch is number => epoch !== undefined)
  const newest = keyed.length ? Math.max(...keyed) : 0
  const tick = 2000
  const parts = live.map((member) => {
    const group = member.group as unknown as Inside | null
    if (!group) return `${member.label}:-`
    const ages = [...group.alive].sort(([a], [b]) => (a < b ? -1 : 1)).map(([id, at]) => `${id.slice(-2)}=${Math.round((sim.now - at) / tick)}`)
    const held = group.current ? `${group.current.epoch - newest}/${group.current.verification.members.length}` : 'none'
    return `${member.id.slice(-2)}:${held}:${sim.asleep(member) ? 'z' : 'a'}:${ages.join(',')}:w${Math.ceil(group.resets.waitMs('self') / tick)}:r${[...group.restarting].map(id => id.slice(-2)).join('.')}:p${[...group.pending.keys()].map(id => id.slice(-2)).sort().join('.')}:h${group.heard.size}`
  })
  const flight = sim.inFlight().map(item => `${item.to.slice(-2)}${item.kind.replace(/@\d+/, '')}${Math.round(item.due / 100)}`).sort()
  const timers = sim.ownTimers().map(item => `${item.owner}@${Math.round(item.due / 100)}`).sort()
  return createHash('sha1').update(`${parts.join('|')}#${flight.join(',')}#${timers.join(',')}`).digest('hex')
}

export interface Repeat {
  /** Virtual seconds of the first time the room was in a state and of the first time it came back to it. */
  firstAtS: number
  againAtS: number
  /** How many events the room repeated itself over, and how many times it came back to some state in all. */
  repeats: number
}

/** Remembers every whole-room state and says when one comes back before the room agreed. */
export class Recurrence {
  private readonly seen = new Map<string, number>()
  private readonly start = Date.UTC(2026, 0, 1)
  private previous = ''
  first: Repeat | null = null
  repeats = 0

  /** Call after every event; a state the room stays in is one visit. */
  observe = (sim: GroupSim) => {
    const key = deep(sim)
    if (key === this.previous) return
    this.previous = key
    const atS = (sim.now - this.start) / 1000
    const before = this.seen.get(key)
    if (before === undefined) {
      this.seen.set(key, atS)
      return
    }
    this.repeats++
    this.first ??= { firstAtS: before, againAtS: atS, repeats: 0 }
    this.first.repeats = this.repeats
  }

  states(): number {
    return this.seen.size
  }
}
