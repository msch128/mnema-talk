// Inline Markdown (Discord's flavour): emphasis, spoilers, code spans, links
// and escapes. A hand-written scanner instead of nested regexes, so work stays
// linear per delimiter and hostile input cannot trigger regex backtracking.
// Nothing here produces markup: the result is a tree of plain nodes.
import type { FormatKind, Inline } from './types'
import { needsConfirm, safeHref } from './link'

/** Formatting nests at most this deep; deeper delimiters stay literal text. */
export const MAX_INLINE_DEPTH = 6

export interface InlineContext {
  depth: number
  /** Inside a link label: no further links. */
  noLinks: boolean
}

const ROOT: InlineContext = { depth: 0, noLinks: false }

interface Hit {
  node: Inline
  end: number
}

const isSpace = (ch: string | undefined): boolean => ch === undefined || /\s/.test(ch)
const isWord = (ch: string | undefined): boolean => ch !== undefined && /[A-Za-z0-9_]/.test(ch)
const isEscapable = (ch: string | undefined): boolean => ch !== undefined && /[^0-9A-Za-z\s]/.test(ch)

/** The first index >= from where test passes, skipping escaped characters (and `pair`, if given). */
function scan(src: string, from: number, test: (j: number) => boolean, pair = ''): number {
  for (let j = from; j < src.length; j++) {
    if (src[j] === '\\') {
      j++
    } else if (pair && src.startsWith(pair, j)) {
      j += pair.length - 1
    } else if (test(j)) {
      return j
    }
  }
  return -1
}

function format(kind: FormatKind, src: string, from: number, to: number, end: number, ctx: InlineContext): Hit | null {
  if (ctx.depth >= MAX_INLINE_DEPTH) return null
  const c = parseInline(src.slice(from, to), { ...ctx, depth: ctx.depth + 1 })
  return { node: { t: 'fmt', k: kind, c }, end }
}

function strong(src: string, i: number, ctx: InlineContext): Hit | null {
  const j = scan(src, i + 3, k => src.startsWith('**', k) && src[k + 2] !== '*')
  return j < 0 ? null : format('strong', src, i + 2, j, j + 2, ctx)
}

function underline(src: string, i: number, ctx: InlineContext): Hit | null {
  const j = scan(src, i + 3, k => src.startsWith('__', k) && src[k + 2] !== '_')
  return j < 0 ? null : format('u', src, i + 2, j, j + 2, ctx)
}

function emStar(src: string, i: number, ctx: InlineContext): Hit | null {
  if (isSpace(src[i + 1]) || src[i + 1] === '*') return null
  const j = scan(src, i + 2, k => src[k] === '*' && src[k + 1] !== '*' && !isSpace(src[k - 1]), '**')
  return j < 0 ? null : format('em', src, i + 1, j, j + 1, ctx)
}

function emUnderscore(src: string, i: number, ctx: InlineContext): Hit | null {
  if (isWord(src[i - 1]) || src[i + 1] === '_') return null
  const j = scan(src, i + 2, k => src[k] === '_' && !isWord(src[k + 1]), '__')
  return j < 0 ? null : format('em', src, i + 1, j, j + 1, ctx)
}

function strike(src: string, i: number, ctx: InlineContext): Hit | null {
  if (isSpace(src[i + 2])) return null
  const j = scan(src, i + 3, k => src.startsWith('~~', k))
  return j < 0 || isSpace(src[j - 1]) ? null : format('del', src, i + 2, j, j + 2, ctx)
}

function spoiler(src: string, i: number, ctx: InlineContext): Hit | null {
  const j = scan(src, i + 3, k => src.startsWith('||', k))
  return j < 0 ? null : format('spoiler', src, i + 2, j, j + 2, ctx)
}

function matchFormat(src: string, i: number, ctx: InlineContext): Hit | null {
  switch (src[i]) {
    case '*':
      return (src[i + 1] === '*' ? strong(src, i, ctx) : null) ?? emStar(src, i, ctx)
    case '_':
      return (src[i + 1] === '_' ? underline(src, i, ctx) : null) ?? emUnderscore(src, i, ctx)
    case '~':
      return src[i + 1] === '~' ? strike(src, i, ctx) : null
    default:
      return src[i + 1] === '|' ? spoiler(src, i, ctx) : null
  }
}

/** `code` and ``code with ` inside``: a run of backticks closes with a run of the same length. */
function matchCode(src: string, i: number): Hit {
  let n = 1
  while (src[i + n] === '`') n++
  const fence = '`'.repeat(n)
  let from = i + n
  while (from < src.length) {
    const close = src.indexOf(fence, from)
    if (close < 0) break
    let runEnd = close + n
    while (src[runEnd] === '`') runEnd++
    if (runEnd - close === n && src.slice(i + n, close).trim() !== '') {
      return { node: { t: 'code', v: codeBody(src.slice(i + n, close)) }, end: close + n }
    }
    from = runEnd
  }
  return { node: { t: 'text', v: fence }, end: i + n }
}

/** One space of padding on both sides is dropped, so `` ` `` can show a backtick. */
function codeBody(body: string): string {
  const padded = body.length > 2 && body.startsWith(' ') && body.endsWith(' ') && body.trim() !== ''
  return padded ? body.slice(1, -1) : body
}

function link(kind: 'bare' | 'auto' | 'masked', href: string, c: Inline[], confirm = false): Inline {
  return { t: 'link', kind, href, confirm, c }
}

const AUTOLINK = /<((?:https?:\/\/|mailto:)[^\s<>]+)>/iy

function matchAutolink(src: string, i: number, ctx: InlineContext): Hit | null {
  if (ctx.noLinks) return null
  AUTOLINK.lastIndex = i
  const m = AUTOLINK.exec(src)
  const href = m ? safeHref(m[1] ?? '') : null
  return m && href ? { node: link('auto', href, [{ t: 'text', v: href }]), end: i + m[0].length } : null
}

const BARE_URL = /https?:\/\/[^\s<]+/iy
const TRAILING = new Set(['.', ',', ':', ';', '!', '?', "'", '"', '*', '_', '~', '|'])

function count(text: string, ch: string): number {
  return text.split(ch).length - 1
}

/** Cuts what belongs to the sentence or the surrounding markup, not to the URL. */
function trimUrl(raw: string): string {
  let url = raw
  const spoilerEnd = url.indexOf('||')
  if (spoilerEnd > 0) url = url.slice(0, spoilerEnd)
  for (;;) {
    const last = url.at(-1) ?? ''
    const closing = (last === ')' && count(url, ')') > count(url, '(')) || (last === ']' && count(url, ']') > count(url, '['))
    if (!TRAILING.has(last) && !closing) return url
    url = url.slice(0, -1)
  }
}

function matchBare(src: string, i: number, ctx: InlineContext): Hit | null {
  if (ctx.noLinks || isWord(src[i - 1])) return null
  BARE_URL.lastIndex = i
  const m = BARE_URL.exec(src)
  if (!m) return null
  const url = trimUrl(m[0])
  const href = safeHref(url)
  return href ? { node: link('bare', href, [{ t: 'text', v: url }]), end: i + url.length } : null
}

/** The index of the "]" closing the "[" at i, or -1. */
function labelEnd(src: string, i: number): number {
  let depth = 0
  for (let j = i; j < src.length; j++) {
    const ch = src[j]
    if (ch === '\\') j++
    else if (ch === '[') depth++
    else if (ch === ']' && --depth === 0) return j
  }
  return -1
}

/** Reads "(url)" or "(<url>)" or "(url "title")" at i; the title is dropped. */
function destination(src: string, i: number): { url: string; end: number } | null {
  if (src[i] !== '(') return null
  const angled = src[i + 1] === '<'
  let j = i + (angled ? 2 : 1)
  const start = j
  let depth = 0
  while (j < src.length) {
    const ch = src[j]
    if (angled ? ch === '>' : (ch === ')' && depth === 0) || /\s/.test(ch ?? '')) break
    if (ch === '(') depth++
    else if (ch === ')') depth--
    j++
  }
  const url = src.slice(start, j)
  if (angled) j++
  const tail = /^\s*(?:"[^"\n]*"|'[^'\n]*')?\s*\)/.exec(src.slice(j, j + 400))
  return tail ? { url, end: j + tail[0].length } : null
}

function matchMasked(src: string, i: number, ctx: InlineContext): Hit | null {
  if (ctx.noLinks || ctx.depth >= MAX_INLINE_DEPTH) return null
  const close = labelEnd(src, i)
  if (close < 0 || close === i + 1) return null
  const dest = destination(src, close + 1)
  const href = dest ? safeHref(dest.url) : null
  if (!dest || !href) return null
  const label = src.slice(i + 1, close)
  const c = parseInline(label, { depth: ctx.depth + 1, noLinks: true })
  return { node: link('masked', href, c, needsConfirm(plainLabel(c), href)), end: dest.end }
}

function plainLabel(nodes: Inline[]): string {
  return nodes.map(n => (n.t === 'text' || n.t === 'code' ? n.v : n.t === 'link' || n.t === 'fmt' || n.t === 'el' ? plainLabel(n.c) : '')).join('')
}

function matchAt(src: string, i: number, ctx: InlineContext): Hit | null {
  switch (src[i]) {
    case '`':
      return matchCode(src, i)
    case '<':
      return matchAutolink(src, i, ctx)
    case '[':
      return matchMasked(src, i, ctx)
    case 'h':
    case 'H':
      return matchBare(src, i, ctx)
    case '*':
    case '_':
    case '~':
    case '|':
      return matchFormat(src, i, ctx)
    default:
      return null
  }
}

function pushText(out: Inline[], text: string): void {
  if (!text) return
  const last = out.at(-1)
  if (last?.t === 'text') last.v += text
  else out.push({ t: 'text', v: text })
}

/** Parses a run of inline Markdown. */
export function parseInline(src: string, ctx: InlineContext = ROOT): Inline[] {
  const out: Inline[] = []
  let buf = ''
  let i = 0
  while (i < src.length) {
    if (src[i] === '\\' && isEscapable(src[i + 1])) {
      buf += src[i + 1]
      i += 2
      continue
    }
    const hit = matchAt(src, i, ctx)
    if (!hit) {
      buf += src[i]
      i++
      continue
    }
    if (hit.node.t === 'text') {
      buf += hit.node.v
    } else {
      pushText(out, buf)
      buf = ''
      out.push(hit.node)
    }
    i = hit.end
  }
  pushText(out, buf)
  return out
}
