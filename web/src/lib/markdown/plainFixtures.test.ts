// The Go server turns a message into push text with internal/mdplain. Both sides
// read the same fixtures, so a push, a notification and a reply preview agree.
import { describe, it, expect, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import '../customEmoji'
import { setLocale } from '../../i18n'
import { previewText } from '../replies'

interface Fixture { input: string; plain: string }

const fixtures = JSON.parse(readFileSync(join(process.cwd(), '..', 'internal', 'mdplain', 'testdata', 'plain.json'), 'utf8')) as Fixture[]

beforeEach(() => setLocale('en'))

describe('plain text of a message (shared with the server)', () => {
  it('has fixtures', () => {
    expect(fixtures.length).toBeGreaterThan(15)
  })

  it.each(fixtures.map(f => [f.input, f.plain] as const))('%j', (input, plain) => {
    expect(previewText(input)).toBe(plain)
  })
})
