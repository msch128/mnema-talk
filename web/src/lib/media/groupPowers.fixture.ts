// What the server can do beyond delaying, reordering, repeating and dropping (SECURITY.md, "Voice and screenshare": it can replay what it relayed, and the roster is its word):
// - say again what it relayed, to every member, long after (replay);
// - tell a member a roster that is not the room (hide a member, list a ghost, list a lower id than all).
//
// The properties, each judged on the same seeds as the invariants of groupSimChecks.fixture.ts:
// - a replay of heartbeats, capability messages or welcomes costs nobody their keys;
// - a replay of key packages or commits may: a package of a present member under another signature key is a
//   reload (SECURITY.md, "reload"), and a commit that names an epoch nobody reached makes the members that
//   take it for the committer's start over (SECURITY.md, C8). That is the denial of service the server
//   already has, and it is limited: no member starts over more than START_OVER_LIMIT times in the window;
// - when the server stops, the room agrees again (the first invariant, after the replay);
// - when the lies about the roster stop and one true roster follows, the room agrees (liarPlan).
import { epochKey, type GroupSim } from './groupSim.fixture'
import type { GroupChecks } from './groupSimChecks.fixture'
import { AGREE_WITHIN_MS, type SimPlan } from './groupSimRun.fixture'

/** How many of the recorded handshakes the server plays again, and over how long. */
export const REPLAY_MOST = 300
export const REPLAY_SPREAD_MS = 20_000
/** How long the replay's window lasts after it began, in which the start-overs are counted. */
export const REPLAY_WINDOW_MS = 50_000
/**
 * The most a member may start over within the window when the replay may cost keys. The limits the code states:
 * a leaf swap per member 3 s, doubling (3, 6, 12, 24, 48: five in 50 s), a reset 10 s doubling (three); a
 * rejoin in between starts both waits over, so the count can reach the number of rejoins that fit the window.
 */
export const START_OVER_LIMIT = 8

const keysLine = /(\S+) keys (none)?/

/** Kinds of handshake whose replay must cost nothing. */
export const HARMLESS = ['heartbeat', 'capability', 'welcome'] as const

/** The start-overs per member in the trace from line `from` on: a member that held keys and reports none (twice in a row is once). */
export function startOvers(trace: readonly string[], from: number): Map<string, number> {
  const counts = new Map<string, number>()
  const holding = new Set<string>()
  for (const [number, line] of trace.entries()) {
    const match = keysLine.exec(line)
    if (!match) continue
    const label = match[1] as string
    if (!match[2]) {
      holding.add(label)
    } else if (holding.delete(label) && number >= from) {
      counts.set(label, (counts.get(label) ?? 0) + 1)
    }
  }
  return counts
}

/** Runs once the room agreed and held it: the server plays old handshakes again, then stops. */
export function replayThenStop(kinds: readonly string[] | undefined, harmless: boolean) {
  return async (sim: GroupSim, checks: GroupChecks): Promise<void> => {
    const held = sim.live().map(member => `${member.label}=${epochKey(member.current)}`).join(' ')
    const from = sim.trace.length
    const name = kinds?.join(',') ?? 'all kinds'
    sim.replayAll(REPLAY_MOST, REPLAY_SPREAD_MS, kinds)
    await sim.runUntil(sim.now + REPLAY_WINDOW_MS)
    const counts = startOvers(sim.trace, from)
    const worst = Math.max(0, ...counts.values())
    if (harmless && counts.size) sim.violation(`[6] a replay of old handshakes (${name}) made ${counts.size} member(s) lose their keys (they held ${held})`)
    else if (worst > START_OVER_LIMIT) sim.violation(`[6] a replay of old handshakes (${name}) made one member start over ${worst} times within ${REPLAY_WINDOW_MS} ms (limit ${START_OVER_LIMIT})`)
    await sim.runUntil(sim.now + AGREE_WITHIN_MS, () => checks.agreed())
    if (!checks.agreed()) sim.violation(`[6] the server replayed old handshakes (${name}) and stopped, and the room did not agree within ${AGREE_WITHIN_MS} ms: ${checks.describe()}`)
  }
}

/** The plan for a run in which the server replays: its handshakes are kept, and the replay follows the agreement. */
export function replayPlan(plan: SimPlan = {}, kinds?: readonly string[]): SimPlan {
  return { ...plan, record: true, then: replayThenStop(kinds, kinds !== undefined && kinds.every(kind => (HARMLESS as readonly string[]).includes(kind))) }
}

/** The plan for a run in which the server also lies about the roster while it interferes. */
export function liarPlan(plan: SimPlan = {}): SimPlan {
  return { ...plan, lies: true }
}
