// A key package that a member's OLD tab made, and that reaches the member's
// committer only after the member reloaded and was swapped in, is stale: it
// must not swap the live leaf for a leaf nobody holds the keys of any more.
import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaTiming } from './bounds'
import { MediaGroup, type MediaStep } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const channelId = fixtureId(9)
const roster = [aliceId, bobId]

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { vi.useRealTimers() })

const timing: MediaTiming = { rotateMs: 1e9, rotateJitterMs: 0, absentMs: 1e9, heartbeatMs: 5000, maintenanceMs: 1000 }
const START = Date.UTC(2026, 0, 1)

async function open(userId: string) {
  const group = await MediaGroup.open(userId, channelId, timing)
  if (!group) throw new Error('expected a media group')
  return group
}

function wireKinds(step: MediaStep) {
  return step.wires.filter(wire => wire[0] !== 0x4d && wire[1] !== 0x43).map(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat)
}

function only(step: MediaStep, kind: 'mls_key_package' | 'mls_welcome' | 'mls_public_message'): Uint8Array {
  const found = step.wires.filter(wire => wire[0] !== 0x4d && wire[1] !== 0x43).filter(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === kind)
  if (found.length !== 1 || !found[0]) throw new Error(`expected one ${kind}, got ${found.length}`)
  return found[0]
}

/** Alice founds and lets Bob in; Bob's tab holds the leaf. */
async function pair() {
  const alice = await open(aliceId)
  const bob = await open(bobId)
  await alice.sync([aliceId])
  await alice.sync(roster)
  const added = await alice.receive(bobId, only(await bob.sync(roster), 'mls_key_package'))
  await bob.receive(aliceId, only(added, 'mls_welcome'))
  return { alice, bob }
}

describe('a late key package of a tab that has been replaced', () => {
  it('does not swap the leaf of the reloaded member for a dead one, but a newer package still does', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: START })
    const { alice, bob } = await pair()

    // The old tab's package: made at +10 s, and held back by the relay.
    vi.setSystemTime(START + 10_000)
    const oldTab = await open(bobId)
    const late = only(await oldTab.sync(roster), 'mls_key_package')

    // The member reloads at +20 s; Alice swaps its leaf at once.
    vi.setSystemTime(START + 20_000)
    const reloaded = await open(bobId)
    const swap = await alice.receive(bobId, only(await reloaded.sync(roster), 'mls_key_package'))
    expect(wireKinds(swap)).toEqual(['mls_welcome', 'mls_public_message'])
    await reloaded.receive(aliceId, only(swap, 'mls_welcome'))

    // The relay now delivers the old tab's package: nothing happens.
    vi.setSystemTime(START + 60_000)
    expect(wireKinds(await alice.receive(bobId, late))).toEqual([])
    expect(wireKinds(await alice.maintain())).toEqual([])

    // Another reload, later than both, is a reload.
    vi.setSystemTime(START + 120_000)
    const third = await open(bobId)
    const again = await alice.receive(bobId, only(await third.sync(roster), 'mls_key_package'))
    expect(wireKinds(again)).toEqual(['mls_welcome', 'mls_public_message'])

    for (const group of [alice, bob, oldTab, reloaded, third]) await group.close()
  }, 60_000)
})
