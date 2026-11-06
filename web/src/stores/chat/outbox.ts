import { caughtErrorMessage, isApiError } from '../../lib/api'
import { t } from '../../i18n'
import type { Cx } from './state'
import type { PendingMessage } from './outboxTypes'
import { landSent, postText, type TextSend } from './messagePost'

// The outbox keeps text messages that could not be sent because the connection
// was down, and sends them again, in order per conversation, once it is back.
// It lives in memory: a reload drops what is still waiting. Files are not kept
// (a message with attachments that fails keeps failing as before). The nonce
// each message carries makes a repeat harmless: if the first try had reached
// the server, the server returns that message instead of posting a second one.

const RETRY_START_MS = 5000
const RETRY_MAX_MS = 30000

/** The request never got an answer (offline, dropped, DNS): the message was possibly not delivered. */
export function isNetworkFailure(error: unknown): boolean {
  return isApiError(error) && error.status === 0 && error.code === 'NETWORK'
}

/** Failures that say "try again later" rather than "no". */
function isTemporary(error: unknown): boolean {
  return !isApiError(error) || isNetworkFailure(error) || error.status === 429 || error.status >= 500
}

/** Is a message of this conversation waiting? A new one then queues behind it, so the order holds. */
export function hasQueued({ cs }: Cx, channelId: string): boolean {
  return cs.outbox.value.some(p => p.channelId === channelId && p.status !== 'failed')
}

/** Keeps a message that could not be sent. It is tried again at the next pace, or at once when it only waits behind others. */
export function enqueue(cx: Cx, channelId: string, send: TextSend, behindOthers = false): PendingMessage {
  const { cs, authStore } = cx
  cs.outbox.value = [...cs.outbox.value, {
    nonce: send.nonce, channelId, userId: authStore.user?.id ?? '', parentId: send.parentId, replyToId: send.replyToId,
    content: send.content.trim(), createdAt: new Date().toISOString(), status: 'waiting', error: ''
  }]
  scheduleRetry(cx)
  if (behindOthers) void flushOutbox(cx)
  return cs.outbox.value[cs.outbox.value.length - 1]!
}

/** The pending messages of a conversation (a thread's replies when parentId is given, else the channel's own). */
export function pendingIn({ cs }: Cx, channelId: string, parentId: string | null): PendingMessage[] {
  return cs.outbox.value.filter(p => p.channelId === channelId && p.parentId === parentId)
}

function change(cx: Cx, nonce: string, patch: Partial<PendingMessage>) {
  cx.cs.outbox.value = cx.cs.outbox.value.map(p => p.nonce === nonce ? { ...p, ...patch } : p)
}

/** Forgets a pending message: it was sent (the echo carries its nonce), or the member took it back. */
export function settlePending(cx: Cx, nonce: string | undefined) {
  if (!nonce || !cx.cs.outbox.value.some(p => p.nonce === nonce)) return
  cx.cs.outbox.value = cx.cs.outbox.value.filter(p => p.nonce !== nonce)
}

/** Takes a pending message back; a request that is out may still land, and then shows as a normal message. */
export function cancelPending(cx: Cx, nonce: string) {
  settlePending(cx, nonce)
}

/** Changes the text of a message that is not sent yet. An emptied text cancels it. */
export function editPending(cx: Cx, nonce: string, content: string) {
  const text = content.trim()
  const entry = cx.cs.outbox.value.find(p => p.nonce === nonce)
  if (!entry || entry.status === 'sending') return
  if (!text) { cancelPending(cx, nonce); return }
  change(cx, nonce, { content: text })
}

/** A message the server refused can be tried again (after an edit, say). */
export function retryPending(cx: Cx, nonce: string) {
  change(cx, nonce, { status: 'waiting', error: '' })
  scheduleRetry(cx)
  void flushOutbox(cx)
}

/** Sends what is waiting, each conversation in the order it was written. */
export async function flushOutbox(cx: Cx) {
  const channels = new Set(cx.cs.outbox.value.filter(p => p.status === 'waiting').map(p => p.channelId))
  await Promise.all([...channels].map(id => flushConversation(cx, id)))
}

/** The connection is back: send at once, and start the retry pace anew. */
export function outboxReconnected(cx: Cx) {
  cx.cs.outboxDelay = RETRY_START_MS
  if (cx.cs.outbox.value.length) void flushOutbox(cx)
}

async function flushConversation(cx: Cx, channelId: string) {
  const { cs } = cx
  if (cs.outboxBusy.has(channelId)) return
  cs.outboxBusy.add(channelId)
  try {
    for (;;) {
      const next = cs.outbox.value.find(p => p.channelId === channelId && p.status === 'waiting')
      if (!next || (await deliver(cx, next)) === 'stop') return
    }
  } finally {
    cs.outboxBusy.delete(channelId)
    scheduleRetry(cx)
  }
}

/** Sends one; "stop" when the rest of the conversation has to wait for a later try. */
async function deliver(cx: Cx, entry: PendingMessage): Promise<'next' | 'stop'> {
  const { cs, authStore } = cx
  if (entry.userId !== authStore.user?.id) { settlePending(cx, entry.nonce); return 'next' }
  const community = cs.communityGeneration
  change(cx, entry.nonce, { status: 'sending' })
  // Edits made while it waited are in the stored entry, not in the copy this loop found.
  const current = cs.outbox.value.find(p => p.nonce === entry.nonce) ?? entry
  try {
    const msg = await postText(current.channelId, current)
    if (community !== cs.communityGeneration) return 'stop'
    settlePending(cx, current.nonce)
    landSent(cx, msg)
    return 'next'
  } catch (error) {
    if (community !== cs.communityGeneration) return 'stop'
    if (isTemporary(error)) {
      change(cx, entry.nonce, { status: 'waiting' })
      return 'stop'
    }
    change(cx, entry.nonce, { status: 'failed', error: caughtErrorMessage(error, t('chat.sendFailed')) })
    return 'next'
  }
}

/** While something waits, try again every few seconds, slower each time (the reconnect sends sooner). */
function scheduleRetry(cx: Cx) {
  const { cs } = cx
  if (cs.outboxTimer || !cs.outbox.value.some(p => p.status === 'waiting')) return
  cs.outboxTimer = setTimeout(() => {
    cs.outboxTimer = undefined
    cs.outboxDelay = Math.min(cs.outboxDelay * 2, RETRY_MAX_MS)
    void flushOutbox(cx)
    scheduleRetry(cx)
  }, cs.outboxDelay)
}

/** Drops everything that waits (another community or account). */
export function resetOutbox(cx: Cx) {
  clearTimeout(cx.cs.outboxTimer)
  cx.cs.outboxTimer = undefined
  cx.cs.outboxDelay = RETRY_START_MS
  cx.cs.outbox.value = []
  cx.cs.outboxBusy.clear()
}
