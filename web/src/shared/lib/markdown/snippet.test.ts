// The server builds snippets with internal/mdplain.Snippet; both sides read the same fixtures.
import { beforeEach, describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import '../customEmoji'
import { setLocale } from '../../../i18n'
import { previewText } from '../../../chat/lib/replies'
import { markdownSnippet } from './snippet'
import { rng } from './linkParity.fixture'

interface Fixture { input: string; limit: number; snippet: string; plain: string }

const fixtures = JSON.parse(readFileSync(join(process.cwd(), '..', 'internal', 'mdplain', 'testdata', 'snippet.json'), 'utf8')) as Fixture[]

beforeEach(() => setLocale('en'))

describe('snippet of a message (shared with the server)', () => {
  it('has fixtures', () => {
    expect(fixtures.length).toBeGreaterThan(10)
  })

  it.each(fixtures.map(f => [f.input, f.limit, f.snippet, f.plain] as const))('%j cut to %i', (input, limit, snippet, plain) => {
    expect(markdownSnippet(input, limit)).toBe(snippet)
    expect(previewText(snippet)).toBe(plain)
  })

  it('reads back as the cut plain text of the message', () => {
    const pieces = ['a', 'b1', '9', '.', ' ', '#', '>', '-', '*', '_', '~', '|', '<', '[', ']', '(', ')', '`', '\\', ':', 'h', 'https://x.example/a_b', '||s||', 'é', 'word ']
    const r = rng(3)
    for (let i = 0; i < 5000; i++) {
      let msg = ''
      while (msg.length < 30) msg += pieces[Math.floor(r() * pieces.length)] ?? ''
      const plain = previewText(msg)
      const cut = Array.from(plain).length > 20
      const snippet = markdownSnippet(msg, 20)
      if (!cut && Array.from(msg).length > 20) expect(previewText(snippet), msg).toBe(plain)
      if (cut) expect(plain.startsWith(previewText(snippet).slice(0, -1).trimEnd()), msg).toBe(true)
    }
  })
})
