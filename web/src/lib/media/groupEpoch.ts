import {
  createCommit,
  mlsExporter,
  zeroOutUint8Array,
  type ClientState,
  type KeyPackage,
  type Proposal,
} from 'ts-mls'
import { FRAME_EPOCH_BYTES, MEDIA_EXPORTER_LABEL, PREVIOUS_EPOCH_MS } from './bounds'
import { clock } from './clock'
import { importEpochKey, importFrameSigner, type EpochKeys } from './frame'
import { sealHeartbeat } from './heartbeat'
import { sealCapability } from './capability'
import { safetyCode } from './verify'
import { CONFLICT_MS, type MediaStep } from './groupParams'
import { credentialIdentity, sameBytes, wipeConsumed, treeMembers, leafIdentities, frameVerifiers, encodeWire } from './groupTree'
import { GroupBase } from './groupBase'

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

  /** A new epoch begins: its age starts, and so does the delay before it is rotated. */
  protected startEpoch(state: ClientState) {
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
    for (const id of [...this.capable.keys()]) if (!members.has(id)) this.capable.delete(id)
    // A member this one has not advertised to yet is told at the next tick.
    for (const id of [...this.capHeard]) if (!members.has(id)) this.capHeard.delete(id)
    for (const id of members.keys()) {
      if (id !== this.userId && !this.capHeard.has(id)) {
        this.capHeard.add(id)
        this.capDue = true
      }
    }
    // Whoever is new to the group starts with a clean slate: it was let in just now.
    for (const id of members.keys()) if (!this.alive.has(id)) this.alive.set(id, now)
    if (this.tickTimer === null) {
      this.lastTick = now
      this.lastTickWall = clock.wall()
      this.lastBeat = now
      this.scheduleTick()
    }
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
    const beatDue = slept || now - this.lastBeat >= this.timing.heartbeatMs
    if (members.size > 1 && (this.capDue || beatDue)) {
      this.capDue = false
      const cap = await sealCapability(current.key.signer, this.groupId, this.userId)
      if (cap) wires.push(cap)
    }
    if (members.size > 1 && beatDue) {
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

  protected async commit(extraProposals: Proposal[], renewal = false): Promise<Uint8Array[]> {
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

  protected async commitAdds(others: readonly string[]): Promise<Uint8Array[]> {
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
