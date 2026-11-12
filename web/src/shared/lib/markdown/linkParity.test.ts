// One rule, two implementations: which hosts a message links to. The vectors
// in internal/automod/testdata/link_parity.json carry the links automod reads
// (written by internal/automod/linkparity_test.go); this test renders every
// message with the real client parser and checks that no rule mode lets
// through a link the client opens. Known disagreements are the classes in
// linkParity.known.fixture.ts (findings H-L3-01..04 of the 2026-11 hunt).
// After a change to the cases below or to the generator:
//   LINK_PARITY_WRITE=1 npx vitest run src/shared/lib/markdown/linkParity.test.ts
//   LINK_PARITY_UPDATE=1 go test -run TestLinkParityVectors ./internal/automod
// The deep run is linkParity.sweep.test.ts.
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { VECTOR_FILE, bypassModes, messages } from './linkParity.fixture'
import type { Mode, Vector } from './linkParity.fixture'
import { KNOWN_CLASSES, knownClass } from './linkParity.known.fixture'

const file = join(import.meta.dirname, '..', '..', '..', '..', '..', VECTOR_FILE)

/** Messages both sides agree on: every rule mode stops what the client opens. */
const AGREED = [
  'https://other.example/x', 'see https://allowed.example@other.example/x', 'https://other.example.', '(https://other.example)',
  '[docs](https://other.example)', '<https://other.example>', '**https://other.example/x**', 'HTTPS://OTHER.EXAMPLE',
  'https://other.example​', 'https://allowed.example\\@other.example', 'https://allowed.example`@other.example',
  'https://bücher.example', 'mailto:x@other.example', '`https://other.example`', 'https://other.example\'s page'
]

/** One minimal message per known class, with the modes it gets past today. */
const KNOWN: readonly (readonly [string, string, readonly Mode[]])[] = [
  ['H-L3-01', "https://allowed.example'@other.example/x", ['allow_list', 'block_list']],
  ['H-L3-01', 'https://allowed.example＂@other.example/x', ['allow_list', 'block_list']],
  ['H-L3-01', 'https://allowed.example／@other.example/x', ['allow_list', 'block_list']],
  ['H-L3-01', "[docs](https://allowed.example'@other.example/x)", ['allow_list', 'block_list']],
  ['H-L3-01', "https://'@other.example/x", ['block_all', 'allow_list', 'block_list']],
  ['H-L3-01', 'https://\u0085@other.example/x', ['block_all', 'allow_list', 'block_list']],
  ['H-L3-02', '**https://other.example**', ['block_list']],
  ['H-L3-02', '||https://other.example||', ['block_list']],
  ['H-L3-02', '[docs](https://other.example)s', ['block_list']],
  ['H-L3-02', 'https://other.example\u007f@allowed.example', ['allow_list', 'block_list']],
  ['H-L3-02', 'https://other.example﻿.allowed.example', ['allow_list', 'block_list']],
  ['H-L3-02', 'h\u0000ttps://other.example', ['block_all', 'allow_list', 'block_list']],
  ['H-L3-02', 'http://_.other.example', ['block_list']],
  ['H-L3-03', 'https://allowed.example:99999/https://other.example', ['allow_list', 'block_list']],
  ['H-L3-03', 'https://[::1/https://other.example', ['block_list']],
  ['H-L3-04', 'https://allowed́.example', ['allow_list']],
  ['H-L3-04', 'https://allowed.exampl̂e', ['allow_list']]
]

const SAMPLE = 500
const expectedMessages = () => [...AGREED, ...KNOWN.map(k => k[1]), ...messages(1, SAMPLE)]

function vectors(): Vector[] {
  return JSON.parse(readFileSync(file, 'utf8')) as Vector[]
}

describe('links: automod against the client renderer', () => {
  it.runIf(process.env.LINK_PARITY_WRITE === '1')('writes the messages of the vector file', () => {
    writeFileSync(file, JSON.stringify(expectedMessages().map(msg => ({ msg, server: [] })), null, 1) + '\n')
  })

  it('the vector file holds exactly these messages, judged by the server', () => {
    const vs = vectors()
    expect(vs.map(v => v.msg), 'rerun with LINK_PARITY_WRITE=1, then the Go side with LINK_PARITY_UPDATE=1').toEqual(expectedMessages())
    expect(vs.every(v => Array.isArray(v.server))).toBe(true)
  })

  it.each(AGREED)('both sides agree on %j', msg => {
    const v = vectors().find(x => x.msg === msg)
    expect(v && bypassModes(v)).toEqual([])
  })

  // An expected failure: the case fails the day the server judges the host the
  // client opens. Then move it to AGREED and shrink the class.
  it.each(KNOWN)('%s: %j still gets past %j', (finding, msg, modes) => {
    const v = vectors().find(x => x.msg === msg)
    expect(v).toBeDefined()
    if (!v) return
    const bypass = bypassModes(v)
    expect(bypass).toEqual(modes)
    expect(KNOWN_CLASSES.find(c => c.name === knownClass(v, bypass))?.finding).toBe(finding)
  })

  it('every generated message the rules let through is in a known class', () => {
    const unexplained = vectors().filter(v => {
      const bypass = bypassModes(v)
      return bypass.length > 0 && !knownClass(v, bypass)
    }).map(v => v.msg)
    expect(unexplained).toEqual([])
  })
})
