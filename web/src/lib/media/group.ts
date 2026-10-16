import {
  acceptAll,
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
  type KeyPackage,
  type PrivateKeyPackage,
  type Proposal,
} from 'ts-mls'
import { FRAME_EPOCH_BYTES, HANDSHAKE_MAX_BYTES, MEDIA_EXPORTER_LABEL, mediaGroupId, mediaUserId } from './bounds'
import type { MediaNotice } from './api'
import { Backoff } from './backoff'
import { importEpochKey, importFrameSigner, importFrameVerifier, type EpochKeys } from './frame'
import { safetyCode, type MediaVerification } from './verify'

// A member's leaf is swapped at most once per this long, doubling while it keeps happening.
const SWAP_BASE_MS = 3000
// A group is thrown away (a missed commit, a restarted committer, a removal)
// at most once per this long, doubling while it keeps happening.
const RESET_BASE_MS = 10_000
const BACKOFF_MAX_MS = 60_000
// A joiner whose offer got no welcome in this long offers again.
const OFFER_WAIT_MS = 8000
// A member lower than me that the room lists but never said a word in this
// long cannot be speaking MLS (an old tab, a failed start): the next one acts.
const TAKEOVER_MS = 15_000

export interface MediaStep {
  wires: Uint8Array[]
  epoch: number | null
  key: EpochKeys | null
  /** What members compare to know they hold the same keys; set with the key. */
  verification?: MediaVerification | null
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
 * The lowest user id in the room creates the group and publishes commits.
 * Anyone higher only offers a key package. A group that does not yet contain
 * a lower id is dropped, and that lower id creates the replacement. A member
 * who leaves is removed by whoever remains.
 */
export class MediaGroup {
  private state: ClientState | null = null
  private readonly pending = new Map<string, KeyPackage>()
  private readonly keys = new Map<number, EpochKeys>()
  private current: { epoch: number, key: EpochKeys, verification: MediaVerification } | null = null
  private room: readonly string[] = []
  private offeredFor: string | null = null
  /** This client created its group, and told this room about it. */
  private founded = false
  private announcedFor: string | null = null
  /** Key-package wires seen from each member, newest last, to ignore a repeat or an old one. */
  private readonly accepted = new Map<string, Uint8Array[]>()
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

  private constructor(
    private readonly userId: string,
    private readonly groupId: Uint8Array,
    private own: OwnPackage,
    private readonly cs: CiphersuiteImpl,
    private readonly credential: Credential,
  ) {}

  static async open(userId: string, channelId: string): Promise<MediaGroup | null> {
    const id = mediaUserId(userId)
    const groupId = mediaGroupId(channelId)
    if (!id || !groupId) return null
    const cs = await suite()
    const credential: Credential = { credentialType: 'basic', identity: new TextEncoder().encode(id) }
    const own = await generateKeyPackage(credential, defaultCapabilities(), defaultLifetime, [], cs)
    return new MediaGroup(id, groupId, own, cs, credential)
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

  sync(roster: readonly string[], resend = false): Promise<MediaStep> {
    return this.enqueue(() => this.syncNow(roster, resend))
  }

  receive(fromUserId: string, wire: Uint8Array): Promise<MediaStep> {
    return this.enqueue(() => this.receiveNow(fromUserId, wire))
  }

  close(): Promise<void> {
    return this.enqueue(async () => {
      this.closed = true
      for (const timer of [this.deferTimer, this.offerTimer, this.takeoverTimer]) if (timer !== null) clearTimeout(timer)
      this.deferTimer = this.offerTimer = this.takeoverTimer = null
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
    return { wires, epoch: this.current?.epoch ?? null, key: this.current?.key ?? null, verification: this.current?.verification ?? null }
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
    for (const id of treeMembers(this.state).keys()) if (room.has(id)) active.push(id)
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
    const now = Date.now()
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
    if (this.closed || this.state || !this.room.some(id => id < this.userId)) return null
    if (!this.resets.allow('self')) {
      this.defer(() => this.reofferNow())
      return null
    }
    await this.rekey()
    return { wires: this.offer(this.room), epoch: null, key: null, verification: null }
  }

  private watchOffer() {
    if (this.offerTimer !== null) clearTimeout(this.offerTimer)
    this.offerTimer = setTimeout(() => {
      this.offerTimer = null
      void this.enqueue(() => this.reofferNow()).then(step => step && this.stepListener(step))
    }, OFFER_WAIT_MS)
  }

  private async syncNow(roster: readonly string[], resend: boolean): Promise<MediaStep> {
    if (this.closed) return this.step()
    const wires: Uint8Array[] = []
    this.lastRoster = roster
    const others = this.reachable(this.others(roster))
    this.room = others
    const state = this.state
    // Only the lowest id of the room decides whether this group is the right
    // one; someone in between is just added by the committer.
    const lead = lowest(others)
    if (state && lead !== null && lead < this.userId && !treeMembers(state).has(lead)) {
      await this.leave()
    }
    if (!this.state && !others.some(id => id < this.userId)) {
      const created = await this.createOwn()
      if (created) wires.push(...created)
    } else if (!this.state) {
      // Asking again with the same package would change nothing: the
      // committer ignores a package it has seen.
      if (resend && this.offeredFor !== null) await this.rekey()
      wires.push(...this.offer(others))
    }
    // Anyone already holding a group that has me in it must hear that this one
    // is new: I may be the lowest id coming back after a reload. Said again
    // whenever the room changes, since the roster can lag behind the join.
    if (this.founded && others.length) {
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
    this.founded = true
    this.announcedFor = null
    return []
  }

  private discard() {
    const state = this.state
    this.state = null
    this.current = null
    this.keys.clear()
    this.pending.clear()
    this.accepted.clear()
    this.offeredFor = null
    this.founded = false
    this.announcedFor = null
    if (state) this.wipeRejected(state)
  }

  private async receiveNow(fromUserId: string, wire: Uint8Array): Promise<MediaStep> {
    if (this.closed || wire.byteLength === 0 || wire.byteLength > HANDSHAKE_MAX_BYTES) return this.step()
    const from = mediaUserId(fromUserId)
    if (!from || from === this.userId) return this.step()
    const decoded = decodeMlsMessage(wire, 0)
    if (!decoded) return this.step()
    const [message] = decoded
    this.heard.add(from)
    const members = this.state ? treeMembers(this.state) : null
    const wires: Uint8Array[] = []
    if (message.wireformat === 'mls_key_package') {
      const id = credentialIdentity(message.keyPackage.leafNode.credential)
      if (id !== from) return this.step()
      const known = this.state ? treeLeafSigner(this.state, from) : null
      const same = known !== null && sameBytes(known, message.keyPackage.leafNode.signaturePublicKey)
      // The committer I know with another leaf has started over (a reload):
      // its group is the one that counts now, so I join it again. Any other
      // member coming back is swapped in by the committer.
      const restarted = members?.has(from) === true && !same && from === this.committer(this.room)
      // Only for someone the roster says is in the room, and not in a loop;
      // what the limit holds back is tried again when it lets go.
      if (members && from < this.userId && this.room.includes(from) && restarted) {
        if (!this.resets.allow('self')) {
          this.defer(() => this.receiveNow(fromUserId, wire))
          return this.step()
        }
        await this.leave()
        wires.push(...this.offer(this.room))
      } else if (members?.has(from)) {
        // The same package again is a duplicate. Another one is the member
        // coming back: swap the old leaf for the new one in a single commit.
        // Only a member the roster says is still here, and not again and again.
        if (same || !this.room.includes(from) || !this.canCommit(this.room) || this.seen(from, wire)) return this.step()
        if (!this.swaps.allow(from)) return this.step()
        this.remember(from, wire)
        wires.push(...await this.replaceMember(from, message.keyPackage))
        return this.step(wires)
      }
      this.remember(from, wire)
      this.pending.set(from, message.keyPackage)
      // A package from someone the roster does not list waits until it does.
      if (this.room.includes(from) && this.canCommit(this.room)) wires.push(...await this.commitAdds([from]))
      return this.step(wires)
    }
    if (members && from < this.userId && !members.has(from)) this.discard()
    if (message.wireformat === 'mls_welcome') {
      if (this.state) return this.step()
      // My view of the room may be a step behind the welcome; what matters is
      // that it comes from the lowest id I know of.
      if (from >= this.userId || from !== lowest([...this.room, from])) return this.step()
      try {
        const joined = await joinGroup(message.welcome, this.own.publicPackage, this.own.privatePackage, emptyPskIndex, this.cs)
        if (await this.adopt(joined)) this.offeredFor = null
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
      if (framed.contentType === 'commit' && framed.sender.senderType === 'member'
        && sameBytes(framed.groupId, this.groupId) && framed.epoch > previous.groupContext.epoch
        && members?.has(from) && [...(members?.values() ?? [])].includes(framed.sender.leafIndex)) {
        return this.recoverNow()
      }
      try {
        const processed = await processPublicMessage(previous, message.publicMessage, emptyPskIndex, this.cs, acceptAll)
        wipeConsumed(processed.consumed)
        // A commit that removes a member who is still here was not asked for:
        // say so, and offer again, but not in a loop.
        if (!treeMembers(processed.newState).has(this.userId)) {
          this.wipeRejected(processed.newState)
          this.discard()
          this.notice('removed')
          if (!this.resets.allow('self')) {
            this.defer(() => this.reofferNow())
            return this.step()
          }
          await this.rekey()
          return { wires: this.offer(this.room), epoch: null, key: null, verification: null }
        }
        await this.adopt(processed.newState)
      } catch {
        return this.step()
      }
    }
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

  private async commit(extraProposals: Proposal[]): Promise<Uint8Array[]> {
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
    wipeConsumed(result.consumed)
    const adding = extraProposals.some(proposal => proposal.proposalType === 'add')
    const welcome = result.welcome
      ? encodeWire({ version: 'mls10', wireformat: 'mls_welcome', welcome: result.welcome })
      : null
    const commit = result.commit.wireformat === 'mls_public_message' ? encodeWire(result.commit) : null
    if (!commit || (adding && !welcome)) {
      this.wipeRejected(result.newState)
      return []
    }
    if (!await this.adopt(result.newState)) return []
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

  private async adopt(state: ClientState): Promise<boolean> {
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
      if (!aes || !code || !signer || !verifiers || !verifiers.has(this.userId)) {
        this.wipeRejected(state)
        return false
      }
      this.state = state
      if (this.offerTimer !== null) clearTimeout(this.offerTimer)
      this.offerTimer = null
      const epochNumber = Number(epoch)
      const key: EpochKeys = { key: aes, signer, verifiers }
      this.current = { epoch: epochNumber, key, verification: { epoch: epochNumber, code, members: leafIdentities(state).sort() } }
      this.keys.set(epochNumber, key)
      const ordered = [...this.keys.keys()].sort((a, b) => a - b)
      for (const drop of ordered.slice(0, -2)) this.keys.delete(drop)
      return true
    } finally {
      zeroOutUint8Array(raw)
    }
  }
}
