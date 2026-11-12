import type { ReadState } from '../../types/domain'
import { isChatReadState, type ChatReadState } from '../../types/rest'
import { arrayDecoder } from '../../types/validation'
import { api } from '../../shared/lib/api'
import { assertCommunity, type Cx } from './state'

const decodeReadStates = arrayDecoder('read states', isChatReadState)

// One counter orders fetch starts and local clears. An answer that is not from
// the newest fetch is dropped, and a channel cleared here after its fetch began
// keeps the local state: the answer predates the clear and would bring the badge back.
//
// Answers and live messages: the server's answer says, per channel, the number
// of the newest message its counts include (last_message_number). Messages of
// a channel are stored in the order of their numbers (the channel row is
// locked from the number to the commit, messages.insertMessageTx), so the
// answer includes exactly the messages up to that number, whenever their
// message_create arrives. A message counted here while a fetch was on its way
// is counted again on top of the answer when its number is above the mark, and
// one that arrives after the answer counts only when its number is above it.
// The counts the server sends after a delete or an edit (read_state with
// last_message_number) are such an answer for one channel.
//
// The messages counted here are kept (at most MAX_COUNTED, the oldest go
// first) until a mark covers them or the channel is read: a message counted
// before a request was sent is always below its answer's mark, so only those
// counted since can come on top.
interface Counted { channel: string; number: number; unread: number; mentions: number }
const MAX_COUNTED = 500
const sequencing = new WeakMap<object, {
  counter: number; latest: number; cleared: Map<string, number>; newest: Promise<unknown> | null
  marks: Map<string, number>; counted: Counted[]
}>()

function sequenceOf(cs: object) {
  let seq = sequencing.get(cs)
  if (!seq) sequencing.set(cs, seq = { counter: 0, latest: 0, cleared: new Map(), newest: null, marks: new Map(), counted: [] })
  return seq
}

/** Whether the read state the client holds already counts message `number` of the channel (it was in the last answer). */
export function countedByAnswer(cx: Cx, channelId: string, number: number | undefined): boolean {
  const mark = sequenceOf(cx.cs).marks.get(channelId)
  return mark !== undefined && number !== undefined && number <= mark
}

/** Notes a message counted here, so an answer that does not include it gets it on top. */
export function noteCounted(cx: Cx, entry: Counted) {
  const seq = sequenceOf(cx.cs)
  seq.counted.push(entry)
  if (seq.counted.length > MAX_COUNTED) seq.counted.shift()
}

type Counts = Pick<ChatReadState, 'channel_id' | 'unread_count' | 'mention_count' | 'last_read_at' | 'last_message_number'>

/** The channel's counts from an answer, plus what was counted here and the answer does not include; sets its mark. */
function fromAnswer(seq: ReturnType<typeof sequenceOf>, item: Counts) {
  let unread = item.unread_count || 0
  let mentions = item.mention_count || 0
  const mark = item.last_message_number
  if (mark !== undefined) {
    for (const c of seq.counted) {
      if (c.channel !== item.channel_id || c.number <= mark) continue
      unread += c.unread
      mentions += c.mentions
    }
    seq.marks.set(item.channel_id, mark)
    seq.counted = seq.counted.filter(c => c.channel !== item.channel_id || c.number > mark)
  }
  return { channel_id: item.channel_id, unread_count: unread, mention_count: mentions, last_read_at: item.last_read_at || null }
}

/** Keeps the channels' state against answers on their way: they were read before. */
function keepAgainstAnswers(seq: ReturnType<typeof sequenceOf>, channelIds: readonly string[]) {
  seq.counter++
  for (const id of channelIds) seq.cleared.set(id, seq.counter)
}

/** Notes that the badges of these channels were cleared here just now (or read in another session). */
export function noteReadStateCleared(cx: Cx, channelIds: readonly string[]) {
  const seq = sequenceOf(cx.cs)
  keepAgainstAnswers(seq, channelIds)
  const read = new Set(channelIds)
  seq.counted = seq.counted.filter(c => !read.has(c.channel))
}

/**
 * The server's counts of one channel after a delete or an edit there (see
 * readstate.Recounter): applied like an answer for that channel, and kept
 * against an answer that was on its way.
 */
export function applyChannelCounts(cx: Cx, p: Counts) {
  const { cs } = cx
  const seq = sequenceOf(cs)
  keepAgainstAnswers(seq, [p.channel_id])
  const prev = cs.readStates.value[p.channel_id]
  cs.readStates.value = {
    ...cs.readStates.value,
    [p.channel_id]: { notify_level: prev?.notify_level ?? 'all', ...fromAnswer(seq, p) }
  }
}

export function fetchReadState(cx: Cx) {
  const seq = sequenceOf(cx.cs)
  const request = loadReadState(cx)
  seq.newest = request
  return request
}

/** Resolves once the newest read-state fetch has landed (a fetch that was overtaken is not the answer). */
async function newestReadStateSettled(cx: Cx) {
  const seq = sequenceOf(cx.cs)
  for (let seen = null; seen !== seq.newest;) {
    seen = seq.newest
    await seen
  }
}

async function loadReadState(cx: Cx) {
  const { cs } = cx
  const community = cs.communityGeneration
  const seq = sequenceOf(cs)
  const started = ++seq.counter
  seq.latest = started
  try {
    const data = await api('/api/read-state', { decode: decodeReadStates })
    if (community !== cs.communityGeneration || seq.latest !== started) return {}
    const map: Record<string, ReadState> = {}
    for (const item of data) {
      const local = cs.readStates.value[item.channel_id]
      if (local && (seq.cleared.get(item.channel_id) ?? 0) > started) {
        map[item.channel_id] = { ...local, notify_level: item.notify_level }
        continue
      }
      map[item.channel_id] = { ...fromAnswer(seq, item), notify_level: item.notify_level }
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

/** Marks a channel read; false when the server could not be told (the badge is cleared either way). */
export async function markChannelRead(cx: Cx, channelId: string): Promise<boolean> {
  const { cs } = cx
  if (!channelId) return false
  if (cs.suppressAutoReadFor === channelId) cs.suppressAutoReadFor = null
  noteReadStateCleared(cx, [channelId])

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
    return true
  } catch (e) {
    console.warn('Failed to mark channel read:', e)
    return false
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
  // A newer fetch may have overtaken ours; the divider follows the answer that was kept.
  await newestReadStateSettled(cx)
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
