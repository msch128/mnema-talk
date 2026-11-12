import type { Message } from '../../types/domain'
import { messageText } from '../../shared/lib/textLength'
import { decodeMessage, decodeReactionResult } from '../../types/domain'
import { api } from '../../shared/lib/api'
import { resolveReaction } from '../../shared/lib/reactionEmoji'
import { reactionForm } from '../../shared/lib/reactionForm'
import { useEmojiStore } from '../emoji'
import { useServerFeaturesStore } from '../serverFeatures'
import { assertCommunity, type Cx } from './state'
import { applyToMessage, onOriginalDeleted, onOriginalEdited, removeFromWindow } from './messageWindow'
import { landSent, newNonce, postText, postWithReplyFallback, withEmojiTokens, type TextSend } from './messagePost'
import { echoedMessage, enqueue, hasQueued, isNetworkFailure } from './outbox'
import { SEND_TIMEOUT_MS } from './outboxStorage'
import { closeThread } from './threads'

/**
 * Posts a message; attachmentIds are files staged with the attachment tray, shown in that order.
 * A text message that cannot be sent because the connection is down is kept in the outbox
 * (see outbox.ts) and answers with that pending entry; the composer clears as for a sent one.
 */
export async function sendMessage(cx: Cx, content: string, parentId: string | null = null, replyToId: string | null = null, attachmentIds: readonly string[] = [], targetChannelId?: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  const channelId = targetChannelId ?? cs.activeChannel.value?.id
  if (!channelId || (!messageText(content) && !attachmentIds.length)) return null
  const send: TextSend = { content, parentId, replyToId, nonce: newNonce(), attachmentIds }
  // Files are not kept for later; a message with them fails as it always did.
  if (!attachmentIds.length && hasQueued(cx, channelId)) return enqueue(cx, channelId, send, true)
  let msg: Message
  const features = useServerFeaturesStore()
  if (!features.loaded) await features.ensureLoaded()
  // A stalled network must not lock the composer: a text-only first try ends like the outbox's, and then waits there.
  // Only when the server de-duplicates by nonce: otherwise the resend of a request that was merely slow would post the message twice, so the composer waits for the request instead.
  const abort = new AbortController()
  const timer = attachmentIds.length || !features.messageNonce ? undefined : setTimeout(() => abort.abort(), SEND_TIMEOUT_MS)
  try {
    msg = await postText(channelId, send, attachmentIds.length ? undefined : abort.signal)
  } catch (error) {
    if (!attachmentIds.length && isNetworkFailure(error) && cs.communityGeneration === community) {
      // The answer was lost, but the server's event may already have brought the message: nothing to keep then.
      return echoedMessage(cx, send.nonce) ?? enqueue(cx, channelId, send)
    }
    throw error
  } finally {
    clearTimeout(timer)
  }
  assertCommunity(cx, community)
  landSent(cx, msg)
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
  landSent(cx, msg)
  return msg
}

export async function editMessage(cx: Cx, channelId: string, messageId: string, content: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!messageText(content)) return null
  const updated = await api(`/api/channels/${channelId}/messages/${messageId}`, {
    method: 'PUT',
    json: { content: withEmojiTokens(messageText(content)) }, decode: decodeMessage
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
  // One form per emoji (the server applies the same rule): the quick thumbs up and the picker's are one reaction.
  const resolved = reactionForm(typeof pending === 'string' ? pending : await pending)
  const data = await api(`/api/messages/${messageId}/reactions`, { method: 'POST', json: { emoji: resolved }, decode: decodeReactionResult })
  assertCommunity(cx, community)
  applyToMessage(cx, messageId, m => { m.reactions = data.reactions || [] })
  return data.reactions
}

