import {
  createCommit,
  createGroup,
  decodeMlsMessage,
  defaultCapabilities,
  defaultKeyPackageEqualityConfig,
  defaultKeyRetentionConfig,
  defaultLifetime,
  defaultLifetimeConfig,
  defaultPaddingConfig,
  emptyPskIndex,
  encodeMlsMessage,
  generateKeyPackage,
  getCiphersuiteFromName,
  getCiphersuiteImpl,
  joinGroup,
  mlsExporter,
  processPublicMessage,
  zeroOutUint8Array,
  type CiphersuiteImpl,
  type ClientConfig,
  type ClientState,
  type Credential,
  type IncomingMessageCallback,
  type KeyPackage,
  type PublicMessage,
  type PrivateKeyPackage,
  type Proposal,
} from 'ts-mls'
import { FRAME_EPOCH_BYTES, HANDSHAKE_MAX_BYTES, MEDIA_EXPORTER_LABEL, PREVIOUS_EPOCH_MS, mediaGroupId, mediaUserId, type MediaTiming } from './bounds'
import { clock } from './clock'
import { mediaTiming } from './lifetime'
import type { MediaNotice } from './api'
import { Backoff } from './backoff'
import { importEpochKey, importFrameSigner, importFrameVerifier, type EpochKeys } from './frame'
import { isHeartbeat, openHeartbeat, sealHeartbeat } from './heartbeat'
import { safetyCode, type MediaVerification } from './verify'

// A member's leaf is swapped at most once per this long, doubling while it keeps happening.
const SWAP_BASE_MS = 3000
// A group is thrown away (a missed commit, a restarted committer, a removal)
// at most once per this long, doubling while it keeps happening.
const RESET_BASE_MS = 10_000
const BACKOFF_MAX_MS = 60_000
// A welcome from a lower id outside my group, built on my own key package, may
// make me drop a group that others depend on at most once per this long per
// sender, doubling while it keeps happening.
const RIVAL_BASE_MS = 60_000
const RIVAL_MAX_MS = 600_000
// A joiner whose offer got no welcome in this long offers again.
const OFFER_WAIT_MS = 4000
// The lowest joiner whose offer got no welcome in this long founds the group:
// nobody here holds one. A welcome that was still on its way is taken anyway
// (see the welcome below), so founding early costs nothing.
const FOUND_WAIT_MS = 4000
// A member lower than me that the room lists but never said a word in this
// long cannot be speaking MLS (an old tab, a failed start, someone whose
// reload is still under way): the next one acts. Coming back, it offers and
// is let in. It stays longer than OFFER_WAIT_MS, so a lost welcome is asked
// for again before the member who sent it is written off.
const TAKEOVER_MS = 2 * OFFER_WAIT_MS
// How long the state before a member's self-update is kept: a commit that
// raced it for the same epoch and wins replaces it (see `selfUpdate`).
const CONFLICT_MS = 10_000

export interface MediaStep {
  wires: Uint8Array[]
  epoch: number | null
  key: EpochKeys | null
  /** What members compare to know they hold the same keys; set with the key. */
  verification?: MediaVerification | null
  /**
   * The epoch was reached by a commit that removed nobody: the sender may go on
   * sealing under the epoch it left for a moment (SEND_GRACE_MS).
   */
  soft?: boolean
}

interface OwnPackage {
  publicPackage: KeyPackage
  privatePackage: PrivateKeyPackage
}


let suitePromise: Promise<CiphersuiteImpl> | null = null

function suite(): Promise<CiphersuiteImpl> {
  suitePromise ??= getCiphersuiteImpl(getCiphersuiteFromName('MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519'))
  return suitePromise
}

function credentialIdentity(credential: Credential): string | null {
  if (credential.credentialType !== 'basic') return null
  const text = new TextDecoder().decode(credential.identity)
  return mediaUserId(text) === text ? text : null
}

function clientConfig(): ClientConfig {
  return {
    keyRetentionConfig: defaultKeyRetentionConfig,
    lifetimeConfig: defaultLifetimeConfig,
    keyPackageEqualityConfig: defaultKeyPackageEqualityConfig,
    paddingConfig: defaultPaddingConfig,
    authService: {
      validateCredential(credential: Credential): Promise<boolean> {
        return Promise.resolve(credentialIdentity(credential) !== null)
      },
    },
  }
}

function sameBytes(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false
  for (let i = 0; i < left.byteLength; i++) if (left[i] !== right[i]) return false
  return true
}

function wipeBytes(bytes: Uint8Array | undefined) {
  if (bytes) zeroOutUint8Array(bytes)
}

function wipeConsumed(consumed: Uint8Array[]) {
  for (const secret of consumed) wipeBytes(secret)
}

function treeMembers(state: ClientState): Map<string, number> {
  const members = new Map<string, number>()
  state.ratchetTree.forEach((node, index) => {
    if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
    const id = credentialIdentity(node.leaf.credential)
    if (id) members.set(id, index / 2)
  })
  return members
}

/** Every leaf's identity, in tree order: a repeated or unreadable one stays visible ('' for unreadable). */
function leafIdentities(state: ClientState): string[] {
  const ids: string[] = []
  state.ratchetTree.forEach((node, index) => {
    if (node && node.nodeType === 'leaf' && index % 2 === 0) ids.push(credentialIdentity(node.leaf.credential) ?? '')
  })
  return ids
}

/**
 * Each member's frame verifying key, from its leaf's signature key. An id that
 * holds more than one leaf gets none: which of its leaves signed a frame could
 * not be told apart.
 */
export async function frameVerifiers(state: ClientState): Promise<Map<string, CryptoKey> | null> {
  const leaves = new Map<string, Uint8Array[]>()
  state.ratchetTree.forEach((node, index) => {
    if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
    const id = credentialIdentity(node.leaf.credential)
    if (id) leaves.set(id, [...(leaves.get(id) ?? []), node.leaf.signaturePublicKey])
  })
  const verifiers = new Map<string, CryptoKey>()
  for (const [id, keys] of leaves) {
    const only = keys.length === 1 ? keys[0] : undefined
    if (!only) continue
    const key = await importFrameVerifier(only)
    if (!key) return null
    verifiers.set(id, key)
  }
  return verifiers
}

/**
 * The signature key of a member's leaf. Unlike the leaf's HPKE key it stays the
 * same when that member commits, so it tells a fresh client (a reload) apart
 * from a member repeating the package it joined or founded the group with.
 */
function treeLeafSigner(state: ClientState, id: string): Uint8Array | null {
  const index = treeMembers(state).get(id)
  const node = index === undefined ? undefined : state.ratchetTree[index * 2]
  return node && node.nodeType === 'leaf' ? node.leaf.signaturePublicKey : null
}

/**
 * No leaf of `before` is gone or swapped in `after` (same position, same
 * signature key): the commit removed nobody. A leaf replaced by a new one of
 * the same user id is a removal.
 */
export function keepsEveryLeaf(before: ClientState, after: ClientState): boolean {
  return before.ratchetTree.every((node, index) => {
    if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return true
    const next = after.ratchetTree[index]
    return !!next && next.nodeType === 'leaf' && sameBytes(node.leaf.signaturePublicKey, next.leaf.signaturePublicKey)
  })
}

function lowest(ids: Iterable<string>): string | null {
  let best: string | null = null
  for (const id of ids) if (best === null || id < best) best = id
  return best
}

function encodeWire(message: Parameters<typeof encodeMlsMessage>[0]): Uint8Array | null {
  try {
    const bytes = encodeMlsMessage(message)
    return bytes.byteLength > 0 && bytes.byteLength <= HANDSHAKE_MAX_BYTES ? bytes : null
  } catch {
    return null
  }
}

/**
 * One browser's membership in one voice channel.
 * Whoever finds the room empty founds the group; everyone who arrives later
 * offers a key package and is let in. The lowest user id in the group (and
 * still in the room) publishes the commits, so a newcomer with a lower id
 * takes that over once it is in, and the group itself goes on: nobody's media
 * stops because someone joined. A reload is a new package for the same id: the
 * committer, or the next one if it was the committer who reloaded, swaps the
 * old leaf. A member who leaves is removed by whoever remains.
 */
export class MediaGroup {
  private state: ClientState | null = null
  private readonly pending = new Map<string, KeyPackage>()
  /** The epoch before the current one: it opens frames only until `until` (PREVIOUS_EPOCH_MS after the switch). */
  private previousEpoch: { epoch: number, key: EpochKeys, until: number } | null = null
  private current: { epoch: number, key: EpochKeys, verification: MediaVerification, soft: boolean } | null = null
  private room: readonly string[] = []
  private offeredFor: string | null = null
  /** This client created its group, and told this room about it. */
  /** Members whose fresh package says they reloaded: the next committer swaps them in. */
  private readonly restarting = new Set<string>()
  /** The room my group of one last announced itself to. */
  private announcedFor: string | null = null
  /** Key-package wires seen from each member, newest last, to ignore a repeat or an old one. */
  private readonly accepted = new Map<string, Uint8Array[]>()
  /**
   * Key-package wires the relay delivered from each member, newest last. A
   * commit may add an identity only with a package listed here. Unlike
   * `accepted` it survives a discarded group and is pruned to the room.
   */
  private readonly relayed = new Map<string, Uint8Array[]>()
  /** A welcome from a lower id outside my group may replace it this rarely per sender. */
  /** The roster as last announced, ahead of what the queue has handled. */
  private hinted = new Set<string>()
  private readonly rivals = new Backoff(RIVAL_BASE_MS, RIVAL_MAX_MS)
  private closed = false
  private readonly swaps = new Backoff(SWAP_BASE_MS, BACKOFF_MAX_MS)
  private readonly resets = new Backoff(RESET_BASE_MS, BACKOFF_MAX_MS)
  private notice: (notice: MediaNotice) => void = () => {}
  private stepListener: (step: MediaStep) => void = () => {}
  private deferTimer: ReturnType<typeof setTimeout> | null = null
  private offerTimer: ReturnType<typeof setTimeout> | null = null
  private takeoverTimer: ReturnType<typeof setTimeout> | null = null
  private lastRoster: readonly string[] = []
  private readonly heard = new Set<string>()
  private readonly firstSeen = new Map<string, number>()
  private tail: Promise<void> = Promise.resolve()
  /** When each member of the group last showed a sign of life (any handshake, or a heartbeat). */
  private readonly alive = new Map<string, number>()
  /** Newest heartbeat time accepted from each member (and the leaf key that signed it), so an older copy counts for nothing. */
  private readonly beats = new Map<string, { at: number, signer: Uint8Array }>()
  /** Since when this browser noticed that it slept, if it did. */
  private sleptAt: number | null = null
  private lastTickWall = 0
  private epochAt = 0
  private rotateAfter = 0
  private lastBeat = 0
  private lastBeatAt = 0
  private lastTick = 0
  private tickTimer: ReturnType<typeof setTimeout> | null = null
  /** Each member's leaf (its HPKE key) and since when this client has seen that very leaf. */
  private readonly leaves = new Map<string, { key: Uint8Array, at: number }>()
  /** When this member's own leaf is due to be replaced by a self-update. */
  private updateAfter = 0
  /** When the last handshake (other than a heartbeat) and the last roster change were seen. */
  private lastHandshakeAt = 0
  private lastRosterAt = 0
  /** When each member's last self-update was taken, so none may force an epoch more than twice per period. */
  private readonly updates = new Map<string, number>()
  /**
   * The group as it was right before a self-update (a commit with nothing in
   * it but its sender's new leaf) took effect, for CONFLICT_MS. Self-updates
   * come from members other than the committer, so one can race the
   * committer's commit or another update for the same epoch; the committer's
   * wins, and between updates the lower id does. A member that took the loser
   * goes back to this state and takes the winner.
   */
  private selfUpdate: {
    state: ClientState
    current: MediaGroup['current']
    previousEpoch: MediaGroup['previousEpoch']
    sender: string
    until: number
    held: Uint8Array[]
  } | null = null

  private constructor(
    private readonly userId: string,
    private readonly groupId: Uint8Array,
    private own: OwnPackage,
    private readonly cs: CiphersuiteImpl,
    private readonly credential: Credential,
    private readonly timing: MediaTiming,
  ) {}

  static async open(userId: string, channelId: string, timing: MediaTiming = mediaTiming()): Promise<MediaGroup | null> {
    const id = mediaUserId(userId)
    const groupId = mediaGroupId(channelId)
    if (!id || !groupId) return null
    const cs = await suite()
    const credential: Credential = { credentialType: 'basic', identity: new TextEncoder().encode(id) }
    const own = await generateKeyPackage(credential, defaultCapabilities(), defaultLifetime, [], cs)
    return new MediaGroup(id, groupId, own, cs, credential, timing)
  }

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

  sync(roster: readonly string[], resend = false): Promise<MediaStep> {
    this.hint(roster)
    return this.enqueue(() => this.syncNow(roster, resend))
  }

  /**
   * With batch, a key package only waits for the next sync instead of being
   * committed at once: the call syncs after the handshakes that arrived
   * together, so members who joined together are added in one commit.
   */
  receive(fromUserId: string, wire: Uint8Array, batch = false): Promise<MediaStep> {
    return this.enqueue(() => this.receiveNow(fromUserId, wire, batch))
  }

  close(): Promise<void> {
    return this.enqueue(async () => {
      this.closed = true
      for (const timer of [this.deferTimer, this.offerTimer, this.takeoverTimer, this.tickTimer]) if (timer !== null) clearTimeout(timer)
      this.deferTimer = this.offerTimer = this.takeoverTimer = this.tickTimer = null
      this.discard()
      wipeBytes(this.own.privatePackage.initPrivateKey)
      wipeBytes(this.own.privatePackage.hpkePrivateKey)
      wipeBytes(this.own.privatePackage.signaturePrivateKey)
    })
  }

  private enqueue<T>(job: () => Promise<T>): Promise<T> {
    const run = this.tail.then(job)
    this.tail = run.then(() => undefined, () => undefined)
    return run
  }

  private step(wires: Uint8Array[] = []): MediaStep {
    const step: MediaStep = { wires, epoch: this.current?.epoch ?? null, key: this.current?.key ?? null, verification: this.current?.verification ?? null }
    if (this.current?.soft) step.soft = true
    return step
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

  private others(roster: readonly string[]): string[] {
    const ids = new Set<string>()
    for (const value of roster) {
      const id = mediaUserId(value)
      if (id && id !== this.userId) ids.add(id)
    }
    return [...ids]
  }

  /** Lowest id that is both in the tree and still in the room. */
  private committer(others: readonly string[]): string | null {
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

  private canCommit(others: readonly string[]): boolean {
    return this.committer(others) === this.userId
  }

  /**
   * Leaves out lower ids the room lists but that have never been heard from
   * after TAKEOVER_MS, so one client that cannot speak MLS does not wedge the
   * room. They count again as soon as they say anything.
   */
  private reachable(others: string[]): string[] {
    const now = clock.now()
    const tree = this.state ? treeMembers(this.state) : null
    for (const id of [...this.firstSeen.keys()]) if (!others.includes(id)) this.firstSeen.delete(id)
    let wait = Infinity
    const kept = others.filter((id) => {
      if (id > this.userId || tree?.has(id) || this.heard.has(id)) return true
      const since = this.firstSeen.get(id) ?? now
      this.firstSeen.set(id, since)
      const left = TAKEOVER_MS - (now - since)
      if (left > 0) wait = Math.min(wait, left)
      return left > 0
    })
    if (this.takeoverTimer !== null) clearTimeout(this.takeoverTimer)
    this.takeoverTimer = null
    if (wait !== Infinity) {
      this.takeoverTimer = setTimeout(() => {
        this.takeoverTimer = null
        void this.enqueue(async () => (this.closed ? null : this.syncNow(this.lastRoster, false))).then(step => step && this.stepListener(step))
      }, wait + 50)
    }
    return kept
  }

  private defer(action: () => Promise<MediaStep | null>) {
    if (this.deferTimer !== null) return
    this.deferTimer = setTimeout(() => {
      this.deferTimer = null
      void this.enqueue(async () => (this.closed ? null : action())).then(step => step && this.stepListener(step))
    }, this.resets.waitMs('self') + 50)
  }

  /** Offers a fresh package again, if still without a group and the reset limit allows. */
  private async reofferNow(): Promise<MediaStep | null> {
    if (this.closed || this.state) return null
    // Nobody let me in although I am the lowest: nobody here holds a group.
    if (!this.room.some(id => id < this.userId)) return this.foundNow()
    if (!this.resets.allow('self')) {
      this.defer(() => this.reofferNow())
      return null
    }
    await this.rekey()
    return { wires: this.offer(this.room), epoch: null, key: null, verification: null }
  }

  private watchOffer(wait = this.room.some(id => id < this.userId) ? OFFER_WAIT_MS : FOUND_WAIT_MS) {
    if (this.offerTimer !== null) clearTimeout(this.offerTimer)
    this.offerTimer = setTimeout(() => {
      this.offerTimer = null
      void this.enqueue(() => this.reofferNow()).then(step => step && this.stepListener(step))
    }, wait)
  }

  private async syncNow(roster: readonly string[], resend: boolean): Promise<MediaStep> {
    if (this.closed) return this.step()
    const wires: Uint8Array[] = []
    this.lastRoster = roster
    const others = this.reachable(this.others(roster))
    if (others.length !== this.room.length || others.some(id => !this.room.includes(id))) this.lastRosterAt = clock.now()
    this.room = others
    for (const id of [...this.relayed.keys()]) if (!others.includes(id)) this.relayed.delete(id)
    const state = this.state
    // A group is never given up because a lower id arrives: the newcomer
    // offers its package and is let in. Only a group of one, which nobody
    // else depends on, steps aside for a lower id, so that two members who
    // each found an empty room end up in one group.
    const lead = lowest(others)
    if (state && lead !== null && lead < this.userId && treeMembers(state).size === 1) {
      await this.leave()
    }
    if (!this.state && (others.length === 0 || this.allOffered(others))) {
      const created = await this.createOwn()
      if (created) wires.push(...created)
    } else if (!this.state) {
      // Asking again with the same package would change nothing: the
      // committer ignores a package it has seen.
      if (resend && this.offeredFor !== null) await this.rekey()
      wires.push(...this.offer(others))
    }
    // A group of one tells the room about itself: I may be someone the others
    // still hold an old leaf of (a reload that found the room empty for a
    // moment). They swap me in, and I take that welcome.
    if (this.state && treeMembers(this.state).size === 1 && others.length) {
      const key = others.slice().sort().join(',')
      const hello = this.announcedFor === key ? null : this.hello()
      if (hello) {
        this.announcedFor = key
        wires.push(hello)
      }
    }
    if (this.canCommit(others)) {
      wires.push(...await this.commitRemoves(others))
      wires.push(...await this.commitAdds(others))
    }
    return this.step(wires)
  }

  /**
   * The group is out of step (a commit was missed): start over as a joiner
   * with a fresh key package, so the committer replaces my old leaf.
   */
  recover(roster: readonly string[]): Promise<MediaStep> {
    this.hint(roster)
    return this.enqueue(async () => {
      if (this.closed) return this.step()
      this.room = this.others(roster)
      return this.recoverNow()
    })
  }

  private async recoverNow(): Promise<MediaStep> {
    if (!this.room.some(id => id < this.userId) || !this.resets.allow('self')) return this.step()
    await this.leave()
    return { wires: this.offer(this.room), epoch: null, key: null, verification: null }
  }

  /** Drops the group and the package it was joined with. */
  private async leave() {
    this.discard()
    await this.rekey()
  }

  private async rekey() {
    const old = this.own
    // Jobs run one at a time, so the group cannot be closed meanwhile.
    this.own = await generateKeyPackage(this.credential, defaultCapabilities(), defaultLifetime, [], this.cs)
    this.offeredFor = null
    wipeBytes(old.privatePackage.initPrivateKey)
    wipeBytes(old.privatePackage.hpkePrivateKey)
    wipeBytes(old.privatePackage.signaturePrivateKey)
  }

  private hello(): Uint8Array | null {
    return encodeWire({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: this.own.publicPackage })
  }

  /** The key package for the room, unless this room already has it. */
  private offer(others: readonly string[]): Uint8Array[] {
    const key = others.slice().sort().join(',')
    if (this.offeredFor === key) return []
    const wire = this.hello()
    if (!wire) return []
    this.offeredFor = key
    this.watchOffer()
    return [wire]
  }

  private async createOwn(): Promise<Uint8Array[] | null> {
    const state = await createGroup(this.groupId, this.own.publicPackage, this.own.privatePackage, [], this.cs, clientConfig())
    if (!await this.adopt(state)) return null
    this.offeredFor = null
    return []
  }

  /**
   * Everyone else here offered a package and nobody is lower: nobody holds a
   * group, so the lowest of us founds one at once instead of waiting.
   */
  private allOffered(others: readonly string[]): boolean {
    return !others.some(id => id < this.userId) && others.every(id => this.pending.has(id))
  }

  private async foundNow(): Promise<MediaStep | null> {
    const created = await this.createOwn()
    if (!created) return null
    return this.step([...created, ...await this.commitAdds(this.room)])
  }

  private discard() {
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
    this.leaves.clear()
    this.updates.clear()
    this.dropSelfUpdate()
    if (this.tickTimer !== null) clearTimeout(this.tickTimer)
    this.tickTimer = null
    if (state) this.wipeRejected(state)
  }

  /** Forgets the state before a self-update once the time for a conflict has passed (or at once with `now`). */
  private dropSelfUpdate(expiredOnly = false) {
    const saved = this.selfUpdate
    if (!saved || (expiredOnly && clock.now() < saved.until)) return
    this.selfUpdate = null
    wipeConsumed(saved.held)
    this.wipeRejected(saved.state)
  }

  private async receiveNow(fromUserId: string, wire: Uint8Array, batch = false): Promise<MediaStep> {
    if (this.closed || wire.byteLength === 0 || wire.byteLength > HANDSHAKE_MAX_BYTES) return this.step()
    const from = mediaUserId(fromUserId)
    if (!from || from === this.userId) return this.step()
    this.dropSelfUpdate(true)
    if (isHeartbeat(wire)) return this.beatNow(from, wire)
    const decoded = decodeMlsMessage(wire, 0)
    if (!decoded) return this.step()
    const [message] = decoded
    this.heard.add(from)
    this.lastHandshakeAt = clock.now()
    const members = this.state ? treeMembers(this.state) : null
    const wires: Uint8Array[] = []
    if (message.wireformat === 'mls_key_package') {
      const id = credentialIdentity(message.keyPackage.leafNode.credential)
      if (id !== from) return this.step()
      this.relay(from, message.keyPackage)
      const known = this.state ? treeLeafSigner(this.state, from) : null
      const same = known !== null && sameBytes(known, message.keyPackage.leafNode.signaturePublicKey)
      if (members?.has(from)) {
        // The same package again is a duplicate. Another one is the member
        // coming back (a reload): swap the old leaf for the new one in a single
        // commit. If it was the committer, the next one does it. Only a member
        // the roster says is still here, and not again and again.
        if (same || !this.room.includes(from) || this.seen(from, wire)) return this.step()
        if (from === this.committer(this.room)) this.restarting.add(from)
        if (!this.canCommit(this.room) || !this.swaps.allow(from)) return this.step()
        this.remember(from, wire)
        wires.push(...await this.replaceMember(from, message.keyPackage))
        return this.step(wires)
      }
      this.remember(from, wire)
      this.pending.set(from, message.keyPackage)
      // A package from someone the roster does not list waits until it does.
      if (!batch && this.room.includes(from) && this.canCommit(this.room)) wires.push(...await this.commitAdds([from]))
      return this.step(wires)
    }
    if (message.wireformat === 'mls_welcome') {
      // Without a group, or with one of my own that nobody joined yet (I
      // founded while this welcome was on its way): the group that lets me in
      // is the one everybody else is in. A group others are in is given up
      // only for a rival that proves itself below: a welcome from a lower id
      // outside my group that opens with my own key package and passes the
      // committer rule. Nothing unauthenticated (a message that merely parses)
      // ever discards a group.
      const previous = this.state
      const rival = previous !== null && members !== null && from < this.userId && !members.has(from)
      if (previous && treeMembers(previous).size > 1 && !rival) return this.step()
      // Whoever commits for the group I offered to lets me in.
      if (!this.room.includes(from)) return this.step()
      if (rival && !this.rivals.allow(from)) return this.step()
      try {
        const joined = await joinGroup(message.welcome, this.own.publicPackage, this.own.privatePackage, emptyPskIndex, this.cs)
        // Only the group's committer welcomes: the lowest of its members the
        // roster lists, leaving me out (I may be the committer coming back).
        const committer = lowest([...treeMembers(joined).keys()].filter(id => id !== this.userId && this.room.includes(id)))
        if (committer !== from) {
          this.wipeRejected(joined)
          return this.step()
        }
        if (rival) this.discard()
        if (await this.adopt(joined)) {
          this.offeredFor = null
          if (previous) this.wipeRejected(previous)
        }
      } catch {
        return this.step()
      }
      return this.step()
    }
    if (message.wireformat === 'mls_public_message' && this.state) {
      const previous = this.state
      const framed = message.publicMessage.content
      // A commit for an epoch I have not reached means I missed one: nothing
      // that follows can be processed, so start over instead of waiting.
      // It counts only from a member of this group, for this group, and ahead.
      // Its signature cannot be checked (the epoch is not mine), so it counts
      // only from a member who may be committing: it names that member's own
      // leaf and the member is among the two lowest live ones.
      if (framed.contentType === 'commit' && framed.sender.senderType === 'member'
        && sameBytes(framed.groupId, this.groupId) && framed.epoch > previous.groupContext.epoch
        && members?.get(from) === framed.sender.leafIndex && this.mayCommit(from)) {
        return this.recoverNow()
      }
      const saved = this.selfUpdate
      if (saved && framed.contentType === 'commit' && framed.sender.senderType === 'member'
        && sameBytes(framed.groupId, this.groupId) && framed.epoch === saved.state.groupContext.epoch && from !== saved.sender) {
        return this.conflictNow(saved, message.publicMessage, from)
      }
      const isUpdate = framed.contentType === 'commit' && framed.commit.proposals.length === 0 && this.committer(this.listed()) !== from
      try {
        const processed = await processPublicMessage(previous, message.publicMessage, emptyPskIndex, this.cs, this.validator(previous, from))
        // Rejected by the rules above (or a proposal, which this group never
        // uses): the sender is not trusted with it, and nothing is thrown away.
        if (processed.actionTaken === 'reject') return this.step()
        // Only a commit that verified against this group's tree shows its member alive.
        this.alive.set(from, clock.now())
        if (isUpdate) {
          this.updates.set(from, clock.now())
          return this.adoptUpdate(previous, processed.newState, processed.consumed, from)
        }
        wipeConsumed(processed.consumed)
        // A commit that removes a member who is still here was not asked for:
        // say so, and offer again, but not in a loop.
        if (!treeMembers(processed.newState).has(this.userId)) {
          this.wipeRejected(processed.newState)
          const absent = this.sleptRecently()
          this.discard()
          this.notice(absent ? 'absent' : 'removed')
          if (!this.resets.allow('self')) {
            this.defer(() => this.reofferNow())
            return this.step()
          }
          await this.rekey()
          return { wires: this.offer(this.room), epoch: null, key: null, verification: null }
        }
        if (!await this.adopt(processed.newState, keepsEveryLeaf(previous, processed.newState) ? 'soft' : 'commit')) {
          // The epoch was refused (for instance a leaf identity held twice): this group is out of step.
          return this.recoverNow()
        }
      } catch {
        return this.step()
      }
    }
    return this.step()
  }

  private mayUpdate(id: string): boolean {
    const last = this.updates.get(id)
    return last === undefined || clock.now() - last >= Math.max(0, this.timing.rotateMs - this.timing.rotateJitterMs) / 2
  }

  /** The roster as last announced. */
  private listed(): string[] {
    return [...this.hinted]
  }

  /**
   * Takes another member's self-update, keeping the state before it for
   * CONFLICT_MS in case the committer's commit for the same epoch wins.
   */
  private async adoptUpdate(previous: ClientState, next: ClientState, consumed: Uint8Array[], sender: string): Promise<MediaStep> {
    const current = this.current
    const previousEpoch = this.previousEpoch
    this.dropSelfUpdate()
    if (!await this.adopt(next, 'soft')) {
      wipeConsumed(consumed)
      return this.recoverNow()
    }
    this.selfUpdate = { state: previous, current, previousEpoch, sender, until: clock.now() + CONFLICT_MS, held: consumed }
    return this.step()
  }

  /**
   * A commit for the epoch I left with a self-update arrived. The committer's
   * commit beats an update, and of two updates the lower id's does; the loser
   * goes back to the state before the update and takes the winner, so a race
   * between two members does not split the call. A commit that does not win,
   * or does not verify against that state, changes nothing.
   */
  private async conflictNow(saved: NonNullable<MediaGroup['selfUpdate']>, message: PublicMessage, from: string): Promise<MediaStep> {
    try {
      const processed = await processPublicMessage(saved.state, message, emptyPskIndex, this.cs, this.validator(saved.state, from))
      const content = message.content
      const empty = content.contentType === 'commit' && content.commit.proposals.length === 0
      const wins = processed.actionTaken !== 'reject' && (!empty || this.committer(this.listed()) === from || from < saved.sender)
      if (!wins) {
        wipeConsumed(processed.consumed)
        if (processed.newState !== saved.state) this.wipeRejected(processed.newState)
        return this.step()
      }
      const mine = this.state as ClientState
      const mineCurrent = this.current
      const minePrevious = this.previousEpoch
      this.selfUpdate = null
      this.state = saved.state
      this.current = saved.current
      this.previousEpoch = saved.previousEpoch
      const adopted = await this.adopt(processed.newState, keepsEveryLeaf(saved.state, processed.newState) ? 'soft' : 'commit')
      wipeConsumed(processed.consumed)
      wipeConsumed(saved.held)
      if (!adopted) {
        this.state = mine
        this.current = mineCurrent
        this.previousEpoch = minePrevious
        return this.recoverNow()
      }
      this.alive.set(from, clock.now())
      this.wipeRejected(mine)
      return this.step()
    } catch {
      return this.step()
    }
  }

  private relay(from: string, keyPackage: KeyPackage) {
    const wire = encodeWire({ version: 'mls10', wireformat: 'mls_key_package', keyPackage })
    if (!wire) return
    const list = (this.relayed.get(from) ?? []).filter(old => !sameBytes(old, wire))
    list.push(wire)
    this.relayed.set(from, list.slice(-8))
  }

  private wasRelayed(id: string, keyPackage: KeyPackage): boolean {
    const wire = encodeWire({ version: 'mls10', wireformat: 'mls_key_package', keyPackage })
    return wire !== null && (this.relayed.get(id) ?? []).some(old => sameBytes(old, wire))
  }

  /** The two lowest members of the group the room lists (zombies included): whose commit a missed one may be blamed on. */
  private mayCommit(id: string): boolean {
    const room = new Set(this.room)
    room.add(this.userId)
    const live = [...treeMembers(this.state as ClientState).keys()].filter(member => room.has(member)).sort()
    return live.slice(0, 2).includes(id)
  }

  /**
   * What a commit may do to this group, checked after its signature and
   * membership tag verified against the tree but before it takes effect:
   * - it comes from the member who commits now (the lowest live one), or has
   *   nothing in it but the sender's own new leaf, which any member may send
   *   at most twice per rotation period;
   * - it only adds or removes;
   * - it adds only an identity in the room whose key package the relay
   *   delivered, and never one that already holds a leaf, unless the same
   *   commit removes that old leaf (a member who came back);
   * - it removes only a member the roster no longer lists, one silent for
   *   about the absence limit, or one it replaces. A browser that slept cannot
   *   judge silence and takes a removal on the committer's word, as does the
   *   member removed itself.
   * Anything else is ignored, like input from an untrusted rival.
   */
  private validator(state: ClientState, from: string): IncomingMessageCallback {
    return (incoming) => {
      if (incoming.kind !== 'commit') return 'reject'
      const members = treeMembers(state)
      const senderLeaf = members.get(from)
      if (senderLeaf === undefined || incoming.senderLeafIndex !== senderLeaf) return 'reject'
      const proposals = incoming.proposals
      if (this.committer(this.listed()) !== from) {
        // A member renewing its own leaf: a commit with nothing in it, not
        // more than twice per rotation period.
        return proposals.length === 0 && this.mayUpdate(from) ? 'accept' : 'reject'
      }
      const identityOf = new Map<number, string>()
      for (const [id, leaf] of members) identityOf.set(leaf, id)
      // Newest roster: who is listed now. Adds also accept the last synced room.
      const room = new Set(this.hinted)
      room.add(this.userId)
      const roomOrLast = new Set([...room, ...this.room])
      const now = clock.now()
      const slack = Math.max(10, this.timing.maintenanceMs)
      const slept = this.sleptRecently()
      const removed = new Set<string>()
      const added = new Set<string>()
      for (const { proposal } of proposals) {
        if (proposal.proposalType === 'remove') {
          const id = identityOf.get(proposal.remove.removed)
          if (id === undefined) return 'reject'
          removed.add(id)
        } else if (proposal.proposalType === 'add') {
          const id = credentialIdentity(proposal.add.keyPackage.leafNode.credential)
          if (id === null || id === from || added.has(id) || !roomOrLast.has(id) || !this.wasRelayed(id, proposal.add.keyPackage)) return 'reject'
          added.add(id)
        } else {
          return 'reject'
        }
      }
      for (const id of added) if (members.has(id) && !removed.has(id)) return 'reject'
      for (const id of removed) {
        // My own removal is the committer's word about me (my link dropped, I
        // slept): I cannot judge it, and taking it means a notice and a rejoin.
        if (added.has(id) || slept || id === this.userId || !room.has(id)) continue
        if (now - (this.alive.get(id) as number) <= this.timing.absentMs - slack) return 'reject'
      }
      return 'accept'
    }
  }

  /** A new epoch begins: its age starts, and so does the delay before it is rotated. */
  private startEpoch(state: ClientState) {
    const now = clock.now()
    this.epochAt = now
    const { rotateMs, rotateJitterMs } = this.timing
    this.rotateAfter = Math.max(0, rotateMs + (Math.random() * 2 - 1) * rotateJitterMs)
    const members = treeMembers(state)
    for (const id of [...this.leaves.keys()]) if (!members.has(id)) this.leaves.delete(id)
    for (const id of [...this.updates.keys()]) if (!members.has(id)) this.updates.delete(id)
    for (const [id, index] of members) {
      const key = (state.ratchetTree[index * 2] as { leaf: { hpkePublicKey: Uint8Array } }).leaf.hpkePublicKey
      const known = this.leaves.get(id)
      if (known && sameBytes(known.key, key)) continue
      this.leaves.set(id, { key: new Uint8Array(key), at: now })
      // My own leaf is due again after a period of its own, jittered like an epoch's.
      if (id === this.userId) this.updateAfter = Math.max(0, rotateMs + (Math.random() * 2 - 1) * rotateJitterMs)
    }
    for (const id of [...this.alive.keys()]) if (!members.has(id)) this.alive.delete(id)
    for (const id of [...this.beats.keys()]) if (!members.has(id)) this.beats.delete(id)
    // Whoever is new to the group starts with a clean slate: it was let in just now.
    for (const id of members.keys()) if (!this.alive.has(id)) this.alive.set(id, now)
    if (this.tickTimer === null) {
      this.lastTick = now
      this.lastTickWall = clock.wall()
      this.lastBeat = now
      this.scheduleTick()
    }
  }

  private scheduleTick() {
    this.tickTimer = setTimeout(() => {
      this.tickTimer = null
      void this.enqueue(async () => (this.closed || !this.state ? null : this.tickNow())).then((step) => {
        if (step && step.wires.length) this.stepListener(step)
      })
      if (!this.closed && this.state) this.scheduleTick()
    }, Math.max(10, this.timing.maintenanceMs))
  }

  /**
   * Looks at the clocks: sends this member's heartbeat when it is due and, if
   * this member is the committer, removes members that showed no sign of life
   * for ABSENT_MS and advances an epoch that has reached its age. Both are
   * decided by the epoch's age and the members' last sign of life, never by a
   * timer that restarts when the committer changes.
   */
  maintain(): Promise<MediaStep> {
    return this.enqueue(async () => (this.closed || !this.state ? this.step() : this.tickNow()))
  }

  private async tickNow(): Promise<MediaStep> {
    const wires: Uint8Array[] = []
    // Callers hold a group: a state and the keys of its epoch go together.
    const state = this.state as ClientState
    const current = this.current as NonNullable<typeof this.current>
    const now = clock.now()
    // This browser slept (a frozen tab, a closed lid): it heard nothing, so it
    // cannot tell who is absent. Everyone starts over, and says it is here.
    const slept = this.sleepGap(now) > this.sleepLimit()
    this.lastTick = now
    this.lastTickWall = clock.wall()
    if (slept) {
      this.sleptAt = now
      for (const id of this.alive.keys()) this.alive.set(id, now)
    }
    const members = treeMembers(state)
    if (members.size > 1 && (slept || now - this.lastBeat >= this.timing.heartbeatMs)) {
      this.lastBeat = now
      this.lastBeatAt = Math.max(now, this.lastBeatAt + 1)
      const beat = await sealHeartbeat(current.key.signer, this.groupId, this.userId, current.epoch, this.lastBeatAt)
      if (beat) wires.push(beat)
    }
    // Right after waking up this view is the stalest it will ever be: the
    // messages that arrived meanwhile are still queued behind this tick. The
    // next tick, one maintenance interval later, decides.
    if (!slept && !this.canCommit(this.room) && members.size > 1 && this.renewalDue(now, members)) {
      wires.push(...await this.commit([], true))
    }
    if (!slept && this.canCommit(this.room)) {
      const absent: Proposal[] = []
      for (const [id, leaf] of members) {
        if (id !== this.userId && now - (this.alive.get(id) ?? now) > this.timing.absentMs) {
          absent.push({ proposalType: 'remove', remove: { removed: leaf } })
        }
      }
      if (absent.length) wires.push(...await this.commit(absent))
      else if (members.size > 1 && (now - this.epochAt >= this.rotateAfter || this.leafAge(now) >= this.updateAfter)) wires.push(...await this.commit([]))
    }
    return this.step(wires)
  }

  private leafAge(now: number): number {
    return now - (this.leaves.get(this.userId) as { at: number }).at
  }

  /**
   * A member that is not the committer renews its own leaf (post-compromise
   * security for it) once the leaf is as old as its period, with a commit
   * that holds nothing else. To keep two commits from meeting, it waits for
   * a quiet moment: no handshake or roster change for a while, the epoch a
   * while old, and, behind every other member that is due before it (by id),
   * one more wait each. A leaf long overdue stops waiting for quiet.
   */
  private renewalDue(now: number, members: Map<string, number>): boolean {
    const age = this.leafAge(now)
    if (age < this.updateAfter) return false
    const settle = Math.max(10, this.timing.maintenanceMs)
    if (age >= this.updateAfter + Math.max(4 * settle, this.timing.rotateMs / 10)) return true
    if (now - this.lastHandshakeAt < settle || now - this.lastRosterAt < settle) return false
    const due = Math.max(0, this.timing.rotateMs - this.timing.rotateJitterMs)
    const committer = this.committer(this.room)
    let ahead = 0
    for (const id of members.keys()) {
      if (id >= this.userId || id === committer) continue
      if (now - (this.leaves.get(id) as { at: number }).at >= due) ahead++
    }
    return now - this.epochAt >= settle * (1 + ahead)
  }

  /** How long this browser was away since the last tick: the larger of what the monotonic and the wall clock saw (a monotonic clock may stand still in sleep). */
  private sleepGap(now: number): number {
    return Math.max(now - this.lastTick, clock.wall() - this.lastTickWall)
  }

  private sleepLimit(): number {
    return 2 * Math.max(10, this.timing.maintenanceMs) + 60_000
  }

  /** This browser was asleep just now: a removal that follows is for its silence, not a hostile relay. */
  private sleptRecently(): boolean {
    const now = clock.now()
    if (this.sleepGap(now) > this.sleepLimit()) return true
    return this.sleptAt !== null && now - this.sleptAt <= this.sleepLimit()
  }

  /** A heartbeat counts when this epoch's member signed it, and it is newer than the last. */
  private async beatNow(from: string, wire: Uint8Array): Promise<MediaStep> {
    const current = this.current
    const verifier = current?.key.verifiers.get(from)
    if (!current || !verifier) return this.step()
    const beat = await openHeartbeat(verifier, this.groupId, from, wire)
    if (!beat || beat.epoch !== current.epoch) return this.step()
    const last = this.beats.get(from)
    // A verifier exists only for a member with exactly one leaf.
    const signer = treeLeafSigner(this.state as ClientState, from) as Uint8Array
    // A reloaded member signs with a new leaf key and starts its clock anew.
    if (last !== undefined && sameBytes(last.signer, signer) && beat.at <= last.at) return this.step()
    this.beats.set(from, { at: beat.at, signer: new Uint8Array(signer) })
    this.alive.set(from, clock.now())
    return this.step()
  }

  private seen(from: string, wire: Uint8Array): boolean {
    return (this.accepted.get(from) ?? []).some(old => sameBytes(old, wire))
  }

  private remember(from: string, wire: Uint8Array) {
    const history = (this.accepted.get(from) ?? []).filter(old => !sameBytes(old, wire))
    history.push(new Uint8Array(wire))
    this.accepted.set(from, history.slice(-8))
  }

  /** One commit that swaps a member's old leaf for the new package. */
  private async replaceMember(id: string, keyPackage: KeyPackage): Promise<Uint8Array[]> {
    const state = this.state as ClientState
    const present = new Set(this.room)
    present.add(this.userId)
    present.delete(id)
    const proposals: Proposal[] = []
    for (const [member, leaf] of treeMembers(state)) {
      if (!present.has(member)) proposals.push({ proposalType: 'remove', remove: { removed: leaf } })
    }
    proposals.push({ proposalType: 'add', add: { keyPackage } })
    // The reloaded member's heartbeats start over with its new leaf.
    this.beats.delete(id)
    return this.commit(proposals)
  }

  private async commitRemoves(others: readonly string[]): Promise<Uint8Array[]> {
    if (!this.state) return []
    const present = new Set(others)
    present.add(this.userId)
    const proposals = []
    for (const [id, leaf] of treeMembers(this.state)) {
      if (!present.has(id)) proposals.push({ proposalType: 'remove' as const, remove: { removed: leaf } })
    }
    if (!proposals.length) return []
    return this.commit(proposals)
  }

  private async commitAdds(others: readonly string[]): Promise<Uint8Array[]> {
    if (!this.state) return []
    const wanted = new Set(others)
    const members = treeMembers(this.state)
    const proposals = []
    for (const [id, keyPackage] of this.pending) {
      if (!wanted.has(id) || members.has(id)) continue
      proposals.push({ proposalType: 'add' as const, add: { keyPackage } })
    }
    if (!proposals.length) return []
    const wires = await this.commit(proposals)
    if (wires.length) {
      for (const proposal of proposals) {
        const id = credentialIdentity(proposal.add.keyPackage.leafNode.credential)
        if (id) this.pending.delete(id)
      }
    }
    return wires
  }

  private async commit(extraProposals: Proposal[], renewal = false): Promise<Uint8Array[]> {
    if (!this.state) return []
    const previous = this.state
    let result: Awaited<ReturnType<typeof createCommit>>
    try {
      result = await createCommit(
        { state: previous, cipherSuite: this.cs },
        { extraProposals, wireAsPublicMessage: true, ratchetTreeExtension: true },
      )
    } catch {
      return []
    }
    if (!renewal) wipeConsumed(result.consumed)
    const adding = extraProposals.some(proposal => proposal.proposalType === 'add')
    const welcome = result.welcome
      ? encodeWire({ version: 'mls10', wireformat: 'mls_welcome', welcome: result.welcome })
      : null
    const commit = result.commit.wireformat === 'mls_public_message' ? encodeWire(result.commit) : null
    if (!commit || (adding && !welcome)) {
      if (renewal) wipeConsumed(result.consumed)
      this.wipeRejected(result.newState)
      return []
    }
    const soft = !extraProposals.some(proposal => proposal.proposalType === 'remove')
    const current = this.current
    const previousEpoch = this.previousEpoch
    if (renewal) this.dropSelfUpdate()
    if (!await this.adopt(result.newState, soft ? 'soft' : 'commit')) {
      if (renewal) wipeConsumed(result.consumed)
      return []
    }
    // A renewal may race another commit for the same epoch: keep the state it left.
    if (renewal) this.selfUpdate = { state: previous, current, previousEpoch, sender: this.userId, until: clock.now() + CONFLICT_MS, held: result.consumed }
    return welcome ? [welcome, commit] : [commit]
  }

  /** Drops secrets of a state we refuse, without erasing keys the current group still uses. */
  private wipeRejected(state: ClientState) {
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

  /**
   * 'new' is a group of its own or one joined by a welcome. A 'commit' moves the
   * group I am in to its next epoch; a 'soft' one removed nobody.
   */
  private async adopt(state: ClientState, via: 'new' | 'commit' | 'soft' = 'new'): Promise<boolean> {
    if (this.closed || !sameBytes(state.groupContext.groupId, this.groupId)) {
      this.wipeRejected(state)
      return false
    }
    const epoch = state.groupContext.epoch
    if (epoch < 0n || epoch > 0xffffffffn) {
      this.wipeRejected(state)
      return false
    }
    const raw = await mlsExporter(state.keySchedule.exporterSecret, MEDIA_EXPORTER_LABEL, this.groupId, FRAME_EPOCH_BYTES, this.cs)
    try {
      const aes = await importEpochKey(raw)
      const code = await safetyCode(state.keySchedule.epochAuthenticator)
      // Without Ed25519 in this browser there is no signer, so no keys and no media.
      const signer = await importFrameSigner(state.signaturePrivateKey)
      const verifiers = await frameVerifiers(state)
      // An identity that holds two leaves (or a leaf nobody can read) is never
      // a group to key media from: refuse the epoch rather than pick a leaf.
      const leaves = leafIdentities(state)
      if (!aes || !code || !signer || !verifiers || !verifiers.has(this.userId) || leaves.includes('') || new Set(leaves).size !== leaves.length) {
        this.wipeRejected(state)
        return false
      }
      this.state = state
      // A swap has landed (or the group changed): everyone counts again.
      this.restarting.clear()
      if (this.offerTimer !== null) clearTimeout(this.offerTimer)
      this.offerTimer = null
      const epochNumber = Number(epoch)
      const key: EpochKeys = { key: aes, signer, verifiers }
      const before = this.current
      this.current = { epoch: epochNumber, key, soft: via === 'soft', verification: { epoch: epochNumber, code, members: leafIdentities(state).sort() } }
      // Only the epoch right before this one, of this group, stays readable, and briefly.
      this.previousEpoch = via !== 'new' && before && before.epoch < epochNumber
        ? { epoch: before.epoch, key: before.key, until: clock.now() + PREVIOUS_EPOCH_MS }
        : null
      this.startEpoch(state)
      return true
    } finally {
      zeroOutUint8Array(raw)
    }
  }
}
