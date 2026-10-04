// Helpers for Discord-style reply previews (message.reply_to).

export const REPLY_PREVIEW_LEN = 200

/** Truncates to `max` characters (code points, like the server's LEFT()). */
export function replySnippet(content, max = REPLY_PREVIEW_LEN) {
  const chars = Array.from(content || '')
  return chars.length > max ? chars.slice(0, max).join('') : chars.join('')
}

/**
 * Strips Discord-style markdown markers so a reply preview reads as plain
 * text: **bold**, *em*, _em_, ~~del~~, `code` and code blocks keep their text,
 * ||spoilers|| become "Spoiler", "> " quote markers are dropped. Links stay.
 */
export function stripMarkdown(content) {
  let text = content || ''
  // Code first so markers inside code are kept verbatim.
  const code = []
  const stash = body => `\u0000${code.push(body) - 1}\u0000`
  text = text.replace(/```(?:[a-zA-Z0-9_-]+\n)?([\s\S]*?)```/g, (_, body) => stash(body.trim()))
  text = text.replace(/`([^`\n]+)`/g, (_, body) => stash(body))

  text = text.replace(/\|\|([\s\S]+?)\|\|/g, 'Spoiler')
  text = text.replace(/\*\*\*([^*\n]+)\*\*\*/g, '$1')
  text = text.replace(/\*\*([^*\n]+)\*\*/g, '$1')
  text = text.replace(/__([^_\n]+)__/g, '$1')
  text = text.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1$2')
  text = text.replace(/(^|[\s(])_([^_\n]+)_(?=$|[\s).,!?])/g, '$1$2')
  text = text.replace(/~~([^~\n]+)~~/g, '$1')
  text = text.replace(/^>\s?/gm, '')

  // eslint-disable-next-line no-control-regex
  return text.replace(/\u0000(\d+)\u0000/g, (_, i) => code[Number(i)])
}

/** One-line plain-text preview; markdown stripped, newlines collapse to spaces. */
export function previewText(content) {
  return stripMarkdown(content).replace(/\s+/g, ' ').trim()
}

/**
 * Applies `patch(preview)` to every reply preview that points at `originalId`
 * in the given message lists. Returns the number of previews touched.
 */
export function patchReplyPreviews(lists, originalId, patch) {
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
export function markPreviewEdited(lists, original) {
  return patchReplyPreviews(lists, original.id, preview => {
    if (typeof original.content === 'string') preview.content = replySnippet(original.content)
    if (Array.isArray(original.attachments)) preview.has_attachments = original.attachments.length > 0
  })
}

/** The original was deleted: show the "deleted" placeholder. */
export function markPreviewDeleted(lists, originalId) {
  return patchReplyPreviews(lists, originalId, preview => {
    preview.deleted = true
    preview.content = ''
  })
}
