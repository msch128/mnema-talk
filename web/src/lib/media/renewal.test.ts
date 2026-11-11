import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { type MediaTiming } from './bounds'
import { MediaGroup, type MediaStep } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

// Leaves and epochs are due 8 s after they begin (rotateMs less the jitter, as Math.random is 0).
const timing: MediaTiming = { rotateMs: 10_000, rotateJitterMs: 2000, absentMs: 1e9, heartbeatMs: 1e9, maintenanceMs: 1000 }
const DUE = 8000
const SETTLE = 1000
const START = Date.UTC(2026, 0, 1)
const roster = [aliceId, bobId, carolId]

async function open(userId: string) {
  const group = await MediaGroup.open(userId, channelId, timing)
  if (!group) throw new Error('expected a media group')
  return group
}

async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[]) {
  const pending = wires.map(wire => ({ from, wire }))
  for (const group of groups.values()) group.hint(roster)
  while (pending.length) {
    const item = pending.shift()
    if (!item) break
    for (const [id, group] of groups) {
      if (id === item.from) continue
      const step = await group.receive(item.from, item.wire)
      for (const wire of step.wires) pending.push({ from: id, wire })
      const synced = await group.sync(roster)
      for (const wire of synced.wires) pending.push({ from: id, wire })
    }
  }
}

async function trio() {
  vi.spyOn(Math, 'random').mockReturnValue(0)
  // The groups tick on a real setTimeout. On a slow machine a tick would run between two steps of a test, against the
  // faked wall clock, and rotate an epoch the test is still working with: so no timer fires unless the test says so.
  vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: START })
  const groups = new Map<string, MediaGroup>()
  for (const id of roster) groups.set(id, await open(id))
  await (groups.get(aliceId) as MediaGroup).sync([aliceId])
  for (const id of roster.slice(1)) await deliver(groups, id, (await (groups.get(id) as MediaGroup).sync(roster)).wires)
  for (const group of groups.values()) await group.sync(roster)
  // Everyone tells the others what it supports now, so later ticks carry only what the test is about.
  for (const [id, group] of groups) await deliver(groups, id, (await group.maintain()).wires)
  return { groups, alice: groups.get(aliceId) as MediaGroup, bob: groups.get(bobId) as MediaGroup, carol: groups.get(carolId) as MediaGroup }
}

function kinds(step: MediaStep) {
  return step.wires.filter(wire => wire[1] !== 0x43).map(wire => wire[0] === 0x4d ? 'heartbeat' : mls.decodeMlsMessage(wire, 0)?.[0].wireformat)
}

async function closeAll(groups: Iterable<MediaGroup>) {
  for (const group of groups) await group.close()
}

async function codes(groups: Iterable<MediaGroup>) {
  const out = new Set<string>()
  for (const group of groups) {
    const step = await group.sync(roster)
    out.add(`${step.epoch}:${step.verification?.code}`)
  }
  return out
}

interface Inside {
  state: mls.ClientState
  cs: mls.CiphersuiteImpl
}

async function forgedEmpty(group: MediaGroup): Promise<Uint8Array> {
  const inside = group as unknown as Inside
  const result = await mls.createCommit({ state: inside.state, cipherSuite: inside.cs }, { extraProposals: [], wireAsPublicMessage: true, ratchetTreeExtension: true })
  return mls.encodeMlsMessage(result.commit)
}

describe('a member renewing its own leaf', () => {
  it('renews after its leaf is as old as its period, in a quiet moment, and everyone follows', async () => {
    const { groups, bob } = await trio()
    const before = await codes(groups.values())
    expect(before.size).toBe(1)
    vi.setSystemTime(START + DUE - 1)
    expect(kinds(await bob.maintain())).toEqual([])
    vi.setSystemTime(START + DUE)
    const renewal = await bob.maintain()
    expect(kinds(renewal)).toEqual(['mls_public_message'])
    await deliver(groups, bobId, renewal.wires)
    const after = await codes(groups.values())
    expect(after.size).toBe(1)
    expect([...after][0]).not.toBe([...before][0])
    expect(renewal.epoch).toBe(3)
    // His leaf is new now: nothing more from him until it is old again.
    vi.setSystemTime(START + DUE + SETTLE * 3)
    expect(kinds(await bob.maintain())).toEqual([])
    await closeAll(groups.values())
  }, 60_000)

  it('waits while handshakes or roster changes are about, and stops waiting once long overdue', async () => {
    const { groups, bob, alice } = await trio()
    vi.setSystemTime(START + DUE)
    // A handshake just now (here a stray key package) means a commit may be on its way.
    const stranger = await open(fixtureId(7))
    const offer = (await stranger.sync([...roster, fixtureId(7)])).wires[0] as Uint8Array
    await bob.receive(fixtureId(7), offer)
    expect(kinds(await bob.maintain())).toEqual([])
    vi.setSystemTime(START + DUE + SETTLE)
    // A roster change counts the same.
    await bob.sync([aliceId, bobId])
    expect(kinds(await bob.maintain())).toEqual([])
    vi.setSystemTime(START + DUE + SETTLE * 2)
    expect(kinds(await bob.maintain())).toEqual(['mls_public_message'])
    await closeAll([...groups.values(), stranger])
    expect(alice).toBeDefined()
  }, 60_000)

  it('takes its turn behind members who are due before it', async () => {
    const { groups, alice, bob, carol } = await trio()
    vi.setSystemTime(START + DUE)
    // The committer rotates first; the others are due too and each waits its turn after it.
    const rotated = await alice.maintain()
    expect(kinds(rotated)).toEqual(['mls_public_message'])
    await deliver(groups, aliceId, rotated.wires)
    vi.setSystemTime(START + DUE + SETTLE)
    expect(kinds(await carol.maintain())).toEqual([])
    const first = await bob.maintain()
    expect(kinds(first)).toEqual(['mls_public_message'])
    await deliver(groups, bobId, first.wires)
    // Bob's leaf is new, so nobody is ahead of Carol any more: she is next, one quiet moment later.
    vi.setSystemTime(START + DUE + SETTLE * 2 - 1)
    expect(kinds(await carol.maintain())).toEqual([])
    vi.setSystemTime(START + DUE + SETTLE * 2)
    const second = await carol.maintain()
    expect(kinds(second)).toEqual(['mls_public_message'])
    await deliver(groups, carolId, second.wires)
    expect((await codes(groups.values())).size).toBe(1)
    await closeAll(groups.values())
  }, 60_000)

  it('is refused from a member more than twice per period, and nothing is discarded', async () => {
    const { groups, alice, bob } = await trio()
    vi.setSystemTime(START + DUE)
    const renewal = await bob.maintain()
    await deliver(groups, bobId, renewal.wires)
    const epoch = (await alice.sync(roster)).epoch
    // Another empty commit at once, from the member who just renewed, is ignored.
    const again = await forgedEmpty(bob)
    const step = await alice.receive(bobId, again)
    expect(step.epoch).toBe(epoch)
    expect(step.key).not.toBeNull()
    // After half a period it is taken.
    vi.setSystemTime(START + DUE + 5000)
    expect((await alice.receive(bobId, again)).epoch).toBe((epoch ?? 0) + 1)
    await closeAll(groups.values())
  }, 60_000)

  it('goes back to the committer\'s commit when a renewal and a rotation met in the same epoch', async () => {
    const { groups, alice, bob, carol } = await trio()
    vi.setSystemTime(START + DUE)
    const renewal = await bob.maintain()
    const rotation = await alice.maintain()
    expect(kinds(renewal)).toEqual(['mls_public_message'])
    expect(kinds(rotation)).toEqual(['mls_public_message'])
    // Carol took Bob's first, then Alice's arrives: she goes back and takes Alice's.
    await carol.receive(bobId, renewal.wires[0] as Uint8Array)
    await carol.receive(aliceId, rotation.wires[0] as Uint8Array)
    // Bob took his own; Alice's is the committer's.
    await bob.receive(aliceId, rotation.wires[0] as Uint8Array)
    // Alice kept her own and ignores his.
    await alice.receive(bobId, renewal.wires[0] as Uint8Array)
    const agreed = await codes(groups.values())
    expect(agreed.size).toBe(1)
    expect([...agreed][0]).toContain(`${rotation.epoch}:`)
    await closeAll(groups.values())
  }, 60_000)

  it('keeps the lower id\'s renewal when two members renewed in the same epoch', async () => {
    const { groups, alice, bob, carol } = await trio()
    vi.setSystemTime(START + DUE)
    const fromBob = await bob.maintain()
    const fromCarol = await carol.maintain()
    // Alice took Carol's first, then Bob's arrives; Bob took his own and ignores Carol's; Carol goes back to Bob's.
    await alice.receive(carolId, fromCarol.wires[0] as Uint8Array)
    await alice.receive(bobId, fromBob.wires[0] as Uint8Array)
    await bob.receive(carolId, fromCarol.wires[0] as Uint8Array)
    await carol.receive(bobId, fromBob.wires[0] as Uint8Array)
    const agreed = await codes(groups.values())
    expect(agreed.size).toBe(1)
    expect([...agreed][0]).toContain(`${fromBob.epoch}:`)
    // A late loser arriving after the time for a conflict changes nothing.
    vi.setSystemTime(START + DUE + 11_000)
    const late = await alice.receive(carolId, fromCarol.wires[0] as Uint8Array)
    expect(late.epoch).toBe(fromBob.epoch)
    await closeAll(groups.values())
  }, 60_000)

  it('keeps what the committer did on top of a renewal when a rival renewal for the old epoch comes late', async () => {
    const { groups, alice, bob, carol } = await trio()
    vi.setSystemTime(START + DUE)
    const fromBob = await bob.maintain()
    const fromCarol = await carol.maintain()
    await alice.receive(carolId, fromCarol.wires[0] as Uint8Array)
    // Bob leaves the Talk, and Alice removes him: a commit on top of Carol's renewal.
    const removed = await alice.sync([aliceId, carolId])
    expect(removed.verification?.members).toEqual([aliceId, carolId])
    // Bob's renewal would have beaten Carol's, but the epoch both were for is two commits back:
    // going back to it would undo the removal.
    const late = await alice.receive(bobId, fromBob.wires[0] as Uint8Array)
    // She does not go back; she moves one epoch on, with a commit of her own (see the next test).
    expect(late.epoch).toBe((removed.epoch ?? 0) + 1)
    expect(late.verification?.members).toEqual([aliceId, carolId])
    await closeAll(groups.values())
  }, 60_000)

  it('commits once more when the rival renewal comes late, so members that took it start over and are let in again', async () => {
    const { groups, alice, bob, carol } = await trio()
    vi.setSystemTime(START + DUE)
    const fromBob = await bob.maintain()
    const fromCarol = await carol.maintain()
    const left = [aliceId, carolId]
    // The server's order: Carol's renewal, Bob's renewal, Alice's removal of Bob, which she built on Carol's.
    await alice.receive(carolId, fromCarol.wires[0] as Uint8Array)
    const removed = await alice.sync(left)
    expect(kinds(removed)).toEqual(['mls_public_message'])
    // Carol goes back to Bob's renewal, the lower id's, and cannot follow the removal from there.
    await carol.receive(bobId, fromBob.wires[0] as Uint8Array)
    carol.hint(left)
    const stuck = await carol.receive(aliceId, removed.wires[0] as Uint8Array)
    expect(stuck.verification?.members).toEqual(roster)
    // Bob's renewal reaches Alice last. She stays where she is and commits again.
    const answer = await alice.receive(bobId, fromBob.wires[0] as Uint8Array)
    expect(kinds(answer)).toEqual(['mls_public_message'])
    expect(answer.epoch).toBe((removed.epoch ?? 0) + 1)
    expect(answer.verification?.members).toEqual(left)
    // Carol sees a commit for an epoch she has not reached, starts over and is let in again. Bob stays out.
    const over = await carol.receive(aliceId, answer.wires[0] as Uint8Array)
    expect(over.key).toBeNull()
    expect(kinds(over)).toEqual(['mls_key_package'])
    await carol.sync(left)
    const back = await alice.receive(carolId, over.wires[0] as Uint8Array)
    expect(kinds(back)).toEqual(['mls_welcome', 'mls_public_message'])
    const joined = await carol.receive(aliceId, back.wires[0] as Uint8Array)
    expect(joined.verification?.members).toEqual(left)
    expect(joined.verification?.code).toBe(back.verification?.code)
    // A second copy of the late renewal is answered by nothing.
    expect(kinds(await alice.receive(bobId, fromBob.wires[0] as Uint8Array))).toEqual([])
    await closeAll(groups.values())
  }, 60_000)

  it('still takes the lowest id\'s renewal after it refused a higher one, when three members renewed in the same epoch', async () => {
    const { groups, alice, bob, carol } = await trio()
    // Dave joins, and everyone's leaf comes due at the same moment.
    const daveId = fixtureId(4)
    const four = [...roster, daveId]
    const dave = await open(daveId)
    for (const group of groups.values()) await group.sync(four)
    const offer = (await dave.sync(four)).wires[0] as Uint8Array
    for (const group of [bob, carol]) await group.receive(daveId, offer)
    const added = await alice.receive(daveId, offer)
    for (const group of [bob, carol]) await group.receive(aliceId, added.wires[1] as Uint8Array)
    await dave.receive(aliceId, added.wires[0] as Uint8Array)
    vi.setSystemTime(START + 60_000)
    const renewals = new Map<string, Uint8Array>()
    for (const [id, group] of [[bobId, bob], [carolId, carol], [daveId, dave]] as const) {
      const wire = (await group.maintain()).wires.find(w => mls.decodeMlsMessage(w, 0)?.[0].wireformat === 'mls_public_message')
      if (!wire) throw new Error('expected a renewal')
      renewals.set(id, wire)
    }
    // Carol holds her own, refuses Dave's (a higher id) and then gets Bob's, which wins.
    const own = (await carol.sync(four)).verification?.code
    expect((await carol.receive(daveId, renewals.get(daveId) as Uint8Array)).verification?.code).toBe(own)
    const switched = await carol.receive(bobId, renewals.get(bobId) as Uint8Array)
    expect(switched.verification?.code).toBe((await bob.sync(four)).verification?.code)
    await closeAll([...groups.values(), dave])
  }, 60_000)

  it('goes back a second time when the committer\'s commit comes after the lower id\'s renewal it had gone back for', async () => {
    const { groups, alice, bob, carol } = await trio()
    vi.setSystemTime(START + DUE)
    const fromBob = await bob.maintain()
    const fromCarol = await carol.maintain()
    const rotation = await alice.maintain()
    expect([fromBob.epoch, fromCarol.epoch]).toEqual([rotation.epoch, rotation.epoch])
    // Carol holds her own renewal, goes back for Bob's, and then Alice's commit for that epoch arrives.
    await carol.receive(bobId, fromBob.wires[0] as Uint8Array)
    await carol.receive(aliceId, rotation.wires[0] as Uint8Array)
    await bob.receive(carolId, fromCarol.wires[0] as Uint8Array)
    await bob.receive(aliceId, rotation.wires[0] as Uint8Array)
    const agreed = await codes(groups.values())
    expect(agreed.size).toBe(1)
    expect([...agreed][0]).toBe(`${rotation.epoch}:${rotation.verification?.code}`)
    await closeAll(groups.values())
  }, 60_000)

  it('ignores a rival commit for that epoch that does not verify, or that loses', async () => {
    const { groups, alice, bob, carol } = await trio()
    vi.setSystemTime(START + DUE)
    const renewal = await bob.maintain()
    await alice.receive(bobId, renewal.wires[0] as Uint8Array)
    const epoch = (await alice.sync(roster)).epoch
    // Carol's empty commit for the old epoch, claimed by Bob: it is not signed with his leaf.
    const forged = await forgedEmpty(carol)
    expect((await alice.receive(bobId, forged)).epoch).toBe(epoch)
    // And Carol's own, a higher id's renewal, loses to Bob's that Alice already holds.
    expect((await alice.receive(carolId, forged)).epoch).toBe(epoch)
    expect((await alice.receive(carolId, forged)).key).not.toBeNull()
    await closeAll(groups.values())
  }, 60_000)

  describe('when an epoch cannot be taken', () => {
    const refuse = (group: MediaGroup) => vi.spyOn(group as unknown as { adopt(): Promise<boolean> }, 'adopt').mockResolvedValueOnce(false)

    it('starts over after a renewal it cannot take', async () => {
      const { groups, bob, carol } = await trio()
      vi.setSystemTime(START + DUE)
      const renewal = await bob.maintain()
      refuse(carol)
      const lost = await carol.receive(bobId, renewal.wires[0] as Uint8Array)
      expect(lost.key).toBeNull()
      await closeAll(groups.values())
    }, 60_000)

    it('starts over after a winner it cannot take, and a proposal changes nothing', async () => {
      const { groups, alice, bob, carol } = await trio()
      vi.setSystemTime(START + DUE)
      const renewal = await bob.maintain()
      const rotation = await alice.maintain()
      await carol.receive(bobId, renewal.wires[0] as Uint8Array)
      refuse(carol)
      const lost = await carol.receive(aliceId, rotation.wires[0] as Uint8Array)
      expect(lost.key).toBeNull()
      // A standalone proposal is not something this group uses.
      const inside = alice as unknown as Inside
      const made = await mls.createProposal(inside.state, true, { proposalType: 'remove', remove: { removed: 2 } }, inside.cs)
      const epoch = (await bob.sync(roster)).epoch
      expect((await bob.receive(aliceId, mls.encodeMlsMessage(made.message))).epoch).toBe(epoch)
      await closeAll(groups.values())
    }, 60_000)

    it('starts over after a commit it cannot take', async () => {
      const { groups, alice, bob } = await trio()
      vi.setSystemTime(START + DUE)
      const rotation = await alice.maintain()
      refuse(bob)
      const lost = await bob.receive(aliceId, rotation.wires[0] as Uint8Array)
      expect(lost.key).toBeNull()
      await closeAll(groups.values())
    }, 60_000)
  })
})
