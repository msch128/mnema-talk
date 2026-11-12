// What the Markdown tree says without its formatting: reply quotes,
// notification bodies and the like use this so they agree with what the
// message view renders. Also the link extraction behind preview cards.
import { t } from '../../../i18n'
import type { Block, Inline } from './types'
import { registeredTextRules } from './rules'

function plainText(text: string): string {
  let out = text
  for (const rule of registeredTextRules()) {
    if (rule.plain) out = out.replace(rule.re, (...args) => rule.plain?.(args as unknown as RegExpMatchArray) ?? '')
  }
  return out
}

/** spoiler: the word shown instead of hidden text (default: the reader's language). */
export function inlinePlain(nodes: readonly Inline[], spoiler = t('chat.spoiler')): string {
  return nodes
    .map(node => {
      switch (node.t) {
        case 'text':
          return plainText(node.v)
        case 'code':
          return node.v
        case 'fmt':
          return node.k === 'spoiler' ? spoiler : inlinePlain(node.c, spoiler)
        default:
          return inlinePlain(node.c, spoiler)
      }
    })
    .join('')
}

function blockPlain(block: Block, spoiler: string): string {
  switch (block.t) {
    case 'quote':
      return blocksPlain(block.c, spoiler)
    case 'list':
      return block.items.map(item => [inlinePlain(item.c, spoiler), blocksPlain(item.sub, spoiler)].filter(Boolean).join('\n')).join('\n')
    case 'code':
      return block.v
    default:
      return inlinePlain(block.c, spoiler)
  }
}

export function blocksPlain(blocks: readonly Block[], spoiler = t('chat.spoiler')): string {
  return blocks.map(block => blockPlain(block, spoiler)).join('\n')
}

function inlineLinks(nodes: readonly Inline[], out: string[]): void {
  for (const node of nodes) {
    if (node.t === 'link' && node.kind !== 'auto' && /^https?:/i.test(node.href)) out.push(node.href)
    else if (node.t === 'fmt' && node.k !== 'spoiler') inlineLinks(node.c, out)
  }
}

function blockLinks(blocks: readonly Block[], out: string[]): void {
  for (const block of blocks) {
    if (block.t === 'quote') {
      blockLinks(block.c, out)
    } else if (block.t === 'list') {
      for (const item of block.items) {
        inlineLinks(item.c, out)
        blockLinks(item.sub, out)
      }
    } else if (block.t !== 'code') {
      inlineLinks(block.c, out)
    }
  }
}

/** The http(s) links a message shows as links, in order: not <autolinks>, not in code or spoilers. */
export function linksIn(blocks: readonly Block[]): string[] {
  const out: string[] = []
  blockLinks(blocks, out)
  return out
}
