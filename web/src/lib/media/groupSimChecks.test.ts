// What the simulator's fourth invariant notices, shown on single wires: the
// checks are handed a member's keys before and after a real commit.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { fixtureId } from '../../test-fixtures.fixture'
import { MediaGroup } from './group'
import { GroupChecks } from './groupSimChecks.fixture'
import type { GroupSim, SimMember } from './groupSim.fixture'
import type { MediaVerification } from './verify'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const daveId = fixtureId(4)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

function view(epoch: number, members: string[], code = `code ${epoch}`): MediaVerification {
  return { epoch, code, members }
}

function watcher(id: string, roster: string[], group: MediaGroup | null = null) {
  const violations: string[] = []
  const checks = new GroupChecks()
  checks.bind({ now: 1000, violation: (text: string) => violations.push(text) } as unknown as GroupSim)
  const member = { id, label: 'watcher', roster, previousRoster: roster, wokeAt: -Infinity, keys: [], group } as unknown as SimMember
  return { checks, member, violations }
}

function wireAt(wires: Uint8Array[], index: number): Uint8Array {
  const wire = wires[index]
  if (!wire) throw new Error('expected a handshake wire')
  return wire
}

async function opened(userId: string) {
  const group = await MediaGroup.open(userId, channelId)
  if (!group) throw new Error('expected a media group')
  return group
}

/** Alice founds alone, says so to the room, and lets Carol in; then Bob offers and she adds him with a commit for epoch 1. */
async function talk() {
  const alice = await opened(aliceId)
  const bob = await opened(bobId)
  const carol = await opened(carolId)
  await alice.sync([aliceId])
  const pair = [aliceId, carolId]
  const hello = wireAt((await alice.sync(pair)).wires, 0)
  const welcome = await alice.receive(carolId, wireAt((await carol.sync(pair)).wires, 0))
  await carol.receive(aliceId, wireAt(welcome.wires, 0))
  const room = [aliceId, bobId, carolId]
  await alice.sync(room)
  const offer = wireAt((await bob.sync(room)).wires, 0)
  const added = await alice.receive(bobId, offer)
  const close = async () => { for (const group of [alice, bob, carol]) await group.close() }
  return { carol, hello, offer, commit: wireAt(added.wires, 1), room, close }
}

describe('the fourth invariant on a single commit', () => {
  it('accepts a commit that leads one epoch on and adds whom the relay delivered a package from', async () => {
    const { offer, commit, room, close } = await talk()
    const { checks, member, violations } = watcher(carolId, room)
    await checks.received(member, bobId, offer, view(1, [aliceId, carolId]), view(1, [aliceId, carolId]))
    await checks.received(member, aliceId, commit, view(1, [aliceId, carolId]), view(2, room))
    expect(violations).toEqual([])
    await close()
  }, 30_000)

  it('reports a commit that leads anywhere but one epoch on', async () => {
    const { offer, commit, room, close } = await talk()
    const { checks, member, violations } = watcher(carolId, room)
    await checks.received(member, bobId, offer, view(3, [aliceId, carolId]), view(3, [aliceId, carolId]))
    // The member was two epochs past the commit's, and ends one epoch back with Bob in its group.
    await checks.received(member, aliceId, commit, view(3, [aliceId, carolId]), view(2, room))
    expect(violations).toEqual([expect.stringContaining('led from epoch 3 to 2')])
    await close()
  }, 30_000)

  it('accepts the one way back: the commit that wins against a renewal taken for the same epoch', async () => {
    const { offer, commit, room, close } = await talk()
    const { checks, member, violations } = watcher(carolId, room)
    await checks.received(member, bobId, offer, view(2, [aliceId, carolId]), view(2, [aliceId, carolId]))
    await checks.received(member, aliceId, commit, view(2, [aliceId, carolId]), view(2, room, 'the winner'))
    expect(violations).toEqual([])
    await close()
  }, 30_000)

  it('reports a member that is in the group afterwards and that the commit does not add', async () => {
    const { offer, commit, room, close } = await talk()
    const all = [...room, daveId]
    const { checks, member, violations } = watcher(carolId, all)
    await checks.received(member, bobId, offer, view(1, [aliceId, carolId]), view(1, [aliceId, carolId]))
    await checks.received(member, aliceId, commit, view(1, [aliceId, carolId]), view(2, all))
    expect(violations).toEqual([expect.stringContaining('m4 is in the group, whom it does not add')])
    await close()
  }, 30_000)

  it('reports nothing when the committer answers a late renewal with a commit of its own', async () => {
    const { commit, room, close } = await talk()
    const { checks, member, violations } = watcher(carolId, room)
    await checks.received(member, aliceId, commit, view(4, room), view(5, room))
    expect(violations).toEqual([])
    await close()
  }, 30_000)
})

describe('who commits now, for the fourth invariant', () => {
  it('still is the lowest member after it repeated the key package it is in the group with', async () => {
    const { carol, hello, offer, commit, room, close } = await talk()
    const { checks, member, violations } = watcher(carolId, room, carol)
    checks.epoch(member, view(1, room))
    await checks.received(member, aliceId, hello, view(1, room), view(1, room))
    await checks.received(member, daveId, offer, view(1, room), view(1, room))
    // A commit with something in it from Bob, although Alice is here and alive.
    await checks.received(member, bobId, commit, view(1, room), view(2, room))
    expect(violations).toContainEqual(expect.stringContaining('although m1 commits now'))
    await close()
  }, 30_000)

  it('may be the next one while the lowest member is coming back with a new key package', async () => {
    const { carol, commit, room, close } = await talk()
    const reloaded = await opened(aliceId)
    const fresh = wireAt((await reloaded.sync(room)).wires, 0)
    const { checks, member, violations } = watcher(carolId, room, carol)
    checks.epoch(member, view(1, room))
    await checks.received(member, aliceId, fresh, view(1, room), view(1, room))
    await checks.received(member, bobId, commit, view(1, room), view(2, room))
    expect(violations.filter(text => text.includes('commits now'))).toEqual([])
    await reloaded.close()
    await close()
  }, 30_000)
})
