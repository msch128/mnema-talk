// What a member feels of the group protocol: for how long it is in a Talk
// without keys it can use. Read from the trace of a simulator run (the
// "keys ..." lines the call's own reports produce), so it measures the same
// run the invariants judge.
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
  const total: Availability = { memberMs: 0, noKeysMs: 0, lostMs: 0, startOvers: 0, longestLostMs: 0, longestEpochMs: 0 }
  const close = (tab: Tab, at: number) => {
    total.memberMs += at - tab.since
    if (tab.epoch !== null) total.longestEpochMs = Math.max(total.longestEpochMs, at - tab.epochSince)
    if (tab.down !== null) {
      const span = at - tab.down
      total.noKeysMs += span
      if (tab.everKeys) { total.lostMs += span; total.longestLostMs = Math.max(total.longestLostMs, span) }
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
      if (tab.everKeys) { total.lostMs += span; total.longestLostMs = Math.max(total.longestLostMs, span) }
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
