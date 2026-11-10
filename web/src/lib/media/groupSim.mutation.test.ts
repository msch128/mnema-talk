// Proof that the simulator notices a broken tie-break. The protocol's one
// tie-break for who commits is "the lowest id" (lowest() in groupTree.ts).
// Here that function is replaced, for this file only, by one that is wrong,
// and the same seeds that pass in groupSim.test.ts must then report a breach.
// Nothing in the product changes: the replacement lives in vi.mock.
import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { runSeed } from './groupSimRun.fixture'

const tieBreak = vi.hoisted(() => ({ mode: 'lowest' as 'lowest' | 'highest' | 'none' }))

vi.mock('./groupTree', async (original) => {
  const real = await original<typeof import('./groupTree')>()
  const broken = (ids: Iterable<string>): string | null => {
    if (tieBreak.mode === 'lowest') return real.lowest(ids)
    let best: string | null = null
    for (const id of ids) {
      // 'none' takes whoever comes first; 'highest' turns the order around.
      if (best === null || (tieBreak.mode === 'highest' && id > best)) best = id
    }
    return best
  }
  return { ...real, lowest: broken }
})

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { tieBreak.mode = 'lowest' })

async function breaches(mode: typeof tieBreak.mode): Promise<string[]> {
  tieBreak.mode = mode
  const hostile = await runSeed(140, { relay: 'hostile' })
  const honest = await runSeed(9, { relay: 'honest' })
  return [...hostile.violations, ...honest.violations]
}

describe('the simulator against a broken committer tie-break', () => {
  it('passes these seeds with the real tie-break', async () => {
    expect(await breaches('lowest')).toEqual([])
  }, 60_000)

  it('reports a commit or welcome from the wrong member when the highest id commits', async () => {
    const found = await breaches('highest')
    expect(found.some(line => line.includes('[4]'))).toBe(true)
  }, 120_000)

  it('reports a commit or welcome from the wrong member when nothing breaks the tie', async () => {
    const found = await breaches('none')
    expect(found.some(line => line.includes('[4]'))).toBe(true)
  }, 120_000)
})
