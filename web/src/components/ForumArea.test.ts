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

  it('survives failing post and tag requests with an empty list and no tag pills', async () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(forumApi.fetchForumPosts).mockRejectedValueOnce(new Error('posts down'))
    vi.mocked(forumApi.fetchForumTags).mockRejectedValueOnce(new Error('tags down'))
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    expect(wrapper.find('[data-testid="forum-empty"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="forum-post-card"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-testid="filter-tag-pill"]')).toHaveLength(0)
    spy.mockRestore()
  })

  it('shows the tag-filter empty text and no create button when a tag filters everything out', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([])
    await wrapper.findAll('[data-testid="filter-tag-pill"]')[0]!.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="forum-empty"]').text()).toContain('No posts match')
    expect(wrapper.find('[data-testid="forum-empty"] button').exists()).toBe(false)
  })

  it('offers to start a post from the empty state', async () => {
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([])
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    await wrapper.find('[data-testid="forum-empty"] button').trigger('click')
    expect(wrapper.findComponent({ name: 'CreateForumPostModal' }).exists()).toBe(true)
  })

  it('hides the action buttons without permission and the empty-state button too', async () => {
    vi.spyOn(permsStore, 'canIn').mockReturnValue(false)
    vi.mocked(forumApi.fetchForumPosts).mockResolvedValueOnce([])
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()

    expect(wrapper.find('[data-testid="new-post-btn"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="manage-tags-btn"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="forum-empty"] button').exists()).toBe(false)
  })

  it('puts a newly created post on top and opens its thread', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()
    await wrapper.find('[data-testid="new-post-btn"]').trigger('click')

    const created = messageFixture({ id: 'post-3', channel_id: 'forum-chan-1', title: 'Brand new', content: 'fresh' })
    wrapper.findComponent({ name: 'CreateForumPostModal' }).vm.$emit('created', created)
    await flushPromises()

    const cards = wrapper.findAll('[data-testid="forum-post-card"]')
    expect(cards).toHaveLength(3)
    expect(cards[0]!.text()).toContain('Brand new')
    expect(chatStore.openThread).toHaveBeenCalledWith(created)

    wrapper.findComponent({ name: 'CreateForumPostModal' }).vm.$emit('close')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'CreateForumPostModal' }).exists()).toBe(false)
  })

  it('does not duplicate a post that is already listed', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()
    await wrapper.find('[data-testid="new-post-btn"]').trigger('click')
    wrapper.findComponent({ name: 'CreateForumPostModal' }).vm.$emit('created', mockPosts[1])
    await flushPromises()

    const cards = wrapper.findAll('[data-testid="forum-post-card"]')
    expect(cards).toHaveLength(2)
    expect(cards[0]!.text()).toContain('Need help with setup')
  })

  it('reloads the tags after the tag manager reports a change and closes it', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()
    await wrapper.find('[data-testid="manage-tags-btn"]').trigger('click')
    const modal = wrapper.findComponent({ name: 'ManageForumTagsModal' })
    expect(modal.exists()).toBe(true)

    vi.mocked(forumApi.fetchForumTags).mockClear()
    modal.vm.$emit('updated')
    await flushPromises()
    expect(forumApi.fetchForumTags).toHaveBeenCalledWith('forum-chan-1')

    modal.vm.$emit('close')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ManageForumTagsModal' }).exists()).toBe(false)
  })

  it('reloads tags when another client changed them in this channel only', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()
    vi.mocked(forumApi.fetchForumTags).mockClear()

    chatStore.forumTagsSignal = { channelId: 'other-chan', seq: 1 }
    await flushPromises()
    expect(forumApi.fetchForumTags).not.toHaveBeenCalled()

    chatStore.forumTagsSignal = { channelId: 'forum-chan-1', seq: 2 }
    await flushPromises()
    expect(forumApi.fetchForumTags).toHaveBeenCalledWith('forum-chan-1')
  })

  it('resets the tag filter and reloads when the channel changes', async () => {
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()
    await wrapper.findAll('[data-testid="filter-tag-pill"]')[0]!.trigger('click')
    await flushPromises()
    vi.mocked(forumApi.fetchForumPosts).mockClear()
    vi.mocked(forumApi.fetchForumTags).mockClear()

    chatStore.activeChannel = channelFixture({ id: 'forum-chan-2', name: 'other', type: 'forum' })
    await flushPromises()

    expect(forumApi.fetchForumTags).toHaveBeenCalledWith('forum-chan-2')
    expect(forumApi.fetchForumPosts).toHaveBeenLastCalledWith('forum-chan-2', { sort: 'activity' })
    expect(wrapper.text()).toContain('other')
  })

  it('loads nothing without an active channel', async () => {
    chatStore.activeChannel = null
    wrapper = mount(ForumArea, { attachTo: document.body })
    await flushPromises()
    expect(forumApi.fetchForumTags).not.toHaveBeenCalled()
    expect(forumApi.fetchForumPosts).not.toHaveBeenCalled()
  })
})
