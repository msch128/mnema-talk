// The cross check of what is hidden (see plainCross.fixture.ts). Modes:
// - default: the corpus the Go side reads (CROSS_FILE) is still what the
//   renderer says, and the web plain form and snippet show nothing it hides;
// - CROSS_UPDATE=1 rewrites the corpus;
// - CROSS_SWEEP=1 [CROSS_N=<n>] [CROSS_SEED=<s>]: the web check on n messages (deep);
// - CROSS_OUT=<file> [CROSS_N] [CROSS_SEED]: writes n records for the Go side's
//   deep run (CROSS_FILE=<file> go test -run TestPlainHidesWhatTheRendererHides ./internal/mdplain).
import { beforeEach, describe, expect, it } from 'vitest'
import { appendFileSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import '../customEmoji'
import { setLocale } from '../../../i18n'
import { previewText } from '../../../chat/lib/replies'
import { markdownSnippet } from './snippet'
import { rng } from './linkParity.fixture'
import { CROSS_FILE, crossMessage, crossRecord, leaked } from './plainCross.fixture'

beforeEach(() => setLocale('en'))

const SMOKE_SEED = 1
const SMOKE_N = 1000
const env = process.env
const corpusPath = join(process.cwd(), '..', CROSS_FILE)

function* records(seed: number, n: number) {
  const r = rng(seed)
  for (let i = 0; i < n; i++) yield crossRecord(crossMessage(r, i))
}

describe('what the renderer hides, judged against the plain forms', () => {
  it('the corpus the server reads is what the renderer says', () => {
    const lines = [...records(SMOKE_SEED, SMOKE_N)].map(rec => JSON.stringify(rec))
    if (env.CROSS_UPDATE) writeFileSync(corpusPath, lines.join('\n') + '\n')
    const stored = readFileSync(corpusPath, 'utf8').trimEnd().split('\n')
    expect(stored.length).toBe(SMOKE_N)
    stored.forEach((line, i) => expect(line, `record ${i}: run with CROSS_UPDATE=1`).toBe(lines[i]))
  })

  it('the web plain form and snippet show nothing the renderer hides', () => {
    const n = Number(env.CROSS_SWEEP ? env.CROSS_N ?? '100000' : '2000')
    const seed = Number(env.CROSS_SEED ?? '2')
    const bad: string[] = []
    let hiddenTotal = 0
    let leaks = 0
    for (const rec of records(seed, n)) {
      hiddenTotal += rec.hidden.length
      if (leaked(rec.hidden, rec.plain).length + leaked(rec.hidden, previewText(markdownSnippet(rec.input))).length === 0) continue
      leaks++
      if (bad.length < 5) bad.push(rec.input)
    }
    console.log(`cross (web): ${n} messages, seed ${seed}, ${hiddenTotal} hidden canaries, ${leaks} leaks`)
    expect(bad).toEqual([])
  }, 3_600_000)

  it.runIf(env.CROSS_OUT)('writes records for the server', () => {
    const out = env.CROSS_OUT ?? ''
    const n = Number(env.CROSS_N ?? '100000')
    writeFileSync(out, '')
    let chunk: string[] = []
    for (const rec of records(Number(env.CROSS_SEED ?? '2'), n)) {
      chunk.push(JSON.stringify(rec))
      if (chunk.length === 10000) { appendFileSync(out, chunk.join('\n') + '\n'); chunk = [] }
    }
    if (chunk.length) appendFileSync(out, chunk.join('\n') + '\n')
  }, 3_600_000)
})
