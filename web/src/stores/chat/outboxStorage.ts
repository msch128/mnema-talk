import { clampChars } from '../../shared/lib/textLength'
import type { PendingMessage } from './outboxTypes'

// Pending messages are kept per account in this browser (localStorage), so a
// reload, a closed tab, an expired session or a switch to another community
// does not lose what was written offline. The account id belongs to one
// community only, so another community never sees them. Signing out removes
// them (a shared device). Every access tolerates blocked or full storage.
//
// Each message has its own key (account and nonce), so tabs that are offline
// at the same time add to each other's messages instead of overwriting one
// shared list: a tab only ever writes or removes the messages it changed.

export const OUTBOX_KEY_PREFIX = 'mnema.outbox.'
const MAX_KEPT = 50
/** A request that gets no answer for this long counts as a lost connection. */
export const SEND_TIMEOUT_MS = 15000
/** Another tab's "sending" mark holds this long: its request has ended by then, answered or timed out. */
export const SEND_LEASE_MS = SEND_TIMEOUT_MS + 5000
const MAX_TEXT = 4000

/**
 * The server remembers the nonce of a send for 24 hours (nonceLifetime in
 * internal/chat/messages/nonce.go); after that a repeat posts the text again.
 * A message that may have arrived (possiblyDelivered) is therefore no longer
 * sent by itself an hour before that, and one that never reached the server is
 * only forgotten after a week.
 */
export const NONCE_LIFETIME_MS = 24 * 60 * 60 * 1000
export const REPEAT_SAFE_MS = NONCE_LIFETIME_MS - 60 * 60 * 1000
export const KEEP_MAX_MS = 7 * 24 * 60 * 60 * 1000

const accountPrefix = (uid: string) => OUTBOX_KEY_PREFIX + uid + '.'
const entryKey = (uid: string, nonce: string) => accountPrefix(uid) + nonce
const legacyKey = (uid: string) => OUTBOX_KEY_PREFIX + uid

function text(value: unknown): string | null {
  return typeof value === 'string' && value ? value : null
}

function asPending(value: unknown, uid: string): PendingMessage | null {
  if (!value || typeof value !== 'object') return null
  const v = value as Record<string, unknown>
  const nonce = text(v['nonce'])
  const channelId = text(v['channelId'])
  const content = text(v['content'])
  if (!nonce || !channelId || !content || v['userId'] !== uid) return null
  const interrupted = v['status'] === 'sending'
  const owner = text(v['owner'])
  const since = typeof v['sendingSince'] === 'number' ? v['sendingSince'] : null
  // A "sending" mark stays what it is (another tab's request may be out); outbox.ts decides whether it still holds.
  const leased = interrupted && owner !== null && since !== null
  return {
    nonce, channelId, userId: uid, content: clampChars(content, MAX_TEXT),
    parentId: text(v['parentId']), replyToId: text(v['replyToId']),
    createdAt: text(v['createdAt']) ?? new Date().toISOString(),
    // Whatever was in flight when the page went away is waiting again, and may have arrived.
    status: v['status'] === 'failed' ? 'failed' : leased ? 'sending' : 'waiting',
    ...(leased ? { owner, sendingSince: since } : {}),
    error: typeof v['error'] === 'string' ? v['error'].slice(0, 300) : '',
    cause: v['cause'] === 'server' ? 'server' : 'offline',
    attempts: typeof v['attempts'] === 'number' ? v['attempts'] : 0,
    // Entries from before this flag had `attempted`: that stays the cautious answer.
    possiblyDelivered: interrupted || (v['possiblyDelivered'] ?? v['attempted']) !== false,
    notBefore: typeof v['notBefore'] === 'number' ? v['notBefore'] : 0,
    ...(typeof v['lifetimeSeconds'] === 'number' && v['lifetimeSeconds'] > 0 ? { lifetimeSeconds: v['lifetimeSeconds'] } : {})
  }
}

/** The pending message another tab wrote under this storage key, or null when the key is not one of the account's messages or holds none. */
export function readStoredEntry(uid: string, key: string, value: string | null): PendingMessage | null {
  if (!value || !key.startsWith(accountPrefix(uid))) return null
  try {
    return asPending(JSON.parse(value), uid)
  } catch {
    return null
  }
}

/** The nonce a storage key of this account's messages stands for, null for any other key. */
export function nonceOfKey(uid: string, key: string | null): string | null {
  return key?.startsWith(accountPrefix(uid)) ? key.slice(accountPrefix(uid).length) : null
}

function storedKeys(prefix: string): string[] {
  const keys: string[] = []
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i)
    if (k?.startsWith(prefix)) keys.push(k)
  }
  return keys
}

export function loadStoredOutbox(uid: string): PendingMessage[] {
  try {
    const found: PendingMessage[] = []
    for (const k of storedKeys(accountPrefix(uid))) {
      const one = readStoredEntry(uid, k, localStorage.getItem(k))
      if (one) found.push(one)
    }
    found.push(...takeLegacyList(uid))
    found.sort((a, b) => a.createdAt.localeCompare(b.createdAt))
    // What is left out here must not stay in storage: it would come back at every load.
    found.slice(0, Math.max(0, found.length - MAX_KEPT)).forEach(p => removeStoredEntry(uid, p.nonce))
    return found.slice(-MAX_KEPT)
  } catch {
    return []
  }
}

/** Older versions kept the account's whole list under one key: its messages move to a key each, and the key goes. */
function takeLegacyList(uid: string): PendingMessage[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(legacyKey(uid)) || '[]')
    if (!Array.isArray(parsed)) return []
    const items = parsed.map(item => asPending(item, uid)).filter((p): p is PendingMessage => p !== null)
    items.forEach(saveStoredEntry)
    localStorage.removeItem(legacyKey(uid))
    return items
  } catch {
    // Unreadable: it can never be read, and must not hide the per-message entries.
    try { localStorage.removeItem(legacyKey(uid)) } catch { /* storage blocked */ }
    return []
  }
}

/** Stores one pending message under its own key. */
export function saveStoredEntry(entry: PendingMessage) {
  try {
    localStorage.setItem(entryKey(entry.userId, entry.nonce), JSON.stringify(entry))
  } catch {
    // Storage blocked or full: the message then lives only until the page closes.
  }
}

/** Forgets one pending message (sent, discarded or expired); the other messages of the account stay. */
export function removeStoredEntry(uid: string, nonce: string) {
  try {
    localStorage.removeItem(entryKey(uid, nonce))
  } catch {
    // Storage blocked: nothing was kept.
  }
}

/** Sign-out and account deletion: no account's pending messages stay on this device. */
export function clearStoredOutbox() {
  try {
    storedKeys(OUTBOX_KEY_PREFIX).forEach(k => localStorage.removeItem(k))
  } catch {
    // Storage blocked: nothing was kept.
  }
}
