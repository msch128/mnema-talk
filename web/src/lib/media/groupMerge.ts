import type { ClientState } from 'ts-mls'
import { clock } from './clock'
import { OTHER_STATE_MS, RIVAL_BASE_MS, type MediaStep, type OwnPackage } from './groupParams'
import { lowest, sameBytes, treeLeafSigner, treeMembers } from './groupTree'
import { GroupSync } from './groupSync'

/**
 * How two groups in one Talk become one. Everyone ends up in the group of the
 * room's lowest id, which is the one that commits there:
 * - a member whose tree lacks that id offers it a key package on its
 *   maintenance tick, is added like any newcomer and takes the welcome under
 *   the rule for a welcome from a lower id outside its group;
 * - a member whose tree holds that id, but in another state than its own,
 *   learns so from heartbeats that id signed with the leaf key in the tree,
 *   and starts over.
 */
export abstract class GroupMerge extends GroupSync {
  /** The package last offered to the lowest id from inside a group, and when it first was. */
  private asked: { own: OwnPackage, at: number } | null = null
  /** The lowest id's first signed heartbeat for an epoch that is not mine: its time, when it came, and the state I held. */
  private elsewhere: { state: ClientState, at: number, seenAt: number } | null = null

  /** The room's lowest id if it is lower than me: the one whose group this member belongs in. */
  private lead(): string | null {
    const id = lowest(this.room)
    return id !== null && id < this.userId ? id : null
  }

  protected override async tickNow(): Promise<MediaStep> {
    const step = await super.tickNow()
    const offer = await this.mergeOffer()
    return offer ? { ...step, wires: [...step.wires, offer] } : step
  }

  /**
   * My key package for the room's lowest id, when my group has other members
   * and does not have that id (a group of one speaks for itself, see
   * aloneTick). Not while a welcome from it would be refused as too soon: it
   * would add a leaf for me that I never take up. A package that brought no
   * welcome for RIVAL_BASE_MS is replaced, since the welcome may have been
   * lost and the same package again changes nothing over there.
   */
  private async mergeOffer(): Promise<Uint8Array | null> {
    const lead = this.lead()
    if (!this.state || lead === null) return null
    const members = treeMembers(this.state)
    if (members.size === 1 || members.has(lead) || this.rivals.waitMs(lead) > 0) return null
    const now = clock.now()
    if (this.asked?.own !== this.own) this.asked = null
    if (this.asked && now - this.asked.at >= RIVAL_BASE_MS) {
      await this.replaceOwn(this.state)
      this.asked = null
    }
    this.asked ??= { own: this.own, at: now }
    return this.hello()
  }

  /** A new package in place of one that was offered in vain. The group keeps the keys of the old one if it was founded with it. */
  private async replaceOwn(state: ClientState) {
    const leaf = treeLeafSigner(state, this.userId)
    if (leaf && sameBytes(leaf, this.own.publicPackage.leafNode.signaturePublicKey)) await this.renewOwn()
    else await this.rekey()
  }

  /**
   * A heartbeat that verified under the leaf key my tree holds for `from`, newer
   * than any taken from that leaf, for an epoch that is not mine. From the
   * room's lowest id, two of them say that it lives in another state of the
   * group than I do: the second at least OTHER_STATE_MS after the first, by the
   * sender's signed time and by my own clock, with my state unchanged in
   * between (a commit that was merely on its way has arrived by then). Its state
   * is the one everyone ends up in, so I start over, as often as the reset
   * limit allows. Anyone else's, an older one or a copy counts for nothing.
   */
  protected async beatElsewhere(from: string, at: number): Promise<MediaStep> {
    const state = this.state
    if (!state || from !== this.lead()) return this.step()
    const now = clock.now()
    const first = this.elsewhere?.state === state ? this.elsewhere : null
    if (!first) {
      this.elsewhere = { state, at, seenAt: now }
      return this.step()
    }
    if (at < first.at + OTHER_STATE_MS || now < first.seenAt + OTHER_STATE_MS || this.resets.waitMs('self') > 0) return this.step()
    this.elsewhere = null
    return this.recoverNow()
  }
}
