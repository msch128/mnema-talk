// When the committer removes a member that has been silent. A member that came
// in by a welcome counts every other member as alive from then on, and refuses
// a removal until the absence limit has passed since (groupVet.ts). So the
// committer waits until the newest member has been in for the limit, instead
// of removing by what it heard alone and being refused (see groupEntry.ts).
import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaTiming } from './bounds'
import { MediaGroup, type MediaStep } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const daveId = fixtureId(4)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { vi.useRealTimers() })

const timing: MediaTiming = { rotateMs: 1e9, rotateJitterMs: 0, absentMs: 30_000, heartbeatMs: 5000, maintenanceMs: 1000 }
const START = Date.UTC(2026, 0, 1)

async function open(userId: string) {
  const group = await MediaGroup.open(userId, channelId, timing)
  if (!group) throw new Error('expected a media group')
  return group
}

function only(step: MediaStep, kind: 'mls_key_package' | 'mls_welcome' | 'mls_public_message'): Uint8Array {
  const found = step.wires.filter(wire => wire[0] !== 0x4d && wire[1] !== 0x43).filter(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === kind)
  if (found.length !== 1 || !found[0]) throw new Error(`expected one ${kind}, got ${found.length}`)
  return found[0]
}

function commits(step: MediaStep): number {
  return step.wires.filter(wire => wire[0] !== 0x4d && wire[1] !== 0x43 && mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_public_message').length
}

/** Alice commits a group with Bob, who then says nothing; `joiners` come in later, at the given seconds, and stay heard. */
async function room(joinAt: readonly [string, number][]) {
  vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: START })
  const alice = await open(aliceId)
  const bob = await open(bobId)
  await alice.sync([aliceId])
  let roster = [aliceId, bobId]
  await alice.sync(roster)
  await bob.receive(aliceId, only(await alice.receive(bobId, only(await bob.sync(roster), 'mls_key_package')), 'mls_welcome'))
  const heard = new Map<string, MediaGroup>()
  const removedAt: number[] = []
  for (let second = 1; second <= 120; second++) {
    vi.setSystemTime(START + second * 1000)
    for (const [id, at] of joinAt) {
      if (at !== second) continue
      const joiner = await open(id)
      roster = [...roster, id]
      await alice.sync(roster)
      const added = await alice.receive(id, only(await joiner.sync(roster), 'mls_key_package'))
      await joiner.receive(aliceId, only(added, 'mls_welcome'))
      heard.set(id, joiner)
    }
    for (const [id, joiner] of heard) for (const wire of (await joiner.maintain()).wires) await alice.receive(id, wire)
    if (commits(await alice.maintain()) > 0) removedAt.push(second)
  }
  for (const group of [alice, bob, ...heard.values()]) await group.close()
  return removedAt
}

describe('the committer removing a member that went silent', () => {
  it('waits until the newest member has been in for the absence limit', async () => {
    // Bob is silent from the start. Carol comes in at 20 s and is heard: a removal of Bob at 31 s
    // would be refused by her (she counts him alive from 20 s on), so Alice removes him after 52 s.
    const first = (await room([[carolId, 20]]))[0]
    expect(first).toBeGreaterThanOrEqual(52)
    expect(first).toBeLessThanOrEqual(56)
  }, 120_000)

  it('removes by its own count when nobody came in lately', async () => {
    const first = (await room([]))[0]
    expect(first).toBeGreaterThanOrEqual(31)
    expect(first).toBeLessThanOrEqual(33)
  }, 120_000)

  it('removes a member silent for twice the limit whoever came in lately', async () => {
    // Carol at 20 s and Dave at 50 s: at 52 s Dave is still new, but Bob has been silent for 60 s.
    const first = (await room([[carolId, 20], [daveId, 50]]))[0]
    expect(first).toBeGreaterThanOrEqual(61)
    expect(first).toBeLessThanOrEqual(63)
  }, 120_000)
})
