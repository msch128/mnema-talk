// Direct message conversations: the list, starting, adding, leaving, and the
// events that change them. A conversation is not a channel, but its messages
// are read and written under its id with the channel routes, so the open
// conversation sits in the chat area's channel slot (see lib/dm).
import type { Channel, Message, ReadState } from '../../types/domain'
import { decodeChatDirectMessage, isChatDirectMessage, type ChatDirectMessage, type ChatDMMember } from '../../types/rest'
import { arrayDecoder } from '../../types/validation'
import { api } from '../../lib/api'
import { dmAsChannel, sortDms } from '../../lib/dm'
import { emptyWindow } from '../../lib/messageWindow'
import { navigate } from '../../lib/router'
import { useServerFeaturesStore } from '../serverFeatures'
import { assertCommunity, type Cx } from './state'
import { selectChannel } from './channels'
import { resetWindow, setWindow } from './messageWindow'
import { markActiveChannelReadIfReading } from './readState'
import { closeThread } from './threads'

const decodeDms = arrayDecoder('direct messages', isChatDirectMessage)

export function dmById(cx: Cx, id: string | null | undefined): ChatDirectMessage | null {
  return id ? cx.cs.dms.value.find(d => d.id === id) ?? null : null
}

export function isDm(cx: Cx, id: string | null | undefined): boolean {
  return dmById(cx, id) !== null
}

/** The conversation in a channel's shape, or null when the id is not one of the caller's conversations. */
export function dmChannel(cx: Cx, id: string | null | undefined): Channel | null {
  const dm = dmById(cx, id)
  return dm ? dmAsChannel(dm, cx.authStore.user?.id) : null
}

/**
 * What the sidebar counts for a conversation. Every message of somebody else
 * in a conversation is addressed to the caller, so each unread one also counts
 * as a mention (the badge, the tab title and the notification follow).
 */
function readStateOf(dm: ChatDirectMessage): ReadState {
  return {
    channel_id: dm.id,
    unread_count: dm.unread_count,
    mention_count: dm.unread_count,
    last_read_at: dm.last_read_at,
    notify_level: dm.notify_level === 'inherit' ? 'all' : dm.notify_level
  }
}

/** The open conversation takes the new title when its members changed. */
function refreshActiveDm(cx: Cx) {
  const { cs } = cx
  const active = cs.activeChannel.value
  const next = dmChannel(cx, active?.id)
  if (active && next && next.name !== active.name) cs.activeChannel.value = next
}

/** fresh: the counts come from a list the server just answered; otherwise only conversations new to this client get theirs. */
function setDms(cx: Cx, list: readonly ChatDirectMessage[], fresh = false) {
  const { cs } = cx
  const next = sortDms(list)
  const states = { ...cs.readStates.value }
  for (const gone of cs.dms.value) if (!next.some(d => d.id === gone.id)) delete states[gone.id]
  for (const dm of next) if (fresh || !states[dm.id]) states[dm.id] = readStateOf(dm)
  cs.dms.value = next
  cs.readStates.value = states
  refreshActiveDm(cx)
}

export async function fetchDms(cx: Cx) {
  const { cs } = cx
  if (!useServerFeaturesStore().dm) return
  const community = cs.communityGeneration
  const seq = ++cs.dmsFetchSeq
  try {
    const list = await api('/api/dms', { decode: decodeDms })
    if (seq !== cs.dmsFetchSeq || community !== cs.communityGeneration) return
    setDms(cx, list, true)
    markActiveChannelReadIfReading(cx)
  } catch (error) {
    console.warn('Failed to fetch direct messages:', error)
  } finally {
    if (seq === cs.dmsFetchSeq) cs.dmsLoaded.value = true
  }
}

function upsertDm(cx: Cx, dm: ChatDirectMessage) {
  const rest = cx.cs.dms.value.filter(d => d.id !== dm.id)
  setDms(cx, [...rest, dm])
}

/** Opens a conversation in the chat area. */
export async function openDm(cx: Cx, id: string) {
  const channel = dmChannel(cx, id)
  if (!channel) return
  cx.voiceStore.activeView = 'chat'
  await selectChannel(cx, channel)
}

/** One member for a conversation of two (the existing one if there is one), several for a new group. */
export async function startDm(cx: Cx, userIds: string[]): Promise<ChatDirectMessage> {
  const community = cx.cs.communityGeneration
  const dm = await api('/api/dms', { method: 'POST', json: { user_ids: userIds }, decode: decodeChatDirectMessage })
  assertCommunity(cx, community)
  upsertDm(cx, dm)
  return dm
}

export async function addDmMember(cx: Cx, dmId: string, userId: string): Promise<ChatDirectMessage> {
  const community = cx.cs.communityGeneration
  const dm = await api(`/api/dms/${dmId}/members`, { method: 'POST', json: { user_id: userId }, decode: decodeChatDirectMessage })
  assertCommunity(cx, community)
  upsertDm(cx, dm)
  return dm
}

/** The caller is out of the conversation (left here or on another device): it leaves the list, and the screen if it was open. */
function dropDm(cx: Cx, id: string) {
  const { cs } = cx
  const wasOpen = cs.activeChannel.value?.id === id
  const states = { ...cs.readStates.value }
  delete states[id]
  cs.readStates.value = states
  cs.dms.value = cs.dms.value.filter(d => d.id !== id)
  if (!wasOpen) return
  cs.activeChannel.value = null
  closeThread(cx)
  resetWindow(cx)
  setWindow(cx, emptyWindow())
  navigate('/', { replace: true })
}

export async function leaveDm(cx: Cx, id: string) {
  const community = cx.cs.communityGeneration
  await api(`/api/dms/${id}/members/@me`, { method: 'DELETE' })
  assertCommunity(cx, community)
  dropDm(cx, id)
}

function withMembers(cx: Cx, id: string, change: (members: ChatDMMember[]) => ChatDMMember[]) {
  const { cs } = cx
  cs.dms.value = cs.dms.value.map(d => (d.id === id ? { ...d, members: change(d.members) } : d))
  refreshActiveDm(cx)
}

/** dm_created: the caller started a conversation elsewhere, or somebody started one with them. */
export function onDmCreated(cx: Cx, dm: ChatDirectMessage) {
  upsertDm(cx, dm)
}

export function onDmMemberAdded(cx: Cx, p: { dm_id: string; member: ChatDMMember }) {
  if (!isDm(cx, p.dm_id)) return
  withMembers(cx, p.dm_id, members => (members.some(m => m.id === p.member.id) ? members : [...members, p.member]))
}

export function onDmMemberRemoved(cx: Cx, p: { dm_id: string; member: ChatDMMember }) {
  if (p.member.id === cx.authStore.user?.id) dropDm(cx, p.dm_id)
  else withMembers(cx, p.dm_id, members => members.filter(m => m.id !== p.member.id))
}

/**
 * A message arrived: its conversation moves to the top. One this client does
 * not know (the creation event is still on its way, or was missed) is read again.
 */
export function noteDmMessage(cx: Cx, msg: Message) {
  const { cs } = cx
  if (!dmById(cx, msg.channel_id)) {
    const known = cs.allChannels.value.some(c => c.id === msg.channel_id)
    if (!known && msg.channel_id) void fetchDms(cx)
    return
  }
  cs.dms.value = sortDms(cs.dms.value.map(d => (d.id === msg.channel_id ? { ...d, last_message_at: msg.created_at } : d)))
}
