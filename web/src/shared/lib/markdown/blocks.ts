// Block Markdown (Discord's flavour): code blocks, headings, subtext, lists
// and quotes. Everything is recognised at the start of a line; a run of
// ordinary lines becomes one paragraph (a newline-joined inline run).
import type { Block, ListItem } from './types'
import { parseInline } from './inline'

/** Lists nest at most this deep; deeper indentation joins the deepest level. */
const MAX_LIST_DEPTH = 6

const HEADING = /^(#{1,3}) +(\S.*)$/
const SUBTEXT = /^-# +(\S.*)$/
const ITEM = /^([ \t]*)(?:([-*])|(\d{1,9})\.)[ \t]+(\S.*)$/
const FENCE_HEAD = /^([A-Za-z0-9_+#.-]{1,20})\n([\s\S]*)$/

interface Segment {
  kind: 'text' | 'code'
  /** The source as typed. */
  raw: string
  /** Text: the source without the line break that touches a code block. Code: the code. */
  text: string
  lang: string
}

function trimNewlines(text: string): string {
  let start = 0
  let end = text.length
  while (start < end && text[start] === '\n') start++
  while (end > start && text[end - 1] === '\n') end--
  return text.slice(start, end)
}

/** The index of the next ``` at or after `from` that is not backslash-escaped. */
function findFence(src: string, from: number): number {
  for (let at = src.indexOf('```', from); at >= 0; at = src.indexOf('```', at + 1)) {
    let slashes = 0
    while (src[at - 1 - slashes] === '\\') slashes++
    if (slashes % 2 === 0) return at
  }
  return -1
}

/** "lang\ncode" or just "code"; null when there is nothing to show. */
function fenceBody(body: string): { lang: string; code: string } | null {
  const head = FENCE_HEAD.exec(body)
  const hasLang = head !== null && (head[2] ?? '').trim() !== ''
  const code = trimNewlines(hasLang ? (head?.[2] ?? '') : body)
  return code.trim() === '' ? null : { lang: hasLang ? (head?.[1] ?? '').toLowerCase() : '', code }
}

function splitFences(src: string): Segment[] {
  const out: Segment[] = []
  let pos = 0
  let from = 0
  const text = (raw: string) => out.push({ kind: 'text', raw, text: raw, lang: '' })
  for (;;) {
    const open = findFence(src, from)
    const close = open < 0 ? -1 : src.indexOf('```', open + 3)
    if (close < 0) break
    const fence = fenceBody(src.slice(open + 3, close))
    from = close + 3
    if (!fence) continue
    if (open > pos) text(src.slice(pos, open))
    out.push({ kind: 'code', raw: src.slice(open, from), text: fence.code, lang: fence.lang })
    pos = from
  }
  if (pos < src.length) text(src.slice(pos))
  return dropBoundaryBreaks(out)
}

/** The line break right before or after a code block belongs to the block. */
function dropBoundaryBreaks(segments: Segment[]): Segment[] {
  segments.forEach((seg, i) => {
    if (seg.kind !== 'text') return
    if (segments[i - 1]?.kind === 'code' && seg.text.startsWith('\n')) seg.text = seg.text.slice(1)
    if (segments[i + 1]?.kind === 'code' && seg.text.endsWith('\n')) seg.text = seg.text.slice(0, -1)
  })
  return segments
}

interface RawItem {
  indent: number
  ordered: boolean
  num: number
  text: string
}

function rawItem(line: string): RawItem | null {
  const m = ITEM.exec(line)
  if (!m) return null
  return { indent: (m[1] ?? '').replace(/\t/g, '    ').length, ordered: m[3] !== undefined, num: Number(m[3] ?? 1), text: (m[4] ?? '').trimEnd() }
}

type ListBlock = Extract<Block, { t: 'list' }>

function newList(item: RawItem): ListBlock {
  return { t: 'list', ordered: item.ordered, start: item.num, items: [] }
}

/** Builds nested lists from consecutive item lines, nesting by indentation. */
function buildLists(items: RawItem[]): Block[] {
  const roots: ListBlock[] = []
  const stack: { indent: number; list: ListBlock }[] = []
  for (const item of items) {
    while (stack.length > 1 && item.indent < (stack.at(-1)?.indent ?? 0)) stack.pop()
    let top = stack.at(-1)
    if (top && item.indent > top.indent && stack.length < MAX_LIST_DEPTH) {
      const nested = newList(item)
      top.list.items.at(-1)?.sub.push(nested)
      top = { indent: item.indent, list: nested }
      stack.push(top)
    } else if (!top || (stack.length === 1 && top.list.ordered !== item.ordered)) {
      const root = newList(item)
      roots.push(root)
      stack.length = 0
      top = { indent: item.indent, list: root }
      stack.push(top)
    }
    const entry: ListItem = { c: parseInline(item.text), sub: [] }
    top.list.items.push(entry)
  }
  return roots
}

/** Consecutive lines from i that satisfy `take`, and where they end. */
function takeWhile<T>(lines: string[], i: number, take: (line: string) => T | null): { taken: T[]; end: number } {
  const taken: T[] = []
  let end = i
  for (let v = take(lines[end] ?? ''); v !== null && end < lines.length; v = take(lines[end] ?? '')) {
    taken.push(v)
    end++
  }
  return { taken, end }
}

interface LineParse {
  blocks: Block[]
  /** A ">>> " quote swallowed the rest of the message. */
  rest: boolean
}

function quoteLine(line: string): string | null {
  return line.startsWith('> ') ? line.slice(2) : null
}

function heading(line: string): Block | null {
  const m = HEADING.exec(line)
  if (m) return { t: 'h', level: (m[1] ?? '#').length as 1 | 2 | 3, c: parseInline((m[2] ?? '').trimEnd()) }
  const sub = SUBTEXT.exec(line)
  return sub ? { t: 'sub', c: parseInline((sub[1] ?? '').trimEnd()) } : null
}

function parseLines(lines: string[], tail: string, quoted: boolean): LineParse {
  const blocks: Block[] = []
  let run: string[] = []
  const flush = () => {
    const text = run.join('\n')
    run = []
    if (text) blocks.push({ t: 'p', c: parseInline(text) })
  }
  let i = 0
  while (i < lines.length) {
    const line = lines[i] ?? ''
    const single = quoted ? null : takeWhile(lines, i, quoteLine)
    const items = takeWhile(lines, i, rawItem)
    const block = heading(line)
    if (!quoted && line.startsWith('>>> ')) {
      flush()
      blocks.push({ t: 'quote', c: parseBlocks([line.slice(4), ...lines.slice(i + 1)].join('\n') + tail, true) })
      return { blocks, rest: true }
    }
    if (single && single.taken.length > 0) {
      flush()
      blocks.push({ t: 'quote', c: parseBlocks(single.taken.join('\n'), true) })
      i = single.end
    } else if (block) {
      flush()
      blocks.push(block)
      i++
    } else if (items.taken.length > 0) {
      flush()
      blocks.push(...buildLists(items.taken))
      i = items.end
    } else {
      run.push(line)
      i++
    }
  }
  flush()
  return { blocks, rest: false }
}

/** Parses a message (or a quote's body) into blocks. */
export function parseBlocks(src: string, quoted = false): Block[] {
  const segments = splitFences(src)
  const out: Block[] = []
  for (const [i, seg] of segments.entries()) {
    if (seg.kind === 'code') {
      out.push({ t: 'code', lang: seg.lang, v: seg.text })
      continue
    }
    const tail = segments.slice(i + 1).map(s => s.raw).join('')
    const { blocks, rest } = parseLines(seg.text.split('\n'), tail, quoted)
    out.push(...blocks)
    if (rest) break
  }
  return out
}
