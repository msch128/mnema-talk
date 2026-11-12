// A deterministic simulator for the media group protocol.
//
// It runs the real call (browserMediaCall on the real MediaGroup, with real
// MLS and frame crypto) for 2 to 12 members on one virtual clock, and plays
// the relay between them: a seeded scheduler delays, reorders, duplicates and
// drops every handshake per recipient while members join, leave, reload,
// reconnect and sleep. Then the scheduler stops interfering and the
// invariants in groupSimChecks.fixture.ts must hold.
//
// What is a model, not the product: the frame worker. Whether a member's
// frames open at another member (which feeds the call's "I fell behind"
// evidence) is judged by comparing safety codes, with the worker's retention
// times. The final checks use the real frame seal and open again.
//
// One event runs at a time and the member it touched is settled before the
// next, so a seed always produces the same trace.
import { AsyncLocalStorage } from 'node:async_hooks'
import { vi } from 'vitest'
import { decodeMlsMessage } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaCall } from './api'
import { PREVIOUS_EPOCH_MS, STALE_SEAL_MS, type MediaTiming } from './bounds'
import { browserMediaCall, mediaSettled } from './browserCall'
import { decodeWire } from './callEngine'
import { isCapability } from './capability'
import type { EpochKeys } from './frame'
import { clock } from './clock'
import { MediaGroup } from './group'
import type { FrameStats, MediaGuard } from './guard'
import { isHeartbeat } from './heartbeat'
import type { MediaVerification } from './verify'

export const SIM_CHANNEL = fixtureId(0xc0ffee)
export const SIM_START = Date.UTC(2026, 0, 1)
/** Short lifetimes, so rotation, renewal, heartbeats and absence all happen inside one run. */
export const SIM_TIMING: MediaTiming = { rotateMs: 40_000, rotateJitterMs: 4000, absentMs: 30_000, heartbeatMs: 7000, maintenanceMs: 2000 }
const EMPTY = new Uint8Array(0)
const STATS_MS = 1000
const QUIET_DELAY_MS = 20
const TIMER_ID_BASE = 2 ** 40
/** Ids nobody is behind: one that sorts above every member and one below. */
export const GHOST_HIGH = fixtureId(0xfffff)
export const GHOST_LOW = '00000000-0000-4000-8000-000000000000'

/** mulberry32: small, seedable, and good enough to pick delays and actions. */
export function seededRandom(seed: number): () => number {
  let state = seed >>> 0
  return () => {
    state = (state + 0x6d2b79f5) >>> 0
    let t = state
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/**
 * How one machine keeps time. Every member runs on the simulator's virtual
 * clock plus this: its own offset, a rate error, a step of the wall clock
 * (the monotonic clock that clock.now reads does not follow it), and the
 * time the monotonic clock stood still while the machine slept. All zero is
 * the simulator's single clock.
 */
export interface ClockSkew {
  /** Both clocks run this far ahead of virtual time, in ms. */
  offset: number
  /** The monotonic clock gains this many ms per ms of virtual time since SIM_START (0.001 is 1000 ppm). */
  drift: number
  /** The wall clock was stepped by this much in total (NTP, a manual change). */
  wallStep: number
  /** Time spent asleep that the monotonic clock did not count. */
  monoLost: number
  /** Whether the monotonic clock stands still while the machine sleeps. */
  monoStops: boolean
}

export const NO_SKEW: Readonly<ClockSkew> = { offset: 0, drift: 0, wallStep: 0, monoLost: 0, monoStops: false }

/** One browser tab in the call: a reload is a new one with the same user id. */
export interface SimMember {
  readonly id: string
  readonly label: string
  /** The machine's clocks: shared by every tab of this user. */
  readonly skew: ClockSkew
  readonly call: MediaCall
  group: MediaGroup | null
  /** The barrier on the group's queue, set once the group is open. */
  probe: (() => Promise<unknown>) | null
  gone: boolean
  /** Frozen (a sleeping tab) until then: its timers and its socket wait. */
  wakeAt: number
  /** When it last woke from a sleep long enough to notice. */
  wokeAt: number
  roster: string[]
  previousRoster: string[]
  /** When the last thing the server sent it is due: with an ordered relay nothing overtakes that. */
  linkDueAt: number
  rosterDueAt: number
  /** Its socket is down until then: what the room relays meanwhile never reaches it, and it sends nothing. */
  offlineUntil: number
  current: MediaVerification | null
  staleAt: number | null
  /** Safety codes of epochs it left and may still open frames of, by `${epoch}:${code}`. */
  readonly opens: Map<string, { epoch: number, until: number }>
  /** Every key the call ever handed its frame worker. */
  readonly keys: { epoch: number, key: EpochKeys }[]
  stats: (senders: Record<string, FrameStats>) => void
}

export interface SimHooks {
  /** A wire went through `member`'s group; `before` and `after` are what it held around it. */
  received(member: SimMember, from: string, wire: Uint8Array, before: MediaVerification | null, after: MediaVerification | null): Promise<void>
  /** The call reported new keys (or none). */
  epoch(member: SimMember, verification: MediaVerification | null): void
}

/** A handshake on its way, as the control sees it. */
export interface WireInfo { from: string, to: string, kind: string }
interface Timer { id: number, at: number, owner: SimMember | null, run: () => void, wire: boolean, info?: WireInfo | undefined }

/**
 * Asked before the simulator delivers a handshake that is on its way: deliver
 * it ('run'), lose it ('drop'), or deliver the next one on its way to the same
 * member first ('swap'). A schedule explorer plays the relay with this.
 */
export type WireControl = (wire: WireInfo, pending: number) => 'run' | 'drop' | 'swap'

export interface Fault { drop: number, duplicate: number, slow: number, stall: number }

export function epochKey(verification: MediaVerification | null): string | null {
  return verification ? `${verification.epoch}:${verification.code}` : null
}

export function memberNumber(id: string): number {
  return Number.parseInt(id.slice(-12), 16)
}

function proposalKinds(proposals: readonly { proposalOrRefType: string, proposal?: { proposalType: unknown } }[]): string {
  return proposals.map(item => (item.proposalOrRefType === 'proposal' ? String(item.proposal?.proposalType) : 'ref')).join(',')
}

/** What a handshake wire is, for the trace. */
export function describeWire(wire: Uint8Array): string {
  if (isHeartbeat(wire)) return 'heartbeat'
  if (isCapability(wire)) return 'capability'
  try {
    const message = decodeMlsMessage(wire, 0)?.[0]
    if (!message) return 'garbage'
    if (message.wireformat === 'mls_key_package') return 'key-package'
    if (message.wireformat === 'mls_welcome') return 'welcome'
    if (message.wireformat !== 'mls_public_message') return message.wireformat
    const content = message.publicMessage.content
    if (content.contentType !== 'commit') return content.contentType
    return `commit@${content.epoch}[${proposalKinds(content.commit.proposals)}]`
  } catch {
    return 'garbage'
  }
}

export class GroupSim {
  now = SIM_START
  readonly trace: string[] = []
  readonly violations: string[] = []
  readonly all: SimMember[] = []
  /** The room as the server holds it: user id to the tab that is connected. */
  readonly room = new Map<string, SimMember>()
  /** While set, the scheduler interferes with the relay. */
  fault: Fault | null = null
  /** The lifetimes the members run with; SIM_TIMING unless a plan says otherwise. */
  timing: MediaTiming = SIM_TIMING
  /**
   * The relay of an honest server: each member gets everything over one
   * socket, so in the order the server sent it, once, and only delayed.
   */
  ordered = false
  /** Each member's link latency in ms, for a scripted run: a wire takes the sender's plus the recipient's. */
  latency: Map<string, number> | null = null
  events = 0
  /** When set, decides the fate of every handshake before it is delivered. */
  control: WireControl | null = null
  /** Whether what a tab sends after it was closed is lost, as it is when the process is gone (a crash point leaves the tab's last step running). */
  dropWiresOfGone = false
  /** While set, every handshake a member sends is kept in `log`, for replayAll. */
  record = false
  readonly log: { from: string, text: string }[] = []
  private readonly random: () => number
  private readonly jitter: () => number
  /** Draws for the machines' clocks only, so that giving them clocks leaves the rest of a seed's run alone. */
  readonly clockDraw: () => number
  private readonly context = new AsyncLocalStorage<SimMember>()
  private readonly timers = new Map<number, Timer>()
  private readonly touched = new Set<SimMember>()
  /** Tabs that crashed: their last step never finishes, so nothing waits for it. */
  private readonly dead = new Set<SimMember>()
  /** Releases whoever waits for a tab that has just crashed. */
  private readonly release = new Map<SimMember, () => void>()
  private readonly restore: (() => void)[] = []
  private timerSeq = TIMER_ID_BASE
  private memberSeq = 0
  /** The clocks of each user's machine, by user id; zero until set. */
  readonly skews = new Map<string, ClockSkew>()

  constructor(readonly seed: number, private readonly hooks: SimHooks) {
    this.random = seededRandom(seed)
    this.jitter = seededRandom(seed ^ 0x5bd1e995)
    this.clockDraw = seededRandom(seed ^ 0x2545f491)
    this.install()
  }

  /** A number in [0, 1) from the seed. */
  draw(): number {
    return this.random()
  }

  between(low: number, high: number): number {
    return low + Math.floor(this.random() * (high - low + 1))
  }

  pick<T>(items: readonly T[]): T {
    return items[Math.floor(this.random() * items.length)] as T
  }

  note(text: string) {
    this.trace.push(`${((this.now - SIM_START) / 1000).toFixed(3).padStart(8)} ${text}`)
  }

  violation(text: string) {
    this.note(`VIOLATION ${text}`)
    this.violations.push(`t=${((this.now - SIM_START) / 1000).toFixed(3)}s ${text}`)
  }

  live(): SimMember[] {
    return [...this.room.values()]
  }

  asleep(member: SimMember): boolean {
    return member.wakeAt > this.now
  }

  /** Runs `run` after `delay`. With an owner it waits while that tab sleeps and is dropped once the tab is gone. */
  after(owner: SimMember | null, delay: number, run: () => void, wire = false, info?: WireInfo | undefined): number {
    const id = ++this.timerSeq
    this.timers.set(id, { id, at: this.now + Math.max(0, delay), owner, run, wire, info })
    return id
  }

  /** Virtual time, the group's own timers and its jitter all come from the simulator while a member's code runs. */
  private install() {
    const realSet = globalThis.setTimeout
    const realClear = globalThis.clearTimeout
    const realRandom = Math.random
    vi.useFakeTimers({ toFake: ['Date'], now: SIM_START })
    // What the code of a member reads as the time: the virtual one through that member's clocks.
    const virtual = Date.now.bind(Date)
    const realMono = clock.now
    const mine = () => this.context.getStore()?.skew ?? NO_SKEW
    Date.now = () => virtual() + mine().offset + mine().wallStep
    clock.now = () => virtual() + mine().offset + mine().drift * (virtual() - SIM_START) - mine().monoLost
    const setTimer = (run: (...args: unknown[]) => void, delay?: number, ...args: unknown[]) => {
      const owner = this.context.getStore()
      if (!owner) return realSet(run, delay, ...args)
      return this.after(owner, delay ?? 0, () => run(...args))
    }
    const clearTimer = (id?: unknown) => {
      if (typeof id === 'number' && this.timers.delete(id)) return
      realClear(id as Parameters<typeof clearTimeout>[0])
    }
    Object.assign(globalThis, { setTimeout: setTimer, clearTimeout: clearTimer })
    Math.random = () => (this.context.getStore() ? this.jitter() : realRandom())
    const open = MediaGroup.open.bind(MediaGroup)
    const spy = vi.spyOn(MediaGroup, 'open').mockImplementation(async (userId, channelId) => {
      const group = await open(userId, channelId, this.timing)
      const member = this.context.getStore()
      if (member && group) this.observe(member, group)
      return group
    })
    this.restore.push(() => {
      clock.now = realMono
      spy.mockRestore()
      Math.random = realRandom
      Object.assign(globalThis, { setTimeout: realSet, clearTimeout: realClear })
      vi.useRealTimers()
    })
  }

  /** Reports every wire the member's group handles, with the group's keys before and after it. */
  private observe(member: SimMember, group: MediaGroup) {
    const receive = group.receive.bind(group)
    member.group = group
    // A wire of no length is refused before anything is touched: it only waits for the queue.
    member.probe = () => receive('', EMPTY)
    group.receive = async (from, wire, batch) => {
      const before = (await receive('', EMPTY)).verification ?? null
      const step = await receive(from, wire, batch)
      await this.hooks.received(member, from, wire, before, step.verification ?? null)
      return step
    }
  }

  private enter(member: SimMember, run: () => void) {
    if (member.gone) return
    this.touched.add(member)
    this.context.run(member, run)
  }

  private newMember(id: string): SimMember {
    const guard: MediaGuard = {
      armSender: () => true,
      armReceiver: () => true,
      setKey: (epoch, key) => { member.keys.push({ epoch, key }) },
      staleKeys: () => undefined,
      onStats: (listener) => { member.stats = listener },
      reset: () => undefined,
    }
    const skew = this.skews.get(id) ?? { ...NO_SKEW }
    this.skews.set(id, skew)
    const member: SimMember = {
      id, skew, label: `m${memberNumber(id)}#${++this.memberSeq}`, call: null as unknown as MediaCall, group: null, probe: null,
      gone: false, wakeAt: 0, wokeAt: -Infinity, roster: [], previousRoster: [], linkDueAt: 0, rosterDueAt: 0, offlineUntil: 0,
      current: null, staleAt: null, opens: new Map(), keys: [], stats: () => undefined,
    }
    Object.assign(member, { call: browserMediaCall(guard) })
    return member
  }

  private keysChanged(member: SimMember, verification: MediaVerification | null) {
    const left = epochKey(member.current)
    if (!verification) {
      member.staleAt ??= this.now
    } else {
      if (left && left !== epochKey(verification) && member.current) member.opens.set(left, { epoch: member.current.epoch, until: this.now + PREVIOUS_EPOCH_MS })
      member.current = verification
      member.staleAt = null
    }
    this.note(`${member.label} keys ${verification ? `${epochKey(verification)} {${verification.members.map(memberNumber).join(',')}}` : 'none'}`)
    this.hooks.epoch(member, verification)
  }

  /** The handshakes on their way: to whom, what, and how long until they arrive. */
  inFlight(): { to: string, kind: string, due: number }[] {
    return [...this.timers.values()].filter(timer => timer.wire && timer.info).map(timer => ({ to: timer.info?.to ?? '', kind: timer.info?.kind ?? '', due: Math.max(0, timer.at - this.now) }))
  }

  /** The tab whose code is running now, if any: the one a timer or a wire was handed to. */
  running(): SimMember | undefined {
    return this.context.getStore()
  }

  /**
   * The tab dies here, in the middle of whatever it is doing: it is gone at once (what it still does is lost, and
   * nothing waits for it), and a new tab with the same id takes its place at the next instant, as after a reload.
   */
  crashNow(member: SimMember, seen: boolean) {
    if (member.gone || this.room.get(member.id) !== member) return
    this.note(`crash ${member.label} (${seen ? 'the roster shows it' : 'unseen'})`)
    this.dead.add(member)
    this.release.get(member)?.()
    this.after(null, 0, () => this.reload(member.id, seen))
    this.drop(member, false)
  }

  /** A tab enters the Talk: the server tells it who is here and tells the others. */
  join(id: string) {
    const member = this.newMember(id)
    this.all.push(member)
    this.room.set(id, member)
    this.note(`join ${member.label}`)
    // The app starts the call with the members the Talk already lists.
    member.roster = [...this.room.keys()].sort()
    this.enter(member, () => {
      member.call.onWire((_channel, wire) => this.relay(member, wire))
      member.call.onVerification(verification => this.keysChanged(member, verification))
      member.call.start(SIM_CHANNEL, id)
      member.call.roster(member.roster)
    })
    this.after(member, STATS_MS, () => this.reportStats(member))
    this.announce(member)
  }

  private drop(member: SimMember, stop = true) {
    member.gone = true
    if (!stop) return
    this.touched.add(member)
    this.context.run(member, () => member.call.stop())
    if (this.room.get(member.id) === member) this.room.delete(member.id)
  }

  leave(id: string) {
    const member = this.room.get(id)
    if (!member) return
    this.note(`leave ${member.label}`)
    this.drop(member)
    this.announce()
  }

  /** The tab is replaced by a new one. With `seen` the server noticed the gap and says so; otherwise the room never changes. */
  reload(id: string, seen: boolean) {
    const member = this.room.get(id)
    if (!member) return
    this.note(`reload ${member.label}${seen ? ' (roster shows it)' : ''}`)
    this.drop(member)
    if (seen) this.announce()
    this.join(id)
  }

  sleep(id: string, ms: number) {
    const member = this.room.get(id)
    if (!member || this.asleep(member)) return
    this.note(`sleep ${member.label} for ${ms} ms`)
    member.wakeAt = this.now + ms
    if (ms > 60_000) member.wokeAt = member.wakeAt
    // A monotonic clock that stands still in sleep never saw this time: only the wall clock did.
    if (member.skew.monoStops) member.skew.monoLost += ms
  }

  /** The wall clock of the member's machine is set by `ms` (negative: back). The monotonic clock does not follow. */
  stepWall(id: string, ms: number) {
    const member = this.room.get(id)
    if (!member) return
    this.note(`step ${member.label} wall clock by ${ms} ms`)
    member.skew.wallStep += ms
    // Forward by more than a sleep is long, the call takes it for one: a removal then is taken on trust.
    if (ms > 60_000) member.wokeAt = this.now
  }

  /** Every sleeper wakes now. */
  wakeAll() {
    for (const member of this.live()) {
      if (!this.asleep(member)) continue
      if (member.skew.monoStops) member.skew.monoLost -= member.wakeAt - this.now
      member.wakeAt = this.now
      if (member.wokeAt > this.now) member.wokeAt = this.now
    }
  }

  /**
   * The socket drops and comes back after `gap`: what was on its way and what
   * the room relays meanwhile is lost, the server keeps the member listed, and
   * the call asks again once it is back.
   */
  reconnect(id: string, gap: number) {
    const member = this.room.get(id)
    if (!member || this.asleep(member) || member.offlineUntil > this.now) return
    this.note(`reconnect ${member.label} after ${gap} ms`)
    for (const [key, timer] of this.timers) if (timer.owner === member && timer.wire) this.timers.delete(key)
    member.offlineUntil = this.now + gap
    this.after(member, gap, () => {
      member.linkDueAt = this.now
      this.deliverRoster(member, [...this.room.keys()].sort())
      this.enter(member, () => member.call.resume())
    })
  }

  /** When something the server sends `to` now arrives. */
  private due(to: SimMember, delay: number): number {
    const at = this.ordered ? Math.max(this.now + delay, to.linkDueAt + 1) : this.now + delay
    to.linkDueAt = Math.max(to.linkDueAt, at)
    return at - this.now
  }

  private delay(from: SimMember | null, to: SimMember): number {
    if (this.latency) return (from ? this.latency.get(from.id) ?? 0 : 0) + (this.latency.get(to.id) ?? 0) + 1
    const fault = this.fault
    if (!fault) return QUIET_DELAY_MS
    const roll = this.random()
    if (roll < fault.stall) return this.between(3000, 12_000)
    if (roll < fault.stall + fault.slow) return this.between(300, 3000)
    return this.between(5, 300)
  }

  /** The relay: one copy per member in the room, each with its own fate. */
  private relay(from: SimMember, text: string) {
    const wire = decodeWire(text)
    const kind = wire ? describeWire(wire) : 'garbage'
    if (from.gone && this.dropWiresOfGone) return
    if (from.offlineUntil > this.now) return this.note(`lost ${from.label} ${kind} (socket down)`)
    if (this.record) this.log.push({ from: from.id, text })
    for (const to of this.live()) {
      if (to === from) continue
      if (to.offlineUntil > this.now) {
        this.note(`lost ${from.label}->${to.label} ${kind} (socket down)`)
        continue
      }
      const fault = this.fault
      if (fault && this.random() < fault.drop) {
        this.note(`drop ${from.label}->${to.label} ${kind}`)
        continue
      }
      const copies = fault && this.random() < fault.duplicate ? 2 : 1
      for (let copy = 0; copy < copies; copy++) {
        this.after(to, this.due(to, this.delay(from, to)), () => {
          this.note(`deliver ${from.label}->${to.label} ${kind}${copy ? ' (copy)' : ''}`)
          this.enter(to, () => to.call.receive(from.id, text))
        }, true, { from: from.id, to: to.id, kind })
      }
    }
  }

  private deliverRoster(member: SimMember, roster: string[]) {
    member.previousRoster = member.roster
    member.roster = roster
    this.enter(member, () => member.call.roster(roster))
  }

  /** The server's next roster reaches everyone, and it is true. */
  correctRosters() {
    this.announce()
  }

  /**
   * The server tells `id`, and nobody else, a roster that is not the room:
   * 'hide' leaves out another member that is here, 'ghost' lists an id nobody
   * is behind, 'lead' lists one that sorts below every member.
   */
  lie(id: string, kind: 'hide' | 'ghost' | 'lead') {
    const member = this.room.get(id)
    if (!member || member.offlineUntil > this.now) return
    const truth = [...this.room.keys()].sort()
    const others = truth.filter(other => other !== id)
    const hidden = kind === 'hide' && others.length ? this.pick(others) : null
    const roster = (hidden ? truth.filter(other => other !== hidden) : [...truth, ...(kind === 'hide' ? [] : [kind === 'ghost' ? GHOST_HIGH : GHOST_LOW])]).sort()
    this.note(`lie to ${member.label}: room {${roster.map(memberNumber).join(',')}}`)
    // Like every roster, it never overtakes an older one.
    const at = Math.max(this.now + this.due(member, this.between(0, 800)), member.rosterDueAt + 1)
    member.rosterDueAt = at
    this.after(member, at - this.now, () => this.deliverRoster(member, roster))
  }

  /**
   * The server says again what it relayed (see `record`): up to `most` of the
   * handshakes, each to every member but its sender, in no order and spread
   * over `spreadMs`; only of the given `kinds` (commit, welcome, key-package,
   * heartbeat, capability) when named. Whoever sent one may be long gone.
   */
  replayAll(most: number, spreadMs: number, kinds?: readonly string[]) {
    const kindOf = (item: { text: string }) => describeWire(decodeWire(item.text) ?? EMPTY).replace(/@.*$/, '')
    const picked = this.log.filter(item => !kinds || kinds.includes(kindOf(item))).sort(() => this.random() - 0.5).slice(0, most)
    this.note(`replay ${picked.length} old handshakes to ${this.room.size} members`)
    for (const item of picked) {
      for (const to of this.live()) {
        if (to.id === item.from) continue
        this.after(to, this.between(0, spreadMs), () => {
          this.note(`replay ${memberNumber(item.from)}->${to.label} ${describeWire(decodeWire(item.text) ?? EMPTY)}`)
          this.enter(to, () => to.call.receive(item.from, item.text))
        }, true)
      }
    }
  }

  /** The room changed. Each member hears it after a moment, and in the order it happened. */
  private announce(except?: SimMember) {
    const roster = [...this.room.keys()].sort()
    this.note(`room {${roster.map(memberNumber).join(',')}}`)
    for (const member of this.live()) {
      if (member === except || member.offlineUntil > this.now) continue
      const delay = this.ordered ? this.delay(null, member) : this.fault ? this.between(0, 800) : 0
      // A roster never overtakes an older one, whatever the relay does to handshakes.
      const at = Math.max(this.now + this.due(member, delay), member.rosterDueAt + 1)
      member.rosterDueAt = at
      this.after(member, at - this.now, () => this.deliverRoster(member, roster))
    }
  }

  private sealing(member: SimMember): MediaVerification | null {
    if (member.gone || this.asleep(member) || !member.current) return null
    if (member.staleAt !== null && this.now - member.staleAt > STALE_SEAL_MS) return null
    return member.current
  }

  private holds(member: SimMember, key: string): boolean {
    if (epochKey(member.current) === key) return true
    return (member.opens.get(key)?.until ?? 0) > this.now
  }

  private newestHeld(member: SimMember): number {
    let newest = member.current?.epoch ?? -1
    for (const held of member.opens.values()) if (held.until > this.now) newest = Math.max(newest, held.epoch)
    return newest
  }

  /** What the frame worker would report this second: whose frames opened, and whose name an epoch newer than any key held. */
  private reportStats(member: SimMember) {
    const senders: Record<string, FrameStats> = {}
    const newest = this.newestHeld(member)
    for (const publisher of this.live()) {
      const sealed = publisher === member ? null : this.sealing(publisher)
      if (!sealed) continue
      if (this.holds(member, epochKey(sealed) as string)) senders[publisher.id] = { opened: 1, newer: 0, epoch: sealed.epoch, legacy: 0 }
      else if (sealed.epoch > newest) senders[publisher.id] = { opened: 0, newer: 1, epoch: sealed.epoch, legacy: 0 }
    }
    this.enter(member, () => member.stats(senders))
    this.after(member, STATS_MS, () => this.reportStats(member))
  }

  /** The earliest timer that may run by `limit`: a sleeping tab's timers wait until it wakes. */
  private next(limit: number): Timer | null {
    let best: Timer | null = null
    let bestAt = Infinity
    for (const timer of this.timers.values()) {
      if (timer.owner?.gone) {
        this.timers.delete(timer.id)
        continue
      }
      const at = Math.max(timer.at, timer.owner?.wakeAt ?? 0)
      if (at < bestAt) {
        best = timer
        bestAt = at
      }
    }
    if (!best || bestAt > limit) return null
    this.timers.delete(best.id)
    this.now = Math.max(this.now, bestAt)
    return best
  }

  /** What `control` makes of the next timer, when it is a handshake: it may be lost, or another one to the same member goes first. */
  private controlled(timer: Timer | null): Timer | null {
    if (!timer?.wire || !timer.info || !this.control) return timer
    const others = [...this.timers.values()].filter(other => other.wire && other.owner === timer.owner && other.info).sort((a, b) => a.at - b.at || a.id - b.id)
    const choice = this.control(timer.info, others.length + 1)
    if (choice === 'drop') {
      this.note(`drop ${memberNumber(timer.info.from)}->${timer.owner?.label} ${timer.info.kind} (the schedule)`)
      return { id: timer.id, at: timer.at, owner: timer.owner, run: () => undefined, wire: false }
    }
    if (choice === 'swap' && others[0]) {
      this.timers.delete(others[0].id)
      this.timers.set(timer.id, timer)
      this.note(`overtake ${memberNumber(others[0].info?.from ?? '')}->${timer.owner?.label} ${others[0].info?.kind} before ${timer.info.kind}`)
      return others[0]
    }
    return timer
  }

  /** `wait`, or until the tab crashes: what a crashed tab was doing never finishes. */
  private async unlessCrashed(member: SimMember, wait: Promise<unknown>): Promise<void> {
    if (this.dead.has(member)) return
    await Promise.race([wait, new Promise<void>((resolve) => { this.release.set(member, resolve) })])
  }

  private async settle() {
    for (const member of this.touched) {
      if (this.dead.has(member)) continue
      for (let round = 0; round < 3; round++) {
        await this.unlessCrashed(member, mediaSettled(member.call))
        if (member.probe && !member.gone) await this.unlessCrashed(member, member.probe())
      }
    }
    this.touched.clear()
  }

  /** Runs events in order until `limit` (virtual ms), or until `done` says so. */
  async runUntil(limit: number, done: () => boolean = () => false): Promise<void> {
    await this.settle()
    while (!done()) {
      const timer = this.controlled(this.next(limit))
      if (!timer) break
      vi.setSystemTime(this.now)
      this.events++
      if (timer.owner) this.enter(timer.owner, timer.run)
      else timer.run()
      await this.settle()
    }
    if (!done()) {
      this.now = Math.max(this.now, limit)
      vi.setSystemTime(this.now)
    }
  }

  /** Closes every call and hands time, timers and Math.random back. */
  async dispose() {
    for (const member of this.live()) this.drop(member)
    await this.settle()
    for (const undo of this.restore) undo()
  }
}
