import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import type { Credential } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { HANDSHAKE_MAX_BYTES, mediaGroupId, mediaUserId } from './bounds'
import { openFrame, sealFrame } from './frame'
import { MediaGroup, type MediaStep } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const daveId = fixtureId(4)
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
    await deliver(groups, bobId, offer.wires, [aliceId, bobId])
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

  it('builds a group for the lowest id already looking at a full room', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const carol = await opened(carolId)
    const groups = new Map([[aliceId, alice], [bobId, bob], [carolId, carol]])
    const room = new Map([[bobId, bob], [carolId, carol]])
    expect((await bob.sync([bobId])).epoch).toBe(0)
    await deliver(room, carolId, (await carol.sync([bobId, carolId])).wires, [bobId, carolId])
    expect((await bob.sync([bobId, carolId])).epoch).toBe(1)
    expect((await carol.sync([bobId, carolId])).epoch).toBe(1)
    const founded = await alice.sync([aliceId, bobId, carolId])
    expect(founded.epoch).toBe(0)
    // The new group announces itself, so the old members rejoin it at once.
    expect(founded.wires).toHaveLength(1)
    expect(mls.decodeMlsMessage(wireAt(founded.wires, 0), 0)?.[0].wireformat).toBe('mls_key_package')
    await deliver(groups, aliceId, founded.wires, [aliceId, bobId, carolId])
    const bobDropped = await bob.sync([aliceId, bobId, carolId])
    const carolDropped = await carol.sync([aliceId, bobId, carolId])
    await deliver(groups, bobId, bobDropped.wires, [aliceId, bobId, carolId])
    await deliver(groups, carolId, carolDropped.wires, [aliceId, bobId, carolId])
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
    vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
    const swapped = await alice.receive(bobId, firstOffer)
    expect(swapped.wires).toHaveLength(2)
    expect((await alice.receive(bobId, firstOffer)).wires).toEqual([])
    await deliver(groups, aliceId, swapped.wires)
    const second = await opened(bobId)
    // Another swap of the same member so soon is held back.
    const tooSoon = await alice.receive(bobId, wireAt((await second.sync(roster)).wires, 0))
    expect(tooSoon.wires).toEqual([])
    vi.setSystemTime(Date.now() + 4000)
    await second.close()
    const third = await opened(bobId)
    const secondOffer = wireAt((await third.sync(roster)).wires, 0)
    const again = await alice.receive(bobId, secondOffer)
    expect(again.wires).toHaveLength(2)
    expect((await alice.receive(bobId, firstOffer)).wires).toEqual([])
    expect(again.epoch).toBe((swapped.epoch ?? 0) + 1)
    vi.useRealTimers()
    await alice.close()
    await first.close()
    await third.close()
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
    // Bob joins between them: only Alice hears him as a committer, and he
    // learns of Carol from the welcome, not from her package.
    const bobOffer = await bob.sync(roster)
    await alice.sync(roster)
    await carol.sync(roster)
    const second = await alice.receive(bobId, wireAt(bobOffer.wires, 0))
    for (const wire of second.wires) {
      await bob.receive(aliceId, wire)
      await carol.receive(aliceId, wire)
    }
    expect((await bob.sync(roster)).epoch).toBe(second.epoch)
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
    await deliver(groups, bobId, (await bob.sync([aliceId, bobId])).wires, [aliceId, bobId])
    await alice.sync([aliceId, bobId])
    await bob.sync([aliceId, bobId])
    // Carol joins; Bob's socket was down, so he misses the commit.
    const offer = await carol.sync(roster)
    await alice.sync(roster)
    const added = await alice.receive(carolId, wireAt(offer.wires, 0))
    await carol.receive(aliceId, wireAt(added.wires, 0))
    await carol.receive(aliceId, wireAt(added.wires, 1))
    // Dave leaves: the next commit is for an epoch Bob has not reached.
    const rotated = await alice.sync([aliceId, bobId])
    // The founder's announcement, then the commit.
    expect(rotated.wires).toHaveLength(2)
    const lost = await bob.receive(aliceId, wireAt(rotated.wires, 1))
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
    await alice.sync([aliceId, bobId, carolId])
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
    // Asking again right away changes nothing; later it is a new request with a new package.
    expect((await bob.recover([aliceId, bobId, carolId])).wires).toEqual([])
    vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
    expect((await bob.recover([aliceId, bobId, carolId])).wires).toEqual([])
    vi.setSystemTime(Date.now() + 11_000)
    const again = await bob.recover([aliceId, bobId, carolId])
    expect(again.wires).toHaveLength(1)
    expect(wireAt(again.wires, 0)).not.toEqual(wireAt(asked.wires, 0))
    vi.useRealTimers()
    await alice.close()
    await bob.close()
    await carol.close()
  }, 60_000)
})

describe('what a flood or a forgery can reset', () => {
  afterEach(() => { vi.useRealTimers() })

  async function ahead() {
    const { alice, bob } = await joinedPair()
    const roster = [aliceId, bobId, carolId, daveId]
    await alice.sync(roster)
    const carol = await opened(carolId)
    const dave = await opened(daveId)
    await alice.receive(carolId, wireAt((await carol.sync(roster)).wires, 0))
    // Bob misses the commit that adds Carol and sees only the one that adds Dave.
    const second = await alice.receive(daveId, wireAt((await dave.sync(roster)).wires, 0))
    return { alice, bob, carol, dave, roster, future: wireAt(second.wires, 1) }
  }

  it('ignores a commit for a later epoch from someone who is not in the group', async () => {
    const { alice, bob, carol, dave, future } = await ahead()
    const before = await bob.sync([aliceId, bobId])
    const forged = await bob.receive(carolId, future)
    expect(forged.wires).toEqual([])
    expect(forged.epoch).toBe(before.epoch)
    expect(forged.key).toBe(before.key)
    const real = await bob.receive(aliceId, future)
    expect(real.key).toBeNull()
    expect(real.wires).toHaveLength(1)
    for (const group of [alice, bob, carol, dave]) await group.close()
  }, 60_000)

  it('resets at most once per window and then waits longer', async () => {
    const { alice, bob, roster, carol, dave } = await ahead()
    vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
    const resets: number[] = []
    for (let second = 0; second < 100; second++) {
      if ((await bob.recover(roster)).wires.length) resets.push(second)
      vi.setSystemTime(Date.now() + 1000)
    }
    expect(resets).toEqual([0, 10, 30, 70])
    bob.opened()
    expect((await bob.recover(roster)).wires).toHaveLength(1)
    for (const group of [alice, bob, carol, dave]) await group.close()
  }, 60_000)

  it('tells a member who was removed while still here, and offers again only now and then', async () => {
    const { alice, bob } = await joinedPair()
    const notices: string[] = []
    bob.onNotice(notice => notices.push(notice))
    vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
    const roster = [aliceId, bobId]
    const removal = async () => {
      const removed = await alice.sync([aliceId])
      const commit = removed.wires.find(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_public_message')
      const step = await bob.receive(aliceId, commit ?? new Uint8Array())
      // Back in the room: Alice adds whoever offers.
      await alice.sync(roster)
      for (const wire of step.wires) for (const added of (await alice.receive(bobId, wire)).wires) await bob.receive(aliceId, added)
      return step
    }
    expect((await removal()).wires).toHaveLength(1)
    expect(notices).toEqual(['removed'])
    expect((await bob.sync(roster)).epoch).not.toBeNull()
    expect((await removal()).wires).toEqual([])
    expect(notices).toEqual(['removed', 'removed'])
    // The roster still says Bob is here, so he asks the ordinary way and is taken back.
    await alice.sync(roster)
    for (const wire of (await bob.sync(roster)).wires) for (const added of (await alice.receive(bobId, wire)).wires) await bob.receive(aliceId, added)
    expect((await bob.sync(roster)).epoch).not.toBeNull()
    vi.setSystemTime(Date.now() + 11_000)
    expect((await removal()).wires).toHaveLength(1)
    await alice.close()
    await bob.close()
  }, 60_000)
})

describe('a room that grows, loses a welcome or has a silent lowest member', () => {
  afterEach(() => { vi.useRealTimers() })
  const timers = { toFake: ['Date', 'setTimeout', 'clearTimeout'] as ('Date' | 'setTimeout' | 'clearTimeout')[] }

  async function form(ids: string[]) {
    const groups = new Map<string, MediaGroup>()
    for (const [index, id] of ids.entries()) {
      const group = await opened(id)
      groups.set(id, group)
      const roster = ids.slice(0, index + 1)
      for (const member of roster) await deliver(groups, member, (await (groups.get(member) as MediaGroup).sync(roster)).wires, roster)
    }
    return groups
  }

  async function epochs(groups: Map<string, MediaGroup>, roster: string[]) {
    const seen = new Map<string, { epoch: number | null, key: CryptoKey | null }>()
    for (const [id, group] of groups) {
      const step = await group.sync(roster)
      seen.set(id, { epoch: step.epoch, key: step.key })
    }
    return seen
  }

  it('adds a member between others without anyone starting over', async () => {
    const ids = [1, 2, 3, 4, 6, 7, 8].map(n => fixtureId(n))
    const groups = await form(ids)
    const before = await epochs(groups, ids)
    const everyone = [...ids, fixtureId(5)].sort()
    const joiner = await opened(fixtureId(5))
    groups.set(fixtureId(5), joiner)
    const tally = { wires: 0 }
    const offer = await joiner.sync(everyone)
    // Everyone learns of the new roster first, as the call does.
    for (const id of ids) {
      const step = await (groups.get(id) as MediaGroup).sync(everyone)
      await deliver(groups, id, step.wires, everyone, tally)
    }
    await deliver(groups, fixtureId(5), offer.wires, everyone, tally)
    const after = await epochs(groups, everyone)
    const epoch = after.get(ids[0] as string)?.epoch
    expect(epoch).toBe((before.get(ids[0] as string)?.epoch ?? 0) + 1)
    for (const [, value] of after) expect(value.epoch).toBe(epoch)
    // The joiner's package, the welcome and the commit; nobody else has to speak.
    expect(tally.wires).toBeLessThanOrEqual(5)
    expect(await roundTrip(after.get(ids[0] as string)?.key ?? null, after.get(fixtureId(5))?.key ?? null, epoch ?? 0)).toEqual([9, 8, 7])
    for (const group of groups.values()) await group.close()
  }, 120_000)

  it('offers again with a fresh package when the welcome never comes', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const roster = [aliceId, bobId]
    vi.useFakeTimers({ ...timers, now: Date.now() })
    await alice.sync([aliceId])
    await alice.sync(roster)
    const offer = await bob.sync(roster)
    const lost = await alice.receive(bobId, wireAt(offer.wires, 0))
    expect(lost.wires).toHaveLength(2)
    const steps: MediaStep[] = []
    bob.onStep(step => steps.push(step))
    await vi.advanceTimersByTimeAsync(7000)
    expect(steps).toEqual([])
    await vi.advanceTimersByTimeAsync(1200)
    await vi.waitFor(() => expect(steps).toHaveLength(1))
    const again = wireAt(steps[0]?.wires ?? [], 0)
    expect(again).not.toEqual(wireAt(offer.wires, 0))
    vi.useRealTimers()
    const healed = await alice.receive(bobId, again)
    expect(healed.wires).toHaveLength(2)
    await bob.receive(aliceId, wireAt(healed.wires, 0))
    expect((await bob.receive(aliceId, wireAt(healed.wires, 1))).epoch).toBe(healed.epoch)
    // With a group, it stops asking.
    vi.useFakeTimers({ ...timers, now: Date.now() })
    await vi.advanceTimersByTimeAsync(30_000)
    expect(steps).toHaveLength(1)
    await alice.close()
    await bob.close()
  }, 60_000)

  it('retries a restart the limit held back, once the limit lets go', async () => {
    const { alice, bob } = await joinedPair()
    const roster = [aliceId, bobId]
    vi.useFakeTimers({ ...timers, now: Date.now() })
    // Use up the limit: Bob starts over once and is welcomed back.
    const asked = await bob.recover(roster)
    await alice.sync(roster)
    const back = await alice.receive(bobId, wireAt(asked.wires, 0))
    for (const wire of back.wires) await bob.receive(aliceId, wire)
    expect((await bob.sync(roster)).epoch).toBe(back.epoch)
    // Alice comes back from a reload and says so; Bob's limit holds him back.
    await alice.close()
    const reloaded = await opened(aliceId)
    const steps: MediaStep[] = []
    bob.onStep(step => steps.push(step))
    const hello = await reloaded.sync(roster)
    expect((await bob.receive(aliceId, wireAt(hello.wires, 0))).wires).toEqual([])
    expect(steps).toEqual([])
    await vi.advanceTimersByTimeAsync(11_000)
    await vi.waitFor(() => expect(steps).toHaveLength(1))
    expect(mls.decodeMlsMessage(wireAt(steps[0]?.wires ?? [], 0), 0)?.[0].wireformat).toBe('mls_key_package')
    await reloaded.close()
    await bob.close()
  }, 60_000)

  it('retries an offer after a removal the limit held back', async () => {
    const { alice, bob } = await joinedPair()
    const roster = [aliceId, bobId]
    vi.useFakeTimers({ ...timers, now: Date.now() })
    const removal = async () => {
      const removed = await alice.sync([aliceId])
      const commit = removed.wires.find(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_public_message')
      const step = await bob.receive(aliceId, commit ?? new Uint8Array())
      await alice.sync(roster)
      return step
    }
    const first = await removal()
    for (const wire of first.wires) for (const added of (await alice.receive(bobId, wire)).wires) await bob.receive(aliceId, added)
    const steps: MediaStep[] = []
    bob.onStep(step => steps.push(step))
    expect((await removal()).wires).toEqual([])
    await vi.advanceTimersByTimeAsync(11_000)
    await vi.waitFor(() => expect(steps).toHaveLength(1))
    expect(steps[0]?.wires).toHaveLength(1)
    await alice.close()
    await bob.close()
  }, 60_000)

  it('lets the next lowest member lead when the lowest never speaks', async () => {
    const bob = await opened(bobId)
    const roster = [aliceId, bobId, carolId]
    vi.useFakeTimers({ ...timers, now: Date.now() })
    const steps: MediaStep[] = []
    bob.onStep(step => steps.push(step))
    expect((await bob.sync(roster)).epoch).toBeNull()
    await vi.advanceTimersByTimeAsync(14_000)
    expect(steps).toEqual([])
    await vi.advanceTimersByTimeAsync(1200)
    await vi.waitFor(() => expect(steps.length).toBeGreaterThan(0))
    expect(steps.at(-1)?.epoch).toBe(0)
    // Heard from again, the lowest counts: Bob's own group no longer has it.
    const alice = await opened(aliceId)
    const hello = await alice.sync(roster)
    await bob.receive(aliceId, wireAt(hello.wires, 0))
    expect((await bob.sync(roster)).key).toBeNull()
    await alice.close()
    await bob.close()
  }, 60_000)
})
