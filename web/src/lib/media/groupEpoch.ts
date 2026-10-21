import {
  createCommit,
  mlsExporter,
  zeroOutUint8Array,
  type ClientState,
  type KeyPackage,
  type Proposal,
} from 'ts-mls'
import { FRAME_EPOCH_BYTES, MEDIA_EXPORTER_LABEL } from './bounds'
import { clock } from './clock'
import { importEpochKey, importFrameSigner, type EpochKeys } from './frame'
import { sealHeartbeat } from './heartbeat'
import { sealCapability } from './capability'
import { safetyCode } from './verify'
import { CONFLICT_MS, type MediaStep } from './groupParams'
import { credentialIdentity, sameBytes, wipeConsumed, treeMembers, leafIdentities, frameVerifiers } from './groupTree'
import { GroupBase } from './groupBase'
import { commitMessage, dropAbsent, retainedPrevious, type AddProposal, type CommitResult, type SealedCommit } from './groupEpochCommit'

type EpochMaterial = {
  aes: CryptoKey
  code: string
  signer: CryptoKey
  verifiers: Map<string, CryptoKey>
}

export abstract class GroupEpoch extends GroupBase {
  /**
   * 'new' is a group of its own or one joined by a welcome. A 'commit' moves the
   * group I am in to its next epoch; a 'soft' one removed nobody.
   */
  protected async adopt(state: ClientState, via: 'new' | 'commit' | 'soft' = 'new'): Promise<boolean> {
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
      const material = await this.epochMaterial(state, raw)
      if (!material) {
        this.wipeRejected(state)
        return false
      }
      this.installEpoch(state, via, epoch, material)
      return true
    } finally {
      zeroOutUint8Array(raw)
    }
  }

  private async epochMaterial(state: ClientState, raw: Uint8Array): Promise<EpochMaterial | null> {
    const aes = await importEpochKey(raw)
    const code = await safetyCode(state.keySchedule.epochAuthenticator)
    // Without Ed25519 in this browser there is no signer, so no keys and no media.
    const signer = await importFrameSigner(state.signaturePrivateKey)
    const verifiers = await frameVerifiers(state)
    // An identity that holds two leaves (or a leaf nobody can read) is never
    // a group to key media from: refuse the epoch rather than pick a leaf.
    const leaves = leafIdentities(state)
    if (!aes) return null
    if (!code) return null
    if (!signer) return null
    if (!verifiers) return null
    if (!verifiers.has(this.userId)) return null
    if (leaves.includes('')) return null
    if (new Set(leaves).size !== leaves.length) return null
    return { aes, code, signer, verifiers }
  }

  private installEpoch(state: ClientState, via: 'new' | 'commit' | 'soft', epoch: bigint, material: EpochMaterial): void {
    this.state = state
    // A swap has landed (or the group changed): everyone counts again.
    this.restarting.clear()
    if (this.offerTimer !== null) clearTimeout(this.offerTimer)
    this.offerTimer = null
    const epochNumber = Number(epoch)
    const before = this.current
    const key: EpochKeys = { key: material.aes, signer: material.signer, verifiers: material.verifiers }
    this.current = { epoch: epochNumber, key, soft: via === 'soft', verification: { epoch: epochNumber, code: material.code, members: leafIdentities(state).sort() } }
    this.previousEpoch = retainedPrevious(via, before, epochNumber)
    this.startEpoch(state)
  }

  /** A new epoch begins: its age starts, and so does the delay before it is rotated. */
  protected startEpoch(state: ClientState) {
    const now = clock.now()
    this.epochAt = now
    const { rotateMs, rotateJitterMs } = this.timing
    this.rotateAfter = Math.max(0, rotateMs + (Math.random() * 2 - 1) * rotateJitterMs)
    const members = treeMembers(state)
    this.trackLeaves(state, members, now)
    this.forgetDeparted(members)
    this.markNewMembers(members, now)
    if (this.tickTimer === null) {
      this.lastTick = now
      this.lastTickWall = clock.wall()
      this.lastBeat = now
      this.scheduleTick()
    }
  }

  /** Keeps when each member's leaf key was last seen to change, and when my own leaf is due again. */
  private trackLeaves(state: ClientState, members: Map<string, number>, now: number) {
    const { rotateMs, rotateJitterMs } = this.timing
    dropAbsent(this.leaves, members)
    dropAbsent(this.updates, members)
    for (const [id, index] of members) {
      const key = (state.ratchetTree[index * 2] as { leaf: { hpkePublicKey: Uint8Array } }).leaf.hpkePublicKey
      const known = this.leaves.get(id)
      if (known && sameBytes(known.key, key)) continue
      this.leaves.set(id, { key: new Uint8Array(key), at: now })
      // My own leaf is due again after a period of its own, jittered like an epoch's.
      if (id === this.userId) this.updateAfter = Math.max(0, rotateMs + (Math.random() * 2 - 1) * rotateJitterMs)
    }
  }

  /** Members that left the group take their signs of life with them. */
  private forgetDeparted(members: Map<string, number>) {
    dropAbsent(this.alive, members)
    dropAbsent(this.beats, members)
    dropAbsent(this.capable, members)
    // A member this one has not advertised to yet is told at the next tick.
    dropAbsent(this.capHeard, members)
  }

  private markNewMembers(members: Map<string, number>, now: number) {
    for (const id of members.keys()) {
      if (id !== this.userId && !this.capHeard.has(id)) {
        this.capHeard.add(id)
        this.capDue = true
      }
    }
    // Whoever is new to the group starts with a clean slate: it was let in just now.
    for (const id of members.keys()) if (!this.alive.has(id)) this.alive.set(id, now)
  }

  protected scheduleTick() {
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

  protected async tickNow(): Promise<MediaStep> {
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
    const wires = await this.livenessWires(current, members, now, slept)
    wires.push(...await this.maintenanceCommits(members, now, slept))
    return this.step(wires)
  }

  /** This member's capability and heartbeat, when they are due and someone else is here to hear them. */
  private async livenessWires(current: NonNullable<GroupEpoch['current']>, members: Map<string, number>, now: number, slept: boolean): Promise<Uint8Array[]> {
    const wires: Uint8Array[] = []
    const beatDue = this.heartbeatDue(now, slept)
    if (members.size <= 1) return wires
    await this.appendCapability(wires, current, beatDue)
    await this.appendHeartbeat(wires, current, now, beatDue)
    return wires
  }

  private heartbeatDue(now: number, slept: boolean): boolean {
    return slept || now - this.lastBeat >= this.timing.heartbeatMs
  }

  private async appendCapability(wires: Uint8Array[], current: NonNullable<GroupEpoch['current']>, beatDue: boolean): Promise<void> {
    if (!this.capDue && !beatDue) return
    this.capDue = false
    const cap = await sealCapability(current.key.signer, this.groupId, this.userId)
    if (cap) wires.push(cap)
  }

  private async appendHeartbeat(wires: Uint8Array[], current: NonNullable<GroupEpoch['current']>, now: number, beatDue: boolean): Promise<void> {
    if (!beatDue) return
    this.lastBeat = now
    this.lastBeatAt = Math.max(now, this.lastBeatAt + 1)
    const beat = await sealHeartbeat(current.key.signer, this.groupId, this.userId, current.epoch, this.lastBeatAt)
    if (beat) wires.push(beat)
  }

  /** The commits a tick may make: a leaf renewal, removing absent members, or rotating an old epoch. */
  private async maintenanceCommits(members: Map<string, number>, now: number, slept: boolean): Promise<Uint8Array[]> {
    const wires: Uint8Array[] = []
    // Right after waking up this view is the stalest it will ever be: the
    // messages that arrived meanwhile are still queued behind this tick. The
    // next tick, one maintenance interval later, decides.
    if (!slept) await this.maintainWhileAwake(members, now, wires)
    return wires
  }

  private async maintainWhileAwake(members: Map<string, number>, now: number, wires: Uint8Array[]): Promise<void> {
    if (!this.canCommit(this.room) && members.size > 1 && this.renewalDue(now, members)) {
      wires.push(...await this.commit([], true))
    }
    if (this.canCommit(this.room)) wires.push(...await this.committerMaintenance(members, now))
  }

  private async committerMaintenance(members: Map<string, number>, now: number): Promise<Uint8Array[]> {
    const wires: Uint8Array[] = []
    const absent = this.absentProposals(members, now)
    if (absent.length) {
      wires.push(...await this.commit(absent))
      return wires
    }
    if (members.size > 1 && this.rotationDue(now)) wires.push(...await this.commit([]))
    return wires
  }

  private absentProposals(members: Map<string, number>, now: number): Proposal[] {
    const absent: Proposal[] = []
    for (const [id, leaf] of members) {
      if (id !== this.userId && now - (this.alive.get(id) ?? now) > this.timing.absentMs) {
        absent.push({ proposalType: 'remove', remove: { removed: leaf } })
      }
    }
    return absent
  }

  private rotationDue(now: number): boolean {
    if (now - this.epochAt >= this.rotateAfter) return true
    return this.leafAge(now) >= this.updateAfter
  }

  protected leafAge(now: number): number {
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
  protected renewalDue(now: number, members: Map<string, number>): boolean {
    const age = this.leafAge(now)
    if (age < this.updateAfter) return false
    const settle = Math.max(10, this.timing.maintenanceMs)
    if (age >= this.updateAfter + Math.max(4 * settle, this.timing.rotateMs / 10)) return true
    if (this.rosterUnsettled(now, settle)) return false
    return now - this.epochAt >= settle * (1 + this.renewalsAhead(now, members))
  }

  private rosterUnsettled(now: number, settle: number): boolean {
    return now - this.lastHandshakeAt < settle || now - this.lastRosterAt < settle
  }

  private renewalsAhead(now: number, members: Map<string, number>): number {
    const due = Math.max(0, this.timing.rotateMs - this.timing.rotateJitterMs)
    const committer = this.committer(this.room)
    let ahead = 0
    for (const id of members.keys()) {
      if (id >= this.userId || id === committer) continue
      if (now - (this.leaves.get(id) as { at: number }).at >= due) ahead++
    }
    return ahead
  }

  protected async commit(extraProposals: Proposal[], renewal = false): Promise<Uint8Array[]> {
    if (!this.state) return []
    const previous = this.state
    let result: CommitResult
    try {
      result = await createCommit(
        { state: previous, cipherSuite: this.cs },
        { extraProposals, wireAsPublicMessage: true, ratchetTreeExtension: true },
      )
    } catch {
      return []
    }
    if (!renewal) wipeConsumed(result.consumed)
    const wires = commitMessage(result, extraProposals)
    if (!wires) {
      if (renewal) wipeConsumed(result.consumed)
      this.wipeRejected(result.newState)
      return []
    }
    return this.finishCommit(result, wires, previous, renewal)
  }

  private async finishCommit(result: CommitResult, wires: SealedCommit, previous: ClientState, renewal: boolean): Promise<Uint8Array[]> {
    const current = this.current
    const previousEpoch = this.previousEpoch
    if (renewal) this.dropSelfUpdate()
    const adopted = await this.adopt(result.newState, wires.soft ? 'soft' : 'commit')
    if (!adopted) {
      if (renewal) wipeConsumed(result.consumed)
      return []
    }
    // A renewal may race another commit for the same epoch: keep the state it left.
    if (renewal) this.selfUpdate = { state: previous, current, previousEpoch, sender: this.userId, until: clock.now() + CONFLICT_MS, held: result.consumed }
    return wires.welcome ? [wires.welcome, wires.commit] : [wires.commit]
  }

  protected async commitAdds(others: readonly string[]): Promise<Uint8Array[]> {
    if (!this.state) return []
    const proposals = this.proposalsFor(others, treeMembers(this.state))
    if (!proposals.length) return []
    const wires = await this.commit(proposals)
    if (wires.length) this.keepAdded(proposals)
    return wires
  }

  private proposalsFor(others: readonly string[], members: Map<string, number>): AddProposal[] {
    const wanted = new Set(others)
    const proposals: AddProposal[] = []
    for (const [id, keyPackage] of this.pending) {
      if (!wanted.has(id) || members.has(id)) continue
      proposals.push({ proposalType: 'add', add: { keyPackage } })
    }
    return proposals
  }

  private keepAdded(proposals: readonly AddProposal[]): void {
    for (const proposal of proposals) {
      const id = credentialIdentity(proposal.add.keyPackage.leafNode.credential)
      if (id) this.pending.delete(id)
    }
  }

  protected async commitRemoves(others: readonly string[]): Promise<Uint8Array[]> {
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

  /** One commit that swaps a member's old leaf for the new package. */
  protected async replaceMember(id: string, keyPackage: KeyPackage): Promise<Uint8Array[]> {
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
}
