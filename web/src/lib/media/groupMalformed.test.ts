import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { fixtureId } from '../../test-fixtures.fixture'
import { capabilityLikeWire, heartbeatLikeWire, mulberry32, randomBytes } from './malformed.fixture'
import { MediaGroup } from './group'

// Handshake wires arrive from other members through the server, so the group
// must take any bytes without throwing and without losing the epoch it holds.
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

describe('malformed handshake wires', () => {
  it('are taken without throwing, whatever their length or first bytes, and the group keeps its epoch', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const roster = [aliceId, bobId]
    alice.hint(roster)
    await alice.sync([aliceId])
    const offer = await bob.sync(roster)
    const hello = offer.wires[0]
    if (!hello) throw new Error('expected a key package')
    await alice.receive(bobId, hello)
    const joined = await alice.sync(roster)
    const epoch = joined.epoch
    expect(epoch).not.toBeNull()

    const random = mulberry32(0x6d6c73)
    const wires: Uint8Array[] = [new Uint8Array(), Uint8Array.of(0), Uint8Array.of(0x4d), Uint8Array.of(0x4d, 0x48), Uint8Array.of(0x4d, 0x43)]
    for (let i = 0; i < 150; i++) wires.push(randomBytes(random, Math.floor(random() * 600)))
    // Every truncation and some mutations of a real key package.
    for (let length = 0; length < hello.byteLength; length += 3) wires.push(hello.slice(0, length))
    for (let i = 0; i < 80; i++) {
      const copy = hello.slice()
      copy[Math.floor(random() * copy.length)] = Math.floor(random() * 256)
      wires.push(copy)
    }
    wires.push(heartbeatLikeWire(random), capabilityLikeWire(random))
    for (const wire of wires) {
      await expect(alice.receive(random() < 0.5 ? bobId : carolId, wire)).resolves.toBeDefined()
    }
    expect((await alice.sync(roster)).epoch).toBe(epoch)
    await alice.close()
    await bob.close()
  }, 60_000)
})
