// Chat Markdown (Discord's syntax) → HTML. The text is parsed into a tree of
// plain nodes (lib/markdown/), and only render.ts turns that tree into HTML:
// every string is escaped there, tags come from fixed tables, and no inline
// event handlers are emitted (the CSP forbids them). Spoilers, mentions,
// copy buttons and masked links are handled by the MarkdownContent component
// through event delegation.
//
// Extending: a feature with its own token inside text (custom emoji, say)
// calls registerTextRule() once; see lib/markdown/rules.ts.
import type { Block } from './markdown/types'
import { parseBlocks } from './markdown/blocks'
import { applyTextRulesToBlocks, registeredTextRules } from './markdown/rules'
import type { TextRule } from './markdown/rules'
import { roleMentionRule, userMentionRule } from './markdown/mentions'
import type { RoleMention } from './markdown/mentions'
import { renderBlocks } from './markdown/render'
import { blocksPlain, linksIn } from './markdown/plain'

export { escapeHtml } from './markdown/escape'
export { registerTextRule } from './markdown/rules'
export type { TextRule, RoleMention }

/** Longer text is shown as it is, unformatted: parsing is for chat messages, not documents. */
export const MAX_MARKDOWN_CHARS = 8000

function parse(content: string): Block[] {
  // NUL characters carry no text and are dropped, so they cannot split tokens.
  // eslint-disable-next-line no-control-regex
  const text = content.replace(/\u0000/g, '')
  return text.length > MAX_MARKDOWN_CHARS ? [{ t: 'p', c: [{ t: 'text', v: text }] }] : parseBlocks(text)
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
  const rules: TextRule[] = [...registeredTextRules()]
  const roleRule = roleMentionRule(roles)
  if (roleRule) rules.push(roleRule)
  rules.push(userMentionRule(known, me))
  return renderBlocks(applyTextRulesToBlocks(parse(content), rules))
}

/**
 * The message without its formatting, for reply quotes and previews: markers
 * disappear, code and link text stay, ||spoilers|| become "Spoiler".
 */
export function markdownToPlain(content: string | null | undefined): string {
  return content ? blocksPlain(parse(content)) : ''
}

/** The http(s) links worth a preview card: shown as links, not in code, spoilers or <autolinks>. */
export function previewLinks(content: string | null | undefined): string[] {
  return content ? linksIn(parse(content)) : []
}
