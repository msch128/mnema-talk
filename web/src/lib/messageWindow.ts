// Pure windowing logic for a channel's root-message history.
//
// The client never holds the whole history: it keeps a contiguous slice
// ("window") of at most WINDOW_CAP messages, oldest first, plus flags telling
// whether older/newer messages exist on the server. Every function returns a
// new window object and never mutates its input, so the store can swap the
// window atomically and the logic stays testable without a DOM.

export const PAGE_SIZE = 50
export const WINDOW_CAP = 250
import type { Message } from '../types/domain'
type IdentifiedMessage = { id: string }
export interface MessageWindow<T extends IdentifiedMessage = Message> { messages: T[]; hasMoreBefore: boolean; hasMoreAfter: boolean }
interface PageOptions { anchorId?: string; limit?: number; cap?: number }
export type LiveAppend<T extends IdentifiedMessage> = { window: MessageWindow<T>; status: 'appended' | 'duplicate' | 'gap' }

/** @typedef {{ messages: Array<{id: string}>, hasMoreBefore: boolean, hasMoreAfter: boolean }} MessageWindow */

/** @returns {MessageWindow} */
export function emptyWindow<T extends IdentifiedMessage = Message>(): MessageWindow<T> {
  return { messages: [], hasMoreBefore: false, hasMoreAfter: false }
}

function isValid<T extends IdentifiedMessage>(msg: T | null | undefined): msg is T {
  return msg != null && msg.id != null
}

/** Drops invalid entries and duplicate ids, keeping the first occurrence. */
export function dedupe<T extends IdentifiedMessage>(list: Array<T | null | undefined> | null | undefined): T[] {
  const seen = new Set<string>()
  const out: T[] = []
  for (const msg of list || []) {
    if (!isValid(msg) || seen.has(msg.id)) continue
    seen.add(msg.id)
    out.push(msg)
  }
  return out
}

function freshOnly<T extends IdentifiedMessage>(existing: T[], page: Array<T | null | undefined>): T[] {
  const ids = new Set(existing.map(m => m.id))
  return dedupe(page).filter(m => !ids.has(m.id))
}

/** Window built from the newest page (no anchor). */
export function fromLatest<T extends IdentifiedMessage = Message>(page: T[] | null | undefined, limit = PAGE_SIZE): MessageWindow<T> {
  const list = Array.isArray(page) ? page : []
  const messages = dedupe(list)
  return { messages, hasMoreBefore: list.length >= limit, hasMoreAfter: false }
}

/**
 * Brings a window that already reaches the newest end up to date with a fresh
 * newest page, keeping the older history the reader has paged in. Messages of
 * the page replace their stale copies, ones the page no longer holds are
 * dropped, new ones are appended. When the page does not overlap the window
 * (more was missed than a page holds) nothing can be joined safely and the
 * window becomes the page (`replaced`).
 */
export function mergeLatest<T extends IdentifiedMessage>(win: MessageWindow<T>, page: T[] | null | undefined, { limit = PAGE_SIZE, cap = WINDOW_CAP }: Pick<PageOptions, 'limit' | 'cap'> = {}): { window: MessageWindow<T>; replaced: boolean } {
  const fresh = fromLatest(page, limit)
  const byId = new Map(fresh.messages.map(m => [m.id, m]))
  const overlap = win.messages.findIndex(m => byId.has(m.id))
  if (overlap < 0) return { window: fresh, replaced: true }
  const kept = win.messages.slice(0, overlap).concat(win.messages.slice(overlap).filter(m => byId.has(m.id)).map(m => byId.get(m.id) as T))
  const known = new Set(kept.map(m => m.id))
  let messages = kept.concat(fresh.messages.filter(m => !known.has(m.id)))
  let hasMoreBefore = win.hasMoreBefore
  if (messages.length > cap) {
    messages = messages.slice(messages.length - cap)
    hasMoreBefore = true
  }
  return { window: { messages, hasMoreBefore, hasMoreAfter: false }, replaced: false }
}

/**
 * Window built from an `around=<anchorId>` page. The server returns up to
 * floor(limit/2)+1 messages older than or equal to the anchor and fills the
 * rest of the page with newer ones; a side that came back short has reached
 * its end of the channel.
 */
export function fromAround<T extends IdentifiedMessage = Message>(page: T[] | null | undefined, anchorId: string, limit = PAGE_SIZE): MessageWindow<T> {
  const messages = dedupe(Array.isArray(page) ? page : [])
  const idx = messages.findIndex(m => m.id === anchorId)
  if (idx === -1) {
    // Should not happen; stay conservative so paging can still fill gaps.
    const any = messages.length > 0
    return { messages, hasMoreBefore: any, hasMoreAfter: any }
  }
  const olderCount = idx + 1
  const newerCount = messages.length - olderCount
  return {
    messages,
    hasMoreBefore: olderCount >= Math.floor(limit / 2) + 1,
    hasMoreAfter: newerCount >= limit - olderCount
  }
}

/**
 * Prepends an older page (`before=<anchorId>`). If the window changed while
 * the request was in flight (its oldest message is no longer `anchorId`), the
 * page is discarded to avoid creating a gap. Trims from the bottom when the
 * cap is exceeded and then marks that newer messages exist.
 */
export function prependOlder<T extends IdentifiedMessage>(win: MessageWindow<T>, page: T[] | null | undefined, { anchorId, limit = PAGE_SIZE, cap = WINDOW_CAP }: PageOptions = {}): MessageWindow<T> {
  if (anchorId !== undefined && win.messages[0]?.id !== anchorId) return win
  const list = Array.isArray(page) ? page : []
  const fresh = freshOnly(win.messages, list)
  let messages = fresh.concat(win.messages)
  let hasMoreAfter = win.hasMoreAfter
  if (messages.length > cap) {
    messages = messages.slice(0, cap)
    hasMoreAfter = true
  }
  // A full page with nothing new cannot advance; stop instead of looping.
  return { messages, hasMoreBefore: list.length >= limit && fresh.length > 0, hasMoreAfter }
}

/**
 * Appends a newer page (`after=<anchorId>`), the mirror image of prependOlder:
 * stale responses are discarded and the window is trimmed from the top.
 */
export function appendNewer<T extends IdentifiedMessage>(win: MessageWindow<T>, page: T[] | null | undefined, { anchorId, limit = PAGE_SIZE, cap = WINDOW_CAP }: PageOptions = {}): MessageWindow<T> {
  if (anchorId !== undefined && win.messages[win.messages.length - 1]?.id !== anchorId) return win
  const list = Array.isArray(page) ? page : []
  const fresh = freshOnly(win.messages, list)
  let messages = win.messages.concat(fresh)
  let hasMoreBefore = win.hasMoreBefore
  if (messages.length > cap) {
    messages = messages.slice(messages.length - cap)
    hasMoreBefore = true
  }
  return { messages, hasMoreBefore, hasMoreAfter: list.length >= limit && fresh.length > 0 }
}

/**
 * Adds a live (WebSocket/HTTP echo) message at the newest end.
 * status: 'appended' | 'duplicate' | 'gap' (window does not reach the newest
 * message, so appending would skip messages in between).
 */
export function appendLive<T extends IdentifiedMessage>(win: MessageWindow<T>, msg: T | null | undefined, { cap = WINDOW_CAP }: Pick<PageOptions, 'cap'> = {}): LiveAppend<T> {
  if (!isValid(msg)) return { window: win, status: 'duplicate' }
  if (win.messages.some(m => m.id === msg.id)) return { window: win, status: 'duplicate' }
  if (win.hasMoreAfter) return { window: win, status: 'gap' }
  let messages = win.messages.concat([msg])
  let hasMoreBefore = win.hasMoreBefore
  if (messages.length > cap) {
    messages = messages.slice(messages.length - cap)
    hasMoreBefore = true
  }
  return { window: { messages, hasMoreBefore, hasMoreAfter: false }, status: 'appended' }
}

/** Removes a message by id (deletions). */
export function removeMessage<T extends IdentifiedMessage>(win: MessageWindow<T>, id: string): MessageWindow<T> {
  if (!win.messages.some(m => m.id === id)) return win
  return { ...win, messages: win.messages.filter(m => m.id !== id) }
}
