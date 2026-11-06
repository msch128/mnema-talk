import { caughtErrorMessage, isApiError } from '../../lib/api'
import { isRecord } from '../../types/validation'
import { t } from '../../i18n'
import type { Message } from '../../types/domain'
import type { Cx } from './state'
import type { PendingMessage } from './outboxTypes'
import { draftSessionAlive, onDraftSessionEnd } from '../../lib/drafts'
import { showToast } from './messageWindow'
import { landSent, newNonce, postText, type TextSend } from './messagePost'
import { KEEP_MAX_MS, REPEAT_SAFE_MS, SEND_LEASE_MS, SEND_TIMEOUT_MS, loadStoredOutbox, nonceOfKey, readStoredEntry, removeStoredEntry, saveStoredEntry } from './outboxStorage'

// The outbox keeps text messages that could not be sent because the connection
// was down, and sends them again, in order per conversation, once it is back.
// What waits is also kept in this browser (outboxStorage.ts) and sent again
// after a reload. Files are not kept (a message with attachments that fails
// keeps failing as before). The nonce each message carries makes a repeat
// harmless: if the first try had reached the server, the server returns that
// message instead of posting a second one (or says it was deleted meanwhile).
// The server forgets a nonce after a day, so what may have arrived is not sent
// again by itself near that age (see REPEAT_SAFE_MS in outboxStorage.ts).
// Tabs share the stored messages: each tab writes only the messages it changed
// and follows the others through the storage event. Two tabs may try the same
// message; the nonce makes that safe, and the echo is shown once. To keep it
// to one sender in practice, a tab marks the message "sending" (with its tab id
// and the time) while its request is out; other tabs show it but leave it
// alone until that mark is older than a request can last. Adopting what
// another tab wrote never sends by itself: only a tab's own timer or the
// reconnect does, so tabs cannot hand a message back and forth.

const RETRY_START_MS = 5000
const RETRY_MAX_MS = 30000
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

/** Replaces the pending list, and keeps this browser's copy of the signed-in account's messages in step: only the messages that changed here are written or removed. */
function commit(cx: Cx, next: PendingMessage[]) {
  // The session ended in another tab (sign-out): nothing may be written back.
  if (!draftSessionAlive()) return
  const prev = cx.cs.outbox.value
  cx.cs.outbox.value = next
  const uid = cx.authStore.user?.id
  if (!uid) return
  const kept = new Set(next.map(p => p.nonce))
  for (const p of prev) if (p.userId === uid && !kept.has(p.nonce)) removeStoredEntry(uid, p.nonce)
  for (const p of next) if (p.userId === uid && !prev.includes(p)) saveStoredEntry(p)
}

const age = (p: PendingMessage, now: number) => {
  const created = Date.parse(p.createdAt)
  return Number.isNaN(created) ? 0 : now - created
}

/** Too old to be sent by itself: it may have arrived, and the server no longer recognises its nonce. */
export const unsafeToRepeat = (p: PendingMessage, now: number) => p.possiblyDelivered && age(p, now) > REPEAT_SAFE_MS

/** Marks as not sent what must not be repeated any more, and drops what is far too old. */
function expire(cx: Cx, now = Date.now()) {
  const list = cx.cs.outbox.value
  const expired = t('outbox.expired')
  // A failed one too: its error (a refusal, say) says nothing about the repeat it would now be.
  const lapsed = (p: PendingMessage) => unsafeToRepeat(p, now) && (p.status !== 'failed' || p.error !== expired)
  if (!list.some(p => age(p, now) > KEEP_MAX_MS || lapsed(p))) return
  commit(cx, list.filter(p => age(p, now) <= KEEP_MAX_MS)
    .map(p => lapsed(p) ? { ...p, status: 'failed' as const, error: expired, ...released } : p))
}

const released = { owner: undefined, sendingSince: undefined }

/** Another tab's "sending" mark holds while its request can still be out. After that, the message waits again and may have arrived. Marks of this tab are its own requests. */
function lapse({ cs }: Cx, p: PendingMessage, now: number): PendingMessage {
  if (p.status !== 'sending' || p.owner === cs.tabId || now - (p.sendingSince ?? 0) < SEND_LEASE_MS) return p
  return { ...p, status: 'waiting', possiblyDelivered: true, ...released }
}

function reclaimLapsed(cx: Cx) {
  const now = Date.now()
  const list = cx.cs.outbox.value
  if (list.some(p => lapse(cx, p, now) !== p)) commit(cx, list.map(p => lapse(cx, p, now)))
}

/** What this tab may send, or must wait for another tab's request on. */
const isDue = ({ cs }: Cx, p: PendingMessage) => p.status === 'waiting' || (p.status === 'sending' && p.owner !== cs.tabId)

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
    cause: 'offline', attempts: 0, possiblyDelivered: !behindOthers, notBefore: 0
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

/** The editor is open on this message: this tab's queue holds the conversation back, so it cannot send the old text meanwhile. The hold is per tab: another tab may still send the message, and saving then says so. */
export function beginEditPending(cx: Cx, nonce: string) {
  cx.cs.outboxEditing.add(nonce)
}

/** The editor closed without saving: the held conversation goes on. */
export function endEditPending(cx: Cx, nonce: string) {
  if (cx.cs.outboxEditing.delete(nonce)) void flushOutbox(cx)
}

/** Changes the text of a message that no request with this text has left for. An emptied text cancels it. Saving also ends the hold begun by beginEditPending. */
export function editPending(cx: Cx, nonce: string, content: string) {
  try {
    saveEdit(cx, nonce, content)
  } finally {
    endEditPending(cx, nonce)
  }
}

function saveEdit(cx: Cx, nonce: string, content: string) {
  const text = content.trim()
  const entry = cx.cs.outbox.value.find(p => p.nonce === nonce)
  // After a try that may have arrived, the server would return the old text for this nonce.
  // Sent from another tab meanwhile: say so instead of dropping the change silently.
  if (!entry) { showToast(t('outbox.editTooLate')); return }
  if (entry.status === 'sending' || entry.possiblyDelivered) return
  if (!text) { cancelPending(cx, nonce); return }
  change(cx, nonce, { content: text })
}

/** A message the server refused can be tried again (after an edit, say). */
export function retryPending(cx: Cx, nonce: string) {
  const entry = cx.cs.outbox.value.find(p => p.nonce === nonce)
  if (entry && unsafeToRepeat(entry, Date.now())) {
    // The server forgot this nonce, so this is a new send, which is what the member asks for.
    const fresh = { ...entry, nonce: newNonce(), createdAt: new Date().toISOString(), possiblyDelivered: false }
    commit(cx, cx.cs.outbox.value.map(p => p === entry ? fresh : p))
    nonce = fresh.nonce
  }
  change(cx, nonce, { status: 'waiting', error: '', attempts: 0, notBefore: 0, ...released })
  scheduleRetry(cx)
  void flushOutbox(cx)
}

/** Sends what is waiting, each conversation in the order it was written. */
export async function flushOutbox(cx: Cx) {
  reclaimLapsed(cx)
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
      const next = cs.outbox.value.find(p => p.channelId === channelId && p.status !== 'failed')
      // Another tab's request is out for the first one: the order holds, so this tab waits. The server asked to wait (slow mode): everything behind it waits too.
      if (next?.status !== 'waiting' || cs.outboxEditing.has(next.nonce) || next.notBefore > Date.now() || (await deliver(cx, next)) === 'stop') return
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
  expire(cx)
  if (!cs.outbox.value.some(p => p.nonce === entry.nonce && p.status === 'waiting')) return 'next'
  change(cx, entry.nonce, { status: 'sending', notBefore: 0, owner: cs.tabId, sendingSince: Date.now() })
  // Edits made while it waited are in the stored entry, not in the copy this loop found.
  const current = cs.outbox.value.find(p => p.nonce === entry.nonce) ?? entry
  // A request that hangs would keep the whole conversation's queue busy.
  const abort = new AbortController()
  const timer = setTimeout(() => abort.abort(), SEND_TIMEOUT_MS)
  try {
    let replayed = false
    const msg = await postText(current.channelId, current, abort.signal, status => { replayed = status === 200 })
    if (community !== cs.communityGeneration) return 'stop'
    settlePending(cx, current.nonce)
    landSent(cx, msg, replayed)
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
  // Cut off, unanswered or failed without a usable answer: the server may have stored it. A refusal (4xx) says nothing about earlier tries.
  const possiblyDelivered = entry.possiblyDelivered || isNetworkFailure(error) || isServerTrouble(error)
  if (isNetworkFailure(error)) {
    change(cx, nonce, { status: 'waiting', cause: 'offline', possiblyDelivered, ...released })
    return 'stop'
  }
  if (isApiError(error) && error.code === 'MESSAGE_DELETED') {
    // Deleted after the first try reached the server (a moderator, say): it is not posted again, and there is nothing to tell.
    settlePending(cx, nonce)
    return 'next'
  }
  if (isApiError(error) && error.status === 429 && error.retryAfter !== null) {
    // Slow mode and rate limits: the composer waits the named time, so does the outbox. The message was not stored.
    change(cx, nonce, { status: 'waiting', cause: 'server', possiblyDelivered, notBefore: Date.now() + error.retryAfter * 1000, ...released })
    return 'stop'
  }
  const attempts = entry.attempts + 1
  const later = isServerTrouble(error) || (isApiError(error) && error.status === 429)
  if (later && attempts < MAX_SERVER_ATTEMPTS) {
    change(cx, nonce, { status: 'waiting', cause: 'server', attempts, possiblyDelivered, ...released })
    return 'stop'
  }
  change(cx, nonce, { status: 'failed', error: caughtErrorMessage(error, t('chat.sendFailed')), attempts, possiblyDelivered, ...released })
  return 'next'
}

/** While something waits, try again every few seconds, slower each time (the reconnect sends sooner). */
function scheduleRetry(cx: Cx) {
  const { cs } = cx
  const waiting = cs.outbox.value.filter(p => isDue(cx, p))
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
  followOtherTabs(cx)
  const known = new Set(cs.outbox.value.map(p => p.nonce))
  const now = Date.now()
  const back = loadStoredOutbox(uid).filter(p => !known.has(p.nonce)).map(p => lapse(cx, p, now))
  if (!back.length) return
  commit(cx, [...cs.outbox.value, ...back].sort(byCreation))
  expire(cx)
  scheduleRetry(cx)
  void flushOutbox(cx)
}

const byCreation = (a: PendingMessage, b: PendingMessage) => a.createdAt.localeCompare(b.createdAt)

/** What another tab does to the account's stored messages shows here too, and a sign-out elsewhere empties this tab's list. */
function followOtherTabs(cx: Cx) {
  const { cs } = cx
  cs.outboxStop?.()
  const stopEnd = onDraftSessionEnd(() => { cs.outbox.value = [] })
  const onStorage = (event: StorageEvent) => adoptFromStorage(cx, event)
  window.addEventListener('storage', onStorage)
  cs.outboxStop = () => { stopEnd(); window.removeEventListener('storage', onStorage) }
}

function adoptFromStorage(cx: Cx, event: StorageEvent) {
  const { cs } = cx
  const uid = cx.authStore.user?.id
  const nonce = uid ? nonceOfKey(uid, event.key) : null
  if (!uid || !nonce || !draftSessionAlive()) return
  const incoming = readStoredEntry(uid, event.key ?? '', event.newValue)
  // This tab's own request for it is out: its answer settles it (another tab's mark does not hold back that tab's outcome). A removal elsewhere still counts.
  if (incoming && cs.outbox.value.some(p => p.nonce === nonce && p.status === 'sending' && p.owner === cs.tabId)) return
  const rest = cs.outbox.value.filter(p => p.nonce !== nonce)
  cs.outbox.value = incoming ? [...rest, lapse(cx, incoming, Date.now())].sort(byCreation) : rest
  // Only this tab's own pace sends it: sending now would hand it back and forth between tabs that are offline.
  if (incoming) scheduleRetry(cx)
}

/** Drops everything that waits in memory (another community or account); what this browser stored for the account stays for its next sign-in. */
export function resetOutbox(cx: Cx) {
  cx.cs.outboxRestoredFor = null
  cx.cs.echoed.clear()
  cx.cs.outboxStop?.()
  cx.cs.outboxStop = undefined
  clearTimeout(cx.cs.outboxTimer)
  cx.cs.outboxTimer = undefined
  cx.cs.outboxDelay = RETRY_START_MS
  cx.cs.outbox.value = []
  cx.cs.outboxBusy.clear()
  cx.cs.outboxEditing.clear()
}
