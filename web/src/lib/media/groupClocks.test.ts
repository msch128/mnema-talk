// A clock of its own for every member (ClockPlan in groupSimRun.fixture.ts), on a few seeds of the honest relay:
// the perturbation must happen in the run, and the invariants must hold. The whole range runs in groupClocks.sweep.test.ts.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { CLOCK_VARIANTS } from './groupClocks.fixture'
import { liarPlan } from './groupPowers.fixture'
import { occurrences, report, runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const SEEDS = [4, 9, 12]

describe('members with clocks of their own', () => {
  it.each(['offset', 'drift', 'steps', 'all'])('keep the invariants and really run on other clocks: %s', async (variant) => {
    const clocks = CLOCK_VARIANTS[variant]!
    let stepped = 0
    for (const seed of SEEDS) {
      const result = await runSeed(seed, { relay: 'honest', clocks })
      expect(result.violations, report(result)).toEqual([])
      expect(occurrences(result, /clock of m\d+: offset/), `${variant} seed ${seed}: every member has a clock`).toBeGreaterThan(1)
      stepped += occurrences(result, / step m\d+#\d+ wall clock by /)
    }
    if (CLOCK_VARIANTS[variant]!.stepMs) expect(stepped, `${variant}: wall clock steps in the runs`).toBeGreaterThan(0)
  }, 60_000)

  it('a roster lie is told to somebody in these runs', async () => {
    let lies = 0
    for (const seed of SEEDS) lies += occurrences(await runSeed(seed, liarPlan({ relay: 'honest' })), /lie to /)
    expect(lies).toBeGreaterThan(0)
  }, 60_000)
})
