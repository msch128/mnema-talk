import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { fixtureId } from '../../test-fixtures.fixture'
import { mediaGroupId, type MediaTiming } from './bounds'
import { CAP_FRAME_V3, isCapability, sealCapability } from './capability'
import { MediaGroup } from './group'
import { testMember } from './keys.fixture'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { vi.useRealTimers() })

const timing: MediaTiming = { rotateMs: 1e9, rotateJitterMs: 0, absentMs: 1e9, heartbeatMs: 1_000_000, maintenanceMs: 1000 }
const START = Date.UTC(2026, 0, 1)

async function open(userId: string) {
  const group = await MediaGroup.open(userId, channelId, timing)
  if (!group) throw new Error('expected a media group')
  return group
}

async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[], roster: string[], skip: (wire: Uint8Array, to: string) => boolean = () => false) {
  const pending = wires.map(wire => ({ from, wire }))
  for (const group of groups.values()) group.hint(roster)
  while (pending.length) {
    const item = pending.shift()
    if (!item) break
    for (const [id, group] of groups) {
      if (id === item.from || skip(item.wire, id)) continue
      const step = await group.receive(item.from, item.wire)
      for (const wire of step.wires) pending.push({ from: id, wire })
      const synced = await group.sync(roster)
      for (const wire of synced.wires) pending.push({ from: id, wire })
    }
  }
}

async function room(ids: string[]) {
  vi.useFakeTimers({ toFake: ['Date'], now: START })
  const groups = new Map<string, MediaGroup>()
  for (const id of ids) groups.set(id, await open(id))
  await (groups.get(ids[0] as string) as MediaGroup).sync([ids[0] as string])
  for (const id of ids.slice(1)) await deliver(groups, id, (await (groups.get(id) as MediaGroup).sync(ids)).wires, ids)
  for (const group of groups.values()) await group.sync(ids)
  return groups
}

const caps = (group: MediaGroup, roster: string[]) => group.sync(roster).then(step => step.caps)

describe('who opens frame version 3', () => {
  it('knows only itself at first: nothing is complete until every member said so', async () => {
    const roster = [aliceId, bobId]
    const groups = await room(roster)
    const alice = groups.get(aliceId) as MediaGroup
    const bob = groups.get(bobId) as MediaGroup
    expect(await caps(alice, roster)).toEqual({ members: [aliceId], complete: false })
    // A group of one is complete by itself.
    const solo = await room([aliceId])
    expect(await caps(solo.get(aliceId) as MediaGroup, [aliceId])).toEqual({ members: [aliceId], complete: true })
    // The first tick tells the other member what this one supports.
    const told = await alice.maintain()
    expect(told.wires.filter(isCapability)).toHaveLength(1)
    expect(await caps(alice, roster)).toEqual({ members: [aliceId], complete: false })
    const reply = await bob.receive(aliceId, told.wires[0] as Uint8Array)
    expect(reply.caps).toEqual({ members: [aliceId, bobId].sort(), complete: true })
    // Hearing a member for the first time is answered at once, and only once.
    expect(reply.wires.filter(isCapability)).toHaveLength(1)
    const back = await alice.receive(bobId, reply.wires[0] as Uint8Array)
    expect(back.caps).toEqual({ members: [aliceId, bobId].sort(), complete: true })
    // The answer to the answer is the last one: a member already known is not answered.
    expect(back.wires.filter(isCapability)).toHaveLength(1)
    expect((await bob.receive(aliceId, back.wires[0] as Uint8Array)).wires).toEqual([])
    expect((await bob.receive(aliceId, told.wires[0] as Uint8Array)).wires).toEqual([])
    await Promise.all([...groups.values(), ...solo.values()].map(group => group.close()))
  }, 60_000)

  it('ignores a capability nobody in the epoch signed, one with other bits and one of a stranger', async () => {
    const roster = [aliceId, bobId]
    const groups = await room(roster)
    const alice = groups.get(aliceId) as MediaGroup
    const bob = groups.get(bobId) as MediaGroup
    const groupId = mediaGroupId(channelId) as Uint8Array
    const mallory = await testMember()
    // Signed with a key that is not Bob's leaf key.
    const forged = await sealCapability(mallory.signer, groupId, bobId) as Uint8Array
    expect((await alice.receive(bobId, forged)).caps?.complete).toBe(false)
    // Bob's real message, claimed by Carol (not in the group) or replayed for another group.
    const real = (await bob.maintain()).wires.find(isCapability) as Uint8Array
    expect((await alice.receive(carolId, real)).caps?.complete).toBe(false)
    const other = await sealCapability(mallory.signer, new Uint8Array(16), aliceId) as Uint8Array
    expect((await alice.receive(bobId, other)).caps?.complete).toBe(false)
    // A member that signed no bits at all is not capable.
    const nothing = await sealCapability(mallory.signer, groupId, bobId, 0) as Uint8Array
    expect((await alice.receive(bobId, nothing)).caps?.complete).toBe(false)
    // Garbage with the right magic.
    expect((await alice.receive(bobId, Uint8Array.of(0x4d, 0x43, CAP_FRAME_V3))).caps?.complete).toBe(false)
    // The real one counts.
    expect((await alice.receive(bobId, real)).caps?.complete).toBe(true)
    await Promise.all([...groups.values()].map(group => group.close()))
  }, 60_000)

  it('decides per epoch: a newcomer that never advertised makes the next epoch incomplete, and its leaving makes it complete again', async () => {
    const roster = [aliceId, bobId]
    const groups = await room(roster)
    const alice = groups.get(aliceId) as MediaGroup
    const bob = groups.get(bobId) as MediaGroup
    for (const [id, group] of groups) await deliver(groups, id, (await group.maintain()).wires, roster)
    expect((await caps(alice, roster))?.complete).toBe(true)
    const epoch = (await alice.sync(roster)).epoch
    // Carol joins and speaks MLS but never sends a capability (an older tab).
    const carol = await open(carolId)
    groups.set(carolId, carol)
    const three = [aliceId, bobId, carolId]
    const silent = (wire: Uint8Array) => isCapability(wire)
    await deliver(groups, carolId, (await carol.sync(three)).wires, three, (wire, to) => silent(wire) && to === carolId)
    const joined = await alice.sync(three)
    expect(joined.epoch).not.toBe(epoch)
    expect(joined.caps).toEqual({ members: [aliceId, bobId].sort(), complete: false })
    expect((await bob.sync(three)).caps?.complete).toBe(false)
    // Carol leaves: the next epoch is complete again, from what was learned before.
    groups.delete(carolId)
    await carol.close()
    const removal = await alice.sync(roster)
    await deliver(groups, aliceId, removal.wires, roster)
    const after = await bob.sync(roster)
    expect(after.epoch).toBe((await alice.sync(roster)).epoch)
    expect(after.caps).toEqual({ members: [aliceId, bobId].sort(), complete: true })
    await Promise.all([...groups.values()].map(group => group.close()))
  }, 60_000)

  it('forgets a member that comes back with a new leaf until it speaks again', async () => {
    const roster = [aliceId, bobId]
    const groups = await room(roster)
    const alice = groups.get(aliceId) as MediaGroup
    for (const [id, group] of groups) await deliver(groups, id, (await group.maintain()).wires, roster)
    expect((await caps(alice, roster))?.complete).toBe(true)
    // Bob reloads (an older tab, say): a new leaf key, and no advertisement.
    const bob = groups.get(bobId) as MediaGroup
    groups.delete(bobId)
    await bob.close()
    const reloaded = await open(bobId)
    groups.set(bobId, reloaded)
    const noCaps = (wire: Uint8Array) => isCapability(wire)
    await deliver(groups, bobId, (await reloaded.sync(roster)).wires, roster, wire => noCaps(wire))
    const step = await alice.sync(roster)
    expect(step.caps).toEqual({ members: [aliceId], complete: false })
    await Promise.all([...groups.values()].map(group => group.close()))
  }, 60_000)
})
