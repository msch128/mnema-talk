import { describe, it, expect, beforeEach, vi } from 'vitest'
import { api } from './api'
import {
  fetchForumTags, createForumTag, updateForumTag, deleteForumTag,
  fetchForumPosts, createForumPost, updateForumPost,
  reorderForumTags, fetchForumSettings, updateForumSettings, fetchUnreadForumPosts, markForumPostRead,
  createForumPostWithFile, uploadForumPostFile
} from './forumApi'
import { fixtureId, messageFixture } from '../test-fixtures.fixture'

vi.mock('./api', () => ({ api: vi.fn() }))

const apiMock = vi.mocked(api) as unknown as ReturnType<typeof vi.fn>
const channelId = 'chan/1 x'
const encoded = 'chan%2F1%20x'

const tagJson = { id: fixtureId(7), channel_id: fixtureId(2), name: 'help', emoji: '?', position: 1, moderated: false, created_at: '2026-01-01T00:00:00Z' }

type Opts = { method?: string; json?: unknown; decode?: (v: unknown) => unknown }

function lastCall(): [string, Opts] {
  const call = apiMock.mock.calls.at(-1)
  if (!call) throw new Error('api was not called')
  return [call[0] as string, call[1] as Opts]
}

beforeEach(() => {
  apiMock.mockReset()
})

describe('forum tags api', () => {
  it('fetches tags of an encoded channel path and decodes the list', async () => {
    apiMock.mockResolvedValue([])
    await fetchForumTags(channelId)
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/tags`)
    expect(opts.decode?.([tagJson])).toEqual([tagJson])
    expect(() => opts.decode?.([{ nope: true }])).toThrow()
  })

  it('creates a tag and drops an empty emoji', async () => {
    apiMock.mockResolvedValue(tagJson)
    await createForumTag(channelId, 'help', '')
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/tags`)
    expect(opts.method).toBe('POST')
    expect(opts.json).toEqual({ name: 'help', emoji: undefined })
    expect(opts.decode?.(tagJson)).toEqual(tagJson)

    await createForumTag(channelId, 'help', '?')
    expect(lastCall()[1].json).toEqual({ name: 'help', emoji: '?' })
  })

  it('updates a tag by id with PATCH', async () => {
    apiMock.mockResolvedValue(tagJson)
    await updateForumTag(channelId, 't/1', 'new', '')
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/tags/t%2F1`)
    expect(opts.method).toBe('PATCH')
    // An empty emoji clears it: it must reach the server as "", not be dropped as "unchanged".
    expect(opts.json).toEqual({ name: 'new', emoji: '' })
    expect(JSON.stringify(opts.json)).toContain('"emoji":""')
    await updateForumTag(channelId, 't1', 'new')
    expect(JSON.stringify(lastCall()[1].json)).not.toContain('emoji')
    await updateForumTag(channelId, 't1', 'new', '!')
    expect(lastCall()[1].json).toEqual({ name: 'new', emoji: '!' })
  })

  it('deletes a tag and resolves to nothing', async () => {
    apiMock.mockResolvedValue(undefined)
    await expect(deleteForumTag(channelId, 't1')).resolves.toBeUndefined()
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/tags/t1`)
    expect(opts.method).toBe('DELETE')
  })
})

describe('forum posts api', () => {
  it('fetches posts without a query string by default', async () => {
    apiMock.mockResolvedValue([])
    await fetchForumPosts(channelId)
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/posts`)
    expect(opts.decode?.([messageFixture()])).toHaveLength(1)
  })

  it('sends sort, tag, cursor and limit as query parameters', async () => {
    apiMock.mockResolvedValue([])
    await fetchForumPosts(channelId, { sort: 'newest', tagId: 'tag-1', before: 'p9', limit: 25 })
    const [path] = lastCall()
    const url = new URL(path, 'http://x')
    expect(url.pathname).toBe(`/api/channels/${encoded}/posts`)
    expect(url.searchParams.get('sort')).toBe('newest')
    expect(url.searchParams.get('tag_id')).toBe('tag-1')
    expect(url.searchParams.get('before')).toBe('p9')
    expect(url.searchParams.get('limit')).toBe('25')
  })

  it('creates a post with tag ids only when there are some', async () => {
    apiMock.mockResolvedValue(messageFixture())
    await createForumPost(channelId, 'Title', 'Body', ['a', 'b'])
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/posts`)
    expect(opts.method).toBe('POST')
    expect(opts.json).toEqual({ title: 'Title', content: 'Body', tag_ids: ['a', 'b'] })
    expect(opts.decode?.(messageFixture())).toMatchObject({ content: 'Synthetic message' })

    await createForumPost(channelId, 'Title', 'Body', [])
    expect(lastCall()[1].json).toEqual({ title: 'Title', content: 'Body', tag_ids: undefined })
    await createForumPost(channelId, 'Title', 'Body')
    expect(lastCall()[1].json).toEqual({ title: 'Title', content: 'Body', tag_ids: undefined })
  })

  it('patches a post with the given payload', async () => {
    apiMock.mockResolvedValue(messageFixture())
    await updateForumPost(channelId, 'p/1', { is_locked: true })
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/posts/p%2F1`)
    expect(opts.method).toBe('PATCH')
    expect(opts.json).toEqual({ is_locked: true })
  })
})

describe('forum tag extras', () => {
  it('sends colour and moderated flag on create and update, only when set', async () => {
    apiMock.mockResolvedValue(tagJson)
    await createForumTag(channelId, 'help', '', { color: 'blue', moderated: true })
    expect(lastCall()[1].json).toEqual({ name: 'help', emoji: undefined, color: 'blue', moderated: true })
    await createForumTag(channelId, 'help')
    expect(lastCall()[1].json).toEqual({ name: 'help' })

    await updateForumTag(channelId, 't1', 'help', '!', { color: '', moderated: false })
    // An empty colour clears it and false un-moderates: both must reach the server.
    expect(JSON.stringify(lastCall()[1].json)).toContain('"color":""')
    expect(JSON.stringify(lastCall()[1].json)).toContain('"moderated":false')
  })

  it('reorders tags with PUT and the ids in order', async () => {
    apiMock.mockResolvedValue([])
    await reorderForumTags(channelId, ['b', 'a'])
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/tags/order`)
    expect(opts.method).toBe('PUT')
    expect(opts.json).toEqual({ tag_ids: ['b', 'a'] })
  })
})

describe('forum settings and read state api', () => {
  it('reads and patches the forum settings', async () => {
    apiMock.mockResolvedValue({ require_tag: true, guidelines: 'x' })
    await fetchForumSettings(channelId)
    expect(lastCall()[0]).toBe(`/api/channels/${encoded}/forum-settings`)
    expect(lastCall()[1].decode?.({ require_tag: true, guidelines: 'x' })).toEqual({ require_tag: true, guidelines: 'x' })
    await updateForumSettings(channelId, { require_tag: false })
    expect(lastCall()[1].method).toBe('PATCH')
    expect(lastCall()[1].json).toEqual({ require_tag: false })
  })

  it('lists unread post ids', async () => {
    apiMock.mockResolvedValue({ post_ids: ['a', 'b'] })
    await expect(fetchUnreadForumPosts(channelId)).resolves.toEqual(['a', 'b'])
    expect(lastCall()[0]).toBe(`/api/channels/${encoded}/posts/unread`)
  })

  it('marks a post read and resolves to the marker it had', async () => {
    apiMock.mockResolvedValue({ previous_read_at: '2026-01-01T00:00:00Z' })
    await expect(markForumPostRead(channelId, 'p/1')).resolves.toBe('2026-01-01T00:00:00Z')
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/posts/p%2F1/read`)
    expect(opts.method).toBe('PUT')
  })

  it('sends the search text as q', async () => {
    apiMock.mockResolvedValue([])
    await fetchForumPosts(channelId, { search: 'roles' })
    expect(new URL(lastCall()[0], 'http://x').searchParams.get('q')).toBe('roles')
  })

  it('patches a solved mark', async () => {
    apiMock.mockResolvedValue(messageFixture())
    await updateForumPost(channelId, 'p1', { is_resolved: true })
    expect(lastCall()[1].json).toEqual({ is_resolved: true })
  })
})

describe('forum post uploads', () => {
  it('puts title, content and tags before the file in the form', async () => {
    apiMock.mockResolvedValue(messageFixture())
    const file = new File(['x'], 'a.png', { type: 'image/png' })
    await createForumPostWithFile(channelId, { title: 'T', content: 'C', tagIds: ['a', 'b'] }, file)
    const [path, opts] = lastCall()
    expect(path).toBe(`/api/channels/${encoded}/upload`)
    const form = (opts as unknown as { form: FormData }).form
    expect([...form.keys()]).toEqual(['title', 'content', 'tag_ids', 'file'])
    expect(form.get('tag_ids')).toBe('a,b')
  })

  it('leaves tag_ids out without tags and uploads further files as replies', async () => {
    apiMock.mockResolvedValue(messageFixture())
    const file = new File(['x'], 'a.png', { type: 'image/png' })
    await createForumPostWithFile(channelId, { title: 'T', content: 'C', tagIds: [] }, file)
    expect([...((lastCall()[1] as unknown as { form: FormData }).form).keys()]).toEqual(['title', 'content', 'file'])
    await uploadForumPostFile(channelId, 'p1', file)
    const form = (lastCall()[1] as unknown as { form: FormData }).form
    expect([...form.keys()]).toEqual(['parent_id', 'file'])
    expect(form.get('parent_id')).toBe('p1')
  })
})
