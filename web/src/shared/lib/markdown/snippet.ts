// The start of a message for places that show only a piece of it (reply
// quotes, the conversation list). The server builds the snippets it sends with
// the same rule (internal/mdplain/snippet.go); internal/mdplain/testdata/snippet.json
// is checked by both.
import { markdownToPlain } from '../markdown'
import { clampChars } from '../textLength'

/** How many characters of a message a snippet shows. */
export const SNIPPET_LEN = 200

// A spoiler while the plain text is cut: a private use character, removed from the message first.
const HIDDEN = ''
// How a spoiler is written back: every reader shows its own word for it.
const SPOILER_MARKDOWN = '||…||'
// Characters that start inline Markdown, and those that start a block at the start of a line.
const ESCAPED_ANYWHERE = '\\`*_~|<['
const ESCAPED_FIRST = '#>-'

/** Plain text written as Markdown that reads back as the same text. */
function escapePlain(plain: string): string {
  let out = ''
  let digits = true
  let first = true
  for (const ch of plain) {
    if (ch === HIDDEN) out += SPOILER_MARKDOWN
    // "1. " would start a numbered list.
    else if (ESCAPED_ANYWHERE.includes(ch) || (first && ESCAPED_FIRST.includes(ch)) || (!first && digits && ch === '.')) out += '\\' + ch
    else out += ch
    digits = digits && ch >= '0' && ch <= '9'
    first = false
  }
  return out
}

/**
 * At most `limit` characters of a message's text, as Markdown. A message that
 * fits is returned as it is. A longer one is reduced to its plain text first
 * and cut after that: cutting the Markdown first left a spoiler (or code, or a
 * link) open at the cut when it closed later, and its hidden text showed. The
 * cut text is escaped back into Markdown with each spoiler as ||…||, so
 * previewText of the snippet is exactly that text.
 */
export function markdownSnippet(content: string | null | undefined, limit = SNIPPET_LEN): string {
  const text = content ?? ''
  if (Array.from(text).length <= limit) return text
  let plain = markdownToPlain(text.replaceAll(HIDDEN, ''), HIDDEN).replace(/\s+/g, ' ').trim()
  if (Array.from(plain).length > limit) plain = clampChars(plain, limit - 1).trim() + '…'
  return escapePlain(plain)
}
