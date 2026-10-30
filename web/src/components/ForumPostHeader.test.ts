import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ForumPostHeader from './ForumPostHeader.vue'
import { setLocale } from '../i18n'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { pendingConfirm } from '../lib/confirm'
import * as forumApi from '../lib/forumApi'
import { channelFixture, messageFixture, userFixture, forumTagFixture } from '../test-fixtures.fixture'
import type { Message } from '../types/domain'

vi.mock('../lib/forumApi', () => ({
  updateForumPost: vi.fn(),
  markForumPostRead: vi.fn(),
  fetchForumTags: vi.fn(),
  fetchForumSettings: vi.fn()
}))

const tags = [
  forumTagFixture({ id: 'a', channel_id: 'ch1', name: 'alpha' }),
  forumTagFixture({ id: 'b', channel_id: 'ch1', name: 'beta', position: 2 }),
  forumTagFixture({ id: 'm', channel_id: 'ch1', name: 'official', position: 3, moderated: true })
]

let w: VueWrapper
let chat: ReturnType<typeof useChatStore>

function post(extra: Partial<Message> = {}) {
  return messageFixture({ id: 'post1', channel_id: 'ch1', user_id: 'me', title: 'How do roles work?', tags: [tags[0]!], ...extra })
}

function setup({ moderator = false, author = true, extra = {} as Partial<Message> } = {}) {
  useAuthStore().user = userFixture({ id: 'me' })
  chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', type: 'forum' })
  vi.spyOn(usePermissionsStore(), 'canIn').mockImplementation((_c, perm) => perm === 'manage_messages' ? moderator : true)
  chat.activeThread = post({ user_id: author ? 'me' : 'other', ...extra })
  w = mount(ForumPostHeader, { attachTo: document.body })
}

const menuItem = (label: string) => [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(e => e.textContent?.includes(label))
async function openMenu() {
  await w.get('[data-testid="forum-post-menu"]').trigger('click')
  await flushPromises()
}
async function choose(label: string) {
  await openMenu()
  const item = menuItem(label)
  expect(item, label).toBeTruthy()
  item!.click()
  await flushPromises()
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.mocked(forumApi.markForumPostRead).mockResolvedValue('2026-01-01T10:00:00Z')
  vi.mocked(forumApi.fetchForumTags).mockResolvedValue(tags)
  vi.mocked(forumApi.fetchForumSettings).mockResolvedValue({ require_tag: false, guidelines: '' })
  vi.mocked(forumApi.updateForumPost).mockImplementation(async (_c, _p, patch) => post(patch as Partial<Message>))
})

afterEach(() => {
  pendingConfirm.value = null
  w?.unmount()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('ForumPostHeader', () => {
  it('shows the title, tags and state of the post', async () => {
    setup({ extra: { is_resolved: true, is_pinned: true, is_locked: true } })
    expect(w.get('[data-testid="forum-post-title"]').text()).toBe('How do roles work?')
    expect(w.findAll('[data-testid="forum-header-tag"]').map(t => t.text())).toEqual(['alpha'])
    expect(w.text()).toContain('Solved')
    expect(w.text()).toContain('Pinned')
    expect(w.text()).toContain('Locked')
  })

  it('renames the post from the menu and saves with Enter', async () => {
    setup()
    await choose('Edit title')
    const input = w.get<HTMLInputElement>('[data-testid="forum-title-input"]')
    expect(input.element.value).toBe('How do roles work?')
    await input.setValue('  Roles explained ')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(forumApi.updateForumPost).toHaveBeenCalledWith('ch1', 'post1', { title: 'Roles explained' })
    expect(chat.activeThread?.title).toBe('Roles explained')
    expect(w.find('[data-testid="forum-title-input"]').exists()).toBe(false)
  })

  it('sends nothing for an unchanged or blank title and Escape cancels', async () => {
    setup()
    await choose('Edit title')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(forumApi.updateForumPost).not.toHaveBeenCalled()
    expect(w.find('[data-testid="forum-title-input"]').exists()).toBe(false)

    await choose('Edit title')
    await w.get('[data-testid="forum-title-input"]').setValue('   ')
    await w.get('form').trigger('submit')
    expect(forumApi.updateForumPost).not.toHaveBeenCalled()
    await w.get('form').trigger('keydown.esc')
    expect(w.find('[data-testid="forum-title-input"]').exists()).toBe(false)
  })

  it('edits the tags of the post: loads the forum tags, preselects, saves tag_ids', async () => {
    setup()
    await w.get('[data-testid="forum-edit-tags"]').trigger('click')
    await flushPromises()
    expect(forumApi.fetchForumTags).toHaveBeenCalledWith('ch1')
    const toggles = w.findAll('[data-testid="tag-toggle-btn"]')
    expect(toggles[0]!.attributes('aria-pressed')).toBe('true')
    // The moderated tag is frozen for its author, who is no moderator.
    expect(toggles[2]!.attributes('disabled')).toBeDefined()
    await toggles[1]!.trigger('click')
    await w.get('[data-testid="forum-tags-save"]').trigger('click')
    await flushPromises()
    expect(forumApi.updateForumPost).toHaveBeenCalledWith('ch1', 'post1', { tag_ids: ['a', 'b'] })
    expect(w.find('[data-testid="forum-tag-edit"]').exists()).toBe(false)
  })

  it('lets a moderator pick the moderated tag', async () => {
    setup({ moderator: true, author: false })
    await w.get('[data-testid="forum-edit-tags"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="tag-toggle-btn"]')[2]!.attributes('disabled')).toBeUndefined()
  })

  it('refuses to save an empty tag list when the forum requires a tag', async () => {
    vi.mocked(forumApi.fetchForumSettings).mockResolvedValue({ require_tag: true, guidelines: '' })
    setup()
    await w.get('[data-testid="forum-edit-tags"]').trigger('click')
    await flushPromises()
    await w.findAll('[data-testid="tag-toggle-btn"]')[0]!.trigger('click')
    expect(w.get('[data-testid="forum-tags-save"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="forum-tags-save"]').trigger('click')
    expect(forumApi.updateForumPost).not.toHaveBeenCalled()
  })

  it('shows an error toast and keeps the editor when the tags cannot be saved', async () => {
    vi.mocked(forumApi.updateForumPost).mockRejectedValue(new Error('the post is locked'))
    setup()
    await w.get('[data-testid="forum-edit-tags"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="forum-tags-save"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(x => x.text).join()).toContain('the post is locked')
    expect(w.find('[data-testid="forum-tag-edit"]').exists()).toBe(true)
  })

  it('marks the post solved and reopens it', async () => {
    setup()
    await choose('Mark as solved')
    expect(forumApi.updateForumPost).toHaveBeenLastCalledWith('ch1', 'post1', { is_resolved: true })
    await w.vm.$nextTick()
    await choose('Reopen')
    expect(forumApi.updateForumPost).toHaveBeenLastCalledWith('ch1', 'post1', { is_resolved: false })
  })

  it('offers pin and lock only to moderators, and only edits to the author or a moderator', async () => {
    setup()
    await openMenu()
    expect(menuItem('Edit title')).toBeTruthy()
    expect(menuItem('Pin to top')).toBeUndefined()
    expect(menuItem('Lock post')).toBeUndefined()
    w.unmount()
    document.body.innerHTML = ''

    setup({ moderator: true, author: false })
    await openMenu()
    expect(menuItem('Pin to top')).toBeTruthy()
    expect(menuItem('Lock post')).toBeTruthy()
    expect(menuItem('Delete post')).toBeTruthy()
    menuItem('Lock post')!.click()
    await flushPromises()
    expect(forumApi.updateForumPost).toHaveBeenLastCalledWith('ch1', 'post1', { is_locked: true })
  })

  it('has no menu for a stranger and no tag editing on a locked post of the author', () => {
    setup({ author: false })
    expect(w.find('[data-testid="forum-post-menu"]').exists()).toBe(false)
    expect(w.find('[data-testid="forum-edit-tags"]').exists()).toBe(false)
    w.unmount()
    setup({ extra: { is_locked: true } })
    expect(w.find('[data-testid="forum-edit-tags"]').exists()).toBe(false)
    // Delete stays with the author.
    expect(w.find('[data-testid="forum-post-menu"]').exists()).toBe(true)
  })

  it('pins through the chat store', async () => {
    setup({ moderator: true })
    const pin = vi.spyOn(chat, 'setPinned').mockResolvedValue(undefined as never)
    await choose('Pin to top')
    expect(pin).toHaveBeenCalledWith('ch1', 'post1', true)
  })

  it('confirms before deleting a post and says how many replies go with it', async () => {
    setup({ extra: { reply_count: 3 } })
    const del = vi.spyOn(chat, 'deleteMessage').mockResolvedValue(undefined as never)
    await choose('Delete post')
    expect(pendingConfirm.value?.body).toContain('3 replies')
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect(del).not.toHaveBeenCalled()

    await choose('Delete post')
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(del).toHaveBeenCalledWith('ch1', 'post1')
  })

  it('marks the post read when opened and offers a jump to the first unread reply', async () => {
    setup()
    chat.threadReplies = [
      messageFixture({ id: 'old', user_id: 'other', created_at: '2026-01-01T09:00:00Z' }),
      messageFixture({ id: 'mine', user_id: 'me', created_at: '2026-01-01T10:30:00Z' }),
      messageFixture({ id: 'new1', user_id: 'other', created_at: '2026-01-01T11:00:00Z' }),
      messageFixture({ id: 'new2', user_id: 'other', created_at: '2026-01-01T12:00:00Z' })
    ]
    await flushPromises()
    expect(forumApi.markForumPostRead).toHaveBeenCalledWith('ch1', 'post1')
    const bar = w.get('[data-testid="forum-jump-unread"]')
    expect(bar.text()).toContain('2 new replies')
    await bar.trigger('click')
    expect(w.emitted('jump')).toEqual([['new1']])
    expect(w.find('[data-testid="forum-jump-unread"]').exists()).toBe(false)
  })

  it('shows no jump bar when everything was read', async () => {
    setup()
    chat.threadReplies = [messageFixture({ id: 'old', user_id: 'other', created_at: '2026-01-01T09:00:00Z' })]
    await flushPromises()
    expect(w.find('[data-testid="forum-jump-unread"]').exists()).toBe(false)
  })
})
