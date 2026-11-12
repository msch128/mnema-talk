// Known disagreements between automod's link reading and the client renderer,
// each a finding of the 2026-11 defect hunt. A message that slips a link past
// a rule is explained by the first class that matches; the list may only
// shrink: a class that no longer occurs fails the sweep.
import { renderedHrefs } from './linkParity.fixture'
import type { Mode, Vector } from './linkParity.fixture'

export interface KnownClass { name: string; finding: string; matches: (v: Vector, bypass: readonly Mode[]) => boolean }

/** The server's view of the text: NFKC, every space a plain one, invisible characters and stray marks dropped (compile.go fold). */
function fold(s: string): string {
  // Go's unicode.IsSpace, which unlike JavaScript's \s does not include U+FEFF.
  return s.normalize('NFKC').replace(/[\t\n\v\f\r \u0085\p{Z}]/gu, ' ').replace(/\p{Cf}|\p{Mn}/gu, '')
}

// The server's link token, on the folded text (compile.go linkToken).
const SERVER_TOKEN = /(?:https?:\/\/|\bwww\.)[^\s<>"'`]+/gi

// Where the client's link ends (inline.ts BARE_URL, safeHref) and where its
// authority ends (the URL parser: an ASCII / ? # or \).
// eslint-disable-next-line no-control-regex
const CLIENT_STOP = /[\s<>"`\\\u0000-\u001f\u007f/?#]/

/** The authority of every http(s) link as the client reads it, up to its first stop. */
function clientAuthorities(msg: string): string[] {
  return [...msg.matchAll(/https?:\/\//gi)].map(m => {
    const rest = msg.slice((m.index ?? 0) + m[0].length)
    const stop = rest.search(CLIENT_STOP)
    return stop < 0 ? rest : rest.slice(0, stop)
  })
}

/** A character the server splits a link at and the client keeps: the quote, and what NFKC turns into a stop, a delimiter or a space. */
function serverSplits(c: string): boolean {
  const f = fold(c)
  return c === "'" || (f !== c && f !== '' && (/^['"`<>/?#\\]$/.test(f) || (f.trim() === '' && !/\s/.test(c))))
}

/** Whether the server's token goes on where the client's link ends, somewhere in the text. */
function serverReadsOn(text: string, href: string): boolean {
  for (let i = text.indexOf(href); i >= 0; i = text.indexOf(href, i + 1)) {
    const next = text[i + href.length]
    if (next !== undefined && !/[\s<>"'`]/.test(next)) return true
  }
  return false
}

export const KNOWN_CLASSES: readonly KnownClass[] = [
  {
    // The server's token or host ends at a character the client keeps in the
    // authority: ' itself, and what NFKC folds into ' " ` < > / ? # \ or a
    // space (U+FF07, U+FF0F, U+0085, ...). The browser reads it as user info
    // and opens the host after the "@"; the server judges the part before it,
    // or sees no link at all.
    name: 'server splits the authority before the client does',
    finding: 'H-L3-01',
    matches: v => clientAuthorities(v.msg).some(a => [...a].some(serverSplits))
  },
  {
    // The other way round: the client ends the link at a control character, at
    // U+FEFF (white space for JavaScript, deleted by the server's fold), at
    // "||" or at the ")" of a masked link, and drops NUL before parsing; the
    // server reads on to a later "@" or host.
    name: 'client ends the link before the server does',
    finding: 'H-L3-02',
    matches: v => v.msg.includes('\u0000') || renderedHrefs(v.msg).some(h => serverReadsOn(fold(v.msg), fold(h)))
  },
  {
    // The client's URL parser rejects the outer token (a port above 65535, an
    // open "[") and links a URL nested inside it; the server judged the outer one.
    name: 'outer token rejected by the client, nested link opened',
    finding: 'H-L3-03',
    matches: v => [...fold(v.msg).matchAll(SERVER_TOKEN)].some(m => /https?:\/\//i.test(m[0].slice(1)))
  },
  {
    // The server's fold deletes combining marks and format characters (U+0301,
    // U+200D, ...) before it reads the host; the browser keeps them, and its
    // IDNA mapping makes them part of another, registrable domain.
    name: 'server deletes a mark the client keeps in the host',
    finding: 'H-L3-04',
    matches: v => clientAuthorities(v.msg).some(a => /[\p{Mn}\p{Cf}]/u.test(a))
  },
  {
    // A host the server's IDNA check refuses and a browser resolves: a label
    // with "_" (http://_.other.example, served by a wildcard record). A block
    // list skips what the server cannot read.
    name: 'unreadable for the server, skipped by the block list',
    finding: 'H-L3-02',
    matches: (v, bypass) => bypass.every(m => m === 'block_list') && v.server.some(l => !l.ok)
  }
]

export function knownClass(v: Vector, bypass: readonly Mode[]): string {
  return KNOWN_CLASSES.find(c => c.matches(v, bypass))?.name ?? ''
}
