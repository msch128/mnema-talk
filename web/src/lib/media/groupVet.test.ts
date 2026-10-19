import { describe, expect, it } from 'vitest'
import type { KeyPackage } from 'ts-mls'
import { scanCommitProposals, type CommitProposals } from './groupVet.ts'

// Only the fields the scan reads: a proposal's type, the removed leaf, and the
// identity in an added key package's credential.
function add(identity: string): CommitProposals[number] {
  const keyPackage = { leafNode: { credential: { credentialType: 'basic', identity: new TextEncoder().encode(identity) } } }
  return { proposal: { proposalType: 'add', add: { keyPackage } } } as unknown as CommitProposals[number]
}
function remove(leaf: number): CommitProposals[number] {
  return { proposal: { proposalType: 'remove', remove: { removed: leaf } } } as unknown as CommitProposals[number]
}
function other(): CommitProposals[number] {
  return { proposal: { proposalType: 'update', update: {} } } as unknown as CommitProposals[number]
}

const alice = '11111111-1111-4111-8111-111111111111'
const bob = '22222222-2222-4222-8222-222222222222'
const carol = '33333333-3333-4333-8333-333333333333'
const dave = '44444444-4444-4444-8444-444444444444'
const erin = '55555555-5555-4555-8555-555555555555'
const members = new Map([[0, alice], [1, bob], [2, carol]])
const room = new Set([alice, bob, carol, dave])
const relayed = () => true

describe('scanCommitProposals', () => {
  it('collects who a committer removes and adds', () => {
    const effects = scanCommitProposals([remove(1), add(dave)], members, alice, room, relayed)
    expect(effects).toEqual({ removed: new Set([bob]), added: new Set([dave]), removedAt: new Set([1]) })
  })

  it('accepts a commit with nothing in it', () => {
    expect(scanCommitProposals([], members, alice, room, relayed)).toEqual({ removed: new Set(), added: new Set(), removedAt: new Set() })
  })

  it('rejects removing a leaf nobody holds', () => {
    expect(scanCommitProposals([remove(9)], members, alice, room, relayed)).toBeNull()
  })

  it('rejects any proposal that is neither an add nor a remove', () => {
    expect(scanCommitProposals([remove(1), other()], members, alice, room, relayed)).toBeNull()
  })

  it('rejects adding the sender itself, an identity twice, or one outside the room', () => {
    expect(scanCommitProposals([add(alice)], members, alice, room, relayed)).toBeNull()
    expect(scanCommitProposals([add(dave), add(dave)], members, alice, room, relayed)).toBeNull()
    expect(scanCommitProposals([add(erin)], members, alice, room, relayed)).toBeNull()
  })

  it('rejects an add whose key package the relay did not deliver', () => {
    const seen: [string, unknown][] = []
    const notRelayed = (id: string, keyPackage: KeyPackage) => { seen.push([id, keyPackage]); return false }
    expect(scanCommitProposals([add(dave)], members, alice, room, notRelayed)).toBeNull()
    expect(seen).toHaveLength(1)
    expect(seen[0]?.[0]).toBe(dave)
  })

  it('rejects an add with an unreadable credential identity', () => {
    const keyPackage = { leafNode: { credential: { credentialType: 'x509', certificates: [] } } }
    const proposal = { proposal: { proposalType: 'add', add: { keyPackage } } } as unknown as CommitProposals[number]
    expect(scanCommitProposals([proposal], members, alice, room, relayed)).toBeNull()
  })
})
