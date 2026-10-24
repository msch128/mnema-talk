import { describe, it, expect, beforeEach, vi } from 'vitest'
import { api } from './api'
import {
  fetchForumTags, createForumTag, updateForumTag, deleteForumTag,
  fetchForumPosts, createForumPost, updateForumPost
} from './forumApi'
import { fixtureId, messageFixture } from '../test-fixtures.fixture'

vi.mock('./api', () => ({ api: vi.fn() }))

const apiMock = vi.mocked(api) as unknown as ReturnType<typeof vi.fn>
const channelId = 'chan/1 x'
const encoded = 'chan%2F1%20x'

const tagJson = { id: fixtureId(7), channel_id: fixtureId(2), name: 'help', emoji: '?', created_at: '2026-01-01T00:00:00Z' }

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
    expect(opts.json).toEqual({ name: 'new', emoji: undefined })
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
