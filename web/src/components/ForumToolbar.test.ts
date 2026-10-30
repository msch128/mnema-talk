import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ForumToolbar from './ForumToolbar.vue'
import { setLocale } from '../i18n'
import { forumTagFixture } from '../test-fixtures.fixture'

const tags = [forumTagFixture({ id: 'a', name: 'alpha' }), forumTagFixture({ id: 'b', name: 'beta', position: 2 })]

function mountBar(tagId: string | null = null, extra: Record<string, unknown> = {}) {
  const w = mount(ForumToolbar, {
    props: {
      tags, search: '', sort: 'activity' as 'activity' | 'newest', tagId,
      'onUpdate:search': (v: string) => w.setProps({ search: v }),
      'onUpdate:sort': (v: string) => w.setProps({ sort: v as 'activity' | 'newest' }),
      'onUpdate:tagId': (v: string | null) => w.setProps({ tagId: v }),
      ...extra
    }
  })
  return w
}

beforeEach(() => setLocale('en'))

describe('ForumToolbar', () => {
  it('types into the search, clears it with the button and with Escape', async () => {
    const w = mountBar()
    expect(w.find('button[aria-label="Clear search"]').exists()).toBe(false)
    await w.get('[data-testid="forum-search"]').setValue('roles')
    expect(w.props('search')).toBe('roles')
    await w.get('button[aria-label="Clear search"]').trigger('click')
    expect(w.props('search')).toBe('')
    await w.get('[data-testid="forum-search"]').setValue('again')
    await w.get('[data-testid="forum-search"]').trigger('keydown.esc')
    expect(w.props('search')).toBe('')
  })

  it('selects a tag, toggles it off again and resets with All', async () => {
    const w = mountBar()
    const pills = () => w.findAll('[data-testid="filter-tag-pill"]')
    expect(w.get('[data-testid="filter-all-tags"]').attributes('aria-pressed')).toBe('true')
    await pills()[1]!.trigger('click')
    expect(w.props('tagId')).toBe('b')
    expect(pills()[1]!.attributes('aria-pressed')).toBe('true')
    await pills()[1]!.trigger('click')
    expect(w.props('tagId')).toBeNull()
    await pills()[0]!.trigger('click')
    await w.get('[data-testid="filter-all-tags"]').trigger('click')
    expect(w.props('tagId')).toBeNull()
  })

  it('changes the sort and hides the filter row for a forum without tags', async () => {
    const w = mountBar(null, { tags: [] })
    await w.get('[data-testid="forum-sort-select"]').setValue('newest')
    expect(w.props('sort')).toBe('newest')
    expect(w.find('[data-testid="filter-all-tags"]').exists()).toBe(false)
  })
})
