// Helpers for reply previews (message.reply_to).
import { t } from '../i18n'
import { CODE_BLOCK_RE, INLINE_CODE_RE, SPOILER_RE, INLINE_FORMATS } from './markdown'
import type { Message, ReplyPreview } from '../types/domain'

export const REPLY_PREVIEW_LEN = 200

/** Truncates to `max` characters (code points, like the server's LEFT()). */
export function replySnippet(content: string | null | undefined, max = REPLY_PREVIEW_LEN): string {
  const chars = Array.from(content || '')
  return chars.length > max ? chars.slice(0, max).join('') : chars.join('')
}

/**
 * Strips the markdown that renderMarkdown renders so a reply preview reads as
 * plain text: **bold**, *em*, _em_, ~~del~~, `code` and code blocks keep their
 * text, ||spoilers|| become "Spoiler", "> " quote markers are dropped. Links
 * stay. Anything renderMarkdown leaves alone (like __x__) stays as typed.
 */
export function stripMarkdown(content: string | null | undefined): string {
  let text = content || ''
  // Code first so markers inside code are kept verbatim.
  const code: string[] = []
  const stash = (body: string) => `\u0000${code.push(body) - 1}\u0000`
  text = text.replace(CODE_BLOCK_RE, (_: string, body: string) => stash(body.trim()))
  text = text.replace(INLINE_CODE_RE, (_: string, body: string) => stash(body))

  text = text.replace(SPOILER_RE, () => t('chat.spoiler'))
  for (const [re, , plain] of INLINE_FORMATS) text = text.replace(re, plain)
  text = text.replace(/^>\s?/gm, '')

  // eslint-disable-next-line no-control-regex
  return text.replace(/\u0000(\d+)\u0000/g, (_: string, i: string) => code[Number(i)] ?? '')
}

/** One-line plain-text preview; markdown stripped, newlines collapse to spaces. */
export function previewText(content: string | null | undefined): string {
  return stripMarkdown(content).replace(/\s+/g, ' ').trim()
}

/**
 * Applies `patch(preview)` to every reply preview that points at `originalId`
 * in the given message lists. Returns the number of previews touched.
 */
export function patchReplyPreviews(lists: Array<Array<Pick<Message, 'reply_to' | 'reply_to_id'>>>, originalId: string, patch: (preview: ReplyPreview) => void): number {
  let touched = 0
  for (const list of lists) {
    for (const msg of list || []) {
      if (msg?.reply_to && msg.reply_to_id === originalId) {
        patch(msg.reply_to)
        touched++
      }
    }
  }
  return touched
}

/** The original was edited: refresh the cached snippet. */
export function markPreviewEdited(lists: Array<Array<Pick<Message, 'reply_to' | 'reply_to_id'>>>, original: Pick<Message, 'id'> & Partial<Pick<Message, 'content' | 'attachments'>>): number {
  return patchReplyPreviews(lists, original.id, preview => {
    if (typeof original.content === 'string') preview.content = replySnippet(original.content)
    if (Array.isArray(original.attachments)) preview.has_attachments = original.attachments.length > 0
  })
}

/** The original was deleted: show the "deleted" placeholder. */
export function markPreviewDeleted(lists: Array<Array<Pick<Message, 'reply_to' | 'reply_to_id'>>>, originalId: string): number {
  return patchReplyPreviews(lists, originalId, preview => {
    preview.deleted = true
    preview.content = ''
  })
}
