import { shallowRef } from 'vue'
import { clampChars } from '../../shared/lib/textLength'

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

const endListeners = new Set<() => void>()

/** Calls back when this tab learns that its session ended or was replaced elsewhere (the unsent outbox follows the same rule). Returns the way to stop listening. */
export function onDraftSessionEnd(listener: () => void): () => void {
  endListeners.add(listener)
  return () => { endListeners.delete(listener) }
}

/** False once the session this tab belongs to was ended or replaced in another tab. */
export function draftSessionAlive(): boolean {
  return sessionAlive()
}

function endSession() {
  ended = true
  unsaved = false
  refused.value = false
  state.value = { uid: null, items: {} }
  endListeners.forEach(listener => listener())
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
  const draft = { text: typeof text === 'string' ? clampChars(text, MAX_DRAFT_TEXT) : '', reply: asReply(reply), at: typeof at === 'number' ? at : 0 }
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

// True while this tab holds drafts that storage does not: the last write failed
// (storage full or restricted) although reads still work.
let unsaved = false
// The same, readable by a watcher (`draftsAtRisk`).
const refused = shallowRef(false)

function write(uid: string, items: Items) {
  try {
    if (Object.keys(items).length === 0) localStorage.removeItem(storageKey(uid))
    else localStorage.setItem(storageKey(uid), JSON.stringify(items))
    unsaved = false
  } catch {
    // Storage blocked or full: drafts then live only until the page closes.
    unsaved = true
  }
  refused.value = unsaved
}

// Composers with typed text that is not written yet (a save waits for a pause in typing).
const pendingWrites = shallowRef<ReadonlySet<symbol>>(new Set())

/** A composer has text waiting to be written (`true`), or has written it (`false`). */
export function notePendingDraft(composer: symbol, pending: boolean) {
  if (pendingWrites.value.has(composer) === pending) return
  const next = new Set(pendingWrites.value)
  if (pending) next.add(composer)
  else next.delete(composer)
  pendingWrites.value = next
}

function current(uid: string): Items {
  if (!sessionAlive()) return {}
  if (state.value.uid !== uid) state.value = { uid, items: read(uid) }
  return state.value.items
}

/**
 * The account's drafts as storage holds them now, so a change made here never
 * drops (or revives) a draft another tab saved (or sent) since this tab last
 * looked. Without readable storage, or after a write storage refused, this tab's
 * own snapshot is the better base: storage holds older drafts than it does.
 */
function fresh(uid: string): Items {
  try {
    localStorage.getItem(storageKey(uid))
  } catch {
    return { ...current(uid) }
  }
  if (unsaved && state.value.uid === uid) return { ...current(uid) }
  return read(uid)
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

/**
 * Reactive: true while closing the page would lose a draft: text typed and
 * not written yet, or a draft storage refused. A draft in storage is safe: it
 * comes back after a reload.
 */
export function draftsAtRisk(): boolean {
  return refused.value || pendingWrites.value.size > 0
}

/** Stores the draft, or forgets it when it holds no text and no reply. */
export function setDraft(uid: string, key: string, draft: Draft | null) {
  if (!sessionAlive()) return
  const items = fresh(uid)
  const text = clampChars(draft?.text ?? '', MAX_DRAFT_TEXT)
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
  const items = fresh(uid)
  const kept = Object.entries(items).filter(([key]) => !key.startsWith('c:') || channelIds.has(key.slice(2)))
  if (kept.length === Object.keys(items).length) { state.value = { uid, items }; return }
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
  unsaved = false
  refused.value = false
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
