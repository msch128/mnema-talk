import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ForumArea from './ForumArea.vue'
import { setLocale } from '../i18n'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import * as forumApi from '../lib/forumApi'
import type { ForumTag } from '../types/domain'
import { channelFixture, messageFixture } from '../test-fixtures.fixture'

vi.mock('../lib/forumApi', () => ({
  createForumPost: vi.fn(),
  fetchForumPosts: vi.fn(),
  fetchForumTags: vi.fn(),
  updateForumPost: vi.fn(),
  createForumTag: vi.fn(),
  updateForumTag: vi.fn(),
  deleteForumTag: vi.fn()
}))

let wrapper: VueWrapper
let chatStore: ReturnType<typeof useChatStore>
let permsStore: ReturnType<typeof usePermissionsStore>

const mockChannel = channelFixture({
  id: 'forum-chan-1',
  name: 'general-discussions',
  type: 'forum',
  topic: 'All community topics'
})

const mockTags: ForumTag[] = [
  { id: 'tag-1', channel_id: 'forum-chan-1', name: 'discussion', emoji: '💬', created_at: '2026-01-01T00:00:00Z' },
  { id: 'tag-2', channel_id: 'forum-chan-1', name: 'help', emoji: '❓', created_at: '2026-01-01T00:00:00Z' }
]

const mockPosts = [
  messageFixture({
    id: 'post-1',
    channel_id: 'forum-chan-1',
    title: 'Welcome to the forum!',
    content: 'This is the first post.',
    tags: [mockTags[0]!],
    reply_count: 2,
    created_at: '2026-01-01T10:00:00Z',
    last_activity_at: '2026-01-01T12:00:00Z'
  }),
  messageFixture({
    id: 'post-2',
    channel_id: 'forum-chan-1',
    title: 'Need help with setup',
    content: 'How do I configure this?',
    tags: [mockTags[1]!],
    reply_count: 0,
    created_at: '2026-01-01T11:00:00Z',
    last_activity_at: '2026-01-01T11:00:00Z'
  })
]

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()

  chatStore = useChatStore()
  permsStore = usePermissionsStore()

  chatStore.activeChannel = mockChannel
  chatStore.openThread = vi.fn()

  vi.spyOn(permsStore, 'canIn').mockReturnValue(true)
  vi.mocked(forumApi.fetchForumTags).mockResolvedValue(mockTags)
  vi.mocked(forumApi.fetchForumPosts).mockResolvedValue(mockPosts)
})

afterEach(() => {
  wrapper?.unmount()
})

describe('ForumArea', () => {
  it('loads tags and posts on mount', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    expect(forumApi.fetchForumTags).toHaveBeenCalledWith('forum-chan-1')
    expect(forumApi.fetchForumPosts).toHaveBeenCalledWith('forum-chan-1', { sort: 'activity' })

    expect(wrapper.text()).toContain('general-discussions')
    expect(wrapper.text()).toContain('All community topics')

    const cards = wrapper.findAll('[data-testid="forum-post-card"]')
    expect(cards).toHaveLength(2)
    expect(wrapper.text()).toContain('Welcome to the forum!')
    expect(wrapper.text()).toContain('Need help with setup')
  })

  it('filters posts by tag when tag pill is clicked', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    const tagPills = wrapper.findAll('[data-testid="filter-tag-pill"]')
    expect(tagPills).toHaveLength(2)

    await tagPills[0]!.trigger('click')
    await flushPromises()

    expect(forumApi.fetchForumPosts).toHaveBeenCalledWith('forum-chan-1', {
      sort: 'activity',
      tagId: 'tag-1'
    })

    const allPill = wrapper.find('[data-testid="filter-all-tags"]')
    await allPill.trigger('click')
    await flushPromises()

    expect(forumApi.fetchForumPosts).toHaveBeenCalledWith('forum-chan-1', {
      sort: 'activity'
    })
  })

  it('changes sort mode when select is changed', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    const select = wrapper.find<HTMLSelectElement>('[data-testid="forum-sort-select"]')
    await select.setValue('newest')
    await flushPromises()

    expect(forumApi.fetchForumPosts).toHaveBeenCalledWith('forum-chan-1', {
      sort: 'newest'
    })
  })

  it('opens thread when clicking a post card', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    const cards = wrapper.findAll('[data-testid="forum-post-card"]')
    await cards[0]!.trigger('click')

    expect(chatStore.openThread).toHaveBeenCalledWith(mockPosts[0])
  })

  it('displays empty state when no posts exist', async () => {
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([])
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    expect(wrapper.find('[data-testid="forum-empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('No posts yet')
  })
})
