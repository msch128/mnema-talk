// Chat Markdown → HTML. Everything is HTML-escaped first; only the
// fixed tags produced below can appear in the output, and no inline event
// handlers are emitted (the CSP forbids them). Spoilers are toggled by the
// MarkdownContent component via event delegation.

export function escapeHtml(text) {
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
export function renderMarkdown(content, { known = null, me = '' } = {}) {
  if (!content) return ''
  let text = escapeHtml(content)

  // Code is extracted first so its contents are never formatted.
  const code = []
  const stash = html => `\u0000${code.push(html) - 1}\u0000`

  text = text.replace(/```(?:[a-zA-Z0-9_-]+\n)?([\s\S]*?)```/g, (_, body) =>
    stash(`<pre class="md-codeblock">${body.replace(/^\n+|\n+$/g, '')}</pre>`))
  text = text.replace(/`([^`\n]+)`/g, (_, body) => stash(`<code class="md-code">${body}</code>`))

  // Links are stashed too, so formatting and mentions never reach inside a
  // URL. Only http(s); the URL was escaped above and cannot contain quotes.
  // The trailing class keeps a closing ")" or "*" that belongs to the text,
  // not to the URL; a "*" inside the URL stays.
  text = text.replace(/\bhttps?:\/\/[^\s<]+[^\s<.,:;!?)\]'"*]/g, url =>
    stash(`<a href="${url}" target="_blank" rel="noopener noreferrer nofollow" class="md-link">${url}</a>`))

  text = text.replace(/\|\|([\s\S]+?)\|\|/g, '<span class="md-spoiler" role="button" tabindex="0">$1</span>')
  text = text.replace(/\*\*\*([^*\n]+)\*\*\*/g, '<strong><em>$1</em></strong>')
  text = text.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
  text = text.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1<em>$2</em>')
  text = text.replace(/(^|[\s(])_([^_\n]+)_(?=$|[\s).,!?])/g, '$1<em>$2</em>')
  text = text.replace(/~~([^~\n]+)~~/g, '<del>$1</del>')
  text = text.replace(/^&gt;\s?(.*)$/gm, '<blockquote class="md-quote">$1</blockquote>')

  // Mentions (links are already stashed, so URLs are never touched).
  const self = me.toLowerCase()
  text = text.replace(/(^|[\s(])@([A-Za-z0-9_.-]{3,32})/g, (all, pre, raw) => {
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
  return text.replace(/\u0000(\d+)\u0000/g, (_, i) => code[Number(i)])
}
