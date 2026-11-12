import { defaultLifetime, type ClientState, type KeyPackage, type Lifetime } from 'ts-mls'
import { clock } from './clock'
import { credentialIdentity, sameBytes } from './groupTree'

/**
 * How a member's committer tells a key package that an OLD tab made from the
 * one of the tab that replaced it.
 *
 * A reload is a package under a new signature key, and the committer swaps the
 * member's leaf for it. The relay may deliver a package of the old tab only
 * after that, and by the key alone it looks like one more reload. So a package
 * says when it was made: `notBefore` of its lifetime, in seconds, less a
 * margin. The lifetime is signed with the package and not checked by a
 * receiver (only by the committer, against its own clock, which the margin
 * keeps this far from), so a client that does not know it sends 0 and
 * receives this unchanged. A package made before the one the member's leaf in
 * my tree came from is never a reload.
 */
const MARGIN_S = 400n * 24n * 3600n

/** The lifetime of a package made now. */
export function freshLifetime(): Lifetime {
  const now = BigInt(Math.floor(clock.now() / 1000))
  return { ...defaultLifetime, notBefore: now > MARGIN_S ? now - MARGIN_S : 0n }
}

/** When the package was made, in the units of notBefore; 0 for one that does not say. */
function madeAt(keyPackage: KeyPackage): bigint {
  const leaf = keyPackage.leafNode
  return leaf.leafNodeSource === 'key_package' ? leaf.lifetime.notBefore : 0n
}

export class PackageAges {
  /** For each member of my tree whose leaf came from a package: its signature key and when that package was made. */
  private readonly leaves = new Map<string, { signer: Uint8Array, madeAt: bigint }>()

  /** Reads the leaves of a new epoch. A leaf that was updated has lost its lifetime, and keeps what was known of its package. */
  observe(state: ClientState) {
    state.ratchetTree.forEach((node, index) => {
      if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
      const id = credentialIdentity(node.leaf.credential)
      if (!id) return
      if (node.leaf.leafNodeSource === 'key_package') this.leaves.set(id, { signer: node.leaf.signaturePublicKey, madeAt: node.leaf.lifetime.notBefore })
      else if (!sameBytes(this.leaves.get(id)?.signer ?? new Uint8Array(), node.leaf.signaturePublicKey)) this.leaves.delete(id)
    })
  }

  /** Whether the package was made before the one the member's leaf in my tree came from. */
  stale(id: string, keyPackage: KeyPackage): boolean {
    const made = madeAt(keyPackage)
    const leaf = this.leaves.get(id)
    return made > 0n && leaf !== undefined && made < leaf.madeAt
  }

  keepOnly(listed: (id: string) => boolean) {
    for (const id of [...this.leaves.keys()]) if (!listed(id)) this.leaves.delete(id)
  }
}
