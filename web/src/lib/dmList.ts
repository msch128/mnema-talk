// How the conversation list in the sidebar reads and orders. Pure: the store
// keeps the conversations, this decides what is shown and how a row is worded.
import type { Message } from '../types/domain'
import type { ChatDMLastMessage, ChatDirectMessage } from '../types/rest'
import { markdownToPlain } from './markdown'
import { dmPeers, nameOf } from './dm'
import { clockTimeOptions, startOfDay } from './time'

/** Most conversations the server lets a member favourite. */
export const DM_MAX_FAVOURITES = 25

const PREVIEW_CHARS = 120

/** Favourites in their saved order; the rest by activity, without the conversations the member closed. */
export function splitDms(dms: readonly ChatDirectMessage[], openId: string | null | undefined) {
  const favourites = dms
    .filter(dm => dm.favourite_position !== null)
    .sort((a, b) => (a.favourite_position ?? 0) - (b.favourite_position ?? 0))
  // The conversation on screen stays in the list even when it was closed, so the member never loses their place.
  const recent = dms.filter(dm => dm.favourite_position === null && (!dm.hidden || dm.id === openId))
  return { favourites, recent }
}

/** What a message shows in the list; the same shape the server sends for the latest one. */
export function lastMessageOf(msg: Message): ChatDMLastMessage {
  return {
    user_id: msg.user_id,
    text: (msg.content ?? '').slice(0, 200),
    has_attachment: (msg.attachments?.length ?? 0) > 0,
    has_poll: msg.poll !== undefined
  }
}

export type PreviewKind = 'none' | 'text' | 'attachment' | 'poll'

export interface DmPreview {
  kind: PreviewKind
  /** The start of the text, without its formatting (empty for an attachment or a poll). */
  text: string
  /** Who wrote it: null for the caller (shown as "You"), the member's name in a group, '' in a pair. */
  sender: 'me' | string
}

/** The last line of a row: who wrote, and what, in words that need no further translation but "You:". */
export function dmPreview(dm: ChatDirectMessage, meId: string | null | undefined): DmPreview {
  const last = dm.last_message
  if (!last) return { kind: 'none', text: '', sender: '' }
  const kind: PreviewKind = last.has_poll ? 'poll' : last.text.trim() ? 'text' : last.has_attachment ? 'attachment' : 'none'
  const text = kind === 'text' ? markdownToPlain(last.text).replace(/\s+/g, ' ').trim().slice(0, PREVIEW_CHARS) : ''
  if (last.user_id === meId) return { kind, text, sender: 'me' }
  // In a pair the other member is obvious; a group names who spoke.
  const author = dm.group ? dmPeers(dm, meId).find(m => m.id === last.user_id) : null
  return { kind, text, sender: author ? nameOf(author) : '' }
}

const DAY_MS = 86_400_000

/** A short time for a row: the clock today, the weekday this week, the date before that. */
export function dmClock(iso: string | null, locale: string, now = Date.now()): string {
  if (!iso) return ''
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ''
  const startOfToday = startOfDay(now)
  if (at.getTime() >= startOfToday) return at.toLocaleTimeString(locale, clockTimeOptions(locale))
  if (at.getTime() >= startOfToday - 6 * DAY_MS) return at.toLocaleDateString(locale, { weekday: 'short' })
  return at.toLocaleDateString(locale, { day: 'numeric', month: 'short' })
}

/** Conversations matching what was typed into "Find or start a conversation". */
export function matchDms(dms: readonly ChatDirectMessage[], meId: string | null | undefined, query: string): ChatDirectMessage[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return []
  return dms.filter(dm => dmPeers(dm, meId).some(m => `${m.display_name} ${m.username}`.toLowerCase().includes(needle)))
}
