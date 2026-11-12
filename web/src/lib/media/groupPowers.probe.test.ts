import { writeFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, it, vi } from 'vitest'
import { liarPlan } from './groupPowers.fixture'
import { runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe.skipIf(!process.env.PROBE)('probe', () => {
  it('shows the trace before a violation', async () => {
    const result = await runSeed(Number(process.env.PROBE), liarPlan({ relay: 'honest' }))
    const at = result.trace.findIndex(line => line.includes('VIOLATION'))
    writeFileSync('/Users/m/mnema-hunt/inventory/work/l2/probe-liar.txt', result.trace.slice(Math.max(0, at - 45), at + 3).join('\n'))
  }, 300_000)
})
