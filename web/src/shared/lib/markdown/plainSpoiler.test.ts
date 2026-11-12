// One rule, two implementations: what of a message is hidden. The renderer
// hides ||spoilers||; every plain form (reply previews, search thread roots,
// reminders, the conversation list, notifications) must hide the same text.
// The snippets of the first four used to be the raw Markdown cut to 200
// characters and stripped afterwards, so a spoiler closed after the cut was
// open in the snippet and its text showed (finding H-L3-06 of the 2026-11
// hunt). They are now markdownSnippet here and mdplain.Snippet on the server,
// one rule checked against internal/mdplain/testdata/snippet.json on both
// sides. Deep run: PLAIN_SWEEP=1 [PLAIN_SWEEP_N=<n>].
import { beforeEach, describe, expect, it } from 'vitest'
import { renderMarkdown } from '../markdown'
import { setLocale } from '../../../i18n'
import { previewText } from '../../../chat/lib/replies'
import { markdownSnippet } from './snippet'
import { rng } from './linkParity.fixture'

beforeEach(() => setLocale('en'))

const CANARY = /zq\d+/g

/** The canaries the renderer shows; the others are hidden in spoilers. */
function shown(msg: string): Set<string> {
  // The renderer's output with every spoiler span cut out (its spans never nest
  // a span of another kind around text, see render.ts spoilerOpen).
  let html = renderMarkdown(msg)
  for (;;) {
    const start = html.indexOf('<span class="md-spoiler"')
    if (start < 0) break
    let depth = 0
    let end = start
    const tag = /<(\/?)span\b[^>]*>/g
    tag.lastIndex = start
    for (let m = tag.exec(html); m; m = tag.exec(html)) {
      depth += m[1] ? -1 : 1
      if (depth === 0) { end = m.index + m[0].length; break }
    }
    html = html.slice(0, start) + html.slice(end > start ? end : html.length)
  }
  return new Set(html.match(CANARY) ?? [])
}

function hiddenShownBy(msg: string, plain: string): string[] {
  const visible = shown(msg)
  const all = new Set(msg.match(CANARY) ?? [])
  return [...new Set(plain.match(CANARY) ?? [])].filter(c => all.has(c) && !visible.has(c))
}

const PIECES = ['word ', 'a longer bit of text ', '**bold** ', '*it* ', '`code` ', '> ', '\n', '- ', '# ', '~~gone~~ ', '[label](https://a.example) ', '\\|| ', '| ', 'x'.repeat(40) + ' ']

function message(r: () => number, n: number): string {
  let s = ''
  let k = 0
  while (s.length < 60 + r() * 400) {
    s += r() < 0.25 ? `||secret zq${n}_${k++} more||` + (r() < 0.5 ? ' ' : '') : (PIECES[Math.floor(r() * PIECES.length)] ?? '')
  }
  return s
}

/** The plain forms the client builds, keyed by where they appear. */
const CONSUMERS: Record<string, (msg: string) => string> = {
  'message preview': msg => previewText(msg),
  // The server's mdplain.Snippet builds the same Markdown (shared fixtures).
  'snippet (reply preview, search root, reminder, conversation list)': msg => previewText(markdownSnippet(msg))
}

describe('plain forms hide what the renderer hides', () => {
  const minimal = 'x'.repeat(180) + ' ||zq1 is the answer to everything||'

  it('the whole message hides the spoiler (control)', () => {
    expect(hiddenShownBy(minimal, previewText(minimal))).toEqual([])
  })

  it('a snippet hides a spoiler that closes after the 200th character', () => {
    const plain = CONSUMERS['snippet (reply preview, search root, reminder, conversation list)']?.(minimal) ?? ''
    expect(plain).toContain('Spoiler')
    expect(hiddenShownBy(minimal, plain)).toEqual([])
  })

  it('generated messages: no plain form shows hidden text', () => {
    const n = Number(process.env.PLAIN_SWEEP ? process.env.PLAIN_SWEEP_N ?? '100000' : '2000')
    const r = rng(7)
    const leaks: Record<string, number> = {}
    const examples: string[] = []
    for (let i = 0; i < n; i++) {
      const msg = message(r, i)
      for (const [name, plain] of Object.entries(CONSUMERS)) {
        if (hiddenShownBy(msg, plain(msg)).length === 0) continue
        leaks[name] = (leaks[name] ?? 0) + 1
        if (name === 'message preview' && examples.length < 5) examples.push(msg)
      }
    }
    console.log(`plain forms: ${n} messages, leaks ${JSON.stringify(leaks)}`)
    expect(examples, 'the full plain form showed hidden text').toEqual([])
    expect(leaks).toEqual({})
  }, 3_600_000)
})
