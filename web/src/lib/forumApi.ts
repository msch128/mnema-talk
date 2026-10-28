import { api } from './api'
import type { ForumTag, Message } from '../types/domain'
import { decodeForumTag, decodeMessage } from '../types/domain'
import { isChatForumTag, isChatMessage } from '../types/rest'
import { arrayDecoder } from '../types/validation'

const decodeForumTags = arrayDecoder('ForumTag[]', isChatForumTag)
const decodeMessages = arrayDecoder('Message[]', isChatMessage)

export async function fetchForumTags(channelId: string): Promise<ForumTag[]> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/tags`, { decode: decodeForumTags })
}

export async function createForumTag(channelId: string, name: string, emoji?: string): Promise<ForumTag> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/tags`, {
    method: 'POST',
    json: { name, emoji: emoji || undefined },
    decode: decodeForumTag
  })
}

export async function updateForumTag(channelId: string, tagId: string, name?: string, emoji?: string): Promise<ForumTag> {
  // emoji: undefined keeps it, '' clears it (the server reads an omitted field as unchanged).
  return api(`/api/channels/${encodeURIComponent(channelId)}/tags/${encodeURIComponent(tagId)}`, {
    method: 'PATCH',
    json: { name, emoji },
    decode: decodeForumTag
  })
}

export async function deleteForumTag(channelId: string, tagId: string): Promise<void> {
  await api(`/api/channels/${encodeURIComponent(channelId)}/tags/${encodeURIComponent(tagId)}`, {
    method: 'DELETE'
  })
}

export interface FetchForumPostsOptions {
  sort?: 'activity' | 'newest' | undefined
  tagId?: string | undefined
  before?: string | undefined
  // With the activity sort: the last_activity_at the client saw for `before`.
  beforeActivity?: string | undefined
  limit?: number | undefined
}

export async function fetchForumPosts(channelId: string, options: FetchForumPostsOptions = {}): Promise<Message[]> {
  const params = new URLSearchParams()
  if (options.sort) params.set('sort', options.sort)
  if (options.tagId) params.set('tag_id', options.tagId)
  if (options.before) params.set('before', options.before)
  if (options.beforeActivity) params.set('before_activity', options.beforeActivity)
  if (options.limit) params.set('limit', String(options.limit))

  const qs = params.toString()
  const path = `/api/channels/${encodeURIComponent(channelId)}/posts${qs ? '?' + qs : ''}`
  return api(path, { decode: decodeMessages })
}

export async function createForumPost(
  channelId: string,
  title: string,
  content: string,
  tagIds?: string[]
): Promise<Message> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/posts`, {
    method: 'POST',
    json: {
      title,
      content,
      tag_ids: tagIds && tagIds.length > 0 ? tagIds : undefined
    },
    decode: decodeMessage
  })
}

export interface UpdateForumPostPayload {
  title?: string | undefined
  tag_ids?: string[] | undefined
  is_locked?: boolean | undefined
}

export async function updateForumPost(
  channelId: string,
  postId: string,
  payload: UpdateForumPostPayload
): Promise<Message> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/posts/${encodeURIComponent(postId)}`, {
    method: 'PATCH',
    json: payload,
    decode: decodeMessage
  })
}
