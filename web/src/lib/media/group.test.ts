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
    // The new group announces itself, so the old members rejoin it at once.
    expect(founded.wires).toHaveLength(1)
    expect(mls.decodeMlsMessage(wireAt(founded.wires, 0), 0)?.[0].wireformat).toBe('mls_key_package')
    await deliver(groups, aliceId, founded.wires)
    const bobDropped = await bob.sync([aliceId, bobId, carolId])
    const carolDropped = await carol.sync([aliceId, bobId, carolId])
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

  it('welcomes a member again with a fresh key package after the first welcome was missed', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    await alice.sync([aliceId])
    const offer = await bob.sync([aliceId, bobId, carolId])
    const keyPackage = wireAt(offer.wires, 0)
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

describe('a member who starts over inside the grace period', () => {
  async function room(ids: string[]) {
    const groups = new Map<string, MediaGroup>()
    for (const id of ids) groups.set(id, await opened(id))
    const first = ids[0] as string
    await groups.get(first)?.sync([first])
    for (const id of ids.slice(1)) {
      const roster = ids.slice(0, ids.indexOf(id) + 1)
      for (const member of roster) {
        const group = groups.get(member) as MediaGroup
        await deliver(groups, member, (await group.sync(roster)).wires)
      }
    }
    return groups
  }

  async function settle(groups: Map<string, MediaGroup>, roster: string[]) {
    const keys = new Map<string, { epoch: number | null, key: CryptoKey | null }>()
    for (let round = 0; round < 3; round++) {
      for (const [id, group] of groups) {
        const step = await group.sync(roster)
        keys.set(id, { epoch: step.epoch, key: step.key })
        await deliver(groups, id, step.wires)
      }
    }
    for (const [id, group] of groups) {
      const step = await group.sync(roster)
      keys.set(id, { epoch: step.epoch, key: step.key })
    }
    return keys
  }

  function agree(keys: Map<string, { epoch: number | null, key: CryptoKey | null }>) {
    const values = [...keys.values()]
    const epoch = values[0]?.epoch ?? null
    expect(epoch).not.toBeNull()
    for (const value of values) expect(value.epoch).toBe(epoch)
    return Promise.all(values.map(value => roundTrip(values[0]?.key ?? null, value.key, epoch ?? 0)))
  }

  it('is swapped in by the committer when a higher id reloads', async () => {
    const roster = [aliceId, bobId, carolId]
    const groups = await room(roster)
    expect(await agree(await settle(groups, roster))).toEqual([[9, 8, 7], [9, 8, 7], [9, 8, 7]])
    const before = (await (groups.get(aliceId) as MediaGroup).sync(roster)).epoch ?? 0
    await groups.get(bobId)?.close()
    const reloaded = await opened(bobId)
    groups.set(bobId, reloaded)
    const keys = await settle(groups, roster)
    expect(keys.get(aliceId)?.epoch).toBe(before + 1)
    expect(await agree(keys)).toEqual([[9, 8, 7], [9, 8, 7], [9, 8, 7]])
    for (const group of groups.values()) await group.close()
  }, 60_000)

  it('is rejoined by the others when the lowest id reloads', async () => {
    const roster = [aliceId, bobId, carolId]
    const groups = await room(roster)
    await settle(groups, roster)
    await groups.get(aliceId)?.close()
    groups.set(aliceId, await opened(aliceId))
    const keys = await settle(groups, roster)
    expect(await agree(keys)).toEqual([[9, 8, 7], [9, 8, 7], [9, 8, 7]])
    for (const group of groups.values()) await group.close()
  }, 60_000)

  it('treats a repeated or an old key package as nothing', async () => {
    const roster = [aliceId, bobId]
    const groups = await room(roster)
    await settle(groups, roster)
    const alice = groups.get(aliceId) as MediaGroup
    const first = await opened(bobId)
    const firstOffer = wireAt((await first.sync(roster)).wires, 0)
    await groups.get(bobId)?.close()
    groups.set(bobId, first)
    const swapped = await alice.receive(bobId, firstOffer)
    expect(swapped.wires).toHaveLength(2)
    expect((await alice.receive(bobId, firstOffer)).wires).toEqual([])
    await deliver(groups, aliceId, swapped.wires)
    const second = await opened(bobId)
    const secondOffer = wireAt((await second.sync(roster)).wires, 0)
    const again = await alice.receive(bobId, secondOffer)
    expect(again.wires).toHaveLength(2)
    expect((await alice.receive(bobId, firstOffer)).wires).toEqual([])
    expect(again.epoch).toBe((swapped.epoch ?? 0) + 1)
    await alice.close()
    await first.close()
    await second.close()
  }, 60_000)

  it('does not swap a leaf when this client is not the committer', async () => {
    const roster = [aliceId, bobId, carolId]
    const groups = await room(roster)
    await settle(groups, roster)
    const bob = groups.get(bobId) as MediaGroup
    const fresh = await opened(carolId)
    const offer = wireAt((await fresh.sync(roster)).wires, 0)
    const before = (await bob.sync(roster)).epoch
    expect((await bob.receive(carolId, offer)).wires).toEqual([])
    expect((await bob.sync(roster)).epoch).toBe(before)
    for (const group of groups.values()) await group.close()
    await fresh.close()
  }, 60_000)
})

describe('a founder announcing a new group', () => {
  it('says so once per room, also when the roster arrives after the group', async () => {
    const alice = await opened(aliceId)
    expect((await alice.sync([aliceId])).wires).toEqual([])
    const hello = await alice.sync([aliceId, bobId])
    expect(hello.wires).toHaveLength(1)
    expect(mls.decodeMlsMessage(wireAt(hello.wires, 0), 0)?.[0].wireformat).toBe('mls_key_package')
    expect((await alice.sync([aliceId, bobId])).wires).toEqual([])
    expect((await alice.sync([aliceId, bobId, carolId])).wires).toHaveLength(1)
    await alice.close()
  }, 30_000)
})

describe('a member who takes over as committer', () => {
  it('swaps in a member whose first key package it never saw', async () => {
    const roster = [aliceId, bobId, carolId]
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const carol = await opened(carolId)
    await alice.sync([aliceId])
    const carolOffer = await carol.sync([aliceId, carolId])
    await alice.sync([aliceId, carolId])
    const first = await alice.receive(carolId, wireAt(carolOffer.wires, 0))
    for (const wire of first.wires) await carol.receive(aliceId, wire)
    // Bob joins; being lower than Carol, he makes her start over. Only Alice
    // hears Carol's second package, so Bob learns of Carol from a commit.
    const bobOffer = await bob.sync(roster)
    await alice.sync(roster)
    const second = await alice.receive(bobId, wireAt(bobOffer.wires, 0))
    const dropped = await carol.receive(bobId, wireAt(bobOffer.wires, 0))
    expect(dropped.key).toBeNull()
    for (const wire of second.wires) await bob.receive(aliceId, wire)
    const again = await carol.sync(roster)
    const third = await alice.receive(carolId, wireAt(again.wires.length ? again.wires : dropped.wires, 0))
    for (const wire of third.wires) {
      await bob.receive(aliceId, wire)
      await carol.receive(aliceId, wire)
    }
    expect((await bob.sync(roster)).epoch).toBe(third.epoch)
    await alice.close()
    const lone = [bobId, carolId]
    const taken = await bob.sync(lone)
    for (const wire of taken.wires) await carol.receive(bobId, wire)
    const reloaded = await opened(carolId)
    const offer = await reloaded.sync(lone)
    const swapped = await bob.receive(carolId, wireAt(offer.wires, 0))
    expect(swapped.wires).toHaveLength(2)
    await bob.close()
    await carol.close()
    await reloaded.close()
  }, 60_000)
})

describe('a member who missed a commit', () => {
  it('has nothing to recover after it closed', async () => {
    const alice = await opened(aliceId)
    await alice.close()
    expect((await alice.recover([bobId, aliceId])).wires).toEqual([])
  }, 30_000)

  it('drops its own group when a lower member it does not hold sends a welcome', async () => {
    const bob = await opened(bobId)
    expect((await bob.sync([bobId])).epoch).toBe(0)
    const carol = await opened(carolId)
    const offer = await carol.sync([aliceId, carolId])
    const welcome = await welcomeFrom(aliceId, mediaGroupId(channelId) as Uint8Array, wireAt(offer.wires, 0))
    const step = await bob.receive(aliceId, welcome)
    expect(step.key).toBeNull()
    await bob.close()
    await carol.close()
  }, 30_000)

  it('starts over when a commit for a later epoch arrives', async () => {
    const roster = [aliceId, bobId, carolId]
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const carol = await opened(carolId)
    const groups = new Map([[aliceId, alice], [bobId, bob], [carolId, carol]])
    await alice.sync([aliceId])
    await deliver(groups, bobId, (await bob.sync([aliceId, bobId])).wires)
    await alice.sync([aliceId, bobId])
    await bob.sync([aliceId, bobId])
    // Carol joins; Bob's socket was down, so he misses the commit.
    const offer = await carol.sync(roster)
    const added = await alice.receive(carolId, wireAt(offer.wires, 0))
    await carol.receive(aliceId, wireAt(added.wires, 0))
    await carol.receive(aliceId, wireAt(added.wires, 1))
    // Dave leaves: the next commit is for an epoch Bob has not reached.
    const rotated = await alice.sync([aliceId, bobId])
    expect(rotated.wires).toHaveLength(1)
    const lost = await bob.receive(aliceId, wireAt(rotated.wires, 0))
    expect(lost.key).toBeNull()
    expect(lost.wires).toHaveLength(1)
    expect(mls.decodeMlsMessage(wireAt(lost.wires, 0), 0)?.[0].wireformat).toBe('mls_key_package')
    const healed = await alice.receive(bobId, wireAt(lost.wires, 0))
    expect(healed.wires).toHaveLength(2)
    await bob.receive(aliceId, wireAt(healed.wires, 0))
    const joined = await bob.receive(aliceId, wireAt(healed.wires, 1))
    expect(joined.epoch).toBe(healed.epoch)
    expect(await roundTrip(healed.key, joined.key, healed.epoch ?? 0)).toEqual([9, 8, 7])
    await alice.close()
    await bob.close()
    await carol.close()
  }, 60_000)

  it('ignores a commit for an epoch it already has, and recovers on request', async () => {
    const { alice, bob } = await joinedPair()
    const carol = await opened(carolId)
    const offer = await carol.sync([aliceId, bobId, carolId])
    const added = await alice.receive(carolId, wireAt(offer.wires, 0))
    const commit = wireAt(added.wires, 1)
    await bob.receive(aliceId, commit)
    const same = await bob.receive(aliceId, commit)
    expect(same.wires).toEqual([])
    expect(same.epoch).toBe(added.epoch)
    // A member with nobody lower in the room has nobody to ask.
    expect((await alice.recover([aliceId, bobId])).wires).toEqual([])
    const asked = await bob.recover([aliceId, bobId, carolId])
    expect(asked.key).toBeNull()
    expect(asked.wires).toHaveLength(1)
    // Asking again is a new request with a new package.
    const again = await bob.recover([aliceId, bobId, carolId])
    expect(again.wires).toHaveLength(1)
    expect(wireAt(again.wires, 0)).not.toEqual(wireAt(asked.wires, 0))
    await alice.close()
    await bob.close()
    await carol.close()
  }, 60_000)
})
