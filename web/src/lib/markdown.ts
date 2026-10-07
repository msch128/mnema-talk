// Chat Markdown → HTML. Everything is HTML-escaped first; only the
// fixed tags produced below can appear in the output, and no inline event
// handlers are emitted (the CSP forbids them). Spoilers are toggled by the
// MarkdownContent component via event delegation.

// The syntax, shared with the plain-text helpers (lib/replies.js reply
// previews, lib/chatLogic.js link previews) so they treat exactly what is
// rendered here as markup. All are global regexes for String.replace/match.

/** ```lang\n code ``` (group 1: the code). */
export const CODE_BLOCK_RE = /```(?:[a-zA-Z0-9_-]+\n)?([\s\S]*?)```/g
/** `code` (group 1: the code). */
export const INLINE_CODE_RE = /`([^`\n]+)`/g
/** An http(s) URL. The trailing class keeps a closing ")" or "*" that belongs
 *  to the text, not to the URL; a "*" inside the URL stays. */
export const URL_RE = /\bhttps?:\/\/[^\s<]+[^\s<.,:;!?)\]'"*]/g
/** ||spoiler|| (group 1: the hidden text). */
export const SPOILER_RE = /\|\|([\s\S]+?)\|\|/g

/**
 * Inline emphasis, in the order it is applied: [regex, html, plain]. `html`
 * is the replacement renderMarkdown uses, `plain` the one that keeps only
 * the text.
 */
export const INLINE_FORMATS: readonly (readonly [RegExp, string, string])[] = [
  [/\*\*\*([^*\n]+)\*\*\*/g, '<strong><em>$1</em></strong>', '$1'],
  [/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>', '$1'],
  [/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1<em>$2</em>', '$1$2'],
  [/(^|[\s(])_([^_\n]+)_(?=$|[\s).,!?])/g, '$1<em>$2</em>', '$1$2'],
  [/~~([^~\n]+)~~/g, '<del>$1</del>', '$1']
]

export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/**
 * Renders chat Markdown. Options:
 * - known: Set of lower-cased usernames; when given, only those (and @all,
 *   @here) are highlighted as mentions.
 * - me: the reader's username; mentions of them get .md-mention-me.
 */
export function renderMarkdown(content: string | null | undefined, { known = null, me = '' }: { known?: ReadonlySet<string> | null; me?: string } = {}): string {
  if (!content) return ''
  let text = escapeHtml(content)

  // Code is extracted first so its contents are never formatted.
  const code: string[] = []
  const stash = (html: string) => `\u0000${code.push(html) - 1}\u0000`

  text = text.replace(CODE_BLOCK_RE, (_: string, body: string) =>
    stash(`<pre class="md-codeblock">${body.replace(/^\n+|\n+$/g, '')}</pre>`))
  text = text.replace(INLINE_CODE_RE, (_: string, body: string) => stash(`<code class="md-code">${body}</code>`))

  // Links are stashed too, so formatting and mentions never reach inside a
  // URL. Only http(s); the URL was escaped above and cannot contain quotes.
  text = text.replace(URL_RE, url =>
    stash(`<a href="${url}" target="_blank" rel="noopener noreferrer nofollow" class="md-link">${url}</a>`))

  text = text.replace(SPOILER_RE, '<span class="md-spoiler" role="button" tabindex="0">$1</span>')
  for (const [re, html] of INLINE_FORMATS) text = text.replace(re, html)
  text = text.replace(/^&gt;\s?(.*)$/gm, '<blockquote class="md-quote">$1</blockquote>')

  // Mentions (links are already stashed, so URLs are never touched).
  const self = me.toLowerCase()
  text = text.replace(/(^|[\s(])@([A-Za-z0-9_.-]{3,32})/g, (all: string, pre: string, raw: string) => {
    // A sentence may end right after a name: "@max." mentions max.
    const name = raw.replace(/[.-]+$/, '')
    const tail = raw.slice(name.length)
    const lower = name.toLowerCase()
    const group = lower === 'all' || lower === 'here'
    if (name.length < 3 || (known && !group && !known.has(lower))) return all
    const cls = ['md-mention']
    if (group) cls.push('md-mention-group')
    if (lower === self) cls.push('md-mention-me')
    const attrs = known ? ` data-mention="${lower}" role="button" tabindex="0"` : ''
    return `${pre}<span class="${cls.join(' ')}"${attrs}>@${name}</span>${tail}`
  })

  // \u0000 is the intentional code-span placeholder delimiter used for code spans above, so matching it is deliberate.
  // eslint-disable-next-line no-control-regex
  return text.replace(/\u0000(\d+)\u0000/g, (_: string, i: string) => code[Number(i)] ?? '')
}
