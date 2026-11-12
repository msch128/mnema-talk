// The same vectors as linkParity.test.ts, rendered by THIS bundle (which
// refuses user info and hidden characters in a host, linkAuthority.ts) and
// not as older bundles did. This bundle is no subset of the older ones: where
// it refuses an outer link it can link an inner one, so both are checked on
// every push.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { VECTOR_FILE, bypassModes, unjudged } from './linkParity.fixture'
import type { Vector } from './linkParity.fixture'

const file = join(import.meta.dirname, '..', '..', '..', '..', '..', VECTOR_FILE)

describe('links: automod against this bundle\'s renderer', () => {
  it('no message of the vector file reaches readers with a link a rule forbids', () => {
    const vs = JSON.parse(readFileSync(file, 'utf8')) as Vector[]
    expect(vs.length).toBeGreaterThan(500)
    const through = vs.filter(v => bypassModes(v).length > 0).map(v => `${JSON.stringify(v.msg)} opens ${unjudged(v).join(', ')}`)
    expect(through).toEqual([])
  })
})
