import type { IncomingMessageCallback, KeyPackage } from 'ts-mls'
import { credentialIdentity } from './groupTree'

type CommitIncoming = Extract<Parameters<IncomingMessageCallback>[0], { kind: 'commit' }>
export type CommitProposals = CommitIncoming['proposals']

/** What a committer's proposals do to the group: who is removed, who is added, and which leaves go. */
export interface CommitEffects {
  removed: Set<string>
  added: Set<string>
  removedAt: Set<number>
}

/**
 * Reads a commit's proposals, which may only remove a known leaf or add an
 * identity in `roomOrLast` (not the sender, not twice) whose key package the
 * relay delivered. Anything else, a proposal of another kind included,
 * makes the whole commit unacceptable (null).
 */
export function scanCommitProposals(
  proposals: CommitProposals,
  identityOf: ReadonlyMap<number, string>,
  from: string,
  roomOrLast: ReadonlySet<string>,
  wasRelayed: (id: string, keyPackage: KeyPackage) => boolean
): CommitEffects | null {
  const removed = new Set<string>()
  const added = new Set<string>()
  const removedAt = new Set<number>()
  for (const { proposal } of proposals) {
    if (proposal.proposalType === 'remove') {
      const id = identityOf.get(proposal.remove.removed)
      if (id === undefined) return null
      removed.add(id)
      removedAt.add(proposal.remove.removed)
    } else if (proposal.proposalType === 'add') {
      const id = credentialIdentity(proposal.add.keyPackage.leafNode.credential)
      if (id === null || id === from || added.has(id) || !roomOrLast.has(id) || !wasRelayed(id, proposal.add.keyPackage)) return null
      added.add(id)
    } else {
      return null
    }
  }
  return { removed, added, removedAt }
}
