// The documents state numbers that the code defines elsewhere; a test that
// imports the constant stays green when the sentence goes wrong. These pin the
// sentences to the constants, so a sentence and its constant cannot drift apart
// again (H-L3-07).
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { MAINTENANCE_MS, PREVIOUS_EPOCH_MS, ROTATE_JITTER_MS, ROTATE_MS } from './lib/media/bounds'
import { FRAME_OVERHEAD_BYTES } from './lib/media/frame'

const root = join(import.meta.dirname, '..', '..')
const doc = (path: string) => readFileSync(join(root, path), 'utf8').replace(/\s+/g, ' ')

describe('numbers in the documents against the code', () => {
  it('SECURITY.md: receivers hold the previous epoch key for PREVIOUS_EPOCH_MS', () => {
    expect(doc('SECURITY.md')).toContain(`previous epoch's key for ${PREVIOUS_EPOCH_MS / 1000} seconds`)
  })

  it('SECURITY.md states one retention for the previous epoch key', () => {
    expect(doc('SECURITY.md')).not.toContain("The previous epoch's key is kept until the next rotation")
    expect(doc('SECURITY.md')).toContain(`previous epoch's key is kept for ${PREVIOUS_EPOCH_MS / 1000} seconds after a switch`)
  })

  it('doc/architecture.md: the export holds participation in an anonymous poll, never the choice', () => {
    expect(doc('doc/architecture.md')).not.toContain('has no entry for anonymous polls')
  })

  // An empty DTX frame sealed is the overhead plus one or two payload bytes.
  it('doc/architecture.md: the size of a sealed empty DTX frame', () => {
    const stated = Number(/sealed, each would be (\d+) bytes of overhead/.exec(doc('doc/architecture.md'))?.[1])
    expect(stated).toBe(FRAME_OVERHEAD_BYTES)
  })

  // The alert's threshold is read from the rule; the document and the alert's summary must name it and its share of the 1g compose default.
  it('doc/operations.md and the alert: the memory threshold as a share of the default limit', () => {
    const limit = /mem_limit: \$\{APP_MEM_LIMIT:-(\d+)g\}/.exec(doc('docker-compose.yml'))?.[1]
    const mb = Number(/process_resident_memory_bytes > (\d+)e6/.exec(doc('deploy/monitoring/mnema-alerts.yml'))?.[1])
    const share = Math.round(((mb * 1e6) / (Number(limit) * 2 ** 30)) * 100)
    expect(doc('doc/operations.md')).toContain(`above ${mb} MB (about ${share}% of the default limit)`)
    expect(doc('deploy/monitoring/mnema-alerts.yml')).toContain(`above ${mb} MB (about ${share}% of the default 1g`)
  })

  // The latest an awake committer advances an epoch: the rotation, its jitter and the maintenance tick that notices the old epoch.
  it('SECURITY.md: the stated key lifetime is the longest one the code allows', () => {
    const m = /advances the epoch at most (\d+(?:\.\d+)?) minutes after it began and receivers then keep the old key for (\d+) more seconds/.exec(doc('SECURITY.md'))
    expect(m).not.toBeNull()
    expect(Number(m?.[1]) * 60_000).toBe(ROTATE_MS + ROTATE_JITTER_MS + MAINTENANCE_MS)
    expect(Number(m?.[2]) * 1000).toBe(PREVIOUS_EPOCH_MS)
  })
})
