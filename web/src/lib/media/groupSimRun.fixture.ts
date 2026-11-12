// One seeded run of the simulator: a chaotic phase in which the relay
// interferes and the room changes, then a quiet phase in which the invariants
// must come to hold.
import { GroupChecks } from './groupSimChecks.fixture'
import { GroupSim, SIM_START, type ClockSkew, type Fault, type WireControl } from './groupSim.fixture'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaTiming } from './bounds'

/** How long the relay interferes. */
export const CHAOS_MS = 75_000
/**
 * How long the room may take to agree once the relay behaves. The slowest
 * repair is a member that has to start over twice: the wait between two resets
 * doubles from 10 s up to 60 s, and a member written off as silent comes back
 * only after the absence limit.
 */
export const AGREE_WITHIN_MS = 240_000
/** How long the agreement must then last, over a rotation and every member's renewal. */
export const HOLD_MS = 60_000

const HONEST: Fault = { drop: 0, duplicate: 0, slow: 0.2, stall: 0.03 }

const FAULTS: readonly Fault[] = [
  { drop: 0, duplicate: 0.05, slow: 0.2, stall: 0.02 },
  { drop: 0.04, duplicate: 0.05, slow: 0.25, stall: 0.05 },
  { drop: 0.12, duplicate: 0.1, slow: 0.3, stall: 0.08 },
]

/** How far the machines' clocks may part (see ClockSkew); each member's are drawn from the seed. */
export interface ClockPlan {
  /** Offsets are drawn in +-offsetMs. */
  offsetMs: number
  /** Rate errors are drawn in +-driftPpm. */
  driftPpm: number
  /** Wall clock steps while the relay interferes are drawn in +-stepMs. */
  stepMs: number
  /** The share of machines whose monotonic clock stops in sleep. */
  monoStops: number
  /** Which way the wall clock may be stepped; both when left out. */
  direction?: 'forward' | 'back'
}

export interface SimPlan {
  /** The server also lies about the roster while it interferes (SimPlan.actions 'lie'): each lie goes to one member; one true roster follows when it stops. */
  lies?: boolean
  /** Keeps every handshake the relay carries (GroupSim.log), for a `then` that replays them. */
  record?: boolean
  /** Runs when the room has agreed and held it, with the checks to judge by: more of the server's powers, after the fact. */
  then?: (sim: GroupSim, checks: GroupChecks) => Promise<void>
  /** Looks at the room after every event of the quiet phase, until it agrees (see groupCycle.fixture.ts). */
  watch?: (sim: GroupSim) => void
  /** Per-member clocks; every member on the one virtual clock when left out. */
  clocks?: ClockPlan
  /** The most members the room holds, 2 to 12; drawn from the seed when left out. */
  members?: number
  chaosMs?: number
  /** How long the room may take to agree; AGREE_WITHIN_MS when left out. */
  agreeWithinMs?: number
  fault?: Fault
  /** The lifetimes of the group (rotation, absence, heartbeat, maintenance); SIM_TIMING when left out. */
  timing?: MediaTiming
  /**
   * 'honest' is what the server does without an attacker: one ordered socket
   * per member, nothing dropped or duplicated, only delay (and a socket that
   * reconnects loses what was relayed meanwhile). 'hostile', the default, also
   * reorders, duplicates and drops per recipient.
   */
  relay?: 'hostile' | 'honest'
  /** What members may do while the relay interferes; everything when left out. */
  actions?: readonly ActionKind[]
}

export interface SimResult {
  seed: number
  members: number
  events: number
  /** Virtual ms the run lasted, from the first instant. */
  endMs: number
  /** Virtual ms from the end of the interference until the room agreed; null if it never did. */
  agreedAfterMs: number | null
  violations: string[]
  trace: string[]
}

export type ActionKind = 'join' | 'leave' | 'reload' | 'sleep' | 'reconnect' | 'step' | 'lie'
type Action = [kind: ActionKind, weight: number, run: () => void]

function actions(sim: GroupSim, ids: readonly string[], plan: SimPlan): Action[] {
  const clocks = plan.clocks
  const absent = ids.filter(id => !sim.room.has(id))
  const awake = sim.live().filter(member => !sim.asleep(member)).map(member => member.id)
  const list: Action[] = []
  if (absent.length) list.push(['join', 3, () => sim.join(sim.pick(absent))])
  if (sim.room.size > 1) list.push(['leave', 2, () => sim.leave(sim.pick(sim.live()).id)])
  if (!awake.length) return list
  list.push(['reload', 2, () => sim.reload(sim.pick(awake), sim.draw() < 0.5)])
  list.push(['sleep', 2, () => sim.sleep(sim.pick(awake), sim.between(2000, 20_000))])
  // A long sleep is one the group notices: past its sleep limit (64 s with the default lifetimes).
  const noticed = 2 * Math.max(10, sim.timing.maintenanceMs) + 60_000
  list.push(['sleep', 1, () => sim.sleep(sim.pick(awake), sim.between(noticed + 6000, noticed + 26_000))])
  list.push(['reconnect', 1, () => sim.reconnect(sim.pick(awake), sim.between(200, 6000))])
  if (plan.lies) list.push(['lie', 2, () => sim.lie(sim.pick(awake), sim.pick(['hide', 'ghost', 'lead'] as const))])
  if (clocks?.stepMs) {
    const step = () => {
      const size = Math.round(sim.clockDraw() * (clocks as ClockPlan).stepMs)
      const sign = clocks.direction === 'forward' ? 1 : clocks.direction === 'back' ? -1 : (sim.clockDraw() < 0.5 ? -1 : 1)
      return sign * size
    }
    list.push(['step', 1, () => sim.stepWall(sim.pick(awake), step())])
  }
  return list
}

function act(sim: GroupSim, ids: readonly string[], plan: SimPlan) {
  const allowed = plan.actions
  const list = actions(sim, ids, plan).filter(([kind]) => !allowed || allowed.includes(kind)).map(([, weight, run]) => [weight, run] as const)
  if (!list.length) return
  let roll = sim.draw() * list.reduce((sum, [weight]) => sum + weight, 0)
  for (const [weight, run] of list) {
    roll -= weight
    if (roll < 0) return run()
  }
}

function schedule(sim: GroupSim, ids: readonly string[], plan: SimPlan, chaosMs: number) {
  const first = sim.between(1, ids.length)
  for (const id of ids.slice(0, first)) sim.after(null, sim.between(0, 3000), () => sim.join(id))
  const next = () => {
    if (sim.now - SIM_START >= chaosMs) return
    act(sim, ids, plan)
    sim.after(null, sim.between(500, 8000), next)
  }
  sim.after(null, sim.between(3000, 8000), next)
}

/** The relay behaves from now on, everyone is awake, and at least two members are here. */
function calm(sim: GroupSim, ids: readonly string[], plan: SimPlan) {
  sim.note('the relay stops interfering')
  sim.fault = null
  sim.wakeAll()
  if (plan.lies) sim.correctRosters()
  for (const id of ids) if (sim.room.size < 2 && !sim.room.has(id)) sim.join(id)
}

async function quietPhase(sim: GroupSim, checks: GroupChecks, within: number, watch?: (sim: GroupSim) => void): Promise<number | null> {
  const quietAt = sim.now
  await sim.runUntil(quietAt + within, () => {
    watch?.(sim)
    return checks.agreed()
  })
  if (!checks.agreed()) {
    sim.violation(`[1] the room did not agree within ${within} ms: ${checks.describe()}`)
    return null
  }
  const agreedAfterMs = sim.now - quietAt
  sim.note(`agreed after ${agreedAfterMs} ms`)
  await sim.runUntil(sim.now + HOLD_MS)
  // A commit may be on its way at this very moment: it lands within a few seconds.
  await sim.runUntil(sim.now + 5000, () => checks.agreed())
  if (!checks.agreed()) sim.violation(`[1] the room agreed and then split again: ${checks.describe()}`)
  else await checks.sameKeys()
  await checks.departedHoldNothing()
  return agreedAfterMs
}

function drawSkew(sim: GroupSim, plan: ClockPlan): ClockSkew {
  const spread = (limit: number) => (sim.clockDraw() * 2 - 1) * limit
  return { offset: Math.round(spread(plan.offsetMs)), drift: spread(plan.driftPpm) * 1e-6, wallStep: 0, monoLost: 0, monoStops: sim.clockDraw() < plan.monoStops }
}

/** How many lines of the trace match `pattern`: what a sweep checks to know that the power or perturbation it claims really happened. */
export function occurrences(result: SimResult, pattern: RegExp): number {
  return result.trace.filter(line => pattern.test(line)).length
}

export async function runSeed(seed: number, plan: SimPlan = {}): Promise<SimResult> {
  const checks = new GroupChecks()
  const sim = new GroupSim(seed, checks)
  checks.bind(sim)
  try {
    const members = plan.members ?? sim.between(2, 12)
    const chaosMs = plan.chaosMs ?? CHAOS_MS
    const ids = Array.from({ length: members }, (_, index) => fixtureId(index + 1))
    if (plan.clocks) {
      for (const id of ids) {
        const skew = drawSkew(sim, plan.clocks)
        sim.skews.set(id, skew)
        sim.note(`clock of m${Number.parseInt(id.slice(-12), 16)}: offset ${skew.offset} ms, drift ${(skew.drift * 1e6).toFixed(0)} ppm${skew.monoStops ? ', stops in sleep' : ''}`)
      }
    }
    sim.record = plan.record ?? false
    sim.ordered = plan.relay === 'honest'
    sim.timing = plan.timing ?? sim.timing
    sim.fault = plan.fault ?? (sim.ordered ? HONEST : sim.pick(FAULTS))
    sim.note(`seed ${seed}: up to ${members} members, ${plan.relay ?? 'hostile'} relay, faults ${JSON.stringify(sim.fault)}`)
    schedule(sim, ids, plan, chaosMs)
    await sim.runUntil(SIM_START + chaosMs)
    calm(sim, ids, plan)
    const agreedAfterMs = await quietPhase(sim, checks, plan.agreeWithinMs ?? AGREE_WITHIN_MS, plan.watch)
    if (agreedAfterMs !== null && plan.then) await plan.then(sim, checks)
    return { seed, members, events: sim.events, endMs: sim.now - SIM_START, agreedAfterMs, violations: [...sim.violations], trace: [...sim.trace] }
  } finally {
    await sim.dispose()
  }
}

/** How long a scripted member sleeps: past the absence limit of SIM_TIMING, so it is removed meanwhile. */
export const SCRIPT_SLEEP_MS = 45_000

export interface Script {
  /** Link latency per member number, in ms. */
  latency: Record<number, number>
  /** What happens, at which virtual ms; members are named by number. */
  steps: readonly (readonly [at: number, action: 'join' | 'leave' | 'reload' | 'sleep', member: number])[]
  /** Decides the fate of every handshake before it is delivered (see WireControl); in order and on time when left out. */
  control?: WireControl
  /** Called with the simulator before anything runs: to hook it (groupCrash.fixture.ts). */
  setup?: (sim: GroupSim) => void
  /** When the script is over and the room must come to agree. */
  quietAt: number
  agreeWithinMs?: number
}

/**
 * Members 2 and 3 are 2.4 s apart, longer than the turn-taking that keeps
 * leaf renewals from meeting: both renew for the same epoch, and member 1
 * hears the higher id's renewal first.
 */
export const RENEWAL_RACE: Script = {
  latency: { 1: 0, 2: 1200, 3: 1200 },
  steps: [[0, 'join', 1], [3000, 'join', 2], [3100, 'join', 3]],
  quietAt: 12_000,
}

/** The senders of renewals (commits that hold nothing) per epoch, as the trace shows them delivered. */
export function renewalsByEpoch(result: SimResult): Map<string, Set<string>> {
  const seen = new Map<string, Set<string>>()
  for (const line of result.trace) {
    const match = /deliver (m\d+)#\d+->\S+ commit@(\d+)\[\]/.exec(line)
    if (!match) continue
    const [, sender, epoch] = match as unknown as [string, string, string]
    seen.set(epoch, (seen.get(epoch) ?? new Set()).add(sender))
  }
  return seen
}

/** Whether a member that held keys lost its group at some point. */
export function someoneStartedOver(result: SimResult): boolean {
  return result.trace.some(line => line.endsWith(' keys none'))
}

/** A scripted run on the honest relay: fixed latencies, nothing dropped, duplicated or reordered. */
export async function runScript(script: Script): Promise<SimResult> {
  const checks = new GroupChecks()
  const sim = new GroupSim(0, checks)
  checks.bind(sim)
  try {
    sim.ordered = true
    sim.latency = new Map(Object.entries(script.latency).map(([member, ms]) => [fixtureId(Number(member)), ms]))
    sim.control = script.control ?? null
    script.setup?.(sim)
    for (const [at, action, member] of script.steps) {
      const id = fixtureId(member)
      sim.after(null, at, () => (action === 'reload' ? sim.reload(id, true) : action === 'sleep' ? sim.sleep(id, SCRIPT_SLEEP_MS) : sim[action](id)))
    }
    await sim.runUntil(SIM_START + script.quietAt)
    sim.note('the script is over')
    const agreedAfterMs = await quietPhase(sim, checks, script.agreeWithinMs ?? AGREE_WITHIN_MS)
    return { seed: 0, members: Object.keys(script.latency).length, events: sim.events, endMs: sim.now - SIM_START, agreedAfterMs, violations: [...sim.violations], trace: [...sim.trace] }
  } finally {
    await sim.dispose()
  }
}

/** A failure message a person can act on: the seed, what broke and the end of the trace. */
export function report(result: SimResult, tail = 60): string {
  return [`seed ${result.seed} (${result.members} members, ${result.events} events)`, ...result.violations, '--- trace (end) ---', ...result.trace.slice(-tail)].join('\n')
}
