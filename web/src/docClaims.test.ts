// The documents state numbers that the code defines elsewhere; a test that
// imports the constant stays green when the sentence goes wrong. These pin the
// sentences to the constants. The it.fails cases are documentation findings of
// the 2026-11 hunt (H-L3-07): they turn red the day the sentence is corrected.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { PREVIOUS_EPOCH_MS } from './lib/media/bounds'
import { FRAME_OVERHEAD_BYTES } from './lib/media/frame'

const root = join(import.meta.dirname, '..', '..')
const doc = (path: string) => readFileSync(join(root, path), 'utf8').replace(/\s+/g, ' ')

describe('numbers in the documents against the code', () => {
  it('SECURITY.md: receivers hold the previous epoch key for PREVIOUS_EPOCH_MS', () => {
    expect(doc('SECURITY.md')).toContain(`previous epoch's key for ${PREVIOUS_EPOCH_MS / 1000} seconds`)
  })

  // SECURITY.md:796 gives a second, different retention for the same key.
  it.fails('SECURITY.md states one retention for the previous epoch key', () => {
    expect(doc('SECURITY.md')).not.toContain("The previous epoch's key is kept until the next rotation")
  })

  // doc/architecture.md:371: an empty DTX frame sealed is the overhead plus a payload byte or two.
  it.fails('doc/architecture.md: the size of a sealed empty DTX frame', () => {
    const stated = Number(/sealed they would be (\d+) bytes each/.exec(doc('doc/architecture.md'))?.[1])
    expect(stated).toBeGreaterThan(FRAME_OVERHEAD_BYTES)
  })

  // doc/operations.md:1478 and the alert: 800 MB of the 1g compose default.
  it.fails('doc/operations.md: the memory alert as a share of the default limit', () => {
    const limit = /mem_limit: \$\{APP_MEM_LIMIT:-(\d+)g\}/.exec(doc('docker-compose.yml'))?.[1]
    const share = Math.round((800e6 / (Number(limit) * 2 ** 30)) * 100)
    expect(doc('doc/operations.md')).toContain(`above 800 MB (${share}% of the default limit)`)
  })
})
