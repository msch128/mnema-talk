import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import type { Credential } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { HANDSHAKE_MAX_BYTES, mediaGroupId, mediaUserId } from './bounds'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys } from './frame'
import { MediaGroup, frameVerifiers } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function opened(userId: string) {
  const group = await MediaGroup.open(userId, channelId)
  if (!group) throw new Error('expected a media group')
  return group
}

// With a roster, a receiver also syncs to it after each wire: the call does the
// same, since the roster arrives before the handshake that depends on it.
async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[], roster?: string[], tally?: { wires: number }) {
  const pending = wires.map(wire => ({ from, wire }))
  if (roster) for (const [id, group] of groups) if (roster.includes(id)) group.hint(roster)
  if (tally) tally.wires += wires.length
  const seen = new Set<string>()
  while (pending.length) {
    const item = pending.shift()
    if (!item) break
    const mark = `${item.from}:${item.wire.byteLength}:${item.wire[0]}:${item.wire[item.wire.byteLength - 1]}`
    if (seen.has(mark)) continue
    seen.add(mark)
    for (const [id, group] of groups) {
      if (id === item.from) continue
      const step = await group.receive(item.from, item.wire)
      for (const wire of step.wires) pending.push({ from: id, wire })
      if (tally) tally.wires += step.wires.length
      if (roster?.includes(id)) {
        const synced = await group.sync(roster)
        for (const wire of synced.wires) pending.push({ from: id, wire })
        if (tally) tally.wires += synced.wires.length
      }
    }
  }
}

/** Seals as `sender` with its keys `left` and opens with `right`. */
async function roundTrip(left: EpochKeys | null, right: EpochKeys | null, epoch: number, sender = aliceId) {
  if (!left || !right) throw new Error('expected epoch keys')
  const packet = await sealFrame({ keys: left, epoch, stream: 1, counter: 0, channelId, senderId: sender, plaintext: new Uint8Array([9, 8, 7]), source: 0 })
  if (!packet) throw new Error('expected a sealed frame')
  const opened = await openFrame({ keys: new Map([[epoch, right]]), channelId, senderId: sender, packet, replay: new ReplayWindow(), source: 0 })
  return opened ? [...opened] : null
}

describe('frame verifiers', () => {
  function leaf(id: string, key: Uint8Array) {
    return { nodeType: 'leaf', leaf: { credential: { credentialType: 'basic', identity: new TextEncoder().encode(id) }, signaturePublicKey: key } }
  }

  it('gives every member with exactly one leaf its leaf key, and nobody else', async () => {
    const raw = async () => {
      const pair = await webcrypto.subtle.generateKey('Ed25519', true, ['sign', 'verify']) as CryptoKeyPair
      return new Uint8Array(await webcrypto.subtle.exportKey('raw', pair.publicKey))
    }
    const tree = [leaf(aliceId, await raw()), null, leaf(bobId, await raw()), null, leaf(bobId, await raw()), null, leaf('not-a-member', await raw())]
    const verifiers = await frameVerifiers({ ratchetTree: tree } as unknown as mls.ClientState)
    expect([...verifiers?.keys() ?? []]).toEqual([aliceId])
    expect(await frameVerifiers({ ratchetTree: [leaf(aliceId, new Uint8Array(3))] } as unknown as mls.ClientState)).toBeNull()
  })

  it('hands each epoch the verifying keys of the members in it', async () => {
    const { alice, bob, aliceStep, bobStep } = await joinedPair()
    for (const step of [aliceStep, bobStep]) expect([...step.key?.verifiers.keys() ?? []].sort()).toEqual([aliceId, bobId])
    expect(await roundTrip(aliceStep.key, bobStep.key, 1)).toEqual([9, 8, 7])
    expect(await roundTrip(bobStep.key, aliceStep.key, 1, bobId)).toEqual([9, 8, 7])
    // Bob's own signer does not make a frame alice's.
    expect(await roundTrip(bobStep.key, aliceStep.key, 1, aliceId)).toBeNull()
    await alice.close()
    await bob.close()
  }, 30_000)
})

describe('media group', () => {
  it('rejects an id that is not a member id', async () => {
    expect(await MediaGroup.open('alice', channelId)).toBeNull()
    expect(await MediaGroup.open(aliceId, 'room')).toBeNull()
  })

  it('seals one epoch for the people in the room and rotates it when someone leaves', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const groups = new Map([[aliceId, alice], [bobId, bob]])
    const alone = await alice.sync([aliceId])
    expect(alone.wires).toEqual([])
    expect(alone.epoch).toBe(0)
    const offer = await bob.sync([aliceId, bobId])
    expect(offer.key).toBeNull()
    expect(offer.wires).toHaveLength(1)
    const again = await bob.sync([aliceId, bobId])
    expect(again.wires).toEqual([])
    const decoded = mls.decodeMlsMessage(offer.wires[0] ?? new Uint8Array(), 0)
    expect(decoded?.[0].wireformat).toBe('mls_key_package')
    await alice.receive(carolId, offer.wires[0] ?? new Uint8Array())
    expect((await alice.sync([aliceId, bobId])).epoch).toBe(0)
    await deliver(groups, bobId, offer.wires, [aliceId, bobId])
    const aliceJoined = await alice.sync([aliceId, bobId])
    const bobJoined = await bob.sync([aliceId, bobId])
    expect(aliceJoined.epoch).toBe(1)
    expect(bobJoined.epoch).toBe(1)
    expect(await roundTrip(aliceJoined.key, bobJoined.key, 1)).toEqual([9, 8, 7])
    expect(alone.verification?.members).toEqual([aliceId])
    expect(offer.verification).toBeNull()
    expect(aliceJoined.verification).toEqual(bobJoined.verification)
    expect(aliceJoined.verification).toMatchObject({ epoch: 1, members: [aliceId, bobId] })
    expect(aliceJoined.verification?.code).not.toBe(alone.verification?.code)
    await alice.receive(bobId, new Uint8Array([1, 2, 3]))
    await alice.receive(aliceId, offer.wires[0] ?? new Uint8Array())
    await alice.receive('not-a-member', offer.wires[0] ?? new Uint8Array())
    await alice.receive(bobId, new Uint8Array(HANDSHAKE_MAX_BYTES + 1))
    const left = await alice.sync([aliceId])
    await deliver(groups, aliceId, left.wires)
    const aliceAlone = await alice.sync([aliceId])
    const bobLeft = await bob.sync([aliceId])
    expect(aliceAlone.epoch).toBe(2)
    expect(aliceAlone.verification).toMatchObject({ epoch: 2, members: [aliceId] })
    expect(bobLeft.key).toBeNull()
    expect(bobLeft.verification).toBeNull()
    expect(await roundTrip(aliceAlone.key, aliceAlone.key, 2)).toEqual([9, 8, 7])
    await alice.close()
    expect((await alice.sync([aliceId])).wires).toEqual([])
    await bob.close()
  }, 30_000)

  it('adds members whose packages arrived together in one commit when the call batches', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const carol = await opened(carolId)
    const roster = [aliceId, bobId, carolId]
    await alice.sync([aliceId])
    const bobOffer = await bob.sync(roster)
    const carolOffer = await carol.sync(roster)
    expect((await alice.receive(bobId, wireAt(bobOffer.wires, 0), true)).wires).toEqual([])
    expect((await alice.receive(carolId, wireAt(carolOffer.wires, 0), true)).wires).toEqual([])
    const committed = await alice.sync(roster)
    expect(committed.epoch).toBe(1)
    // One commit and one welcome for both (beside the founder's announcement).
    const kinds = committed.wires.map(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat).filter(kind => kind !== 'mls_key_package')
    expect(kinds.sort()).toEqual(['mls_public_message', 'mls_welcome'])
    const groups = new Map([[aliceId, alice], [bobId, bob], [carolId, carol]])
    await deliver(groups, aliceId, committed.wires, roster)
    const keys = await Promise.all([bob, carol].map(group => group.sync(roster)))
    for (const step of keys) expect(step.epoch).toBe(1)
    expect(await roundTrip(committed.key, keys[0]?.key ?? null, 1)).toEqual([9, 8, 7])
    for (const group of groups.values()) await group.close()
  }, 30_000)

  it('drops a solo group when a lower id is already in the room', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const groups = new Map([[aliceId, alice], [bobId, bob]])
    expect((await bob.sync([bobId])).epoch).toBe(0)
    expect((await alice.sync([aliceId])).epoch).toBe(0)
    const dropped = await bob.sync([aliceId, bobId])
    expect(dropped.key).toBeNull()
    expect(dropped.wires.length).toBeGreaterThan(0)
    await deliver(groups, bobId, dropped.wires, [aliceId, bobId])
    const sealed = await alice.sync([aliceId, bobId])
    const joined = await bob.sync([aliceId, bobId])
    expect(sealed.epoch).toBe(1)
    expect(joined.epoch).toBe(1)
    expect(await roundTrip(sealed.key, joined.key, 1)).toEqual([9, 8, 7])
    await alice.close()
    const removed = await bob.sync([bobId])
    expect(removed.wires).toHaveLength(1)
    expect(removed.epoch).toBe(2)
    await bob.close()
  }, 30_000)

  it('lets the lowest id into a full room\'s group instead of rebuilding it', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const carol = await opened(carolId)
    const groups = new Map([[aliceId, alice], [bobId, bob], [carolId, carol]])
    const room = new Map([[bobId, bob], [carolId, carol]])
    const roster = [aliceId, bobId, carolId]
    expect((await bob.sync([bobId])).epoch).toBe(0)
    await deliver(room, carolId, (await carol.sync([bobId, carolId])).wires, [bobId, carolId])
    expect((await bob.sync([bobId, carolId])).epoch).toBe(1)
    // Others are here, so Alice offers instead of founding her own group.
    const offered = await alice.sync(roster)
    expect(offered.epoch).toBeNull()
    expect(offered.wires.map(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat)).toEqual(['mls_key_package'])
    // Bob and Carol keep their group, and Bob lets her in.
    expect((await bob.sync(roster)).epoch).toBe(1)
    await deliver(groups, aliceId, offered.wires, roster)
    const shared = await alice.sync(roster)
    const bobShared = await bob.sync(roster)
    const carolShared = await carol.sync(roster)
    expect(shared.epoch).toBe(2)
    expect(bobShared.epoch).toBe(2)
    expect(carolShared.epoch).toBe(2)
    expect(await roundTrip(shared.key, bobShared.key, 2)).toEqual([9, 8, 7])
    expect(await roundTrip(shared.key, carolShared.key, 2)).toEqual([9, 8, 7])
    // Now the lowest, Alice commits: Bob leaving is her commit.
    const removed = await alice.sync([aliceId, carolId])
    expect(removed.epoch).toBe(3)
    for (const group of groups.values()) await group.close()
  }, 30_000)

  it('founds at once when everyone else here is offering too', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const roster = [aliceId, bobId]
    // Both arrive at the same moment: nobody holds a group yet.
    const aliceOffer = await alice.sync(roster)
    const bobOffer = await bob.sync(roster)
    expect(aliceOffer.epoch).toBeNull()
    await alice.receive(bobId, wireAt(bobOffer.wires, 0), true)
    const founded = await alice.sync(roster)
    expect(founded.epoch).toBe(1)
    const groups = new Map([[aliceId, alice], [bobId, bob]])
    await deliver(groups, aliceId, founded.wires, roster)
    const joined = await bob.sync(roster)
    expect(joined.epoch).toBe(1)
    expect(await roundTrip(founded.key, joined.key, 1)).toEqual([9, 8, 7])
    await alice.close()
    await bob.close()
  }, 30_000)

  it('rejects a welcome before the roster is known', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const fresh = await opened(fixtureId(5))
    await alice.sync([aliceId])
    const offer = await bob.sync([aliceId, bobId])
    const channel = mediaGroupId(channelId)
    if (!channel) throw new Error('expected a group id')
    expect((await fresh.receive(aliceId, await welcomeFrom(aliceId, channel, wireAt(offer.wires, 0)))).key).toBeNull()
    await alice.close()
    await bob.close()
    await fresh.close()
  }, 30_000)

  it('forgets a group that never started', async () => {
    const idle = await opened(aliceId)
    await idle.close()
    expect((await idle.sync([aliceId])).wires).toEqual([])
  })

  it('publishes nothing while the group has no state', async () => {
    const idle = await opened(aliceId)
    const guarded = idle as unknown as {
      commitRemoves(others: readonly string[]): Promise<Uint8Array[]>
      commitAdds(others: readonly string[]): Promise<Uint8Array[]>
      commit(proposals: readonly []): Promise<Uint8Array[]>
    }
    expect(await guarded.commitRemoves([bobId])).toEqual([])
    expect(await guarded.commitAdds([bobId])).toEqual([])
    expect(await guarded.commit([])).toEqual([])
    const started = await idle.sync([aliceId])
    expect(started.wires).toEqual([])
    expect(started.epoch).toBe(0)
    await idle.close()
  })

  it('welcomes a member again with a fresh key package after the first welcome was missed', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    await alice.sync([aliceId])
    const offer = await bob.sync([aliceId, bobId, carolId])
    const keyPackage = wireAt(offer.wires, 0)
    await alice.sync([aliceId, bobId, carolId])
    const added = await alice.receive(bobId, keyPackage)
    expect(added.epoch).toBe(1)
    expect((await bob.sync([aliceId, bobId, carolId])).wires).toEqual([])
    // Bob never got the welcome and asks again. The repeat of an old package
    // changes nothing, a fresh one replaces his leaf.
    const resent = await bob.sync([aliceId, bobId, carolId], true)
    expect(resent.wires).toHaveLength(1)
    expect(wireAt(resent.wires, 0)).not.toEqual(keyPackage)
    await alice.sync([aliceId, bobId, carolId])
    expect((await alice.receive(bobId, keyPackage)).wires).toEqual([])
    const healed = await alice.receive(bobId, wireAt(resent.wires, 0))
    expect(healed.wires).toHaveLength(2)
    expect(healed.epoch).toBe(2)
    expect((await alice.receive(bobId, wireAt(resent.wires, 0))).wires).toEqual([])
    expect((await alice.receive(bobId, keyPackage)).wires).toEqual([])
    await bob.receive(aliceId, wireAt(healed.wires, 0))
    const joined = await bob.receive(aliceId, wireAt(healed.wires, 1))
    expect(joined.epoch).toBe(2)
    expect(await roundTrip(healed.key, joined.key, 2)).toEqual([9, 8, 7])
    await alice.close()
    await bob.close()
  }, 30_000)

  it('ignores a repeated key package when this client cannot commit', async () => {
    const { alice, bob } = await joinedPair()
    const carol = await opened(carolId)
    const offer = await carol.sync([aliceId, bobId, carolId])
    const keyPackage = wireAt(offer.wires, 0)
    await bob.sync([aliceId, bobId, carolId])
    expect((await bob.receive(carolId, keyPackage)).wires).toEqual([])
    await alice.sync([aliceId, bobId, carolId])
    const added = await alice.receive(carolId, keyPackage)
    expect((await bob.receive(aliceId, wireAt(added.wires, 1))).epoch).toBe(added.epoch)
    expect((await bob.receive(carolId, keyPackage)).wires).toEqual([])
    expect((await bob.sync([aliceId, bobId, carolId])).epoch).toBe(added.epoch)
    await alice.close()
    await bob.close()
    await carol.close()
  }, 30_000)

  it('removes a committer who left and ignores a pending member who is gone', async () => {
    const { alice, bob, bobStep } = await joinedPair()
    const carol = await opened(carolId)
    const offer = await carol.sync([aliceId, bobId, carolId])
    const stored = await bob.receive(carolId, wireAt(offer.wires, 0))
    expect(stored.wires).toEqual([])
    expect(stored.epoch).toBe(bobStep.epoch)
    await alice.close()
    const rotated = await bob.sync([bobId])
    expect(rotated.wires).toHaveLength(1)
    expect(mls.decodeMlsMessage(wireAt(rotated.wires, 0), 0)?.[0].wireformat).toBe('mls_public_message')
    expect(rotated.epoch).toBe((bobStep.epoch ?? 0) + 1)
    expect(await roundTrip(rotated.key, bobStep.key, rotated.epoch ?? 0, bobId)).toBeNull()
    expect(await roundTrip(rotated.key, rotated.key, rotated.epoch ?? 0, bobId)).toEqual([9, 8, 7])
    await bob.close()
    await carol.close()
  }, 30_000)

  it('rejects the wrong group, the wrong sender and a repeated welcome', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const carol = await opened(carolId)
    await alice.sync([aliceId])
    const offer = await bob.sync([aliceId, bobId])
    const keyPackage = wireAt(offer.wires, 0)
    const carolOffer = await carol.sync([aliceId, bobId, carolId])
    expect((await carol.receive(bobId, await welcomeFrom(bobId, new Uint8Array([1, 2, 3, 4]), wireAt(carolOffer.wires, 0)))).key).toBeNull()
    expect((await bob.receive(carolId, await welcomeFrom(carolId, new Uint8Array([1, 2, 3, 4]), keyPackage))).key).toBeNull()
    const otherChannel = mediaGroupId(fixtureId(8))
    if (!otherChannel) throw new Error('expected a group id')
    expect((await bob.receive(aliceId, await welcomeFrom(aliceId, new Uint8Array([9, 9, 9, 9]), keyPackage))).key).toBeNull()
    expect((await bob.receive(aliceId, await welcomeFrom(aliceId, otherChannel, keyPackage))).key).toBeNull()
    await alice.sync([aliceId, bobId])
    const committed = await alice.receive(bobId, keyPackage)
    const welcome = wireAt(committed.wires, 0)
    const joined = await bob.receive(aliceId, welcome)
    const replay = await bob.receive(aliceId, welcome)
    expect(joined.epoch).toBe(1)
    expect(replay.epoch).toBe(1)
    expect(replay.wires).toEqual([])
    expect(await roundTrip(committed.key, joined.key, 1)).toEqual([9, 8, 7])
    const dave = await opened(fixtureId(4))
    expect((await dave.receive(aliceId, wireAt(committed.wires, 1))).key).toBeNull()
    await alice.close()
    await bob.close()
    await carol.close()
    await dave.close()
  }, 30_000)

  it('rejects a key package that is not a member credential', async () => {
    const alice = await opened(aliceId)
    expect((await alice.sync([aliceId])).epoch).toBe(0)
    const cs = await mls.getCiphersuiteImpl(mls.getCiphersuiteFromName('MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519'))
    const x509 = await mls.generateKeyPackage(
      { credentialType: 'x509', certificates: [new Uint8Array([1, 2, 3])] },
      mls.defaultCapabilities(), mls.defaultLifetime, [], cs,
    )
    const named = await mls.generateKeyPackage(
      { credentialType: 'basic', identity: new TextEncoder().encode('not-a-member') },
      mls.defaultCapabilities(), mls.defaultLifetime, [], cs,
    )
    const certificate = mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: x509.publicPackage })
    const person = mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: named.publicPackage })
    expect((await alice.receive(bobId, certificate)).wires).toEqual([])
    expect((await alice.receive(bobId, person)).epoch).toBe(0)
    await alice.close()
  }, 30_000)

})

function wireAt(wires: Uint8Array[], index: number): Uint8Array {
  const wire = wires[index]
  if (!wire) throw new Error('expected a handshake wire')
  return wire
}

async function joinedPair() {
  const alice = await opened(aliceId)
  const bob = await opened(bobId)
  const groups = new Map([[aliceId, alice], [bobId, bob]])
  await alice.sync([aliceId])
  const offer = await bob.sync([aliceId, bobId])
  await deliver(groups, bobId, offer.wires, [aliceId, bobId])
  const aliceStep = await alice.sync([aliceId, bobId])
  const bobStep = await bob.sync([aliceId, bobId])
  if (aliceStep.epoch !== 1 || bobStep.epoch !== 1) throw new Error('expected the pair to share epoch 1')
  return { alice, bob, groups, aliceStep, bobStep }
}

async function welcomeFrom(signerId: string, groupId: Uint8Array, joiner: Uint8Array): Promise<Uint8Array> {
  const cs = await mls.getCiphersuiteImpl(mls.getCiphersuiteFromName('MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519'))
  const own = await mls.generateKeyPackage(
    { credentialType: 'basic', identity: new TextEncoder().encode(signerId) },
    mls.defaultCapabilities(), mls.defaultLifetime, [], cs,
  )
  const state = await mls.createGroup(groupId, own.publicPackage, own.privatePackage, [], cs, {
    keyRetentionConfig: mls.defaultKeyRetentionConfig,
    lifetimeConfig: mls.defaultLifetimeConfig,
    keyPackageEqualityConfig: mls.defaultKeyPackageEqualityConfig,
    paddingConfig: mls.defaultPaddingConfig,
    authService: {
      async validateCredential(credential: Credential) {
        if (credential.credentialType !== 'basic') return false
        const text = new TextDecoder().decode(credential.identity)
        return mediaUserId(text) === text
      },
    },
  })
  const decoded = mls.decodeMlsMessage(joiner, 0)
  const message = decoded?.[0]
  if (!message || message.wireformat !== 'mls_key_package') throw new Error('expected a key package')
  const committed = await mls.createCommit(
    { state, cipherSuite: cs },
    { extraProposals: [{ proposalType: 'add', add: { keyPackage: message.keyPackage } }], wireAsPublicMessage: true, ratchetTreeExtension: true },
  )
  if (!committed.welcome) throw new Error('expected a welcome')
  return mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_welcome', welcome: committed.welcome })
}

