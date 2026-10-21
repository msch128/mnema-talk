import {
  type Proposal,
  type createCommit,
} from 'ts-mls'
import { PREVIOUS_EPOCH_MS } from './bounds'
import { clock } from './clock'
import { type GroupBase } from './groupBase'
import { encodeWire } from './groupTree'

export type CommitResult = Awaited<ReturnType<typeof createCommit>>

export type SealedCommit = {
  welcome: Uint8Array | null
  commit: Uint8Array
  soft: boolean
}

export type AddProposal = Extract<Proposal, { proposalType: 'add' }>

type IdBag = { keys(): Iterable<string>, delete(id: string): boolean }

/** The wires a created commit goes out as: the welcome for new members, if any, and the public commit. */
function commitWires(result: CommitResult): { welcome: Uint8Array | null, commit: Uint8Array | null } {
  const welcome = result.welcome
    ? encodeWire({ version: 'mls10', wireformat: 'mls_welcome', welcome: result.welcome })
    : null
  const commit = result.commit.wireformat === 'mls_public_message' ? encodeWire(result.commit) : null
  return { welcome, commit }
}

export function commitMessage(result: CommitResult, extraProposals: readonly Proposal[]): SealedCommit | null {
  const adding = extraProposals.some(proposal => proposal.proposalType === 'add')
  const { welcome, commit } = commitWires(result)
  if (!commit || (adding && !welcome)) return null
  const soft = !extraProposals.some(proposal => proposal.proposalType === 'remove')
  return { welcome, commit, soft }
}

export function dropAbsent(held: IdBag, present: ReadonlyMap<string, number>): void {
  for (const id of [...held.keys()]) {
    if (!present.has(id)) held.delete(id)
  }
}

/** Only the epoch right before this one, of this group, stays readable, and briefly. */
export function retainedPrevious(
  via: 'new' | 'commit' | 'soft',
  before: GroupBase['current'],
  epochNumber: number,
): GroupBase['previousEpoch'] {
  if (via === 'new') return null
  if (!before) return null
  if (before.epoch >= epochNumber) return null
  return { epoch: before.epoch, key: before.key, until: clock.now() + PREVIOUS_EPOCH_MS }
}
