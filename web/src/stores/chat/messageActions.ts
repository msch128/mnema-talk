import type { CreateMessageRequest } from '../../types/domain'
import { decodeMessage, decodeReactionResult } from '../../types/domain'
import { api } from '../../lib/api'
import { assertCommunity, type Cx } from './state'
import { applyToMessage, countReply, insertMessage, onOriginalDeleted, onOriginalEdited, removeFromWindow } from './messageWindow'
import { closeThread } from './threads'

export async function sendMessage(cx: Cx, content: string, parentId: string | null = null, replyToId: string | null = null) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!cs.activeChannel.value || !content.trim()) return null
  const json: CreateMessageRequest = { content: content.trim() }
  if (parentId) json.parent_id = parentId
  if (replyToId) json.reply_to_id = replyToId
  const msg = await api(`/api/channels/${cs.activeChannel.value.id}/messages`, { method: 'POST', json, decode: decodeMessage })
  assertCommunity(cx, community)
  // The WebSocket echo is de-duplicated by insertMessage / countReply.
  countReply(cx, msg)
  insertMessage(cx, msg)
  return msg
}

export async function uploadMedia(cx: Cx, file: File, content = '', parentId: string | null = null, replyToId: string | null = null) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!cs.activeChannel.value || !file) return null
  const form = new FormData()
  // Text fields go before the file so the server reads them first.
  if (content) form.append('content', content)
  if (parentId) form.append('parent_id', parentId)
  if (replyToId) form.append('reply_to_id', replyToId)
  form.append('file', file)
  const msg = await api(`/api/channels/${cs.activeChannel.value.id}/upload`, { method: 'POST', form, decode: decodeMessage })
  assertCommunity(cx, community)
  countReply(cx, msg)
  insertMessage(cx, msg)
  return msg
}

export async function editMessage(cx: Cx, channelId: string, messageId: string, content: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!content.trim()) return null
  const updated = await api(`/api/channels/${channelId}/messages/${messageId}`, {
    method: 'PUT',
    json: { content: content.trim() }, decode: decodeMessage
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
  const data = await api(`/api/messages/${messageId}/reactions`, { method: 'POST', json: { emoji }, decode: decodeReactionResult })
  assertCommunity(cx, community)
  applyToMessage(cx, messageId, m => { m.reactions = data.reactions || [] })
  return data.reactions
}

