import {
  zeroOutUint8Array,
  type CiphersuiteImpl,
  type ClientState,
  type Credential,
  type KeyPackage,
} from 'ts-mls'
import { mediaUserId, type MediaTiming } from './bounds'
import { clock } from './clock'
import type { MediaNotice } from './api'
import { Backoff } from './backoff'
import { type EpochKeys } from './frame'
import { type MediaVerification } from './verify'
import { SWAP_BASE_MS, RESET_BASE_MS, BACKOFF_MAX_MS, RIVAL_BASE_MS, RIVAL_MAX_MS, type MediaStep, type OwnPackage } from './groupParams'
import { sameBytes, wipeConsumed, treeMembers, treeLeafSigner, lowest, encodeWire } from './groupTree'

export abstract class GroupBase {
  protected state: ClientState | null = null
  protected readonly pending = new Map<string, KeyPackage>()
  /** The epoch before the current one: it opens frames only until `until` (PREVIOUS_EPOCH_MS after the switch). */
  protected previousEpoch: { epoch: number, key: EpochKeys, until: number } | null = null
  protected current: { epoch: number, key: EpochKeys, verification: MediaVerification, soft: boolean } | null = null
  protected room: readonly string[] = []
  protected offeredFor: string | null = null
  /** This client created its group, and told this room about it. */
  /** Members whose fresh package says they reloaded: the next committer swaps them in. */
  protected readonly restarting = new Set<string>()
  /** The room my group of one last announced itself to. */
  protected announcedFor: string | null = null
  /** Key-package wires seen from each member, newest last, to ignore a repeat or an old one. */
  protected readonly accepted = new Map<string, Uint8Array[]>()
  /**
   * Key-package wires the relay delivered from each member, newest last. A
   * commit may add an identity only with a package listed here. Unlike
   * `accepted` it survives a discarded group and is pruned to the room.
   */
  protected readonly relayed = new Map<string, Uint8Array[]>()
  /** A welcome from a lower id outside my group may replace it this rarely per sender. */
  /** The roster as last announced, ahead of what the queue has handled. */
  protected hinted = new Set<string>()
  /** The leaves removed by the commit the rules last accepted: all others must keep identity and signature key. */
  protected vetted: ReadonlySet<number> = new Set()
  protected readonly rivals = new Backoff(RIVAL_BASE_MS, RIVAL_MAX_MS)
  protected closed = false
  protected readonly swaps = new Backoff(SWAP_BASE_MS, BACKOFF_MAX_MS)
  protected readonly resets = new Backoff(RESET_BASE_MS, BACKOFF_MAX_MS)
  protected notice: (notice: MediaNotice) => void = () => {}
  protected stepListener: (step: MediaStep) => void = () => {}
  protected deferTimer: ReturnType<typeof setTimeout> | null = null
  protected offerTimer: ReturnType<typeof setTimeout> | null = null
  protected takeoverTimer: ReturnType<typeof setTimeout> | null = null
  protected lastRoster: readonly string[] = []
  protected readonly heard = new Set<string>()
  protected readonly firstSeen = new Map<string, number>()
  protected tail: Promise<void> = Promise.resolve()
  /** When each member of the group last showed a sign of life (any handshake, or a heartbeat). */
  protected readonly alive = new Map<string, number>()
  /** Newest heartbeat time accepted from each member (and the leaf key that signed it), so an older copy counts for nothing. */
  protected readonly beats = new Map<string, { at: number, signer: Uint8Array }>()
  /** Members that advertised frame version 3, with the leaf key that signed it: a different leaf (a reload) counts for nothing. */
  protected readonly capable = new Map<string, Uint8Array>()
  /** This member's own capability is due: a member it has not told is in the group. */
  protected capDue = true
  /** Members seen in the group since this member last advertised. */
  protected readonly capHeard = new Set<string>()
  /** Since when this browser noticed that it slept, if it did. */
  protected sleptAt: number | null = null
  protected lastTickWall = 0
  protected epochAt = 0
  protected rotateAfter = 0
  protected lastBeat = 0
  protected lastBeatAt = 0
  protected lastTick = 0
  protected tickTimer: ReturnType<typeof setTimeout> | null = null
  /** Each member's leaf (its HPKE key) and since when this client has seen that very leaf. */
  protected readonly leaves = new Map<string, { key: Uint8Array, at: number }>()
  /** When this member's own leaf is due to be replaced by a self-update. */
  protected updateAfter = 0
  /** When the last handshake (other than a heartbeat) and the last roster change were seen. */
  protected lastHandshakeAt = 0
  protected lastRosterAt = 0
  /** When each member's last self-update was taken, so none may force an epoch more than twice per period. */
  protected readonly updates = new Map<string, number>()
  /**
   * The group as it was right before a self-update (a commit with nothing in
   * it but its sender's new leaf) took effect, for CONFLICT_MS. Self-updates
   * come from members other than the committer, so one can race the
   * committer's commit or another update for the same epoch; the committer's
   * wins, and between updates the lower id does. A member that took the loser
   * goes back to this state and takes the winner.
   */
  protected selfUpdate: {
    state: ClientState
    current: GroupBase['current']
    previousEpoch: GroupBase['previousEpoch']
    sender: string
    until: number
    held: Uint8Array[]
  } | null = null

  protected constructor(
    protected readonly userId: string,
    protected readonly groupId: Uint8Array,
    protected own: OwnPackage,
    protected readonly cs: CiphersuiteImpl,
    protected readonly credential: Credential,
    protected readonly timing: MediaTiming,
  ) {}

  /** Called when keys changed in a way the member did not ask for. */
  onNotice(listener: (notice: MediaNotice) => void) {
    this.notice = listener
  }

  /** Called with what a timer produced: a retry, a new offer, a takeover. */
  onStep(listener: (step: MediaStep) => void) {
    this.stepListener = listener
  }

  /** Frames of the current epoch open again: the wait before the next reset starts over. */
  opened() {
    this.resets.reset()
  }

  /**
   * The newest roster, noted at once: handshakes queued before the next sync
   * are judged against it, not against the room the last sync saw.
   */
  hint(roster: readonly string[]) {
    this.hinted = new Set(this.others(roster))
  }

  protected enqueue<T>(job: () => Promise<T>): Promise<T> {
    const run = this.tail.then(job)
    this.tail = run.then(() => undefined, () => undefined)
    return run
  }

  protected step(wires: Uint8Array[] = []): MediaStep {
    const step: MediaStep = { wires, epoch: this.current?.epoch ?? null, key: this.current?.key ?? null, verification: this.current?.verification ?? null }
    if (this.current?.soft) step.soft = true
    if (this.current && this.state) step.caps = this.capabilities(this.current.key, this.state)
    return step
  }

  /**
   * Who can open frame version 3 in the current epoch (this member always
   * can), and whether that is every member: only then may anyone seal it.
   */
  protected capabilities(key: EpochKeys, state: ClientState): { members: string[], complete: boolean } {
    const members: string[] = []
    let complete = true
    for (const id of key.verifiers.keys()) {
      const signer = this.capable.get(id)
      const leaf = id === this.userId ? null : treeLeafSigner(state, id)
      if (id === this.userId || (signer !== undefined && leaf !== null && sameBytes(signer, leaf))) members.push(id)
      else complete = false
    }
    return { members: members.sort(), complete }
  }

  /**
   * The keys that may open frames now: the current epoch's and, for
   * PREVIOUS_EPOCH_MS after the switch, the one before it. After that the
   * previous epoch's key and verifier set are gone.
   */
  openKeys(): Map<number, EpochKeys> {
    const out = new Map<number, EpochKeys>()
    const previous = this.previousEpoch
    if (previous && clock.now() >= previous.until) this.previousEpoch = null
    if (this.previousEpoch) out.set(this.previousEpoch.epoch, this.previousEpoch.key)
    if (this.current) out.set(this.current.epoch, this.current.key)
    return out
  }

  protected others(roster: readonly string[]): string[] {
    const ids = new Set<string>()
    for (const value of roster) {
      const id = mediaUserId(value)
      if (id && id !== this.userId) ids.add(id)
    }
    return [...ids]
  }

  /** Lowest id that is both in the tree and still in the room. */
  protected committer(others: readonly string[]): string | null {
    if (!this.state) return null
    const room = new Set(others)
    room.add(this.userId)
    const active: string[] = []
    // A member that is connected but silent for longer than it takes to be
    // removed (a hung tab) must not hold the role: the next one takes over and
    // removes it. Everyone derives this from the same signs of life.
    const now = clock.now()
    const patience = this.timing.absentMs + 2 * Math.max(10, this.timing.maintenanceMs)
    for (const id of treeMembers(this.state).keys()) {
      if (!room.has(id) || this.restarting.has(id)) continue
      if (id !== this.userId && now - (this.alive.get(id) as number) > patience) continue
      active.push(id)
    }
    return lowest(active)
  }

  protected canCommit(others: readonly string[]): boolean {
    return this.committer(others) === this.userId
  }

  /** Drops secrets of a state we refuse, without erasing keys the current group still uses. */
  protected wipeRejected(state: ClientState) {
    const kept = new Set<Uint8Array>()
    const keep = (bytes: Uint8Array | undefined) => { if (bytes) kept.add(bytes) }
    keep(this.own.privatePackage.initPrivateKey)
    keep(this.own.privatePackage.hpkePrivateKey)
    keep(this.own.privatePackage.signaturePrivateKey)
    if (this.state) {
      const schedule = this.state.keySchedule
      for (const secret of [
        schedule.exporterSecret, schedule.senderDataSecret, schedule.externalSecret, schedule.confirmationKey,
        schedule.membershipKey, schedule.resumptionPsk, schedule.epochAuthenticator, schedule.initSecret,
        this.state.signaturePrivateKey, this.state.confirmationTag,
      ]) keep(secret)
      for (const secret of Object.values(this.state.privatePath.privateKeys)) keep(secret)
    }
    const wipe = (bytes: Uint8Array | undefined) => { if (bytes && !kept.has(bytes)) zeroOutUint8Array(bytes) }
    const schedule = state.keySchedule
    for (const secret of [
      schedule.exporterSecret, schedule.senderDataSecret, schedule.externalSecret, schedule.confirmationKey,
      schedule.membershipKey, schedule.resumptionPsk, schedule.epochAuthenticator, schedule.initSecret,
      state.signaturePrivateKey, state.confirmationTag,
    ]) wipe(secret)
    for (const secret of Object.values(state.privatePath.privateKeys)) wipe(secret)
  }

  protected discard() {
    const state = this.state
    this.state = null
    this.current = null
    this.previousEpoch = null
    this.sleptAt = null
    this.pending.clear()
    this.accepted.clear()
    this.offeredFor = null
    this.restarting.clear()
    this.announcedFor = null
    this.alive.clear()
    this.beats.clear()
    this.capable.clear()
    this.capHeard.clear()
    this.capDue = true
    this.leaves.clear()
    this.updates.clear()
    this.dropSelfUpdate()
    if (this.tickTimer !== null) clearTimeout(this.tickTimer)
    this.tickTimer = null
    if (state) this.wipeRejected(state)
  }

  /** Forgets the state before a self-update once the time for a conflict has passed (or at once with `now`). */
  protected dropSelfUpdate(expiredOnly = false) {
    const saved = this.selfUpdate
    if (!saved || (expiredOnly && clock.now() < saved.until)) return
    this.selfUpdate = null
    wipeConsumed(saved.held)
    this.wipeRejected(saved.state)
  }

  protected seen(from: string, wire: Uint8Array): boolean {
    return (this.accepted.get(from) ?? []).some(old => sameBytes(old, wire))
  }

  protected remember(from: string, wire: Uint8Array) {
    const history = (this.accepted.get(from) ?? []).filter(old => !sameBytes(old, wire))
    history.push(new Uint8Array(wire))
    this.accepted.set(from, history.slice(-8))
  }

  protected relay(from: string, keyPackage: KeyPackage) {
    const wire = encodeWire({ version: 'mls10', wireformat: 'mls_key_package', keyPackage })
    if (!wire) return
    const list = (this.relayed.get(from) ?? []).filter(old => !sameBytes(old, wire))
    list.push(wire)
    this.relayed.set(from, list.slice(-8))
  }

  protected wasRelayed(id: string, keyPackage: KeyPackage): boolean {
    const wire = encodeWire({ version: 'mls10', wireformat: 'mls_key_package', keyPackage })
    return wire !== null && (this.relayed.get(id) ?? []).some(old => sameBytes(old, wire))
  }

  /** The roster as last announced. */
  protected listed(): string[] {
    return [...this.hinted]
  }

  /** How long this browser was away since the last tick: the larger of what the monotonic and the wall clock saw (a monotonic clock may stand still in sleep). */
  protected sleepGap(now: number): number {
    return Math.max(now - this.lastTick, clock.wall() - this.lastTickWall)
  }

  protected sleepLimit(): number {
    return 2 * Math.max(10, this.timing.maintenanceMs) + 60_000
  }

  /** This browser was asleep just now: a removal that follows is for its silence, not a hostile relay. */
  protected sleptRecently(): boolean {
    const now = clock.now()
    if (this.sleepGap(now) > this.sleepLimit()) return true
    return this.sleptAt !== null && now - this.sleptAt <= this.sleepLimit()
  }

  protected mayUpdate(id: string): boolean {
    const last = this.updates.get(id)
    return last === undefined || clock.now() - last >= Math.max(0, this.timing.rotateMs - this.timing.rotateJitterMs) / 2
  }

  /** The two lowest members of the group the room lists (zombies included): whose commit a missed one may be blamed on. */
  protected mayCommit(id: string): boolean {
    const room = new Set(this.room)
    room.add(this.userId)
    const live = [...treeMembers(this.state as ClientState).keys()].filter(member => room.has(member)).sort()
    return live.slice(0, 2).includes(id)
  }
}
