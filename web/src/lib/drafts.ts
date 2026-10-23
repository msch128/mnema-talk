import { shallowRef } from 'vue'

// Unsent composer text per channel and thread, kept in this browser only
// (localStorage, one record per account) so it survives switching and
// reloading. It is never sent to the server and is removed on sign-out and
// account deletion. Every storage access tolerates blocked or full storage.
//
// Tabs agree on when a session ends: signing in stores a session token next to
// the drafts, signing out removes it and tells the other tabs (BroadcastChannel,
// with the storage event as fallback). A tab whose token is gone or replaced
// stops writing and forgets its drafts, so a stale tab cannot bring a draft
// back after sign-out.

/** Who a draft replies to: just enough to show the "Replying to" strip and send the id. */
export interface DraftReply { id: string; display_name?: string | undefined; username?: string | undefined }
export interface Draft { text: string; reply: DraftReply | null }

export const DRAFT_KEY_PREFIX = 'mnema.drafts.'
/** The server rejects longer messages anyway. */
export const MAX_DRAFT_TEXT = 4000
export const MAX_DRAFTS = 50

type Stored = Draft & { at: number }
type Items = Record<string, Stored>

/** Not under DRAFT_KEY_PREFIX: removing drafts must not remove the token by accident. */
export const DRAFT_SESSION_KEY = 'mnema.draftsession'
const CHANNEL_NAME = 'mnema.drafts'

const state = shallowRef<{ uid: string | null; items: Items }>({ uid: null, items: {} })

// This tab's session token (null until a sign-in or restore began one) and
// whether it learned that the session ended.
let session: string | null = null
let ended = false

/** The token in storage, null when there is none, undefined when storage is blocked. */
function storedSession(): string | null | undefined {
  try { return localStorage.getItem(DRAFT_SESSION_KEY) } catch { return undefined }
}

/** False once the session this tab belongs to was ended or replaced elsewhere. */
function sessionAlive(): boolean {
  if (ended) return false
  const stored = storedSession()
  if (stored !== undefined && stored !== (session ?? null)) endSession()
  return !ended
}

function endSession() {
  ended = true
  state.value = { uid: null, items: {} }
}

function newToken(uid: string) {
  return `${uid}:${Math.random().toString(36).slice(2)}${Date.now().toString(36)}`
}

function announceEnd() {
  try {
    const channel = new BroadcastChannel(CHANNEL_NAME)
    channel.postMessage('end')
    channel.close()
  } catch {
    // No BroadcastChannel: the storage event reaches the other tabs.
  }
}

function listen() {
  if (typeof window === 'undefined') return
  window.addEventListener('storage', event => {
    if (event.key === DRAFT_SESSION_KEY && session !== null && event.newValue !== session) endSession()
  })
  try {
    new BroadcastChannel(CHANNEL_NAME).addEventListener('message', () => {
      const stored = storedSession()
      if (session !== null && stored !== undefined && stored !== session) endSession()
    })
  } catch {
    // No BroadcastChannel: the storage event is enough.
  }
}
listen()

export const channelDraftKey = (channelId: string) => `c:${channelId}`
export const threadDraftKey = (threadId: string) => `t:${threadId}`

function storageKey(uid: string) { return DRAFT_KEY_PREFIX + uid }

function asReply(value: unknown): DraftReply | null {
  if (!value || typeof value !== 'object') return null
  const { id, display_name, username } = value as Record<string, unknown>
  if (typeof id !== 'string' || !id) return null
  return {
    id,
    ...(typeof display_name === 'string' ? { display_name: display_name.slice(0, 100) } : {}),
    ...(typeof username === 'string' ? { username: username.slice(0, 100) } : {})
  }
}

function asStored(value: unknown): Stored | null {
  if (!value || typeof value !== 'object') return null
  const { text, reply, at } = value as Record<string, unknown>
  const draft = { text: typeof text === 'string' ? text.slice(0, MAX_DRAFT_TEXT) : '', reply: asReply(reply), at: typeof at === 'number' ? at : 0 }
  return draft.text || draft.reply ? draft : null
}

function read(uid: string): Items {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(storageKey(uid)) || '{}')
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const items: Items = {}
    for (const [key, value] of Object.entries(parsed)) {
      const stored = asStored(value)
      if (stored) items[key] = stored
    }
    return items
  } catch {
    return {}
  }
}

function write(uid: string, items: Items) {
  try {
    if (Object.keys(items).length === 0) localStorage.removeItem(storageKey(uid))
    else localStorage.setItem(storageKey(uid), JSON.stringify(items))
  } catch {
    // Storage blocked or full: drafts then live only until the page closes.
  }
}

function current(uid: string): Items {
  if (!sessionAlive()) return {}
  if (state.value.uid !== uid) state.value = { uid, items: read(uid) }
  return state.value.items
}

/** The newest MAX_DRAFTS drafts. */
function trimmed(items: Items): Items {
  const keys = Object.keys(items)
  if (keys.length <= MAX_DRAFTS) return items
  keys.sort((a, b) => (items[b]?.at ?? 0) - (items[a]?.at ?? 0))
  return Object.fromEntries(keys.slice(0, MAX_DRAFTS).map(key => [key, items[key] as Stored]))
}

export function getDraft(uid: string, key: string): Draft | null {
  const stored = current(uid)[key]
  return stored ? { text: stored.text, reply: stored.reply } : null
}

/** Reactive: true while the account has an unsent draft under the key. */
export function hasDraft(uid: string | null | undefined, key: string): boolean {
  const snapshot = state.value
  return !!uid && snapshot.uid === uid && key in snapshot.items
}

/** Stores the draft, or forgets it when it holds no text and no reply. */
export function setDraft(uid: string, key: string, draft: Draft | null) {
  if (!sessionAlive()) return
  const items = { ...current(uid) }
  const text = (draft?.text ?? '').slice(0, MAX_DRAFT_TEXT)
  if (draft && (text.trim() || draft.reply)) items[key] = { text, reply: draft.reply, at: Date.now() }
  else delete items[key]
  const next = trimmed(items)
  state.value = { uid, items: next }
  write(uid, next)
}

/**
 * Forgets the drafts of channels that are gone (deleted, or no longer visible
 * to the account). Thread drafts are left alone: the channel list does not
 * say which threads exist.
 */
export function pruneChannelDrafts(uid: string, channelIds: ReadonlySet<string>) {
  if (!sessionAlive()) return
  const items = current(uid)
  const kept = Object.entries(items).filter(([key]) => !key.startsWith('c:') || channelIds.has(key.slice(2)))
  if (kept.length === Object.keys(items).length) return
  state.value = { uid, items: Object.fromEntries(kept) }
  write(uid, state.value.items)
}

/**
 * A sign-in or restored session: joins the account's draft session (the one
 * another tab already began, or a new one) and removes the drafts of any other
 * account on this device.
 */
export function beginDraftSession(uid: string) {
  try {
    const existing = localStorage.getItem(DRAFT_SESSION_KEY)
    session = existing?.startsWith(uid + ':') ? existing : newToken(uid)
    localStorage.setItem(DRAFT_SESSION_KEY, session)
  } catch {
    session = newToken(uid)
  }
  ended = false
  try {
    const own = storageKey(uid)
    const stale: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key?.startsWith(DRAFT_KEY_PREFIX) && key !== own) stale.push(key)
    }
    stale.forEach(key => localStorage.removeItem(key))
  } catch {
    // Storage blocked: nothing was kept.
  }
  if (state.value.uid !== null && state.value.uid !== uid) state.value = { uid: null, items: {} }
}

/** Sign-out and account deletion: every account's drafts leave this browser. */
export function clearAllDrafts() {
  state.value = { uid: null, items: {} }
  const hadSession = session !== null || !!storedSession()
  session = null
  ended = false
  try {
    localStorage.removeItem(DRAFT_SESSION_KEY)
    const stale: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key?.startsWith(DRAFT_KEY_PREFIX)) stale.push(key)
    }
    stale.forEach(key => localStorage.removeItem(key))
  } catch {
    // Storage blocked: nothing was kept.
  }
  if (hadSession) announceEnd()
}
