// The comments under community events.
import { api } from './api'
import {
  decodeCommunityeventsCommentList,
  decodeCommunityeventsCommentMute,
  decodeCommunityeventsEventComment,
  type CommunityeventsCommentList as CommentList,
  type CommunityeventsEventComment as EventComment
} from '../types/rest'

export type { CommentList, EventComment }

const base = (eventId: string) => `/api/events/${encodeURIComponent(eventId)}/comments`

/** A page of comments, oldest first: the newest page, or the one before `before`. */
export function fetchComments(eventId: string, before?: string, limit = 50): Promise<CommentList> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (before) params.set('before', before)
  return api(`${base(eventId)}?${params}`, { decode: decodeCommunityeventsCommentList })
}

export function postComment(eventId: string, content: string): Promise<EventComment> {
  return api(base(eventId), { method: 'POST', json: { content }, decode: decodeCommunityeventsEventComment })
}

export function editComment(eventId: string, id: string, content: string): Promise<EventComment> {
  return api(`${base(eventId)}/${encodeURIComponent(id)}`, { method: 'PATCH', json: { content }, decode: decodeCommunityeventsEventComment })
}

export async function deleteComment(eventId: string, id: string): Promise<void> {
  await api(`${base(eventId)}/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** Switches the notices of an event's comments off (muted) or on. */
export async function setCommentsMuted(eventId: string, muted: boolean): Promise<boolean> {
  const answer = await api(`${base(eventId)}/mute`, { method: 'PUT', json: { muted }, decode: decodeCommunityeventsCommentMute })
  return answer.muted
}
