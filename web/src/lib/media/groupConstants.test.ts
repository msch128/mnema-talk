// The numbers SECURITY.md ("Voice and screenshare") states about the media group, as literals, so that a change
// of a constant fails here and sends the person who changed it to the sentence that is no longer true. The
// other tests import these constants and so cannot notice a change.
import { describe, expect, it } from 'vitest'
import { Backoff } from './backoff'
import { ABSENT_MS, HEARTBEAT_MS, MAINTENANCE_MS, PREVIOUS_EPOCH_MS, ROTATE_JITTER_MS, ROTATE_MS, SEND_GRACE_MS } from './bounds'
import { ASK_BASE_MS, ASK_MAX_MS, BACKOFF_MAX_MS, CONFLICT_MS, OTHER_STATE_BEATS, OTHER_STATE_MS, RESET_BASE_MS, RIVAL_BASE_MS, RIVAL_MAX_MS } from './groupParams'

const SECOND = 1000
const MINUTE = 60 * SECOND

describe('the constants of the media group that SECURITY.md states', () => {
  it('key lifetime: 30 minutes, plus or minus 2 minutes; a heartbeat every 5; absent after 20; the maintenance tick every 30 seconds', () => {
    expect([ROTATE_MS, ROTATE_JITTER_MS, HEARTBEAT_MS, ABSENT_MS, MAINTENANCE_MS]).toEqual([30 * MINUTE, 2 * MINUTE, 5 * MINUTE, 20 * MINUTE, 30 * SECOND])
  })

  it('the previous epoch opens frames for 30 seconds; the sender keeps the epoch it left for 1.5 seconds after a commit that removes nobody', () => {
    expect([PREVIOUS_EPOCH_MS, SEND_GRACE_MS]).toEqual([30 * SECOND, 1500])
  })

  it('a start-over: once per 10 seconds, doubling to a minute', () => {
    expect([RESET_BASE_MS, BACKOFF_MAX_MS]).toEqual([10 * SECOND, MINUTE])
  })

  it('a rival welcome: once per minute per sender, doubling to ten minutes; an offer: one minute, doubling to ten, and a sign older than ten is none', () => {
    expect([RIVAL_BASE_MS, RIVAL_MAX_MS, ASK_BASE_MS, ASK_MAX_MS]).toEqual([MINUTE, 10 * MINUTE, MINUTE, 10 * MINUTE])
  })

  it('two heartbeats of the lowest id: at least 8 seconds apart and within three heartbeat periods; a lost self-update race takes 10 seconds', () => {
    expect([OTHER_STATE_MS, OTHER_STATE_BEATS, CONFLICT_MS]).toEqual([8 * SECOND, 3, 10 * SECOND])
  })
})

describe('the wait that doubles', () => {
  it('lets an action through once, then after 1, 2, 4 ... times the base, up to the ceiling, and from the base again after a quiet time', () => {
    let now = 0
    const limit = new Backoff(RIVAL_BASE_MS, RIVAL_MAX_MS, () => now)
    const waits: number[] = []
    for (let round = 0; round < 6; round++) {
      expect(limit.allow('a')).toBe(true)
      waits.push(limit.waitMs('a'))
      now += limit.waitMs('a') - 1
      expect(limit.allow('a')).toBe(false)
      now += 1
    }
    expect(waits).toEqual([MINUTE, 2 * MINUTE, 4 * MINUTE, 8 * MINUTE, 10 * MINUTE, 10 * MINUTE])
    now += 10 * MINUTE + 1
    expect(limit.allow('a')).toBe(true)
    expect(limit.waitMs('a')).toBe(MINUTE)
  })

  it('counts every sender apart', () => {
    const limit = new Backoff(RIVAL_BASE_MS, RIVAL_MAX_MS, () => 0)
    expect(limit.allow('a')).toBe(true)
    expect(limit.allow('b')).toBe(true)
    expect(limit.allow('a')).toBe(false)
  })
})
