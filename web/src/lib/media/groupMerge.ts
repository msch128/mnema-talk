import type { ClientState, MLSMessage } from 'ts-mls'
import { clock } from './clock'
import { importFrameVerifier } from './frame'
import { openHeartbeat } from './heartbeat'
import { ASK_BASE_MS, ASK_MAX_MS, OTHER_STATE_BEATS, OTHER_STATE_MS, type MediaStep, type OwnPackage } from './groupParams'
import { lowest, sameBytes, treeLeafSigner, treeMembers } from './groupTree'
import { GroupSync } from './groupSync'

/**
 * How two groups in one Talk become one. Everyone ends up in the group of the
 * room's lowest id, which is the one that commits there:
 * - a member whose tree lacks that id, once that id has shown that it holds a
 *   group, offers it a key package on its maintenance tick, is added like any
 *   newcomer and takes the welcome under the rule for a welcome from a lower
 *   id outside its group;
 * - a member whose tree holds that id, but in another state than its own,
 *   learns so from heartbeats that id signed with the leaf key in the tree,
 *   and starts over.
 *
 * The offer costs the member's own group nothing. The package is signed with
 * the key of the member's leaf, which every member of its group takes for a
 * duplicate; only a package under a new signature key is a reload.
 */
export abstract class GroupMerge extends GroupSync {
  /**
   * When each listed id outside my tree last showed that it holds a group: a
   * commit or a welcome under its id, which cannot be checked from outside that
   * group, or a heartbeat signed with the key of the leaf it had in a tree of
   * mine (see beatOutside). All it decides is whether I ask that id to let me
   * in. Forgotten once the id is in my tree or leaves the room.
   */
  private readonly groupSigns = new Map<string, number>()
  /**
   * My last offer to such an id from inside a group: the package, when it went
   * out, and how long until the next. It outlives the id's leaving and coming
   * back, and goes when the id is in my tree or has been gone for long.
   */
  private readonly asks = new Map<string, { own: OwnPackage, at: number, wait: number }>()
  /** The lowest id's first signed heartbeat for an epoch that is not mine: its time, when it came, and the state I held. */
  private elsewhere: { state: ClientState, at: number, seenAt: number } | null = null

  /** The room's lowest id if it is lower than me: the one whose group this member belongs in. */
  private lead(): string | null {
    const id = lowest(this.room)
    return id !== null && id < this.userId ? id : null
  }

  protected noteGroupSign(from: string, message: MLSMessage, members: Map<string, number> | null) {
    if (members?.has(from) || !this.hinted.has(from)) return
    const commit = message.wireformat === 'mls_public_message' && message.publicMessage.content.contentType === 'commit'
      && sameBytes(message.publicMessage.content.groupId, this.groupId)
    if (commit || message.wireformat === 'mls_welcome') this.groupSigns.set(from, clock.now())
  }

  protected override startEpoch(state: ClientState) {
    super.startEpoch(state)
    // Whoever is in my tree is no other group to me, and what it showed before says nothing about later.
    for (const id of treeMembers(state).keys()) {
      this.groupSigns.delete(id)
      this.asks.delete(id)
    }
  }

  protected override async tickNow(): Promise<MediaStep> {
    const step = await super.tickNow()
    const now = clock.now()
    for (const id of [...this.groupSigns.keys()]) if (!this.hinted.has(id)) this.groupSigns.delete(id)
    for (const [id, ask] of [...this.asks]) if (!this.hinted.has(id) && now > ask.at + ask.wait + ASK_MAX_MS) this.asks.delete(id)
    const offer = await this.mergeOffer(now)
    return offer ? { ...step, wires: [...step.wires, offer] } : step
  }

  /**
   * My key package for the room's lowest id, when my group has other members
   * and does not have that id (a group of one speaks for itself, see
   * aloneTick), and that id has shown that it holds a group. Not while a
   * welcome from it would be refused as too soon: it would add a leaf for me
   * that I never take up. An offer that brought no welcome is followed by
   * another only after a wait that doubles, and only once that id has shown
   * its group again since: one that fell silent is asked once. A package that
   * was offered before is replaced first, since the welcome may have been lost
   * and the same package again changes nothing over there. A sign older than
   * ASK_MAX_MS is none.
   */
  private async mergeOffer(now: number): Promise<Uint8Array | null> {
    const lead = this.lead()
    const sign = lead === null ? undefined : this.groupSigns.get(lead)
    if (!this.state || lead === null || sign === undefined) return null
    const members = treeMembers(this.state)
    if (members.size === 1 || members.has(lead) || this.rivals.waitMs(lead) > 0) return null
    const last = this.asks.get(lead)
    // A sign says that the id held a group then. An old one, of an id that has only now become the lowest, says nothing about now.
    if (now - sign > ASK_MAX_MS || (last && (sign < last.at || now < last.at + last.wait))) return null
    // What I offer from inside a group is always under my leaf's key. The package I hold may not be: a group of one made itself a new one, and grew again.
    const leaf = treeLeafSigner(this.state, this.userId)
    // A package that went out before, to this id or to one that was the lowest then, may have a leaf over there already.
    const offered = [...this.asks.values()].some(ask => ask.own === this.own)
    if (offered || !leaf || !sameBytes(leaf, this.own.publicPackage.leafNode.signaturePublicKey)) await this.renewOwn()
    this.asks.set(lead, { own: this.own, at: now, wait: last ? Math.min(2 * last.wait, ASK_MAX_MS) : ASK_BASE_MS })
    return this.hello()
  }

  /**
   * A heartbeat counts when the sender's leaf of my epoch signed it and it is
   * newer than every heartbeat that leaf key ever signed for me, whatever the
   * epoch. For my epoch it is a sign of life. For another it is none; from the
   * room's lowest id it may say that the group has split, but only once that
   * leaf has signed one for my own epoch since I hold this group: without it,
   * "newer" has nothing to stand on, and heartbeats sent before I joined
   * would count.
   */
  protected async beatNow(from: string, wire: Uint8Array): Promise<MediaStep> {
    const current = this.current
    const verifier = current?.key.verifiers.get(from)
    if (!current || !verifier) return this.beatOutside(from, wire)
    const beat = await openHeartbeat(verifier, this.groupId, from, wire)
    if (!beat) return this.step()
    // A verifier exists only for a member with exactly one leaf.
    const signer = treeLeafSigner(this.state as ClientState, from) as Uint8Array
    const known = this.beats.get(from)
    // A reloaded member signs with a new leaf key and starts its clock anew.
    const last = known && sameBytes(known.signer, signer) ? known : null
    if (last && beat.at <= last.at) return this.step()
    const mine = beat.epoch === current.epoch
    this.beats.set(from, { at: beat.at, signer: last?.signer ?? new Uint8Array(signer), here: mine || (last?.here ?? false) })
    if (!mine) return last?.here ? this.beatElsewhere(from, beat.at) : this.step()
    this.alive.set(from, clock.now())
    return this.step()
  }

  /**
   * A heartbeat of a listed id that is not in my tree. I can check it only if
   * that id was in a tree of mine before and I verified a heartbeat of its leaf
   * there: the key of that leaf is kept with the time of its newest heartbeat.
   * One that verifies under that key and is newer shows that the id lives in a
   * group of several members now (a group of one sends none), which is the
   * sign that I may ask it to let me in. It cannot be forged without that
   * leaf's key, and an older one or a copy says nothing. An id I never shared
   * a group with, or one that reloaded since, has no key here: its heartbeats
   * stay unread, and only its commit or welcome is a sign.
   */
  private async beatOutside(from: string, wire: Uint8Array): Promise<MediaStep> {
    const known = this.beats.get(from)
    if (!known || !this.state || treeMembers(this.state).has(from) || !this.hinted.has(from)) return this.step()
    const verifier = await importFrameVerifier(known.signer)
    const beat = verifier ? await openHeartbeat(verifier, this.groupId, from, wire) : null
    if (!beat || beat.at <= known.at) return this.step()
    known.at = beat.at
    this.groupSigns.set(from, clock.now())
    // It also counts as heard from, like after any handshake of its that merely parses: the next sync has it in the room.
    this.heard.add(from)
    return this.step()
  }

  /**
   * A heartbeat of `from` for an epoch that is not mine, which passed the
   * checks of beatNow. From the room's lowest id, two of them say that it lives
   * in another state of the group than I do: the second at least
   * OTHER_STATE_MS after the first, by the sender's signed time and by my own
   * clock (a commit that was merely on its way has arrived by then), no more
   * than OTHER_STATE_BEATS heartbeat periods after it, and with my state
   * unchanged in between. Its state is the one everyone ends up in, so I start
   * over, as often as the reset limit allows; while that limit holds me back,
   * the newest of them stands as the first.
   */
  private async beatElsewhere(from: string, at: number): Promise<MediaStep> {
    const state = this.state
    if (!state || from !== this.lead()) return this.step()
    const now = clock.now()
    const first = this.elsewhere
    const fresh = first?.state === state && now - first.seenAt <= OTHER_STATE_BEATS * this.timing.heartbeatMs
    if (first && fresh && (at < first.at + OTHER_STATE_MS || now < first.seenAt + OTHER_STATE_MS)) return this.step()
    if (!first || !fresh || this.resets.waitMs('self') > 0) {
      this.elsewhere = { state, at, seenAt: now }
      return this.step()
    }
    this.elsewhere = null
    return this.recoverNow()
  }
}
