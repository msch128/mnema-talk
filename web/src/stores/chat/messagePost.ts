import type { CreateMessageRequest, Message } from '../../types/domain'
import { decodeMessage } from '../../types/domain'
import { api, isApiError } from '../../lib/api'
import { encodeShortcodes } from '../../lib/customEmoji'
import { t } from '../../i18n'
import { useEmojiStore } from '../emoji'
import { useServerFeaturesStore } from '../serverFeatures'
import type { Cx } from './state'
import { countReply, insertMessage, showToast } from './messageWindow'

/** Typed :name: of known custom emoji become their stable token. */
export function withEmojiTokens(text: string): string {
  return encodeShortcodes(text, useEmojiStore().byName)
}

/**
 * Posts with the reply target; when the server refuses the request as invalid
 * (the message replied to is gone, as with a restored draft), posts once more
 * without it. A request that is invalid for another reason fails the same way
 * again and its error reaches the caller.
 */
export async function postWithReplyFallback<T>(replyToId: string | null, post: (replyToId: string | null) => Promise<T>): Promise<T> {
  try {
    return await post(replyToId)
  } catch (error) {
    if (!replyToId || !isApiError(error) || error.status !== 400) throw error
    const posted = await post(null)
    showToast(t('chat.replyTargetGone'))
    return posted
  }
}

/** Writing a message means having read everything before it: the server moves the marker (and tells the other sessions); this does the same here at once. Thread replies change nothing, and neither does a message older than the marker (the answer to a repeated send). */
export function markReadBySending({ cs }: Cx, msg: Message, isRepeat = false) {
  if (msg.parent_id) return
  const prev = cs.readStates.value[msg.channel_id] || { channel_id: msg.channel_id, notify_level: 'all' as const, unread_count: 0, mention_count: 0, last_read_at: null }
  // The answer to a replayed send (the server's 200) is an old message: it must not pull the marker back. A newly stored one (201) always moves it, whatever clock the marker came from.
  if (isRepeat && prev.last_read_at && Date.parse(prev.last_read_at) > Date.parse(msg.created_at)) return
  cs.readStates.value = {
    ...cs.readStates.value,
    [msg.channel_id]: { ...prev, unread_count: 0, mention_count: 0, last_read_at: msg.created_at }
  }
  if (msg.channel_id === cs.activeChannel.value?.id) cs.activeChannelLastReadAt.value = msg.created_at
}

/** A message the server accepted joins the window. `isRepeat` marks the server's answer to a replayed nonce. The WebSocket echo is de-duplicated by insertMessage / countReply. */
export function landSent(cx: Cx, msg: Message, isRepeat = false) {
  countReply(cx, msg)
  insertMessage(cx, msg)
  markReadBySending(cx, msg, isRepeat)
}

export interface TextSend { content: string; parentId: string | null; replyToId: string | null; nonce: string; attachmentIds?: readonly string[] }

/** One text POST. The nonce makes a repeat of it (after a lost answer) return the first message. */
/** `onStatus` hears the answer's HTTP status: 200 means the nonce was replayed (an earlier try had been stored), 201 a new message. */
export function postText(channelId: string, send: TextSend, signal?: AbortSignal, onStatus?: (status: number) => void): Promise<Message> {
  // The list may have failed to load at start; without it the nonce would be left out for the whole session.
  const features = useServerFeaturesStore()
  if (features.loaded) return postLoaded(channelId, send, signal, onStatus)
  return features.ensureLoaded().then(() => postLoaded(channelId, send, signal, onStatus))
}

function postLoaded(channelId: string, send: TextSend, signal?: AbortSignal, onStatus?: (status: number) => void): Promise<Message> {
  return postWithReplyFallback(send.replyToId, reply => {
    const json: CreateMessageRequest = { content: withEmojiTokens(send.content.trim()) }
    // Servers before the nonce (no "messages.nonce" feature) reject unknown JSON fields, so it is only sent to
    // a server that lists it. Without it the outbox still sends, but a resend after a lost answer cannot be
    // de-duplicated there and may post the message twice; that is accepted on an older server.
    if (useServerFeaturesStore().messageNonce) json.nonce = send.nonce
    if (send.attachmentIds?.length) json.attachment_ids = [...send.attachmentIds]
    if (send.parentId) json.parent_id = send.parentId
    if (reply) json.reply_to_id = reply
    return api(`/api/channels/${channelId}/messages`, { method: 'POST', json, decode: decodeMessage, ...(signal ? { signal } : {}), ...(onStatus ? { onStatus } : {}) })
  })
}

/** A new random UUID for a send (randomUUID only exists in secure contexts; a trusted-LAN http:// install lacks it). */
export function newNonce(): string {
  if (globalThis.crypto.randomUUID) return globalThis.crypto.randomUUID()
  const b = globalThis.crypto.getRandomValues(new Uint8Array(16))
  b[6] = (b[6]! & 0x0f) | 0x40
  b[8] = (b[8]! & 0x3f) | 0x80
  const hex = Array.from(b, x => x.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
