// Notification levels (channel, category), timed mutes and "mark all read".
// The server resolves the effective level of every channel (channel, then
// category, then "all"; an expired mute counts as none) and sends it in the
// read states; the stored settings only feed the menus.
import type { NotifyLevel, ReadState } from '../../types/domain'
import { decodeNotificationSettings } from '../../types/domain'
import { api } from '../../lib/api'
import { assertCommunity, type Cx } from './state'
import { fetchReadState } from './readState'

// setTimeout takes a 32-bit delay; a longer wait is re-armed when it fires.
const MAX_TIMER_MS = 2 ** 31 - 1
// A little after the mute ends, so the server's clock has surely passed it.
const EXPIRY_SLACK_MS = 1500

export async function fetchNotificationSettings(cx: Cx) {
  const { cs } = cx
  const community = cs.communityGeneration
  try {
    const data = await api('/api/notification-settings', { decode: decodeNotificationSettings })
    if (community !== cs.communityGeneration) return
    cs.notifySettings.value = data
    armExpiryTimer(cx)
  } catch (e) {
    console.warn('Failed to fetch notification settings:', e)
  }
}

/** Read states and settings again (another session changed them, a mute ended). */
export async function refreshNotifications(cx: Cx) {
  await Promise.all([fetchReadState(cx), fetchNotificationSettings(cx)])
}

/** The next moment a timed mute ends, in epoch milliseconds, or null. */
export function nextMuteEnd(cx: Cx, now = Date.now()): number | null {
  const { channels, categories } = cx.cs.notifySettings.value
  let next: number | null = null
  for (const s of [...channels, ...categories]) {
    if (!s.muted_until) continue
    const at = Date.parse(s.muted_until)
    if (Number.isFinite(at) && at > now && (next === null || at < next)) next = at
  }
  return next
}

function armExpiryTimer(cx: Cx) {
  const { cs } = cx
  clearTimeout(cs.notifyExpiryTimer)
  const end = nextMuteEnd(cx)
  if (end === null) return
  const delay = Math.min(end - Date.now() + EXPIRY_SLACK_MS, MAX_TIMER_MS)
  cs.notifyExpiryTimer = setTimeout(() => { void refreshNotifications(cx) }, delay)
}

// A setting that decides by itself is already shown; "inherit" and category
// changes move other channels' effective levels, so the server's answer is read.
async function putSetting(cx: Cx, path: string, level: NotifyLevel, mutedUntil: string | null, readStatesChanged: boolean) {
  const community = cx.cs.communityGeneration
  try {
    await api(path, { method: 'PUT', json: { level, muted_until: mutedUntil } })
    assertCommunity(cx, community)
    if (readStatesChanged) await refreshNotifications(cx)
    else await fetchNotificationSettings(cx)
    return true
  } catch (e) {
    console.warn('Failed to set notification level:', e)
    return false
  }
}

/** Set a channel's level; mutedUntil (an ISO time) ends a mute, null mutes until unmuted. */
export async function setNotificationLevel(cx: Cx, channelId: string, level: NotifyLevel, mutedUntil: string | null = null) {
  const { cs } = cx
  if (!channelId || !level) return false
  // Show the change at once; "inherit" needs the server to resolve it.
  if (level !== 'inherit') {
    const prev = cs.readStates.value[channelId] || { channel_id: channelId, unread_count: 0, mention_count: 0, last_read_at: null }
    cs.readStates.value = { ...cs.readStates.value, [channelId]: { ...prev, notify_level: level } }
  }
  return putSetting(cx, `/api/channels/${channelId}/notifications`, level, mutedUntil, level === 'inherit')
}

/** Set the level the channels of a category inherit. */
export async function setCategoryNotificationLevel(cx: Cx, categoryId: string, level: NotifyLevel, mutedUntil: string | null = null) {
  if (!categoryId || !level) return false
  return putSetting(cx, `/api/categories/${categoryId}/notifications`, level, mutedUntil, true)
}

/** The effective level of a channel: 'all' | 'mentions' | 'mute'. */
export function notificationLevel(cx: Cx, channelId: string): NotifyLevel {
  return cx.cs.readStates.value[channelId]?.notify_level || 'all'
}

/** Mark every channel read, or those of one category. */
export async function markAllRead(cx: Cx, categoryId?: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  const scope = categoryId ? cs.categories.value.find(c => c.id === categoryId)?.channels : cs.allChannels.value
  const now = new Date().toISOString()
  const next: Record<string, ReadState> = { ...cs.readStates.value }
  for (const ch of scope || []) {
    const prev = next[ch.id]
    if (prev) next[ch.id] = { ...prev, unread_count: 0, mention_count: 0, last_read_at: now }
  }
  cs.readStates.value = next
  try {
    await api('/api/read-state/read-all', { method: 'POST', json: categoryId ? { category_id: categoryId } : {} })
    assertCommunity(cx, community)
    return true
  } catch (e) {
    console.warn('Failed to mark all read:', e)
    await fetchReadState(cx)
    return false
  }
}
