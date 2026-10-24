import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import ForumPostCard from './ForumPostCard.vue'
import { setLocale } from '../i18n'
import { messageFixture } from '../test-fixtures.fixture'
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
      tags: [{ id: 't1', channel_id: 'c', name: 'help', emoji: '❓', created_at: '2026-01-01T00:00:00Z' },
        { id: 't2', channel_id: 'c', name: 'plain', created_at: '2026-01-01T00:00:00Z' }]
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

  it('shows the last activity only when it differs from the creation time', async () => {
    mountCard({ created_at: '2026-01-01T10:00:00Z', last_activity_at: '2026-01-01T10:00:00Z' })
    expect(wrapper.text()).not.toContain('Last activity')
    await wrapper.setProps({ post: messageFixture({ created_at: '2026-01-01T10:00:00Z', last_activity_at: '2026-02-03T10:00:00Z' }) })
    expect(wrapper.text()).toContain('Feb')
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
