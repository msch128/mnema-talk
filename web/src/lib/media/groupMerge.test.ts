// Two groups in one Talk become one: a member offers its key package to the
// room's lowest id when its group lacks that id and that id has shown a group
// of its own, and starts over when that id's signed heartbeats say it lives in
// another state of the group.
import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaTiming } from './bounds'
import { MediaGroup, type MediaStep } from './group'
import { sealHeartbeat } from './heartbeat'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const daveId = fixtureId(4)
const eveId = fixtureId(5)
const channelId = fixtureId(9)
const everyone = [aliceId, bobId, carolId, daveId]

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { vi.useRealTimers() })

// Nothing rotates, nobody counts as absent and no tick fires by itself: the clock is
// stepped with setSystemTime and a group looks at it when the test calls maintain().
const timing: MediaTiming = { rotateMs: 1e9, rotateJitterMs: 0, absentMs: 1e9, heartbeatMs: 5000, maintenanceMs: 1000 }
const START = Date.UTC(2026, 0, 1)
const FAKED: ('Date' | 'setTimeout' | 'clearTimeout')[] = ['Date', 'setTimeout', 'clearTimeout']

async function open(userId: string) {
  const group = await MediaGroup.open(userId, channelId, timing)
  if (!group) throw new Error('expected a media group')
  return group
}

function kinds(step: MediaStep) {
  return step.wires.filter(wire => wire[1] !== 0x43).map(wire => wire[0] === 0x4d ? 'heartbeat' : mls.decodeMlsMessage(wire, 0)?.[0].wireformat)
}

function only(step: MediaStep, kind: 'heartbeat' | 'mls_key_package' | 'mls_welcome' | 'mls_public_message'): Uint8Array {
  const found = step.wires.filter(wire => wire[1] !== 0x43).filter(wire => (wire[0] === 0x4d ? 'heartbeat' : mls.decodeMlsMessage(wire, 0)?.[0].wireformat) === kind)
  if (found.length !== 1 || !found[0]) throw new Error(`expected one ${kind}, got ${found.length}`)
  return found[0]
}

/** A welcome for `offer` from a member that adds it on its own, committer or not. */
async function sideDoor(group: MediaGroup, offer: Uint8Array): Promise<Uint8Array> {
  const decoded = mls.decodeMlsMessage(offer, 0)?.[0]
  if (decoded?.wireformat !== 'mls_key_package') throw new Error('expected a key package')
  const wires = await (group as unknown as { commit(proposals: unknown[]): Promise<Uint8Array[]> }).commit([{ proposalType: 'add', add: { keyPackage: decoded.keyPackage } }])
  const welcome = wires.find(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_welcome')
  if (!welcome) throw new Error('expected a welcome')
  return welcome
}

function signerOf(offer: Uint8Array): number[] {
  const decoded = mls.decodeMlsMessage(offer, 0)?.[0]
  if (decoded?.wireformat !== 'mls_key_package') throw new Error('expected a key package')
  return [...decoded.keyPackage.leafNode.signaturePublicKey]
}

async function closeAll(...groups: MediaGroup[]) {
  for (const group of groups) await group.close()
}

/** `first` founds and lets `second` in: a group of two that knows only itself. `shown` is the commit that did it. */
async function pairOf(firstId: string, secondId: string) {
  const first = await open(firstId)
  const second = await open(secondId)
  const roster = [firstId, secondId]
  await first.sync([firstId])
  await first.sync(roster)
  const added = await first.receive(secondId, only(await second.sync(roster), 'mls_key_package'))
  await second.receive(firstId, only(added, 'mls_welcome'))
  // Each tells the other what it supports, so later ticks carry only what a test is about.
  for (const wire of (await first.maintain()).wires) for (const reply of (await second.receive(firstId, wire)).wires) await first.receive(secondId, reply)
  for (const wire of (await second.maintain()).wires) await first.receive(secondId, wire)
  return [first, second, only(added, 'mls_public_message')] as const
}

/**
 * Alice and Bob in one group, Carol and Dave in another, and all four in the
 * Talk. Carol and Dave have seen a commit of Alice's, as everyone in a Talk
 * does: the server hands every handshake to all of it.
 */
async function twoGroups(shown = true) {
  vi.useFakeTimers({ toFake: FAKED, now: START })
  const [alice, bob, commit] = await pairOf(aliceId, bobId)
  const [carol, dave] = await pairOf(carolId, daveId)
  for (const group of [alice, bob, carol, dave]) expect(kinds(await group.sync(everyone))).toEqual([])
  if (shown) for (const group of [carol, dave]) await group.receive(aliceId, commit)
  return { alice, bob, carol, dave, commit }
}

describe('a member whose group lacks the room\'s lowest id', () => {
  it('offers that id its key package on its tick, is added and takes the welcome', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    vi.setSystemTime(START + 1000)
    const offer = await carol.maintain()
    expect(kinds(offer)).toEqual(['mls_key_package'])
    expect(offer.verification?.members).toEqual([carolId, daveId])
    const added = await alice.receive(carolId, only(offer, 'mls_key_package'))
    expect(kinds(added)).toEqual(['mls_welcome', 'mls_public_message'])
    await bob.receive(carolId, only(offer, 'mls_key_package'))
    await bob.receive(aliceId, only(added, 'mls_public_message'))
    const joined = await carol.receive(aliceId, only(added, 'mls_welcome'))
    expect(joined.verification?.members).toEqual([aliceId, bobId, carolId])
    // Dave still holds what is left of his group, and does the same on his tick.
    const second = await dave.maintain()
    const last = await alice.receive(daveId, only(second, 'mls_key_package'))
    const codes = new Set<string | undefined>([last.verification?.code])
    for (const group of [bob, carol]) {
      await group.receive(daveId, only(second, 'mls_key_package'))
      codes.add((await group.receive(aliceId, only(last, 'mls_public_message'))).verification?.code)
    }
    const whole = await dave.receive(aliceId, only(last, 'mls_welcome'))
    codes.add(whole.verification?.code)
    expect(whole.verification?.members).toEqual(everyone)
    expect(codes.size).toBe(1)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('says nothing while its group has that id, and the lowest id itself offers nobody anything', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    vi.setSystemTime(START + 1000)
    expect(kinds(await alice.maintain())).toEqual([])
    expect(kinds(await bob.maintain())).toEqual([])
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('offers nothing to a lower id that is merely listed, or that only sent key packages', async () => {
    const { alice, bob, carol, dave } = await twoGroups(false)
    // Alice and Bob say that they are there, as a newcomer or a group of one would.
    for (const [id, group] of [[aliceId, alice], [bobId, bob]] as const) {
      const hello = (group as unknown as { hello(): Uint8Array }).hello()
      for (const other of [carol, dave]) await other.receive(id, hello, true)
    }
    for (const at of [1000, 31_000, 600_000]) {
      vi.setSystemTime(START + at)
      for (const group of [carol, dave]) expect(kinds(await group.maintain()).filter(kind => kind !== 'heartbeat')).toEqual([])
    }
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('leaves its own group as it is: to its members the offer is a package they know the key of', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    vi.setSystemTime(START + 1000)
    // Dave joined Carol's group with a welcome; what he offers now is another package under his leaf's key.
    const offer = only(await dave.maintain(), 'mls_key_package')
    const held = await carol.sync(everyone)
    const heard = await carol.receive(daveId, offer)
    expect(kinds(heard)).toEqual([])
    expect(kinds(await carol.sync(everyone))).toEqual([])
    expect(heard.verification?.code).toBe(held.verification?.code)
    // Bob is in the group Dave asks to join, but does not commit there: he keeps the package for Alice's commit.
    expect(kinds(await bob.receive(daveId, offer))).toEqual([])
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('offers nothing on a sign that is more than ten minutes old', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    // Alice showed her group at the start. Carol's first tick with Alice as the room's lowest comes much later.
    vi.setSystemTime(START + 601_000)
    expect(kinds(await carol.maintain()).filter(kind => kind !== 'heartbeat')).toEqual([])
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('asks once, and again only after a wait and once that id has shown its group again', async () => {
    const { alice, bob, carol, dave, commit } = await twoGroups()
    vi.setSystemTime(START + 1000)
    const first = only(await carol.maintain(), 'mls_key_package')
    // No welcome comes, and Alice shows nothing more: Carol does not ask again.
    for (const at of [31_000, 61_000, 121_000, 3_600_000]) {
      vi.setSystemTime(START + at)
      expect(kinds(await carol.maintain()).filter(kind => kind !== 'heartbeat')).toEqual([])
    }
    // Alice shows a commit again: the next offer is a new package under the same signature key.
    await carol.receive(aliceId, commit)
    vi.setSystemTime(START + 3_601_000)
    const second = only(await carol.maintain(), 'mls_key_package')
    expect([...second]).not.toEqual([...first])
    expect(signerOf(second)).toEqual(signerOf(first))
    // The wait doubles: two minutes after the second offer, however much Alice shows.
    for (const at of [3_631_000, 3_691_000]) {
      vi.setSystemTime(START + at)
      await carol.receive(aliceId, commit)
      expect(kinds(await carol.maintain()).filter(kind => kind !== 'heartbeat')).toEqual([])
    }
    vi.setSystemTime(START + 3_722_000)
    expect(kinds(await carol.maintain()).filter(kind => kind !== 'heartbeat')).toEqual(['mls_key_package'])
    // Her group goes on: she still seals with the keys it has.
    expect((await carol.sync(everyone)).verification?.members).toEqual([carolId, daveId])
    // A welcome built on the package she replaced opens nothing.
    const stale = await alice.receive(carolId, first)
    expect((await carol.receive(aliceId, only(stale, 'mls_welcome'))).verification?.members).toEqual([carolId, daveId])
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('is let in by the lowest id although that id still holds a leaf of it from a welcome that was lost', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    vi.setSystemTime(START + 1000)
    const first = only(await carol.maintain(), 'mls_key_package')
    // Alice adds her; the commit reaches the Talk, the welcome does not reach Carol.
    const lost = await alice.receive(carolId, first)
    await bob.receive(carolId, first)
    await bob.receive(aliceId, only(lost, 'mls_public_message'))
    await carol.receive(aliceId, only(lost, 'mls_public_message'))
    // The same package again changes nothing over there.
    expect(kinds(await alice.receive(carolId, first))).toEqual([])
    vi.setSystemTime(START + 62_000)
    const second = only(await carol.maintain(), 'mls_key_package')
    // Bob holds her leaf too, but is not the lowest id: to him it is a package under a key he knows.
    expect(kinds(await bob.receive(carolId, second))).toEqual([])
    const again = await alice.receive(carolId, second)
    expect(kinds(again)).toEqual(['mls_welcome', 'mls_public_message'])
    await bob.receive(aliceId, only(again, 'mls_public_message'))
    const joined = await carol.receive(aliceId, only(again, 'mls_welcome'))
    expect(joined.verification?.members).toEqual([aliceId, bobId, carolId])
    expect(joined.verification?.code).toBe(again.verification?.code)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('takes the welcome built on its own package although one built on another member\'s came first', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    vi.setSystemTime(START + 1000)
    const hers = only(await carol.maintain(), 'mls_key_package')
    const his = only(await dave.maintain(), 'mls_key_package')
    const forCarol = await alice.receive(carolId, hers)
    const forDave = await alice.receive(daveId, his)
    // Every welcome goes to the whole Talk. Carol's opens nothing at Dave, and costs him nothing.
    expect((await dave.receive(aliceId, only(forCarol, 'mls_welcome'))).verification?.members).toEqual([carolId, daveId])
    expect((await dave.receive(aliceId, only(forDave, 'mls_welcome'))).verification?.members).toEqual(everyone)
    expect((await carol.receive(aliceId, only(forCarol, 'mls_welcome'))).verification?.members).toEqual([aliceId, bobId, carolId])
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('does not ask for a welcome it would refuse as too soon after the last one from that id', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    // Carol took a welcome from Alice a moment ago (the limit for such welcomes is per sender).
    const rivals = (carol as unknown as { rivals: { allow(id: string): boolean } }).rivals
    expect(rivals.allow(aliceId)).toBe(true)
    vi.setSystemTime(START + 1000)
    expect(kinds(await carol.maintain())).toEqual([])
    vi.setSystemTime(START + 59_000)
    expect(kinds(await carol.maintain())).toEqual(['heartbeat'])
    vi.setSystemTime(START + 61_000)
    expect(kinds(await carol.maintain())).toEqual(['mls_key_package'])
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('still takes no welcome built on that package from an id that is not lower, or from one in its own group', async () => {
    const { alice, bob, carol, dave } = await twoGroups()
    vi.setSystemTime(START + 1000)
    const offer = only(await carol.maintain(), 'mls_key_package')
    // Dave is in her group and higher: a second tab of his lets her into a group of its own making.
    const rogue = await open(daveId)
    await rogue.sync([daveId])
    expect((await carol.receive(daveId, await sideDoor(rogue, offer))).verification?.members).toEqual([carolId, daveId])
    // Bob is lower and outside her group, but Alice commits in the group his welcome leads into.
    expect((await carol.receive(bobId, await sideDoor(bob, offer))).verification?.members).toEqual([carolId, daveId])
    await closeAll(alice, bob, carol, dave, rogue)
  }, 60_000)
})

describe('a welcome from an id between a member and the one who commits in its group', () => {
  it('is refused, while one from an id below that committer is taken', async () => {
    vi.useFakeTimers({ toFake: FAKED, now: START })
    const room = [...everyone, eveId]
    const [bob, dave] = await pairOf(bobId, daveId)
    const [alice, eve, commit] = await pairOf(aliceId, eveId)
    for (const group of [alice, eve, bob, dave]) await group.sync(room)
    await dave.receive(aliceId, commit)
    vi.setSystemTime(START + 1000)
    const offer = only(await dave.maintain(), 'mls_key_package')
    // Carol is lower than Dave and outside his group, but not lower than Bob, who commits in it.
    const carol = await open(carolId)
    await carol.sync([carolId])
    expect((await dave.receive(carolId, await sideDoor(carol, offer))).verification?.members).toEqual([bobId, daveId])
    // Alice is lower than Bob: hers is the group everyone ends up in.
    const added = await alice.receive(daveId, offer)
    expect((await dave.receive(aliceId, only(added, 'mls_welcome'))).verification?.members).toEqual([aliceId, daveId, eveId])
    await closeAll(alice, bob, carol, dave, eve)
  }, 60_000)
})

/**
 * Alice, Bob and Carol in one group, each with a heartbeat of the others for
 * that epoch. Then Alice lets Dave in, and that commit reaches everyone but
 * `behind`: Alice lives one epoch on, in a state `behind` has not reached,
 * with the same leaf key as in the tree `behind` holds.
 */
async function splitState(behindId: string) {
  vi.useFakeTimers({ toFake: FAKED, now: START })
  const trio = [aliceId, bobId, carolId]
  const [alice, bob] = await pairOf(aliceId, bobId)
  const carol = await open(carolId)
  for (const group of [alice, bob]) await group.sync(trio)
  const asked = only(await carol.sync(trio), 'mls_key_package')
  const third = await alice.receive(carolId, asked)
  await bob.receive(carolId, asked)
  await bob.receive(aliceId, only(third, 'mls_public_message'))
  await carol.receive(aliceId, only(third, 'mls_welcome'))
  const groups = new Map([[aliceId, alice], [bobId, bob], [carolId, carol]])
  for (const at of [0, 5000]) {
    vi.setSystemTime(START + at)
    for (const [id, group] of groups) for (const wire of (await group.maintain()).wires) for (const [other, peer] of groups) if (other !== id) await peer.receive(id, wire)
  }
  const dave = await open(daveId)
  for (const group of groups.values()) await group.sync(everyone)
  const offer = only(await dave.sync(everyone), 'mls_key_package')
  const fourth = await alice.receive(daveId, offer)
  for (const [id, group] of groups) {
    if (id === aliceId) continue
    await group.receive(daveId, offer)
    if (id !== behindId) await group.receive(aliceId, only(fourth, 'mls_public_message'))
  }
  await dave.receive(aliceId, only(fourth, 'mls_welcome'))
  const behind = groups.get(behindId) as MediaGroup
  const held = await behind.sync(everyone)
  expect(held.verification?.members).toEqual(trio)
  return { alice, bob, carol, dave, behind, held, lets: fourth }
}

/** A member's heartbeat at `at` ms after the start. */
async function beatAt(group: MediaGroup, at: number): Promise<Uint8Array> {
  vi.setSystemTime(START + at)
  return only(await group.maintain(), 'heartbeat')
}

describe('a member whose tree holds the room\'s lowest id in another state', () => {
  it('starts over after two of that id\'s heartbeats for an epoch that is not its own, and is let in again', async () => {
    const { alice, bob, carol, dave, held } = await splitState(bobId)
    const first = await beatAt(alice, 10_000)
    const second = await beatAt(alice, 20_000)
    vi.setSystemTime(START + 10_000)
    // One heartbeat may only mean that a commit is still on its way.
    expect((await bob.receive(aliceId, first)).verification?.code).toBe(held.verification?.code)
    vi.setSystemTime(START + 20_000)
    const over = await bob.receive(aliceId, second)
    expect(over.key).toBeNull()
    expect(kinds(over)).toEqual(['mls_key_package'])
    const back = await alice.receive(bobId, only(over, 'mls_key_package'))
    expect(kinds(back)).toEqual(['mls_welcome', 'mls_public_message'])
    const joined = await bob.receive(aliceId, only(back, 'mls_welcome'))
    expect(joined.verification?.members).toEqual(everyone)
    expect(joined.verification?.code).toBe(back.verification?.code)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('keeps its group when the same heartbeat comes twice', async () => {
    const { alice, bob, carol, dave, held } = await splitState(bobId)
    const first = await beatAt(alice, 10_000)
    await bob.receive(aliceId, first)
    vi.setSystemTime(START + 20_000)
    expect((await bob.receive(aliceId, first)).verification?.code).toBe(held.verification?.code)
    vi.setSystemTime(START + 30_000)
    expect((await bob.receive(aliceId, first)).verification?.code).toBe(held.verification?.code)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('keeps its group when the two heartbeats come in the wrong order', async () => {
    const { alice, bob, carol, dave, held } = await splitState(bobId)
    const first = await beatAt(alice, 10_000)
    const second = await beatAt(alice, 20_000)
    await bob.receive(aliceId, second)
    vi.setSystemTime(START + 30_000)
    expect((await bob.receive(aliceId, first)).verification?.code).toBe(held.verification?.code)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('keeps its group when both arrive at once, or were signed within a few seconds of each other', async () => {
    const { alice, bob, carol, dave, held } = await splitState(bobId)
    const first = await beatAt(alice, 10_000)
    const second = await beatAt(alice, 20_000)
    // Held back and delivered together: by Bob's clock no time has passed, a commit may be right behind them.
    await bob.receive(aliceId, first)
    expect((await bob.receive(aliceId, second)).verification?.code).toBe(held.verification?.code)
    // Signed 3 s apart, whenever they arrive.
    const signer = (await alice.sync(everyone)).key?.signer as CryptoKey
    const group = new Uint8Array(16)
    group.set([0x09], 15)
    const epoch = (await alice.sync(everyone)).epoch as number
    const near = [await sealHeartbeat(signer, group, aliceId, epoch, START + 40_000), await sealHeartbeat(signer, group, aliceId, epoch, START + 43_000)] as Uint8Array[]
    vi.setSystemTime(START + 50_000)
    await bob.receive(aliceId, near[0] as Uint8Array)
    vi.setSystemTime(START + 60_000)
    expect((await bob.receive(aliceId, near[1] as Uint8Array)).verification?.code).toBe(held.verification?.code)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('keeps its group when the second heartbeat comes more than three heartbeat periods after the first', async () => {
    const { alice, bob, carol, dave, held } = await splitState(bobId)
    const first = await beatAt(alice, 10_000)
    const second = await beatAt(alice, 20_000)
    const third = await beatAt(alice, 40_000)
    vi.setSystemTime(START + 10_000)
    await bob.receive(aliceId, first)
    // Sixteen seconds on, with a heartbeat every five: the first no longer stands, and this one is a first again.
    vi.setSystemTime(START + 26_000)
    expect((await bob.receive(aliceId, second)).verification?.code).toBe(held.verification?.code)
    vi.setSystemTime(START + 40_000)
    expect((await bob.receive(aliceId, third)).key).toBeNull()
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('keeps its group whatever a member that is not the lowest id signs, also under the lowest id\'s name', async () => {
    const { alice, bob, carol, dave, held } = await splitState(carolId)
    // Bob lives in the other state too, and his heartbeats verify under the leaf key Carol holds for him.
    const first = await beatAt(bob, 10_000)
    const second = await beatAt(bob, 20_000)
    vi.setSystemTime(START + 10_000)
    await carol.receive(bobId, first)
    await carol.receive(aliceId, first)
    vi.setSystemTime(START + 20_000)
    expect((await carol.receive(bobId, second)).verification?.code).toBe(held.verification?.code)
    expect((await carol.receive(aliceId, second)).verification?.code).toBe(held.verification?.code)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('keeps its group when heartbeats it took in an earlier epoch are sent again after the group moved on', async () => {
    const { alice, bob, carol, dave, lets } = await splitState(bobId)
    // Bob takes two of Alice's heartbeats while he is in the epoch they name ...
    await bob.receive(aliceId, only(lets, 'mls_public_message'))
    const first = await beatAt(alice, 10_000)
    const second = await beatAt(alice, 20_000)
    await bob.receive(aliceId, first)
    await bob.receive(aliceId, second)
    // ... then the group moves on (Dave leaves), and the server sends both again, well apart.
    const three = [aliceId, bobId, carolId]
    await bob.sync(three)
    const moved = await bob.receive(aliceId, only(await alice.sync(three), 'mls_public_message'))
    expect(moved.verification?.members).toEqual(three)
    vi.setSystemTime(START + 30_000)
    await bob.receive(aliceId, first)
    vi.setSystemTime(START + 40_000)
    expect((await bob.receive(aliceId, second)).verification?.code).toBe(moved.verification?.code)
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('keeps the group it was let into again when the two heartbeats that made it start over are sent once more', async () => {
    const { alice, bob, carol, dave } = await splitState(bobId)
    const first = await beatAt(alice, 10_000)
    const second = await beatAt(alice, 20_000)
    vi.setSystemTime(START + 10_000)
    await bob.receive(aliceId, first)
    vi.setSystemTime(START + 20_000)
    const over = await bob.receive(aliceId, second)
    expect(over.key).toBeNull()
    const back = await alice.receive(bobId, only(over, 'mls_key_package'))
    const joined = await bob.receive(aliceId, only(back, 'mls_welcome'))
    expect(joined.verification?.members).toEqual(everyone)
    // The group he held then is gone; the times of her heartbeats are not.
    for (const round of [0, 1, 2]) {
      vi.setSystemTime(START + 100_000 + round * 100_000)
      await bob.receive(aliceId, first)
      vi.setSystemTime(START + 108_000 + round * 100_000)
      expect((await bob.receive(aliceId, second)).verification?.code).toBe(joined.verification?.code)
    }
    await closeAll(alice, bob, carol, dave)
  }, 60_000)

  it('starts over no more often than the limit for starting over allows', async () => {
    const { alice, bob, carol, dave, held } = await splitState(bobId)
    const first = await beatAt(alice, 10_000)
    const second = await beatAt(alice, 20_000)
    const resets = (bob as unknown as { resets: { allow(key: string): boolean } }).resets
    vi.setSystemTime(START + 10_000)
    await bob.receive(aliceId, first)
    vi.setSystemTime(START + 19_000)
    // He started over a second ago for another reason.
    expect(resets.allow('self')).toBe(true)
    vi.setSystemTime(START + 20_000)
    expect((await bob.receive(aliceId, second)).verification?.code).toBe(held.verification?.code)
    // The next heartbeat after the wait does it.
    const third = await beatAt(alice, 30_000)
    expect((await bob.receive(aliceId, third)).key).toBeNull()
    await closeAll(alice, bob, carol, dave)
  }, 60_000)
})

describe('a member that joined after heartbeats of the room\'s lowest id were sent', () => {
  it('keeps its group when they are sent to it, however far apart', async () => {
    vi.useFakeTimers({ toFake: FAKED, now: START })
    const trio = [aliceId, bobId, carolId]
    const [alice, bob] = await pairOf(aliceId, bobId)
    // Alice signs two heartbeats for the epoch she shares with Bob; nothing is kept from anyone.
    const first = await beatAt(alice, 6000)
    await bob.receive(aliceId, first)
    const second = await beatAt(alice, 20_000)
    await bob.receive(aliceId, second)
    const carol = await open(carolId)
    for (const group of [alice, bob]) await group.sync(trio)
    const offer = only(await carol.sync(trio), 'mls_key_package')
    const added = await alice.receive(carolId, offer)
    await bob.receive(carolId, offer)
    await bob.receive(aliceId, only(added, 'mls_public_message'))
    const joined = await carol.receive(aliceId, only(added, 'mls_welcome'))
    expect(joined.verification?.members).toEqual(trio)
    // Both verify under the leaf key Carol holds for Alice, and name an epoch that is not hers.
    for (const round of [0, 1, 2]) {
      vi.setSystemTime(START + 100_000 + round * 100_000)
      await carol.receive(aliceId, first)
      vi.setSystemTime(START + 108_000 + round * 100_000)
      expect((await carol.receive(aliceId, second)).verification?.code).toBe(joined.verification?.code)
    }
    // Nor do they count once she has one for her own epoch: they are older than it.
    await carol.receive(aliceId, await beatAt(alice, 400_000))
    vi.setSystemTime(START + 410_000)
    await carol.receive(aliceId, first)
    vi.setSystemTime(START + 420_000)
    expect((await carol.receive(aliceId, second)).verification?.code).toBe(joined.verification?.code)
    await closeAll(alice, bob, carol)
  }, 60_000)
})
