import {
  decodeMlsMessage,
  defaultCapabilities,
  defaultLifetime,
  emptyPskIndex,
  generateKeyPackage,
  joinGroup,
  processPublicMessage,
  type ClientState,
  type Credential,
  type IncomingMessageCallback,
  type KeyPackage,
  type MLSMessage,
  type PublicMessage,
  type Welcome,
} from 'ts-mls'
import { mediaGroupId, mediaUserId, type MediaTiming } from './bounds'
import { clock } from './clock'
import { mediaTiming } from './lifetime'
import { isHeartbeat, openHeartbeat } from './heartbeat'
import { isCapability, openCapability, sealCapability } from './capability'
import { CONFLICT_MS, type MediaStep } from './groupParams'
import { suite, credentialIdentity, sameBytes, wipeConsumed, treeMembers, treeLeafSigner, onlyRenewsLeaf, keepsEveryLeaf, lowest } from './groupTree'
import { GroupBase } from './groupBase'
import { GroupSync } from './groupSync'

type MlsMessage = MLSMessage
type ProcessedPublicMessage = Awaited<ReturnType<typeof processPublicMessage>>
import {
  validCapBits,
  isKnownSigner,
  invalidHandshakeWire,
  invalidHandshakeSender,
  isRivalWelcome,
  shouldIgnoreWelcome,
  commitWinsConflict,
  vetProposals,
} from './groupVet'
export { frameVerifiers, onlyRenewsLeaf, keepsEveryLeaf } from './groupTree'
export type { MediaStep } from './groupParams'

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
export class MediaGroup extends GroupSync {
  static async open(userId: string, channelId: string, timing: MediaTiming = mediaTiming()): Promise<MediaGroup | null> {
    const id = mediaUserId(userId)
    const groupId = mediaGroupId(channelId)
    if (!id || !groupId) return null
    const cs = await suite()
    const credential: Credential = { credentialType: 'basic', identity: new TextEncoder().encode(id) }
    const own = await generateKeyPackage(credential, defaultCapabilities(), defaultLifetime, [], cs)
    return new MediaGroup(id, groupId, own, cs, credential, timing)
  }

  /**
   * With batch, a key package only waits for the next sync instead of being
   * committed at once: the call syncs after the handshakes that arrived
   * together, so members who joined together are added in one commit.
   */
  receive(fromUserId: string, wire: Uint8Array, batch = false): Promise<MediaStep> {
    return this.enqueue(() => this.receiveNow(fromUserId, wire, batch))
  }

  protected async receiveNow(fromUserId: string, wire: Uint8Array, batch = false): Promise<MediaStep> {
    if (invalidHandshakeWire(this.closed, wire.byteLength)) return this.step()
    const from = mediaUserId(fromUserId)
    if (invalidHandshakeSender(from, this.userId)) return this.step()
    const sender = from as string
    this.dropSelfUpdate(true)
    if (isHeartbeat(wire)) return this.beatNow(sender, wire)
    if (isCapability(wire)) return this.capNow(sender, wire)
    const decoded = decodeMlsMessage(wire, 0)
    if (!decoded) return this.step()
    const [message] = decoded
    this.heard.add(sender)
    this.lastHandshakeAt = clock.now()
    const members = this.state ? treeMembers(this.state) : null
    return this.dispatchMlsMessage(sender, wire, message, members, batch)
  }

  protected dispatchMlsMessage(from: string, wire: Uint8Array, message: MlsMessage, members: Map<string, number> | null, batch: boolean): Promise<MediaStep> {
    if (message.wireformat === 'mls_key_package') {
      return this.receiveKeyPackage(from, wire, message.keyPackage, members, batch)
    }
    if (message.wireformat === 'mls_welcome') {
      return this.receiveWelcome(from, message.welcome, members)
    }
    if (message.wireformat === 'mls_public_message' && this.state) {
      return this.receivePublic(from, message.publicMessage, members)
    }
    return Promise.resolve(this.step())
  }

  /** A member's key package: kept for the next commit, or, from a member coming back, swapped in. */
  protected async receiveKeyPackage(from: string, wire: Uint8Array, keyPackage: KeyPackage, members: Map<string, number> | null, batch: boolean): Promise<MediaStep> {
    const id = credentialIdentity(keyPackage.leafNode.credential)
    if (id !== from) return this.step()
    this.relay(from, keyPackage)
    const known = this.state ? treeLeafSigner(this.state, from) : null
    const same = known !== null && sameBytes(known, keyPackage.leafNode.signaturePublicKey)
    if (members?.has(from)) return this.receiveReturningMember(from, wire, keyPackage, same)
    const wires: Uint8Array[] = []
    this.remember(from, wire)
    this.pending.set(from, keyPackage)
    // A package from someone the roster does not list waits until it does.
    if (!batch && this.room.includes(from) && this.canCommit(this.room)) wires.push(...await this.commitAdds([from]))
    return this.step(wires)
  }

  protected async receiveReturningMember(from: string, wire: Uint8Array, keyPackage: KeyPackage, same: boolean): Promise<MediaStep> {
    const wires: Uint8Array[] = []
    // The same package again is a duplicate. Another one is the member
    // coming back (a reload): swap the old leaf for the new one in a single
    // commit. If it was the committer, the next one does it. Only a member
    // the roster says is still here, and not again and again.
    if (same || !this.room.includes(from) || this.seen(from, wire)) return this.step()
    if (from === this.committer(this.room)) this.restarting.add(from)
    if (!this.canCommit(this.room) || !this.swaps.allow(from)) return this.step()
    this.remember(from, wire)
    wires.push(...await this.replaceMember(from, keyPackage))
    return this.step(wires)
  }

  protected async receiveWelcome(from: string, welcome: Welcome, members: Map<string, number> | null): Promise<MediaStep> {
    const previous = this.state
    const rival = isRivalWelcome(previous, members, from, this.userId)
    const prevCount = previous ? treeMembers(previous).size : 0
    if (shouldIgnoreWelcome(previous !== null, prevCount, rival)) return this.step()
    if (!this.room.includes(from)) return this.step()
    if (rival && !this.rivals.allow(from)) return this.step()
    try {
      await this.joinFromWelcome(from, welcome, previous, rival)
    } catch {
      return this.step()
    }
    return this.step()
  }

  protected async joinFromWelcome(from: string, welcome: Welcome, previous: ClientState | null, rival: boolean): Promise<void> {
    const joined = await joinGroup(welcome, this.own.publicPackage, this.own.privatePackage, emptyPskIndex, this.cs)
    // Only the group's committer welcomes: the lowest of its members the
    // roster lists, leaving me out (I may be the committer coming back).
    const committer = lowest([...treeMembers(joined).keys()].filter(id => id !== this.userId && this.room.includes(id)))
    if (committer !== from) {
      this.wipeRejected(joined)
      return
    }
    if (rival) this.discard()
    if (await this.adopt(joined)) {
      this.offeredFor = null
      if (previous) this.wipeRejected(previous)
    }
  }

  /**
   * A commit for an epoch I have not reached means I missed one: nothing
   * that follows can be processed, so start over instead of waiting.
   * It counts only from a member of this group, for this group, and ahead.
   * Its signature cannot be checked (the epoch is not mine), so it counts
   * only from a member who may be committing: it names that member's own
   * leaf and the member is among the two lowest live ones.
   */
  protected isCommitAhead(framed: PublicMessage['content'], previous: ClientState, members: Map<string, number> | null, from: string): boolean {
    return framed.contentType === 'commit' && framed.sender.senderType === 'member'
      && sameBytes(framed.groupId, this.groupId) && framed.epoch > previous.groupContext.epoch
      && members?.get(from) === framed.sender.leafIndex && this.mayCommit(from)
  }

  /** A commit for the epoch I left with a self-update, from someone else. */
  protected isCommitForUpdatedEpoch(framed: PublicMessage['content'], saved: NonNullable<GroupBase['selfUpdate']>, from: string): boolean {
    return framed.contentType === 'commit' && framed.sender.senderType === 'member'
      && sameBytes(framed.groupId, this.groupId) && framed.epoch === saved.state.groupContext.epoch && from !== saved.sender
  }

  protected async receivePublic(from: string, publicMessage: PublicMessage, members: Map<string, number> | null): Promise<MediaStep> {
    const previous = this.state as ClientState
    const framed = publicMessage.content
    if (this.isCommitAhead(framed, previous, members, from)) return this.recoverNow()
    const saved = this.selfUpdate
    if (saved && this.isCommitForUpdatedEpoch(framed, saved, from)) return this.conflictNow(saved, publicMessage, from)
    const isUpdate = framed.contentType === 'commit' && framed.commit.proposals.length === 0 && this.committer(this.listed()) !== from
    return this.processCommit(previous, publicMessage, from, members, isUpdate)
  }

  protected async processCommit(previous: ClientState, publicMessage: PublicMessage, from: string, members: Map<string, number> | null, isUpdate: boolean): Promise<MediaStep> {
    try {
      const processed = await processPublicMessage(previous, publicMessage, emptyPskIndex, this.cs, this.validator(previous, from))
      // Rejected by the rules above (or a proposal, which this group never
      // uses): the sender is not trusted with it, and nothing is thrown away.
      if (processed.actionTaken === 'reject') return this.step()
      // Only a commit that verified against this group's tree shows its member alive.
      this.alive.set(from, clock.now())
      // Every commit funnels through this: nobody's identity or signature key may change, and no
      // leaf but the sender's own may get a new encryption key, except leaves the commit removes.
      if (!onlyRenewsLeaf(previous, processed.newState, members?.get(from) as number, this.vetted)) {
        wipeConsumed(processed.consumed)
        this.wipeRejected(processed.newState)
        return this.step()
      }
      if (isUpdate) {
        this.updates.set(from, clock.now())
        return this.adoptUpdate(previous, processed.newState, processed.consumed, from)
      }
      wipeConsumed(processed.consumed)
      // A commit that removes a member who is still here was not asked for:
      // say so, and offer again, but not in a loop.
      if (!treeMembers(processed.newState).has(this.userId)) return await this.removedFromGroup(processed.newState)
      if (!await this.adopt(processed.newState, keepsEveryLeaf(previous, processed.newState) ? 'soft' : 'commit')) {
        // The epoch was refused (for instance a leaf identity held twice): this group is out of step.
        return this.recoverNow()
      }
    } catch {
      return this.step()
    }
    return this.step()
  }

  protected async removedFromGroup(rejected: ClientState): Promise<MediaStep> {
    this.wipeRejected(rejected)
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

  /**
   * Takes another member's self-update, keeping the state before it for
   * CONFLICT_MS in case the committer's commit for the same epoch wins.
   */
  protected async adoptUpdate(previous: ClientState, next: ClientState, consumed: Uint8Array[], sender: string): Promise<MediaStep> {
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
  protected async conflictNow(saved: NonNullable<GroupBase['selfUpdate']>, message: PublicMessage, from: string): Promise<MediaStep> {
    try {
      const processed = await processPublicMessage(saved.state, message, emptyPskIndex, this.cs, this.validator(saved.state, from))
      const content = message.content
      const empty = content.contentType === 'commit' && content.commit.proposals.length === 0
      const isCommitter = this.committer(this.listed()) === from
      const renews = processed.actionTaken === 'reject' || onlyRenewsLeaf(saved.state, processed.newState, treeMembers(saved.state).get(from) as number, this.vetted)
      const wins = commitWinsConflict(processed.actionTaken === 'reject', renews, empty, isCommitter, from, saved.sender)
      if (!wins) {
        wipeConsumed(processed.consumed)
        if (processed.newState !== saved.state) this.wipeRejected(processed.newState)
        return this.step()
      }
      return this.switchConflictState(saved, processed, from)
    } catch {
      return this.step()
    }
  }

  protected async switchConflictState(saved: NonNullable<GroupBase['selfUpdate']>, processed: ProcessedPublicMessage, from: string): Promise<MediaStep> {
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
  protected validator(state: ClientState, from: string): IncomingMessageCallback {
    return (incoming) => {
      this.vetted = new Set()
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
      const removedAt = vetProposals(
        members, proposals, from, this.hinted, this.userId, this.room,
        this.timing, this.sleptRecently(), (id, kp) => this.wasRelayed(id, kp),
        clock.now(), this.alive
      )
      if (!removedAt) return 'reject'
      this.vetted = removedAt
      return 'accept'
    }
  }

  /** A heartbeat counts when this epoch's member signed it, and it is newer than the last. */
  protected async beatNow(from: string, wire: Uint8Array): Promise<MediaStep> {
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

  /**
   * A capability counts when a member of this epoch signed it with its leaf
   * key. Hearing a member's for the first time is answered with this member's
   * own at once, so two newcomers do not wait for a tick to learn each other.
   */
  protected async capNow(from: string, wire: Uint8Array): Promise<MediaStep> {
    const current = this.current
    if (!current || !this.state) return this.step()
    const verifier = current.key.verifiers.get(from)
    if (!verifier) return this.step()
    const bits = await openCapability(verifier, this.groupId, from, wire)
    if (!validCapBits(bits)) return this.step()
    const signer = treeLeafSigner(this.state, from) as Uint8Array
    const known = this.capable.get(from)
    this.capable.set(from, new Uint8Array(signer))
    if (isKnownSigner(known, signer)) return this.step()
    this.capDue = false
    const own = await sealCapability(current.key.signer, this.groupId, this.userId)
    return this.step(own ? [own] : [])
  }
}
