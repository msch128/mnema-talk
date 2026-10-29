import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useForumPosts } from './useForumPosts'
import * as forumApi from '../lib/forumApi'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { messageFixture } from '../test-fixtures.fixture'

vi.mock('../lib/forumApi', () => ({ fetchForumPosts: vi.fn(), fetchUnreadForumPosts: vi.fn() }))

const post = (id: string, extra = {}) => messageFixture({ id, channel_id: 'c1', title: id, ...extra })
const page = (n: number, prefix = 'p') => Array.from({ length: n }, (_, i) => post(`${prefix}${i}`, { last_activity_at: `2026-01-01T10:${String(59 - i).padStart(2, '0')}:00Z` }))

const channel = ref('c1')
const open = ref<string | undefined>(undefined)
let list!: ReturnType<typeof useForumPosts>

let mounted: ReturnType<typeof mount> | undefined

function host() {
  mounted = mount(defineComponent({ setup() { list = useForumPosts(channel, open); return () => null } }))
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.useFakeTimers()
  channel.value = 'c1'
  open.value = undefined
  vi.mocked(forumApi.fetchForumPosts).mockResolvedValue([post('a')])
  vi.mocked(forumApi.fetchUnreadForumPosts).mockResolvedValue([])
})
afterEach(() => {
  mounted?.unmount()
  vi.useRealTimers()
})

describe('useForumPosts', () => {
  it('loads on mount with the default sort and no filter', async () => {
    host()
    await flushPromises()
    expect(forumApi.fetchForumPosts).toHaveBeenCalledWith('c1', { sort: 'activity', tagId: undefined, search: undefined })
    expect(list.posts.value.map(p => p.id)).toEqual(['a'])
  })

  it('searches after a pause, with the trimmed text as q, and loads once', async () => {
    host()
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockClear()
    list.search.value = ' ro'
    await vi.advanceTimersByTimeAsync(200)
    list.search.value = ' roles '
    await vi.advanceTimersByTimeAsync(200)
    expect(forumApi.fetchForumPosts).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(200)
    expect(forumApi.fetchForumPosts).toHaveBeenCalledTimes(1)
    expect(forumApi.fetchForumPosts).toHaveBeenCalledWith('c1', expect.objectContaining({ search: 'roles' }))
  })

  it('clears the search and reloads once when the channel changes', async () => {
    host()
    await flushPromises()
    list.search.value = 'x'
    await vi.advanceTimersByTimeAsync(400)
    vi.mocked(forumApi.fetchForumPosts).mockClear()
    channel.value = 'c2'
    await vi.advanceTimersByTimeAsync(500)
    expect(forumApi.fetchForumPosts).toHaveBeenCalledTimes(1)
    expect(forumApi.fetchForumPosts).toHaveBeenCalledWith('c2', expect.objectContaining({ search: undefined }))
  })

  it('does not count pinned posts toward a full page', async () => {
    const pinned = Array.from({ length: 3 }, (_, i) => post(`pin${i}`, { is_pinned: true }))
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([...pinned, ...page(47)])
    host()
    await flushPromises()
    expect(list.posts.value).toHaveLength(50)
    expect(list.hasMore.value).toBe(false)

    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([...pinned, ...page(50)])
    await list.load()
    expect(list.hasMore.value).toBe(true)
  })

  it('pages behind the last unpinned post with its activity', async () => {
    const first = [post('pin', { is_pinned: true }), ...page(50)]
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce(first)
    host()
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([post('old')])
    await list.loadMore()
    expect(forumApi.fetchForumPosts).toHaveBeenLastCalledWith('c1', expect.objectContaining({ before: 'p49', beforeActivity: first[50]!.last_activity_at }))
    expect(list.posts.value.at(-1)!.id).toBe('old')
    expect(list.hasMore.value).toBe(false)
  })

  it('refreshes when the forum posts signal fires for this channel, after a short pause', async () => {
    host()
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockClear()
    const chat = useChatStore()
    chat.forumPostsSignal = { channelId: 'elsewhere', seq: 1 }
    await vi.advanceTimersByTimeAsync(1000)
    expect(forumApi.fetchForumPosts).not.toHaveBeenCalled()
    chat.forumPostsSignal = { channelId: 'c1', seq: 2 }
    chat.forumPostsSignal = { channelId: 'c1', seq: 3 }
    await vi.advanceTimersByTimeAsync(1000)
    expect(forumApi.fetchForumPosts).toHaveBeenCalledTimes(1)
  })

  it('refreshes on a pin change and keeps the pages that were loaded', async () => {
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce(page(50))
    host()
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce(page(50, 'q'))
    useChatStore().pinsSignal = { channelId: 'c1', seq: 1 }
    await flushPromises()
    expect(forumApi.fetchForumPosts).toHaveBeenLastCalledWith('c1', expect.objectContaining({ limit: 50 }))
    expect(list.posts.value[0]!.id).toBe('q0')
  })

  it('leaves the post that is open out of the unread set and clears a read one', async () => {
    vi.mocked(forumApi.fetchUnreadForumPosts).mockResolvedValue(['a', 'b'])
    open.value = 'a'
    host()
    await flushPromises()
    expect([...list.unread.value]).toEqual(['b'])
    list.markRead('b')
    expect(list.unread.value.size).toBe(0)
  })

  it('shows the error and an empty list when loading fails, and drops a slower old answer', async () => {
    vi.mocked(forumApi.fetchForumPosts).mockRejectedValueOnce(new Error('down'))
    host()
    await flushPromises()
    expect(list.error.value).toBe('down')
    expect(list.posts.value).toEqual([])

    let release!: (v: ReturnType<typeof page>) => void
    vi.mocked(forumApi.fetchForumPosts).mockImplementationOnce(() => new Promise(r => { release = r }))
    const slow = list.load()
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([post('fresh')])
    await list.load()
    release([post('stale')])
    await slow
    expect(list.posts.value.map(p => p.id)).toEqual(['fresh'])
  })

  it('toasts when paging fails and keeps the list; a paging answer of an older list is dropped', async () => {
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce(page(50))
    host()
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockRejectedValueOnce(new Error('page failed'))
    await list.loadMore()
    expect(useToastStore().toasts.map(t => t.text).join()).toContain('page failed')
    expect(list.posts.value).toHaveLength(50)
    expect(list.isLoadingMore.value).toBe(false)

    let release!: (v: ReturnType<typeof page>) => void
    vi.mocked(forumApi.fetchForumPosts).mockImplementationOnce(() => new Promise(r => { release = r }))
    const more = list.loadMore()
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([post('fresh')])
    await list.load()
    release([post('stale')])
    await more
    expect(list.posts.value.map(p => p.id)).toEqual(['fresh'])
  })

  it('ignores a second paging request while one runs and does nothing without posts', async () => {
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([])
    host()
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockClear()
    await list.loadMore()
    expect(forumApi.fetchForumPosts).not.toHaveBeenCalled()

    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce(page(50))
    await list.load()
    let release!: (v: ReturnType<typeof page>) => void
    vi.mocked(forumApi.fetchForumPosts).mockImplementationOnce(() => new Promise(r => { release = r }))
    const first = list.loadMore()
    await list.loadMore()
    expect(forumApi.fetchForumPosts).toHaveBeenCalledTimes(2)
    release([])
    await first
  })

  it('drops the answer of a refresh that a newer request overtook, and survives a failing one', async () => {
    host()
    await flushPromises()
    let release!: (v: ReturnType<typeof page>) => void
    vi.mocked(forumApi.fetchForumPosts).mockImplementationOnce(() => new Promise(r => { release = r }))
    const stale = list.refresh()
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([post('newer')])
    await list.refresh()
    release([post('stale')])
    await stale
    expect(list.posts.value.map(p => p.id)).toEqual(['newer'])

    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(forumApi.fetchForumPosts).mockRejectedValueOnce(new Error('offline'))
    await list.refresh()
    expect(list.posts.value.map(p => p.id)).toEqual(['newer'])
    warn.mockRestore()
  })

  it('does not refresh while the list is loading and does nothing without a channel', async () => {
    host()
    await flushPromises()
    let release!: (v: ReturnType<typeof page>) => void
    vi.mocked(forumApi.fetchForumPosts).mockImplementationOnce(() => new Promise(r => { release = r }))
    const loading = list.load()
    vi.mocked(forumApi.fetchForumPosts).mockClear()
    await list.refresh()
    expect(forumApi.fetchForumPosts).not.toHaveBeenCalled()
    release([])
    await loading

    channel.value = ''
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockClear()
    await list.load()
    await list.refresh()
    expect(forumApi.fetchForumPosts).not.toHaveBeenCalled()
  })

  it('keeps going when the unread list cannot be loaded and drops an answer for a channel that was left', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(forumApi.fetchUnreadForumPosts).mockRejectedValueOnce(new Error('no'))
    host()
    await flushPromises()
    expect(list.posts.value).toHaveLength(1)
    expect(list.unread.value.size).toBe(0)
    warn.mockRestore()

    let release!: (v: string[]) => void
    vi.mocked(forumApi.fetchUnreadForumPosts).mockImplementationOnce(() => new Promise(r => { release = r }))
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValue([post('b')])
    await list.load()
    channel.value = 'c2'
    release(['x'])
    await flushPromises()
    expect(list.unread.value.has('x')).toBe(false)
  })
})
