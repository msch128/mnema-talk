// When the committer removes a member that has been silent. A member that came
// in by a welcome counts every other member as alive from then on, refuses a
// removal until the absence limit has passed since (groupVet.ts), and takes a
// silent lowest id for the committer until the patience has passed since. So
// the committer waits until the newest member has been in for the patience and
// an interval, instead of removing by what it heard alone and being refused
// (see groupEntry.ts). Limit 30 s, interval 1 s: the wait ends 33 s after the
// newest member came in.
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

function commits(step: MediaStep): Uint8Array[] {
  return step.wires.filter(wire => wire[0] !== 0x4d && wire[1] !== 0x43 && mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_public_message')
}

/** What happens in a room of Alice and Bob: who comes in or reloads at which second, and who stays heard. */
interface Script {
  joinAt: readonly [string, number][]
  reloadAt?: readonly [string, number][]
  /** Joiners that say nothing after they came in. */
  quiet?: readonly string[]
  /** Bob speaks up to this second and says nothing after (0: never). */
  bobUntil?: number
}

/** Alice commits a group with Bob; the script plays on. Returns the second of Alice's first removal and the members a heard joiner holds after it. */
async function room(script: Script) {
  vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: START })
  const alice = await open(aliceId)
  const bob = await open(bobId)
  await alice.sync([aliceId])
  let roster = [aliceId, bobId]
  await alice.sync(roster)
  await bob.receive(aliceId, only(await alice.receive(bobId, only(await bob.sync(roster), 'mls_key_package')), 'mls_welcome'))
  const joiners = new Map<string, MediaGroup>()
  let first: number | null = null
  let held: string[] | undefined
  for (let second = 1; second <= 120; second++) {
    vi.setSystemTime(START + second * 1000)
    const arriving = [...script.joinAt, ...(script.reloadAt ?? [])].filter(([, at]) => at === second)
    for (const [id] of arriving) {
      const joiner = await open(id)
      if (!roster.includes(id)) roster = [...roster, id]
      await alice.sync(roster)
      const offer = only(await joiner.sync(roster), 'mls_key_package')
      const bobHears = second <= (script.bobUntil ?? 0)
      if (bobHears) {
        await bob.sync(roster)
        await bob.receive(id, offer)
      }
      const added = await alice.receive(id, offer)
      await joiner.receive(aliceId, only(added, 'mls_welcome'))
      if (bobHears) await bob.receive(aliceId, only(added, 'mls_public_message'))
      joiners.set(id, joiner)
    }
    if (second <= (script.bobUntil ?? 0)) for (const wire of (await bob.maintain()).wires) await alice.receive(bobId, wire)
    for (const [id, joiner] of joiners) if (!script.quiet?.includes(id)) for (const wire of (await joiner.maintain()).wires) await alice.receive(id, wire)
    const step = await alice.maintain()
    for (const joiner of joiners.values()) for (const wire of step.wires) await joiner.receive(aliceId, wire)
    if (commits(step).length && first === null) {
      first = second
      const heard = [...joiners].find(([id]) => !script.quiet?.includes(id))
      held = (await heard?.[1].sync(roster))?.verification?.members
    }
  }
  for (const group of [alice, bob, ...joiners.values()]) await group.close()
  return { first, held }
}

describe('the committer removing a member that went silent', () => {
  it('removes by its own count when nobody came in lately', async () => {
    const { first } = await room({ joinAt: [] })
    expect(first).toBeGreaterThanOrEqual(31)
    expect(first).toBeLessThanOrEqual(33)
  }, 120_000)

  it('waits until the newest member has been in for the patience and an interval, and that member takes the removal', async () => {
    // Bob speaks until 30 s. Carol comes in at 45 s and is heard: a removal of Bob at 61 s would be refused by
    // her (she counts him alive from 45 s on), so Alice removes him after 78 s.
    const { first, held } = await room({ joinAt: [[carolId, 45]], bobUntil: 30 })
    expect(first).toBeGreaterThanOrEqual(79)
    expect(first).toBeLessThanOrEqual(81)
    expect(held).toEqual([aliceId, carolId])
  }, 120_000)

  it('starts the wait again when a member reloads and its leaf is swapped', async () => {
    // Carol came in at 20 s and reloads at 50 s: the new tab counts everyone alive from 50 s on.
    const { first, held } = await room({ joinAt: [[carolId, 20]], reloadAt: [[carolId, 50]], bobUntil: 30 })
    expect(first).toBeGreaterThanOrEqual(84)
    expect(first).toBeLessThanOrEqual(86)
    expect(held).toEqual([aliceId, carolId])
  }, 120_000)

  it('does not wait for a newcomer that is silent itself', async () => {
    // Bob is silent from 30 s, Carol from her join at 45 s: both go at 76 s, not after Carol's own wait.
    const { first } = await room({ joinAt: [[carolId, 45]], quiet: [carolId], bobUntil: 30 })
    expect(first).toBeGreaterThanOrEqual(76)
    expect(first).toBeLessThanOrEqual(77)
  }, 120_000)

  it('removes a member silent for twice the limit whoever came in lately', async () => {
    // Carol at 20 s and Dave at 50 s: at 52 s Dave is still new, but Bob has been silent for 60 s.
    const { first } = await room({ joinAt: [[carolId, 20], [daveId, 50]] })
    expect(first).toBeGreaterThanOrEqual(61)
    expect(first).toBeLessThanOrEqual(63)
  }, 120_000)
})

describe('a member taking over from a lowest id that went silent', () => {
  it('removes it only once a newcomer has stopped taking it for the committer, and the newcomer takes the commit', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: START })
    const alice = await open(aliceId)
    const bob = await open(bobId)
    await alice.sync([aliceId])
    let roster = [aliceId, bobId]
    await alice.sync(roster)
    await bob.receive(aliceId, only(await alice.receive(bobId, only(await bob.sync(roster), 'mls_key_package')), 'mls_welcome'))
    // Carol comes in at 20 s through Alice's last commit; Alice says nothing after it.
    vi.setSystemTime(START + 20_000)
    const carol = await open(carolId)
    roster = [aliceId, bobId, carolId]
    for (const group of [alice, bob]) await group.sync(roster)
    const offer = only(await carol.sync(roster), 'mls_key_package')
    await bob.receive(carolId, offer)
    const added = await alice.receive(carolId, offer)
    const bobSaw = await bob.receive(aliceId, only(added, 'mls_public_message'))
    expect(bobSaw.verification?.members).toEqual([aliceId, bobId, carolId])
    await carol.receive(aliceId, only(added, 'mls_welcome'))
    let first: number | null = null
    let held: string[] | undefined
    for (let second = 21; second <= 70 && first === null; second++) {
      vi.setSystemTime(START + second * 1000)
      for (const wire of (await carol.maintain()).wires) await bob.receive(carolId, wire)
      const step = await bob.maintain()
      for (const wire of step.wires) await carol.receive(bobId, wire)
      if (commits(step).length) {
        first = second
        held = (await carol.sync(roster)).verification?.members
      }
    }
    // Alice counts as the committer to Carol until 52 s (the limit and two intervals after Carol came in).
    expect(first).toBe(54)
    expect(held).toEqual([bobId, carolId])
    for (const group of [alice, bob, carol]) await group.close()
  }, 120_000)
})

describe('a member that starts over into a group with the same members', () => {
  it('counts them as new from the start of that group, not from the group it left', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: START })
    const alice = await open(aliceId)
    const bob = await open(bobId)
    const carol = await open(carolId)
    const roster = [aliceId, bobId, carolId]
    await alice.sync([aliceId])
    for (const joiner of [bob, carol]) {
      await alice.sync(roster)
      if (joiner === carol) await bob.sync(roster)
      const offer = only(await joiner.sync(roster), 'mls_key_package')
      if (joiner === carol) await bob.receive(carolId, offer)
      const added = await alice.receive(joiner === bob ? bobId : carolId, offer)
      await joiner.receive(aliceId, only(added, 'mls_welcome'))
      if (joiner === carol) await bob.receive(aliceId, only(added, 'mls_public_message'))
    }
    // Bob has known Alice and Carol since the start. He loses his group and is let into a new one 100 s later.
    vi.setSystemTime(START + 100_000)
    ;(bob as unknown as { discard(): void }).discard()
    const offered = await bob.sync(roster)
    const offer = offered.wires.some(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_key_package') ? offered : await bob.sync([bobId, aliceId])
    const again = await alice.receive(bobId, only(offer, 'mls_key_package'))
    await bob.receive(aliceId, only(again, 'mls_welcome'))
    // A removal of Carol would be refused by Alice, who came in a moment ago for Bob: he waits for her, and not for 100 s.
    const entries = (bob as unknown as { entries: { holds(silent: ReadonlySet<string>, self: string, now: number, timing: MediaTiming): boolean } }).entries
    expect(entries.holds(new Set([carolId]), bobId, START + 101_000, timing)).toBe(true)
    expect(entries.holds(new Set([carolId]), bobId, START + 134_000, timing)).toBe(false)
    for (const group of [alice, bob, carol]) await group.close()
  }, 120_000)
})
