// The threads the member follows. A reply notifies only its thread's
// followers, so the in-page notification asks this list. The server keeps it
// (reply authors, root authors and mentioned members follow on their own) and
// tells open sessions through thread_follow_updated; an older server has no
// such list and the notification rules stay as they were.
import { api, isApiError } from '../../shared/lib/api'
import { decodeChatFollowedThreads, decodeChatThreadFollow } from '../../types/rest'
import { useToastStore } from '../toast'
import { t } from '../../i18n'
import type { ServerEventPayloads } from '../../types/events'
import type { Cx } from './state'

// The answer of GET /api/threads/following was read when the request was made:
// a follow or unfollow that arrived (or was made here) while it was on its way
// is applied to it again, in the order it came. Each says the whole state of
// its thread, so applying one the answer already holds changes nothing. Only
// the answer of the newest request is kept.
const followSequencing = new WeakMap<object, { latest: number; inFlight: number; changes: [string, boolean][] }>()

function followSequenceOf(cs: object) {
  let seq = followSequencing.get(cs)
  if (!seq) followSequencing.set(cs, seq = { latest: 0, inFlight: 0, changes: [] })
  return seq
}

function setFollowing(cx: Cx, threadId: string, following: boolean) {
  const next = new Set(cx.cs.followedThreads.value)
  if (following) next.add(threadId)
  else next.delete(threadId)
  cx.cs.followedThreads.value = next
  const seq = followSequenceOf(cx.cs)
  if (seq.inFlight > 0) seq.changes.push([threadId, following])
}

/** Reads the followed threads; a server without the route leaves the list unknown. */
export async function fetchFollowedThreads(cx: Cx) {
  const { cs } = cx
  const community = cs.communityGeneration
  const seq = followSequenceOf(cs)
  const mine = ++seq.latest
  const from = seq.changes.length
  seq.inFlight++
  try {
    const data = await api('/api/threads/following', { decode: decodeChatFollowedThreads })
    if (community !== cs.communityGeneration || mine !== seq.latest) return
    const next = new Set(data.thread_ids)
    for (const [id, following] of seq.changes.slice(from)) {
      if (following) next.add(id)
      else next.delete(id)
    }
    cs.followedThreads.value = next
    cs.followKnown.value = true
  } catch (e) {
    if (community !== cs.communityGeneration) return
    // Only a missing route (an older server) leaves the list unknown; a failed
    // fetch keeps what was known, so replies in unfollowed threads stay quiet.
    if (isApiError(e) && (e.status === 404 || e.status === 501)) cs.followKnown.value = false
  } finally {
    if (--seq.inFlight === 0) seq.changes = []
  }
}

export function isFollowingThread(cx: Cx, threadId: string): boolean {
  return cx.cs.followedThreads.value.has(threadId)
}

/** The thread read by openThread says whether the member follows it. */
export function noteThreadFollowing(cx: Cx, threadId: string, following: boolean | undefined) {
  if (following === undefined) return
  setFollowing(cx, threadId, following)
}

export async function setThreadFollow(cx: Cx, threadId: string, following: boolean) {
  const before = isFollowingThread(cx, threadId)
  setFollowing(cx, threadId, following)
  try {
    const state = await api(`/api/threads/${threadId}/follow`, { method: following ? 'PUT' : 'DELETE', decode: decodeChatThreadFollow })
    setFollowing(cx, threadId, state.following)
  } catch {
    setFollowing(cx, threadId, before)
    useToastStore().error(t(following ? 'thread.followFailed' : 'thread.unfollowFailed'))
  }
}

export function onThreadFollowUpdated(cx: Cx, p: ServerEventPayloads['thread_follow_updated']) {
  if (p) setFollowing(cx, p.thread_id, p.following)
}
