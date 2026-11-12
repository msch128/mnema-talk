import type { ClientState, Proposal } from 'ts-mls'
import type { MediaTiming } from './bounds'
import { credentialIdentity } from './groupTree'

/**
 * How long a member that shows no sign of life stays the committer, or counts
 * as the committer to a member that joined meanwhile: the absence limit plus
 * two maintenance intervals. The member that takes over from a hung committer
 * must not remove it before every receiver agrees that it no longer commits,
 * or the receivers reject that commit as one from a non-committer.
 */
export function patienceMs(timing: MediaTiming): number {
  return timing.absentMs + 2 * Math.max(10, timing.maintenanceMs)
}

/** The ids a commit adds. */
export function addedIds(proposals: readonly Proposal[]): Set<string> {
  const ids = new Set<string>()
  for (const proposal of proposals) {
    if (proposal.proposalType !== 'add') continue
    const id = credentialIdentity(proposal.add.keyPackage.leafNode.credential)
    if (id) ids.add(id)
  }
  return ids
}

/**
 * When each member of my tree came in, as this client saw it: the epoch whose
 * commit added its id (a reload is a swap of the leaf, which is an add), or the
 * first epoch with its id. A self-update is no entry. The committer reads it
 * before it removes someone for silence.
 *
 * A member that came in by a welcome counts every other member as alive from
 * the moment it came in, since it heard none of their heartbeats, and refuses
 * a removal until the absence limit has passed since then (see
 * prematureRemoval in groupVet.ts), and takes a silent lowest id for the
 * committer until the patience has passed since then (see patienceMs). A
 * committer that removes by the heartbeats it heard itself would be earlier
 * than that, be refused by every newcomer, be one epoch ahead of them, and
 * start them all over: which sets their clocks back, so the next removal is
 * refused again. So it waits until the newest member has been in for the
 * patience and a little more. An add of an id that is in the tree already
 * (its leaf swapped, even for the same leaf) is a welcome like any other.
 */
export class Entries {
  private readonly at = new Map<string, number>()

  /** A new epoch: `added` are the ids its commit added (a swap of a leaf is an add). */
  observe(state: ClientState, now: number, added: ReadonlySet<string>) {
    const present = new Set<string>()
    state.ratchetTree.forEach((node, index) => {
      if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
      const id = credentialIdentity(node.leaf.credential)
      if (!id) return
      present.add(id)
      if (!this.at.has(id) || added.has(id)) this.at.set(id, now)
    })
    for (const id of [...this.at.keys()]) if (!present.has(id)) this.at.delete(id)
  }

  /** The group is gone: so is what was known of when its members came in. */
  clear() {
    this.at.clear()
  }

  /** Whether a member other than `silent` and `self` came in lately enough to refuse a removal that is made now. */
  holds(silent: ReadonlySet<string>, self: string, now: number, timing: MediaTiming): boolean {
    const wait = patienceMs(timing) + Math.max(10, timing.maintenanceMs)
    for (const [id, at] of this.at) if (id !== self && !silent.has(id) && now - at <= wait) return true
    return false
  }
}
