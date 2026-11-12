// The hard limit of the nightly availability budget: the shards of groupSim.sweep.test.ts write their sums to a
// directory (SIM_POOL_OUT), the job "availability" of sim-sweep.yml collects them and runs
//   SIM_POOL_DIR=<dir> npx vitest run src/lib/media/groupAvailability.pool.test.ts
// Every shard of both relays must be there once, and each relay's share of member time lost after holding keys,
// pooled over all 400 seeds, must lie within POOL_WINDOW of LOST_BUDGET.<relay>.pooled (groupAvailability.fixture.ts).
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { poolBreaches, type ShardSums } from './groupAvailability.fixture'

describe.skipIf(!process.env.SIM_POOL_DIR)('the availability of all shards pooled', () => {
  it('is within the budget on both relays', () => {
    const dir = process.env.SIM_POOL_DIR as string
    const rows = readdirSync(dir).filter(name => /^pool-\w+-\d+\.json$/.test(name)).map(name => JSON.parse(readFileSync(join(dir, name), 'utf8')) as ShardSums)
    expect(poolBreaches(rows)).toEqual([])
  })
})
