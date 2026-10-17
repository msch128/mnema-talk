import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { type MediaTiming } from './bounds'
import { MediaGroup } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const outsiderId = fixtureId(7)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { vi.useRealTimers() })

const timing: MediaTiming = { rotateMs: 1e9, rotateJitterMs: 0, absentMs: 4000, heartbeatMs: 1e9, maintenanceMs: 1000 }
const START = Date.UTC(2026, 0, 1)
const roster = [aliceId, bobId, carolId]

async function open(userId: string) {
  const group = await MediaGroup.open(userId, channelId, timing)
  if (!group) throw new Error('expected a media group')
  return group
}

async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[], ids: string[]) {
  const pending = wires.map(wire => ({ from, wire }))
  for (const group of groups.values()) group.hint(ids)
  while (pending.length) {
    const item = pending.shift()
    if (!item) break
    for (const [id, group] of groups) {
      if (id === item.from) continue
      const step = await group.receive(item.from, item.wire)
      for (const wire of step.wires) pending.push({ from: id, wire })
      const synced = await group.sync(ids)
      for (const wire of synced.wires) pending.push({ from: id, wire })
    }
  }
}

async function trio() {
  vi.useFakeTimers({ toFake: ['Date'], now: START })
  const groups = new Map<string, MediaGroup>()
  for (const id of roster) groups.set(id, await open(id))
  const alice = groups.get(aliceId) as MediaGroup
  await alice.sync([aliceId])
  for (const id of roster.slice(1)) {
    await deliver(groups, id, (await (groups.get(id) as MediaGroup).sync(roster)).wires, roster)
  }
  for (const group of groups.values()) await group.sync(roster)
  return { groups, alice, bob: groups.get(bobId) as MediaGroup, carol: groups.get(carolId) as MediaGroup }
}

interface Inside {
  state: mls.ClientState
  cs: mls.CiphersuiteImpl
  commit(proposals: mls.Proposal[]): Promise<Uint8Array[]>
}

/** A commit exactly as a member's own, modified browser would send it, whatever the rules say. */
async function forged(group: MediaGroup, extraProposals: mls.Proposal[]): Promise<Uint8Array> {
  const inside = group as unknown as Inside
  const result = await mls.createCommit({ state: inside.state, cipherSuite: inside.cs }, { extraProposals, wireAsPublicMessage: true, ratchetTreeExtension: true })
  return mls.encodeMlsMessage(result.commit)
}

function leafOf(group: MediaGroup, id: string): number {
  const inside = group as unknown as Inside
  const index = inside.state.ratchetTree.findIndex((node, i) => i % 2 === 0 && node?.nodeType === 'leaf' && node.leaf.credential.credentialType === 'basic' && new TextDecoder().decode(node.leaf.credential.identity) === id)
  return index / 2
}

async function packageFor(id: string) {
  const fresh = await open(id)
  const offer = (await fresh.sync([...new Set([...roster, id])])).wires[0] as Uint8Array
  const decoded = mls.decodeMlsMessage(offer, 0)
  if (decoded?.[0].wireformat !== 'mls_key_package') throw new Error('expected a key package')
  return { fresh, offer, keyPackage: decoded[0].keyPackage }
}

async function closeAll(groups: Iterable<MediaGroup>) {
  for (const group of groups) await group.close()
}

describe('what a member may do to the group with a commit', () => {
  it('ignores a commit from a member who is not the committer, and nobody discards anything', async () => {
    const { groups, alice, bob, carol } = await trio()
    const epoch = (await alice.sync(roster)).epoch
    // Carol, no committer, removes Bob.
    const removal = await forged(carol, [{ proposalType: 'remove', remove: { removed: leafOf(carol, bobId) } }])
    for (const group of [alice, bob]) {
      group.hint(roster)
      const step = await group.receive(carolId, removal)
      expect(step.epoch).toBe(epoch)
      expect(step.key).not.toBeNull()
      expect(step.wires).toEqual([])
    }
    await closeAll(groups.values())
  }, 60_000)

  it('refuses the committer adding a second leaf for an identity, or a package nobody relayed', async () => {
    const { groups, bob } = await trio()
    const alice = groups.get(aliceId) as MediaGroup
    const epoch = (await bob.sync(roster)).epoch
    const { fresh, offer, keyPackage } = await packageFor(carolId)
    // Carol's other package reaches Bob through the relay; Alice's add of it, next to the old leaf, is no replacement.
    await bob.receive(carolId, offer)
    const duplicate = await forged(alice, [{ proposalType: 'add', add: { keyPackage } }])
    expect((await bob.receive(aliceId, duplicate)).epoch).toBe(epoch)
    // A package made up for an identity that never sent one to Bob.
    const made = await packageFor(outsiderId)
    const unseen = await forged(alice, [{ proposalType: 'add', add: { keyPackage: made.keyPackage } }])
    bob.hint([...roster, outsiderId])
    expect((await bob.receive(aliceId, unseen)).epoch).toBe(epoch)
    // Seen, but the identity is not in the room.
    await bob.receive(outsiderId, made.offer)
    bob.hint(roster)
    await bob.sync(roster)
    expect((await bob.receive(aliceId, unseen)).epoch).toBe(epoch)
    await closeAll([...groups.values(), fresh, made.fresh])
  }, 60_000)

  it('refuses to key an epoch from a tree where an identity holds two leaves', async () => {
    const { groups, alice } = await trio()
    const { fresh, keyPackage } = await packageFor(carolId)
    const wires = await (alice as unknown as Inside).commit([{ proposalType: 'add', add: { keyPackage } }])
    expect(wires).toEqual([])
    expect((await alice.sync(roster)).verification?.members).toEqual([aliceId, bobId, carolId])
    await closeAll([...groups.values(), fresh])
  }, 60_000)

  it('removes only members that are gone, silent, or replaced', async () => {
    const { groups, alice, bob } = await trio()
    const epoch = (await bob.sync(roster)).epoch
    // Carol is in the room and alive: a removal of her is refused.
    const removeCarol = await forged(alice, [{ proposalType: 'remove', remove: { removed: leafOf(alice, carolId) } }])
    expect((await bob.receive(aliceId, removeCarol)).epoch).toBe(epoch)
    // Once she is gone from the roster, it is taken.
    bob.hint([aliceId, bobId])
    const taken = await bob.receive(aliceId, removeCarol)
    expect(taken.epoch).toBe((epoch ?? 0) + 1)
    expect(taken.verification?.members).toEqual([aliceId, bobId])
    await closeAll(groups.values())
  }, 60_000)

  it('takes a removal for silence only after the silence, by its own clock', async () => {
    const { groups, alice, bob } = await trio()
    const epoch = (await bob.sync(roster)).epoch
    const removeCarol = await forged(alice, [{ proposalType: 'remove', remove: { removed: leafOf(alice, carolId) } }])
    vi.setSystemTime(START + 2000)
    expect((await bob.receive(aliceId, removeCarol)).epoch).toBe(epoch)
    vi.setSystemTime(START + 5000)
    // Bob heard Carol's last sign of life at the start of the epoch and nothing since.
    expect((await bob.receive(aliceId, removeCarol)).epoch).toBe((epoch ?? 0) + 1)
    await closeAll(groups.values())
  }, 60_000)

  it('does not start over for a later-epoch commit that is not from the committer', async () => {
    const { groups, alice, bob } = await trio()
    const epoch = (await bob.sync(roster)).epoch
    // A real commit of Alice's for the epoch after this one, claimed by Carol: its leaf is not Carol's.
    const later = await (alice as unknown as Inside).commit([])
    const wire = later[0] as Uint8Array
    expect((await bob.receive(carolId, wire)).epoch).toBe(epoch)
    expect((await bob.receive(carolId, wire)).key).not.toBeNull()
    // The real thing from Alice, whose leaf it names: Bob would be behind a commit he missed, so he starts over.
    // (Here the commit is for the very next epoch, so he simply takes it.)
    expect((await bob.receive(aliceId, wire)).epoch).toBe((epoch ?? 0) + 1)
    await closeAll(groups.values())
  }, 60_000)
})
