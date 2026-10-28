// Direct message conversations: the list, starting, adding, leaving, and the
// events that change them. A conversation is not a channel, but its messages
// are read and written under its id with the channel routes, so the open
// conversation sits in the chat area's channel slot (see lib/dm).
import type { Channel, Message, ReadState } from '../../types/domain'
import { decodeChatDMSettings, decodeChatDirectMessage, isChatDirectMessage, type ChatDirectMessage, type ChatDMMember } from '../../types/rest'
import { arrayDecoder } from '../../types/validation'
import { api } from '../../lib/api'
import { dmAsChannel, sortDms } from '../../lib/dm'
import { lastMessageOf } from '../../lib/dmList'
import { emptyWindow } from '../../lib/messageWindow'
import { navigate } from '../../lib/router'
import { useBlocksStore } from '../blocks'
import { useServerFeaturesStore } from '../serverFeatures'
import { watch } from 'vue'
import { assertCommunity, type Cx } from './state'
import { selectChannel } from './channels'
import { resetWindow, setWindow } from './messageWindow'
import { markActiveChannelReadIfReading } from './readState'
import { closeThread } from './threads'

const decodeDms = arrayDecoder('direct messages', isChatDirectMessage)

/**
 * Resolves when the conversation list has been read (at once on a server
 * without direct messages), or after ms. The list is read once the socket is
 * open, so no creation event can fall between the list and the socket; a first
 * route into a conversation waits for it here.
 */
export function waitForDms(cx: Cx, ms: number): Promise<void> {
  const { cs } = cx
  if (!useServerFeaturesStore().dm || cs.dmsLoaded.value) return Promise.resolve()
  return new Promise<void>(resolve => {
    const stop = watch(cs.dmsLoaded, loaded => { if (loaded) done() })
    const timer = setTimeout(done, ms)
    function done() {
      stop()
      clearTimeout(timer)
      resolve()
    }
  })
}

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

/** What the sidebar counts for a conversation (mentions are only the real ones, so the inbox badge stays about mentions). */
function readStateOf(dm: ChatDirectMessage): ReadState {
  return {
    channel_id: dm.id,
    unread_count: dm.unread_count,
    mention_count: dm.mention_count,
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
export function setDms(cx: Cx, list: readonly ChatDirectMessage[], fresh = false) {
  const { cs } = cx
  const next = sortDms(list)
  const states = { ...cs.readStates.value }
  for (const gone of cs.dms.value) if (!next.some(d => d.id === gone.id)) delete states[gone.id]
  for (const dm of next) if (fresh || !states[dm.id]) states[dm.id] = readStateOf(dm)
  cs.dms.value = next
  cs.readStates.value = states
  refreshActiveDm(cx)
}

export async function fetchDmSettings(cx: Cx) {
  const { cs } = cx
  if (!useServerFeaturesStore().dmV2) return
  const community = cs.communityGeneration
  try {
    const settings = await api('/api/users/me/dm-settings', { decode: decodeChatDMSettings })
    if (community !== cs.communityGeneration) return
    cs.dmSettings.value = settings
    cs.dmSettingsLoaded.value = true
  } catch (error) {
    console.warn('Failed to read the direct message settings:', error)
  }
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
    // Only a list the server answered counts as loaded; draft pruning relies on it.
    cs.dmsLoaded.value = true
    void fetchDmSettings(cx)
    markActiveChannelReadIfReading(cx)
  } catch (error) {
    console.warn('Failed to fetch direct messages:', error)
  }
}

/** fresh: the row is the caller's own, just read from the server, so its counts replace what is held. */
export function upsertDm(cx: Cx, dm: ChatDirectMessage, fresh = false) {
  const rest = cx.cs.dms.value.filter(d => d.id !== dm.id)
  setDms(cx, [...rest, dm])
  if (fresh) cx.cs.readStates.value = { ...cx.cs.readStates.value, [dm.id]: readStateOf(dm) }
}

/** Closes a conversation in the caller's list, or shows it again; only their own view changes. */
export async function setDmHidden(cx: Cx, id: string, hidden: boolean) {
  const community = cx.cs.communityGeneration
  const before = dmById(cx, id)
  if (!before || before.hidden === hidden) return
  upsertDm(cx, { ...before, hidden })
  try {
    await api(`/api/dms/${id}/hidden`, { method: 'PUT', json: { hidden } })
    assertCommunity(cx, community)
  } catch (error) {
    if (community === cx.cs.communityGeneration) upsertDm(cx, before)
    throw error
  }
}

/** Opens a conversation in the chat area; one the caller closed is in their list again. */
export async function openDm(cx: Cx, id: string) {
  const channel = dmChannel(cx, id)
  if (!channel) return
  if (dmById(cx, id)?.hidden) void setDmHidden(cx, id, false).catch(error => console.warn('Failed to reopen the conversation:', error))
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
  // The event carries the starter's view only: no preview (it depends on whom the receiver blocked), and a notify level and send right that are not the receiver's own.
  void refreshDm(cx, dm.id, true)
}

/** Reads one conversation again (its preview, or what the member did to it on another device); fresh also takes its counts and notify level. */
export async function refreshDm(cx: Cx, id: string, fresh = false) {
  const community = cx.cs.communityGeneration
  const known = isDm(cx, id)
  try {
    const dm = await api(`/api/dms/${id}`, { decode: decodeChatDirectMessage })
    // A conversation dropped while this ran (left, removed) stays gone.
    if (community === cx.cs.communityGeneration && (!known || isDm(cx, id))) upsertDm(cx, dm, fresh)
  } catch (error) {
    console.warn('Failed to read the conversation:', error)
  }
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
  // A blocked member's message stays folded away: it neither shows in the preview nor opens the conversation.
  if (msg.user_id && useBlocksStore().isBlocked(msg.user_id)) return
  // The server opens a closed conversation with the message; so does the list here.
  const latest = { last_message_at: msg.created_at, last_message: lastMessageOf(msg), hidden: false }
  cs.dms.value = sortDms(cs.dms.value.map(d => (d.id === msg.channel_id ? { ...d, ...latest } : d)))
}

/**
 * A message of a conversation was edited or deleted. It may be the one the list
 * previews, so the row is read again: an edit shows its new text, and a
 * deleted message never stays visible (the preview falls back to the one before).
 */
export function noteDmMessageChanged(cx: Cx, channelId: string | null | undefined) {
  if (channelId && dmById(cx, channelId)) void refreshDm(cx, channelId)
}
