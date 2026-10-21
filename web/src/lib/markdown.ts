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

function stashCodeAndLinks(escaped: string): { text: string; code: string[] } {
  const code: string[] = []
  const stash = (html: string) => `\u0000${code.push(html) - 1}\u0000`

  let text = escaped.replace(CODE_BLOCK_RE, (_: string, body: string) =>
    stash(`<pre class="md-codeblock">${body.replace(/^\n+|\n+$/g, '')}</pre>`))
  text = text.replace(INLINE_CODE_RE, (_: string, body: string) => stash(`<code class="md-code">${body}</code>`))
  text = text.replace(URL_RE, url =>
    stash(`<a href="${url}" target="_blank" rel="noopener noreferrer nofollow" class="md-link">${url}</a>`))

  return { text, code }
}

function applyInlineStyles(text: string): string {
  let styled = text.replace(SPOILER_RE, '<span class="md-spoiler" role="button" tabindex="0">$1</span>')
  for (const [re, html] of INLINE_FORMATS) styled = styled.replace(re, html)
  return styled.replace(/^&gt;\s?(.*)$/gm, '<blockquote class="md-quote">$1</blockquote>')
}

function isKnownMention(lower: string, group: boolean, known: ReadonlySet<string> | null): boolean {
  if (!known || group) return true
  return known.has(lower)
}

interface MentionMatch {
  all: string
  pre: string
  raw: string
}

function formatMention(
  m: MentionMatch,
  known: ReadonlySet<string> | null,
  self: string
): string {
  const name = m.raw.replace(/[.-]+$/, '')
  const tail = m.raw.slice(name.length)
  const lower = name.toLowerCase()
  const group = lower === 'all' || lower === 'here'
  if (name.length < 3 || !isKnownMention(lower, group, known)) return m.all
  const cls = ['md-mention']
  if (group) cls.push('md-mention-group')
  if (lower === self) cls.push('md-mention-me')
  const attrs = known ? ` data-mention="${lower}" role="button" tabindex="0"` : ''
  return `${m.pre}<span class="${cls.join(' ')}"${attrs}>@${name}</span>${tail}`
}

/** A role the message mentions; me marks a role the reader holds. */
export interface RoleMention {
  name: string
  me?: boolean
}

/**
 * Draws "@Role Name" as a pill for the roles the server recorded on the
 * message. Role names may contain spaces, so the known names are matched
 * (longest first) instead of a username-like token. Roles are not clickable
 * and carry no colour here (the CSP forbids inline styles in this markup).
 */
function highlightRoles(html: string, roles: readonly RoleMention[]): string {
  if (!roles.length) return html
  const byName = new Map(roles.map(r => [escapeHtml(r.name).toLowerCase(), r]))
  const names = [...byName.keys()]
    .sort((a, b) => b.length - a.length)
    .map(n => n.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  const re = new RegExp(`(^|[\\s(])@(${names.join('|')})(?![A-Za-z0-9_-]|\\.[A-Za-z0-9_-])`, 'gi')
  return html.replace(re, (_all: string, pre: string, name: string) => {
    const cls = byName.get(name.toLowerCase())?.me ? 'md-mention md-mention-role md-mention-me' : 'md-mention md-mention-role'
    return `${pre}<span class="${cls}">@${name}</span>`
  })
}

/**
 * Renders chat Markdown. Options:
 * - known: Set of lower-cased usernames; when given, only those (and @all,
 *   @here) are highlighted as mentions.
 * - me: the reader's username; mentions of them get .md-mention-me.
 * - roles: the roles the message mentions (by the server), drawn as pills.
 */
export function renderMarkdown(
  content: string | null | undefined,
  { known = null, me = '', roles = [] }: { known?: ReadonlySet<string> | null; me?: string; roles?: readonly RoleMention[] } = {}
): string {
  if (!content) return ''
  const { text: stashed, code } = stashCodeAndLinks(escapeHtml(content))
  const styled = applyInlineStyles(stashed)
  const self = me.toLowerCase()
  const mentioned = highlightRoles(styled, roles).replace(/(^|[\s(])@([A-Za-z0-9_.-]{3,32})/g, (all: string, pre: string, raw: string) =>
    formatMention({ all, pre, raw }, known, self))

  // \u0000 is the intentional code-span placeholder delimiter used for code spans above, so matching it is deliberate.
  // eslint-disable-next-line no-control-regex
  return mentioned.replace(/\u0000(\d+)\u0000/g, (_: string, i: string) => code[Number(i)] ?? '')
}
