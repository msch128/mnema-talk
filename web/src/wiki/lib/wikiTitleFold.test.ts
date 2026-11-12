// One rule, two implementations: when two wiki titles are "the same page".
// The server refuses a title that SQL lower() maps onto an existing one; the
// client resolves [[Title]] with toLowerCase(). The pairs and the server's
// verdicts are in internal/server/testdata/wiki_title_pairs.json
// (internal/server/wiki_title_fold_integration_test.go). The check: the server
// refuses the second title exactly when the client would resolve [[second]]
// to the first page, and two titles that look the same (one NFC form) never
// both exist. Finding H-L3-09 of the 2026-11 hunt: canonically equivalent
// titles (NFC against NFD) are two pages. After changing the pairs:
//   WIKI_PAIRS_WRITE=1 npx vitest run src/wiki/lib/wikiTitleFold.test.ts
//   WIKI_PAIRS_UPDATE=1 go test -tags=integration -run TestWikiTitlePairsMatchTheServer ./internal/server
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { resolveWikiLink } from './wikiTree'
import type { WikiPageSummary } from './wikiApi'

const file = join(import.meta.dirname, '..', '..', '..', '..', 'internal/server/testdata/wiki_title_pairs.json')
interface Pair { a: string; b: string; conflict: boolean }

const WORDS = ['Caf\u00e9', 'Xa\u00dfo', '\u00c5ngstr\u00f6m', 'Istanbul', '\u03a3\u03bf\u03c6\u03af\u03b1', 'Kelvin', 'office', 'Ma\u00f1ana', '\u1e9ee']

/** Spellings of one title a reader may take for the same. */
function variants(w: string): string[] {
  return [...new Set([
    w, w.toUpperCase(), w.toLowerCase(), w.normalize('NFD'), w.normalize('NFC'), w.toUpperCase().normalize('NFD'),
    w.replace(/K/g, '\u212a'), w.replace(/\u00c5/g, '\u212b'), w.replace(/ffi/g, '\ufb03'), w.replace(/I/g, '\u0130'),
    w.replace(/\u03c3/g, '\u03c2'), w.replace(/\u00df/g, 'ss'), w.replace(/\u00df/g, '\u1e9e')
  ])]
}

function pairs(): Pair[] {
  const out: Pair[] = []
  for (const w of WORDS) {
    const vs = variants(w)
    for (const b of vs) if (b !== w) out.push({ a: w, b, conflict: false })
  }
  return out
}

const page = (title: string): WikiPageSummary => ({ id: 'p1', slug: 'first', title, parent_id: null, position: 0, locked: false, updated_at: '2026-01-01T00:00:00Z' })
const clientSame = (p: Pair) => resolveWikiLink([page(p.a)], p.b) === 'first'
const looksSame = (p: Pair) => p.a.normalize('NFC') === p.b.normalize('NFC')

const vectors = (): Pair[] => JSON.parse(readFileSync(file, 'utf8')) as Pair[]

describe('wiki titles: the server against the client', () => {
  it.runIf(process.env.WIKI_PAIRS_WRITE === '1')('writes the pairs', () => {
    writeFileSync(file, JSON.stringify(pairs(), null, 1) + '\n')
  })

  it('the vector file holds the generated pairs', () => {
    expect(vectors().map(p => [p.a, p.b])).toEqual(pairs().map(p => [p.a, p.b]))
  })

  it('the server refuses a title exactly when the client resolves it to the existing page, but for canonical equivalents and the known list', () => {
    const disagree = vectors().filter(p => !looksSame(p) && p.conflict !== clientSame(p)).map(p => `${JSON.stringify(p.b)} next to ${JSON.stringify(p.a)}: server ${p.conflict}, client ${clientSame(p)}`)
    // Known (H-L3-09): SQL lower() takes U+0130 to i, toLowerCase() to i and a combining dot,
    // so the server calls the titles one page and [[\u0130stanbul]] finds none. The list may only shrink.
    expect(disagree).toEqual(['"\u0130stanbul" next to "Istanbul": server true, client false'])
  })

  // Expected failure: canonically equivalent titles look identical and are two pages.
  it.fails('two titles that look the same are never both pages', () => {
    expect(vectors().filter(p => looksSame(p) && !p.conflict).map(p => [p.a, p.b])).toEqual([])
  })
})
