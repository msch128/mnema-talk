// One rule, two implementations: what of a message is hidden. The web
// renderer hides ||spoilers||; the server's plain form (internal/mdplain:
// pushes, and the snippets every client shows) is a port of this parser. This
// generator writes messages full of canaries; the renderer decides which are
// hidden, and the Go side checks that its plain form and snippet show none of
// those (internal/mdplain/cross_test.go reads CROSS_FILE).
import { renderMarkdown } from '../markdown'
import { previewText } from '../../../chat/lib/replies'
import { markdownSnippet } from './snippet'

export const CROSS_FILE = 'internal/mdplain/testdata/cross.jsonl'

export interface CrossRecord {
  input: string
  /** Canaries the renderer hides. */
  hidden: string[]
  /** The web client's plain form and snippet of the message. */
  plain: string
  snippet: string
}

// A canary ends in q, so a snippet's cut never leaves a shorter canary behind.
const CANARY = /zq\d+k\d+q/g

/** The canaries the renderer shows: its HTML with every spoiler span cut out. */
export function shownCanaries(msg: string): Set<string> {
  let html = renderMarkdown(msg)
  for (;;) {
    const start = html.indexOf('<span class="md-spoiler"')
    if (start < 0) break
    let depth = 0
    let end = html.length
    const tag = /<(\/?)span\b[^>]*>/g
    tag.lastIndex = start
    for (let m = tag.exec(html); m; m = tag.exec(html)) {
      depth += m[1] ? -1 : 1
      if (depth === 0) { end = m.index + m[0].length; break }
    }
    html = html.slice(0, start) + html.slice(end)
  }
  return new Set(html.match(CANARY) ?? [])
}

/** The canaries of msg the renderer hides. */
export function hiddenCanaries(msg: string): string[] {
  const shown = shownCanaries(msg)
  return [...new Set(msg.match(CANARY) ?? [])].filter(c => !shown.has(c))
}

/** The canaries of msg that plain shows although the renderer hides them. */
export function leaked(hidden: readonly string[], plain: string): string[] {
  const seen = new Set(plain.match(CANARY) ?? [])
  return hidden.filter(c => seen.has(c))
}

// "C" becomes a canary. The pieces aim at what decides hiding: spoiler
// delimiters, code spans and fences, escapes, emphasis, paragraph and line
// breaks, quotes, lists, masked links, autolinks and bare URLs (also hosts only
// the browser's URL parser can judge, user info, percent escapes and control
// or format characters in the authority, which linkAuthority.ts refuses).
const PIECES = [
  'word ', 'C ', 'a longer bit of text ', 'x'.repeat(40) + ' ', 'C',
  '||C||', '||C more||', '|| C ||', '||', '|', '\\||', '||C', 'C||',
  '`', '``', '`C`', '``C`` ', '` C `', '`||`', '`||C`', '```', '```js\n', '\n```\n',
  '*', '**', '_', '__', '~~', '*C*', '**C**', '_C_', '__C__', '~~C~~', ' * ', '~~ ',
  '\\', '\\*', '\\`', '\\_', '\\[', '\\\\',
  '[', ']', '(', ')', '](', '[C](https://a.example)', '[||C||](https://b.example/x)', '[C](<https://c.example> "t")', '[C](x)',
  '<https://d.example/C>', '<', '>', '<http://e.example',
  'https://f.example/C ', 'http://g.example/a_b||C|| ', 'https://caf\u00e9.example/C ', 'https://x_y.example/C ',
  'http://1.2.3.4/C ', 'https://h.example/(C) ', '-https://i.example ', 'https://j.example:99999/C ', 'HTTPS://K.EXAMPLE/C ',
  '\n', '\n\n', '> ', '>>> ', '- ', '* ', '1. ', '# ', '-# ', '\t', '  ',
  '+ ', '2) ', '***', '___', '||||', ' ||', '|| ', '`` ` ``', '~', '\\|', '&amp;', '&#124;', '<@00000000-0000-4000-8000-000000000002>', '<t:1700000000:R>',
  '\u{1f468}\u200d\u{1f469}\u200d\u{1f467}', '\u{1f1e9}\u{1f1ea}', '[C]', '[[C]](https://a.example)', '[C](https://a.example/(x)', '(C)', 'mailto:C ', '<mailto:C>', 'http://', 'https://l.example/C|| ',
  'https://u@m.example/C ', 'https://allowed.example@n.example/C ', '[C](https://u:p@o.example)', '<https://u@p.example/C>', 'https://q%41.example/C ',
  'https://r%zz.example/C ', 'https://s%E2%80%8B.example/C ', 'https://t\u200b.example/C ', '[C](https://v%C3%A9.example)', 'https://w\u00ad.example/C ',
  'http://x%40y.example/C ', 'https://z.example\u2060/C ', '<https://a%2eb.example/C>', '[C](https://c%00d.example)', 'https://e%C3.example/C ',
  '\u00a0', '\u2028', '\r', '\ufeff', '\u0085', '\u{1f600}', '\u00e9', '\u0301', '<:wave:00000000-0000-4000-8000-000000000001>', '@everyone ', '\u0000'
]

/** A generated message: canaries zq<n>k<i>q in every kind of place. */
export function crossMessage(r: () => number, n: number): string {
  let s = ''
  let k = 0
  const length = 40 + r() * 560
  while (s.length < length) {
    const piece = PIECES[Math.floor(r() * PIECES.length)] ?? ''
    s += piece.replace(/C/g, () => `zq${n}k${k++}q`)
  }
  return s
}

export function crossRecord(input: string): CrossRecord {
  return { input, hidden: hiddenCanaries(input), plain: previewText(input), snippet: markdownSnippet(input) }
}
