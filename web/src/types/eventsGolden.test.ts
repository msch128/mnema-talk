import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { decodeServerEvent } from './events'

// The Go tests pin every event the server writes to a connection in golden
// files (internal/events/eventstest). The web client has to accept each of
// those frames: one it rejects is dropped with "WS parse error".
const repoRoot = join(import.meta.dirname, '..', '..', '..')
const goldenDirs = ['internal/server/testdata', 'internal/ws/testdata']

interface GoldenFrame { file: string; step: string; line: string }

function goldenFrames(): GoldenFrame[] {
  const frames: GoldenFrame[] = []
  for (const dir of goldenDirs) {
    for (const file of readdirSync(join(repoRoot, dir)).filter(name => /^event_wire_.*\.golden$/.test(name)).sort()) {
      let step = ''
      for (const line of readFileSync(join(repoRoot, dir, file), 'utf8').split('\n')) {
        if (line.startsWith('# ')) step = line.slice(2)
        else if (line.startsWith('{')) frames.push({ file, step, line })
      }
    }
  }
  return frames
}

/** Puts real values where the golden file has the placeholders of eventstest.Normalize. */
function withValues(line: string): string {
  return line
    .replace(/<id-(\d+)>/g, (_, n: string) => `00000000-0000-4000-8000-${n.padStart(12, '0')}`)
    .replace(/<time>/g, '2026-01-02T03:04:05Z')
    .replace(/<n>/g, '7')
    .replace(/<hex>/g, '0123456789abcdef')
}

describe('the frames the server writes', () => {
  const frames = goldenFrames()

  it('are found in the golden files of the Go tests', () => {
    expect(frames.length).toBeGreaterThan(100)
  })

  it('are all accepted by the client', () => {
    const rejected: string[] = []
    for (const frame of frames) {
      try {
        if (decodeServerEvent(JSON.parse(withValues(frame.line))) === null) rejected.push(`${frame.file} (${frame.step}): unknown event ${frame.line}`)
      } catch {
        rejected.push(`${frame.file} (${frame.step}): ${frame.line}`)
      }
    }
    expect(rejected).toEqual([])
  })
})
