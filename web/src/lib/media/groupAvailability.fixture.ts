// What a member feels of the group protocol: for how long it is in a Talk
// without keys it can use. Read from the trace of a simulator run (the
// "keys ..." lines the call's own reports produce), so it measures the same
// run the invariants judge.
import { SIM_TIMING } from './groupSim.fixture'
import type { SimResult } from './groupSimRun.fixture'

export interface Availability {
  /** Member-milliseconds in a Talk, from a tab's join until it leaves, is reloaded away or the run ends. */
  memberMs: number
  /** Of those, without keys. */
  noKeysMs: number
  /** Of those, without keys after the member held keys once: the cost of starting over, not of joining. */
  lostMs: number
  /** Times a member that held keys reported none. */
  startOvers: number
  /** The longest stretch one member had no keys after it first held keys. */
  longestLostMs: number
  /** The longest a member went on holding one epoch's keys: what a key's life comes to. */
  longestEpochMs: number
  /** The longest a tab waited for its first keys (a tab that left or ended the run still waiting counts what it waited). */
  longestJoinMs: number
}

interface Tab { since: number, everKeys: boolean, down: number | null, epoch: string | null, epochSince: number }

const LINE = /^\s*(\d+(?:\.\d+)?) (.*)$/

/** The share of member time without keys, 0 when nobody was in a Talk. */
export function share(part: number, whole: number): number {
  return whole > 0 ? part / whole : 0
}

/** Reads `trace` (see GroupSim.note) for a run of `endMs` virtual milliseconds. */
export function availability(trace: readonly string[], endMs: number): Availability {
  const tabs = new Map<string, Tab>()
  const total: Availability = { memberMs: 0, noKeysMs: 0, lostMs: 0, startOvers: 0, longestLostMs: 0, longestEpochMs: 0, longestJoinMs: 0 }
  const close = (tab: Tab, at: number) => {
    total.memberMs += at - tab.since
    if (tab.epoch !== null) total.longestEpochMs = Math.max(total.longestEpochMs, at - tab.epochSince)
    if (tab.down !== null) {
      const span = at - tab.down
      total.noKeysMs += span
      if (tab.everKeys) { total.lostMs += span; total.longestLostMs = Math.max(total.longestLostMs, span) } else total.longestJoinMs = Math.max(total.longestJoinMs, span)
    }
  }
  for (const line of trace) {
    const match = LINE.exec(line)
    if (!match) continue
    const at = Math.round(Number(match[1]) * 1000)
    const text = match[2] as string
    const joined = /^join (\S+)$/.exec(text)
    if (joined) { tabs.set(joined[1] as string, { since: at, everKeys: false, down: at, epoch: null, epochSince: at }); continue }
    const gone = /^(?:leave|reload) (\S+)/.exec(text)
    if (gone) {
      const tab = tabs.get(gone[1] as string)
      if (tab) { close(tab, at); tabs.delete(gone[1] as string) }
      continue
    }
    const keyed = /^(\S+) keys (none|\d+)/.exec(text)
    const tab = keyed ? tabs.get(keyed[1] as string) : undefined
    if (!keyed || !tab) continue
    if (keyed[2] === 'none') {
      if (tab.epoch !== null) total.longestEpochMs = Math.max(total.longestEpochMs, at - tab.epochSince)
      tab.epoch = null
      if (tab.down !== null) continue
      tab.down = at
      total.startOvers++
      continue
    }
    if (tab.down !== null) {
      const span = at - tab.down
      total.noKeysMs += span
      if (tab.everKeys) { total.lostMs += span; total.longestLostMs = Math.max(total.longestLostMs, span) } else total.longestJoinMs = Math.max(total.longestJoinMs, span)
      tab.down = null
    }
    tab.everKeys = true
    if (tab.epoch !== keyed[2]) {
      if (tab.epoch !== null) total.longestEpochMs = Math.max(total.longestEpochMs, at - tab.epochSince)
      tab.epoch = keyed[2] as string
      tab.epochSince = at
    }
  }
  for (const tab of tabs.values()) close(tab, endMs)
  return total
}

export function describeAvailability(a: Availability): string {
  const pct = (part: number) => `${(100 * share(part, a.memberMs)).toFixed(1)}%`
  return `no keys ${pct(a.noKeysMs)} of ${Math.round(a.memberMs / 1000)} member-s, lost after holding ${pct(a.lostMs)}, ${a.startOvers} start-overs, longest ${Math.round(a.longestLostMs / 1000)} s, longest epoch ${Math.round(a.longestEpochMs / 1000)} s`
}

export function availabilityOf(result: SimResult): Availability {
  return availability(result.trace, result.endMs)
}

/**
 * What the nightly sweep (groupSim.sweep.test.ts) accepts of the time members spend without keys, per relay. Set on
 * 2026-11-12 on the tree that ships: origin/main 3.0.9 (f58bc3fc6) with lane 2's harness (80a176920), the fix of
 * H-L2-04 (branch fix-h-l2-04, 95b8e2b6b) and the monotonic pacer and back-offs (f8e6e5667).
 * - seed / knownSeed: the most one seed may lose of its member time after it held keys, outside and inside the
 *   failure budget (worst measured 26 % honest; hostile 38.9, 38.2 and 36.3 % for seeds 187, 352 and 107).
 * - joinMs: the longest a tab may wait for its first keys (worst measured 97 s honest and 135 s hostile; the room
 *   must agree within 240 s).
 * - pooled: the share of member time lost after holding keys over all 400 seeds. This is the hard limit: the job
 *   "availability" of sim-sweep.yml pools the 8 shards and fails outside POOL_WINDOW (hostile 12.74 % before H-L2-04
 *   and 12.82 % after it).
 * - shardLost: the same share per shard of 50 seeds. Any change of the group's timing reshuffles the seeds'
 *   trajectories (H-L2-04 moved 230 of 400 hostile seeds, up and down alike) and a shard swings by about 1.3 points from
 *   that alone, so SHARD_WINDOW (2 points either way) is about 1.5 standard deviations of a timing change: a shard
 *   outside it means "measure again", not "regression" by itself. It is a warning in the job summary, not a failure.
 */
export const LOST_BUDGET = {
  honest: { seed: 0.3, knownSeed: 0.5, joinMs: 110_000, pooled: 0.061, shardLost: [0.0643, 0.046, 0.0581, 0.0517, 0.0593, 0.0669, 0.0738, 0.0646] },
  hostile: { seed: 0.4, knownSeed: 0.5, joinMs: 140_000, pooled: 0.1282, shardLost: [0.1505, 0.1067, 0.1295, 0.1373, 0.1114, 0.1179, 0.148, 0.1108] },
} as const
export const SHARD_WINDOW = 0.02
/** About two standard deviations of the pooled share under a change of timing (1.3 points per shard, 8 shards). */
export const POOL_WINDOW = 0.01
export const SHARDS = 8

/**
 * The longest one epoch's keys may be held in a sweep run: rotated late by the jitter, with a committer that
 * is silent until it is taken for absent and its successor commits (the age bound of SECURITY.md, "Key
 * lifetime"), and 20 s for a slow link. The worst measured over 800 runs at v3.0.8: 75 s.
 */
export const EPOCH_LIFE_MS = SIM_TIMING.rotateMs + SIM_TIMING.rotateJitterMs + SIM_TIMING.absentMs + 3 * SIM_TIMING.maintenanceMs + 20_000

/** What a run of seeds spent: the sums to pool and the seeds that broke a per-seed limit. */
export class AvailabilityBudget {
  private memberMs = 0
  private lostMs = 0
  private readonly over: string[] = []

  constructor(private readonly relay: 'honest' | 'hostile', private readonly shard: readonly [index: number, count: number] = [0, 1]) {}

  add(seed: number, known: boolean, a: Availability) {
    this.memberMs += a.memberMs
    this.lostMs += a.lostMs
    const limit = LOST_BUDGET[this.relay][known ? 'knownSeed' : 'seed']
    const lost = share(a.lostMs, a.memberMs)
    if (a.longestEpochMs > EPOCH_LIFE_MS) this.over.push(`seed ${seed}: one epoch's keys were held for ${Math.round(a.longestEpochMs / 1000)} s (limit ${EPOCH_LIFE_MS / 1000} s)`)
    if (a.longestJoinMs > LOST_BUDGET[this.relay].joinMs) this.over.push(`seed ${seed}: a tab waited ${Math.round(a.longestJoinMs / 1000)} s for its first keys (limit ${LOST_BUDGET[this.relay].joinMs / 1000} s)`)
    if (lost > limit) this.over.push(`seed ${seed}: ${(100 * lost).toFixed(1)}% of member time lost its keys (limit ${100 * limit}% for a seed ${known ? 'inside' : 'outside'} the failure budget)`)
  }

  /** What is over a per-seed limit, one line each; empty when every seed is within them. */
  breaches(): string[] {
    return this.over
  }

  /** The shard's pooled share against its row of the table, when the run is one of the 8 shards: a warning (see LOST_BUDGET). */
  shardNote(): string | null {
    const [index, count] = this.shard
    if (count !== SHARDS) return null
    const pooled = share(this.lostMs, this.memberMs)
    const expected = LOST_BUDGET[this.relay].shardLost[index] as number
    if (Math.abs(pooled - expected) <= SHARD_WINDOW) return null
    return `${this.relay} shard ${index}: pooled ${(100 * pooled).toFixed(1)}% of member time lost its keys, the table says ${(100 * expected).toFixed(1)}% (window ${100 * SHARD_WINDOW} points): measure again`
  }

  /** The sums of this shard, for the job that pools all shards. */
  sums(): ShardSums {
    return { relay: this.relay, shard: this.shard[0], memberMs: this.memberMs, lostMs: this.lostMs }
  }
}

/** What one shard of one relay spent, as the sweep writes it for the job that pools the shards. */
export interface ShardSums { relay: 'honest' | 'hostile', shard: number, memberMs: number, lostMs: number }

/** The hard limit: each shard of both relays present once, and each relay's pooled share within POOL_WINDOW of its budget. */
export function poolBreaches(rows: readonly ShardSums[]): string[] {
  const out: string[] = []
  for (const relay of ['honest', 'hostile'] as const) {
    const mine = rows.filter(row => row.relay === relay)
    const shards = mine.map(row => row.shard).sort((a, b) => a - b).join(',')
    if (shards !== [...Array(SHARDS).keys()].join(',')) {
      out.push(`${relay}: the shards present are [${shards}], not each of 0..${SHARDS - 1} once`)
      continue
    }
    const pooled = share(mine.reduce((sum, row) => sum + row.lostMs, 0), mine.reduce((sum, row) => sum + row.memberMs, 0))
    const expected = LOST_BUDGET[relay].pooled
    if (pooled > expected + POOL_WINDOW) out.push(`${relay}: pooled ${(100 * pooled).toFixed(2)}% of member time lost its keys (budget ${(100 * expected).toFixed(2)}% plus ${100 * POOL_WINDOW} point)`)
    if (pooled < expected - POOL_WINDOW) out.push(`${relay}: pooled ${(100 * pooled).toFixed(2)}% is below the budget of ${(100 * expected).toFixed(2)}%: the protocol got better, so set LOST_BUDGET.${relay}.pooled to ${pooled.toFixed(4)}`)
  }
  return out
}
