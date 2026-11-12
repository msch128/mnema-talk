import { writeFileSync } from 'node:fs'
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, it, vi } from 'vitest'
import { CLOCK_VARIANTS } from './groupClocks.fixture'
import { runSeed } from './groupSimRun.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe.skipIf(!process.env.PROBE)('probe', () => {
  it('trace', async () => {
    const result = await runSeed(Number(process.env.PROBE), { relay: 'honest', clocks: CLOCK_VARIANTS[process.env.VARIANT ?? 'steps']! })
    const at = result.trace.findIndex(line => line.includes('the relay stops'))
    const keep = result.trace.filter((line, i) => /step m|sleep|reload|leave|join|crash|keys none|VIOLATION|agreed after|relay stops|commit@\d+\[remove/.test(line) && i < result.trace.length)
    writeFileSync(process.env.OUT ?? '/tmp/probe.txt', `${at}\n${keep.slice(0, 160).join('\n')}`)
  }, 300_000)
})
