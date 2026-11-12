// The title key is one rule in two languages: this file and the Go test
// TestTitleKeysMatchTheVectors (internal/wiki/title_key_test.go) both check
// their implementation against internal/wiki/testdata/title_keys.json.
// After changing the titles below:
//   WIKI_KEYS_WRITE=1 npx vitest run src/wiki/lib/wikiTitleKey.test.ts
// and run the Go test, which must agree without being regenerated.
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { wikiTitleKey } from './wikiTitleKey'

const file = join(import.meta.dirname, '..', '..', '..', '..', 'internal/wiki/testdata/title_keys.json')
interface Vector { title: string; key: string }

/** Titles where the languages could part: special casing, compatibility forms, mark order, other scripts. */
const WORDS = [
  'Caf\u00e9', 'Cafe\u0301', 'CAFE\u0301', '\u0130stanbul', 'Istanbul', '\u0131stanbul', 'I\u0307stanbul',
  '\u03a3\u039f\u03a6\u0399\u0391\u03a3', '\u03a3\u03bf\u03c6\u03b9\u03b1\u03c2', 'Xa\u00dfo', 'XA\u1e9eO', 'XASSO',
  '\u212aelvin', '\u212bngstr\u00f6m', 'A\u030angstro\u0308m', 'o\ufb03ce', '\u2460 \u216b x\u00b2',
  '\u01c4 \u01c5 \u01c6', 'E\u0302\u0323', 'E\u0323\u0302', '\u1ec6', '\u1100\u1161\u11a8 \uac01',
  '\u13a0\u13a1', '\u1c90\u1c91', '\ud801\udc00\ud801\udc28', '\ud801\udcb0', '\ud83a\udd00', '\ud81b\ude40', 'Ru\u200dles'
]

/** Every character below U+A000 whose key is not itself, in runs of 64: the bulk of the case mappings. */
function sweep(): string[] {
  const chars: string[] = []
  for (let cp = 0x20; cp < 0xa000; cp++) {
    const ch = String.fromCodePoint(cp)
    if (wikiTitleKey(ch) !== ch) chars.push(ch)
  }
  const runs: string[] = []
  for (let i = 0; i < chars.length; i += 64) runs.push(chars.slice(i, i + 64).join(''))
  return runs
}

const titles = (): string[] => [...WORDS, ...sweep()]
const vectors = (): Vector[] => JSON.parse(readFileSync(file, 'utf8')) as Vector[]

describe('wikiTitleKey', () => {
  it.runIf(process.env.WIKI_KEYS_WRITE === '1')('writes the vectors', () => {
    const json = JSON.stringify(titles().map(title => ({ title, key: wikiTitleKey(title) })), null, 1)
    // ASCII only, so the file reads the same in every editor.
    writeFileSync(file, json.replace(/[^\x20-\x7e\n]/g, c => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0')) + '\n')
  })

  it('matches the shared vectors the server is checked against', () => {
    const v = vectors()
    expect(v.map(x => x.title)).toEqual(titles())
    expect(v.filter(x => wikiTitleKey(x.title) !== x.key)).toEqual([])
  })

  it('makes canonically equivalent titles and titles that differ in case one key', () => {
    expect(wikiTitleKey('Cafe\u0301')).toBe(wikiTitleKey('CAF\u00c9'))
    expect(wikiTitleKey('E\u0302\u0323')).toBe(wikiTitleKey('\u1ec6'))
    expect(wikiTitleKey('\u212aelvin')).toBe('kelvin')
  })

  it('keeps apart what only full case folding or compatibility mapping would join', () => {
    expect(wikiTitleKey('\u0130stanbul')).not.toBe(wikiTitleKey('Istanbul'))
    expect(wikiTitleKey('Xa\u00dfo')).not.toBe(wikiTitleKey('Xasso'))
    expect(wikiTitleKey('\u03a3\u03bf\u03c6\u03b9\u03b1\u03c2')).not.toBe(wikiTitleKey('\u03a3\u039f\u03a6\u0399\u0391\u03a3'))
    expect(wikiTitleKey('o\ufb03ce')).not.toBe('office')
  })
})
