import type { PendingMessage } from './outboxTypes'

// Pending messages are kept per account in this browser (localStorage), so a
// reload, a closed tab, an expired session or a switch to another community
// does not lose what was written offline. The account id belongs to one
// community only, so another community never sees them. Signing out removes
// them (a shared device). Every access tolerates blocked or full storage.

export const OUTBOX_KEY_PREFIX = 'mnema.outbox.'
const MAX_KEPT = 50
const MAX_TEXT = 4000

const key = (uid: string) => OUTBOX_KEY_PREFIX + uid

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
  return {
    nonce, channelId, userId: uid, content: content.slice(0, MAX_TEXT),
    parentId: text(v['parentId']), replyToId: text(v['replyToId']),
    createdAt: text(v['createdAt']) ?? new Date().toISOString(),
    // Whatever was in flight when the page went away is waiting again.
    status: v['status'] === 'failed' ? 'failed' : 'waiting',
    error: typeof v['error'] === 'string' ? v['error'].slice(0, 300) : '',
    cause: v['cause'] === 'server' ? 'server' : 'offline',
    attempts: typeof v['attempts'] === 'number' ? v['attempts'] : 0,
    attempted: v['attempted'] !== false,
    notBefore: typeof v['notBefore'] === 'number' ? v['notBefore'] : 0
  }
}

export function loadStoredOutbox(uid: string): PendingMessage[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(key(uid)) || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.map(item => asPending(item, uid)).filter((p): p is PendingMessage => p !== null).slice(-MAX_KEPT)
  } catch {
    return []
  }
}

/** Stores the account's pending messages (the entries of other accounts are not written), or forgets them when there are none. */
export function saveStoredOutbox(uid: string, items: readonly PendingMessage[]) {
  const own = items.filter(p => p.userId === uid).slice(-MAX_KEPT)
  try {
    if (own.length === 0) localStorage.removeItem(key(uid))
    else localStorage.setItem(key(uid), JSON.stringify(own))
  } catch {
    // Storage blocked or full: the messages then live only until the page closes.
  }
}

/** Sign-out and account deletion: no account's pending messages stay on this device. */
export function clearStoredOutbox() {
  try {
    const stale: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (k?.startsWith(OUTBOX_KEY_PREFIX)) stale.push(k)
    }
    stale.forEach(k => localStorage.removeItem(k))
  } catch {
    // Storage blocked: nothing was kept.
  }
}
