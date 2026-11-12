// A step of one member's wall clock against the group's back-off. The key
// lifecycle runs on the monotonic clock (clock.ts). Before the fix of H-L9-02
// the three Backoff instances of a group (resets, swaps, rivals; groupBase.ts)
// were built without a clock and read Date.now: a backward step of the wall
// clock by d on one machine kept a back-off of that member that was open when
// the step came closed for d longer (314 s without a common key for a 5 min
// step, 1304 s for 60 min). They now wait on the monotonic clock.
//
// The step is modelled on the member's Backoff clocks only: in production the
// only other reader of that member's wall clock is the sleep check
// (groupBase.ts, the larger of the monotonic and the wall gap), which a
// backward step does not move. Everything else, the fake timers included,
// runs on the shared monotonic test clock, as setTimeout does in a browser.
import { webcrypto } from 'node:crypto'
import { appendFileSync } from 'node:fs'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { clock } from './clock'
import type { MediaGroup } from './group'
import { ROOM_IDS, TalkRoom } from './groupRoom.fixture'

const previous = globalThis.crypto
const previousNow = clock.now
/** The monotonic clock starts elsewhere than the wall clock, as performance.timeOrigin + performance.now() does. */
const MONOTONIC_ORIGIN = 7 * 24 * 60 * 60_000
beforeAll(() => {
  vi.stubGlobal('crypto', webcrypto)
  clock.now = () => Date.now() + MONOTONIC_ORIGIN
})
afterAll(() => {
  vi.stubGlobal('crypto', previous)
  clock.now = previousNow
})

const MINUTE = 60_000
const STEP = 60 * MINUTE

/** With ROOM_OUT set, the measured numbers go to that file (as in groupMerge.room.test.ts). */
function note(text: string) {
  if (process.env.ROOM_OUT) appendFileSync(process.env.ROOM_OUT, `${text}\n`)
}

interface Clocked { clock: () => number, allow: (key: string) => boolean }

/** From now on `group`'s back-off reads a wall clock `ms` behind; counts its refusals and the back-offs that read the wall clock. */
function stepWallBack(group: MediaGroup, ms: number): { refused: number, wall: number } {
  const tally = { refused: 0, wall: 0 }
  for (const name of ['resets', 'swaps', 'rivals'] as const) {
    const backoff = (group as unknown as Record<string, Clocked>)[name] as Clocked
    // Only a back-off that reads the wall clock sees the step: the monotonic
    // clock is MONOTONIC_ORIGIN away from it here, so a fixed back-off is not hit.
    const read = backoff.clock
    const wall = Math.abs(read() - Date.now()) < 1000
    if (wall) tally.wall++
    backoff.clock = wall ? () => read() - ms : read
    const allow = backoff.allow.bind(backoff)
    backoff.allow = (key: string) => {
      const allowed = allow(key)
      if (!allowed) tally.refused++
      return allowed
    }
  }
  return tally
}

/** Carol's tab reloads: her group goes, a new one under the same id joins. */
async function reload(room: TalkRoom, roster: string[]) {
  const carol = roster[2] as string
  await room.hang(carol)
  await room.join(carol, roster)
}

/**
 * Carol reloads, the committer Alice swaps her leaf in; Alice's wall clock
 * steps back by `step`; ten seconds after the first reload Carol reloads
 * again (a second F5). Returns how many seconds of the next hour the room
 * had no common key, and how often Alice's stepped back-off refused.
 */
async function apartAfterSecondReload(step: number): Promise<{ apart: number, refused: number, wall: number }> {
  const room = new TalkRoom()
  const roster = ROOM_IDS.slice(0, 3)
  await room.form(roster)
  expect(room.agreed()).toBe(true)
  await reload(room, roster)
  expect(await room.run(MINUTE, () => room.agreed())).toBe(true)
  const tally = stepWallBack(room.groups.get(roster[0] as string) as MediaGroup, step)
  await room.run(5000)
  await reload(room, roster)
  const apart = await room.secondsApart(60 * MINUTE)
  await room.close()
  return { apart, refused: tally.refused, wall: tally.wall }
}

describe('a backward step of the committer wall clock', () => {
  // H-L9-02: the room shares one key again within a minute of the second
  // reload, with or without a step, as the key lifecycle promises for its own
  // deadlines (clock.ts). On the wall clock the stepped runs took 314 s and
  // 1304 s, the 60 min one capped by the removal of the silent old leaf.
  it('costs a member that reloads twice no more than a minute', async () => {
    const steady = await apartAfterSecondReload(0)
    const small = await apartAfterSecondReload(5 * MINUTE)
    const large = await apartAfterSecondReload(STEP)
    expect([steady.apart, small.apart, large.apart], 'seconds without a common key').toSatisfy((apart: number[]) => apart.every(s => s < 60))
    expect([steady.wall, small.wall, large.wall], 'back-offs that read the wall clock').toEqual([0, 0, 0])
    note(`seconds without a common key after the second reload: ${steady.apart} without a step, ${small.apart} after a step back of 5 min, ${large.apart} after ${STEP / MINUTE} min (${large.refused} refusals)`)
  }, 600_000)
})
