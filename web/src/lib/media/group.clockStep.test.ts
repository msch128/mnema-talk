// A step of one member's wall clock against the group's back-off. The key
// lifecycle runs on the monotonic clock (clock.ts), but the three Backoff
// instances of a group (resets, swaps, rivals; groupBase.ts) are built
// without a clock and read Date.now. A backward step of the wall clock by d
// on one machine keeps a back-off of that member that was open when the step
// came closed for d longer.
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

/** From now on `group`'s back-off reads a wall clock `ms` behind; returns a count of its refusals. */
function stepWallBack(group: MediaGroup, ms: number): { refused: number } {
  const tally = { refused: 0 }
  for (const name of ['resets', 'swaps', 'rivals'] as const) {
    const backoff = (group as unknown as Record<string, Clocked>)[name] as Clocked
    // Only a back-off that reads the wall clock sees the step: the monotonic
    // clock is MONOTONIC_ORIGIN away from it here, so a fixed back-off is not hit.
    const read = backoff.clock
    const wall = Math.abs(read() - Date.now()) < 1000
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
async function apartAfterSecondReload(step: number): Promise<{ apart: number, refused: number }> {
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
  return { apart, refused: tally.refused }
}

describe('a backward step of the committer wall clock', () => {
  // The rig: without a step the second reload costs nothing; with one the
  // stepped back-off really refuses, and the cost grows with the step (the
  // removal of the silent old leaf after the absence limit ends it at about
  // 22 minutes).
  it('measures the cost of the step (H-L9-02)', async () => {
    const steady = await apartAfterSecondReload(0)
    const small = await apartAfterSecondReload(5 * MINUTE)
    const large = await apartAfterSecondReload(STEP)
    expect(steady.refused).toBe(0)
    expect(steady.apart).toBeLessThan(60)
    expect(small.refused).toBeGreaterThan(0)
    expect(large.refused).toBeGreaterThan(0)
    expect(small.apart).toBeGreaterThan(4 * 60)
    expect(large.apart).toBeGreaterThan(15 * 60)
    note(`seconds without a common key after the second reload: ${steady.apart} without a step, ${small.apart} after a step back of 5 min, ${large.apart} after ${STEP / MINUTE} min (${large.refused} refusals)`)
  }, 600_000)

  // KNOWN DEFECT (H-L9-02). Wanted: a wall-clock step costs nothing, as the
  // key lifecycle promises for its own deadlines (clock.ts). Turns red the
  // day the back-off reads the monotonic clock.
  it.fails('costs a member that reloads twice no more than a minute', async () => {
    const stepped = await apartAfterSecondReload(STEP)
    expect(stepped.apart).toBeLessThan(60)
  }, 600_000)
})
