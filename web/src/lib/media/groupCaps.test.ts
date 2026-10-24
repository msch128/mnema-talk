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

// The capability message stays: members that still decide their frame format
// by it (older clients) learn from it that this one opens version 3.
describe('the capability message', () => {
  it('is sent on the first tick and answered once when a member is heard for the first time', async () => {
    const roster = [aliceId, bobId]
    const groups = await room(roster)
    const alice = groups.get(aliceId) as MediaGroup
    const bob = groups.get(bobId) as MediaGroup
    const told = await alice.maintain()
    expect(told.wires.filter(isCapability)).toHaveLength(1)
    const reply = await bob.receive(aliceId, told.wires[0] as Uint8Array)
    expect(reply.wires.filter(isCapability)).toHaveLength(1)
    const back = await alice.receive(bobId, reply.wires[0] as Uint8Array)
    // The answer to the answer is the last one: a member already known is not answered.
    expect(back.wires.filter(isCapability)).toHaveLength(1)
    expect((await bob.receive(aliceId, back.wires[0] as Uint8Array)).wires).toEqual([])
    expect((await bob.receive(aliceId, told.wires[0] as Uint8Array)).wires).toEqual([])
    await Promise.all([...groups.values()].map(group => group.close()))
  }, 60_000)

  it('ignores a capability nobody in the epoch signed, one with other bits and one of a stranger', async () => {
    const roster = [aliceId, bobId]
    const groups = await room(roster)
    const alice = groups.get(aliceId) as MediaGroup
    const bob = groups.get(bobId) as MediaGroup
    const groupId = mediaGroupId(channelId) as Uint8Array
    const mallory = await testMember()
    const answered = async (from: string, wire: Uint8Array) => (await alice.receive(from, wire)).wires.some(isCapability)
    // Signed with a key that is not Bob's leaf key.
    expect(await answered(bobId, await sealCapability(mallory.signer, groupId, bobId) as Uint8Array)).toBe(false)
    // Bob's real message, claimed by Carol (not in the group) or replayed for another group.
    const real = (await bob.maintain()).wires.find(isCapability) as Uint8Array
    expect(await answered(carolId, real)).toBe(false)
    expect(await answered(bobId, await sealCapability(mallory.signer, new Uint8Array(16), aliceId) as Uint8Array)).toBe(false)
    // A member that signed no bits at all is not capable.
    expect(await answered(bobId, await sealCapability(mallory.signer, groupId, bobId, 0) as Uint8Array)).toBe(false)
    // Garbage with the right magic.
    expect(await answered(bobId, Uint8Array.of(0x4d, 0x43, CAP_FRAME_V3))).toBe(false)
    // The real one counts.
    expect(await answered(bobId, real)).toBe(true)
    await Promise.all([...groups.values()].map(group => group.close()))
  }, 60_000)
})
