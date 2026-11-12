// One rule, two implementations: which hosts a message links to. The vectors
// in internal/automod/testdata/link_parity.json carry the links automod reads
// (written by internal/automod/linkparity_test.go); this test renders every
// message with the real client parser and checks that no rule mode lets
// through a link the client opens (findings H-L3-01..04 of the 2026-11 hunt
// were the cases that once did).
// After a change to the cases below or to the generator:
//   LINK_PARITY_WRITE=1 npx vitest run src/shared/lib/markdown/linkParity.test.ts
//   LINK_PARITY_UPDATE=1 go test -run TestLinkParityVectors ./internal/automod
// The deep run is linkParity.sweep.test.ts.
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { VECTOR_FILE, bypassModes, messages, serverPasses, unjudged } from './linkParity.fixture'
import type { Vector } from './linkParity.fixture'
import { AGREED, PASSING } from './linkParity.cases.fixture'

// Installed desktop clients keep an older web bundle until they update, and it
// links user info and invisible characters in a host. The server has to hold
// on its own, so the check renders the way that bundle did.
vi.mock('./linkAuthority', () => ({ plainAuthority: () => true }))

const file = join(import.meta.dirname, '..', '..', '..', '..', '..', VECTOR_FILE)

const SAMPLE = 500
const expectedMessages = () => [...AGREED, ...PASSING, ...messages(1, SAMPLE)]

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

  it.each(AGREED)('no rule mode lets a link of %j past', msg => {
    const v = vectors().find(x => x.msg === msg)
    expect(v && bypassModes(v)).toEqual([])
  })

  it.each(PASSING)('an allowed link stays allowed: %j', msg => {
    const v = vectors().find(x => x.msg === msg)
    expect(v && serverPasses(v)).toBe(true)
  })

  it('no generated message reaches readers with a link a rule forbids', () => {
    const through = vectors().filter(v => bypassModes(v).length > 0).map(v => `${JSON.stringify(v.msg)} opens ${unjudged(v).join(', ')}`)
    expect(through).toEqual([])
  })
})
