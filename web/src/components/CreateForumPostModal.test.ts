import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import CreateForumPostModal from './CreateForumPostModal.vue'
import { setLocale } from '../i18n'
import * as forumApi from '../lib/forumApi'
import type { ForumTag } from '../types/domain'
import { messageFixture } from '../test-fixtures.fixture'

vi.mock('../lib/forumApi', () => ({
  createForumPost: vi.fn(),
  fetchForumPosts: vi.fn(),
  fetchForumTags: vi.fn(),
  updateForumPost: vi.fn()
}))

let wrapper: VueWrapper

const sampleTags: ForumTag[] = [
  { id: 'tag-1', channel_id: 'chan-1', name: 'announcements', emoji: '📢', created_at: '2026-01-01T00:00:00Z' },
  { id: 'tag-2', channel_id: 'chan-1', name: 'help', emoji: '❓', created_at: '2026-01-01T00:00:00Z' },
  { id: 'tag-3', channel_id: 'chan-1', name: 'feedback', emoji: '💡', created_at: '2026-01-01T00:00:00Z' },
  { id: 'tag-4', channel_id: 'chan-1', name: 'bugs', emoji: '🐛', created_at: '2026-01-01T00:00:00Z' },
  { id: 'tag-5', channel_id: 'chan-1', name: 'general', emoji: '💬', created_at: '2026-01-01T00:00:00Z' },
  { id: 'tag-6', channel_id: 'chan-1', name: 'extra', emoji: '✨', created_at: '2026-01-01T00:00:00Z' }
]

function mountModal(tags = sampleTags) {
  wrapper = mount(CreateForumPostModal, {
    props: {
      channelId: 'chan-1',
      availableTags: tags
    },
    attachTo: document.body
  })
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
})

afterEach(() => {
  wrapper?.unmount()
})

describe('CreateForumPostModal', () => {
  it('disables submit when title or content is empty', async () => {
    const w = mountModal()
    const submitBtn = w.find('button[type="submit"]')
    expect(submitBtn.attributes('disabled')).toBeDefined()

    await w.find<HTMLInputElement>('#post-title').setValue('My First Post')
    expect(submitBtn.attributes('disabled')).toBeDefined()

    await w.find<HTMLTextAreaElement>('#post-content').setValue('Here is the first message body.')
    expect(submitBtn.attributes('disabled')).toBeUndefined()
  })

  it('names the tag group for assistive technology', () => {
    const w = mountModal()
    const group = w.get('[role="group"]')
    const label = w.get(`#${group.attributes('aria-labelledby')}`)
    expect(label.text()).not.toBe('')
    expect(group.findAll('[data-testid="tag-toggle-btn"]')).toHaveLength(6)
  })

  it('allows selecting up to 5 tags and disables further selections', async () => {
    const w = mountModal()
    const tagButtons = w.findAll('[data-testid="tag-toggle-btn"]')
    expect(tagButtons).toHaveLength(6)

    // Select 5 tags
    for (let i = 0; i < 5; i++) {
      await tagButtons[i]!.trigger('click')
      expect(tagButtons[i]!.attributes('aria-pressed')).toBe('true')
    }

    // 6th tag button should be disabled
    expect(tagButtons[5]!.attributes('disabled')).toBeDefined()

    // Unselect 1 tag
    await tagButtons[0]!.trigger('click')
    expect(tagButtons[0]!.attributes('aria-pressed')).toBe('false')
    expect(tagButtons[5]!.attributes('disabled')).toBeUndefined()
  })

  it('creates post successfully and emits events', async () => {
    const mockPost = messageFixture({
      id: 'post-123',
      channel_id: 'chan-1',
      title: 'My First Post',
      content: 'Here is the first message body.'
    })
    vi.mocked(forumApi.createForumPost).mockResolvedValueOnce(mockPost)

    const w = mountModal()
    await w.find<HTMLInputElement>('#post-title').setValue('My First Post')
    await w.find<HTMLTextAreaElement>('#post-content').setValue('Here is the first message body.')

    const tagButtons = w.findAll('[data-testid="tag-toggle-btn"]')
    await tagButtons[0]!.trigger('click')

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(forumApi.createForumPost).toHaveBeenCalledWith(
      'chan-1',
      'My First Post',
      'Here is the first message body.',
      ['tag-1']
    )
    expect(w.emitted('created')).toBeTruthy()
    expect(w.emitted('created')![0]![0]).toEqual(mockPost)
    expect(w.emitted('close')).toBeTruthy()
  })

  it('displays error on creation failure', async () => {
    vi.mocked(forumApi.createForumPost).mockRejectedValueOnce(new Error('Server error'))

    const w = mountModal()
    await w.find<HTMLInputElement>('#post-title').setValue('My First Post')
    await w.find<HTMLTextAreaElement>('#post-content').setValue('Here is the first message body.')

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(w.text()).toContain('Server error')
    expect(w.get('[role="alert"]').text()).toContain('Server error')
    expect(w.emitted('created')).toBeFalsy()
  })
})
