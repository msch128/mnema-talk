import type { ReadState } from '../../types/domain'
import { isChatReadState } from '../../types/rest'
import { arrayDecoder } from '../../types/validation'
import { api } from '../../lib/api'
import { assertCommunity, type Cx } from './state'

const decodeReadStates = arrayDecoder('read states', isChatReadState)

export async function fetchReadState(cx: Cx) {
  const { cs } = cx
  const community = cs.communityGeneration
  try {
    const data = await api('/api/read-state', { decode: decodeReadStates })
    if (community !== cs.communityGeneration) return {}
    const map: Record<string, ReadState> = {}
    for (const item of data) {
      map[item.channel_id] = {
        channel_id: item.channel_id,
        unread_count: item.unread_count || 0,
        mention_count: item.mention_count || 0,
        last_read_at: item.last_read_at || null,
        notify_level: item.notify_level
      }
    }
    // Conversations are not in the server's list; their counts are kept (GET /api/dms has them).
    for (const dm of cs.dms.value) {
      const kept = cs.readStates.value[dm.id]
      if (kept) map[dm.id] = kept
    }
    cs.readStates.value = map
    // The server may still hold counts for the channel being read right now.
    markActiveChannelReadIfReading(cx)
    return map
  } catch (e) {
    console.warn('Failed to fetch read-state:', e)
    return {}
  }
}

export async function markChannelRead(cx: Cx, channelId: string) {
  const { cs } = cx
  if (!channelId) return
  if (cs.suppressAutoReadFor === channelId) cs.suppressAutoReadFor = null

  const prev = cs.readStates.value[channelId] || { channel_id: channelId, notify_level: 'all' as const, unread_count: 0, mention_count: 0, last_read_at: null }
  cs.readStates.value = {
    ...cs.readStates.value,
    [channelId]: {
      ...prev,
      unread_count: 0,
      mention_count: 0,
      last_read_at: new Date().toISOString()
    }
  }
  try {
    await api(`/api/channels/${channelId}/read`, { method: 'POST' })
  } catch (e) {
    console.warn('Failed to mark channel read:', e)
  }
}

export function isTabVisible() {
  return typeof document === 'undefined' || !document.hidden
}

// The user can see the newest messages of the open text channel, or of the
// voice channel whose chat panel is open.
export function isReadingActiveChannel(cx: Cx) {
  const { cs } = cx
  const ch = cs.activeChannel.value
  return !!ch && (ch.type !== 'voice' || cs.voiceChatReading.value) && ch.id !== cs.suppressAutoReadFor && isTabVisible() && !cs.hasMoreAfter.value && !cs.isLoadingWindow.value
}

// Catch up after the tab becomes visible again, a jump to the present or a
// read-state refresh: clears whatever piled up while nobody was looking.
export function markActiveChannelReadIfReading(cx: Cx) {
  const { cs } = cx
  const channel = cs.activeChannel.value
  if (!channel || !isReadingActiveChannel(cx)) return
  const id = channel.id
  const st = cs.readStates.value[id]
  if (st && (st.unread_count > 0 || st.mention_count > 0)) cs.markReadThrottled.call(id)
}

/** The voice chat panel opened (true) or closed (false). */
export function setVoiceChatReading(cx: Cx, on: boolean) {
  const { cs } = cx
  cs.voiceChatReading.value = !!on
  if (on) markActiveChannelReadIfReading(cx)
}

export async function markChannelUnread(cx: Cx, channelId: string, messageId: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!channelId || !messageId) return

  // Keep the channel unread even though it is open: no auto-read until the
  // user marks it read (Esc) or switches channel.
  if (channelId === cs.activeChannel.value?.id) cs.suppressAutoReadFor = channelId
  cs.markReadThrottled.cancel(channelId)
  try {
    await api(`/api/channels/${channelId}/unread`, {
      method: 'POST',
      json: { message_id: messageId }
    })
    assertCommunity(cx, community)
  } catch (e) {
    assertCommunity(cx, community)
    if (cs.suppressAutoReadFor === channelId) cs.suppressAutoReadFor = null
    throw e
  }
  await fetchReadState(cx)
  assertCommunity(cx, community)
  if (channelId === cs.activeChannel.value?.id) {
    // The "new since" divider moves to the chosen message.
    cs.activeChannelLastReadAt.value = cs.readStates.value[channelId]?.last_read_at || null
  }
}

// Back in the tab (visible / focused): the store catches up on reading. One
// pair of listeners for the page; the live store registers its handler.
let onReturnToTab: (() => void) | null = null
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => onReturnToTab?.())
  window.addEventListener('focus', () => onReturnToTab?.())
}

export function setReturnToTabHandler(handler: () => void) {
  onReturnToTab = handler
}
