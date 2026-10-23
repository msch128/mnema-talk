// The threads the member follows. A reply notifies only its thread's
// followers, so the in-page notification asks this list. The server keeps it
// (reply authors, root authors and mentioned members follow on their own) and
// tells open sessions through thread_follow_updated; an older server has no
// such list and the notification rules stay as they were.
import { api } from '../../lib/api'
import { decodeChatFollowedThreads, decodeChatThreadFollow } from '../../types/rest'
import { useToastStore } from '../toast'
import { t } from '../../i18n'
import type { ServerEventPayloads } from '../../types/events'
import type { Cx } from './state'

function setFollowing(cx: Cx, threadId: string, following: boolean) {
  const next = new Set(cx.cs.followedThreads.value)
  if (following) next.add(threadId)
  else next.delete(threadId)
  cx.cs.followedThreads.value = next
}

/** Reads the followed threads; a server without the route leaves the list unknown. */
export async function fetchFollowedThreads(cx: Cx) {
  const { cs } = cx
  const community = cs.communityGeneration
  try {
    const data = await api('/api/threads/following', { decode: decodeChatFollowedThreads })
    if (community !== cs.communityGeneration) return
    cs.followedThreads.value = new Set(data.thread_ids)
    cs.followKnown.value = true
  } catch {
    cs.followKnown.value = false
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
