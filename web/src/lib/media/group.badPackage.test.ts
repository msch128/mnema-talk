import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { fixtureId } from '../../test-fixtures.fixture'
import { MediaGroup } from './group'

// One key package a committer cannot add (a broken signature, say) must not
// keep the other pending members out of the group.
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

async function offerOf(group: MediaGroup, roster: string[]): Promise<Uint8Array> {
  const wire = (await group.sync(roster)).wires[0]
  if (!wire) throw new Error('expected a key package')
  return wire
}

/** The package with its signature (the last bytes) damaged. */
function forged(wire: Uint8Array): Uint8Array {
  const copy = wire.slice()
  copy[copy.length - 1] = (copy[copy.length - 1] ?? 0) ^ 0xff
  return copy
}

describe('a bad key package among pending ones', () => {
  it('adds the valid members in the same sync and leaves the committer consistent', async () => {
    const roster = [aliceId, bobId, carolId, daveId]
    const alice = await opened(aliceId)
    const [bob, carol, dave] = [await opened(bobId), await opened(carolId), await opened(daveId)]
    await alice.sync([aliceId])
    await alice.receive(bobId, forged(await offerOf(bob, roster)))
    await alice.receive(carolId, await offerOf(carol, roster))
    await alice.receive(daveId, await offerOf(dave, roster))

    const step = await alice.sync(roster)
    expect(step.wires.length).toBeGreaterThan(0)
    expect(step.epoch).toBeGreaterThan(0)
    const epoch = step.epoch

    // The forged package is gone: a later sync neither retries it nor moves the epoch.
    const again = await alice.sync(roster)
    expect(again.epoch).toBe(epoch)
    expect(again.wires).toEqual([])
    await Promise.all([alice, bob, carol, dave].map(group => group.close()))
  }, 60_000)

  it('does not block a later add after a bad package was dropped', async () => {
    const roster = [aliceId, bobId, carolId]
    const alice = await opened(aliceId)
    const [bob, carol] = [await opened(bobId), await opened(carolId)]
    await alice.sync([aliceId])
    await alice.receive(bobId, forged(await offerOf(bob, roster)))
    const first = await alice.sync(roster)
    expect(first.epoch).toBe(0)

    const received = await alice.receive(carolId, await offerOf(carol, roster))
    const second = await alice.sync(roster)
    expect(second.epoch ?? received.epoch).toBe(1)
    expect([...received.wires, ...second.wires].length).toBeGreaterThan(0)
    await Promise.all([alice, bob, carol].map(group => group.close()))
  }, 60_000)
})
