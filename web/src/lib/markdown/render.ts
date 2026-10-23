// The only place the Markdown tree becomes HTML. Every string is escaped; tags
// come from the fixed tables below; generated elements ('el' nodes) pass an
// attribute allowlist with a pattern per attribute, so extensions cannot
// introduce script, styles, event handlers or off-site URLs.
import { t } from '../../i18n'
import type { Block, FormatKind, Inline, ListItem } from './types'
import { escapeHtml } from './escape'
import { codeHtml } from './highlight'
import { hrefHost } from './link'

const FORMAT_TAGS: Record<FormatKind, string> = {
  strong: '<strong>',
  em: '<em>',
  u: '<u class="md-underline">',
  del: '<del>',
  spoiler: '<span class="md-spoiler" role="button" tabindex="0">'
}
const FORMAT_CLOSE: Record<FormatKind, string> = {
  strong: '</strong>',
  em: '</em>',
  u: '</u>',
  del: '</del>',
  spoiler: '</span>'
}

// eslint-disable-next-line no-control-regex
const PLAIN_VALUE = /^[^\u0000-\u001f\u007f]{0,200}$/
/** Allowed attributes of generated elements, each with the pattern its value must match. */
const ELEMENT_ATTRS: Record<string, RegExp> = {
  class: /^[A-Za-z0-9 _-]{1,120}$/,
  alt: PLAIN_VALUE,
  title: PLAIN_VALUE,
  role: /^button$/,
  tabindex: /^0$/,
  loading: /^lazy$/,
  draggable: /^false$/,
  width: /^\d{1,3}$/,
  height: /^\d{1,3}$/,
  // A same-origin path, never a protocol-relative or absolute URL.
  src: /^\/(?!\/)[A-Za-z0-9_\-./~%]{1,200}(?:\?[A-Za-z0-9_\-=&%.]{0,100})?$/
}
const DATA_ATTR = /^data-[a-z][a-z0-9-]{0,30}$/

function elementAttrs(attrs: Readonly<Record<string, string>>): string {
  let out = ''
  for (const [name, value] of Object.entries(attrs)) {
    const rule = DATA_ATTR.test(name) ? PLAIN_VALUE : ELEMENT_ATTRS[name]
    if (rule?.test(value)) out += ` ${name}="${escapeHtml(value)}"`
  }
  return out
}

function element(node: Extract<Inline, { t: 'el' }>): string {
  const attrs = elementAttrs(node.attrs)
  if (node.tag === 'img') {
    // An image without a valid same-origin source degrades to its alt text.
    return ELEMENT_ATTRS.src?.test(node.attrs.src ?? '') ? `<img${attrs}>` : escapeHtml(node.attrs.alt ?? '')
  }
  return `<span${attrs}>${renderInline(node.c)}</span>`
}

function link(node: Extract<Inline, { t: 'link' }>): string {
  const href = escapeHtml(node.href)
  const mail = node.href.toLowerCase().startsWith('mailto:')
  const cls = node.kind === 'masked' ? 'md-link md-link-masked' : 'md-link'
  const target = mail ? '' : ' target="_blank"'
  const masked = node.kind === 'masked' ? ` title="${href}" data-host="${escapeHtml(hrefHost(node.href))}"` : ''
  const confirm = node.confirm ? ' data-confirm="1"' : ''
  return `<a href="${href}"${target} rel="noopener noreferrer nofollow" class="${cls}"${masked}${confirm}>${renderInline(node.c)}</a>`
}

export function renderInline(nodes: readonly Inline[]): string {
  let out = ''
  for (const node of nodes) {
    switch (node.t) {
      case 'text':
        out += escapeHtml(node.v)
        break
      case 'code':
        out += `<code class="md-code">${escapeHtml(node.v)}</code>`
        break
      case 'fmt':
        out += FORMAT_TAGS[node.k] + renderInline(node.c) + FORMAT_CLOSE[node.k]
        break
      case 'link':
        out += link(node)
        break
      case 'el':
        out += element(node)
        break
    }
  }
  return out
}

function codeBlock(lang: string, code: string): string {
  const label = escapeHtml(t('chat.copyCode'))
  const name = lang ? `<span class="md-code-lang">${escapeHtml(lang)}</span>` : ''
  const copy = `<button type="button" class="md-copy" data-md-copy aria-label="${label}" title="${label}">${label}</button>`
  return `<div class="md-codeblock-wrap"><div class="md-code-tools">${name}${copy}</div><pre class="md-codeblock"><code class="hljs">${codeHtml(code, lang)}</code></pre></div>`
}

function listItem(item: ListItem): string {
  return `<li>${renderInline(item.c)}${renderBlocks(item.sub)}</li>`
}

function list(block: Extract<Block, { t: 'list' }>): string {
  const items = block.items.map(listItem).join('')
  if (!block.ordered) return `<ul class="md-list">${items}</ul>`
  const start = block.start === 1 ? '' : ` start="${Math.trunc(block.start)}"`
  return `<ol class="md-list"${start}>${items}</ol>`
}

function renderBlock(block: Block): string {
  switch (block.t) {
    case 'p':
      return renderInline(block.c)
    case 'h':
      return `<h${block.level} class="md-heading md-h${block.level}">${renderInline(block.c)}</h${block.level}>`
    case 'sub':
      return `<div class="md-subtext">${renderInline(block.c)}</div>`
    case 'quote':
      return `<blockquote class="md-quote">${renderBlocks(block.c)}</blockquote>`
    case 'list':
      return list(block)
    case 'code':
      return codeBlock(block.lang, block.v)
  }
}

export function renderBlocks(blocks: readonly Block[]): string {
  return blocks.map(renderBlock).join('')
}
