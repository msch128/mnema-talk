import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import type { Credential } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { HANDSHAKE_MAX_BYTES, mediaGroupId, mediaUserId } from './bounds'
import { openFrame, sealFrame } from './frame'
import { MediaGroup } from './group'

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

async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[]) {
  const pending = wires.map(wire => ({ from, wire }))
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
    }
  }
}

async function roundTrip(left: CryptoKey | null, right: CryptoKey | null, epoch: number) {
  if (!left || !right) throw new Error('expected epoch keys')
  const packet = await sealFrame({ key: left, epoch, channelId, senderId: aliceId, plaintext: new Uint8Array([9, 8, 7]) })
  if (!packet) throw new Error('expected a sealed frame')
  const opened = await openFrame({ keys: new Map([[epoch, right]]), channelId, senderId: aliceId, packet })
  return opened ? [...opened] : null
}

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
    await deliver(groups, bobId, offer.wires)
    const aliceJoined = await alice.sync([aliceId, bobId])
    const bobJoined = await bob.sync([aliceId, bobId])
    expect(aliceJoined.epoch).toBe(1)
    expect(bobJoined.epoch).toBe(1)
    expect(await roundTrip(aliceJoined.key, bobJoined.key, 1)).toEqual([9, 8, 7])
    await alice.receive(bobId, new Uint8Array([1, 2, 3]))
    await alice.receive(aliceId, offer.wires[0] ?? new Uint8Array())
    await alice.receive('not-a-member', offer.wires[0] ?? new Uint8Array())
    await alice.receive(bobId, new Uint8Array(HANDSHAKE_MAX_BYTES + 1))
    const left = await alice.sync([aliceId])
    await deliver(groups, aliceId, left.wires)
    const aliceAlone = await alice.sync([aliceId])
    const bobLeft = await bob.sync([aliceId])
    expect(aliceAlone.epoch).toBe(2)
    expect(bobLeft.key).toBeNull()
    expect(await roundTrip(aliceAlone.key, aliceAlone.key, 2)).toEqual([9, 8, 7])
    await alice.close()
    expect((await alice.sync([aliceId])).wires).toEqual([])
    await bob.close()
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
    await deliver(groups, bobId, dropped.wires)
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

  it('builds a group for the lowest id already looking at a full room', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const carol = await opened(carolId)
    const groups = new Map([[aliceId, alice], [bobId, bob], [carolId, carol]])
    const room = new Map([[bobId, bob], [carolId, carol]])
    expect((await bob.sync([bobId])).epoch).toBe(0)
    await deliver(room, carolId, (await carol.sync([bobId, carolId])).wires)
    expect((await bob.sync([bobId, carolId])).epoch).toBe(1)
    expect((await carol.sync([bobId, carolId])).epoch).toBe(1)
    const founded = await alice.sync([aliceId, bobId, carolId])
    expect(founded.epoch).toBe(0)
    expect(founded.wires).toEqual([])
    const bobDropped = await bob.sync([aliceId, bobId, carolId])
    const carolDropped = await carol.sync([aliceId, bobId, carolId])
    expect(bobDropped.key).toBeNull()
    expect(carolDropped.key).toBeNull()
    await deliver(groups, bobId, bobDropped.wires)
    await deliver(groups, carolId, carolDropped.wires)
    const shared = await alice.sync([aliceId, bobId, carolId])
    const bobShared = await bob.sync([aliceId, bobId, carolId])
    const carolShared = await carol.sync([aliceId, bobId, carolId])
    expect(shared.epoch).toBeGreaterThan(0)
    expect(bobShared.epoch).toBe(shared.epoch)
    expect(carolShared.epoch).toBe(shared.epoch)
    expect(await roundTrip(shared.key, bobShared.key, shared.epoch ?? 0)).toEqual([9, 8, 7])
    expect(await roundTrip(shared.key, carolShared.key, shared.epoch ?? 0)).toEqual([9, 8, 7])
    await alice.close()
    const removed = await bob.sync([bobId, carolId])
    expect(removed.wires.length).toBeGreaterThan(0)
    expect(removed.epoch).toBe((shared.epoch ?? 0) + 1)
    await bob.close()
    await carol.close()
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

  it('welcomes the same key package again after the first welcome was missed', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    await alice.sync([aliceId])
    const offer = await bob.sync([aliceId, bobId, carolId])
    const keyPackage = wireAt(offer.wires, 0)
    const added = await alice.receive(bobId, keyPackage)
    expect(added.epoch).toBe(1)
    expect((await bob.sync([aliceId, bobId, carolId])).wires).toEqual([])
    const resent = await bob.sync([aliceId, bobId, carolId], true)
    expect(resent.wires).toHaveLength(1)
    await alice.sync([aliceId, bobId, carolId])
    const other = await opened(bobId)
    const otherOffer = await other.sync([aliceId, bobId])
    expect((await alice.receive(bobId, wireAt(otherOffer.wires, 0))).wires).toEqual([])
    const healed = await alice.receive(bobId, wireAt(resent.wires, 0))
    expect(healed.wires.length).toBeGreaterThan(1)
    expect(healed.epoch).toBeGreaterThan(1)
    const late = await bob.receive(aliceId, wireAt(added.wires, 0))
    expect(late.epoch).toBe(1)
    const dropped = await bob.receive(aliceId, wireAt(healed.wires, 0))
    expect(dropped.key).toBeNull()
    let joined = dropped
    for (const wire of healed.wires.slice(1)) joined = await bob.receive(aliceId, wire)
    expect(joined.epoch).toBe(healed.epoch)
    expect(await roundTrip(healed.key, joined.key, healed.epoch ?? 0)).toEqual([9, 8, 7])
    await alice.close()
    await bob.close()
    await other.close()
  }, 30_000)

  it('ignores a repeated key package when this client cannot commit', async () => {
    const { alice, bob } = await joinedPair()
    const carol = await opened(carolId)
    const offer = await carol.sync([aliceId, bobId, carolId])
    const keyPackage = wireAt(offer.wires, 0)
    expect((await bob.receive(carolId, keyPackage)).wires).toEqual([])
    const added = await alice.receive(carolId, keyPackage)
    expect((await bob.receive(aliceId, wireAt(added.wires, 1))).epoch).toBe(added.epoch)
    expect((await bob.receive(carolId, keyPackage)).wires).toEqual([])
    expect((await bob.sync([aliceId, bobId, carolId])).epoch).toBe(added.epoch)
    await alice.close()
    await bob.close()
    await carol.close()
  }, 30_000)

  it('drops its own group when a lower member sends a key package', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    expect((await bob.sync([bobId])).epoch).toBe(0)
    const lower = '00000000-0000-4000-8000-000000000000'
    const offer = await alice.sync([lower, aliceId])
    const dropped = await bob.receive(aliceId, wireAt(offer.wires, 0))
    expect(dropped.key).toBeNull()
    const next = await bob.sync([aliceId, bobId])
    expect(next.key).toBeNull()
    expect(next.wires.length).toBeGreaterThan(0)
    await alice.close()
    await bob.close()
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
    expect(await roundTrip(rotated.key, bobStep.key, rotated.epoch ?? 0)).toBeNull()
    expect(await roundTrip(rotated.key, rotated.key, rotated.epoch ?? 0)).toEqual([9, 8, 7])
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
  await deliver(groups, bobId, offer.wires)
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
