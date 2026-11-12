import type { ClientState, KeyPackage, Proposal } from 'ts-mls'
import { credentialIdentity, treeMembers } from './groupTree'
import { GroupEpoch } from './groupEpoch'
import { vetAdds } from './groupAdds'
import { type AddProposal } from './groupEpochCommit'

/** The commits that change who is in the group: adds, removals of those who left, and the swap of a leaf. */
export abstract class GroupCommits extends GroupEpoch {
  protected async commitAdds(others: readonly string[]): Promise<Uint8Array[]> {
    if (!this.state) return []
    const proposals = this.proposalsFor(others, treeMembers(this.state))
    if (!proposals.length) return []
    // One bad package would fail a whole batch again and again as it stays pending: try each alone, drop the failing.
    const vetted = proposals.length > 1 ? await vetAdds(this.state, this.cs, proposals) : null
    for (const trial of vetted?.trials ?? []) this.wipeRejected(trial)
    if (vetted) this.dropPackages(vetted.dropped)
    const usable = vetted ? vetted.usable : proposals
    if (!usable.length) return []
    const wires = await this.commit(usable)
    if (wires.length || usable.length === 1) this.dropPackages(usable)
    return wires
  }

  private dropPackages(proposals: readonly AddProposal[]): void {
    for (const proposal of proposals) {
      const id = credentialIdentity(proposal.add.keyPackage.leafNode.credential)
      if (id && this.pending.get(id) === proposal.add.keyPackage) this.pending.delete(id)
    }
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
    return this.commit(proposals)
  }
}
