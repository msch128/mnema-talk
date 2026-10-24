import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import type { Credential, KeyPackage } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { mediaGroupId, mediaUserId } from './bounds'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys } from './frame'
import { MediaGroup } from './group'

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
    const keys = new Map<string, { epoch: number | null, key: EpochKeys | null }>()
    // The roster reaches everyone before any handshake that depends on it.
    for (const group of groups.values()) group.hint(roster)
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

  function agree(keys: Map<string, { epoch: number | null, key: EpochKeys | null }>) {
    const values = [...keys.values()]
    const epoch = values[0]?.epoch ?? null
    expect(epoch).not.toBeNull()
    for (const value of values) expect(value.epoch).toBe(epoch)
    const first = [...keys.keys()][0] ?? aliceId
    return Promise.all(values.map(value => roundTrip(values[0]?.key ?? null, value.key, epoch ?? 0, first)))
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

  it('is swapped back in by the next lowest when the lowest id reloads', async () => {
    const roster = [aliceId, bobId, carolId]
    const groups = await room(roster)
    const before = (await settle(groups, roster)).get(bobId)?.epoch ?? 0
    await groups.get(aliceId)?.close()
    groups.set(aliceId, await opened(aliceId))
    const keys = await settle(groups, roster)
    // One swap, not a new group.
    expect(keys.get(bobId)?.epoch).toBe(before + 1)
    expect(await agree(keys)).toEqual([[9, 8, 7], [9, 8, 7], [9, 8, 7]])
    for (const group of groups.values()) await group.close()
  }, 60_000)

  it('ignores a member repeating the package it founded with after its leaf changed', async () => {
    const roster = [aliceId, bobId, carolId]
    const groups = await room([...roster, daveId])
    await settle(groups, [...roster, daveId])
    // Removing a member commits with a path, which replaces the founder's leaf
    // HPKE key; its signature key stays.
    await groups.get(daveId)?.close()
    groups.delete(daveId)
    const settled = await settle(groups, roster)
    expect(await agree(settled)).toEqual([[9, 8, 7], [9, 8, 7], [9, 8, 7]])
    const before = settled.get(bobId)?.epoch
    // The package Alice founded with arrives again (a late copy, a resend).
    const alice = groups.get(aliceId) as MediaGroup
    const founding = mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: (alice as unknown as { own: { publicPackage: KeyPackage } }).own.publicPackage })
    await deliver(groups, aliceId, [founding])
    for (const id of [bobId, carolId]) {
      const step = await (groups.get(id) as MediaGroup).sync(roster)
      expect(step.epoch).toBe(before)
      expect(step.key).not.toBeNull()
    }
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

describe('a group of one', () => {
  it('announces itself once per room, and no longer once someone is in', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    expect((await alice.sync([aliceId])).wires).toEqual([])
    const hello = await alice.sync([aliceId, bobId])
    expect(hello.wires.map(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat)).toEqual(['mls_key_package'])
    expect((await alice.sync([aliceId, bobId])).wires).toEqual([])
    expect((await alice.sync([aliceId, bobId, carolId])).wires).toHaveLength(1)
    const offer = await bob.sync([aliceId, bobId])
    const added = await alice.receive(bobId, wireAt(offer.wires, 0))
    expect(added.epoch).toBe(1)
    expect((await alice.sync([aliceId, bobId, daveId])).wires.map(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat)).not.toContain('mls_key_package')
    await alice.close()
    await bob.close()
  }, 30_000)

  it('steps aside and offers itself when a lower id is in the room', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const roster = [aliceId, bobId]
    expect((await bob.sync([bobId])).epoch).toBe(0)
    const offer = await alice.sync(roster)
    await bob.receive(aliceId, wireAt(offer.wires, 0), true)
    const aside = await bob.sync(roster)
    expect(aside.key).toBeNull()
    expect(aside.wires.map(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat)).toEqual(['mls_key_package'])
    // Alice now has everyone's offer and founds at once.
    await alice.receive(bobId, wireAt(aside.wires, 0), true)
    const founded = await alice.sync(roster)
    expect(founded.epoch).toBe(1)
    await alice.close()
    await bob.close()
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

  async function laterPair() {
    const carol = await opened(carolId)
    const dave = await opened(daveId)
    const groups = new Map([[carolId, carol], [daveId, dave]])
    await carol.sync([carolId])
    await deliver(groups, daveId, (await dave.sync([carolId, daveId])).wires, [carolId, daveId])
    await carol.sync([carolId, daveId])
    await dave.sync([carolId, daveId])
    return { carol, dave }
  }
  const ownPackage = (group: MediaGroup) => mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: (group as unknown as { own: { publicPackage: KeyPackage } }).own.publicPackage })

  it('keeps its group when a lower member outside it sends a welcome that is not for it', async () => {
    const { carol, dave } = await laterPair()
    const before = (await carol.sync([aliceId, carolId, daveId])).epoch
    const stranger = await opened(bobId)
    const offer = await stranger.sync([aliceId, bobId])
    const welcome = await welcomeFrom(aliceId, mediaGroupId(channelId) as Uint8Array, wireAt(offer.wires, 0))
    for (let i = 0; i < 20; i++) {
      const step = await carol.receive(aliceId, welcome)
      expect(step.epoch).toBe(before)
      expect(step.key).not.toBeNull()
    }
    // Neither does a key package or a parseable handshake of any kind from outside the group.
    expect((await carol.receive(aliceId, wireAt(offer.wires, 0))).key).not.toBeNull()
    const alone = await opened(bobId)
    expect((await alone.sync([bobId])).epoch).toBe(0)
    expect((await alone.receive(aliceId, welcome)).key).not.toBeNull()
    for (const group of [carol, dave, stranger, alone]) await group.close()
  }, 30_000)

  describe('a rival welcome', () => {
    afterEach(() => { vi.useRealTimers() })

    it('replaces a group others are in only when it opens with my own package, and not more than rarely per sender', async () => {
      const { carol, dave } = await laterPair()
      const roster = [aliceId, carolId, daveId]
      const before = (await carol.sync(roster)).epoch
      vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
      const channel = mediaGroupId(channelId) as Uint8Array
      // A welcome for somebody else's package spends the sender's allowance and changes nothing.
      const stranger = await opened(bobId)
      const bogus = await welcomeFrom(aliceId, channel, wireAt((await stranger.sync([aliceId, bobId])).wires, 0))
      expect((await carol.receive(aliceId, bogus)).epoch).toBe(before)
      const real = await welcomeFrom(aliceId, channel, ownPackage(carol))
      expect((await carol.receive(aliceId, real)).epoch).toBe(before)
      // Later, the same sender proves itself.
      vi.setSystemTime(Date.now() + 61_000)
      const joined = await carol.receive(aliceId, real)
      expect(joined.verification?.members).toEqual([aliceId, carolId])
      for (const group of [carol, dave, stranger]) await group.close()
    }, 30_000)
  })

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
    await alice.sync([aliceId, bobId, carolId])
    await bob.sync([aliceId, bobId, carolId])
    await bob.receive(carolId, wireAt(offer.wires, 0))
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
