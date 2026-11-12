import type { ClientState } from 'ts-mls'
import { credentialIdentity, sameBytes } from './groupTree'

/**
 * When each member of my tree came in, as this client saw it: the first epoch
 * with its id, or the first with its current signature key (a reload is a new
 * one; a self-update keeps it). The committer reads it before it removes
 * someone for silence.
 *
 * A member that came in by a welcome counts every other member as alive from
 * the moment it came in, since it heard none of their heartbeats, and
 * refuses a removal until the absence limit has passed since then (see
 * prematureRemoval in groupVet.ts). A committer that removes by the
 * heartbeats it heard itself would be earlier than that, be refused by every
 * newcomer, be one epoch ahead of them, and start them all over: which sets
 * their clocks back, so the next removal is refused again. So it waits until
 * the newest member has been in for the limit and a little more.
 */
export class Entries {
  private readonly at = new Map<string, { signer: Uint8Array, at: number }>()

  observe(state: ClientState, now: number) {
    const present = new Set<string>()
    state.ratchetTree.forEach((node, index) => {
      if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
      const id = credentialIdentity(node.leaf.credential)
      if (!id) return
      present.add(id)
      const known = this.at.get(id)
      if (!known || !sameBytes(known.signer, node.leaf.signaturePublicKey)) this.at.set(id, { signer: node.leaf.signaturePublicKey, at: now })
    })
    for (const id of [...this.at.keys()]) if (!present.has(id)) this.at.delete(id)
  }

  /** Whether a member other than `except` came in less than `within` ms ago. */
  anyWithin(except: ReadonlySet<string>, now: number, within: number): boolean {
    for (const [id, entry] of this.at) if (!except.has(id) && now - entry.at <= within) return true
    return false
  }
}
