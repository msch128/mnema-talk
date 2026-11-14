// Helpers for reply previews (message.reply_to).
import { markdownToPlain } from '../../shared/lib/markdown'
import { markdownSnippet } from '../../shared/lib/markdown/snippet'
import type { Message, ReplyPreview } from '../../types/domain'

export const REPLY_PREVIEW_LEN = 200

/** Truncates plain text to `max` characters (code points); Markdown is cut with markdownSnippet. */
export function replySnippet(content: string | null | undefined, max = REPLY_PREVIEW_LEN): string {
  const chars = Array.from(content || '')
  return chars.length > max ? chars.slice(0, max).join('') : chars.join('')
}

/**
 * Strips the markdown that renderMarkdown renders so a reply preview reads as
 * plain text (the same parser, so both always agree): emphasis markers go,
 * code and link texts stay, ||spoilers|| become "Spoiler", quote, heading and
 * list markers are dropped.
 */
export function stripMarkdown(content: string | null | undefined): string {
  return markdownToPlain(content)
}

// previewText lives with the Markdown helpers, so other features can use it.
export { previewText } from '../../shared/lib/markdown'

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

/**
 * The original was edited, or a file of it removed: refresh the cached snippet,
 * built like the server's (mdplain.Snippet), and say "File removed" once every
 * file is gone, as the server does.
 */
export function markPreviewEdited(lists: Array<Array<Pick<Message, 'reply_to' | 'reply_to_id'>>>, original: Pick<Message, 'id'> & Partial<Pick<Message, 'content' | 'attachments'>>): number {
  const files = original.attachments
  return patchReplyPreviews(lists, original.id, preview => {
    if (typeof original.content === 'string') preview.content = markdownSnippet(original.content)
    if (!Array.isArray(files)) return
    preview.has_attachments = files.length > 0
    preview.attachments_removed = files.length > 0 && files.every(file => file.is_deleted)
  })
}

/** The original was deleted: show the "deleted" placeholder. */
export function markPreviewDeleted(lists: Array<Array<Pick<Message, 'reply_to' | 'reply_to_id'>>>, originalId: string): number {
  return patchReplyPreviews(lists, originalId, preview => {
    preview.deleted = true
    preview.content = ''
  })
}
