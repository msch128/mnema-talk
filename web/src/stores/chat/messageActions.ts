import type { CreateMessageRequest, Message } from '../../types/domain'
import { decodeMessage, decodeReactionResult } from '../../types/domain'
import { api, isApiError } from '../../lib/api'
import { encodeShortcodes } from '../../lib/customEmoji'
import { resolveReaction } from '../../lib/reactionEmoji'
import { t } from '../../i18n'
import { useEmojiStore } from '../emoji'
import { assertCommunity, type Cx } from './state'
import { applyToMessage, countReply, insertMessage, onOriginalDeleted, onOriginalEdited, removeFromWindow, showToast } from './messageWindow'
import { closeThread } from './threads'

/** Typed :name: of known custom emoji become their stable token. */
function withEmojiTokens(text: string): string {
  return encodeShortcodes(text, useEmojiStore().byName)
}

/**
 * Posts with the reply target; when the server refuses the request as invalid
 * (the message replied to is gone, as with a restored draft), posts once more
 * without it. A request that is invalid for another reason fails the same way
 * again and its error reaches the caller.
 */
async function postWithReplyFallback<T>(replyToId: string | null, post: (replyToId: string | null) => Promise<T>): Promise<T> {
  try {
    return await post(replyToId)
  } catch (error) {
    if (!replyToId || !isApiError(error) || error.status !== 400) throw error
    const posted = await post(null)
    showToast(t('chat.replyTargetGone'))
    return posted
  }
}

// Writing a message means having read everything before it: the server moves
// the marker to the message (and tells the other sessions); this does the same
// here at once. The "new since" divider is dropped, so the "Jump to first
// unread" pill goes, and only what others post afterwards counts again.
// Thread replies are no channel messages and change nothing.
function markReadBySending({ cs }: Cx, msg: Message) {
  if (msg.parent_id) return
  const prev = cs.readStates.value[msg.channel_id] || { channel_id: msg.channel_id, notify_level: 'all' as const, unread_count: 0, mention_count: 0, last_read_at: null }
  cs.readStates.value = {
    ...cs.readStates.value,
    [msg.channel_id]: { ...prev, unread_count: 0, mention_count: 0, last_read_at: msg.created_at }
  }
  if (msg.channel_id === cs.activeChannel.value?.id) cs.activeChannelLastReadAt.value = msg.created_at
}

/** Posts a message; attachmentIds are files staged with the attachment tray, shown in that order. */
export async function sendMessage(cx: Cx, content: string, parentId: string | null = null, replyToId: string | null = null, attachmentIds: readonly string[] = []) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!cs.activeChannel.value || (!content.trim() && !attachmentIds.length)) return null
  const channelId = cs.activeChannel.value.id
  const msg = await postWithReplyFallback(replyToId, reply => {
    const json: CreateMessageRequest = { content: withEmojiTokens(content.trim()) }
    if (attachmentIds.length) json.attachment_ids = [...attachmentIds]
    if (parentId) json.parent_id = parentId
    if (reply) json.reply_to_id = reply
    return api(`/api/channels/${channelId}/messages`, { method: 'POST', json, decode: decodeMessage })
  })
  assertCommunity(cx, community)
  // The WebSocket echo is de-duplicated by insertMessage / countReply.
  countReply(cx, msg)
  insertMessage(cx, msg)
  markReadBySending(cx, msg)
  return msg
}

export async function uploadMedia(cx: Cx, file: File, content = '', parentId: string | null = null, replyToId: string | null = null) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!cs.activeChannel.value || !file) return null
  const channelId = cs.activeChannel.value.id
  const msg = await postWithReplyFallback(replyToId, reply => {
    const form = new FormData()
    // Text fields go before the file so the server reads them first.
    if (content) form.append('content', withEmojiTokens(content))
    if (parentId) form.append('parent_id', parentId)
    if (reply) form.append('reply_to_id', reply)
    form.append('file', file)
    return api(`/api/channels/${channelId}/upload`, { method: 'POST', form, decode: decodeMessage })
  })
  assertCommunity(cx, community)
  countReply(cx, msg)
  insertMessage(cx, msg)
  markReadBySending(cx, msg)
  return msg
}

export async function editMessage(cx: Cx, channelId: string, messageId: string, content: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!content.trim()) return null
  const updated = await api(`/api/channels/${channelId}/messages/${messageId}`, {
    method: 'PUT',
    json: { content: withEmojiTokens(content.trim()) }, decode: decodeMessage
  })
  assertCommunity(cx, community)
  applyToMessage(cx, messageId, m => Object.assign(m, updated))
  onOriginalEdited(cx, updated)
  return updated
}

export async function deleteMessage(cx: Cx, channelId: string, messageId: string, reason?: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  await api(`/api/channels/${channelId}/messages/${messageId}`, reason ? { method: 'DELETE', json: { reason } } : { method: 'DELETE' })
  assertCommunity(cx, community)
  removeFromWindow(cx, messageId)
  cs.threadReplies.value = cs.threadReplies.value.filter(m => m.id !== messageId)
  onOriginalDeleted(cx, messageId)
  if (cs.activeThread.value?.id === messageId) closeThread(cx)
}

/** Pins or unpins a message (Manage messages); the server's message_update keeps every client in step. */
export async function setPinned(cx: Cx, channelId: string, messageId: string, pinned: boolean) {
  const { cs } = cx
  const community = cs.communityGeneration
  const message = await api(`/api/channels/${channelId}/messages/${messageId}/pin`, { method: pinned ? 'PUT' : 'DELETE', decode: decodeMessage })
  assertCommunity(cx, community)
  applyToMessage(cx, messageId, m => Object.assign(m, message))
  return message
}

export async function toggleReaction(cx: Cx, messageId: string, emoji: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  const pending = resolveReaction(emoji, useEmojiStore().byName)
  const resolved = typeof pending === 'string' ? pending : await pending
  const data = await api(`/api/messages/${messageId}/reactions`, { method: 'POST', json: { emoji: resolved }, decode: decodeReactionResult })
  assertCommunity(cx, community)
  applyToMessage(cx, messageId, m => { m.reactions = data.reactions || [] })
  return data.reactions
}

