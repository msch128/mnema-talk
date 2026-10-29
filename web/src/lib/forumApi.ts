import { api } from './api'
import type { ForumTag, Message } from '../types/domain'
import { decodeForumTag, decodeMessage } from '../types/domain'
import {
  isChatForumTag, isChatMessage, decodeChatForumSettings, decodeChatUnreadForumPosts, decodeChatPostRead
} from '../types/rest'
import type { ChatForumSettings } from '../types/rest'
import { arrayDecoder } from '../types/validation'

const decodeForumTags = arrayDecoder('ForumTag[]', isChatForumTag)
const decodeMessages = arrayDecoder('Message[]', isChatMessage)

export async function fetchForumTags(channelId: string): Promise<ForumTag[]> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/tags`, { decode: decodeForumTags })
}

export interface ForumTagFields {
  emoji?: string | undefined
  color?: string | undefined
  moderated?: boolean | undefined
}

export async function createForumTag(channelId: string, name: string, emoji?: string, extra: Omit<ForumTagFields, 'emoji'> = {}): Promise<ForumTag> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/tags`, {
    method: 'POST',
    json: { name, emoji: emoji || undefined, color: extra.color || undefined, moderated: extra.moderated || undefined },
    decode: decodeForumTag
  })
}

export async function updateForumTag(channelId: string, tagId: string, name?: string, emoji?: string, extra: Omit<ForumTagFields, 'emoji'> = {}): Promise<ForumTag> {
  // emoji and color: undefined keeps them, '' clears them (the server reads an omitted field as unchanged).
  return api(`/api/channels/${encodeURIComponent(channelId)}/tags/${encodeURIComponent(tagId)}`, {
    method: 'PATCH',
    json: { name, emoji, color: extra.color, moderated: extra.moderated },
    decode: decodeForumTag
  })
}

export async function reorderForumTags(channelId: string, tagIds: string[]): Promise<ForumTag[]> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/tags/order`, {
    method: 'PUT',
    json: { tag_ids: tagIds },
    decode: decodeForumTags
  })
}

export type ForumSettings = ChatForumSettings

export async function fetchForumSettings(channelId: string): Promise<ForumSettings> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/forum-settings`, { decode: decodeChatForumSettings })
}

export async function updateForumSettings(channelId: string, patch: { require_tag?: boolean; guidelines?: string }): Promise<ForumSettings> {
  return api(`/api/channels/${encodeURIComponent(channelId)}/forum-settings`, {
    method: 'PATCH',
    json: patch,
    decode: decodeChatForumSettings
  })
}

/** The posts of a forum with something the member has not read. */
export async function fetchUnreadForumPosts(channelId: string): Promise<string[]> {
  const unread = await api(`/api/channels/${encodeURIComponent(channelId)}/posts/unread`, { decode: decodeChatUnreadForumPosts })
  return unread.post_ids
}

/** Marks a post read; resolves to the read marker the member had before (where the first unread reply starts). */
export async function markForumPostRead(channelId: string, postId: string): Promise<string> {
  const read = await api(`/api/channels/${encodeURIComponent(channelId)}/posts/${encodeURIComponent(postId)}/read`, {
    method: 'PUT',
    decode: decodeChatPostRead
  })
  return read.previous_read_at
}

export async function deleteForumTag(channelId: string, tagId: string): Promise<void> {
  await api(`/api/channels/${encodeURIComponent(channelId)}/tags/${encodeURIComponent(tagId)}`, {
    method: 'DELETE'
  })
}

export interface FetchForumPostsOptions {
  sort?: 'activity' | 'newest' | undefined
  tagId?: string | undefined
  search?: string | undefined
  before?: string | undefined
  // With the activity sort: the last_activity_at the client saw for `before`.
  beforeActivity?: string | undefined
  limit?: number | undefined
}

export async function fetchForumPosts(channelId: string, options: FetchForumPostsOptions = {}): Promise<Message[]> {
  const params = new URLSearchParams()
  if (options.sort) params.set('sort', options.sort)
  if (options.tagId) params.set('tag_id', options.tagId)
  if (options.search) params.set('q', options.search)
  if (options.before) params.set('before', options.before)
  if (options.beforeActivity) params.set('before_activity', options.beforeActivity)
  if (options.limit) params.set('limit', String(options.limit))

  const qs = params.toString()
  const path = `/api/channels/${encodeURIComponent(channelId)}/posts${qs ? '?' + qs : ''}`
  return api(path, { decode: decodeMessages, dedupe: true })
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
  is_resolved?: boolean | undefined
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

export interface ForumPostDraft {
  title: string
  content: string
  tagIds: string[]
}

/** Creates a post whose first message carries a file. */
export async function createForumPostWithFile(channelId: string, draft: ForumPostDraft, file: File): Promise<Message> {
  const form = new FormData()
  // Text fields go before the file so the server reads them first.
  form.append('title', draft.title)
  form.append('content', draft.content)
  if (draft.tagIds.length > 0) form.append('tag_ids', draft.tagIds.join(','))
  form.append('file', file)
  return api(`/api/channels/${encodeURIComponent(channelId)}/upload`, { method: 'POST', form, decode: decodeMessage })
}

/** Adds a file to a post as a reply of its author. */
export async function uploadForumPostFile(channelId: string, postId: string, file: File): Promise<Message> {
  const form = new FormData()
  form.append('parent_id', postId)
  form.append('file', file)
  return api(`/api/channels/${encodeURIComponent(channelId)}/upload`, { method: 'POST', form, decode: decodeMessage })
}
