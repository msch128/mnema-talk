// Text rules: the extension point for tokens that live inside ordinary text
// (@mentions, role pills, custom emoji). A rule turns regex matches in text
// nodes into inline nodes. Rules never run inside code, and by default not
// inside link labels; they run inside every kind of formatting.
//
// A feature adds its token once at module load with registerTextRule(); it
// then applies to every message render and to every plain-text preview
// (reply quotes, notifications). Per-render rules (mentions need the member
// list) are passed to renderMarkdown instead.
import type { Block, Inline } from './types'

export interface TextRule {
  /** Global regex (the g flag is added when missing). Keep it linear: no nested quantifiers. */
  re: RegExp
  /** The nodes for one match, or null to leave the text as it is. */
  build(match: RegExpMatchArray): Inline[] | null
  /** What a plain-text preview shows for a match; default: the matched text. */
  plain?(match: RegExpMatchArray): string
  /** Also apply inside link labels (default: no, to avoid interactive nodes inside a link). */
  inLinks?: boolean
}

const registered: TextRule[] = []

function global(rule: TextRule): TextRule {
  return rule.re.global ? rule : { ...rule, re: new RegExp(rule.re.source, `${rule.re.flags}g`) }
}

/** Adds a rule for every render. Returns a function that removes it again. */
export function registerTextRule(rule: TextRule): () => void {
  const entry = global(rule)
  registered.push(entry)
  return () => {
    const at = registered.indexOf(entry)
    if (at >= 0) registered.splice(at, 1)
  }
}

export function registeredTextRules(): readonly TextRule[] {
  return registered
}

function splitText(text: string, rule: TextRule): Inline[] {
  const out: Inline[] = []
  let last = 0
  for (const match of text.matchAll(rule.re)) {
    const built = match[0] === '' ? null : rule.build(match)
    if (!built) continue
    const at = match.index ?? 0
    if (at > last) out.push({ t: 'text', v: text.slice(last, at) })
    out.push(...built)
    last = at + match[0].length
  }
  if (last < text.length) out.push({ t: 'text', v: text.slice(last) })
  return out
}

/** Applies the rules, in order, to the text nodes of the tree. */
export function applyTextRules(nodes: Inline[], rules: readonly TextRule[], inLink = false): Inline[] {
  const active = inLink ? rules.filter(r => r.inLinks) : rules
  if (active.length === 0) return nodes
  return nodes.flatMap((node): Inline[] => {
    switch (node.t) {
      case 'text':
        return active.reduce<Inline[]>((acc, rule) => acc.flatMap(n => (n.t === 'text' ? splitText(n.v, rule) : [n])), [node])
      case 'fmt':
        return [{ ...node, c: applyTextRules(node.c, active, inLink) }]
      case 'link':
        return [{ ...node, c: applyTextRules(node.c, active, true) }]
      default:
        return [node]
    }
  })
}

/** applyTextRules over every inline run of a block tree. */
export function applyTextRulesToBlocks(blocks: Block[], rules: readonly TextRule[]): Block[] {
  if (rules.length === 0) return blocks
  return blocks.map((block): Block => {
    switch (block.t) {
      case 'p':
      case 'h':
      case 'sub':
        return { ...block, c: applyTextRules(block.c, rules) }
      case 'quote':
        return { ...block, c: applyTextRulesToBlocks(block.c, rules) }
      case 'list':
        return { ...block, items: block.items.map(item => ({ c: applyTextRules(item.c, rules), sub: applyTextRulesToBlocks(item.sub, rules) })) }
      default:
        return block
    }
  })
}
