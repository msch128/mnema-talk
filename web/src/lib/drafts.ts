import { shallowRef } from 'vue'

// Unsent composer text per channel and thread, kept in this browser only
// (localStorage, one record per account) so it survives switching and
// reloading. It is never sent to the server and is removed on sign-out and
// account deletion. Every storage access tolerates blocked or full storage.

/** Who a draft replies to: just enough to show the "Replying to" strip and send the id. */
export interface DraftReply { id: string; display_name?: string | undefined; username?: string | undefined }
export interface Draft { text: string; reply: DraftReply | null }

export const DRAFT_KEY_PREFIX = 'mnema.drafts.'
/** The server rejects longer messages anyway. */
export const MAX_DRAFT_TEXT = 4000
export const MAX_DRAFTS = 50

type Stored = Draft & { at: number }
type Items = Record<string, Stored>

const state = shallowRef<{ uid: string | null; items: Items }>({ uid: null, items: {} })

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
  const items = { ...current(uid) }
  const text = (draft?.text ?? '').slice(0, MAX_DRAFT_TEXT)
  if (draft && (text.trim() || draft.reply)) items[key] = { text, reply: draft.reply, at: Date.now() }
  else delete items[key]
  const next = trimmed(items)
  state.value = { uid, items: next }
  write(uid, next)
}

/** Sign-out and account deletion: every account's drafts leave this browser. */
export function clearAllDrafts() {
  state.value = { uid: null, items: {} }
  try {
    const stale: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key?.startsWith(DRAFT_KEY_PREFIX)) stale.push(key)
    }
    stale.forEach(key => localStorage.removeItem(key))
  } catch {
    // Storage blocked: nothing was kept.
  }
}
