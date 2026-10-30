import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ForumEmptyState from './ForumEmptyState.vue'
import { setLocale } from '../i18n'

beforeEach(() => setLocale('en'))

describe('ForumEmptyState', () => {
  it('explains what a forum is, how to start and shows the guidelines', async () => {
    const w = mount(ForumEmptyState, { props: { filtered: false, canPost: true, guidelines: 'Be kind.' } })
    expect(w.text()).toContain('every discussion in its own post')
    expect(w.text()).toContain('Start with a post')
    expect(w.text()).toContain('Be kind.')
    await w.get('[data-testid="forum-empty-new"]').trigger('click')
    expect(w.emitted('create')).toHaveLength(1)
  })

  it('has no start button for a member who cannot post', () => {
    const w = mount(ForumEmptyState, { props: { filtered: false, canPost: false, guidelines: '' } })
    expect(w.find('[data-testid="forum-empty-new"]').exists()).toBe(false)
    expect(w.text()).toContain('can’t start posts')
  })

  it('says nothing matched and offers to reset a filter', async () => {
    const w = mount(ForumEmptyState, { props: { filtered: true, canPost: true, guidelines: 'x' } })
    expect(w.find('[data-testid="forum-empty"]').exists()).toBe(false)
    await w.get('button').trigger('click')
    expect(w.emitted('reset')).toHaveLength(1)
  })
})
