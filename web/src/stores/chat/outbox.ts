import { caughtErrorMessage, isApiError } from '../../lib/api'
import { isRecord } from '../../types/validation'
import { t } from '../../i18n'
import type { Message } from '../../types/domain'
import type { Cx } from './state'
import type { PendingMessage } from './outboxTypes'
import { landSent, postText, type TextSend } from './messagePost'
import { loadStoredOutbox, saveStoredOutbox } from './outboxStorage'

// The outbox keeps text messages that could not be sent because the connection
// was down, and sends them again, in order per conversation, once it is back.
// What waits is also kept in this browser (outboxStorage.ts) and sent again
// after a reload. Files are not kept (a message with attachments that fails
// keeps failing as before). The nonce each message carries makes a repeat
// harmless: if the first try had reached the server, the server returns that
// message instead of posting a second one (or says it was deleted meanwhile).

const RETRY_START_MS = 5000
const RETRY_MAX_MS = 30000
/** A request that gets no answer for this long counts as a lost connection. */
const SEND_TIMEOUT_MS = 15000
/** A server that keeps failing for one message is not "offline": after this many tries it is shown as not sent. */
const MAX_SERVER_ATTEMPTS = 5
const ECHO_MEMORY_MS = 60000

/** The request never got an answer (offline, dropped, DNS, or our own timeout): the message was possibly not delivered. */
export function isNetworkFailure(error: unknown): boolean {
  if (isRecord(error) && error['name'] === 'AbortError') return true
  return isApiError(error) && error.status === 0 && error.code === 'NETWORK'
}

/** The server cannot say yes or no right now: it failed (5xx) or the failure was not an API answer at all (unreadable body). */
function isServerTrouble(error: unknown): boolean {
  return !isApiError(error) || error.status >= 500
}

/** Replaces the pending list, and keeps this browser's copy of the signed-in account's messages in step. */
function commit(cx: Cx, next: PendingMessage[]) {
  cx.cs.outbox.value = next
  const uid = cx.authStore.user?.id
  if (uid) saveStoredOutbox(uid, next)
}

/** Is a message of this conversation waiting? A new one then queues behind it, so the order holds. */
export function hasQueued({ cs }: Cx, channelId: string): boolean {
  return cs.outbox.value.some(p => p.channelId === channelId && p.status !== 'failed')
}

/** Keeps a message that could not be sent. It is tried again at the next pace, or at once when it only waits behind others. */
export function enqueue(cx: Cx, channelId: string, send: TextSend, behindOthers = false): PendingMessage {
  const { cs, authStore } = cx
  commit(cx, [...cs.outbox.value, {
    nonce: send.nonce, channelId, userId: authStore.user?.id ?? '', parentId: send.parentId, replyToId: send.replyToId,
    content: send.content.trim(), createdAt: new Date().toISOString(), status: 'waiting', error: '',
    cause: 'offline', attempts: 0, attempted: !behindOthers, notBefore: 0
  }])
  scheduleRetry(cx)
  if (behindOthers) void flushOutbox(cx)
  return cs.outbox.value[cs.outbox.value.length - 1]!
}

/** The pending messages of a conversation (a thread's replies when parentId is given, else the channel's own). */
export function pendingIn({ cs }: Cx, channelId: string, parentId: string | null): PendingMessage[] {
  return cs.outbox.value.filter(p => p.channelId === channelId && p.parentId === parentId)
}

function change(cx: Cx, nonce: string, patch: Partial<PendingMessage>) {
  commit(cx, cx.cs.outbox.value.map(p => p.nonce === nonce ? { ...p, ...patch } : p))
}

/** Forgets a pending message: it was sent (the echo carries its nonce), or the member took it back. */
export function settlePending(cx: Cx, nonce: string | undefined) {
  if (!nonce || !cx.cs.outbox.value.some(p => p.nonce === nonce)) return
  commit(cx, cx.cs.outbox.value.filter(p => p.nonce !== nonce))
}

/** The author's own send came back as an event. It may be faster than the failure of the request that carried it: remember it, so that failure does not queue a second copy. */
export function noteEcho(cx: Cx, msg: Message) {
  if (!msg.nonce) return
  const { echoed } = cx.cs
  const now = Date.now()
  for (const [nonce, seen] of echoed) if (now - seen.at > ECHO_MEMORY_MS) echoed.delete(nonce)
  echoed.set(msg.nonce, { at: now, msg })
  settlePending(cx, msg.nonce)
}

/** The message of a send that the server had already announced, if it did. */
export function echoedMessage({ cs }: Cx, nonce: string): Message | undefined {
  const seen = cs.echoed.get(nonce)
  return seen && Date.now() - seen.at <= ECHO_MEMORY_MS ? seen.msg : undefined
}

/** Takes a pending message back; a request that is out may still land, and then shows as a normal message. */
export function cancelPending(cx: Cx, nonce: string) {
  settlePending(cx, nonce)
}

/** Changes the text of a message that no request with this text has left for. An emptied text cancels it. */
export function editPending(cx: Cx, nonce: string, content: string) {
  const text = content.trim()
  const entry = cx.cs.outbox.value.find(p => p.nonce === nonce)
  // After a try that may have arrived, the server would return the old text for this nonce.
  if (!entry || entry.status === 'sending' || entry.attempted) return
  if (!text) { cancelPending(cx, nonce); return }
  change(cx, nonce, { content: text })
}

/** A message the server refused can be tried again (after an edit, say). */
export function retryPending(cx: Cx, nonce: string) {
  change(cx, nonce, { status: 'waiting', error: '', attempts: 0, notBefore: 0 })
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
      // The server asked to wait (slow mode): everything behind it waits too.
      if (!next || next.notBefore > Date.now() || (await deliver(cx, next)) === 'stop') return
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
  change(cx, entry.nonce, { status: 'sending', attempted: true, notBefore: 0 })
  // Edits made while it waited are in the stored entry, not in the copy this loop found.
  const current = cs.outbox.value.find(p => p.nonce === entry.nonce) ?? entry
  // A request that hangs would keep the whole conversation's queue busy.
  const abort = new AbortController()
  const timer = setTimeout(() => abort.abort(), SEND_TIMEOUT_MS)
  try {
    const msg = await postText(current.channelId, current, abort.signal)
    if (community !== cs.communityGeneration) return 'stop'
    settlePending(cx, current.nonce)
    landSent(cx, msg)
    return 'next'
  } catch (error) {
    if (community !== cs.communityGeneration) return 'stop'
    return failedAttempt(cx, current, error)
  } finally {
    clearTimeout(timer)
  }
}

/** What an attempt that did not succeed means for the message: wait, drop it, or show it as not sent. */
function failedAttempt(cx: Cx, entry: PendingMessage, error: unknown): 'next' | 'stop' {
  const nonce = entry.nonce
  if (isNetworkFailure(error)) {
    change(cx, nonce, { status: 'waiting', cause: 'offline' })
    return 'stop'
  }
  if (isApiError(error) && error.code === 'MESSAGE_DELETED') {
    // Deleted after the first try reached the server (a moderator, say): it is not posted again, and there is nothing to tell.
    settlePending(cx, nonce)
    return 'next'
  }
  if (isApiError(error) && error.status === 429 && error.retryAfter !== null) {
    // Slow mode and rate limits: the composer waits the named time, so does the outbox. The message was not stored.
    change(cx, nonce, { status: 'waiting', cause: 'server', attempted: false, notBefore: Date.now() + error.retryAfter * 1000 })
    return 'stop'
  }
  const attempts = entry.attempts + 1
  const later = isServerTrouble(error) || (isApiError(error) && error.status === 429)
  if (later && attempts < MAX_SERVER_ATTEMPTS) {
    change(cx, nonce, { status: 'waiting', cause: 'server', attempts })
    return 'stop'
  }
  // A refusal says the message was not stored; after a server failure it might have been.
  const refused = !isServerTrouble(error)
  change(cx, nonce, { status: 'failed', error: caughtErrorMessage(error, t('chat.sendFailed')), attempts, attempted: refused ? false : entry.attempted })
  return 'next'
}

/** While something waits, try again every few seconds, slower each time (the reconnect sends sooner). */
function scheduleRetry(cx: Cx) {
  const { cs } = cx
  const waiting = cs.outbox.value.filter(p => p.status === 'waiting')
  if (cs.outboxTimer || !waiting.length) return
  // A wait the server named is honoured: each conversation sends in order, so only its first waiting message counts.
  const heads = new Map<string, number>()
  for (const p of waiting) if (!heads.has(p.channelId)) heads.set(p.channelId, p.notBefore)
  const due = Math.min(...heads.values()) - Date.now()
  cs.outboxTimer = setTimeout(() => {
    cs.outboxTimer = undefined
    cs.outboxDelay = Math.min(cs.outboxDelay * 2, RETRY_MAX_MS)
    void flushOutbox(cx)
    scheduleRetry(cx)
  }, due > 0 ? due + 250 : cs.outboxDelay)
}

/** Brings back what this account wrote before the page went away (a reload, a closed tab, an expired session). Once per sign-in. */
export function restoreOutbox(cx: Cx) {
  const { cs, authStore } = cx
  const uid = authStore.user?.id
  if (!uid || cs.outboxRestoredFor === uid) return
  cs.outboxRestoredFor = uid
  const known = new Set(cs.outbox.value.map(p => p.nonce))
  const back = loadStoredOutbox(uid).filter(p => !known.has(p.nonce))
  if (!back.length) return
  commit(cx, [...cs.outbox.value, ...back].sort((a, b) => a.createdAt.localeCompare(b.createdAt)))
  scheduleRetry(cx)
  void flushOutbox(cx)
}

/** Drops everything that waits in memory (another community or account); what this browser stored for the account stays for its next sign-in. */
export function resetOutbox(cx: Cx) {
  cx.cs.outboxRestoredFor = null
  cx.cs.echoed.clear()
  clearTimeout(cx.cs.outboxTimer)
  cx.cs.outboxTimer = undefined
  cx.cs.outboxDelay = RETRY_START_MS
  cx.cs.outbox.value = []
  cx.cs.outboxBusy.clear()
}
