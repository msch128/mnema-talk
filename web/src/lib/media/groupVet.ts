import type { IncomingMessageCallback, KeyPackage } from 'ts-mls'
import { HANDSHAKE_MAX_BYTES, type MediaTiming } from './bounds'
import { CAP_FRAME_V3 } from './capability'
import { credentialIdentity, sameBytes } from './groupTree'

type CommitIncoming = Extract<Parameters<IncomingMessageCallback>[0], { kind: 'commit' }>
export type CommitProposals = CommitIncoming['proposals']

/** What a committer's proposals do to the group: who is removed, who is added, and which leaves go. */
export interface CommitEffects {
  removed: Set<string>
  added: Set<string>
  removedAt: Set<number>
}

function isValidAddIdentity(id: string | null, from: string, added: ReadonlySet<string>, roomOrLast: ReadonlySet<string>): id is string {
  if (id === null || id === from) return false
  if (added.has(id) || !roomOrLast.has(id)) return false
  return true
}

function processAddProposal(
  addProposal: Extract<CommitProposals[number]['proposal'], { proposalType: 'add' }>['add'],
  from: string,
  roomOrLast: ReadonlySet<string>,
  wasRelayed: (id: string, keyPackage: KeyPackage) => boolean,
  added: Set<string>
): boolean {
  const id = credentialIdentity(addProposal.keyPackage.leafNode.credential)
  if (!isValidAddIdentity(id, from, added, roomOrLast)) return false
  if (!wasRelayed(id, addProposal.keyPackage)) return false
  added.add(id)
  return true
}

function processRemoveProposal(
  removeProposal: Extract<CommitProposals[number]['proposal'], { proposalType: 'remove' }>['remove'],
  identityOf: ReadonlyMap<number, string>,
  removed: Set<string>,
  removedAt: Set<number>
): boolean {
  const id = identityOf.get(removeProposal.removed)
  if (id === undefined) return false
  removed.add(id)
  removedAt.add(removeProposal.removed)
  return true
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
      if (!processRemoveProposal(proposal.remove, identityOf, removed, removedAt)) return null
    } else if (proposal.proposalType === 'add') {
      if (!processAddProposal(proposal.add, from, roomOrLast, wasRelayed, added)) return null
    } else {
      return null
    }
  }
  return { removed, added, removedAt }
}

export function illegalAdd(id: string, members: Map<string, number>, removed: Set<string>): boolean {
  return members.has(id) && !removed.has(id)
}

export function skipRemovalCheck(id: string, added: Set<string>, slept: boolean, userId: string, room: Set<string>): boolean {
  if (added.has(id) || slept) return true
  return id === userId || !room.has(id)
}

export function prematureRemoval(now: number, lastAlive: number | undefined, absentMs: number, slack: number): boolean {
  if (lastAlive === undefined) return false
  return now - lastAlive <= absentMs - slack
}

export function validCapBits(bits: number | null): boolean {
  if (bits === null) return false
  return (bits & CAP_FRAME_V3) !== 0
}

export function isKnownSigner(known: Uint8Array | undefined, signer: Uint8Array): boolean {
  if (!known) return false
  return sameBytes(known, signer)
}

export function invalidHandshakeWire(closed: boolean, len: number): boolean {
  if (closed) return true
  return len === 0 || len > HANDSHAKE_MAX_BYTES
}

export function invalidHandshakeSender(from: string | null, userId: string): boolean {
  if (!from) return true
  return from === userId
}

export function isRivalWelcome(previousState: unknown, members: Map<string, number> | null, from: string, userId: string): boolean {
  if (!previousState || !members) return false
  if (from >= userId) return false
  return !members.has(from)
}

export function shouldIgnoreWelcome(hasPrevious: boolean, membersCount: number, rival: boolean): boolean {
  if (!hasPrevious || rival) return false
  return membersCount > 1
}

export function commitWinsConflict(rejected: boolean, renews: boolean, empty: boolean, isCommitter: boolean, from: string, savedSender: string): boolean {
  if (rejected || !renews) return false
  if (!empty) return true
  return isCommitter || from < savedSender
}

export function vetProposals(
  members: Map<string, number>,
  proposals: CommitProposals,
  from: string,
  hinted: ReadonlySet<string>,
  userId: string,
  roomList: readonly string[],
  timing: MediaTiming,
  slept: boolean,
  wasRelayed: (id: string, keyPackage: KeyPackage) => boolean,
  now: number,
  alive: ReadonlyMap<string, number>
): Set<number> | null {
  const identityOf = new Map<number, string>()
  for (const [id, leaf] of members) identityOf.set(leaf, id)
  const room = new Set(hinted)
  room.add(userId)
  const roomOrLast = new Set([...room, ...roomList])
  const slack = Math.max(10, timing.maintenanceMs)
  const effects = scanCommitProposals(proposals, identityOf, from, roomOrLast, wasRelayed)
  if (!effects) return null
  const { removed, added, removedAt } = effects
  for (const id of added) {
    if (illegalAdd(id, members, removed)) return null
  }
  for (const id of removed) {
    if (skipRemovalCheck(id, added, slept, userId, room)) continue
    if (prematureRemoval(now, alive.get(id), timing.absentMs, slack)) return null
  }
  return removedAt
}
