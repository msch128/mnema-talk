import type { Message } from '../../types/domain'
import { isChatMessage } from '../../types/rest'
import { arrayDecoder, isRecord } from '../../types/validation'
import { api } from '../../lib/api'
import { useToastStore } from '../toast'
import { t } from '../../i18n'
import { PAGE_SIZE, WINDOW_CAP, fromLatest, mergeLatest, fromAround, prependOlder, appendNewer, appendLive, removeMessage } from '../../lib/messageWindow'
import { markPreviewEdited, markPreviewDeleted } from '../../lib/replies'
import { navigate } from '../../lib/router'
import { MAX_PENDING_LIVE } from './types'
import type { ThreadRoot, MessageWindow } from './types'
import type { ChatState, Cx } from './state'
import { linkTo } from './channels'
import { markActiveChannelReadIfReading } from './readState'
import { openThread } from './threads'

const decodeMessages = arrayDecoder('messages', isChatMessage)

function getWindow(cx: Cx) {
  const { cs } = cx
  return { messages: cs.messages.value, hasMoreBefore: cs.hasMoreBefore.value, hasMoreAfter: cs.hasMoreAfter.value }
}

export function setWindow(cx: Cx, win: MessageWindow) {
  const { cs } = cx
  cs.messages.value = win.messages
  cs.hasMoreBefore.value = win.hasMoreBefore
  cs.hasMoreAfter.value = win.hasMoreAfter
  if (!win.hasMoreAfter) flushPendingLive(cx)
}

// Live messages buffered while the window was not at the newest end join
// once it is (they are newer than anything a page could have returned).
function flushPendingLive(cx: Cx) {
  const { cs } = cx
  cs.missedLiveCount.value = 0
  if (!cs.pendingLive.length) return
  const queued = cs.pendingLive
  cs.pendingLive = []
  let win = getWindow(cx)
  for (const msg of queued) {
    if (msg.channel_id !== cs.activeChannel.value?.id) continue
    win = appendLive(win, msg, { cap: WINDOW_CAP }).window
  }
  cs.messages.value = win.messages
  cs.hasMoreBefore.value = win.hasMoreBefore
}

export function resetWindow(cx: Cx) {
  const { cs } = cx
  cs.windowGen++
  cs.isLoadingBefore.value = false
  cs.isLoadingAfter.value = false
  cs.isLoadingWindow.value = false
  cs.missedLiveCount.value = 0
}

function messagesUrl(channelId: string, params: Record<string, string> = {}) {
  const q = new URLSearchParams({ limit: String(PAGE_SIZE), ...params })
  return `/api/channels/${channelId}/messages?${q}`
}

export function showToast(text: string) {
  useToastStore().info(text)
}

// Per chat state: refreshes in flight for the current window generation.
const refreshesInFlight = new WeakMap<ChatState, { gen: number; n: number }>()

/** Loads the newest page of a channel and replaces the window. */
export async function fetchMessages(cx: Cx, channelId: string) {
  const { cs } = cx
  resetWindow(cx)
  const gen = cs.windowGen
  cs.isLoadingWindow.value = true
  try {
    const data = await api(messagesUrl(channelId), { decode: decodeMessages })
    // Ignore a late response for a channel the user already left.
    if (gen !== cs.windowGen || cs.activeChannel.value?.id !== channelId) return false
    setWindow(cx, fromLatest(data, PAGE_SIZE))
    cs.latestLoadSeq.value++
    return true
  } catch (e) {
    console.error('Failed to fetch messages:', e)
    // The window stays as it is: a refresh that fails must not empty a chat being read.
    if (gen === cs.windowGen && cs.activeChannel.value?.id === channelId) showToast(t('chat.messageLoadFailed'))
    return false
  } finally {
    if (gen === cs.windowGen) cs.isLoadingWindow.value = false
  }
}

/**
 * After a reconnect: catches up with what was missed without cutting the
 * history the reader paged in or moving the view. Only a window that does
 * not reach the newest end is left alone.
 */
export async function refreshLatest(cx: Cx, channelId: string) {
  const { cs } = cx
  if (!cs.messages.value.length) return fetchMessages(cx, channelId)
  if (cs.hasMoreAfter.value) return false
  const gen = cs.windowGen
  // Live messages that arrive while the page is in flight are newer than what
  // it can hold: hold them like a window load does and join them afterwards.
  // Overlapping refreshes (quick reconnects) keep holding until the last one ends.
  const held = refreshesInFlight.get(cs)
  refreshesInFlight.set(cs, { gen, n: held?.gen === gen ? held.n + 1 : 1 })
  cs.isLoadingWindow.value = true
  try {
    const data = await api(messagesUrl(channelId), { decode: decodeMessages })
    if (gen !== cs.windowGen || cs.activeChannel.value?.id !== channelId || cs.hasMoreAfter.value) return false
    const { window: win, replaced } = mergeLatest(getWindow(cx), data, { limit: PAGE_SIZE, cap: WINDOW_CAP })
    setWindow(cx, win)
    if (replaced) cs.latestLoadSeq.value++
    return true
  } catch (e) {
    console.error('Failed to refresh messages:', e)
    return false
  } finally {
    const now = refreshesInFlight.get(cs)
    const left = now?.gen === gen ? now.n - 1 : 0
    if (now?.gen === gen) refreshesInFlight.set(cs, { gen, n: left })
    if (gen === cs.windowGen && left === 0) {
      cs.isLoadingWindow.value = false
      // Messages held during a refresh that did not replace the window belong to it as it is.
      if (!cs.hasMoreAfter.value) flushPendingLive(cx)
    }
  }
}

/** "Jump to present": reload the newest page. */
export async function jumpToLatest(cx: Cx) {
  const { cs } = cx
  if (!cs.activeChannel.value) return false
  const ok = await fetchMessages(cx, cs.activeChannel.value.id)
  if (ok) markActiveChannelReadIfReading(cx)
  return ok
}

function pageBlocked(cs: ChatState, channelId: string | undefined, paging: boolean, hasMore: boolean): boolean {
  if (!channelId || cs.isLoadingWindow.value) return true
  if (paging) return true
  return !hasMore
}

export async function loadOlder(cx: Cx) {
  const { cs } = cx
  const channelId = cs.activeChannel.value?.id
  if (pageBlocked(cs, channelId, cs.isLoadingBefore.value, cs.hasMoreBefore.value)) return false
  if (!channelId) return false
  const anchorId = cs.messages.value[0]?.id
  if (!anchorId) return false
  const gen = cs.windowGen
  cs.isLoadingBefore.value = true
  try {
    const data = await api(messagesUrl(channelId, { before: anchorId }), { decode: decodeMessages })
    if (gen !== cs.windowGen) return false
    setWindow(cx, prependOlder(getWindow(cx), data, { anchorId, limit: PAGE_SIZE, cap: WINDOW_CAP }))
    return true
  } catch (e) {
    console.error('Failed to load older messages:', e)
    return false
  } finally {
    if (gen === cs.windowGen) cs.isLoadingBefore.value = false
  }
}

export async function loadNewer(cx: Cx) {
  const { cs } = cx
  const channelId = cs.activeChannel.value?.id
  if (pageBlocked(cs, channelId, cs.isLoadingAfter.value, cs.hasMoreAfter.value)) return false
  if (!channelId) return false
  const anchorId = cs.messages.value[cs.messages.value.length - 1]?.id
  if (!anchorId) return false
  const gen = cs.windowGen
  cs.isLoadingAfter.value = true
  try {
    const data = await api(messagesUrl(channelId, { after: anchorId }), { decode: decodeMessages })
    if (gen !== cs.windowGen) return false
    setWindow(cx, appendNewer(getWindow(cx), data, { anchorId, limit: PAGE_SIZE, cap: WINDOW_CAP }))
    markActiveChannelReadIfReading(cx)
    return true
  } catch (e) {
    console.error('Failed to load newer messages:', e)
    return false
  } finally {
    if (gen === cs.windowGen) cs.isLoadingAfter.value = false
  }
}

/**
 * Scrolls the view to a root message, loading the history around it when it
 * is outside the window. Resolves to false (and shows a toast) if it's gone.
 */
export async function jumpToMessage(cx: Cx, id: string) {
  const { cs } = cx
  const community = cs.communityGeneration
  const ok = await jumpToRootMessage(cx, id)
  if (community !== cs.communityGeneration) return false
  const pending = cs.pendingThreadOpen
  if (pending && pending.rootId === id) {
    cs.pendingThreadOpen = null
    if (ok && cs.activeChannel.value?.id === pending.channelId) openThread(cx, pending.rootId)
  }
  return ok
}

/**
 * Navigates to any message (search result, notification): thread replies
 * open their thread on top of the root message, the only valid jump anchor.
 */
export function goToMessage(cx: Cx, msg: Pick<Message, 'id' | 'channel_id' | 'parent_id'>) {
  const { cs } = cx
  if (!msg?.id || !msg.channel_id) return
  const rootId = msg.parent_id || msg.id
  cs.pendingThreadOpen = msg.parent_id ? { channelId: msg.channel_id, rootId } : null
  // A voice channel's messages live in the chat next to its Talk.
  navigate(linkTo(cx, msg.channel_id, { messageId: rootId }))
}

function rememberJump(cs: ChatState, id: string) {
  cs.jumpTarget.value = { id, seq: ++cs.jumpSeq }
}

function jumpWindowCurrent(cs: ChatState, gen: number, channelId: string): boolean {
  if (gen !== cs.windowGen) return false
  return cs.activeChannel.value?.id === channelId
}

function reportMissedJump(cs: ChatState, gen: number, error: unknown) {
  if (gen !== cs.windowGen) return
  const missing = isRecord(error) && error['status'] === 404
  showToast(missing ? t('chat.messageNotFound') : t('chat.messageLoadFailed'))
}

async function jumpToRootMessage(cx: Cx, id: string) {
  const { cs } = cx
  const channelId = cs.activeChannel.value?.id
  if (!id || !channelId) return false
  if (cs.messages.value.some(m => m.id === id)) {
    rememberJump(cs, id)
    return true
  }
  resetWindow(cx)
  const gen = cs.windowGen
  cs.isLoadingWindow.value = true
  try {
    const data = await api(messagesUrl(channelId, { around: id }), { decode: decodeMessages })
    if (!jumpWindowCurrent(cs, gen, channelId)) return false
    setWindow(cx, fromAround(data, id, PAGE_SIZE))
    // Same tick as the window swap so the view handles both in one render.
    rememberJump(cs, id)
    return true
  } catch (error) {
    reportMissedJump(cs, gen, error)
    return false
  } finally {
    if (gen === cs.windowGen) {
      cs.isLoadingWindow.value = false
      // Live messages held during a failed jump belong to the unchanged window.
      if (!cs.hasMoreAfter.value) flushPendingLive(cx)
    }
  }
}

export function insertMessage(cx: Cx, msg: Message) {
  const { cs } = cx
  if (msg.parent_id) {
    insertThreadReply(cx, msg)
    return
  }
  if (cs.activeChannel.value?.id !== msg.channel_id) return
  if (cs.isLoadingWindow.value || cs.hasMoreAfter.value) {
    // The window doesn't reach the newest end; appending would leave a gap.
    holdLiveMessage(cx, msg)
    return
  }
  const { window: win, status } = appendLive(getWindow(cx), msg, { cap: WINDOW_CAP })
  if (status !== 'appended') return
  cs.messages.value = win.messages
  cs.hasMoreBefore.value = win.hasMoreBefore
  cs.liveAppendSeq.value++
}

function insertThreadReply({ cs }: Cx, msg: Message) {
  if (cs.activeThread.value?.id === msg.parent_id && !cs.threadReplies.value.some(m => m.id === msg.id)) {
    cs.threadReplies.value.push(msg)
  }
}

function holdLiveMessage(cx: Cx, msg: Message) {
  const { cs } = cx
  if (cs.messages.value.some(m => m.id === msg.id) || cs.pendingLive.some(m => m.id === msg.id)) return
  cs.pendingLive.push(msg)
  if (cs.pendingLive.length > MAX_PENDING_LIVE) cs.pendingLive.shift()
  // My own messages are not news to me.
  if (!cs.isLoadingWindow.value && !isOwnMessage(cx, msg)) cs.missedLiveCount.value++
}

function isOwnMessage(cx: Cx, msg: Message) {
  const { authStore } = cx
  const me = authStore.user?.id
  return !!me && msg?.user_id === me
}

export function removeFromWindow(cx: Cx, id: string) {
  const { cs } = cx
  const win = removeMessage(getWindow(cx), id)
  if (win.messages !== cs.messages.value) cs.messages.value = win.messages
  cs.pendingLive = cs.pendingLive.filter(m => m.id !== id)
}

export function countReply(cx: Cx, msg: Message) {
  const { cs } = cx
  if (!msg?.parent_id || cs.countedReplies.has(msg.id)) return
  cs.countedReplies.add(msg.id)
  bumpReplyCount(cx, msg.parent_id, 1)
}

export function bumpReplyCount(cx: Cx, parentId: string, delta: number) {
  const { cs } = cx
  const root = cs.messages.value.find(m => m.id === parentId)
  if (root) root.reply_count = Math.max(0, (root.reply_count || 0) + delta)
  if (cs.activeThread.value?.id === parentId) {
    cs.activeThread.value.reply_count = Math.max(0, (cs.activeThread.value.reply_count || 0) + delta)
  }
}

export function applyToMessage(cx: Cx, id: string, fn: (message: ThreadRoot) => void) {
  const { cs } = cx
  const m = cs.messages.value.find(x => x.id === id)
  if (m) fn(m)
  const t = cs.threadReplies.value.find(x => x.id === id)
  if (t) fn(t)
  if (cs.activeThread.value?.id === id) fn(cs.activeThread.value)
}

export function loadedLists(cx: Cx) {
  const { cs } = cx
  return [cs.messages.value, cs.threadReplies.value, cs.activeThread.value ? [cs.activeThread.value] : []]
}

// Keep "replied to" previews in sync with their original message.
export function onOriginalEdited(cx: Cx, original: Message) {
  markPreviewEdited(loadedLists(cx), original)
}

export function onOriginalDeleted(cx: Cx, id: string) {
  markPreviewDeleted(loadedLists(cx), id)
}

