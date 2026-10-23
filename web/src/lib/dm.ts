// Direct messages as the interface names and orders them. Pure: the stores
// keep the conversations, this decides how one reads.
import type { Channel } from '../types/domain'
import type { ChatDMMember, ChatDirectMessage } from '../types/rest'
import { isApiError } from './api'
import { t } from '../i18n'

/** The most members a conversation takes, the caller included (the server's limit). */
export const DM_MAX_MEMBERS = 10

/** The other members of a conversation (all of them when it has no caller to leave out). */
export function dmPeers(dm: Pick<ChatDirectMessage, 'members'>, meId: string | null | undefined): ChatDMMember[] {
  return dm.members.filter(m => m.id !== meId)
}

const nameOf = (m: ChatDMMember) => m.display_name || m.username

/** What the conversation is called: the other member, or the others in a group. */
export function dmTitle(dm: Pick<ChatDirectMessage, 'members'>, meId: string | null | undefined): string {
  const peers = dmPeers(dm, meId)
  if (peers.length === 0) return t('dm.alone')
  return peers.map(nameOf).join(', ')
}

/** A conversation with one other member, shown with that member's avatar. */
export function dmPeer(dm: ChatDirectMessage, meId: string | null | undefined): ChatDMMember | null {
  const peers = dmPeers(dm, meId)
  return !dm.group && peers.length === 1 ? peers[0] ?? null : null
}

/** Newest activity first; a conversation without messages sorts by when it began. */
export function sortDms(list: readonly ChatDirectMessage[]): ChatDirectMessage[] {
  const stamp = (dm: ChatDirectMessage) => Date.parse(dm.last_message_at ?? dm.created_at)
  return [...list].sort((a, b) => stamp(b) - stamp(a))
}

/**
 * The conversation as the chat area's channel. A conversation is not a
 * channel, but its messages are read and written under its id like one, so the
 * open conversation takes a channel's place; dmStore tells them apart by id.
 */
export function dmAsChannel(dm: ChatDirectMessage, meId: string | null | undefined): Channel {
  return {
    id: dm.id,
    name: dmTitle(dm, meId),
    type: 'text',
    topic: '',
    category_id: null,
    created_at: dm.created_at,
    number: 0,
    slow_mode_seconds: 0,
    sort_order: 0,
    user_limit: 0
  }
}

const FAILED = { start: 'dm.startFailed', add: 'dm.addFailed', send: 'dm.sendFailed' } as const

/**
 * What to tell the member when the server refuses something about a
 * conversation. Every refusal about another member is the same 403 on purpose
 * (a block, their setting, a disabled account and an unknown id look alike),
 * so the words give nothing away either.
 */
export function dmErrorText(error: unknown, kind: 'start' | 'add' | 'send'): string {
  if (isApiError(error)) {
    if (error.status === 403 && error.code === 'FORBIDDEN') return t('dm.refused')
    if (error.status === 409) return t(kind === 'add' ? 'dm.groupFull' : 'dm.tooManyGroups')
    if (error.status === 429) return t('dm.slowDown')
  }
  return t(FAILED[kind])
}
