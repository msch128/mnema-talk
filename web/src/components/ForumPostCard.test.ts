import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import ForumPostCard from './ForumPostCard.vue'
import { setLocale } from '../i18n'
import { messageFixture, attachmentFixture, forumTagFixture } from '../test-fixtures.fixture'
import type { Message } from '../types/domain'

let wrapper: VueWrapper

function mountCard(overrides: Partial<Message> = {}) {
  const post = messageFixture({ title: 'Release plans', content: 'What ships next?', ...overrides })
  wrapper = mount(ForumPostCard, { props: { post } })
  return post
}

beforeEach(() => setLocale('en'))
afterEach(() => wrapper?.unmount())

describe('ForumPostCard', () => {
  it('shows title, snippet, author, reply count and tags', () => {
    mountCard({
      display_name: 'Ada', reply_count: 4,
      tags: [forumTagFixture({ id: 't1', name: 'help', emoji: '❓' }), forumTagFixture({ id: 't2', name: 'plain', color: 'blue' })]
    })
    const text = wrapper.text()
    expect(text).toContain('Release plans')
    expect(text).toContain('What ships next?')
    expect(text).toContain('Ada')
    expect(text).toContain('4')
    const tags = wrapper.findAll('[data-testid="forum-post-tag"]')
    expect(tags).toHaveLength(2)
    expect(tags[0]!.text()).toContain('❓')
    expect(tags[1]!.text()).toBe('plain')
    expect(wrapper.attributes('aria-label')).toBe('Release plans')
  })

  it('falls back to an untitled label, zero replies and no snippet for an empty post', () => {
    mountCard({ title: '', content: '', reply_count: 0 })
    expect(wrapper.find('h3').text()).toBe('Untitled post')
    expect(wrapper.attributes('aria-label')).toBe('Untitled post')
    expect(wrapper.find('p').exists()).toBe(false)
  })

  it('treats a missing reply count as zero', () => {
    const post = messageFixture({ title: 'x' })
    delete (post as { reply_count?: number }).reply_count
    wrapper = mount(ForumPostCard, { props: { post } })
    expect(wrapper.find('[title]').text()).toBe('0')
  })

  it('marks a locked post', () => {
    mountCard({ is_locked: true })
    expect(wrapper.text()).toContain('Locked')
  })

  it('names the last activity only for a post that has replies', async () => {
    mountCard({ reply_count: 0, last_activity_at: '2026-02-03T10:00:00Z' })
    expect(wrapper.text()).not.toContain('Active')
    await wrapper.setProps({ post: messageFixture({ title: 'x', reply_count: 2, last_activity_at: '2026-02-03T10:00:00Z' }) })
    expect(wrapper.text()).toContain('Active')
  })

  it('marks an unread post with a dot and in its label', () => {
    mountCard()
    expect(wrapper.find('[data-testid="forum-post-unread"]').exists()).toBe(false)
    wrapper.unmount()
    wrapper = mount(ForumPostCard, { props: { post: messageFixture({ title: 'Release plans' }), unread: true } })
    expect(wrapper.find('[data-testid="forum-post-unread"]').exists()).toBe(true)
    expect(wrapper.attributes('aria-label')).toBe('Release plans (Unread)')
    expect(wrapper.attributes('data-unread')).toBe('true')
  })

  it('shows the pinned and solved marks', () => {
    mountCard({ is_pinned: true, is_resolved: true })
    expect(wrapper.find('[data-testid="forum-post-pinned"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="forum-post-solved"]').text()).toContain('Solved')
  })

  it('previews the first image of the first message, not a file or a deleted image', () => {
    mountCard({ attachments: [
      attachmentFixture({ id: 'f', mime_type: 'application/pdf', url: '/api/media/f' }),
      attachmentFixture({ id: 'd', is_deleted: true, url: '/api/media/d' }),
      attachmentFixture({ id: 'i', mime_type: 'image/png', url: '/api/media/i' })
    ] })
    expect(wrapper.get('[data-testid="forum-post-thumbnail"]').attributes('src')).toBe('/api/media/i')
    wrapper.unmount()
    mountCard({ attachments: [] })
    expect(wrapper.find('[data-testid="forum-post-thumbnail"]').exists()).toBe(false)
  })

  it('renders no date when the creation time is missing', () => {
    mountCard({ created_at: '' })
    expect(wrapper.find('span.shrink-0').text()).toBe('')
  })

  it('emits select on click, Enter and Space', async () => {
    const post = mountCard()
    await wrapper.trigger('click')
    await wrapper.trigger('keydown.enter')
    await wrapper.trigger('keydown.space')
    expect(wrapper.emitted('select')).toEqual([[post], [post], [post]])
  })
})
