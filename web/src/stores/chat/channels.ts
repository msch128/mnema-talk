import type { Channel } from '../../types/domain'
import { decodeChannelHierarchy, decodeChannel, decodeCategory } from '../../types/domain'
import { decodeChatRenamedCategory } from '../../types/rest'
import { api } from '../../lib/api'
import { emptyWindow } from '../../lib/messageWindow'
import { pruneChannelDrafts } from '../../lib/drafts'
import { currentRoute, findChannel } from '../../lib/router'
import { channelPath, dmPath, textPath, type ChannelTail } from '../../lib/routePaths'
import { pathDms } from '../../lib/dm'
import { useServerFeaturesStore } from '../serverFeatures'
import { assertCommunity, type Cx } from './state'
import { fetchMessages, resetWindow, setWindow } from './messageWindow'
import { markChannelRead } from './readState'
import { closeThread } from './threads'

/** Whether channelId is a known voice channel. */
export function isVoiceChannel(cx: Cx, channelId: string) {
  const { cs } = cx
  return cs.allChannels.value.some(c => c.id === channelId && c.type === 'voice')
}

/**
 * The canonical address of a channel, Talk or conversation, by id: /c/:slug,
 * /v/:slug or /dm/…. An id the client does not know yet gets the legacy
 * /c/:id address, which the router rewrites once it does.
 */
export function linkTo(cx: Cx, channelId: string, tail: ChannelTail = {}): string {
  const channel = cx.cs.allChannels.value.find(c => c.id === channelId)
  if (channel) return channelPath(channel, tail)
  const dms = pathDms(cx.cs.dms.value, cx.authStore.user?.id)
  const dm = dms.find(d => d.id === channelId)
  return dm ? dmPath(dm, dms, tail) : textPath({ id: channelId }, tail)
}

export function fetchChannels(cx: Cx): Promise<void> {
  const { cs } = cx
  const fetching = loadChannels(cx, ++cs.channelsFetchSeq)
  cs.latestChannelsFetch = fetching
  return fetching
}

function resolveFallbackChannel(channels: Channel[]): Channel | null {
  const route = currentRoute.value
  const routeCh = 'channelRef' in route ? findChannel(channels, route.channelRef)?.channel : undefined
  return routeCh ?? channels.find(c => c.type === 'text') ?? channels[0] ?? null
}

function syncActiveChannel(cx: Cx) {
  const { cs } = cx
  const activeId = cs.activeChannel.value?.id
  // An open conversation is not in the channel list and stays open.
  if (activeId && cs.dms.value.some(d => d.id === activeId)) return
  const fresh = activeId ? cs.allChannels.value.find(c => c.id === activeId) : null
  if (fresh) {
    // Same channel, new object: a rename or topic change shows in the header.
    cs.activeChannel.value = fresh
    return
  }
  const next = resolveFallbackChannel(cs.allChannels.value)
  if (next) {
    selectChannel(cx, next)
    return
  }
  cs.activeChannel.value = null
  resetWindow(cx)
  setWindow(cx, emptyWindow())
}

/** An empty list proves nothing (a hiccup, a fresh sign-in), so it prunes nothing. */
function forgetDraftsOfGoneChannels(cx: Cx) {
  const uid = cx.authStore.user?.id
  const channels = cx.cs.allChannels.value
  // Drafts of conversations stay until the conversation list is known.
  if (useServerFeaturesStore().dm && !cx.cs.dmsLoaded.value) return
  const keep = new Set([...channels.map(c => c.id), ...cx.cs.dms.value.map(d => d.id)])
  if (uid && channels.length) pruneChannelDrafts(uid, keep)
}

async function loadChannels(cx: Cx, seq: number): Promise<void> {
  const { cs } = cx
  try {
    const data = await api('/api/channels', { decode: decodeChannelHierarchy })
    if (seq !== cs.channelsFetchSeq) return cs.latestChannelsFetch
    cs.categories.value = data.categories
    cs.uncategorized.value = data.uncategorized
    forgetDraftsOfGoneChannels(cx)
    syncActiveChannel(cx)
  } catch (e) {
    console.error('Failed to fetch channels:', e)
  }
}

export async function selectChannel(cx: Cx, channel: Channel | null) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (!channel) return
  if (cs.activeChannel.value?.id !== channel.id) cs.suppressAutoReadFor = null
  cs.activeChannel.value = channel
  closeThread(cx)
  cs.pendingLive = []
  resetWindow(cx)
  setWindow(cx, emptyWindow())
  cs.activeChannelLastReadAt.value = cs.readStates.value[channel.id]?.last_read_at || null
  const shown = await fetchMessages(cx, channel.id)
  if (community !== cs.communityGeneration) return
  // Only a window the reader actually got to see counts as read.
  if (!shown || cs.activeChannel.value?.id !== channel.id) return
  if (channel.type !== 'voice' || cs.voiceChatReading.value) {
    await markChannelRead(cx, channel.id)
  }
}

export async function createChannel(cx: Cx, { categoryId, name, type, topic, sortOrder = 0, isStage = false }: { categoryId?: string | null; name: string; type?: Channel['type']; topic?: string; sortOrder?: number; isStage?: boolean }) {
  const { cs } = cx
  const community = cs.communityGeneration
  const channel = await api('/api/admin/channels', {
    method: 'POST',
    json: { category_id: categoryId || null, name, type: type || 'text', topic: topic || '', sort_order: sortOrder, ...(isStage ? { is_stage: true } : {}) }, decode: decodeChannel
  })
  assertCommunity(cx, community)
  await fetchChannels(cx)
  assertCommunity(cx, community)
  selectChannel(cx, channel)
  return channel
}

/** Copy of a channel, placed right below it by the server. */
export async function duplicateChannel(cx: Cx, channelId: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  const channel = await api(`/api/admin/channels/${channelId}/duplicate`, { method: 'POST', decode: decodeChannel })
  assertCommunity(cx, community)
  await fetchChannels(cx)
  assertCommunity(cx, community)
  return channel
}

export async function deleteChannel(cx: Cx, channelId: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  await api(`/api/admin/channels/${channelId}`, { method: 'DELETE' })
  assertCommunity(cx, community)
  if (cs.activeChannel.value?.id === channelId) cs.activeChannel.value = null
  await fetchChannels(cx)
  assertCommunity(cx, community)

}

export async function createCategory(cx: Cx, name: string, sortOrder = 0) {
  const { cs } = cx
  const community = cs.communityGeneration
  const category = await api('/api/admin/categories', { method: 'POST', json: { name, sort_order: sortOrder }, decode: decodeCategory })
  assertCommunity(cx, community)
  await fetchChannels(cx)
  assertCommunity(cx, community)
  return category
}

export async function deleteCategory(cx: Cx, categoryId: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  await api(`/api/admin/categories/${categoryId}`, { method: 'DELETE' })
  assertCommunity(cx, community)
  await fetchChannels(cx)
  assertCommunity(cx, community)

}

export async function updateChannel(cx: Cx, channelId: string, { name, topic, slow_mode_seconds }: { name: string; topic: string; slow_mode_seconds?: number }) {
  const { cs } = cx
  const community = cs.communityGeneration
  const updated = await api(`/api/admin/channels/${channelId}`, {
    method: 'PATCH',
    json: slow_mode_seconds === undefined ? { name, topic } : { name, topic, slow_mode_seconds }, decode: decodeChannel
  })
  assertCommunity(cx, community)
  await fetchChannels(cx)
  assertCommunity(cx, community)
  return updated
}

/** Sets how long a channel keeps its messages: 1, 7, 30 or 90 days; 0 keeps them for good. */
export async function setChannelAutoDelete(cx: Cx, channelId: string, days: number) {
  const { cs } = cx
  const community = cs.communityGeneration
  const updated = await api(`/api/admin/channels/${channelId}/auto-delete`, { method: 'PUT', json: { days }, decode: decodeChannel })
  assertCommunity(cx, community)
  await fetchChannels(cx)
  assertCommunity(cx, community)
  return updated
}

export async function updateCategory(cx: Cx, categoryId: string, { name }: { name: string }) {
  const { cs } = cx
  const community = cs.communityGeneration
  const updated = await api(`/api/admin/categories/${categoryId}`, {
    method: 'PATCH',
    json: { name }, decode: decodeChatRenamedCategory
  })
  assertCommunity(cx, community)
  await fetchChannels(cx)
  assertCommunity(cx, community)
  return updated
}

