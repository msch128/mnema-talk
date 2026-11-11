// A Talk watched for an hour on production timing: a lower id that is listed
// but holds no group must cost the others nothing, and two groups must become
// one within about a maintenance interval once the lowest id's group shows itself.
import { webcrypto } from 'node:crypto'
import { appendFileSync } from 'node:fs'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { MediaGroup } from './group'
import { PRODUCTION, ROOM_CHANNEL, ROOM_IDS, TalkRoom, wireKind } from './groupRoom.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const HOUR = 60 * 60_000
/** With ROOM_OUT set, the measured numbers go to that file. */
function note(text: string) {
  if (process.env.ROOM_OUT) appendFileSync(process.env.ROOM_OUT, `${text}\n`)
}
function tally(room: TalkRoom): string {
  return `commits ${room.count('commit')}, key packages ${room.count('key-package')}, welcomes ${room.count('welcome')}, heartbeats ${room.count('heartbeat')}, capabilities ${room.count('capability')}`
}

/** A commit for this Talk made by a pair that formed elsewhere: under a listed id it looks like that id's group. */
async function foreignCommit() {
  const [first, second] = [ROOM_IDS[10] as string, ROOM_IDS[11] as string]
  const founder = await MediaGroup.open(first, ROOM_CHANNEL, PRODUCTION) as MediaGroup
  const joiner = await MediaGroup.open(second, ROOM_CHANNEL, PRODUCTION) as MediaGroup
  await founder.sync([first])
  await founder.sync([first, second])
  const offer = (await joiner.sync([first, second])).wires[0] as Uint8Array
  const commit = (await founder.receive(second, offer)).wires.find(wire => wireKind(wire) === 'commit') as Uint8Array
  for (const group of [founder, joiner]) await group.close()
  return commit
}

describe('a lower id that the Talk lists but that holds no group', () => {
  it.each([3, 12])('costs nothing when it is the lowest tab and hangs in a room of %i', async (size) => {
    const room = new TalkRoom()
    const everyone = ROOM_IDS.slice(0, size)
    const lowestId = everyone[0] as string
    // It joined last and was let in, so nobody holds a key package of it that was never used.
    await room.form([...everyone.slice(1), lowestId])
    expect(room.agreed()).toBe(true)
    await room.hang(lowestId)
    room.sent.clear()
    const apart = await room.secondsApart(HOUR)
    note(`hung lowest tab, room of ${size}: ${apart} s of 3600 s without a common key; ${tally(room)}`)
    expect(apart).toBe(0)
    expect(room.count('key-package')).toBe(0)
    expect(room.count('welcome')).toBe(0)
    await room.close()
  }, 300_000)

  it.each([4, 8])('costs nothing when it sent one key package that is not its own and then stays silent, room of %i', async (size) => {
    const room = new TalkRoom()
    const present = ROOM_IDS.slice(1, size)
    await room.form(present)
    await room.run(60_000)
    const silent = ROOM_IDS[0] as string
    const stranger = await MediaGroup.open(ROOM_IDS[12] as string, ROOM_CHANNEL, PRODUCTION) as MediaGroup
    const foreign = (await stranger.sync([silent, ROOM_IDS[12] as string])).wires[0] as Uint8Array
    await room.list([silent, ...present])
    await room.inject(silent, foreign)
    room.sent.clear()
    const apart = await room.secondsApart(HOUR / 2)
    note(`silent listed lower id, room of ${size}: ${apart} s of 1800 s without a common key; ${tally(room)}`)
    expect(apart).toBe(0)
    expect(room.count('key-package')).toBe(0)
    expect(room.count('welcome')).toBe(0)
    await stranger.close()
    await room.close()
  }, 300_000)

  it('is asked once by each member when it sends what looks like a commit, and their group goes on', async () => {
    const room = new TalkRoom()
    const present = ROOM_IDS.slice(1, 4)
    await room.form(present)
    const silent = ROOM_IDS[0] as string
    const commit = await foreignCommit()
    await room.list([silent, ...present])
    room.sent.clear()
    let apart = 0
    for (let minute = 0; minute < 60; minute++) {
      // It goes on showing that commit every minute for the whole hour.
      await room.inject(silent, commit)
      apart += await room.secondsApart(60_000)
    }
    note(`listed lower id that shows a commit and never answers, room of 4: ${apart} s of 3600 s without a common key; ${tally(room)}`)
    expect(apart).toBe(0)
    expect(room.count('welcome')).toBe(0)
    // Offers go out 0, 1, 3, 7, 15, 25, 35, 45 and 55 minutes after the first: the wait doubles to ten minutes.
    expect(room.count('key-package')).toBeLessThanOrEqual(3 * 9)
    expect(room.count('key-package')).toBeGreaterThanOrEqual(3)
    await room.close()
  }, 300_000)

  it('is asked no more often when it leaves the Talk and comes back each time', async () => {
    const room = new TalkRoom()
    const present = ROOM_IDS.slice(1, 4)
    await room.form(present)
    const silent = ROOM_IDS[0] as string
    const commit = await foreignCommit()
    await room.list([silent, ...present])
    room.sent.clear()
    let apart = 0
    for (let cycle = 0; cycle < 51; cycle++) {
      await room.inject(silent, commit)
      apart += await room.secondsApart(35_000)
      await room.list(present)
      apart += await room.secondsApart(35_000)
      await room.list([silent, ...present])
    }
    note(`listed lower id that shows a commit, leaves for 35 s and returns, 51 times in an hour, room of 4: ${apart} s without a common key; ${tally(room)}`)
    expect(apart).toBe(0)
    expect(room.count('key-package')).toBeLessThanOrEqual(3 * 9)
    await room.close()
  }, 300_000)

  it('costs a member nothing whose group had shrunk to one and grew again', async () => {
    const room = new TalkRoom()
    const [silent, bob, carol, dave, eve] = ROOM_IDS as [string, string, string, string, string]
    // Bob and Eve hold a group, Eve leaves the Talk, Carol and Dave join Bob.
    await room.form([bob, eve])
    await room.hang(eve)
    await room.list([bob])
    await room.run(3000)
    await room.join(carol, [bob, carol])
    await room.run(3000)
    await room.join(dave, [bob, carol, dave])
    await room.run(3000)
    const commit = await foreignCommit()
    await room.list([silent, bob, carol, dave])
    room.sent.clear()
    let apart = 0
    for (let minute = 0; minute < 20; minute++) {
      await room.inject(silent, commit)
      apart += await room.secondsApart(60_000)
    }
    note(`group that shrank to one and grew again, listed lower id shows a commit every minute, 20 min: ${apart} s without a common key; ${tally(room)}`)
    // Alone, Bob made himself a package under a new signature key. What he offers is under his leaf's key all the same.
    expect(apart).toBe(0)
    expect(room.count('welcome')).toBe(0)
    expect(room.count('key-package')).toBeGreaterThanOrEqual(3)
    await room.close()
  }, 300_000)
})

describe('two groups that formed apart and then share a Talk', () => {
  it.each([[2, 2], [2, 4], [6, 6]])('of %i and %i members are one group about a maintenance interval after the lowest id\'s group shows a commit', async (low, high) => {
    // Nobody sends media here, so no call starts over for frames of a newer epoch: what merges the halves is the offer alone.
    const room = new TalkRoom(false)
    const everyone = ROOM_IDS.slice(0, low + high)
    await room.form(everyone.slice(0, low), 0)
    await room.form(everyone.slice(low), 1)
    for (const id of everyone) room.part.set(id, 0)
    await room.list(everyone)
    const joinedAt = Date.now()
    // Nobody has seen the other half do anything yet. The lowest id's next commit is its ordinary rotation.
    let shownAt = 0
    room.onWire = (from, kind) => {
      if (from === everyone[0] && kind === 'commit' && !shownAt) shownAt = Date.now()
    }
    room.sent.clear()
    expect(await room.run(HOUR, () => shownAt > 0)).toBe(true)
    expect(await room.run(HOUR, () => room.agreed())).toBe(true)
    const took = (Date.now() - shownAt) / 1000
    note(`${low}+${high}: the lowest id's group showed a commit ${(shownAt - joinedAt) / 1000} s after the halves met; one group ${took} s after that; ${tally(room)}`)
    expect(took).toBeLessThanOrEqual(PRODUCTION.maintenanceMs / 1000 + 5)
    // One offer and one welcome per member of the other half, and nobody had to start over.
    expect(room.count('key-package')).toBe(high)
    expect(room.count('welcome')).toBe(high)
    await room.close()
  }, 300_000)
})

describe('one group that split in two while its halves could not hear each other', () => {
  /** Everyone in one group, long enough for heartbeats; then the halves are cut off until each removed the other. */
  async function split(low: number, high: number) {
    const room = new TalkRoom(false)
    const everyone = ROOM_IDS.slice(0, low + high)
    const beats: Uint8Array[] = []
    room.onWire = (from, kind, wire) => {
      if (from === everyone[0] && kind === 'heartbeat') beats.push(wire)
    }
    await room.form(everyone)
    await room.run(6 * 60_000)
    expect(room.agreed()).toBe(true)
    const old = beats.length
    for (const id of everyone.slice(low)) room.part.set(id, 1)
    await room.run(25 * 60_000)
    expect(room.agreed()).toBe(false)
    return { room, everyone, beats, old }
  }

  it.each([[2, 2], [2, 4], [6, 6]])('of %i and %i members is one again a heartbeat period and a tick after they meet', async (low, high) => {
    const { room, everyone } = await split(low, high)
    for (const id of everyone) room.part.set(id, 0)
    room.sent.clear()
    const metAt = Date.now()
    expect(await room.run(HOUR, () => room.agreed())).toBe(true)
    const took = (Date.now() - metAt) / 1000
    note(`one group split ${low}+${high}, no media: one group ${took} s after the halves met; ${tally(room)}`)
    // The lowest id commits nothing in that time: what shows its group is its heartbeat, under the leaf key the others held for it.
    expect(took).toBeLessThanOrEqual((PRODUCTION.heartbeatMs + PRODUCTION.maintenanceMs) / 1000 + 5)
    expect(room.count('key-package')).toBe(high)
    expect(room.count('welcome')).toBe(high)
    await room.close()
  }, 300_000)

  it('is one again as fast when the lowest id left meanwhile and the next one never sent the others a handshake', async () => {
    const room = new TalkRoom(false)
    const everyone = ROOM_IDS.slice(0, 5)
    await room.form(everyone)
    await room.run(6 * 60_000)
    for (const id of everyone.slice(3)) room.part.set(id, 1)
    await room.run(25 * 60_000)
    // The second id joined before the last two and committed nothing while they were in: they hold its leaf key and no more.
    await room.hang(everyone[0] as string)
    const rest = everyone.slice(1)
    await room.list(rest)
    await room.run(10_000)
    for (const id of rest) room.part.set(id, 0)
    room.sent.clear()
    const metAt = Date.now()
    expect(await room.run(HOUR, () => room.agreed())).toBe(true)
    const took = (Date.now() - metAt) / 1000
    note(`one group split 3+2, the lowest id left during the cut, no media: one group ${took} s after the halves met; ${tally(room)}`)
    expect(took).toBeLessThanOrEqual((PRODUCTION.heartbeatMs + PRODUCTION.maintenanceMs) / 1000 + 5)
    await room.close()
  }, 300_000)

  it('takes only a new heartbeat of the lowest id for a sign, not one it has seen or an older one', async () => {
    const { room, everyone, beats, old } = await split(2, 2)
    const others = everyone.slice(2)
    room.sent.clear()
    // The halves still cannot hear each other. Heartbeats the others took while the group was whole come again.
    for (const wire of beats.slice(0, old)) await room.inject(everyone[0] as string, wire, others)
    await room.run(2 * PRODUCTION.maintenanceMs)
    expect(room.count('key-package')).toBe(0)
    // One the lowest id signed since reaches them: they ask it to let them in, once each.
    await room.inject(everyone[0] as string, beats.at(-1) as Uint8Array, others)
    await room.run(2 * PRODUCTION.maintenanceMs)
    expect(room.count('key-package')).toBe(2)
    // The same one again, and the one before it, say nothing more.
    for (const wire of beats.slice(-2)) await room.inject(everyone[0] as string, wire, others)
    await room.run(10 * 60_000)
    expect(room.count('key-package')).toBe(2)
    await room.close()
  }, 300_000)
})
